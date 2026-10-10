package observability

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"
)

// Metrics contains the low-cardinality HTTP and business metrics exported by a
// process. Labels intentionally describe bounded operational dimensions only;
// request bodies, document IDs and user IDs must never become label values.
type Metrics struct {
	requestsTotal *prometheus.CounterVec
	requestTime   *prometheus.HistogramVec
	inFlight      prometheus.Gauge
	queueDepth    *prometheus.GaugeVec
	indexTasks    *prometheus.CounterVec
	retrievals    *prometheus.CounterVec
	retrievalTime *prometheus.HistogramVec
	health        *prometheus.GaugeVec
	registry      *prometheus.Registry
	healthMu      sync.Mutex
	healthState   map[string]string
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
		queueDepth: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "eino_harness",
			Name:      "queue_depth",
			Help:      "Current number of tasks in an indexed queue.",
		}, []string{"queue"}),
		indexTasks: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "eino_harness",
			Name:      "index_tasks_total",
			Help:      "Index task outcomes.",
		}, []string{"status"}),
		retrievals: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "eino_harness",
			Name:      "retrievals_total",
			Help:      "Retrieval outcomes by bounded channel and result state.",
		}, []string{"channel", "outcome", "degraded"}),
		retrievalTime: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "eino_harness",
			Name:      "retrieval_duration_seconds",
			Help:      "Knowledge retrieval duration.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"channel"}),
		health: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "eino_harness",
			Name:      "component_health",
			Help:      "Current component health state; one state is 1 per component.",
		}, []string{"component", "state"}),
		registry:    registry,
		healthState: make(map[string]string),
	}
	if err := metrics.Register(registry); err != nil {
		panic(err)
	}

	return metrics
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// Register adds the business collectors to an arbitrary Prometheus registry.
// The metrics object keeps a private registry for deterministic tests, while
// production entrypoints also register the same collectors in the process
// default registry served by go-zero's Prometheus agent.
func (m *Metrics) Register(reg prometheus.Registerer) error {
	if m == nil || reg == nil {
		return nil
	}
	for _, collector := range m.collectors() {
		if err := reg.Register(collector); err != nil {
			return err
		}
	}
	return nil
}

// RegisterDefault exposes business metrics on the standard Prometheus registry
// used by go-zero's prometheus.StartAgent. Call it once per process.
func (m *Metrics) RegisterDefault() error {
	return m.Register(prometheus.DefaultRegisterer)
}

func (m *Metrics) collectors() []prometheus.Collector {
	return []prometheus.Collector{
		m.requestsTotal,
		m.requestTime,
		m.inFlight,
		m.queueDepth,
		m.indexTasks,
		m.retrievals,
		m.retrievalTime,
		m.health,
	}
}

// Gather is intended for tests and local acceptance checks.
func (m *Metrics) Gather() ([]*dto.MetricFamily, error) { return m.registry.Gather() }

// SetQueueDepth updates a queue snapshot. Queue names are infrastructure names,
// not user-controlled values.
func (m *Metrics) SetQueueDepth(queue string, depth int) {
	if m == nil || queue == "" || depth < 0 {
		return
	}
	m.queueDepth.WithLabelValues(queue).Set(float64(depth))
}

// RecordIndex records one bounded index outcome: success, failure or retry.
func (m *Metrics) RecordIndex(status string) {
	if m == nil {
		return
	}
	switch status {
	case "success", "failure", "retry":
		m.indexTasks.WithLabelValues(status).Inc()
	}
}

// RecordRetrieval records a bounded channel/result observation and duration.
func (m *Metrics) RecordRetrieval(channel, outcome string, degraded bool, duration time.Duration) {
	if m == nil {
		return
	}
	if channel == "" {
		channel = "none"
	}
	if outcome != "hit" && outcome != "empty" && outcome != "error" {
		outcome = "error"
	}
	degradedLabel := "false"
	if degraded {
		degradedLabel = "true"
	}
	m.retrievals.WithLabelValues(channel, outcome, degradedLabel).Inc()
	m.retrievalTime.WithLabelValues(channel).Observe(duration.Seconds())
}

// SetHealth marks one state as active for a bounded component. Callers should
// use values such as ready, degraded or unavailable; arbitrary request data is
// intentionally not accepted by the application call sites.
func (m *Metrics) SetHealth(component, state string) {
	if m == nil || component == "" || state == "" {
		return
	}
	m.healthMu.Lock()
	defer m.healthMu.Unlock()
	if previous := m.healthState[component]; previous != "" && previous != state {
		m.health.WithLabelValues(component, previous).Set(0)
	}
	m.health.WithLabelValues(component, state).Set(1)
	m.healthState[component] = state
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
