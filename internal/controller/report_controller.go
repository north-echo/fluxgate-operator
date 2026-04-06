package controller

import (
	"context"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fluxgatev1alpha1 "github.com/north-echo/fluxgate-operator/api/v1alpha1"
	"github.com/north-echo/fluxgate-operator/internal/analyzer"
)

// ReportController creates and manages PipelineSecurityReport CRDs
// based on analysis results. It watches for report changes and ensures
// the compliance state is consistent with the findings.
type ReportController struct {
	Client client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

func (r *ReportController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Log.Info("reconciling report", "name", req.NamespacedName)

	// Fetch the PipelineSecurityReport
	report := &fluxgatev1alpha1.PipelineSecurityReport{}
	if err := r.Client.Get(ctx, req.NamespacedName, report); err != nil {
		// Report was deleted — nothing to do
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Recompute compliance state from findings to ensure consistency
	state, summary := analyzer.ComputeComplianceState(report.Status.Findings)

	needsUpdate := false
	if report.Status.ComplianceState != state {
		report.Status.ComplianceState = state
		needsUpdate = true
	}
	if report.Status.Summary != summary {
		report.Status.Summary = summary
		needsUpdate = true
	}

	if needsUpdate {
		if err := r.Client.Status().Update(ctx, report); err != nil {
			r.Log.Error(err, "failed to update report status", "name", req.NamespacedName)
			return ctrl.Result{}, err
		}
		r.Log.Info("updated report compliance state",
			"name", req.NamespacedName,
			"state", state,
			"total", summary.Total,
		)
	}

	return ctrl.Result{}, nil
}

func (r *ReportController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&fluxgatev1alpha1.PipelineSecurityReport{}).
		Named("report").
		Complete(r)
}
