# Architecture

kaddons uses a **Plan-and-Execute** architecture that separates deterministic data collection from LLM-based analysis. This design ensures repeatable addon detection while using the LLM only where human-like interpretation is needed — reading unstructured compatibility pages.

## Pipeline overview

```
┌─────────────────────────────────────────────────────────────────────┐
│                      PLAN-AND-EXECUTE PIPELINE                      │
├──────────────────┬──────────────────────────────────────────────────┤
│ Phase 1          │ Discovery (deterministic)                        │
│ Phase 2          │ Enrichment (deterministic)                       │
│ Phase 3          │ Analysis (single LLM call)                       │
└──────────────────┴──────────────────────────────────────────────────┘
```

Phases 1 and 2 are fully deterministic — the same cluster state always produces the same set of addons and fetched pages. The LLM is only used in Phase 3.

## Phase 1: Discovery

Deterministic cluster interrogation via `kubectl`. No LLM involved.

**Cluster version detection** (`internal/cluster/cluster.go:GetClusterVersion`):
- Runs `kubectl version --output=json`
- Extracts `serverVersion.major` and `serverVersion.minor`
- Returns a `major.minor` string (e.g., `"1.30"`)
- Can be overridden with `--cluster` (`-c`) flag

**Workload discovery** (`internal/cluster/cluster.go:ListInstalledAddons`):
- Queries five Kubernetes resource types:

| Resource | Source label | CRD? |
|----------|-------------|------|
| Deployments | `deployment` | No |
| DaemonSets | `daemonset` | No |
| StatefulSets | `statefulset` | No |
| Flux HelmReleases | `helmrelease` | Yes (skipped if CRD missing) |
| ArgoCD Applications | `argocd-app` | Yes (skipped if CRD missing) |

**Addon name extraction** — label priority order:

1. `app.kubernetes.io/name` label
2. `meta.helm.sh/release-name` annotation
3. `helm.sh/chart` label (version suffix stripped)
4. `metadata.name` (fallback)

**Version extraction** — tried in order:

1. `app.kubernetes.io/version` label
2. `helm.sh/chart` label (version suffix extracted)
3. First container image tag

**Filtering** — applied after discovery:
- `--namespace` restricts which namespaces are queried
- `--addons` filters by addon name after database matching

## Phase 2: Enrichment

Deterministic data fetching. No LLM involved.

### Database matching

Each detected workload is matched against the embedded addon database (668 addons) using a six-pass algorithm (`internal/addon/addon.go:LookupAddon`):

| Pass | Strategy | Example |
|------|----------|---------|
| 0 | Alias resolution | `nodelocaldns` → `NodeLocal DNSCache` |
| 1 | Exact case-insensitive match | `istio` → `Istio` |
| 2 | Normalize (hyphens→spaces, amazon→aws) + exact | `amazon-vpc-cni` → `AWS VPC CNI` |
| 3 | Strip role suffix + exact | `ebs-csi-node` → `AWS EBS CSI Driver` |
| 4 | Forward prefix (DB starts with detected) | `cert` → `cert-manager`, `cert-manager-csi-driver` |
| 5 | Reverse prefix (detected starts with DB) | `prometheus-operator` → `Prometheus` |
| 6 | Word-subset (all words of core appear in DB) | `node-exporter` → `Prometheus Node Exporter` |

Names shorter than 4 characters skip fuzzy matching (passes 4-6) to avoid false positives.

Unmatched workloads are silently dropped — they are application workloads, not known addons.

### Deduplication

When multiple workloads resolve to the same addon (e.g., `ebs-csi-node` and `ebs-csi-controller` both match `AWS EBS CSI Driver`), the entry with a version is preferred.

### Compatibility page fetching

For each matched addon with a `compatibility_matrix_url`:

1. **GitHub URLs** are converted to `raw.githubusercontent.com` equivalents (`internal/fetch/fetch.go:GitHubRawURL`), fetching raw Markdown that preserves tables, headers, and lists for the LLM
2. **Non-GitHub URLs** are fetched as HTML and stripped of tags (collapsed to text)
3. Results are cached by URL (if two addons share the same page, it's fetched once)
4. Content is truncated to 30KB

GitHub URL conversion patterns:

| Input | Output |
|-------|--------|
| `github.com/{owner}/{repo}` | `raw.githubusercontent.com/{owner}/{repo}/HEAD/README.md` |
| `github.com/{owner}/{repo}/tree/{ref}/{path}` | `raw.githubusercontent.com/{owner}/{repo}/{ref}/{path}/README.md` |
| `github.com/{owner}/{repo}/blob/{ref}/{path}` | `raw.githubusercontent.com/{owner}/{repo}/{ref}/{path}` |
| Wiki, release, non-GitHub URLs | Unchanged |

### EOL data fetching

Addons with a known mapping in `eolProductSlugs` (currently covering Argo CD, Calico, cert-manager, Cilium, Containerd, Contour, Envoy, etcd, Flux, Gatekeeper, Grafana, Harbor, Istio, KEDA, Kuma, Kyverno, Prometheus, Traefik, Redis, and Kubernetes itself) get supplementary lifecycle data from the [endoflife.date](https://endoflife.date) API.

This provides EOL dates, latest versions, and support status per release cycle.

## Phase 3: Analysis

A single Gemini API call receives all pre-collected data as JSON:

```json
{
  "k8s_version": "1.30",
  "addons": [
    {
      "name": "cert-manager",
      "namespace": "cert-manager",
      "version": "v1.14.2",
      "source": "deployment",
      "db_match": { "name": "cert-manager", "compatibility_matrix_url": "..." },
      "compatibility_content": "... (fetched page content) ...",
      "compatibility_url": "https://cert-manager.io/docs/releases/",
      "eol_data": [{ "cycle": "1.14", "eol": "2025-09-10", "latest": "1.14.7" }]
    }
  ]
}
```

The system prompt instructs the LLM to:
- Return a JSON array with `compatible` as `"true"`, `"false"`, or `"unknown"` (strings, not booleans)
- Cite the source URL in every `note` field
- Include supported-until dates from EOL data when available
- Return `"unknown"` rather than guessing when data is unclear
- Include every addon from the input (no filtering)

### Response processing

1. Text is extracted from the Gemini response
2. Markdown code fences are stripped (`extractJSON`)
3. JSON is deserialized into `[]AddonCompatibility`
4. Custom `Status.UnmarshalJSON` handles LLM non-compliance: boolean `true` → `"true"`, `null` → `"unknown"`, garbage → `"unknown"`

## Database validation tool

`kaddons-validate` (`cmd/kaddons-validate`) is a separate binary for CI and development — it is not a subcommand of `kaddons` and is not distributed with releases.

Operates on the embedded addon database only — no cluster access or API key needed.

Uses a URL-centric pipeline that processes unique URLs (not per-addon items), guaranteeing one HTTP request per unique URL regardless of how many addons reference it.

**Pipeline:**

1. **Harvest** — extracts every URL from all addon fields (`project_url`, `repository`, `compatibility_matrix_url`, `changelog_location`)
2. **Aggregate** — builds `map[string]*urlTask` keyed by URL. Each task tracks which addons/fields reference it and whether it needs content validation (`needsContent` is true if any addon uses it as `compatibility_matrix_url`)
3. **Execute** — 10-worker pool iterates unique URLs only. Content URLs use `fetch.CompatibilityPageWithClient`; link-only URLs use HEAD with GET fallback
4. **Report** — maps results back to addons for two output tables

**Flag semantics:**

| Flag | Behavior |
|------|----------|
| (none) | Both checks run |
| `--links` | Downgrade all tasks to HEAD-only (skip body fetch) |
| `--matrix` | Remove non-matrix tasks (only process `compatibility_matrix_url` entries) |

Flags are mutually exclusive.

**Reporting:**

- **Table 1: Unreachable URLs** — network/HTTP errors for any field
- **Table 2: Missing K8s matrix data** — HTTP 200 but regex fails, reported only for `compatibility_matrix_url` consumers

A URL returning 200 but failing regex appears only in Table 2. An unreachable matrix URL appears only in Table 1 (network failure trumps content check).

**Content validation heuristic** — requires **both**:
- A K8s version pattern (e.g., `Kubernetes 1.28`)
- A matrix keyword (e.g., `supported versions`, `compatibility matrix`)

## File map

```
cmd/kaddons/
  main.go                             CLI entrypoint (Cobra), flag parsing
cmd/kaddons-validate/
  main.go                             DB validation tool (dev/CI only, not distributed)

internal/
  addon/
    addon.go                          Embedded addon DB, 6-pass matching, EOL slug mapping
    addon_test.go                     Matching, normalization, EOL tests
    k8s_universal_addons.json         668-addon database (embedded via go:embed)
  agent/
    agent.go                          Plan-and-Execute pipeline: discovery → enrichment → LLM analysis
  cluster/
    cluster.go                        kubectl interaction, version detection, workload discovery
    cluster_test.go                   Chart version, image tag extraction tests
  fetch/
    fetch.go                          HTTP fetching, GitHub raw URL conversion, EOL data
    fetch_test.go                     GitHub URL conversion tests
  output/
    output.go                         JSON/table formatting, Status type, JSON extraction
    output_test.go                    Status round-trip, JSON backward compat tests
  validate/
    validate.go                       URL reachability + matrix content validation library
    validate_test.go                  URL check, matrix detection, aggregation, flag tests
```
