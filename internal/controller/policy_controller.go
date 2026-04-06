package controller

import (
	"context"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fluxgatev1alpha1 "github.com/north-echo/fluxgate-operator/api/v1alpha1"
)

// PolicyController watches PipelineSecurityPolicy resources and evaluates
// PipelineSecurityReports against policy thresholds to trigger enforcement actions.
type PolicyController struct {
	Client client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

func (r *PolicyController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Log.Info("reconciling", "name", req.NamespacedName)
	// TODO: evaluate reports against policy thresholds, trigger enforcement
	return ctrl.Result{}, nil
}

func (r *PolicyController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&fluxgatev1alpha1.PipelineSecurityPolicy{}).
		Named("policy").
		Complete(r)
}
