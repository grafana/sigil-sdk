.PHONY: build test lint install

build:
	go build -o sigil-codex ./cmd/sigil-codex

test:
	go test ./...

lint:
	golangci-lint run ./...

install:
	go install ./cmd/sigil-codex
