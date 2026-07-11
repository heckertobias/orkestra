// Package metrics registers and exposes Prometheus metrics for the Agent.
package metrics

import (
	"bytes"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/expfmt"
)

// Gather collects the current metrics from the default registry and returns them in Prometheus
// text exposition format — the same content promhttp.Handler() serves on the local metrics port.
// Used to federate agent metrics to the Master over the mTLS stream (see MetricsResponse).
func Gather() (string, error) {
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	enc := expfmt.NewEncoder(&buf, expfmt.NewFormat(expfmt.TypeTextPlain))
	for _, mf := range mfs {
		if err := enc.Encode(mf); err != nil {
			return "", err
		}
	}
	return buf.String(), nil
}

var (
	ContainersRunning = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "orkestra_agent_containers_running",
		Help: "Currently running managed containers.",
	})

	ContainersDrift = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "orkestra_agent_containers_drift",
		Help: "Containers in drift state.",
	})

	ReconcileDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "orkestra_agent_reconcile_duration_seconds",
		Help:    "Per-stack reconcile duration.",
		Buckets: prometheus.DefBuckets,
	})

	ReconcileErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orkestra_agent_reconcile_errors_total",
		Help: "Reconcile errors by stack_id.",
	}, []string{"stack_id"})

	DockerAPIDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "orkestra_agent_docker_api_duration_seconds",
		Help:    "Docker SDK call latency by operation.",
		Buckets: prometheus.DefBuckets,
	}, []string{"operation"})

	StreamReconnectsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "orkestra_agent_stream_reconnects_total",
		Help: "Master stream reconnects.",
	})
)
