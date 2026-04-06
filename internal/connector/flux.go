package connector

import (
	"context"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// FluxConnector discovers pipeline sources from Flux GitRepository resources.
type FluxConnector struct {
	Client client.Client
	Log    logr.Logger
}

var _ PipelineConnector = &FluxConnector{}

func (c *FluxConnector) Name() string {
	return "flux"
}

func (c *FluxConnector) Discover(ctx context.Context, namespace string) ([]PipelineSource, error) {
	c.Log.Info("discovering Flux GitRepositories", "namespace", namespace)
	// TODO: list Flux GitRepository resources, extract repo URLs
	return nil, nil
}
