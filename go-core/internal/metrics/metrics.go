// Package metrics registers all application Prometheus metrics in one place so
// there is a single canonical source of metric names. Using the Prometheus
// client directly (rather than a wrapper) keeps the dependency graph shallow
// and lets ops query metrics with standard PromQL without any translation layer.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// registry is process-scoped so that tests can create a fresh one without
// polluting the default registry, while production uses the singleton.
var (
	reg = prometheus.NewRegistry()

	// Counters
	brainCacheHits = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "thaiagent_brain_cache_hits_total",
		Help: "Number of successful brain cache hits.",
	}, []string{})

	brainCacheMisses = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "thaiagent_brain_cache_misses_total",
		Help: "Number of brain cache misses that triggered a retrieval.",
	}, []string{})

	smlCacheHits = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "thaiagent_sml_cache_hits_total",
		Help: "Number of SML gateway response cache hits.",
	}, []string{})

	lineRateLimitBlocked = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "thaiagent_line_ratelimit_blocked_total",
		Help: "Number of LINE webhook requests blocked by the per-user rate limiter.",
	}, []string{})

	extractionDedupSkips = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "thaiagent_extraction_dedup_skips_total",
		Help: "Number of extraction jobs skipped because an identical job was already in-flight.",
	}, []string{})

	outboxPublished = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "thaiagent_outbox_published_total",
		Help: "Number of outbox events successfully published to NATS.",
	}, []string{})

	outboxFailed = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "thaiagent_outbox_failed_total",
		Help: "Number of outbox events that exhausted retries and were marked failed.",
	}, []string{})

	// Histograms
	brainRetrieveSeconds = promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
		Name:    "thaiagent_brain_retrieve_seconds",
		Help:    "End-to-end latency for a brain retrieval call (embedding + vector search).",
		Buckets: prometheus.DefBuckets,
	})

	outboxDrainSize = promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
		Name:    "thaiagent_outbox_drain_size",
		Help:    "Number of outbox rows processed per drain iteration.",
		Buckets: []float64{1, 5, 10, 25, 50, 100, 250, 500},
	})
)

// counterByName maps the public metric name to its CounterVec so callers can
// use the Inc helper without importing individual variables.
var counterByName = map[string]*prometheus.CounterVec{
	"thaiagent_brain_cache_hits_total":        brainCacheHits,
	"thaiagent_brain_cache_misses_total":       brainCacheMisses,
	"thaiagent_sml_cache_hits_total":           smlCacheHits,
	"thaiagent_line_ratelimit_blocked_total":   lineRateLimitBlocked,
	"thaiagent_extraction_dedup_skips_total":   extractionDedupSkips,
	"thaiagent_outbox_published_total":         outboxPublished,
	"thaiagent_outbox_failed_total":            outboxFailed,
}

var histogramByName = map[string]prometheus.Histogram{
	"thaiagent_brain_retrieve_seconds": brainRetrieveSeconds,
	"thaiagent_outbox_drain_size":      outboxDrainSize,
}

// Inc increments a named counter by 1. Unregistered names are silently ignored
// rather than panicking so that a misspelled metric name in a hotpath does not
// take down the service.
func Inc(name string) {
	if c, ok := counterByName[name]; ok {
		c.WithLabelValues().Inc()
	}
}

// Observe records a float64 sample against a named histogram.
func Observe(name string, value float64) {
	if h, ok := histogramByName[name]; ok {
		h.Observe(value)
	}
}

// Handler returns an http.Handler that renders all registered metrics in
// Prometheus text exposition format. Mount at GET /metrics.
func Handler() http.Handler {
	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}
