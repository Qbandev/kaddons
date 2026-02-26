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
| `--output` | `-o` | `json` | Output format. Must be `json` or `html`. |
| `--output-path` | | `./kaddons-report.html` | Output file path used when `--output html` is selected. |
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

### HTML

Activated with `-o html`. Writes a styled report file to `./kaddons-report.html` by default, or to the `--output-path` location.

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

The `Done: ...` summary is printed as the final stderr line after output is fully written.

Gemini analysis is executed one addon at a time in deterministic sorted order under the same `Analyzing with ...` stage.

This keeps stdout clean for piping JSON output to other tools:

```bash
kaddons | jq '.addons[] | select(.compatible == "false")'
```

## Retry and timeout policy

All external calls use a shared deterministic retry policy (`internal/resilience`):

- **Gemini calls**: 3 attempts, per-attempt timeout 90s, backoff 1s then 2s
- **HTTP fetch/EOL/validate calls**: 3 attempts, backoff 500ms then 1s
- **kubectl calls**: 3 attempts, backoff 500ms then 1s

Retryable conditions include transient transport errors (`timeout`, `EOF`, connection resets), plus HTTP `429` and `5xx`.
