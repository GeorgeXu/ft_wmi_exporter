package cloudcare

import (
	"net/url"
	"sync"
	"time"

	config_util "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/storage"
)

type Storage struct {
	//logger log.Logger
	mtx sync.RWMutex

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
func NewStorage(remoteUrl string, flushDeadline time.Duration) (*Storage, error) {

	u, err := url.Parse(remoteUrl)
	if err != nil {
		return nil, err
	}

	rwCfg := config.DefaultRemoteWriteConfig

	rwCfg.URL = &config_util.URL{
		URL: u,
	}

	clientCfg := &ClientConfig{
		URL:              rwCfg.URL,
		Timeout:          rwCfg.RemoteTimeout,
		HTTPClientConfig: rwCfg.HTTPClientConfig,
	}

	c, err := NewClient(0, clientCfg)
	if err != nil {
		return nil, err
	}

	qm := []*QueueManager{
		newQueueManager(c, flushDeadline),
	}

	for _, q := range qm {
		q.Start()
	}

	return &Storage{
		flushDeadline: flushDeadline,
		mtx:           sync.RWMutex{},
		queues:        qm,
	}, nil

}

// ApplyConfig updates the state as the new config requires.
// func (s *Storage) ApplyConfig(conf *config.Config, ur *url.URL) error {
// 	s.mtx.Lock()
// 	defer s.mtx.Unlock()

// 	newQueues := []*QueueManager{}

// 	urls := []*url.URL{ur}

// 	for _, u := range urls {
// 		rwCfg := config.DefaultRemoteWriteConfig
// 		rwCfg.URL = &config_util.URL{
// 			URL: u,
// 		}

// 		clientCfg := &ClientConfig{
// 			URL:              rwCfg.URL,
// 			Timeout:          rwCfg.RemoteTimeout,
// 			HTTPClientConfig: rwCfg.HTTPClientConfig,
// 		}

// 		c, err := NewClient(0, clientCfg)
// 		if err != nil {
// 			return err
// 		}

// 		newQueues = append(newQueues, newQueueManager(
// 			rwCfg.QueueConfig,
// 			conf.GlobalConfig.ExternalLabels,
// 			rwCfg.WriteRelabelConfigs,
// 			c,
// 			s.flushDeadline,
// 		))
// 	}

// 	for _, q := range s.queues {
// 		q.Stop()
// 	}

// 	s.queues = newQueues

// 	for _, q := range s.queues {
// 		q.Start()
// 	}

// 	return nil
// }

// Close the background processing of the storage queues.
func (s *Storage) Close() error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	for _, q := range s.queues {
		q.Stop()
	}

	return nil
}
