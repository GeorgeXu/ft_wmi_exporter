package cloudcare

import (
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	config_util "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/storage"
)

type Storage struct {
	logger log.Logger
	mtx    sync.RWMutex

	// For writes
	queues []*QueueManager

	// For reads
	queryables []storage.Queryable
	//localStartTimeCallback startTimeCallback
	flushDeadline time.Duration
}

func (s *Storage) Add(l labels.Labels, t int64, v float64) (uint64, error) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	for _, q := range s.queues {
		if err := q.Append(&model.Sample{
			Metric:    labelsToMetric(l),
			Timestamp: model.Time(t),
			Value:     model.SampleValue(v),
		}); err != nil {
			panic(err) // QueueManager.Append() should always return nil as per doc string.
		}
	}
	return 0, nil
}

// NewStorage returns a remote.Storage.
func NewStorage(l log.Logger, stCallback func(), flushDeadline time.Duration) *Storage {
	if l == nil {
		l = log.NewNopLogger()
	}
	return &Storage{
		logger: l,
		//localStartTimeCallback: stCallback,
		flushDeadline: flushDeadline,
	}
}

// ApplyConfig updates the state as the new config requires.
func (s *Storage) ApplyConfig(conf *config.Config) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	newQueues := []*QueueManager{}

	rwCfg := config.DefaultRemoteWriteConfig
	rwCfg.URL = &config_util.URL{
		URL: GetDataBridgeUrl(),
	}

	clientCfg := &ClientConfig{
		URL:              rwCfg.URL,
		Timeout:          rwCfg.RemoteTimeout,
		HTTPClientConfig: rwCfg.HTTPClientConfig,
	}

	c, err := NewClient(0, s.logger, clientCfg)
	if err != nil {
		return err
	}

	newQueues = append(newQueues, newQueueManager(
		s.logger,
		rwCfg.QueueConfig,
		conf.GlobalConfig.ExternalLabels,
		rwCfg.WriteRelabelConfigs,
		c,
		s.flushDeadline,
	))

	for _, q := range s.queues {
		q.Stop()
	}

	s.queues = newQueues

	for _, q := range s.queues {
		q.Start()
	}

	return nil
}

// Close the background processing of the storage queues.
func (s *Storage) Close() error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	for _, q := range s.queues {
		q.Stop()
	}

	return nil
}
