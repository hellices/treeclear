.DEFAULT_GOAL := help
.PHONY: help verify fmt-check test test-race vet build install
VERSION ?= dev

help:
	@printf '%s\n' 'make verify     Format check, vet, tests, race tests, and build' 'make test       Run uncached Go tests' 'make build      Build the native CLI in bin/' 'make install    Install the macOS source preview to GOBIN or GOPATH/bin'

verify: fmt-check vet test test-race build

fmt-check:
	@files="$$(gofmt -l .)" && { test -z "$$files" || { printf '%s\n' "$$files"; exit 1; }; }

test:
	go test -count=1 ./...

test-race:
	go test -race -count=1 ./...

vet:
	go vet ./...

build:
	go build -trimpath -ldflags="-X github.com/hellices/treeclear/internal/version.Value=$(VERSION)" -o bin/ ./cmd/treeclear

install:
	@set -eu; \
		host_os="$$(go env GOHOSTOS)"; \
		host_arch="$$(go env GOHOSTARCH)"; \
		target_os="$$(go env GOOS)"; \
		target_arch="$$(go env GOARCH)"; \
		if [ "$$host_os" != darwin ] || [ "$$target_os" != "$$host_os" ] || [ "$$target_arch" != "$$host_arch" ]; then \
			printf '%s\n' 'make install supports native macOS builds only; Windows support is tracked in #15.' >&2; \
			exit 1; \
		fi; \
		go install -trimpath -ldflags="-X github.com/hellices/treeclear/internal/version.Value=$(VERSION)" ./cmd/treeclear
