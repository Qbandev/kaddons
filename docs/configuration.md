# Configuration

kaddons is configured entirely through CLI flags and environment variables. There are no configuration files.

## Environment variables

| Variable | Description |
|----------|-------------|
| `GEMINI_API_KEY` | Gemini API key. Used when `--key` flag is not provided. Required for the main command; not needed for `kaddons-validate`. |

## Root command flags

```bash
kaddons [flags]
```

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--namespace` | `-n` | `""` | Filter workloads by Kubernetes namespace. Empty means all namespaces. |
| `--cluster` | `-c` | `""` | Override cluster version detection. Skips `kubectl version` call. Format: `1.30` |
| `--addons` | `-a` | `""` | Comma-separated addon name filter. Only matched addons with these names are analyzed. |
| `--key` | `-k` | `""` | Gemini API key. Overrides `GEMINI_API_KEY` env var. |
| `--model` | `-m` | `gemini-3-flash-preview` | Gemini model to use for compatibility analysis. |
| `--output` | `-o` | `json` | Output format. Must be `json` or `table`. |
| `--version` | | | Print version, commit hash, and build date. |

## Database validation tool

`kaddons-validate` is a separate binary for development and CI — it is not a subcommand of `kaddons`.

```bash
go run ./cmd/kaddons-validate              # Run both checks (default)
go run ./cmd/kaddons-validate --links      # Only reachability checks
go run ./cmd/kaddons-validate --matrix     # Only content validation
make validate                              # Shorthand
```

Validates addon database URLs. No cluster access or API key needed. Flags `--links` and `--matrix` are mutually exclusive.

- Exit `0`: all checks passed
- Exit `1`: validation failures found (Markdown table printed)
- Exit `2`: runtime error (can't load addon DB, flag parsing error)

## Output formats

### JSON

Default format (`-o json`). Returns a `CompatibilityReport` object:

```json
{
  "k8s_version": "1.30",
  "addons": [
    {
      "name": "cert-manager",
      "namespace": "cert-manager",
      "installed_version": "v1.14.2",
      "compatible": "true",
      "latest_compatible_version": "1.18",
      "note": "Source-cited explanation..."
    }
  ]
}
```

**Field definitions:**

| Field | Type | Description |
|-------|------|-------------|
| `k8s_version` | string | Cluster Kubernetes version (top-level) |
| `name` | string | Addon display name |
| `namespace` | string | Kubernetes namespace where the addon runs |
| `installed_version` | string | Version detected from cluster labels/images |
| `compatible` | string | `"true"`, `"false"`, or `"unknown"` |
| `latest_compatible_version` | string | Recommended version (omitted if not determined) |
| `note` | string | Source-cited explanation with URL and support dates |

The `compatible` field is always a JSON string, never a boolean or null. This is enforced by the `Status` type's custom `UnmarshalJSON` which normalizes LLM output.

### Table

Activated with `-o table`. Renders a Unicode box-drawing table to stdout.

Column mapping:

| Column | JSON field |
|--------|-----------|
| NAME | `name` |
| NAMESPACE | `namespace` |
| VERSION | `installed_version` |
| K8S | `k8s_version` (from top-level) |
| COMPATIBLE | `compatible` (`"true"` → `yes`, `"false"` → `NO`, `"unknown"` → `unknown`) |
| LATEST | `latest_compatible_version` |
| NOTE | `note` (truncated to 60 characters) |

## Progress output

Progress messages are written to stderr during execution:

```
Detecting cluster version...
Cluster version: 1.30
Discovered 47 workloads
Matched 12 known addons
Enriching 12 addons...
Analyzing with gemini-3-flash-preview...
Done: 8 compatible, 2 incompatible, 2 unknown
```

This keeps stdout clean for piping JSON output to other tools:

```bash
kaddons | jq '.addons[] | select(.compatible == "false")'
```
