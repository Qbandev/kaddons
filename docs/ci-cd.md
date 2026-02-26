# CI/CD

kaddons uses GitHub Actions for continuous integration, security scanning, automated link checking, and release management.

## Pipelines

### CI (`ci.yml`)

Runs on every push to `main` and every pull request targeting `main`.

**Test and quality jobs:**

| Step | Command | Purpose |
|------|---------|---------|
| Vet | `go vet ./...` | Static analysis for common mistakes |
| Lint | `golangci-lint` (pinned version) | Code style and quality enforcement |
| Test | `go test ./... -race -v` | Unit tests with race detector |
| Tidy check (advisory) | `go mod tidy` + diff | Reports module tidy differences observed in CI environment |

**Addon DB validation job:**

| Step | Command | Purpose |
|------|---------|---------|
| Matrix validation (advisory) | `go run ./cmd/kaddons-validate --matrix` | Reports compatibility matrix coverage without blocking CI while the dataset is being cleaned |

**Security job:**

| Step | Command | Purpose |
|------|---------|---------|
| govulncheck | `govulncheck ./...` | Known vulnerability scanning in dependencies |
| gosec | `gosec ./...` | Static security analysis |

**Installation verification jobs:**

| Job | Check | Purpose |
|-----|-------|---------|
| Homebrew tap install | `brew tap qbandev/tap` + `brew install qbandev/tap/kaddons` | Ensures Homebrew installation path works (runs on push only) |

### Dependency review (`dependency-review.yml`)

Runs on pull requests to `main` and blocks risky dependency introductions using GitHub's dependency review action.

### Scorecards (`scorecards.yml`)

Runs OpenSSF Scorecards checks on pushes, weekly schedule, and manual trigger, and publishes SARIF results.

### Weekly link check (`linkcheck.yml`)

Runs every Monday at 08:00 UTC (also manually triggerable).

1. Runs `go run ./cmd/kaddons-validate --links`
2. If broken links are found, creates or updates a GitHub issue labeled `broken-links` with the Markdown report and timestamp
3. If all links are healthy, no issue is created

This catches URL rot in the addon database — projects move, rename repositories, or restructure documentation.

### Release automation (`release-please.yml`)

Triggered on each push to `main` (including merged PRs).

1. Uses [release-please](https://github.com/googleapis/release-please-action) to determine semantic version bumps from commit history
2. Creates or updates an automated release PR with proposed version and changelog updates
3. Creates the final tag/release when the release PR is merged

### Publish release artifacts (`release.yml`)

Triggered by tags matching `v*` (created by release-please automation).

1. Runs the full test suite (must pass before publish)
2. Runs [GoReleaser](https://goreleaser.com) to build, package, publish GitHub assets, and update Homebrew tap

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

Normal development pull requests are merged into `main` as usual. Release automation then:

1. creates or updates an automated release PR based on merged commits
2. proposes the next version and changelog in that release PR

When the release PR is merged, the publish workflow:

1. creates the corresponding version tag automatically
2. publishes release artifacts and Homebrew formula updates

Manual tag pushes (`git tag vX.Y.Z && git push origin vX.Y.Z`) are still supported as a fallback.

`HOMEBREW_TAP_TOKEN` is required because Homebrew publishing writes formula updates to the external `qbandev/homebrew-tap` repository.

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
