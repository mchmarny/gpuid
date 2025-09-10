package counter

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type IncrementalCounter interface {
	Increment(val ...string)
}

type Counter struct {
	Name string
	Help string

	vec *prometheus.CounterVec
}

func (c *Counter) Increment(val ...string) {
	c.vec.WithLabelValues(val...).Inc()
}

func New(name, help string, labels ...string) IncrementalCounter {
	counter := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: name,
		Help: help,
	}, labels)

	prometheus.MustRegister(counter)

	return &Counter{
		Name: name,
		Help: help,
		vec:  counter,
	}
}

// Handler returns an HTTP handler for serving Prometheus metrics.
func Handler() http.Handler {
	return promhttp.Handler()
}
