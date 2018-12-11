package cloudcare

import (
	"context"
	"encoding/json"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/prompb"
	"golang.org/x/time/rate"
)

// String constants for instrumentation.
const (

	// We track samples in/out and how long pushes take using an Exponentially
	// Weighted Moving Average.
	ewmaWeight          = 0.2
	shardUpdateDuration = 10 * time.Second

	// Allow 30% too many shards before scaling down.
	shardToleranceFraction = 0.3

	// Limit to 1 log event every 10s
	logRateLimit = 0.1
	logBurst     = 10
)

type StorageClient interface {
	// Store stores the given samples in the remote storage.
	Store(context.Context, *prompb.WriteRequest) error
	// Name identifies the remote storage implementation.
	Name() string
}

type QueueManager struct {
	logger log.Logger

	flushDeadline  time.Duration
	cfg            config.QueueConfig
	externalLabels model.LabelSet
	relabelConfigs []*config.RelabelConfig
	client         StorageClient
	queueName      string
	logLimiter     *rate.Limiter

	shardsMtx   sync.Mutex
	shards      *shards
	numShards   int
	reshardChan chan int
	quit        chan struct{}
	wg          sync.WaitGroup

	samplesIn, samplesOut, samplesOutDuration *ewmaRate
	integralAccumulator                       float64
}

func dumpSamples(samples model.Samples) (string, error) {
	b, err := json.Marshal(samples)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func newQueueManager(logger log.Logger, cfg config.QueueConfig, externalLabels model.LabelSet, relabelConfigs []*config.RelabelConfig, client StorageClient, flushDeadline time.Duration) *QueueManager {
	if logger == nil {
		logger = log.NewNopLogger()
	} else {
		logger = log.With(logger, "queue", client.Name())
	}
	t := &QueueManager{
		logger:         logger,
		flushDeadline:  flushDeadline,
		cfg:            cfg,
		externalLabels: externalLabels,
		relabelConfigs: relabelConfigs,
		client:         client,
		queueName:      client.Name(),

		logLimiter:  rate.NewLimiter(logRateLimit, logBurst),
		numShards:   1,
		reshardChan: make(chan int),
		quit:        make(chan struct{}),

		samplesIn:          newEWMARate(ewmaWeight, shardUpdateDuration),
		samplesOut:         newEWMARate(ewmaWeight, shardUpdateDuration),
		samplesOutDuration: newEWMARate(ewmaWeight, shardUpdateDuration),
	}
	t.shards = t.newShards(t.numShards)
	//numShards.WithLabelValues(t.queueName).Set(float64(t.numShards))
	//shardCapacity.WithLabelValues(t.queueName).Set(float64(t.cfg.Capacity))

	// Initialise counter labels to zero.
	// sentBatchDuration.WithLabelValues(t.queueName)
	// succeededSamplesTotal.WithLabelValues(t.queueName)
	// failedSamplesTotal.WithLabelValues(t.queueName)
	// droppedSamplesTotal.WithLabelValues(t.queueName)

	return t
}

// Append queues a sample to be sent to the remote storage. It drops the
// sample on the floor if the queue is full.
// Always returns nil.
func (t *QueueManager) Append(s *model.Sample) error {
	snew := *s
	snew.Metric = s.Metric.Clone()

	for ln, lv := range t.externalLabels {
		if _, ok := s.Metric[ln]; !ok {
			snew.Metric[ln] = lv
		}
	}

	//snew.Metric = model.Metric(
	//relabel.Process(model.LabelSet(snew.Metric), t.relabelConfigs...))

	if snew.Metric == nil {
		return nil
	}

	t.shardsMtx.Lock()
	enqueued := t.shards.enqueue(&snew)
	t.shardsMtx.Unlock()

	if enqueued {
		//queueLength.WithLabelValues(t.queueName).Inc()
	} else {
		//droppedSamplesTotal.WithLabelValues(t.queueName).Inc()
		if t.logLimiter.Allow() {
			level.Warn(t.logger).Log("msg", "Remote storage queue full, discarding sample. Multiple subsequent messages of this kind may be suppressed.")
		}
	}
	return nil
}

// NeedsThrottling implements storage.SampleAppender. It will always return
// false as a remote storage drops samples on the floor if backlogging instead
// of asking for throttling.
func (*QueueManager) NeedsThrottling() bool {
	return false
}

// Start the queue manager sending samples to the remote storage.
// Does not block.
func (t *QueueManager) Start() {
	//t.wg.Add(2)
	//go t.updateShardsLoop()
	//go t.reshardLoop()

	t.shardsMtx.Lock()
	defer t.shardsMtx.Unlock()
	t.shards.start()
}

// Stop stops sending samples to the remote storage and waits for pending
// sends to complete.
func (t *QueueManager) Stop() {
	level.Info(t.logger).Log("msg", "Stopping remote storage...")
	close(t.quit)
	t.wg.Wait()

	t.shardsMtx.Lock()
	defer t.shardsMtx.Unlock()
	t.shards.stop(t.flushDeadline)

	level.Info(t.logger).Log("msg", "Remote storage stopped.")
}

func (t *QueueManager) updateShardsLoop() {
	defer t.wg.Done()

	ticker := time.NewTicker(shardUpdateDuration)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			t.calculateDesiredShards()
		case <-t.quit:
			return
		}
	}
}

func (t *QueueManager) calculateDesiredShards() {
	t.samplesIn.tick()
	t.samplesOut.tick()
	t.samplesOutDuration.tick()

	// We use the number of incoming samples as a prediction of how much work we
	// will need to do next iteration.  We add to this any pending samples
	// (received - send) so we can catch up with any backlog. We use the average
	// outgoing batch latency to work out how many shards we need.
	var (
		samplesIn          = t.samplesIn.rate()
		samplesOut         = t.samplesOut.rate()
		samplesPending     = samplesIn - samplesOut
		samplesOutDuration = t.samplesOutDuration.rate()
	)

	// We use an integral accumulator, like in a PID, to help dampen oscillation.
	t.integralAccumulator = t.integralAccumulator + (samplesPending * 0.1)

	if samplesOut <= 0 {
		return
	}

	var (
		timePerSample = samplesOutDuration / samplesOut
		desiredShards = (timePerSample * (samplesIn + samplesPending + t.integralAccumulator)) / float64(time.Second)
	)
	level.Debug(t.logger).Log("msg", "QueueManager.caclulateDesiredShards",
		"samplesIn", samplesIn, "samplesOut", samplesOut,
		"samplesPending", samplesPending, "desiredShards", desiredShards)

	// Changes in the number of shards must be greater than shardToleranceFraction.
	var (
		lowerBound = float64(t.numShards) * (1. - shardToleranceFraction)
		upperBound = float64(t.numShards) * (1. + shardToleranceFraction)
	)
	level.Debug(t.logger).Log("msg", "QueueManager.updateShardsLoop",
		"lowerBound", lowerBound, "desiredShards", desiredShards, "upperBound", upperBound)
	if lowerBound <= desiredShards && desiredShards <= upperBound {
		return
	}

	numShards := int(math.Ceil(desiredShards))
	if numShards > t.cfg.MaxShards {
		numShards = t.cfg.MaxShards
	} else if numShards < 1 {
		numShards = 1
	}
	if numShards == t.numShards {
		return
	}

	// Resharding can take some time, and we want this loop
	// to stay close to shardUpdateDuration.
	select {
	case t.reshardChan <- numShards:
		level.Info(t.logger).Log("msg", "Remote storage resharding", "from", t.numShards, "to", numShards)
		t.numShards = numShards
	default:
		level.Info(t.logger).Log("msg", "Currently resharding, skipping.")
	}
}

func (t *QueueManager) reshardLoop() {
	defer t.wg.Done()

	for {
		select {
		case numShards := <-t.reshardChan:
			t.reshard(numShards)
		case <-t.quit:
			return
		}
	}
}

func (t *QueueManager) reshard(n int) {
	//numShards.WithLabelValues(t.queueName).Set(float64(n))

	t.shardsMtx.Lock()
	newShards := t.newShards(n)
	oldShards := t.shards
	t.shards = newShards
	t.shardsMtx.Unlock()

	oldShards.stop(t.flushDeadline)

	// We start the newShards after we have stopped (the therefore completely
	// flushed) the oldShards, to guarantee we only every deliver samples in
	// order.
	newShards.start()
}

type shards struct {
	qm      *QueueManager
	queues  []chan *model.Sample
	done    chan struct{}
	running int32
	ctx     context.Context
	cancel  context.CancelFunc
}

func (t *QueueManager) newShards(numShards int) *shards {
	queues := make([]chan *model.Sample, numShards)
	for i := 0; i < numShards; i++ {
		queues[i] = make(chan *model.Sample, t.cfg.Capacity)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &shards{
		qm:      t,
		queues:  queues,
		done:    make(chan struct{}),
		running: int32(numShards),
		ctx:     ctx,
		cancel:  cancel,
	}
	return s
}

func (s *shards) len() int {
	return len(s.queues)
}

func (s *shards) start() {
	for i := 0; i < len(s.queues); i++ {
		go s.runShard(i)
	}
}

func (s *shards) stop(deadline time.Duration) {
	// Attempt a clean shutdown.
	for _, shard := range s.queues {
		close(shard)
	}
	select {
	case <-s.done:
		return
	case <-time.After(deadline):
		level.Error(s.qm.logger).Log("msg", "Failed to flush all samples on shutdown")
	}

	// Force an unclean shutdown.
	s.cancel()
	<-s.done
	return
}

func (s *shards) enqueue(sample *model.Sample) bool {
	s.qm.samplesIn.incr(1)

	fp := sample.Metric.FastFingerprint()
	shard := uint64(fp) % uint64(len(s.queues))

	select {
	case s.queues[shard] <- sample:
		return true
	default:
		return false
	}
}

func addTags(s *model.Sample) {
	s.Metric[model.LabelName(`cloud_asset_id`)] = model.LabelValue(CorsairCloudAssetID)
	s.Metric[model.LabelName(`host`)] = model.LabelValue(CorsairHost)
}

func (s *shards) runShard(i int) {
	defer func() {
		if atomic.AddInt32(&s.running, -1) == 0 {
			close(s.done)
		}
	}()

	queue := s.queues[i]

	// Send batches of at most MaxSamplesPerSend samples to the remote storage.
	// If we have fewer samples than that, flush them out after a deadline
	// anyways.
	pendingSamples := model.Samples{}

	timer := time.NewTimer(time.Duration(s.qm.cfg.BatchSendDeadline))
	stop := func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}
	defer stop()

	for {
		select {
		case <-s.ctx.Done():
			return

		case sample, ok := <-queue:
			if !ok {
				if len(pendingSamples) > 0 {
					level.Debug(s.qm.logger).Log("msg", "Flushing samples to remote storage...", "count", len(pendingSamples))
					s.sendSamples(pendingSamples)
					level.Debug(s.qm.logger).Log("msg", "Done flushing.")
				}
				return
			}

			// queueLength.WithLabelValues(s.qm.queueName).Dec()
			addTags(sample)

			pendingSamples = append(pendingSamples, sample)

			if len(pendingSamples) >= s.qm.cfg.MaxSamplesPerSend {
				s.sendSamples(pendingSamples[:s.qm.cfg.MaxSamplesPerSend])
				pendingSamples = pendingSamples[s.qm.cfg.MaxSamplesPerSend:]

				stop()
				timer.Reset(time.Duration(s.qm.cfg.BatchSendDeadline))
			}

		case <-timer.C:
			if len(pendingSamples) > 0 {
				s.sendSamples(pendingSamples)
				pendingSamples = pendingSamples[:0]
			}
			timer.Reset(time.Duration(s.qm.cfg.BatchSendDeadline))
		}
	}
}

func (s *shards) sendSamples(samples model.Samples) {
	begin := time.Now()
	s.sendSamplesWithBackoff(samples)

	// These counters are used to calculate the dynamic sharding, and as such
	// should be maintained irrespective of success or failure.
	s.qm.samplesOut.incr(int64(len(samples)))
	s.qm.samplesOutDuration.incr(int64(time.Since(begin)))
}

// sendSamples to the remote storage with backoff for recoverable errors.
func (s *shards) sendSamplesWithBackoff(samples model.Samples) {
	backoff := s.qm.cfg.MinBackoff
	req := ToWriteRequest(samples)

	for retries := s.qm.cfg.MaxRetries; retries > 0; retries-- {
		err := s.qm.client.Store(s.ctx, req)

		if err == nil {
			return
		}

		ds, _ := dumpSamples(samples)

		level.Warn(s.qm.logger).Log("msg",
			"Error sending samples to remote storage", "samples",
			ds, "err", err)

		if _, ok := err.(recoverableError); !ok {
			break
		}

		time.Sleep(time.Duration(backoff))
		backoff = backoff * 2
		if backoff > s.qm.cfg.MaxBackoff {
			backoff = s.qm.cfg.MaxBackoff
		}
	}
}
