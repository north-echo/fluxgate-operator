package connector

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/north-echo/fluxgate-operator/api/v1alpha1"
)

// Minimal ArgoCD Application types for discovery (avoids importing the full ArgoCD package).

// ArgoCDApplication is a minimal representation of an ArgoCD Application CRD.
type ArgoCDApplication struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ArgoCDApplicationSpec `json:"spec"`
}

// ArgoCDApplicationSpec is the spec of an ArgoCD Application.
type ArgoCDApplicationSpec struct {
	Source      ArgoCDSource      `json:"source"`
	Destination ArgoCDDestination `json:"destination"`
	SyncPolicy  *ArgoCDSyncPolicy `json:"syncPolicy,omitempty"`
}

// ArgoCDSource defines the Git source for an ArgoCD Application.
type ArgoCDSource struct {
	RepoURL        string `json:"repoURL"`
	Path           string `json:"path"`
	TargetRevision string `json:"targetRevision"`
}

// ArgoCDDestination defines the deployment target for an ArgoCD Application.
type ArgoCDDestination struct {
	Server    string `json:"server"`
	Namespace string `json:"namespace"`
}

// ArgoCDSyncPolicy defines the sync policy for an ArgoCD Application.
type ArgoCDSyncPolicy struct {
	Automated *ArgoCDAutomatedSync `json:"automated,omitempty"`
}

// ArgoCDAutomatedSync enables automatic sync.
type ArgoCDAutomatedSync struct {
	Prune    bool `json:"prune,omitempty"`
	SelfHeal bool `json:"selfHeal,omitempty"`
}

var argoCDApplicationGVR = schema.GroupVersionResource{
	Group:    "argoproj.io",
	Version:  "v1alpha1",
	Resource: "applications",
}

// ArgoCDConnector discovers pipeline sources from ArgoCD Application resources.
type ArgoCDConnector struct {
	Client client.Client
	Log    logr.Logger
}

var _ PipelineConnector = &ArgoCDConnector{}

func (c *ArgoCDConnector) Name() string {
	return "argocd"
}

// Discover lists ArgoCD Application objects in the namespace and returns pipeline sources.
func (c *ArgoCDConnector) Discover(ctx context.Context, namespace string) ([]PipelineSource, error) {
	c.Log.Info("discovering ArgoCD applications", "namespace", namespace)

	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "argoproj.io",
		Version: "v1alpha1",
		Kind:    "ApplicationList",
	})

	if err := c.Client.List(ctx, list, client.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("listing ArgoCD Applications: %w", err)
	}

	var sources []PipelineSource
	for _, item := range list.Items {
		app, err := unmarshalArgoCDApp(&item)
		if err != nil {
			c.Log.Error(err, "failed to unmarshal ArgoCD Application", "name", item.GetName())
			continue
		}

		owner, repo, ok := ParseRepoURL(app.Spec.Source.RepoURL)
		if !ok {
			c.Log.Info("skipping application with unparseable repo URL",
				"name", app.Name, "repoURL", app.Spec.Source.RepoURL)
			continue
		}

		branch := app.Spec.Source.TargetRevision
		if branch == "" || branch == "HEAD" {
			branch = "main"
		}

		src := PipelineSource{
			Name:            app.Name,
			Repository:      app.Spec.Source.RepoURL,
			Branch:          branch,
			Paths:           []string{app.Spec.Source.Path},
			Platform:        "github-actions",
			Labels:          app.Labels,
			Namespace:       app.Namespace,
			SourceName:      app.Name,
			SourceKind:      "Application",
			Owner:           owner,
			Repo:            repo,
			TargetNamespace: app.Spec.Destination.Namespace,
		}
		sources = append(sources, src)
	}

	c.Log.Info("discovered ArgoCD applications", "count", len(sources))
	return sources, nil
}

// ResolveWorkloads lists Deployments and StatefulSets in the application's destination namespace.
func (c *ArgoCDConnector) ResolveWorkloads(ctx context.Context, src PipelineSource) ([]v1alpha1.WorkloadRef, error) {
	ns := src.TargetNamespace
	if ns == "" {
		return nil, nil
	}

	var refs []v1alpha1.WorkloadRef

	// List Deployments
	var deployments appsv1.DeploymentList
	if err := c.Client.List(ctx, &deployments, client.InNamespace(ns)); err != nil {
		return nil, fmt.Errorf("listing deployments in %s: %w", ns, err)
	}
	for _, d := range deployments.Items {
		refs = append(refs, v1alpha1.WorkloadRef{
			Kind:      "Deployment",
			Name:      d.Name,
			Namespace: d.Namespace,
		})
	}

	// List StatefulSets
	var statefulSets appsv1.StatefulSetList
	if err := c.Client.List(ctx, &statefulSets, client.InNamespace(ns)); err != nil {
		return nil, fmt.Errorf("listing statefulsets in %s: %w", ns, err)
	}
	for _, s := range statefulSets.Items {
		refs = append(refs, v1alpha1.WorkloadRef{
			Kind:      "StatefulSet",
			Name:      s.Name,
			Namespace: s.Namespace,
		})
	}

	return refs, nil
}

// Suspend disables automatic sync by removing spec.syncPolicy.automated from the Application.
func (c *ArgoCDConnector) Suspend(ctx context.Context, src PipelineSource) error {
	c.Log.Info("suspending ArgoCD application sync", "name", src.SourceName, "namespace", src.Namespace)

	// Use a JSON merge patch to remove the automated sync policy
	patch := []byte(`{"spec":{"syncPolicy":{"automated":null}}}`)
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "argoproj.io",
		Version: "v1alpha1",
		Kind:    "Application",
	})
	obj.SetName(src.SourceName)
	obj.SetNamespace(src.Namespace)

	return c.Client.Patch(ctx, obj, client.RawPatch(types.MergePatchType, patch))
}

// Resume re-enables automatic sync by adding spec.syncPolicy.automated to the Application.
func (c *ArgoCDConnector) Resume(ctx context.Context, src PipelineSource) error {
	c.Log.Info("resuming ArgoCD application sync", "name", src.SourceName, "namespace", src.Namespace)

	patch := []byte(`{"spec":{"syncPolicy":{"automated":{"prune":true,"selfHeal":true}}}}`)
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "argoproj.io",
		Version: "v1alpha1",
		Kind:    "Application",
	})
	obj.SetName(src.SourceName)
	obj.SetNamespace(src.Namespace)

	return c.Client.Patch(ctx, obj, client.RawPatch(types.MergePatchType, patch))
}

// unmarshalArgoCDApp converts an unstructured object to an ArgoCDApplication.
func unmarshalArgoCDApp(u *unstructured.Unstructured) (*ArgoCDApplication, error) {
	data, err := u.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var app ArgoCDApplication
	if err := json.Unmarshal(data, &app); err != nil {
		return nil, err
	}
	return &app, nil
}
