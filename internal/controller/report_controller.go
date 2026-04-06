package controller

import (
	"context"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fluxgatev1alpha1 "github.com/north-echo/fluxgate-operator/api/v1alpha1"
)

// ReportController creates and manages PipelineSecurityReport CRDs
// based on analysis results.
type ReportController struct {
	Client client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

func (r *ReportController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Log.Info("reconciling", "name", req.NamespacedName)
	// TODO: create/update PipelineSecurityReport from analysis results
	return ctrl.Result{}, nil
}

func (r *ReportController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&fluxgatev1alpha1.PipelineSecurityReport{}).
		Named("report").
		Complete(r)
}
