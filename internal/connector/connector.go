package connector

import "context"

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
}

// PipelineConnector discovers CI/CD pipeline sources from a GitOps platform.
type PipelineConnector interface {
	// Discover returns all pipeline sources managed by this connector.
	Discover(ctx context.Context, namespace string) ([]PipelineSource, error)

	// Name returns the connector name (e.g. "argocd", "flux").
	Name() string
}
