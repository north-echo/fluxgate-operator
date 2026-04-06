package analyzer

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	v1alpha1 "github.com/north-echo/fluxgate-operator/api/v1alpha1"
	"github.com/north-echo/fluxgate-operator/internal/connector"
	"github.com/north-echo/fluxgate/pkg/scanner"
)

// Analyzer scans pipeline sources for security findings using the Fluxgate engine.
type Analyzer struct {
	github   *GitHubClient
	mu       sync.RWMutex
	cache    map[string]cachedResult
	cacheTTL time.Duration
}

// cachedResult stores scan results with a timestamp for cache expiry.
type cachedResult struct {
	findings  []v1alpha1.Finding
	fetchedAt time.Time
}

// NewAnalyzer creates a new Analyzer with the given GitHub token and cache TTL.
func NewAnalyzer(githubToken string, cacheTTL time.Duration) *Analyzer {
	return &Analyzer{
		github: &GitHubClient{
			Token: githubToken,
		},
		cache:    make(map[string]cachedResult),
		cacheTTL: cacheTTL,
	}
}

// Analyze scans the workflow files in the given pipeline source and returns findings.
func (a *Analyzer) Analyze(ctx context.Context, src connector.PipelineSource) ([]v1alpha1.Finding, error) {
	cacheKey := fmt.Sprintf("%s/%s@%s", src.Owner, src.Repo, src.Branch)

	// Check cache
	a.mu.RLock()
	if cached, ok := a.cache[cacheKey]; ok {
		if time.Since(cached.fetchedAt) < a.cacheTTL {
			a.mu.RUnlock()
			return cached.findings, nil
		}
	}
	a.mu.RUnlock()

	// Fetch workflow files from GitHub
	files, err := a.github.FetchWorkflowFiles(ctx, src.Owner, src.Repo, src.Branch)
	if err != nil {
		return nil, fmt.Errorf("fetching workflows for %s/%s: %w", src.Owner, src.Repo, err)
	}

	var allFindings []v1alpha1.Finding
	for _, file := range files {
		path := fmt.Sprintf(".github/workflows/%s", file.Name)

		scanFindings, err := scanner.ScanWorkflowBytes(file.Content, path, scanner.ScanOptions{})
		if err != nil {
			continue // skip unparseable workflows
		}

		for _, sf := range scanFindings {
			finding := convertFinding(sf)
			allFindings = append(allFindings, finding)
		}
	}

	// Cache results
	a.mu.Lock()
	a.cache[cacheKey] = cachedResult{
		findings:  allFindings,
		fetchedAt: time.Now(),
	}
	a.mu.Unlock()

	return allFindings, nil
}

// convertFinding converts a scanner.Finding to the CRD v1alpha1.Finding type.
func convertFinding(sf scanner.Finding) v1alpha1.Finding {
	remediation := ""
	if len(sf.Mitigations) > 0 {
		remediation = strings.Join(sf.Mitigations, "; ")
	}

	return v1alpha1.Finding{
		Rule:        sf.RuleID,
		Severity:    sf.Severity,
		File:        sf.File,
		Line:        sf.Line,
		Message:     sf.Message,
		Remediation: remediation,
	}
}

// ComputeComplianceState determines the compliance state from finding severity counts.
func ComputeComplianceState(findings []v1alpha1.Finding) (string, v1alpha1.FindingSummary) {
	summary := v1alpha1.FindingSummary{}
	for _, f := range findings {
		summary.Total++
		switch f.Severity {
		case "critical":
			summary.Critical++
		case "high":
			summary.High++
		case "medium":
			summary.Medium++
		case "low":
			summary.Low++
		case "info":
			summary.Info++
		}
	}

	state := "Compliant"
	if summary.Critical > 0 {
		state = "Critical"
	} else if summary.High > 0 {
		state = "NonCompliant"
	} else if summary.Medium > 0 {
		state = "Warning"
	}

	return state, summary
}
