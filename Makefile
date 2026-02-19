PREFIX ?= /usr/local
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

.PHONY: build clean install uninstall

build:
	go build -ldflags "-s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)" -o kaddons .

clean:
	rm -f kaddons

install: build
	install -d $(PREFIX)/bin
	install -m 755 kaddons $(PREFIX)/bin/kaddons

uninstall:
	rm -f $(PREFIX)/bin/kaddons
