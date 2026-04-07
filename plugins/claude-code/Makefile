.PHONY: build test lint

build:
	go build -o sigil-cc ./cmd/sigil-cc

test:
	go test ./...

lint:
	golangci-lint run ./...
