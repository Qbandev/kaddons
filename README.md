# kaddons

Kubernetes addon compatibility checker. Works with any Kubernetes cluster (EKS, GKE, AKS, self-managed, etc.). Discovers addons installed in a cluster, cross-references them against a built-in database of 664 known addons, fetches their compatibility matrix pages, and uses Gemini AI to determine whether each addon is compatible with the cluster's Kubernetes version.

## Prerequisites

- `kubectl` configured with access to a Kubernetes cluster
- A [Gemini API key](https://aistudio.google.com/apikey)

### Install prerequisites

**macOS (Homebrew):**

```bash
brew install kubectl
```

**Linux:**

```bash
# kubectl (see https://kubernetes.io/docs/tasks/tools/install-kubectl-linux/)
curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"
sudo install -o root -g root -m 0755 kubectl /usr/local/bin/kubectl
```

## Install

### Homebrew (macOS/Linux)

```bash
brew install qbandev/tap/kaddons
```

### Go install

Requires Go 1.23+:

```bash
go install github.com/qbandev/kaddons@latest
```

### Build from source

```bash
git clone https://github.com/qbandev/kaddons.git
cd kaddons
make build
# Binary is at ./kaddons
```

### Move to PATH (optional)

```bash
sudo make install
# Installs to /usr/local/bin/kaddons
```

## Usage

```bash
# Set your Gemini API key
export GEMINI_API_KEY=your-key-here

# Scan all addons in the cluster (JSON output)
./kaddons

# Table output
./kaddons -o table

# Check specific addons
./kaddons --addons cert-manager,karpenter -o table

# Override cluster version (skip kubectl version call)
./kaddons --k8s-version 1.30 -o table

# Filter by namespace
./kaddons --namespace kube-system -o table
```

## Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--namespace` | | `""` (all) | Kubernetes namespace filter |
| `--k8s-version` | | `""` (auto-detect) | Kubernetes version override (e.g. 1.30) |
| `--addons` | | `""` (all matched) | Comma-separated addon name filter |
| `--api-key` | | `""` (uses `GEMINI_API_KEY`) | Gemini API key |
| `--model` | | `gemini-3-flash-preview` | Gemini model |
| `--output` | `-o` | `json` | Output format: `json` or `table` |

## Output

### JSON (default)

```json
[
  {
    "name": "cert-manager",
    "namespace": "cert-manager",
    "installed_version": "v1.14.2",
    "k8s_version": "1.30",
    "compatible": true,
    "latest_compatible_version": "1.18",
    "compatibility_source": "https://cert-manager.io/docs/releases/",
    "note": "Version 1.14 is compatible with Kubernetes 1.24 through 1.31."
  },
  {
    "name": "external-secrets",
    "namespace": "external-secrets",
    "installed_version": "v0.14.2",
    "k8s_version": "1.30",
    "compatible": false,
    "latest_compatible_version": "0.13.x",
    "compatibility_source": "https://external-secrets.io/latest/introduction/stability-support/",
    "note": "ESO 0.14.x guarantees support only for K8s 1.32."
  },
  {
    "name": "goldilocks",
    "namespace": "kube-system",
    "installed_version": "8.0.0",
    "k8s_version": "1.30",
    "compatible": null,
    "note": "The provided documentation does not contain a Kubernetes compatibility matrix."
  }
]
```

- `compatible: true` — addon version is confirmed compatible with the cluster version
- `compatible: false` — addon version is not compatible; check `latest_compatible_version`
- `compatible: null` — compatibility could not be determined (no matrix found, page unavailable, etc.)

### Table (`-o table`)

```
┌────────────────────┬──────────────────┬─────────────────────┬──────┬────────────┬─────────┬──────────────────────────────────────────────────────────────┐
│ NAME               │ NAMESPACE        │ VERSION             │ K8S  │ COMPATIBLE │ LATEST  │ NOTE                                                         │
├────────────────────┼──────────────────┼─────────────────────┼──────┼────────────┼─────────┼──────────────────────────────────────────────────────────────┤
│ cert-manager       │ cert-manager     │ v1.14.2             │ 1.30 │ yes        │ 1.18    │ Version 1.14 is compatible with Kubernetes 1.24 through 1... │
├────────────────────┼──────────────────┼─────────────────────┼──────┼────────────┼─────────┼──────────────────────────────────────────────────────────────┤
│ karpenter          │ karpenter        │ 1.0.5               │ 1.30 │ yes        │ 1.9.x   │ K8s 1.30 requires Karpenter >= 0.37. Version 1.0.5 is co... │
├────────────────────┼──────────────────┼─────────────────────┼──────┼────────────┼─────────┼──────────────────────────────────────────────────────────────┤
│ external-secrets   │ external-secrets │ v0.14.2             │ 1.30 │ NO         │ 0.13.x  │ ESO 0.14.x guarantees support only for K8s 1.32.             │
├────────────────────┼──────────────────┼─────────────────────┼──────┼────────────┼─────────┼──────────────────────────────────────────────────────────────┤
│ goldilocks         │ kube-system      │ 8.0.0               │ 1.30 │ unknown    │         │ No Kubernetes compatibility matrix found in documentation.    │
└────────────────────┴──────────────────┴─────────────────────┴──────┴────────────┴─────────┴──────────────────────────────────────────────────────────────┘
```

## How It Works

kaddons uses a **Plan-and-Execute** architecture with three phases:

```
Phase 1: Discovery (deterministic, no LLM)
  kubectl version                          -> cluster K8s version
  kubectl get deploy,ds,sts,helmrelease,app -> all workloads

Phase 2: Enrichment (deterministic, no LLM)
  Match workloads against 664-addon database
  Deduplicate by addon name (prefer entries with version info)
  Fetch compatibility matrix pages via HTTP

Phase 3: Analysis (single LLM call)
  Send all pre-collected data to Gemini
  LLM analyzes page content and returns compatibility verdicts
```

Phases 1 and 2 are fully deterministic — the same cluster state always produces the same set of addons and fetched pages. The LLM is only used in Phase 3 for interpreting compatibility matrix pages, which ensures consistent addon detection across runs. The LLM analysis may produce slightly different wording in notes between runs, but the compatibility verdicts are generally stable since they're grounded in fetched page content.

### Accuracy and Limitations

The LLM reads each addon's official compatibility matrix page and extracts version support information. It is instructed to return `null` rather than guess when the page doesn't contain clear compatibility data. Results should be treated as a **triage tool** — always verify critical compatibility decisions against the official documentation linked in `compatibility_source`.

### Addon Detection

Workloads are discovered from five Kubernetes resource types:

1. Deployments
2. DaemonSets
3. StatefulSets
4. Flux HelmReleases (skipped if CRD not installed)
5. ArgoCD Applications (skipped if CRD not installed)

Addon identity is extracted from labels in priority order:

1. `app.kubernetes.io/name`
2. `meta.helm.sh/release-name` (annotation)
3. `helm.sh/chart` (version suffix stripped)
4. `metadata.name` (fallback)

Version is extracted from: `app.kubernetes.io/version` label, `helm.sh/chart` suffix, or first container image tag.

Only workloads that match the built-in addon database are included in the report. Application workloads (your own services) are automatically filtered out.

### Addon Database

The embedded database (`k8s_universal_addons.json`) contains 664 Kubernetes addons with metadata:

- Project URL and repository
- Compatibility matrix URL (used to fetch version support data)
- Changelog location
- Upgrade path type (Helm-driven, manual manifest, etc.)

Covers 35 categories from the CNCF Landscape and non-CNCF ecosystem including ingress controllers, service meshes, monitoring, security, storage, CI/CD, and more.

## Project Structure

```
main.go       CLI entrypoint (Cobra), flag parsing, Gemini client init
agent.go      Plan-and-Execute pipeline: discovery, enrichment, LLM analysis
tools.go      Cluster interaction: kubectl commands, HTTP fetching, label parsing
addons.go     Embedded addon database (go:embed), lookup/matching logic
output.go     JSON and box-drawing table output formatting
```
