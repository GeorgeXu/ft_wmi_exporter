package cloudcare

import (
	"bytes"
	"log"
	"os"
	"time"
	"wmi_exporter/rtpanic"
)

const (
	TagUploaderUID = `uploader_uid`
	TagHost        = `host`
	TagProbeName   = `probe_name`
)

var (
	HostName string
)

func init() {

	var err error
	HostName, err = os.Hostname()
	if err != nil {
		log.Printf("[error] get hostname: %s", err.Error())
	}
}

func Start(remoteHost string, scrapehost string, interval int) error {

	s, err := NewStorage(remoteHost, time.Duration(interval)*time.Millisecond)
	if err != nil {
		return err
	}

	var f rtpanic.RecoverCallback
	f = func(_ []byte, _ error) {

		time.Sleep(1 * time.Second)

		defer rtpanic.Recover(f, nil)

		sp := &scrape{
			storage: s,
		}

		ticker := time.NewTicker(time.Duration(interval) * time.Millisecond)
		defer ticker.Stop()

		for {
			var buf bytes.Buffer

			var (
				start = time.Now()
			)

			log.Printf("[info] start scrap at %s", scrapehost)

			contentType, err := sp.scrape(&buf, scrapehost)

			if err != nil {
				log.Printf("[error] scrape error:%s at %s", err, scrapehost)
			} else {
				sp.appendScrape(buf.Bytes(), contentType, start)
			}

			select {
			case <-ticker.C:
			}
		}
	}

	go f(nil, nil)

	return nil
}
