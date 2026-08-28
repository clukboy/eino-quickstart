package observability

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	requestsTotal *prometheus.CounterVec
	requestTime   *prometheus.HistogramVec
	inFlight      prometheus.Gauge
	registry      *prometheus.Registry
}

func NewMetrics() *Metrics {
	registry := prometheus.NewRegistry()
	metrics := &Metrics{
		requestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "eino_harness",
			Name:      "http_requests_total",
			Help:      "Total HTTP requests.",
		}, []string{"method", "route", "status"}),
		requestTime: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "eino_harness",
				Name:      "http_request_duration_seconds",
				Help:      "HTTP request duration.",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{"method", "route"},
		),
		inFlight: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "eino_harness",
				Name:      "http_in_flight_requests",
				Help:      "In-flight HTTP requests.",
			},
		),
		registry: registry,
	}
	registry.MustRegister(
		metrics.requestsTotal,
		metrics.requestTime,
		metrics.inFlight,
	)

	return metrics
}
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}
func (m *Metrics) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.inFlight.Inc()
		defer m.inFlight.Dec()

		started := time.Now()

		recorder := &statusRecorder{ResponseWriter: w}

		next.ServeHTTP(recorder, r)
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}

		route := r.Pattern
		if route == "" {
			route = "unknown"
		}

		m.requestsTotal.WithLabelValues(
			r.Method,
			route,
			strconv.Itoa(status),
		).Inc()

		m.requestTime.WithLabelValues(
			r.Method,
			route).Observe(time.Since(started).Seconds())
	})
}
