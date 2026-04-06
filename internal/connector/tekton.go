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

// Minimal Tekton CRD types (avoids importing the full Tekton package).

// TektonPipeline is a minimal representation of a Tekton Pipeline CRD.
type TektonPipeline struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              TektonPipelineSpec `json:"spec"`
}

// TektonPipelineSpec is the spec of a Tekton Pipeline.
type TektonPipelineSpec struct {
	Tasks  []TektonPipelineTask `json:"tasks,omitempty"`
	Params []TektonParam        `json:"params,omitempty"`
}

// TektonPipelineTask defines a task within a Tekton Pipeline.
type TektonPipelineTask struct {
	Name     string          `json:"name"`
	TaskRef  *TektonTaskRef  `json:"taskRef,omitempty"`
	TaskSpec *TektonTaskSpec `json:"taskSpec,omitempty"`
}

// TektonTaskRef references an external Tekton Task.
type TektonTaskRef struct {
	Name   string `json:"name"`
	Bundle string `json:"bundle,omitempty"`
}

// TektonTaskSpec defines an inline task specification.
type TektonTaskSpec struct {
	Steps []TektonStep `json:"steps,omitempty"`
}

// TektonStep defines a single step within a Tekton Task.
type TektonStep struct {
	Name    string   `json:"name"`
	Image   string   `json:"image"`
	Script  string   `json:"script,omitempty"`
	Command []string `json:"command,omitempty"`
}

// TektonParam defines a parameter for a Tekton Pipeline.
type TektonParam struct {
	Name    string            `json:"name"`
	Type    string            `json:"type,omitempty"`
	Default *TektonParamValue `json:"default,omitempty"`
}

// TektonParamValue holds a parameter default value.
type TektonParamValue struct {
	Type      string `json:"type,omitempty"`
	StringVal string `json:"stringVal,omitempty"`
}

// UnmarshalJSON handles both string and object forms of Tekton param defaults.
func (v *TektonParamValue) UnmarshalJSON(data []byte) error {
	// Try as a plain string first
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		v.StringVal = s
		v.Type = "string"
		return nil
	}
	// Fall back to object form
	type raw TektonParamValue
	var r raw
	if err := json.Unmarshal(data, &r); err != nil {
		return err
	}
	*v = TektonParamValue(r)
	return nil
}

// TektonPipelineRun is a minimal representation of a Tekton PipelineRun.
type TektonPipelineRun struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              TektonPipelineRunSpec `json:"spec"`
}

// TektonPipelineRunSpec is the spec of a Tekton PipelineRun.
type TektonPipelineRunSpec struct {
	PipelineRef *TektonPipelineRef `json:"pipelineRef,omitempty"`
}

// TektonPipelineRef references a Tekton Pipeline.
type TektonPipelineRef struct {
	Name string `json:"name"`
}

const (
	tektonSuspendAnnotation = "fluxgate.north-echo.dev/suspended"
)

// TektonConnector discovers pipeline sources from Tekton Pipeline resources.
type TektonConnector struct {
	Client client.Client
	Log    logr.Logger
}

var _ PipelineConnector = &TektonConnector{}

func (c *TektonConnector) Name() string {
	return "tekton"
}

// Discover lists Tekton Pipeline objects in the namespace and returns pipeline sources.
func (c *TektonConnector) Discover(ctx context.Context, namespace string) ([]PipelineSource, error) {
	c.Log.Info("discovering Tekton Pipelines", "namespace", namespace)

	// List Pipeline objects
	pipelineList := &unstructured.UnstructuredList{}
	pipelineList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "tekton.dev",
		Version: "v1",
		Kind:    "PipelineList",
	})

	if err := c.Client.List(ctx, pipelineList, client.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("listing Tekton Pipelines: %w", err)
	}

	// List PipelineRun objects to find which Pipelines are actively used
	pipelineRunList := &unstructured.UnstructuredList{}
	pipelineRunList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "tekton.dev",
		Version: "v1",
		Kind:    "PipelineRunList",
	})

	activePipelines := make(map[string]bool)
	if err := c.Client.List(ctx, pipelineRunList, client.InNamespace(namespace)); err != nil {
		// PipelineRuns are optional; log and continue
		c.Log.Info("unable to list Tekton PipelineRuns, skipping active-use check", "error", err)
	} else {
		for _, item := range pipelineRunList.Items {
			run, err := unmarshalTektonPipelineRun(&item)
			if err != nil {
				c.Log.Error(err, "failed to unmarshal Tekton PipelineRun", "name", item.GetName())
				continue
			}
			if run.Spec.PipelineRef != nil && run.Spec.PipelineRef.Name != "" {
				activePipelines[run.Spec.PipelineRef.Name] = true
			}
		}
	}

	var sources []PipelineSource
	for _, item := range pipelineList.Items {
		pipeline, err := unmarshalTektonPipeline(&item)
		if err != nil {
			c.Log.Error(err, "failed to unmarshal Tekton Pipeline", "name", item.GetName())
			continue
		}

		// Extract Git repository URL and branch from pipeline params
		repoURL, branch := extractGitParams(pipeline)
		if repoURL == "" {
			c.Log.Info("skipping Tekton Pipeline without git-url param",
				"name", pipeline.Name, "namespace", pipeline.Namespace)
			continue
		}

		owner, repo, ok := ParseRepoURL(repoURL)
		if !ok {
			c.Log.Info("skipping Tekton Pipeline with unparseable repo URL",
				"name", pipeline.Name, "repoURL", repoURL)
			continue
		}

		if branch == "" {
			branch = "main"
		}

		labels := pipeline.Labels
		if labels == nil {
			labels = make(map[string]string)
		}
		if activePipelines[pipeline.Name] {
			labels["fluxgate.north-echo.dev/active"] = "true"
		}

		// Check for external task references (bundles)
		hasExternalTasks := false
		for _, task := range pipeline.Spec.Tasks {
			if task.TaskRef != nil && task.TaskRef.Bundle != "" {
				hasExternalTasks = true
				break
			}
		}
		if hasExternalTasks {
			labels["fluxgate.north-echo.dev/external-tasks"] = "true"
		}

		src := PipelineSource{
			Name:       pipeline.Name,
			Repository: repoURL,
			Branch:     branch,
			Platform:   "github-actions",
			Labels:     labels,
			Namespace:  pipeline.Namespace,
			SourceName: pipeline.Name,
			SourceKind: "Pipeline",
			Owner:      owner,
			Repo:       repo,
		}
		sources = append(sources, src)
	}

	c.Log.Info("discovered Tekton Pipelines", "count", len(sources))
	return sources, nil
}

// ResolveWorkloads lists Deployments in the same namespace as the Pipeline (best-effort correlation).
func (c *TektonConnector) ResolveWorkloads(ctx context.Context, src PipelineSource) ([]v1alpha1.WorkloadRef, error) {
	ns := src.Namespace
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

// Suspend annotates the Pipeline with the fluxgate suspend annotation.
// Tekton does not have a native suspend mechanism, so this is advisory-only.
func (c *TektonConnector) Suspend(ctx context.Context, src PipelineSource) error {
	c.Log.Info("suspending Tekton Pipeline (advisory-only annotation)",
		"name", src.SourceName, "namespace", src.Namespace)
	c.Log.Info("WARNING: Tekton suspend is advisory-only; PipelineRuns can still be created")

	patch := []byte(fmt.Sprintf(`{"metadata":{"annotations":{%q:"true"}}}`, tektonSuspendAnnotation))
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "tekton.dev",
		Version: "v1",
		Kind:    "Pipeline",
	})
	obj.SetName(src.SourceName)
	obj.SetNamespace(src.Namespace)

	return c.Client.Patch(ctx, obj, client.RawPatch(types.MergePatchType, patch))
}

// Resume removes the fluxgate suspend annotation from the Pipeline.
func (c *TektonConnector) Resume(ctx context.Context, src PipelineSource) error {
	c.Log.Info("resuming Tekton Pipeline (removing advisory annotation)",
		"name", src.SourceName, "namespace", src.Namespace)

	// Use a JSON merge patch to set the annotation to null (removes it)
	patch := []byte(fmt.Sprintf(`{"metadata":{"annotations":{%q:null}}}`, tektonSuspendAnnotation))
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "tekton.dev",
		Version: "v1",
		Kind:    "Pipeline",
	})
	obj.SetName(src.SourceName)
	obj.SetNamespace(src.Namespace)

	return c.Client.Patch(ctx, obj, client.RawPatch(types.MergePatchType, patch))
}

// extractGitParams extracts git-url and git-revision from Tekton Pipeline params.
func extractGitParams(pipeline *TektonPipeline) (repoURL, branch string) {
	for _, p := range pipeline.Spec.Params {
		if p.Default == nil {
			continue
		}
		switch p.Name {
		case "git-url", "repo-url", "repository-url", "url":
			repoURL = p.Default.StringVal
		case "git-revision", "revision", "branch":
			branch = p.Default.StringVal
		}
	}
	return repoURL, branch
}

// unmarshalTektonPipeline converts an unstructured object to a TektonPipeline.
func unmarshalTektonPipeline(u *unstructured.Unstructured) (*TektonPipeline, error) {
	data, err := u.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var pipeline TektonPipeline
	if err := json.Unmarshal(data, &pipeline); err != nil {
		return nil, err
	}
	return &pipeline, nil
}

// unmarshalTektonPipelineRun converts an unstructured object to a TektonPipelineRun.
func unmarshalTektonPipelineRun(u *unstructured.Unstructured) (*TektonPipelineRun, error) {
	data, err := u.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var run TektonPipelineRun
	if err := json.Unmarshal(data, &run); err != nil {
		return nil, err
	}
	return &run, nil
}
