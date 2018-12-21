package envinfo

import (
	"wmi_exporter/collector"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"
)

func init() {
	collector.Factories["win_env_userinfo"] = NewEnvUserInfoCollector
}

func Init() {

}

type EnvUserInfoCollector struct {
	UserList *prometheus.Desc
}

func NewEnvUserInfoCollector() (collector.Collector, error) {

	const subSystem = "user"

	return &EnvUserInfoCollector{
		UserList: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subSystem, "list"),
			"list windows user info",
			[]string{"json"},
			nil,
		),
	}, nil
}

func (c *EnvUserInfoCollector) Collect(ch chan<- prometheus.Metric) error {
	if desc, err := c.collect(ch); err != nil {
		log.Error("failed collecting user envs:", desc, err)
		return err
	}
	return nil
}

func (c *EnvUserInfoCollector) collect(ch chan<- prometheus.Metric) (*prometheus.Desc, error) {

	ch <- prometheus.MustNewConstMetric(
		c.UserList,
		prometheus.GaugeValue,
		float64(1),
		`"description":"","directory":"","gid":"513","gid_signed":"513","shell":"C:\\Windows\\System32\\cmd.exe","type":"local","uid":"500","uid_signed":"500","username":"Administrator","uuid":""`,
	)

	return nil, nil
}
