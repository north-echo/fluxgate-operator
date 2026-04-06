package connector

import (
	"context"

	v1alpha1 "github.com/north-echo/fluxgate-operator/api/v1alpha1"
)

// PipelineSource represents a discovered CI/CD pipeline source.
type PipelineSource struct {
	// Name is the human-readable name of the source.
	Name string

	// Repository is the Git repository URL (e.g. "https://github.com/org/repo").
	Repository string

	// Branch is the Git branch to scan.
	Branch string

	// Paths is the list of workflow file paths within the repository.
	Paths []string

	// Platform identifies the CI/CD platform (e.g. "github-actions", "gitlab-ci", "azure-pipelines").
	Platform string

	// Labels are arbitrary key-value pairs from the source object.
	Labels map[string]string

	// Namespace is the Kubernetes namespace of the source object.
	Namespace string

	// SourceName is the name of the Kubernetes object that owns this source.
	SourceName string

	// SourceKind is the kind of the Kubernetes object (e.g. "Application", "GitRepository").
	SourceKind string

	// Owner is the parsed repository owner (e.g. "org" from "github.com/org/repo").
	Owner string

	// Repo is the parsed repository name (e.g. "repo" from "github.com/org/repo").
	Repo string

	// TargetNamespace is the Kubernetes namespace that workloads are deployed to.
	TargetNamespace string
}

// PipelineConnector discovers CI/CD pipeline sources from a GitOps platform.
type PipelineConnector interface {
	// Discover returns all pipeline sources managed by this connector.
	Discover(ctx context.Context, namespace string) ([]PipelineSource, error)

	// Name returns the connector name (e.g. "argocd", "flux").
	Name() string

	// ResolveWorkloads returns the workloads deployed by the given pipeline source.
	ResolveWorkloads(ctx context.Context, src PipelineSource) ([]v1alpha1.WorkloadRef, error)

	// Suspend disables automatic sync/reconciliation for the given source.
	Suspend(ctx context.Context, src PipelineSource) error

	// Resume re-enables automatic sync/reconciliation for the given source.
	Resume(ctx context.Context, src PipelineSource) error
}
