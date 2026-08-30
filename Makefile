BINARY := anubis

.PHONY: build test fmt vet check clean

build:
	CGO_ENABLED=0 go build -o $(BINARY) ./cmd/anubis

test:
	go test ./...

fmt:
	gofmt -w $$(find cmd pkg -name '*.go')

vet:
	go vet ./...

check:
	@test -z "$$(gofmt -l cmd pkg)"
	go vet ./...
	go test ./...

clean:
	rm -f $(BINARY)