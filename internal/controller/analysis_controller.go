package controller

import (
	"context"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fluxgatev1alpha1 "github.com/north-echo/fluxgate-operator/api/v1alpha1"
)

// AnalysisController evaluates CI/CD pipeline sources using Fluxgate rules
// and produces findings for report generation.
type AnalysisController struct {
	Client client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

func (r *AnalysisController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Log.Info("reconciling", "name", req.NamespacedName)
	// TODO: fetch pipeline source, run Fluxgate scan, update report status
	return ctrl.Result{}, nil
}

func (r *AnalysisController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&fluxgatev1alpha1.PipelineSecurityReport{}).
		Named("analysis").
		Complete(r)
}
