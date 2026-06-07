// Package metrics registers and exposes Prometheus metrics for the Master.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	AgentsConnected = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "orkestra_agents_connected_total",
		Help: "Currently connected Agents.",
	})

	AgentsOffline = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "orkestra_agents_offline_total",
		Help: "Agents marked offline.",
	})

	DeployDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "orkestra_deploy_duration_seconds",
		Help:    "Time from deploy trigger to reconcile push.",
		Buckets: prometheus.DefBuckets,
	})

	DeployTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orkestra_deploy_total",
		Help: "Deployments by status.",
	}, []string{"status"})

	ReconcilePushTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "orkestra_reconcile_push_total",
		Help: "ApplyDesiredState messages sent.",
	})

	APIRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orkestra_api_requests_total",
		Help: "UI API requests by method and status.",
	}, []string{"method", "status"})

	APIDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "orkestra_api_duration_seconds",
		Help:    "UI API latency by method.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method"})

	SecretResolvesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orkestra_secret_resolves_total",
		Help: "Secret provider calls by provider and status.",
	}, []string{"provider", "status"})
)
