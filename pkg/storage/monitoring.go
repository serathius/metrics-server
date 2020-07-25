package storage

import (
	"k8s.io/component-base/metrics"
	"k8s.io/component-base/metrics/legacyregistry"
)

var (
	pointsStored = metrics.NewGaugeVec(
		&metrics.GaugeOpts{
			Namespace: "metrics_server",
			Subsystem: "storage",
			Name:      "points_stored",
			Help:      "Number of metrics points stored.",
		},
		[]string{"type"},
	)
)

func init() {
	legacyregistry.Register(pointsStored)
}