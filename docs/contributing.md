# Contributing

## Development setup

**Requirements:**

- Go 1.25.7+
- `kubectl` (for integration testing against a cluster)
- A Gemini API key (only needed for the main command, not tests)

**Clone and build:**

```bash
git clone https://github.com/qbandev/kaddons.git
cd kaddons
make build
```

**Run tests:**

```bash
go test -v -race ./...
```

## Project structure

```
cmd/kaddons/
  main.go                             CLI entrypoint (Cobra), flags
cmd/kaddons-validate/
  main.go                             DB validation tool (dev/CI only, not distributed)

internal/
  addon/
    addon.go                          Embedded addon DB, 6-pass matching, EOL slug mapping
    addon_test.go                     Matching, normalization, EOL resolution tests
    k8s_universal_addons.json         Addon database (668 entries, embedded via go:embed)
  agent/
    agent.go                          Plan-and-Execute pipeline (discovery → enrichment → analysis)
  cluster/
    cluster.go                        kubectl interaction, version detection, workload discovery
    cluster_test.go                   Chart version, image tag extraction tests
  fetch/
    fetch.go                          HTTP fetching, GitHub raw URL conversion, EOL data
    fetch_test.go                     GitHub URL conversion tests
  output/
    output.go                         JSON/HTML formatting, Status type
    output_test.go                    Status type, JSON formatting, backward compat tests
  validate/
    validate.go                       URL reachability + matrix content validation library
    validate_test.go                  URL check, matrix detection, aggregation, flag tests

Makefile                              Build, install, clean targets
.goreleaser.yaml                      Release configuration
```

## Testing

Tests are table-driven and do not require cluster access or API keys. They cover:

- **Addon matching** (`internal/addon/addon_test.go`) — exact match, normalization, role suffix stripping, word-subset matching, alias resolution, EOL slug lookup, version cycle matching
- **URL conversion** (`internal/fetch/fetch_test.go`) — GitHub→raw conversion for all URL patterns (repo root, blob, tree, wiki, releases, non-GitHub)
- **Output formatting** (`internal/output/output_test.go`) — Status tri-state unmarshaling (bool, string, null, garbage), JSON round-trips, backward compatibility
- **Validation** (`internal/validate/validate_test.go`) — HTTP HEAD/GET fallback, error codes, User-Agent header, matrix detection heuristic, URL aggregation, flag logic
- **Cluster interaction** (`internal/cluster/cluster_test.go`) — chart version stripping, version extraction, image tag parsing

Run with race detector:

```bash
go test -v -race ./...
```

## Code conventions

- No external test frameworks — stdlib `testing` only
- Table-driven tests following the `[]struct{ name, input, want }` pattern
- Progress messages go to stderr, output goes to stdout
- All HTTP fetching uses `context.Context` for cancellation
- Concurrent operations use a semaphore pattern (buffered channel of size 10)

## Adding an addon

See [addon-database.md](addon-database.md) for the full process.

Short version:

1. Add entry to `internal/addon/k8s_universal_addons.json` with all five fields
2. `go build ./...` — verify JSON parses
3. `make validate` — verify URLs are reachable and compatibility pages have K8s data
4. `go test -v ./...` — all tests pass

## Adding an EOL mapping

If the addon is tracked on [endoflife.date](https://endoflife.date):

1. Find the product slug at `https://endoflife.date/api/{slug}.json`
2. Add entries to `eolProductSlugs` in `internal/addon/addon.go` for all common name variants
3. Include lowercase variants: with hyphens, with spaces, and common aliases

## Makefile targets

| Target | Command | Description |
|--------|---------|-------------|
| `build` | `make build` | Build binary with version metadata |
| `validate` | `make validate` | Run DB URL + matrix validation (no cluster needed) |
| `clean` | `make clean` | Remove built binary |
| `install` | `make install` | Build and install to `/usr/local/bin` |
| `uninstall` | `make uninstall` | Remove from `/usr/local/bin` |

Override install prefix: `make install PREFIX=/opt`
