package controller

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/north-echo/fluxgate-operator/internal/connector"
	fluxgatemetrics "github.com/north-echo/fluxgate-operator/internal/metrics"
)

// SourceRegistry is an in-memory store of discovered pipeline sources, keyed by name.
type SourceRegistry struct {
	mu      sync.RWMutex
	sources map[string]connector.PipelineSource
}

// NewSourceRegistry creates a new empty source registry.
func NewSourceRegistry() *SourceRegistry {
	return &SourceRegistry{
		sources: make(map[string]connector.PipelineSource),
	}
}

// Put stores or updates a pipeline source.
func (r *SourceRegistry) Put(src connector.PipelineSource) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sources[src.Name] = src
}

// Get returns a pipeline source by name.
func (r *SourceRegistry) Get(name string) (connector.PipelineSource, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	src, ok := r.sources[name]
	return src, ok
}

// List returns all stored pipeline sources.
func (r *SourceRegistry) List() []connector.PipelineSource {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]connector.PipelineSource, 0, len(r.sources))
	for _, src := range r.sources {
		result = append(result, src)
	}
	return result
}

// DiscoveryController watches ArgoCD Applications and Flux GitRepositories
// to discover CI/CD pipeline sources that should be scanned.
type DiscoveryController struct {
	Client     client.Client
	Log        logr.Logger
	Scheme     *runtime.Scheme
	Connectors []connector.PipelineConnector
	Registry   *SourceRegistry
}

func (r *DiscoveryController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Log.Info("reconciling discovery", "name", req.NamespacedName)

	for _, conn := range r.Connectors {
		sources, err := conn.Discover(ctx, req.Namespace)
		if err != nil {
			r.Log.Error(err, "connector discovery failed", "connector", conn.Name(), "namespace", req.Namespace)
			continue
		}

		// Update pipelines discovered metric
		fluxgatemetrics.PipelinesDiscovered.WithLabelValues(conn.Name()).Set(float64(len(sources)))

		for _, src := range sources {
			existing, found := r.Registry.Get(src.Name)
			if !found || sourceChanged(existing, src) {
				r.Registry.Put(src)
				r.Log.Info("discovered pipeline source",
					"connector", conn.Name(),
					"name", src.Name,
					"repo", src.Repository,
					"branch", src.Branch,
				)
			}
		}
	}

	// Requeue periodically to discover new sources
	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

func (r *DiscoveryController) SetupWithManager(mgr ctrl.Manager) error {
	// Watch Namespaces as a trigger — when a namespace event fires, we
	// discover sources across all connectors in that namespace.
	// In production this could also watch ArgoCD Application and Flux
	// GitRepository CRDs once their schemes are registered.
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Namespace{}).
		Named("discovery").
		Complete(r)
}

// sourceChanged returns true if a discovered source differs from the stored version.
func sourceChanged(old, new connector.PipelineSource) bool {
	if old.Repository != new.Repository {
		return true
	}
	if old.Branch != new.Branch {
		return true
	}
	if fmt.Sprintf("%v", old.Paths) != fmt.Sprintf("%v", new.Paths) {
		return true
	}
	return false
}
