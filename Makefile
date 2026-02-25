PREFIX ?= /usr/local
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

.PHONY: build clean install uninstall validate

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
	go run ./cmd/kaddons-validate
