GO      ?= go
BINARY  := kvant
PKG     := github.com/tamnd/kvant-solver
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

.PHONY: all
all: fmt vet test build

.PHONY: build
build:
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o bin/$(BINARY) ./cmd/$(BINARY)

.PHONY: install
install:
	$(GO) install -trimpath -ldflags '$(LDFLAGS)' ./cmd/$(BINARY)

.PHONY: test
test:
	$(GO) test -race ./...

.PHONY: cover
cover:
	$(GO) test -race -coverprofile=coverage.out -covermode=atomic ./...
	$(GO) tool cover -func=coverage.out | tail -1

.PHONY: fmt
fmt:
	$(GO) fmt ./...

.PHONY: vet
vet:
	$(GO) vet ./...

.PHONY: lint
lint:
	golangci-lint run

.PHONY: tidy
tidy:
	$(GO) mod tidy

.PHONY: snapshot
snapshot:
	goreleaser release --snapshot --clean

.PHONY: clean
clean:
	rm -rf bin dist coverage.out
