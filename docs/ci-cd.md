# CI/CD

kaddons uses GitHub Actions for continuous integration, security scanning, automated link checking, and release management.

## Pipelines

### CI (`ci.yml`)

Runs on every push to `main` and every pull request targeting `main`.

**Test job:**

| Step | Command | Purpose |
|------|---------|---------|
| Vet | `go vet ./...` | Static analysis for common mistakes |
| Lint | `golangci-lint` (latest) | Code style and quality enforcement |
| Test | `go test ./... -race -v` | Unit tests with race detector |
| Tidy check | `go mod tidy` + diff | Ensures module files are committed clean |

**Addon DB validation job:**

| Step | Command | Purpose |
|------|---------|---------|
| Matrix validation | `go run ./cmd/kaddons-validate --matrix` | Ensures compatibility matrix URLs still contain Kubernetes version data |

**Security job:**

| Step | Command | Purpose |
|------|---------|---------|
| govulncheck | `govulncheck ./...` | Known vulnerability scanning in dependencies |
| gosec | `gosec ./...` | Static security analysis |

### Weekly link check (`linkcheck.yml`)

Runs every Monday at 08:00 UTC (also manually triggerable).

1. Runs `go run ./cmd/kaddons-validate --links`
2. If broken links are found, creates or updates a GitHub issue labeled `broken-links` with the Markdown report and timestamp
3. If all links are healthy, no issue is created

This catches URL rot in the addon database — projects move, rename repositories, or restructure documentation.

### Release (`release.yml`)

Triggered by pushing a tag matching `v*` (e.g., `v1.2.0`).

1. Runs the full test suite (must pass before release)
2. Runs [GoReleaser](https://goreleaser.com) to build, package, and publish

## Release process

### Building releases

GoReleaser builds static binaries (`CGO_ENABLED=0`) for:

| OS | Architecture |
|----|-------------|
| Linux | amd64, arm64 |
| macOS | amd64, arm64 |

Binaries are compiled with version metadata injected via ldflags:

```
-X main.version={{.Version}}
-X main.commit={{.Commit}}
-X main.date={{.Date}}
```

### Distribution

Releases are published through:

1. **GitHub Releases** — tar.gz archives with SHA256 checksums
2. **Homebrew** — formula pushed to `qbandev/homebrew-tap` repository

Archive naming: `kaddons_{version}_{os}_{arch}.tar.gz`

### Creating a release

```bash
git tag v1.2.0
git push origin v1.2.0
```

The release workflow handles everything after the tag push. The Homebrew formula is automatically updated.

### Changelog

GoReleaser generates changelogs from commit messages, excluding prefixes: `docs:`, `test:`, `ci:`, `chore:`.

## Local development CI

To run the same checks locally before pushing:

```bash
# Tests
go test -v -race ./...

# Vet
go vet ./...

# Lint (requires golangci-lint installed)
golangci-lint run

# Security
govulncheck ./...
gosec ./...

# Validate addon DB (no cluster needed)
make validate                              # both checks
go run ./cmd/kaddons-validate --links      # links only
go run ./cmd/kaddons-validate --matrix     # matrix only

# Module tidy check
go mod tidy && git diff --exit-code go.mod go.sum
```
