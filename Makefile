VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X github.com/darkcarard/sdd-framework/internal/version.Version=$(VERSION)

.PHONY: build install test vet fmt check clean live-test

build:
	go build -ldflags "$(LDFLAGS)" -o bin/sdd ./cmd/sdd

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/sdd

test:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

# check fails if code is not gofmt-clean, then vets and tests.
check:
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed:"; gofmt -l .; exit 1)
	go vet ./...
	go test -race ./...

# live-test calls the real Anthropic API (costs a few cents); needs ANTHROPIC_API_KEY.
live-test:
	go test -tags live -run Live -v ./internal/provider/anthropic/

clean:
	rm -rf bin
