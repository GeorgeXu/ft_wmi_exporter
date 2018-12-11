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
	remoteWriteUrl *url.URL
	remoteStorage  *Storage
	PromCfg        config.Config
	chStop         chan struct{}

	CorsairCloudAssetID   string
	CorsairTeamID         string
	CorsairSK             string
	CorsairAK             string
	CorsairHost           string
	CorsairPort           int
	CorsairScrapeInterval int
)

func loop(s *Storage, scrapeurl string) {

	sp := &scrape{
		storage: s,
	}

	ticker := time.NewTicker(time.Duration(PromCfg.GlobalConfig.ScrapeInterval))
	defer ticker.Stop()

	for {
		var buf bytes.Buffer

		select {
		case <-chStop:
			return
		default:
		}

		var (
			start = time.Now()
		)

		contentType, err := sp.scrape(&buf, scrapeurl)

		if err == nil {
			sp.appendScrape(buf.Bytes(), contentType, start)
		} else {
			fmt.Println("scrape error:", err)
			return
		}

		select {
		case <-ticker.C:
		case <-chStop:
			return
		}
	}
}

func Start(remotehost string, scrapehost string) error {

	u, err := url.Parse(remotehost)
	if err != nil {
		return err
	}

	remoteWriteUrl = u

	var l promlog.AllowedLevel
	l.Set("info")
	logger := promlog.New(l)

	chStop = make(chan struct{})

	RemoteFlushDeadline := time.Duration(60 * time.Second)

	remoteStorage = NewStorage(log.With(logger, "component", "remote"), nil, time.Duration(RemoteFlushDeadline))

	if err := remoteStorage.ApplyConfig(&PromCfg); err != nil {
		return err
	}

	go func() {
		time.Sleep(1 * time.Second)
		loop(remoteStorage, scrapehost)
	}()

	return nil
}

func GetDataBridgeUrl() *url.URL {
	return remoteWriteUrl
}
