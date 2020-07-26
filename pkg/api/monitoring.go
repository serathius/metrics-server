package api

import (
	"k8s.io/component-base/metrics"
	"k8s.io/component-base/metrics/legacyregistry"
)

var (
	metricFreshness = metrics.NewHistogram(
		&metrics.HistogramOpts{
			Namespace:         "metrics_server",
			Subsystem:         "api",
			Name:              "metric_latency",
			Help:              "Latency of metrics exported",
			Buckets:           metrics.ExponentialBuckets(1, 1.364, 20),
		},
	)
)

func init() {
	legacyregistry.Register(metricFreshness)
}
