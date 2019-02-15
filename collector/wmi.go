package collector

import (
	"bytes"
	"reflect"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"
)

const (
	MaxTryCount    = 3
	MaxTryInterval = 60 * 3
)

type BaseErrControl struct {
	errTime     int64
	errTryCount int
}

func (c *BaseErrControl) shouldSkip() bool {
	current := time.Now().Unix()
	if c.errTime > 0 && (current-c.errTime) > MaxTryInterval {
		c.errTryCount = 0
		c.errTime = current
		return false
	}
	return (c.errTryCount >= 3)
}

func (c *BaseErrControl) updateErrCounter() {
	if c.errTryCount < MaxTryCount {
		c.errTryCount++
	}
	if c.errTryCount == MaxTryCount {
		c.errTime = time.Now().Unix()
	}
}

// ...
const (
	Namespace = "wmi"

	// Conversion factors
	ticksToSecondsScaleFactor = 1 / 1e7
)

// Factories ...
var Factories = make(map[string]func() (Collector, error))

// Collector is the interface a collector has to implement.
type Collector interface {
	// Get new metrics and expose them via prometheus registry.
	Collect(ch chan<- prometheus.Metric) (err error)
}

func className(src interface{}) string {
	s := reflect.Indirect(reflect.ValueOf(src))
	t := s.Type()
	if s.Kind() == reflect.Slice {
		t = t.Elem()
	}
	return t.Name()
}

func queryAll(src interface{}) string {
	var b bytes.Buffer
	b.WriteString("SELECT * FROM ")
	b.WriteString(className(src))

	log.Debugf("Generated WMI query %s", b.String())
	return b.String()
}

func queryAllForClass(src interface{}, class string) string {
	var b bytes.Buffer
	b.WriteString("SELECT * FROM ")
	b.WriteString(class)

	log.Debugf("Generated WMI query %s", b.String())
	return b.String()
}

func queryAllWhere(src interface{}, where string) string {
	var b bytes.Buffer
	b.WriteString("SELECT * FROM ")
	b.WriteString(className(src))

	if where != "" {
		b.WriteString(" WHERE ")
		b.WriteString(where)
	}

	log.Debugf("Generated WMI query %s", b.String())
	return b.String()
}

func queryAllForClassWhere(src interface{}, class string, where string) string {
	var b bytes.Buffer
	b.WriteString("SELECT * FROM ")
	b.WriteString(class)

	if where != "" {
		b.WriteString(" WHERE ")
		b.WriteString(where)
	}

	log.Debugf("Generated WMI query %s", b.String())
	return b.String()
}
