# Addon database

The addon database is an embedded JSON file (`internal/addon/k8s_universal_addons.json`) compiled into the binary at build time via `go:embed`. It contains 668 Kubernetes addons with metadata used for matching, compatibility analysis, and URL checking.

## Schema

```json
{
  "addons": [
    {
      "name": "cert-manager",
      "project_url": "https://cert-manager.io",
      "repository": "https://github.com/cert-manager/cert-manager",
      "compatibility_matrix_url": "https://cert-manager.io/docs/releases/",
      "changelog_location": "https://github.com/cert-manager/cert-manager/releases"
    }
  ],
  "metadata": {}
}
```

### Fields

| Field | Required | Description |
|-------|----------|-------------|
| `name` | Yes | Display name used for matching and output |
| `project_url` | Yes | Official project homepage |
| `repository` | Yes | Source code repository |
| `compatibility_matrix_url` | Yes | Page containing K8s version compatibility data |
| `changelog_location` | Yes | Release notes or changelog URL |
| `kubernetes_compatibility` | No | Map of addon version → supported K8s versions (enables stored-data resolution without LLM) |
| `kubernetes_min_version` | No | Minimum supported K8s version (floor check fallback) |
| `kubernetes_max_version` | No | Maximum supported K8s version (ceiling check fallback) |

## Matching algorithm

When kaddons discovers a workload in the cluster, it attempts to match the workload name against the database using a six-pass algorithm. See [architecture.md](architecture.md) for full details on each pass.

The matching is designed to handle real-world naming inconsistencies:
- Helm charts use hyphens (`cert-manager`), DB uses spaces (`Cert Manager`)
- AWS EKS addons use `amazon-` prefix, DB uses `AWS` prefix
- Sub-components include role suffixes (`ebs-csi-node`, `redis-master`)
- Partial names need to resolve (`node-exporter` → `Prometheus Node Exporter`)

## EOL data integration

A subset of addons have mappings to [endoflife.date](https://endoflife.date) product slugs in `internal/addon/addon.go:eolProductSlugs`. These currently cover:

- Argo CD, Argo Workflows
- Calico
- cert-manager
- Cilium (including Hubble, ClusterMesh, Service Mesh)
- Containerd, Contour, Envoy
- etcd, Flux, Gatekeeper
- Grafana (including Loki, Mimir, Tempo, Alloy, OnCall, Pyroscope)
- Harbor, Istio, KEDA, Kuma, Kyverno
- Prometheus (including Adapter, Pushgateway, Blackbox Exporter)
- Redis, Traefik
- kube-proxy (maps to Kubernetes release cycle)

When a matched addon has an EOL slug, kaddons fetches lifecycle data from the endoflife.date API. This provides support dates, latest versions, and EOL status per release cycle — used by the LLM when configured, and included in local-only notes when no API key is present.

## GitHub URL handling

About one-third of the 668 addons have GitHub URLs as their `compatibility_matrix_url`. These URLs are automatically converted to `raw.githubusercontent.com` equivalents at fetch time, returning raw Markdown instead of rendered HTML.

This preserves Markdown tables, headers, and lists that the LLM can parse directly, rather than receiving flat text with all structure stripped.

The conversion is transparent — the database stores the human-readable GitHub URL, and the fetch layer handles the conversion. See [architecture.md](architecture.md) for the conversion rules.

## Adding a new addon

To add an addon to the database:

1. Edit `internal/addon/k8s_universal_addons.json` and add an entry to the `addons` array
2. Fill in all five fields — the most important is `compatibility_matrix_url`, which should point to a page with actual K8s version compatibility data (not a generic README)
3. Prefer these URL sources in order:
   - Dedicated compatibility or prerequisites page in official docs
   - Helm chart README with K8s version requirements
   - GitHub repo README with a specific compatibility section
   - A `compatibility.md` or `COMPATIBILITY.md` file
4. Run `go build ./...` to verify the JSON parses correctly
5. Run `make validate` to verify URLs are reachable and compatibility pages contain K8s version data
6. Run `go test -v ./...` to verify all tests pass

If the addon is tracked on [endoflife.date](https://endoflife.date), add a slug mapping to `eolProductSlugs` in `internal/addon/addon.go`.

## Quality checks

The `kaddons-validate` tool (`cmd/kaddons-validate`) audits the database. It is a separate binary for development and CI — not a subcommand of `kaddons`.

- `make validate` — checks all URLs for reachability and validates compatibility matrix pages contain K8s version data
- `go run ./cmd/kaddons-validate --links` — only URL reachability checks
- `go run ./cmd/kaddons-validate --matrix` — only compatibility matrix content validation

The content heuristic requires both a version pattern (e.g., "Kubernetes 1.28") and a keyword (e.g., "supported versions").

A [GitHub Actions workflow](../.github/workflows/linkcheck.yml) runs `go run ./cmd/kaddons-validate --links` weekly and creates issues for broken links.
