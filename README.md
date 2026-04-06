# Fluxgate Operator

**Continuous CI/CD Pipeline Security Posture for Kubernetes and OpenShift**

Fluxgate Operator is a Kubernetes operator that continuously monitors and enforces the security posture of CI/CD pipeline configurations connected to your cluster. It watches in-cluster CI/CD resources (ArgoCD Applications, Flux GitRepositories/Kustomizations) and evaluates their upstream workflow files against the [Fluxgate](https://github.com/north-echo/fluxgate) detection engine.

Every production cluster has deployment pipelines feeding into it. No existing tool continuously evaluates whether those pipelines themselves are securely configured. Fluxgate Operator fills that gap.

## Key Capabilities

- **53 detection rules** across GitHub Actions, GitLab CI, Azure Pipelines, Jenkins, Tekton, CircleCI — powered by the [Fluxgate scanner](https://github.com/north-echo/fluxgate)
- **ArgoCD and Flux native** — discovers Applications and GitRepositories, correlates findings with downstream workloads
- **Workload correlation** — maps pipeline vulnerabilities to the Deployments they deploy into your cluster
- **Policy-driven enforcement** — configurable thresholds with annotate, alert (Slack/webhook), suspend-sync, and label-workloads actions
- **Kubernetes-native reporting** — findings stored as `PipelineSecurityReport` CRDs, queryable via `kubectl`
- **Prometheus metrics** — compliance state, finding counts, enforcement actions, API rate limits
- **Kubernetes Events** — `PipelineFindingDetected`, `ComplianceStateChanged`, `SyncSuspended`, `SyncResumed`

## How It Works

```
ArgoCD Application ──┐
                     ├── DiscoveryController ──► AnalysisController ──► PipelineSecurityReport
Flux GitRepository ──┘        │                       │                        │
                              │                       │                        │
                         Extract source          Fetch workflows          PolicyController
                         repo/branch             Scan with Fluxgate           │
                                                                         Enforce policy
                                                                    (annotate/alert/suspend)
```

1. **Discover** — DiscoveryController watches ArgoCD Applications and Flux GitRepositories. Extracts upstream repository URL, branch, and path.
2. **Analyze** — AnalysisController fetches workflow files via GitHub API and scans them with the Fluxgate rule engine.
3. **Report** — Creates `PipelineSecurityReport` CRDs with findings, compliance state, and correlated workloads.
4. **Enforce** — PolicyController evaluates reports against `PipelineSecurityPolicy` thresholds and triggers enforcement actions.

## Quick Start

```bash
# Install CRDs
kubectl apply -f config/crd/

# Create a secret with your GitHub token
kubectl create secret generic fluxgate-github-token \
  --from-literal=token=ghp_your_token_here \
  -n fluxgate-system

# Deploy the operator
kubectl apply -f config/manager/

# Create a security policy
kubectl apply -f - <<EOF
apiVersion: fluxgate.north-echo.dev/v1alpha1
kind: PipelineSecurityPolicy
metadata:
  name: production-pipelines
  namespace: default
spec:
  selector:
    cicdKinds:
      - Application
      - GitRepository
  thresholds:
    maxCritical: 0
    maxHigh: 2
    requirePinnedActions: true
  evaluationInterval: 30m
  enforcement:
    - action: annotate
      trigger: Warning
    - action: alert
      trigger: NonCompliant
      alertTarget:
        type: slack
        channel: "#pipeline-security"
    - action: suspendSync
      trigger: Critical
      gracePeriod: 4h
EOF
```

## CRDs

### PipelineSecurityPolicy

Declares acceptable CI/CD security thresholds and enforcement behavior:

| Field | Description |
|-------|-------------|
| `spec.selector.cicdKinds` | Which CI/CD resource types to evaluate (Application, GitRepository, Pipeline) |
| `spec.selector.matchLabels` | Label selector for filtering resources |
| `spec.thresholds.maxCritical` | Maximum allowed critical findings (0 = zero tolerance) |
| `spec.thresholds.maxHigh` | Maximum allowed high findings |
| `spec.thresholds.requirePinnedActions` | Require SHA-pinned action references |
| `spec.enforcement[].action` | annotate, alert, suspendSync, labelWorkloads |
| `spec.enforcement[].trigger` | Warning, NonCompliant, Critical |
| `spec.ruleOverrides` | Per-rule severity adjustments |

### PipelineSecurityReport

Operator-managed resource representing the security evaluation of a CI/CD source:

```bash
kubectl get pipelinesecurityreports -A
NAMESPACE   NAME                    STATE          CRITICAL  HIGH  MEDIUM  AGE
default     payment-service-app     NonCompliant   1         2     3       5m
default     frontend-app            Compliant      0         0     1       5m
```

```bash
kubectl describe pipelinesecurityreport payment-service-app
```

## Detection Rules

Fluxgate Operator uses the [Fluxgate v0.7.0](https://github.com/north-echo/fluxgate) detection engine with 53 rules:

| Platform | Rules | Examples |
|----------|------:|---------|
| GitHub Actions | 23 | Pwn Request, Script Injection, OIDC Misconfiguration, Cache Poisoning |
| GitLab CI | 9 | MR Secrets, Script Injection, Unsafe Includes, Cache Poisoning |
| Azure Pipelines | 9 | PR Secrets, Script Injection, Unpinned Templates |
| Jenkins | 4 | PR Secrets, Script Injection, Unpinned Libraries |
| Tekton | 4 | Param Injection, Unpinned Tasks, Privileged Steps |
| CircleCI | 4 | Fork PR Exec, Script Injection, Unpinned Orbs |

## Enforcement Actions

| Action | Behavior | Reversible |
|--------|----------|:----------:|
| `annotate` | Adds compliance-state annotation to the CI/CD resource | Yes |
| `alert` | Sends Slack or webhook notification | N/A |
| `suspendSync` | Pauses ArgoCD auto-sync or Flux reconciliation | Yes |
| `labelWorkloads` | Adds `pipeline-risk` label to correlated Deployments | Yes |

When compliance is restored, the operator automatically removes annotations, resumes sync, and removes risk labels.

## Prometheus Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `fluxgate_pipeline_compliance_state` | Gauge | Compliance state per resource (0-3) |
| `fluxgate_findings_total` | Gauge | Findings by rule and severity |
| `fluxgate_pipelines_discovered_total` | Gauge | Discovered CI/CD resources by connector |
| `fluxgate_enforcement_actions_total` | Counter | Enforcement actions taken |
| `fluxgate_evaluation_duration_seconds` | Histogram | Evaluation time per source |
| `fluxgate_github_api_requests_total` | Counter | GitHub API usage |
| `fluxgate_github_api_rate_remaining` | Gauge | Remaining rate limit |

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                  Kubernetes Cluster                   │
│                                                       │
│  ┌──────────┐    ┌──────────┐    ┌──────────────┐    │
│  │ ArgoCD   │    │  Flux    │    │   Tekton     │    │
│  │ App CRDs │    │ GitRepo  │    │  Pipeline    │    │
│  └────┬─────┘    └────┬─────┘    └──────────────┘    │
│       │               │                               │
│       └───────┬───────┘                               │
│               ▼                                       │
│  ┌─────────────────────────────────┐                  │
│  │     Fluxgate Operator           │                  │
│  │                                 │                  │
│  │  DiscoveryController           │                  │
│  │       ↓                        │                  │
│  │  AnalysisController            │──► GitHub API    │
│  │       ↓        (Fluxgate       │    (fetch        │
│  │  ReportController  pkg/scanner)│     workflows)   │
│  │       ↓                        │                  │
│  │  PolicyController              │                  │
│  │    (enforce)                   │                  │
│  └────────┬────────────────────────┘                  │
│           ▼                                           │
│  ┌─────────────────┐  ┌──────────────────┐           │
│  │ PipelineSecurity │  │ PipelineSecurity │           │
│  │ Report CRDs      │  │ Policy CRDs      │           │
│  └─────────────────┘  └──────────────────┘           │
└─────────────────────────────────────────────────────┘
```

## Relationship to Fluxgate CLI

| | Fluxgate CLI | Fluxgate Operator |
|---|---|---|
| Interface | Command-line tool | Kubernetes operator |
| Input | Local files or GitHub API | In-cluster CI/CD CRDs |
| Output | JSON/SARIF/table | PipelineSecurityReport CRDs |
| Continuous | No (one-shot scan) | Yes (continuous re-evaluation) |
| Workload correlation | No | Yes (maps pipeline → Deployment) |
| Enforcement | No | Yes (suspend sync, alert, label) |
| Use case | CI/CD integration, research | Production cluster security posture |

This is the same relationship as [Trivy](https://github.com/aquasecurity/trivy) (CLI) and [Trivy Operator](https://github.com/aquasecurity/trivy-operator) (Kubernetes operator).

## Development

```bash
# Build
make build

# Run locally (uses kubeconfig)
make run

# Run tests
make test

# Build container
make docker-build

# Install CRDs
make install
```

## Roadmap

| Version | Scope |
|---------|-------|
| **v0.1.0** | ArgoCD + Flux connectors, analysis engine, annotate enforcement |
| v0.2.0 | PolicyController enforcement (suspendSync, alerts), Tekton connector |
| v0.3.0 | Prometheus metrics, Grafana dashboard, event enrichment |
| v0.4.0 | Helm chart, OLM bundle, OperatorHub submission |
| v1.0.0 | Stable API, comprehensive docs, CNCF Sandbox consideration |

## License

Apache License 2.0

## Acknowledgements

Built on the [Fluxgate](https://github.com/north-echo/fluxgate) CI/CD security scanner. Detection coverage informed by comparative analysis of [Poutine](https://github.com/boostsecurityio/poutine) (BoostSecurity) and [zizmor](https://github.com/zizmorcore/zizmor) (Trail of Bits).
