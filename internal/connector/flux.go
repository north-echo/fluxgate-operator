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

// Minimal Flux CRD types (avoids importing the full Flux toolkit packages).

// FluxGitRepository represents a Flux GitRepository source.
type FluxGitRepository struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              FluxGitRepoSpec `json:"spec"`
}

// FluxGitRepoSpec is the spec of a Flux GitRepository.
type FluxGitRepoSpec struct {
	URL       string            `json:"url"`
	Reference *FluxGitReference `json:"ref,omitempty"`
}

// FluxGitReference specifies the Git reference to track.
type FluxGitReference struct {
	Branch string `json:"branch,omitempty"`
	Tag    string `json:"tag,omitempty"`
	Commit string `json:"commit,omitempty"`
}

// FluxKustomization represents a Flux Kustomization resource.
type FluxKustomization struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              FluxKustomizationSpec `json:"spec"`
}

// FluxKustomizationSpec is the spec of a Flux Kustomization.
type FluxKustomizationSpec struct {
	SourceRef       FluxSourceRef `json:"sourceRef"`
	Path            string        `json:"path"`
	Suspend         bool          `json:"suspend"`
	TargetNamespace string        `json:"targetNamespace,omitempty"`
}

// FluxSourceRef references a Flux source object.
type FluxSourceRef struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

// FluxConnector discovers pipeline sources from Flux GitRepository resources.
type FluxConnector struct {
	Client client.Client
	Log    logr.Logger
}

var _ PipelineConnector = &FluxConnector{}

func (c *FluxConnector) Name() string {
	return "flux"
}

// Discover lists Flux GitRepository objects and their referencing Kustomizations.
func (c *FluxConnector) Discover(ctx context.Context, namespace string) ([]PipelineSource, error) {
	c.Log.Info("discovering Flux GitRepositories", "namespace", namespace)

	// List GitRepository objects
	gitRepoList := &unstructured.UnstructuredList{}
	gitRepoList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "source.toolkit.fluxcd.io",
		Version: "v1",
		Kind:    "GitRepositoryList",
	})

	if err := c.Client.List(ctx, gitRepoList, client.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("listing Flux GitRepositories: %w", err)
	}

	// List Kustomization objects to find references to GitRepositories
	kustomizationList := &unstructured.UnstructuredList{}
	kustomizationList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "kustomize.toolkit.fluxcd.io",
		Version: "v1",
		Kind:    "KustomizationList",
	})

	if err := c.Client.List(ctx, kustomizationList, client.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("listing Flux Kustomizations: %w", err)
	}

	// Index kustomizations by source ref
	ksBySource := make(map[string][]FluxKustomization)
	for _, item := range kustomizationList.Items {
		ks, err := unmarshalFluxKustomization(&item)
		if err != nil {
			c.Log.Error(err, "failed to unmarshal Flux Kustomization", "name", item.GetName())
			continue
		}
		key := fmt.Sprintf("%s/%s", ks.Spec.SourceRef.Kind, ks.Spec.SourceRef.Name)
		ksBySource[key] = append(ksBySource[key], *ks)
	}

	var sources []PipelineSource
	for _, item := range gitRepoList.Items {
		gitRepo, err := unmarshalFluxGitRepo(&item)
		if err != nil {
			c.Log.Error(err, "failed to unmarshal Flux GitRepository", "name", item.GetName())
			continue
		}

		owner, repo, ok := ParseRepoURL(gitRepo.Spec.URL)
		if !ok {
			c.Log.Info("skipping GitRepository with unparseable URL",
				"name", gitRepo.Name, "url", gitRepo.Spec.URL)
			continue
		}

		branch := "main"
		if gitRepo.Spec.Reference != nil {
			if gitRepo.Spec.Reference.Branch != "" {
				branch = gitRepo.Spec.Reference.Branch
			} else if gitRepo.Spec.Reference.Tag != "" {
				branch = gitRepo.Spec.Reference.Tag
			}
		}

		// Find kustomizations referencing this GitRepository
		key := fmt.Sprintf("GitRepository/%s", gitRepo.Name)
		kustomizations := ksBySource[key]

		if len(kustomizations) == 0 {
			// No kustomization references this GitRepository; still discover it
			src := PipelineSource{
				Name:       gitRepo.Name,
				Repository: gitRepo.Spec.URL,
				Branch:     branch,
				Platform:   "github-actions",
				Labels:     gitRepo.Labels,
				Namespace:  gitRepo.Namespace,
				SourceName: gitRepo.Name,
				SourceKind: "GitRepository",
				Owner:      owner,
				Repo:       repo,
			}
			sources = append(sources, src)
			continue
		}

		for _, ks := range kustomizations {
			var paths []string
			if ks.Spec.Path != "" {
				paths = []string{ks.Spec.Path}
			}

			targetNS := ks.Spec.TargetNamespace
			if targetNS == "" {
				targetNS = ks.Namespace
			}

			src := PipelineSource{
				Name:            fmt.Sprintf("%s/%s", gitRepo.Name, ks.Name),
				Repository:      gitRepo.Spec.URL,
				Branch:          branch,
				Paths:           paths,
				Platform:        "github-actions",
				Labels:          gitRepo.Labels,
				Namespace:       gitRepo.Namespace,
				SourceName:      ks.Name,
				SourceKind:      "Kustomization",
				Owner:           owner,
				Repo:            repo,
				TargetNamespace: targetNS,
			}
			sources = append(sources, src)
		}
	}

	c.Log.Info("discovered Flux GitRepositories", "count", len(sources))
	return sources, nil
}

// ResolveWorkloads lists Deployments in the Kustomization's target namespace.
func (c *FluxConnector) ResolveWorkloads(ctx context.Context, src PipelineSource) ([]v1alpha1.WorkloadRef, error) {
	ns := src.TargetNamespace
	if ns == "" {
		return nil, nil
	}

	var refs []v1alpha1.WorkloadRef

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

	return refs, nil
}

// Suspend patches the Kustomization to set spec.suspend: true.
func (c *FluxConnector) Suspend(ctx context.Context, src PipelineSource) error {
	c.Log.Info("suspending Flux Kustomization", "name", src.SourceName, "namespace", src.Namespace)

	patch := []byte(`{"spec":{"suspend":true}}`)
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "kustomize.toolkit.fluxcd.io",
		Version: "v1",
		Kind:    "Kustomization",
	})
	obj.SetName(src.SourceName)
	obj.SetNamespace(src.Namespace)

	return c.Client.Patch(ctx, obj, client.RawPatch(types.MergePatchType, patch))
}

// Resume patches the Kustomization to set spec.suspend: false.
func (c *FluxConnector) Resume(ctx context.Context, src PipelineSource) error {
	c.Log.Info("resuming Flux Kustomization", "name", src.SourceName, "namespace", src.Namespace)

	patch := []byte(`{"spec":{"suspend":false}}`)
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "kustomize.toolkit.fluxcd.io",
		Version: "v1",
		Kind:    "Kustomization",
	})
	obj.SetName(src.SourceName)
	obj.SetNamespace(src.Namespace)

	return c.Client.Patch(ctx, obj, client.RawPatch(types.MergePatchType, patch))
}

// unmarshalFluxGitRepo converts an unstructured object to a FluxGitRepository.
func unmarshalFluxGitRepo(u *unstructured.Unstructured) (*FluxGitRepository, error) {
	data, err := u.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var repo FluxGitRepository
	if err := json.Unmarshal(data, &repo); err != nil {
		return nil, err
	}
	return &repo, nil
}

// unmarshalFluxKustomization converts an unstructured object to a FluxKustomization.
func unmarshalFluxKustomization(u *unstructured.Unstructured) (*FluxKustomization, error) {
	data, err := u.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var ks FluxKustomization
	if err := json.Unmarshal(data, &ks); err != nil {
		return nil, err
	}
	return &ks, nil
}
