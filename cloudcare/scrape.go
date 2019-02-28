package cloudcare

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"time"

	"wmi_exporter/git"

	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/pkg/textparse"
	"github.com/prometheus/prometheus/pkg/timestamp"
)

type scrape struct {
	storage *Storage
}

const acceptHeader = `application/openmetrics-text; version=0.0.1,text/plain;version=0.0.4;q=0.5,*/*;q=0.1`

var userAgentHeader = fmt.Sprintf("Corsair/%s", git.Version)

// @scrapeurl: i.e., http://0.0.0.0:9100/metrics
func (s *scrape) scrape(w io.Writer, scrapeurl string) (string, error) {

	req, err := http.NewRequest("GET", scrapeurl, nil)
	if err != nil {
		return "", err
	}

	req.Header.Add("Accept", acceptHeader)
	req.Header.Add("Accept-Encoding", "gzip")
	req.Header.Set("User-Agent", userAgentHeader)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("server returned HTTP status %s", resp.Status)
	}

	var gziper *gzip.Reader
	var buf *bufio.Reader

	if resp.Header.Get("Content-Encoding") != "gzip" {
		_, err = io.Copy(w, resp.Body)
		return "", err
	}

	if gziper == nil {

		buf = bufio.NewReader(resp.Body)
		gziper, err = gzip.NewReader(buf)
		if err != nil {
			return "", err
		}

	} else {
		buf.Reset(resp.Body)
		if err := gziper.Reset(buf); err != nil {
			return "", err
		}
	}

	_, err = io.Copy(w, gziper)
	gziper.Close()

	if err != nil {
		return "", err
	}

	return resp.Header.Get("Content-Type"), nil

}

func (s *scrape) appendScrape(b []byte, contentType string, ts time.Time) {
	var (
		p       = textparse.New(b, contentType)
		defTime = timestamp.FromTime(ts)
		err     error
	)

	for {
		var et textparse.Entry
		if et, err = p.Next(); err != nil {
			if err == io.EOF {
				err = nil
			}
			break
		}

		switch et {
		case textparse.EntryType:
			continue
		case textparse.EntryHelp:
			continue
		case textparse.EntryUnit:
			continue
		case textparse.EntryComment:
			continue
		default:
		}

		t := defTime
		_, tp, v := p.Series()
		if tp != nil {
			t = *tp
		}

		var lset labels.Labels

		_ = p.Metric(&lset)

		if lset == nil {
			continue
		}

		s.storage.Add(lset, t, v)
	}
}
