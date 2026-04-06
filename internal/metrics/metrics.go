package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// PipelineComplianceState tracks the current compliance state per CI/CD resource.
	// 0=Compliant, 1=Warning, 2=NonCompliant, 3=Critical
	PipelineComplianceState = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "fluxgate_pipeline_compliance_state",
			Help: "Current compliance state per CI/CD resource (0=Compliant, 1=Warning, 2=NonCompliant, 3=Critical)",
		},
		[]string{"namespace", "name", "connector"},
	)

	// FindingsTotal tracks the total findings per CI/CD resource by rule and severity.
	FindingsTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "fluxgate_findings_total",
			Help: "Total findings per CI/CD resource by rule and severity",
		},
		[]string{"namespace", "name", "rule_id", "severity"},
	)

	// PipelinesDiscovered tracks the total CI/CD resources discovered by connector type.
	PipelinesDiscovered = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "fluxgate_pipelines_discovered_total",
			Help: "Total CI/CD resources discovered by connector type",
		},
		[]string{"connector"},
	)

	// EnforcementActions counts cumulative enforcement actions taken by type.
	EnforcementActions = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "fluxgate_enforcement_actions_total",
			Help: "Cumulative enforcement actions taken by type",
		},
		[]string{"action", "namespace", "name"},
	)

	// EvaluationDuration tracks the time taken to evaluate a pipeline source.
	EvaluationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "fluxgate_evaluation_duration_seconds",
			Help:    "Time taken to evaluate a pipeline source",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"namespace", "name"},
	)

	// GitHubAPIRequests counts GitHub API requests by endpoint and response code.
	GitHubAPIRequests = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "fluxgate_github_api_requests_total",
			Help: "GitHub API requests by endpoint and response code",
		},
		[]string{"endpoint", "status"},
	)

	// GitHubAPIRateRemaining tracks the remaining GitHub API rate limit.
	GitHubAPIRateRemaining = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "fluxgate_github_api_rate_remaining",
			Help: "Remaining GitHub API rate limit",
		},
	)
)

func init() {
	metrics.Registry.MustRegister(
		PipelineComplianceState,
		FindingsTotal,
		PipelinesDiscovered,
		EnforcementActions,
		EvaluationDuration,
		GitHubAPIRequests,
		GitHubAPIRateRemaining,
	)
}

// ComplianceStateToFloat converts a compliance state string to a numeric value for metrics.
func ComplianceStateToFloat(state string) float64 {
	switch state {
	case "Compliant":
		return 0
	case "Warning":
		return 1
	case "NonCompliant":
		return 2
	case "Critical":
		return 3
	default:
		return -1
	}
}
