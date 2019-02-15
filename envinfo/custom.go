package envinfo

import (
	"time"
	"wmi_exporter/cloudcare"
	"wmi_exporter/collector"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"
)

var (
	queryInfos = []*EnvQueryInfo{}
)

type EnvQueryInfo struct {
	SubSystem string   `yaml:"sub_system"`
	Name      string   `yaml:"name"`
	Type      string   `yaml:"type"`
	Tags      []string `yaml:"tags"`
	Help      string   `yaml:"help"`
	Enable    bool     `yaml:"enabled"`
	Sql       string   `yaml:"sql"`
	Platform  string   `yaml:"platform"`
}

func (item *EnvQueryInfo) fullName() string {
	return namespace + "_" + item.SubSystem + "_" + item.Name
}

func (item *EnvQueryInfo) envMetricDesc() *prometheus.Desc {
	return prometheus.NewDesc(
		item.fullName(),
		item.Help,
		item.Tags,
		map[string]string{
			"host":         cloudcare.CorsairHost,
			"uploader_uid": cloudcare.CorsairUploaderUID,
		},
	)
}

type descWrap struct {
	desc         *prometheus.Desc
	envQueryInfo *EnvQueryInfo
}

type EnvinfoCollector struct {
	descs       []descWrap
	errTime     int64
	errTryCount int
}

const (
	maxTryCount    = 3
	maxTryInterval = 60 * 3
)

func (c *EnvinfoCollector) shouldSkip() bool {
	current := time.Now().Unix()
	if c.errTime > 0 && (current-c.errTime) > maxTryInterval {
		c.errTryCount = 0
		c.errTime = current
		return false
	}
	return (c.errTryCount >= 3)
}

func (c *EnvinfoCollector) updateErrCounter() {
	if c.errTryCount < maxTryCount {
		c.errTryCount++
	}
	if c.errTryCount == maxTryCount {
		c.errTime = time.Now().Unix()
	}
}

func NewEnvCustomCollector(infos []*EnvQueryInfo) (collector.Collector, error) {

	var descs []descWrap
	for _, info := range infos {
		var d descWrap
		d.desc = info.envMetricDesc()
		d.envQueryInfo = info
		descs = append(descs, d)
	}
	return &EnvinfoCollector{
		descs:       descs,
		errTime:     0,
		errTryCount: 0}, nil
}

func (c *EnvinfoCollector) Collect(ch chan<- prometheus.Metric) error {
	if c.shouldSkip() {
		return nil
	}
	if _, err := c.collect(ch); err != nil {
		//log.Error("failed collecting:", desc, err)
		c.updateErrCounter()
		return err
	}
	return nil
}

func (c *EnvinfoCollector) collect(ch chan<- prometheus.Metric) (*prometheus.Desc, error) {

	defer func() {
		if err := recover(); err != nil {

		}
	}()

	for _, d := range c.descs {

		sql := d.envQueryInfo.Sql

		json, err := oq.query(sql)
		if err != nil {
			log.Errorf("failed query %s: %s", d.envQueryInfo.fullName(), err)
			return nil, err
		}

		ch <- prometheus.MustNewConstMetric(
			d.desc,
			prometheus.GaugeValue,
			float64(-1),
			json,
		)
	}

	return nil, nil
}
