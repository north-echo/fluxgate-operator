package connector

import (
	"context"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ArgoCDConnector discovers pipeline sources from ArgoCD Application resources.
type ArgoCDConnector struct {
	Client client.Client
	Log    logr.Logger
}

var _ PipelineConnector = &ArgoCDConnector{}

func (c *ArgoCDConnector) Name() string {
	return "argocd"
}

func (c *ArgoCDConnector) Discover(ctx context.Context, namespace string) ([]PipelineSource, error) {
	c.Log.Info("discovering ArgoCD applications", "namespace", namespace)
	// TODO: list ArgoCD Application resources, extract repo URLs and paths
	return nil, nil
}
