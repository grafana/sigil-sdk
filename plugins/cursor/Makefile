.PHONY: build test lint install

build:
	go build -o sigil-cursor ./cmd/sigil-cursor

test:
	go test ./...

lint:
	golangci-lint run ./...

install:
	go install ./cmd/sigil-cursor
