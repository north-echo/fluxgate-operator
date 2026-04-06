package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=psp
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`

// PipelineSecurityPolicy defines thresholds and enforcement for pipeline security.
type PipelineSecurityPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              PipelineSecurityPolicySpec   `json:"spec"`
	Status            PipelineSecurityPolicyStatus `json:"status,omitempty"`
}

// PipelineSecurityPolicySpec defines the desired state of PipelineSecurityPolicy.
type PipelineSecurityPolicySpec struct {
	Selector           PolicySelector      `json:"selector"`
	Thresholds         PolicyThresholds    `json:"thresholds"`
	EvaluationInterval metav1.Duration     `json:"evaluationInterval"`
	Enforcement        []EnforcementAction `json:"enforcement,omitempty"`
	RuleOverrides      []RuleOverride      `json:"ruleOverrides,omitempty"`
}

// PolicySelector selects which CI/CD sources a policy applies to.
type PolicySelector struct {
	CICDKinds   []string          `json:"cicdKinds,omitempty"`
	MatchLabels map[string]string `json:"matchLabels,omitempty"`
}

// PolicyThresholds defines compliance thresholds.
type PolicyThresholds struct {
	MaxCritical             int  `json:"maxCritical"`
	MaxHigh                 int  `json:"maxHigh"`
	RequirePinnedActions    bool `json:"requirePinnedActions"`
	RequireSignedCommits    bool `json:"requireSignedCommits"`
	RequireBranchProtection bool `json:"requireBranchProtection"`
}

// EnforcementAction defines an action to take when a threshold is breached.
type EnforcementAction struct {
	Action      string       `json:"action"`                // annotate, alert, suspendSync, labelWorkloads
	Trigger     string       `json:"trigger"`               // Warning, NonCompliant, Critical
	GracePeriod string       `json:"gracePeriod,omitempty"` // e.g. "24h"
	AlertTarget *AlertTarget `json:"alertTarget,omitempty"`
}

// AlertTarget specifies where to send alerts.
type AlertTarget struct {
	Type    string `json:"type"`              // slack, webhook, pagerduty
	Channel string `json:"channel,omitempty"` // e.g. "#security-alerts"
	URL     string `json:"url,omitempty"`     // webhook URL
}

// RuleOverride allows overriding the severity of a specific rule.
type RuleOverride struct {
	Rule     string             `json:"rule"`
	Severity string             `json:"severity"`
	Reason   string             `json:"reason"`
	Scope    *RuleOverrideScope `json:"scope,omitempty"`
}

// RuleOverrideScope limits a rule override to specific repositories.
type RuleOverrideScope struct {
	Repositories []string `json:"repositories,omitempty"`
}

// PipelineSecurityPolicyStatus defines the observed state of PipelineSecurityPolicy.
type PipelineSecurityPolicyStatus struct {
	Phase   string `json:"phase,omitempty"`
	Message string `json:"message,omitempty"`
}

// +kubebuilder:object:root=true

// PipelineSecurityPolicyList contains a list of PipelineSecurityPolicy.
type PipelineSecurityPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PipelineSecurityPolicy `json:"items"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=psr
// +kubebuilder:printcolumn:name="Compliance",type=string,JSONPath=`.status.complianceState`
// +kubebuilder:printcolumn:name="Repository",type=string,JSONPath=`.status.source.repository`

// PipelineSecurityReport represents a security evaluation of a CI/CD pipeline source.
type PipelineSecurityReport struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Status            PipelineSecurityReportStatus `json:"status"`
}

// PipelineSecurityReportStatus defines the observed state of PipelineSecurityReport.
type PipelineSecurityReportStatus struct {
	ComplianceState     string            `json:"complianceState"`            // Compliant, Warning, NonCompliant, Critical
	LastEvaluated       metav1.Time       `json:"lastEvaluated"`
	Source              PipelineSourceRef `json:"source"`
	Summary             FindingSummary    `json:"summary"`
	CorrelatedWorkloads []WorkloadRef     `json:"correlatedWorkloads,omitempty"`
	Findings            []Finding         `json:"findings,omitempty"`
}

// PipelineSourceRef identifies the CI/CD source that was evaluated.
type PipelineSourceRef struct {
	Repository   string `json:"repository"`
	Branch       string `json:"branch"`
	WorkflowPath string `json:"workflowPath,omitempty"`
}

// FindingSummary provides a count of findings by severity.
type FindingSummary struct {
	Critical int `json:"critical"`
	High     int `json:"high"`
	Medium   int `json:"medium"`
	Low      int `json:"low"`
	Info     int `json:"info"`
	Total    int `json:"total"`
}

// WorkloadRef identifies a Kubernetes workload correlated with a pipeline source.
type WorkloadRef struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// Finding represents a single security finding from a pipeline scan.
type Finding struct {
	Rule        string `json:"rule"`
	Severity    string `json:"severity"`
	File        string `json:"file"`
	Line        int    `json:"line"`
	Message     string `json:"message"`
	Remediation string `json:"remediation,omitempty"`
}

// +kubebuilder:object:root=true

// PipelineSecurityReportList contains a list of PipelineSecurityReport.
type PipelineSecurityReportList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PipelineSecurityReport `json:"items"`
}
