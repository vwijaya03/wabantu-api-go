package kb

import "encore.dev/metrics"

type indexingMetricLabels struct {
	Entity string `metric:"entity" encore:"entity"`
	Lane   string `metric:"lane" encore:"lane"`
}

var (
	metricIndexingSuccess = metrics.NewCounterGroup[indexingMetricLabels, uint64](
		"retrieval_indexing_success_total",
		metrics.CounterConfig{},
	)
	metricIndexingFailure = metrics.NewCounterGroup[indexingMetricLabels, uint64](
		"retrieval_indexing_failure_total",
		metrics.CounterConfig{},
	)
	metricIndexingLagSeconds = metrics.NewGauge[uint64](
		"retrieval_indexing_lag_seconds",
		metrics.GaugeConfig{},
	)
)

func recordIndexingMetrics(entity, lane string, success bool, lagSec uint64) {
	labels := indexingMetricLabels{Entity: entity, Lane: lane}
	if success {
		metricIndexingSuccess.With(labels).Add(1)
	} else {
		metricIndexingFailure.With(labels).Add(1)
	}
	if lagSec > 0 {
		metricIndexingLagSeconds.Set(lagSec)
	}
}
