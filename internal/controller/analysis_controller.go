package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fluxgatev1alpha1 "github.com/north-echo/fluxgate-operator/api/v1alpha1"
	"github.com/north-echo/fluxgate-operator/internal/analyzer"
	"github.com/north-echo/fluxgate-operator/internal/connector"
	fluxgatemetrics "github.com/north-echo/fluxgate-operator/internal/metrics"
)

// AnalysisController evaluates CI/CD pipeline sources using Fluxgate rules
// and produces findings for report generation.
type AnalysisController struct {
	Client     client.Client
	Log        logr.Logger
	Scheme     *runtime.Scheme
	Analyzer   *analyzer.Analyzer
	Registry   *SourceRegistry
	Connectors []connector.PipelineConnector
	Recorder   record.EventRecorder
}

func (r *AnalysisController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Log.Info("reconciling analysis", "name", req.NamespacedName)

	// Analyze all sources in the registry
	sources := r.Registry.List()
	if len(sources) == 0 {
		return ctrl.Result{RequeueAfter: 2 * time.Minute}, nil
	}

	for _, src := range sources {
		if err := r.analyzeSource(ctx, src); err != nil {
			r.Log.Error(err, "analysis failed for source", "name", src.Name, "repo", src.Repository)
			continue
		}
	}

	// Requeue periodically
	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

func (r *AnalysisController) analyzeSource(ctx context.Context, src connector.PipelineSource) error {
	evalStart := time.Now()

	findings, err := r.Analyzer.Analyze(ctx, src)
	if err != nil {
		return fmt.Errorf("analyzing %s: %w", src.Name, err)
	}

	// Record evaluation duration
	ns := src.Namespace
	if ns == "" {
		ns = "default"
	}
	fluxgatemetrics.EvaluationDuration.WithLabelValues(ns, src.Name).Observe(time.Since(evalStart).Seconds())

	// Resolve correlated workloads
	var workloads []fluxgatev1alpha1.WorkloadRef
	for _, conn := range r.Connectors {
		if conn.Name() == strings.TrimSuffix(src.SourceKind, "ation") || // "Application" -> "argocd" won't match, use SourceKind
			(src.SourceKind == "Application" && conn.Name() == "argocd") ||
			(src.SourceKind == "Kustomization" && conn.Name() == "flux") ||
			(src.SourceKind == "GitRepository" && conn.Name() == "flux") {
			wl, err := conn.ResolveWorkloads(ctx, src)
			if err != nil {
				r.Log.Error(err, "failed to resolve workloads", "source", src.Name)
			} else {
				workloads = append(workloads, wl...)
			}
		}
	}

	// Compute compliance state
	state, summary := analyzer.ComputeComplianceState(findings)

	// Create or update PipelineSecurityReport
	reportName := sanitizeName(fmt.Sprintf("psr-%s-%s", src.Owner, src.Repo))
	reportNS := src.Namespace
	if reportNS == "" {
		reportNS = "default"
	}

	report := &fluxgatev1alpha1.PipelineSecurityReport{}
	err = r.Client.Get(ctx, types.NamespacedName{Name: reportName, Namespace: reportNS}, report)

	if errors.IsNotFound(err) {
		// Create new report
		report = &fluxgatev1alpha1.PipelineSecurityReport{
			ObjectMeta: metav1.ObjectMeta{
				Name:      reportName,
				Namespace: reportNS,
				Labels: map[string]string{
					"fluxgate.north-echo.dev/source-kind": src.SourceKind,
					"fluxgate.north-echo.dev/source-name": src.SourceName,
				},
			},
			Status: fluxgatev1alpha1.PipelineSecurityReportStatus{
				ComplianceState: state,
				LastEvaluated:   metav1.Now(),
				Source: fluxgatev1alpha1.PipelineSourceRef{
					Repository: src.Repository,
					Branch:     src.Branch,
				},
				Summary:             summary,
				CorrelatedWorkloads: workloads,
				Findings:            findings,
			},
		}

		if err := r.Client.Create(ctx, report); err != nil {
			return fmt.Errorf("creating report %s: %w", reportName, err)
		}

		r.Log.Info("created security report",
			"name", reportName,
			"state", state,
			"findings", summary.Total,
		)

		r.recordFindingsMetrics(report, findings, state, src)
		r.emitFindingEvents(report, state, summary)
		return nil
	} else if err != nil {
		return fmt.Errorf("getting report %s: %w", reportName, err)
	}

	// Update existing report
	report.Status.ComplianceState = state
	report.Status.LastEvaluated = metav1.Now()
	report.Status.Source = fluxgatev1alpha1.PipelineSourceRef{
		Repository: src.Repository,
		Branch:     src.Branch,
	}
	report.Status.Summary = summary
	report.Status.CorrelatedWorkloads = workloads
	report.Status.Findings = findings

	if err := r.Client.Status().Update(ctx, report); err != nil {
		return fmt.Errorf("updating report %s: %w", reportName, err)
	}

	r.Log.Info("updated security report",
		"name", reportName,
		"state", state,
		"findings", summary.Total,
	)

	r.recordFindingsMetrics(report, findings, state, src)
	r.emitFindingEvents(report, state, summary)
	return nil
}

// recordFindingsMetrics updates Prometheus metrics for findings and compliance state.
func (r *AnalysisController) recordFindingsMetrics(report *fluxgatev1alpha1.PipelineSecurityReport, findings []fluxgatev1alpha1.Finding, state string, src connector.PipelineSource) {
	connectorName := "unknown"
	switch src.SourceKind {
	case "Application":
		connectorName = "argocd"
	case "Kustomization", "GitRepository":
		connectorName = "flux"
	}

	// Update compliance state gauge
	fluxgatemetrics.PipelineComplianceState.WithLabelValues(
		report.Namespace, report.Name, connectorName,
	).Set(fluxgatemetrics.ComplianceStateToFloat(state))

	// Update findings gauge per rule/severity
	for _, f := range findings {
		fluxgatemetrics.FindingsTotal.WithLabelValues(
			report.Namespace, report.Name, f.Rule, f.Severity,
		).Set(1)
	}
}

// emitFindingEvents emits Kubernetes events for significant findings.
func (r *AnalysisController) emitFindingEvents(report *fluxgatev1alpha1.PipelineSecurityReport, state string, summary fluxgatev1alpha1.FindingSummary) {
	if r.Recorder == nil {
		return
	}

	if summary.Critical > 0 || summary.High > 0 {
		r.Recorder.Eventf(report, corev1.EventTypeWarning, "PipelineFindingDetected",
			"Detected %d critical, %d high findings for %s",
			summary.Critical, summary.High, report.Status.Source.Repository)
	}

	r.Recorder.Eventf(report, corev1.EventTypeNormal, "ComplianceStateChanged",
		"Compliance state is now %s (total findings: %d)", state, summary.Total)
}

func (r *AnalysisController) SetupWithManager(mgr ctrl.Manager) error {
	r.Recorder = mgr.GetEventRecorderFor("fluxgate-analysis-controller")
	return ctrl.NewControllerManagedBy(mgr).
		For(&fluxgatev1alpha1.PipelineSecurityReport{}).
		Named("analysis").
		Complete(r)
}

// sanitizeName converts a string to a valid Kubernetes object name.
func sanitizeName(s string) string {
	s = strings.ToLower(s)
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '.' {
			return r
		}
		return '-'
	}, s)
	// Trim leading/trailing dashes
	s = strings.Trim(s, "-.")
	if len(s) > 253 {
		s = s[:253]
	}
	return s
}
