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
| Matrix validation (advisory) | `go run ./cmd/kaddons-validate --matrix` | Reports live compatibility matrix coverage without blocking CI |

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

### Release (`release.yml`)

Triggered on each push to `main`. Single workflow with three chained jobs:

1. **release-please** — [release-please](https://github.com/googleapis/release-please-action) computes the next semantic version from conventional commits, creates or updates a release PR, and creates the GitHub Release + tag when the PR is merged
2. **test** — runs the full test suite (only if a release was created)
3. **goreleaser** — [GoReleaser](https://goreleaser.com) builds binaries, uploads assets to the existing GitHub Release, and pushes the Homebrew formula (only if tests pass)

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

Releases are fully automated. Use [conventional commits](https://www.conventionalcommits.org/) when merging to `main`:

- `feat: ...` → minor version bump
- `fix: ...` → patch version bump
- `feat!: ...` or `BREAKING CHANGE:` → major version bump

release-please accumulates changes into a release PR. When the release PR is merged:

1. release-please creates the GitHub Release with changelog and tag
2. GoReleaser builds binaries and uploads them to the release
3. GoReleaser pushes the Homebrew formula to `qbandev/homebrew-tap`

`RELEASE_PLEASE_TOKEN` (PAT) is required so release PRs trigger CI checks.
`HOMEBREW_TAP_TOKEN` (PAT) is required because Homebrew publishing writes formula updates to the external `qbandev/homebrew-tap` repository.

**Release immutability** is enabled on this repository. Once a release is published, its tag and assets cannot be modified or reused — even if the release is later deleted.

### Changelog

release-please generates and maintains `CHANGELOG.md` from conventional commit messages. GoReleaser's changelog is disabled to avoid duplication.

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
make validate                               # deterministic stored-data validation (no network)
make validate-live                          # live checks (links + matrix content)
go run ./cmd/kaddons-validate --stored-only # stored-data validation only
go run ./cmd/kaddons-validate --links       # links only
go run ./cmd/kaddons-validate --matrix      # matrix only

# Module tidy check
go mod tidy && git diff --exit-code go.mod go.sum
```
