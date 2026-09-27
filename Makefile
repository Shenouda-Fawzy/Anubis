BINARY := anubis
GOFILES := $(shell find cmd -name '*.go')

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build install test e2e cover fmt fmt-check vet lint tidy clean

## build: compile the static ./anubis binary
build:
	CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o $(BINARY) ./cmd/anubis

## install: build into $GOBIN
install:
	CGO_ENABLED=0 go install -trimpath -ldflags '$(LDFLAGS)' ./cmd/anubis

## test: run the full suite, including end-to-end tests against local mock servers
test:
	go test ./...

## e2e: run only the end-to-end tests
e2e:
	go test -run TestE2E -v ./...

## cover: report statement coverage
cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

## fmt: rewrite sources with gofmt
fmt:
	gofmt -w $(GOFILES)

## fmt-check: fail if any source is not gofmt-clean
fmt-check:
	@unformatted=$$(gofmt -l $(GOFILES)); \
	if [ -n "$$unformatted" ]; then \
		echo "not gofmt-clean:"; echo "$$unformatted"; exit 1; \
	fi

## vet: run go vet
vet:
	go vet ./...

## lint: run golangci-lint if it is installed
lint:
	@command -v golangci-lint >/dev/null 2>&1 \
		|| { echo "golangci-lint not installed; see https://golangci-lint.run/usage/install/"; exit 1; }
	golangci-lint run ./...

## tidy: sync go.mod and go.sum
tidy:
	go mod tidy

## clean: remove build artifacts
clean:
	rm -f $(BINARY) coverage.out

run:
	./$(BINARY) -pr 18 -publish