package controller

import (
	"context"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DiscoveryController watches ArgoCD Applications and Flux GitRepositories
// to discover CI/CD pipeline sources that should be scanned.
type DiscoveryController struct {
	Client client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

func (r *DiscoveryController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Log.Info("reconciling", "name", req.NamespacedName)
	// TODO: discover pipeline sources from ArgoCD/Flux objects
	return ctrl.Result{}, nil
}

func (r *DiscoveryController) SetupWithManager(mgr ctrl.Manager) error {
	// Watch ConfigMaps as a placeholder; in production this would watch
	// ArgoCD Application and Flux GitRepository CRDs once their schemes
	// are registered.
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.ConfigMap{}).
		Named("discovery").
		Complete(r)
}
