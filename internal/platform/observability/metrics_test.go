package observability

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func TestBusinessMetricsUseBoundedLabelsAndRecordValues(t *testing.T) {
	metrics := NewMetrics()
	metrics.SetQueueDepth("index", 3)
	metrics.RecordIndex("success")
	metrics.RecordIndex("retry")
	metrics.RecordRetrieval("keyword", "hit", false, 20*time.Millisecond)
	metrics.RecordRetrieval("vector", "empty", true, 40*time.Millisecond)
	metrics.SetHealth("retrieval", "degraded")

	families, err := metrics.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	var names []string
	for _, family := range families {
		names = append(names, family.GetName())
	}
	joined := strings.Join(names, "\n")
	for _, want := range []string{
		"eino_harness_queue_depth",
		"eino_harness_index_tasks_total",
		"eino_harness_retrievals_total",
		"eino_harness_retrieval_duration_seconds",
		"eino_harness_component_health",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("metric %q missing from gather:\n%s", want, joined)
		}
	}
	for _, family := range families {
		for _, metric := range family.GetMetric() {
			for _, label := range metric.GetLabel() {
				if strings.Contains(label.GetValue(), "document") || strings.Contains(label.GetValue(), "user") {
					t.Fatalf("business metric leaked an unbounded label value: %s=%s", label.GetName(), label.GetValue())
				}
			}
		}
	}
}

func TestBusinessMetricsCanBeRegisteredForPrometheusAgent(t *testing.T) {
	metrics := NewMetrics()
	registry := prometheus.NewRegistry()
	if err := metrics.Register(registry); err != nil {
		t.Fatalf("register business metrics: %v", err)
	}

	metrics.SetQueueDepth("index", 2)
	metrics.RecordIndex("success")
	metrics.SetHealth("retrieval", "ready")
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather registered metrics: %v", err)
	}
	var names []string
	for _, family := range families {
		names = append(names, family.GetName())
	}
	joined := strings.Join(names, "\n")
	for _, want := range []string{
		"eino_harness_queue_depth",
		"eino_harness_index_tasks_total",
		"eino_harness_component_health",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("registered metric %q missing from gather:\n%s", want, joined)
		}
	}
}

func TestHealthMetricKeepsOneActiveStatePerComponent(t *testing.T) {
	metrics := NewMetrics()
	metrics.SetHealth("retrieval", "ready")
	metrics.SetHealth("retrieval", "degraded")

	families, err := metrics.Gather()
	if err != nil {
		t.Fatalf("gather health metrics: %v", err)
	}
	for _, family := range families {
		if family.GetName() != "eino_harness_component_health" {
			continue
		}
		for _, metric := range family.GetMetric() {
			labels := map[string]string{}
			for _, label := range metric.GetLabel() {
				labels[label.GetName()] = label.GetValue()
			}
			if labels["component"] != "retrieval" {
				continue
			}
			want := float64(0)
			if labels["state"] == "degraded" {
				want = 1
			}
			if got := metric.GetGauge().GetValue(); got != want {
				t.Fatalf("health state %s = %v, want %v", labels["state"], got, want)
			}
		}
		return
	}
	t.Fatal("component health metric family missing")
}
