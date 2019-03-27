package envinfo

import (
	"encoding/base64"
	"encoding/json"
	"log"
	"os/exec"
	"sync"
	"time"
	"wmi_exporter/collector"

	"github.com/prometheus/client_golang/prometheus"
)

const namespace = "kv_node" //"envinfo"

type queryResult struct {
	rawJson    string
	formatJson []map[string]string
}

var (
	OSQuerydPath = ""
	// run osquery:  ./osqueryd -S --json 'select * from users'
	//   -S: run as shell mode
	//   --json: output result in json format

	factories      = make(map[string]func(*envCfg, bool) (collector.Collector, error))
	collectorState = make(map[string]bool)
	factoryArgs    = make(map[string]*envCfg)

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

func registerCollector(collector string, isDefaultEnabled bool, factory func(*envCfg, bool) (collector.Collector, error), arg *envCfg) {
	collectorState[collector] = isDefaultEnabled
	factories[collector] = factory
	if arg != nil {
		factoryArgs[collector] = arg
	}
}

type EnvInfoCollector struct {
	collectors map[string]collector.Collector
	jsonFormat bool
}

func NewEnvInfoCollector(jsonFormat bool) *EnvInfoCollector {

	collectors := make(map[string]collector.Collector)
	for k, enable := range collectorState {
		if enable {
			if fn, ok := factories[k]; ok {
				c, err := fn(factoryArgs[k], jsonFormat)
				if err == nil {
					collectors[k] = c
				}
			}
		}
	}

	return &EnvInfoCollector{
		collectors: collectors,
		jsonFormat: jsonFormat,
	}
}

func (c EnvInfoCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- envScrapeDurationDesc
	ch <- envScrapeSuccessDesc
}

// Collect sends the collected metrics from each of the collectors to
// prometheus. Collect could be called several times concurrently
// and thus its run is protected by a single mutex.
func (ec EnvInfoCollector) Collect(ch chan<- prometheus.Metric) {
	wg := sync.WaitGroup{}
	wg.Add(len(ec.collectors))

	ts := time.Now()
	log.Printf("[info] start kvs, jsonformat(%v)", ec.jsonFormat)

	for name, c := range ec.collectors {
		go func(name string, _c collector.Collector) {
			execute(name, _c, ch)
			wg.Done()
		}(name, c)
	}

	wg.Wait()

	te := time.Now()

	log.Printf("[info] finish kvs, used: %v", te.Sub(ts))

}

func execute(name string, c collector.Collector, ch chan<- prometheus.Metric) {
	begin := time.Now()
	err := c.Collect(ch)
	duration := time.Since(begin)

	if err != nil {
		log.Printf("[error] collector %s failed after %fs: %s", name, duration.Seconds(), err)
	}

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

//for debug
type KVUser struct {
	Username string `json:"username"`
}

func doQuery(sql string, jsonFormat bool) (*queryResult, error) {
	cmd := exec.Command(OSQuerydPath, []string{`-S`, `--json`, sql}...)

	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var res queryResult
	//集群模式下
	if !jsonFormat {
		err = json.Unmarshal(out, &res.formatJson)
		if err != nil {
			return nil, err
		}
	} else {
		//for debug
		// if strings.Contains(sql, "from users") {
		// 	var kvusers []KVUser
		// 	if err := json.Unmarshal(out, &kvusers); err == nil {
		// 		log.Println("[debug] kv_users:", kvusers)
		// 	}
		// }
		res.rawJson = base64.RawURLEncoding.EncodeToString(out)
	}

	return &res, nil
}
