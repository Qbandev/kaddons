PREFIX ?= /usr/local
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

GOLANGCI_LINT_VERSION ?= v2.10.1
GOSEC_VERSION ?= v2.23.0
GOVULNCHECK_VERSION ?= v1.1.4

.PHONY: build clean install uninstall validate validate-live extract sync check vet lint test govulncheck gosec

build:
	go build -ldflags "-s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)" -o kaddons ./cmd/kaddons

clean:
	rm -f kaddons

install: build
	install -d $(PREFIX)/bin
	install -m 755 kaddons $(PREFIX)/bin/kaddons

uninstall:
	rm -f $(PREFIX)/bin/kaddons

validate:
	go run ./cmd/kaddons-validate --stored-only

validate-live:
	go run ./cmd/kaddons-validate

extract:
	go run ./cmd/kaddons-extract

sync:
	go run ./cmd/kaddons-extract --sync

vet:
	go vet ./...

lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run

test:
	go test ./... -race

govulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

gosec:
	go run github.com/securego/gosec/v2/cmd/gosec@$(GOSEC_VERSION) ./...

check: vet lint test govulncheck build validate
	@echo "All checks passed (run 'make gosec' separately — requires Go 1.25.x)."
