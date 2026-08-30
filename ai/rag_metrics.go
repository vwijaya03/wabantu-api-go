package ai

import "encore.dev/metrics"

type retrievalMetricLabels struct {
	Source string `metric:"source" encore:"source"`
	Mode   string `metric:"mode" encore:"mode"`
}

var (
	metricRetrievalRequests = metrics.NewCounterGroup[retrievalMetricLabels, uint64](
		"retrieval_requests_total",
		metrics.CounterConfig{},
	)
	metricRetrievalFallbacks = metrics.NewCounterGroup[retrievalMetricLabels, uint64](
		"retrieval_fallback_total",
		metrics.CounterConfig{},
	)
	metricRetrievalZeroHits = metrics.NewCounterGroup[retrievalMetricLabels, uint64](
		"retrieval_zero_result_total",
		metrics.CounterConfig{},
	)
	metricEmbedCacheHits = metrics.NewCounter[uint64](
		"retrieval_embed_cache_hits_total",
		metrics.CounterConfig{},
	)
	metricEmbedCacheMisses = metrics.NewCounter[uint64](
		"retrieval_embed_cache_misses_total",
		metrics.CounterConfig{},
	)
	metricRetrievalLatencyP95Ms = metrics.NewGauge[uint64](
		"retrieval_latency_p95_ms",
		metrics.GaugeConfig{},
	)
)

func recordRetrievalQueryMetrics(source, mode string, failed, zeroResult bool, latencyP95Ms uint64) {
	metricRetrievalRequests.With(retrievalMetricLabels{Source: source, Mode: mode}).Add(1)
	if failed {
		metricRetrievalFallbacks.With(retrievalMetricLabels{Source: source, Mode: mode}).Add(1)
	}
	if zeroResult {
		metricRetrievalZeroHits.With(retrievalMetricLabels{Source: source, Mode: mode}).Add(1)
	}
	if latencyP95Ms > 0 {
		metricRetrievalLatencyP95Ms.Set(latencyP95Ms)
	}
}

func recordEmbedCacheMetrics(hits, misses uint64) {
	if hits > 0 {
		metricEmbedCacheHits.Add(hits)
	}
	if misses > 0 {
		metricEmbedCacheMisses.Add(misses)
	}
}
