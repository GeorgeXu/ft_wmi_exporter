package envinfo

import (
	"wmi_exporter/collector"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"
)

type CustomEnvItem struct {
	Name      string   `yaml:"name"`
	SubSystem string   `yaml:"sub_system"`
	Type      string   `yaml:"type"`
	Tags      []string `yaml:"tags"`
	Help      string   `yaml:"help"`
	Enable    bool     `yaml:"enabled"`
	Sql       string   `yaml:"sql"`
}

type EnvCustomCollector struct {
	desc   *prometheus.Desc
	config *CustomEnvItem
}

func NewEnvCustomCollector(cfg *CustomEnvItem) (collector.Collector, error) {

	name := cfg.Name
	subname := cfg.SubSystem

	return &EnvCustomCollector{
		desc: prometheus.NewDesc(
			prometheus.BuildFQName(name, "", subname),
			cfg.Help,
			cfg.Tags,
			nil,
		),
		config: cfg,
	}, nil
}

func (c *EnvCustomCollector) Collect(ch chan<- prometheus.Metric) error {
	if desc, err := c.collect(ch); err != nil {
		log.Error("failed collecting:", desc, err)
		return err
	}
	return nil
}

func (c *EnvCustomCollector) collect(ch chan<- prometheus.Metric) (*prometheus.Desc, error) {

	sql := c.config.Sql
	_ = sql

	json := ``
	//get from osquery
	_ = json

	ch <- prometheus.MustNewConstMetric(
		c.desc,
		prometheus.GaugeValue,
		float64(1),
		json,
	)

	return nil, nil
}
