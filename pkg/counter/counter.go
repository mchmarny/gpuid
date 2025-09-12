package counter

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// IncrementalCounter defines an interface for counters that can be incremented with optional label values.
type IncrementalCounter interface {
	Increment(val ...string)
}

// Counter wraps a Prometheus CounterVec to provide a simple interface for incrementing counters with labels.
type Counter struct {
	Name string
	Help string

	vec *prometheus.CounterVec
}

// Increment increases the counter by 1 for the given label values.
func (c *Counter) Increment(val ...string) {
	c.vec.WithLabelValues(val...).Inc()
}

// New creates and registers a new Prometheus counter with the given name, help text, and optional labels.
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
