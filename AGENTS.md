# AGENTS.md

Coding agent instructions for the kaddons repository.

## Project overview

kaddons is a Go CLI tool that checks Kubernetes addon compatibility. It discovers addons running in a cluster, matches them against a 668-entry embedded database, and optionally uses Gemini LLM to analyze compatibility pages for addons without stored data. Works without an API key using local-only fallback.

## Architecture

Three-phase Plan-and-Execute pipeline:

1. **Discovery** (deterministic) — `kubectl` queries for cluster version and workloads
2. **Enrichment** (deterministic) — database matching, compatibility page fetching, EOL data
3. **Analysis** (optional LLM call) — Gemini interprets fetched pages and returns verdicts; falls back to local-only results when no API key

The LLM is only used in Phase 3. Phases 1 and 2 are fully deterministic.

## Key files

| File | Purpose |
|------|---------|
| `cmd/kaddons/main.go` | CLI entrypoint (Cobra), flags |
| `cmd/kaddons-validate/main.go` | DB validation tool (dev/CI only, not distributed) |
| `internal/agent/agent.go` | Pipeline orchestration, system prompt, LLM call |
| `internal/addon/addon.go` | Embedded addon DB (`go:embed`), 6-pass matching algorithm, EOL slugs |
| `internal/cluster/cluster.go` | kubectl interaction, workload discovery, version detection |
| `internal/fetch/fetch.go` | HTTP fetching, GitHub raw URL conversion, EOL data |
| `internal/output/output.go` | JSON/HTML output, `Status` tri-state type, `ExtractJSON` |
| `internal/validate/validate.go` | DB validation library — URL reachability + matrix content checks |
| `internal/addon/k8s_universal_addons.json` | 668-addon database (embedded at build time) |

## Language and stack

- **Go** (1.25.6) — single-binary CLI, no CGO
- **Dependencies**: `spf13/cobra` (CLI), `google.golang.org/genai` (Gemini API)
- **No config files** — flags and optional `GEMINI_API_KEY` env var only
- **CI**: GitHub Actions (test, lint, security scan, release via GoReleaser)

## Build and test

```bash
make build                  # Build binary with version metadata
go test -v -race ./...      # Run all tests with race detector
go vet ./...                # Static analysis
make validate                           # Check all DB URLs + matrix content (no cluster needed)
go run ./cmd/kaddons-validate --links   # Only URL reachability
go run ./cmd/kaddons-validate --matrix  # Only matrix content validation
```

## Code conventions

- **Testing**: stdlib `testing` only, table-driven tests, no external frameworks
- **Output**: progress to stderr, results to stdout (enables piping)
- **Concurrency**: semaphore pattern with buffered channel (10 workers)
- **HTTP**: all fetches use `context.Context`, 10-15s timeouts
- **Matching**: six-pass algorithm in `LookupAddon` — see `internal/addon/addon.go`
- **Status type**: tri-state `"true"` / `"false"` / `"unknown"` (strings, not booleans). Custom `UnmarshalJSON` normalizes LLM output.

## Important patterns

### Addon database is embedded

The JSON file is compiled into the binary via `go:embed`. Changes to `internal/addon/k8s_universal_addons.json` require a rebuild. The file must be valid JSON matching the `addonsFile` struct in `internal/addon/addon.go`.

### GitHub URL conversion

`GitHubRawURL()` in `internal/fetch/fetch.go` converts `github.com` URLs to `raw.githubusercontent.com` at fetch time. This is transparent — the database stores human-readable URLs, the fetch layer handles conversion. Raw Markdown is not HTML-stripped (the LLM reads it natively).

### LLM output normalization

The Gemini model sometimes returns booleans instead of strings, or wraps JSON in code fences. `ExtractJSON()` in `internal/output/output.go` strips fences. `Status.UnmarshalJSON()` normalizes `true` → `"true"`, `null` → `"unknown"`, garbage → `"unknown"`.

### EOL slug mapping

`eolProductSlugs` in `internal/addon/addon.go` maps addon names to endoflife.date slugs. Include all common name variants (hyphenated, spaced, aliased) since matching is by exact lowercase key.

## Common tasks

### Adding an addon to the database

1. Add entry to `internal/addon/k8s_universal_addons.json` with all five required fields
2. `compatibility_matrix_url` should point to a page with actual K8s version data
3. Verify: `go build ./...`, `make validate`, `go test -v ./...`

### Adding an EOL mapping

1. Verify the product exists at `https://endoflife.date/api/{slug}.json`
2. Add entries to `eolProductSlugs` in `internal/addon/addon.go` for all name variants
3. Run tests to verify

### Modifying the matching algorithm

The six-pass algorithm in `LookupAddon` is order-sensitive — earlier passes take priority. Changes should be tested against the full test suite in `internal/addon/addon_test.go` which covers exact matches, normalization, suffix stripping, prefix matching, and word-subset matching.

### Modifying the LLM prompt

The system prompt is in `internal/agent/agent.go:analysisSystemPrompt`. It instructs the LLM to return specific JSON schema. Changes to the prompt may require updating `internal/output/output.go` types and `internal/output/output_test.go` tests if the schema changes.

## Do not

- Do not add external test frameworks — use stdlib `testing`
- Do not add configuration files — flags and env vars only
- Do not call the LLM outside Phase 3 — Phases 1 and 2 must remain deterministic
- Do not modify test assertions to match broken implementations — fix the code
- Do not add `--no-verify` or skip hooks
- Do not commit API keys or secrets
