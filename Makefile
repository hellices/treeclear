.DEFAULT_GOAL := help
.PHONY: help doctor verify fmt-check docs test test-race vet build cross-build status

help:
	go run ./tools/harness --help

doctor:
	go run ./tools/harness doctor

verify:
	go run ./tools/harness verify

fmt-check:
	go run ./tools/harness fmt

docs:
	go run ./tools/harness docs

test:
	go run ./tools/harness test

test-race:
	go run ./tools/harness race

vet:
	go run ./tools/harness vet

build:
	go run ./tools/harness build

cross-build:
	go run ./tools/harness cross

status:
	go run ./tools/harness status
