.DEFAULT_GOAL := help
.PHONY: help verify fmt-check test test-race vet build

help:
	@printf '%s\n' 'make verify     Format check, vet, tests, race tests, and build' 'make test       Run uncached Go tests' 'make build      Build Go packages'

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
	go build ./...
