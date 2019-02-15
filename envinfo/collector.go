package envinfo

import (
	"sync"
	"time"
	"wmi_exporter/collector"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"
)

const namespace = "envinfo"

var (
	factories      = make(map[string]collector.Collector)
	collectorState = make(map[string]bool)

	envScrapeDurationDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "collector_duration_seconds"),
		"envinfo: Duration of a collection.",
		[]string{"collector"},
		nil,
	)
	envScrapeSuccessDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "collector_success"),
		"envinfo: Whether the collector was successful.",
		[]string{"collector"},
		nil,
	)
)

type EnvCollector struct {
	collectors map[string]collector.Collector
}

func NewEnvCollector() *EnvCollector {

	coll := make(map[string]collector.Collector)

	for k, enable := range collectorState {
		if enable {
			if c, ok := factories[k]; ok {
				coll[k] = c
			}
		}
	}

	return &EnvCollector{coll}
}

func (coll EnvCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- envScrapeDurationDesc
	ch <- envScrapeSuccessDesc
}

// Collect sends the collected metrics from each of the collectors to
// prometheus. Collect could be called several times concurrently
// and thus its run is protected by a single mutex.
func (coll EnvCollector) Collect(ch chan<- prometheus.Metric) {
	wg := sync.WaitGroup{}
	wg.Add(len(coll.collectors))
	for name, c := range coll.collectors {
		go func(name string, c collector.Collector) {
			execute(name, c, ch)
			wg.Done()
		}(name, c)
	}

	wg.Wait()
}

func execute(name string, c collector.Collector, ch chan<- prometheus.Metric) {
	begin := time.Now()
	err := c.Collect(ch)
	duration := time.Since(begin)
	var success float64

	if err != nil {
		log.Errorf("collector %s failed after %fs: %s", name, duration.Seconds(), err)
		success = 0
	} else {
		log.Debugf("collector %s succeeded after %fs.", name, duration.Seconds())
		success = 1
	}
	_ = success
	// ch <- prometheus.MustNewConstMetric(
	// 	envScrapeDurationDesc,
	// 	prometheus.GaugeValue,
	// 	duration.Seconds(),
	// 	name,
	// )
	// ch <- prometheus.MustNewConstMetric(
	// 	envScrapeSuccessDesc,
	// 	prometheus.GaugeValue,
	// 	success,
	// 	name,
	// )
}
