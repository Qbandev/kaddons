# Architecture

kaddons uses a **Plan-and-Execute** architecture that separates deterministic data collection from LLM-based analysis. This design ensures repeatable addon detection while using the LLM only where human-like interpretation is needed — reading unstructured compatibility pages.

## Pipeline overview

```
┌─────────────────────────────────────────────────────────────────────┐
│                      PLAN-AND-EXECUTE PIPELINE                      │
├──────────────────┬──────────────────────────────────────────────────┤
│ Phase 1          │ Discovery (deterministic)                        │
│ Phase 2          │ Enrichment + stored resolution + table extraction │
│ Phase 3          │ Runtime analysis (LLM only when needed)          │
└──────────────────┴──────────────────────────────────────────────────┘
```

Phases 1 and 2 are fully deterministic — the same cluster state always produces the same matched addons and stored-data verdict candidates. Phase 2 also attempts deterministic table extraction from fetched compatibility pages. The LLM is only used in Phase 3 for addons unresolved by stored data and extraction.

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

## Phase 2: Enrichment and stored-data resolution

Deterministic enrichment. No LLM involved.

### Database matching

Each detected workload is matched against the embedded addon database (668 addons) using a seven-pass algorithm (`internal/addon/addon.go:LookupAddon`):

| Pass | Strategy | Example |
|------|----------|---------|
| 0 | Alias resolution | `nodelocaldns` → `NodeLocal DNSCache` |
| 1 | Exact case-insensitive match | `istio` → `Istio` |
| 2 | Normalize (hyphens→spaces, amazon→aws) + exact | `amazon-vpc-cni` → `AWS VPC CNI` |
| 3 | Strip role suffix + exact | `ebs-csi-node` → `AWS EBS CSI Driver` |
| 4 | Forward prefix (DB starts with detected) | `cert` → `cert-manager`, `cert-manager-csi-driver` |
| 5 | Reverse prefix (detected starts with DB) | `prometheus-operator` → `Prometheus` |
| 6 | Word-subset (all words of core appear in DB) | `node-exporter` → `Prometheus Node Exporter` |
| 7 | Levenshtein fuzzy match (distance ≤ 2, < 25% of name length) | `cert-manger` → `cert-manager` |

Names shorter than 4 characters skip fuzzy matching (passes 4-7) to avoid false positives. Pass 7 additionally requires both the detected and DB names to be at least 6 characters.

Unmatched workloads are silently dropped — they are application workloads, not known addons.

### Deduplication

When multiple workloads resolve to the same addon (e.g., `ebs-csi-node` and `ebs-csi-controller` both match `AWS EBS CSI Driver`), the entry with a version is preferred.

### Stored compatibility resolution

Before any network fetches, the agent resolves addon compatibility from embedded database fields when possible (`internal/agent/agent.go:resolveFromStoredData`):

- `kubernetes_compatibility` matrix keys are matched deterministically against installed addon versions
- `kubernetes_min_version` is used as a floor check when matrix keys are absent
- direct matrix-key selection is deterministic (most specific match wins)
- key handling includes exact/prefix semver, `.x` wildcards (case-insensitive), ranges (`A-B`), and threshold/floor forms (`>=`, `+`)

Stored verdicts are emitted immediately with `data_source="stored"`, and only unresolved addons continue to runtime fetching/LLM analysis.

### Runtime compatibility page fetching

For each addon that still requires runtime analysis and has a `compatibility_matrix_url`:

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

### Deterministic table extraction

After fetching compatibility pages and before LLM analysis, the agent attempts deterministic extraction of K8s compatibility matrices from the fetched content (`internal/extract/table.go`). This works without any LLM:

1. **GitHub raw content** (Markdown) is parsed for `|`-delimited tables
2. **Non-GitHub content** (HTML) is parsed for `<table>` elements using regex-based extraction

Two extraction strategies are applied:

- **Version-header strategy**: Column headers contain K8s version strings directly (e.g., `1.28`, `1.29`). Non-empty cells indicate support.
- **Labeled-column strategy**: Headers contain labels like "Kubernetes Version" and "Addon Version". Data rows contain version strings.

Extracted versions are validated: K8s versions must match `1.\d+`, addon versions must match semver-like patterns. If extraction produces a valid matrix, the addon is resolved with `data_source="extracted"` and does not proceed to LLM analysis.

Extraction failure (malformed table, no matching columns, validation failure) is not an error — it falls through silently to the LLM/local path. Tables exceeding 1000 cells are discarded entirely (not truncated) to prevent incomplete matrices from producing incorrect verdicts.

### EOL data fetching

EOL slug resolution uses a runtime catalog from [endoflife.date v1](https://endoflife.date/docs/api/v1/) (`/api/v1/products`) and matches addon names against product slug, label, and aliases. If runtime lookup fails, a static fallback alias map is used for irregular names.

This provides EOL dates, latest versions, and support status per release cycle.

## Phase 3: Runtime analysis

Gemini is called in a deterministic linear loop only for unresolved runtime addons: one addon per request, in sorted order.

For each addon, the agent builds a bounded structured payload:
- addon identity fields (`name`, `namespace`, `installed_version`)
- source fields (`compatibility_url`, `fetch_error`)
- deterministic compatibility evidence excerpt (keyword-based pruning with fixed line/byte caps)
- bounded EOL summary rows

The system prompt enforces a strict single-object response:
- `compatible` must be `"true"`, `"false"`, or `"unknown"` (string)
- `note` is mandatory and source-cited when URLs are available
- unknown is used when evidence is insufficient
- extra keys are rejected by schema settings

If per-addon analysis fails after bounded retries/timeouts, that addon is emitted as `compatible="unknown"` with an explanatory note so the run always completes without hanging.

Runtime verdicts are tagged with `data_source="llm"`. Deterministic extraction verdicts are tagged with `data_source="extracted"`.

### Local-only mode

When no Gemini API key is configured (`GEMINI_API_KEY` unset and `--key` not provided), Phase 3 skips LLM analysis entirely. Instead, addons that require runtime resolution receive:

- `compatible = "unknown"`, `data_source = "local"`
- A note built from available local data: EOL latest release info and the compatibility matrix URL from the database

Compatibility page HTTP fetches always run because the fetched content feeds deterministic table extraction (Phase 2), which does not require an LLM. Addons resolved by extraction receive `data_source="extracted"`. EOL data fetching also runs because it provides structured data (latest version, EOL status) useful without LLM interpretation. Only the remaining unresolved addons receive local-only results.

This allows `kaddons` to run to completion without any API key, producing deterministic stored-data results, deterministic extraction results, plus `"unknown"` local-only results for remaining unresolved addons.

### Response processing

1. Text is extracted from each Gemini response
2. Markdown code fences are stripped (`extractJSON`)
3. JSON is deserialized into `AddonCompatibility` and aggregated in deterministic order
4. Custom `Status.UnmarshalJSON` handles LLM non-compliance: boolean `true` → `"true"`, `null` → `"unknown"`, garbage → `"unknown"`
5. Final JSON/HTML output is rendered once, then summary is printed to stderr

## Database validation tool

`kaddons-validate` (`cmd/kaddons-validate`) is a separate binary for CI and development — it is not a subcommand of `kaddons` and is not distributed with releases.

Operates on the embedded addon database only — no cluster access or API key needed.

Uses a URL-centric pipeline that processes unique URLs (not per-addon items), guaranteeing one HTTP request per unique URL regardless of how many addons reference it.

**Pipeline:**

1. **Harvest** — extracts every URL from all addon fields (`project_url`, `repository`, `compatibility_matrix_url`, `changelog_location`)
2. **Aggregate** — builds `map[string]*urlTask` keyed by URL. Each task tracks which addons/fields reference it and whether it needs content validation (`needsContent` is true if any addon uses it as `compatibility_matrix_url`)
3. **Execute** — 10-worker pool iterates unique URLs only. Content URLs use `fetch.CompatibilityPageWithClient`; link-only URLs use HEAD with GET fallback
4. **Report** — maps results back to addons for link and matrix validation summaries

**Flag semantics:**

| Flag | Behavior |
|------|----------|
| (none) | Both checks run |
| `--stored-only` | Validate embedded stored fields only; no network calls |
| `--links` | Downgrade all tasks to HEAD-only (skip body fetch) |
| `--matrix` | Remove non-matrix tasks (only process `compatibility_matrix_url` entries) |

`--links` and `--matrix` are mutually exclusive. `--stored-only` cannot be combined with either.

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
cmd/kaddons-extract/
  main.go                             Matrix extraction tool: cache/manifest mode and --sync for CI-driven DB updates
cmd/kaddons-validate/
  main.go                             DB validation tool (dev/CI only, not distributed)

internal/
  addon/
    addon.go                          Embedded addon DB, 7-pass matching, EOL slug resolution (runtime+fallback)
    addon_test.go                     Matching, normalization, Levenshtein, EOL tests
    k8s_universal_addons.json         668-addon database (embedded via go:embed)
  agent/
    agent.go                          Plan-and-Execute pipeline: discovery → enrichment → extraction → LLM analysis
  cluster/
    cluster.go                        kubectl interaction, version detection, workload discovery
    cluster_test.go                   Chart version, image tag extraction tests
  extract/
    table.go                          Deterministic Markdown/HTML table extraction for K8s compatibility matrices
    table_test.go                     Table extraction tests (version headers, labeled columns, edge cases)
  fetch/
    fetch.go                          HTTP fetching, GitHub raw URL conversion, EOL data, FetchedPage
    fetch_test.go                     GitHub URL conversion tests
  resilience/
    retry.go                          Shared retry policy, deterministic backoff, retry classifiers
    retry_test.go                     Retry policy and retry behavior tests
  output/
    output.go                         JSON/HTML formatting, Status type, `data_source`, JSON extraction
    output_test.go                    Status round-trip, JSON backward compat tests
  validate/
    validate.go                       URL reachability + matrix content validation library
    validate_test.go                  URL check, matrix detection, aggregation, flag tests
```
