package cloudcare

import (
	"bytes"
	"fmt"
	"net/url"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/prometheus/common/promlog"
	"github.com/prometheus/prometheus/config"
)

var (
	remoteMetricWrite *Storage
	remoteEnvWrite    *Storage

	PromCfg config.Config
	chStop  chan struct{}

	CorsairUploaderUID       string
	CorsairTeamID            string
	CorsairSK                string
	CorsairAK                string
	CorsairHost              string
	CorsairPort              int
	CorsairScrapeInterval    int
	CorsairScrapeEnvInterval int

	CorsairRemoteHost       string
	CorsairMetricsWritePath = "v1/write"
	CorsairEnvWritePath     = "v1/write/env"

	CorsairInstallPath string

	logger log.Logger
)

func loop() {

	sp := []*scrape{
		&scrape{
			storage:   remoteMetricWrite,
			scrapeUrl: fmt.Sprintf("http://0.0.0.0:%d/metrics", CorsairPort),
		},
		&scrape{
			storage:   remoteEnvWrite,
			scrapeUrl: fmt.Sprintf("http://0.0.0.0:%d/env_infos", CorsairPort),
		},
	}

	ticker := time.NewTicker(time.Duration(CorsairScrapeInterval) * time.Second)

	defer ticker.Stop()

	var contentType string
	var err error

	for {

		select {
		case <-chStop:
			return
		default:
		}

		for _, s := range sp {

			var buf bytes.Buffer

			start := time.Now()
			contentType, err = s.scrape(&buf, false)
			if err != nil {
				fmt.Println("scrape error:", err)
				return
			}
			s.appendScrape(buf.Bytes(), contentType, start)
		}

		select {
		case <-ticker.C:
		case <-chStop:
			return
		}
	}
}

func Start() error {

	var al promlog.AllowedLevel
	al.Set("info")
	var af promlog.AllowedFormat
	af.Set("logfmt")
	logger = promlog.New(
		&promlog.Config{
			Level:  &al,
			Format: &af,
		})

	chStop = make(chan struct{})

	//addConstantLabels()

	if err := setRemoteStorage(CorsairMetricsWritePath); err != nil {
		return err
	}

	if err := setRemoteStorage(CorsairEnvWritePath); err != nil {
		return err
	}

	go func() {
		time.Sleep(2 * time.Second)
		loop()
	}()

	return nil
}

func setRemoteStorage(path string) error {

	ustr := fmt.Sprintf("%s%s", CorsairRemoteHost, path)
	u, err := url.Parse(ustr)
	if err != nil {
		return err
	}

	if path == CorsairMetricsWritePath {
		remoteMetricWrite = NewStorage(log.With(logger, "component", "metrics"), nil, time.Duration(time.Duration(60*time.Second)))
		if err := remoteMetricWrite.ApplyConfig(&PromCfg, u); err != nil {
			return err
		}
	} else {
		remoteEnvWrite = NewStorage(log.With(logger, "component", "env"), nil, time.Duration(time.Duration(60*time.Second)))
		if err := remoteEnvWrite.ApplyConfig(&PromCfg, u); err != nil {
			return err
		}
	}

	return nil
}

// func addConstantLabels() {
// 	exlabels := make(map[model.LabelName]model.LabelValue)
// 	exlabels["cloud_asset_id"] = model.LabelValue(CorsairCloudAssetID)
// 	if CorsairHost != "" {
// 		exlabels["host"] = model.LabelValue(CorsairHost)
// 	} else {
// 		exlabels["host"] = "default"
// 	}
// 	PromCfg.GlobalConfig.ExternalLabels = exlabels
// }

func GetDataBridgeUrl() ([]*url.URL, error) {
	var result []*url.URL

	ustr := fmt.Sprintf("%s%s", CorsairRemoteHost, CorsairMetricsWritePath)
	u, err := url.Parse(ustr)
	if err != nil {
		return nil, err
	}

	result = append(result, u)

	ustr = fmt.Sprintf("%s%s", CorsairRemoteHost, CorsairEnvWritePath)
	u, err = url.Parse(ustr)
	if err != nil {
		return nil, err
	}

	result = append(result, u)

	return result, nil
}
