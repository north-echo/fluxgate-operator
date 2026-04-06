package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fluxgatev1alpha1 "github.com/north-echo/fluxgate-operator/api/v1alpha1"
	"github.com/north-echo/fluxgate-operator/internal/alert"
	"github.com/north-echo/fluxgate-operator/internal/connector"
	fluxgatemetrics "github.com/north-echo/fluxgate-operator/internal/metrics"
)

const (
	annotationComplianceState = "fluxgate.north-echo.dev/compliance-state"
	annotationFindingSummary  = "fluxgate.north-echo.dev/finding-summary"
	annotationFirstDetected   = "fluxgate.north-echo.dev/first-detected"
	labelPipelineRisk         = "fluxgate.north-echo.dev/pipeline-risk"
)

// PolicyController watches PipelineSecurityPolicy resources and evaluates
// PipelineSecurityReports against policy thresholds to trigger enforcement actions.
type PolicyController struct {
	Client     client.Client
	Log        logr.Logger
	Scheme     *runtime.Scheme
	Connectors []connector.PipelineConnector
	Registry   *SourceRegistry
	Recorder   record.EventRecorder
}

func (r *PolicyController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Log.Info("reconciling policy", "name", req.NamespacedName)

	// Fetch the PipelineSecurityPolicy
	policy := &fluxgatev1alpha1.PipelineSecurityPolicy{}
	if err := r.Client.Get(ctx, req.NamespacedName, policy); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// List all PipelineSecurityReport objects in the policy's namespace
	reportList := &fluxgatev1alpha1.PipelineSecurityReportList{}
	listOpts := []client.ListOption{}
	if policy.Namespace != "" {
		listOpts = append(listOpts, client.InNamespace(policy.Namespace))
	}
	if err := r.Client.List(ctx, reportList, listOpts...); err != nil {
		r.Log.Error(err, "failed to list reports")
		return ctrl.Result{}, err
	}

	for i := range reportList.Items {
		report := &reportList.Items[i]

		// Check if the policy selector matches this report
		if !r.selectorMatches(policy, report) {
			continue
		}

		// Evaluate report against policy thresholds
		violationLevel := r.evaluateThresholds(policy, report)

		// Execute enforcement actions based on violation level
		if err := r.executeEnforcement(ctx, policy, report, violationLevel); err != nil {
			r.Log.Error(err, "enforcement failed",
				"policy", policy.Name,
				"report", report.Name,
				"violation", violationLevel,
			)
		}
	}

	// Update policy status
	policy.Status.Phase = "Active"
	policy.Status.Message = fmt.Sprintf("Evaluated %d reports", len(reportList.Items))
	if err := r.Client.Status().Update(ctx, policy); err != nil {
		r.Log.Error(err, "failed to update policy status")
		return ctrl.Result{}, err
	}

	// Requeue at the policy's evaluation interval
	requeueAfter := policy.Spec.EvaluationInterval.Duration
	if requeueAfter == 0 {
		requeueAfter = 5 * time.Minute
	}

	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// selectorMatches returns true if the policy selector matches the given report.
func (r *PolicyController) selectorMatches(policy *fluxgatev1alpha1.PipelineSecurityPolicy, report *fluxgatev1alpha1.PipelineSecurityReport) bool {
	sel := policy.Spec.Selector

	// Check cicdKinds match
	if len(sel.CICDKinds) > 0 {
		sourceKind := report.Labels["fluxgate.north-echo.dev/source-kind"]
		matched := false
		for _, kind := range sel.CICDKinds {
			if strings.EqualFold(kind, sourceKind) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Check matchLabels
	if len(sel.MatchLabels) > 0 {
		for key, val := range sel.MatchLabels {
			if report.Labels[key] != val {
				return false
			}
		}
	}

	return true
}

// evaluateThresholds evaluates report findings against policy thresholds
// and returns the violation level.
func (r *PolicyController) evaluateThresholds(policy *fluxgatev1alpha1.PipelineSecurityPolicy, report *fluxgatev1alpha1.PipelineSecurityReport) string {
	thresholds := policy.Spec.Thresholds
	summary := report.Status.Summary

	// Check critical threshold
	if summary.Critical > thresholds.MaxCritical {
		return "Critical"
	}

	// Check high threshold
	if summary.High > thresholds.MaxHigh {
		return "NonCompliant"
	}

	// Check requirePinnedActions — look for unpinned action findings
	if thresholds.RequirePinnedActions {
		for _, f := range report.Status.Findings {
			if strings.Contains(strings.ToLower(f.Rule), "pin") ||
				strings.Contains(strings.ToLower(f.Message), "unpinned") ||
				strings.Contains(strings.ToLower(f.Message), "not pinned") {
				return "NonCompliant"
			}
		}
	}

	// Determine warning level
	if summary.Medium > 0 {
		return "Warning"
	}

	return "Compliant"
}

// executeEnforcement executes enforcement actions for the given violation level.
func (r *PolicyController) executeEnforcement(ctx context.Context, policy *fluxgatev1alpha1.PipelineSecurityPolicy, report *fluxgatev1alpha1.PipelineSecurityReport, violationLevel string) error {
	// If compliant, restore normal state
	if violationLevel == "Compliant" {
		return r.restoreCompliance(ctx, report)
	}

	for _, action := range policy.Spec.Enforcement {
		// Only execute actions whose trigger matches the violation level or lower
		if !triggerMatches(action.Trigger, violationLevel) {
			continue
		}

		var err error
		switch action.Action {
		case "annotate":
			err = r.executeAnnotate(ctx, report)
		case "alert":
			err = r.executeAlert(ctx, report, action, violationLevel)
		case "suspendSync":
			err = r.executeSuspendSync(ctx, report, action)
		case "labelWorkloads":
			err = r.executeLabelWorkloads(ctx, report, violationLevel)
		default:
			r.Log.Info("unknown enforcement action", "action", action.Action)
			continue
		}

		if err != nil {
			r.Log.Error(err, "enforcement action failed",
				"action", action.Action,
				"report", report.Name,
			)
			continue
		}

		// Record enforcement metric
		fluxgatemetrics.EnforcementActions.WithLabelValues(
			action.Action, report.Namespace, report.Name,
		).Inc()

		r.Log.Info("enforcement action executed",
			"action", action.Action,
			"report", report.Name,
			"violation", violationLevel,
		)
	}

	return nil
}

// triggerMatches returns true if the violation level meets or exceeds the trigger level.
func triggerMatches(trigger, violationLevel string) bool {
	levels := map[string]int{
		"Warning":      1,
		"NonCompliant": 2,
		"Critical":     3,
	}

	triggerLevel, ok := levels[trigger]
	if !ok {
		return false
	}
	violLevel, ok := levels[violationLevel]
	if !ok {
		return false
	}

	return violLevel >= triggerLevel
}

// executeAnnotate adds compliance annotations to the CI/CD source resource.
func (r *PolicyController) executeAnnotate(ctx context.Context, report *fluxgatev1alpha1.PipelineSecurityReport) error {
	summary := report.Status.Summary
	summaryStr := fmt.Sprintf("%d critical, %d high, %d medium",
		summary.Critical, summary.High, summary.Medium)

	// Annotate the report itself
	if report.Annotations == nil {
		report.Annotations = make(map[string]string)
	}
	report.Annotations[annotationComplianceState] = report.Status.ComplianceState
	report.Annotations[annotationFindingSummary] = summaryStr

	if err := r.Client.Update(ctx, report); err != nil {
		return fmt.Errorf("annotating report %s: %w", report.Name, err)
	}

	return nil
}

// executeAlert sends a notification based on the alert target configuration.
func (r *PolicyController) executeAlert(ctx context.Context, report *fluxgatev1alpha1.PipelineSecurityReport, action fluxgatev1alpha1.EnforcementAction, violationLevel string) error {
	if action.AlertTarget == nil {
		return fmt.Errorf("alert action has no alertTarget configured")
	}

	target := action.AlertTarget
	summary := report.Status.Summary
	message := fmt.Sprintf(
		"[Fluxgate] Pipeline %s/%s is %s: %d critical, %d high, %d medium findings (repo: %s)",
		report.Namespace, report.Name,
		violationLevel,
		summary.Critical, summary.High, summary.Medium,
		report.Status.Source.Repository,
	)

	switch target.Type {
	case "slack":
		if target.URL == "" {
			return fmt.Errorf("slack alert target has no URL")
		}
		return alert.SendSlack(target.URL, target.Channel, message)

	case "webhook":
		if target.URL == "" {
			return fmt.Errorf("webhook alert target has no URL")
		}
		payload := map[string]interface{}{
			"report":     report.Name,
			"namespace":  report.Namespace,
			"state":      violationLevel,
			"repository": report.Status.Source.Repository,
			"summary": map[string]int{
				"critical": summary.Critical,
				"high":     summary.High,
				"medium":   summary.Medium,
				"low":      summary.Low,
				"total":    summary.Total,
			},
			"message": message,
		}
		return alert.SendWebhook(target.URL, payload)

	default:
		return fmt.Errorf("unsupported alert target type: %s", target.Type)
	}
}

// executeSuspendSync suspends the CI/CD source's sync/reconciliation.
func (r *PolicyController) executeSuspendSync(ctx context.Context, report *fluxgatev1alpha1.PipelineSecurityReport, action fluxgatev1alpha1.EnforcementAction) error {
	// Check grace period
	if action.GracePeriod != "" {
		graceDuration, err := time.ParseDuration(action.GracePeriod)
		if err != nil {
			return fmt.Errorf("parsing grace period %q: %w", action.GracePeriod, err)
		}

		firstDetected := report.Annotations[annotationFirstDetected]
		if firstDetected == "" {
			// Mark first detection time
			if report.Annotations == nil {
				report.Annotations = make(map[string]string)
			}
			report.Annotations[annotationFirstDetected] = time.Now().Format(time.RFC3339)
			if err := r.Client.Update(ctx, report); err != nil {
				return fmt.Errorf("setting first-detected annotation: %w", err)
			}
			r.Log.Info("grace period started, skipping suspend",
				"report", report.Name,
				"gracePeriod", action.GracePeriod,
			)
			return nil
		}

		detectedTime, err := time.Parse(time.RFC3339, firstDetected)
		if err != nil {
			return fmt.Errorf("parsing first-detected time: %w", err)
		}

		if time.Since(detectedTime) < graceDuration {
			r.Log.Info("within grace period, skipping suspend",
				"report", report.Name,
				"firstDetected", firstDetected,
				"gracePeriod", action.GracePeriod,
			)
			return nil
		}
	}

	// Find the matching connector and source, then suspend
	sourceName := report.Labels["fluxgate.north-echo.dev/source-name"]
	sourceKind := report.Labels["fluxgate.north-echo.dev/source-kind"]

	if sourceName == "" || sourceKind == "" {
		return fmt.Errorf("report %s missing source labels", report.Name)
	}

	src, found := r.Registry.Get(sourceName)
	if !found {
		return fmt.Errorf("source %s not found in registry", sourceName)
	}

	for _, conn := range r.Connectors {
		if (sourceKind == "Application" && conn.Name() == "argocd") ||
			(sourceKind == "Kustomization" && conn.Name() == "flux") ||
			(sourceKind == "GitRepository" && conn.Name() == "flux") {
			if err := conn.Suspend(ctx, src); err != nil {
				return fmt.Errorf("suspending %s via %s: %w", sourceName, conn.Name(), err)
			}

			// Emit event
			if r.Recorder != nil {
				r.Recorder.Eventf(report, corev1.EventTypeWarning, "SyncSuspended",
					"Sync suspended for %s/%s due to policy violation", report.Namespace, sourceName)
			}

			r.Log.Info("suspended sync", "source", sourceName, "connector", conn.Name())
			return nil
		}
	}

	return fmt.Errorf("no connector found for source kind %s", sourceKind)
}

// executeLabelWorkloads adds risk labels to correlated workloads.
func (r *PolicyController) executeLabelWorkloads(ctx context.Context, report *fluxgatev1alpha1.PipelineSecurityReport, violationLevel string) error {
	riskLevel := strings.ToLower(violationLevel)

	for _, wl := range report.Status.CorrelatedWorkloads {
		if err := r.patchWorkloadLabel(ctx, wl, labelPipelineRisk, riskLevel); err != nil {
			r.Log.Error(err, "failed to label workload",
				"kind", wl.Kind,
				"name", wl.Name,
				"namespace", wl.Namespace,
			)
			continue
		}
	}

	return nil
}

// patchWorkloadLabel patches a label onto a Deployment or StatefulSet.
func (r *PolicyController) patchWorkloadLabel(ctx context.Context, wl fluxgatev1alpha1.WorkloadRef, key, value string) error {
	patchData := []byte(fmt.Sprintf(
		`{"metadata":{"labels":{%q:%q}}}`,
		key, value,
	))
	patch := client.RawPatch(types.MergePatchType, patchData)

	nn := types.NamespacedName{Name: wl.Name, Namespace: wl.Namespace}

	switch wl.Kind {
	case "Deployment":
		obj := &appsv1.Deployment{}
		if err := r.Client.Get(ctx, nn, obj); err != nil {
			return err
		}
		return r.Client.Patch(ctx, obj, patch)
	case "StatefulSet":
		obj := &appsv1.StatefulSet{}
		if err := r.Client.Get(ctx, nn, obj); err != nil {
			return err
		}
		return r.Client.Patch(ctx, obj, patch)
	default:
		return fmt.Errorf("unsupported workload kind: %s", wl.Kind)
	}
}

// restoreCompliance removes enforcement artifacts when a report returns to compliance.
func (r *PolicyController) restoreCompliance(ctx context.Context, report *fluxgatev1alpha1.PipelineSecurityReport) error {
	updated := false

	// Remove compliance annotations
	if report.Annotations != nil {
		for _, key := range []string{annotationComplianceState, annotationFindingSummary, annotationFirstDetected} {
			if _, exists := report.Annotations[key]; exists {
				delete(report.Annotations, key)
				updated = true
			}
		}
	}

	if updated {
		if err := r.Client.Update(ctx, report); err != nil {
			return fmt.Errorf("removing annotations from report %s: %w", report.Name, err)
		}
	}

	// Resume sync if it was suspended
	sourceName := report.Labels["fluxgate.north-echo.dev/source-name"]
	sourceKind := report.Labels["fluxgate.north-echo.dev/source-kind"]
	if sourceName != "" && sourceKind != "" {
		if src, found := r.Registry.Get(sourceName); found {
			for _, conn := range r.Connectors {
				if (sourceKind == "Application" && conn.Name() == "argocd") ||
					(sourceKind == "Kustomization" && conn.Name() == "flux") ||
					(sourceKind == "GitRepository" && conn.Name() == "flux") {
					if err := conn.Resume(ctx, src); err != nil {
						r.Log.Error(err, "failed to resume sync", "source", sourceName)
					} else {
						r.Log.Info("resumed sync", "source", sourceName, "connector", conn.Name())
						if r.Recorder != nil {
							r.Recorder.Eventf(report, corev1.EventTypeNormal, "SyncResumed",
								"Sync resumed for %s/%s after compliance restored", report.Namespace, sourceName)
						}
					}
					break
				}
			}
		}
	}

	// Remove risk labels from correlated workloads
	for _, wl := range report.Status.CorrelatedWorkloads {
		if err := r.removeWorkloadLabel(ctx, wl, labelPipelineRisk); err != nil {
			r.Log.Error(err, "failed to remove label from workload",
				"kind", wl.Kind,
				"name", wl.Name,
				"namespace", wl.Namespace,
			)
		}
	}

	return nil
}

// removeWorkloadLabel removes a label from a Deployment or StatefulSet.
func (r *PolicyController) removeWorkloadLabel(ctx context.Context, wl fluxgatev1alpha1.WorkloadRef, key string) error {
	patchData := []byte(fmt.Sprintf(
		`{"metadata":{"labels":{%q:null}}}`,
		key,
	))
	patch := client.RawPatch(types.MergePatchType, patchData)

	nn := types.NamespacedName{Name: wl.Name, Namespace: wl.Namespace}

	switch wl.Kind {
	case "Deployment":
		obj := &appsv1.Deployment{}
		if err := r.Client.Get(ctx, nn, obj); err != nil {
			return client.IgnoreNotFound(err)
		}
		return r.Client.Patch(ctx, obj, patch)
	case "StatefulSet":
		obj := &appsv1.StatefulSet{}
		if err := r.Client.Get(ctx, nn, obj); err != nil {
			return client.IgnoreNotFound(err)
		}
		return r.Client.Patch(ctx, obj, patch)
	default:
		return nil
	}
}

func (r *PolicyController) SetupWithManager(mgr ctrl.Manager) error {
	r.Recorder = mgr.GetEventRecorderFor("fluxgate-policy-controller")
	return ctrl.NewControllerManagedBy(mgr).
		For(&fluxgatev1alpha1.PipelineSecurityPolicy{}).
		Named("policy").
		Complete(r)
}
