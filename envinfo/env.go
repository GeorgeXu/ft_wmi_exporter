package envinfo

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"runtime"
	"wmi_exporter/collector"

	"github.com/prometheus/client_golang/prometheus"
)

var forbidTags = []string{
	"alertname",
	"exported_",
	"__name__",
	"__scheme__",
	"__address__",
	"__metrics_path__",
	"__",
	"__meta_",
	"__tmp_",
	"__param_",
	"job",
	"instance",
	"le",
	"quantile",
}

const (
	envCollectorTypeCat     = `cat`
	envCollectorTypeOSQuery = `osquery`

	envPlatformWindows = `windows`
	envPlatformLinux   = `linux`

	fileSep = "\r\n"
)

type envCfg struct {
	Platform  string `json:"platform"`
	SubSystem string `json:"sub_system"`
	Type      string `json:"type"`
	SQL       string `json:"sql"`

	Files []string `json:"files"`
	Any   bool     `json:"any"`

	Tags    []string `json:"tags"`
	Help    string   `json:"help"`
	Enabled bool     `json:"enabled"`
}

type envCfgs struct {
	Envs []*envCfg `json:"kvs"`
}

type envCollector struct {
	desc *prometheus.Desc
	cfg  *envCfg

	jsonFormat bool
}

func NewEnvCollector(conf *envCfg, jsonFormat bool) (collector.Collector, error) {
	c := &envCollector{
		cfg:        conf,
		jsonFormat: jsonFormat,
	}

	//conf.Tags = append(conf.Tags, cloudcare.TagUploaderUID)
	c.desc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", conf.SubSystem),
		conf.Help, conf.Tags, nil)

	return c, nil
}

func Init(cfgFile string) {

	var envCfgs envCfgs
	j, err := ioutil.ReadFile(cfgFile)
	if err != nil {
		log.Fatalf("[fatal] open %s failed: %s", cfgFile, err)
	}

	if err := json.Unmarshal(j, &envCfgs); err != nil {
		log.Fatalf("[fatal] yaml load %s failed: %s", cfgFile, err)
	}

	for _, ec := range envCfgs.Envs {
		if ec.Platform != "" && ec.Platform == runtime.GOOS {

			//ec.Tags = append(ec.Tags, cloudcare.TagUploaderUID, cloudcare.TagHost) // 追加默认 tags
			registerCollector(ec.SubSystem, ec.Enabled, NewEnvCollector, ec)

		} else {
			//log.Printf("[info] skip collector %s(platform: %s)", ec.SubSystem, ec.Platform)
		}
	}

}

func (ec *envCollector) Collect(ch chan<- prometheus.Metric) error {
	switch ec.cfg.Type {
	case envCollectorTypeCat:
		//return ec.catUpdate(ch)
	case envCollectorTypeOSQuery:
		return ec.osqueryUpdate(ch)
	default:
		log.Printf("[warn] unsupported env collector type: %s", ec.cfg.Type)
	}
	return nil
}

func newEnvMetric(ec *envCollector, envVal string) prometheus.Metric {
	return prometheus.MustNewConstMetric(ec.desc, prometheus.GaugeValue, float64(-1), envVal)
}

func (ec *envCollector) osqueryUpdate(ch chan<- prometheus.Metric) error {
	res, err := doQuery(ec.cfg.SQL, ec.jsonFormat)
	if err != nil {
		return err
	}

	//集群模式下，兼容promtheous
	if !ec.jsonFormat {
		n := len(res.formatJson)
		if n == 0 {
			return nil
		}

		//uploaduidKey := "ft_" + cloudcare.TagUploaderUID //集群模式下 避免和osquery产生的结果冲突

		entry := res.formatJson[0]
		var keys = []string{}
		var tuned []string
		for k := range entry {
			bforbid := false
			for _, ft := range forbidTags {
				if ft == k {
					bforbid = true
					break
				}
			}
			if bforbid {
				k = k + "_"
				tuned = append(tuned, k)
			}
			keys = append(keys, k)
		}

		desc := prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", ec.cfg.SubSystem),
			ec.cfg.Help,
			keys,
			nil,
		)

		for _, m := range res.formatJson {
			//m[uploaduidKey] = cfg.Cfg.UploaderUID
			var vals []string
			for _, k := range keys {
				for _, tk := range tuned {
					if tk == k {
						k = k[:len(k)-1]
						break
					}
				}
				vals = append(vals, m[k])
			}
			ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, -1, vals...)
		}
	} else {
		ch <- newEnvMetric(ec, res.rawJson)
	}

	return nil
}
