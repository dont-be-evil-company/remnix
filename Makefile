BIN_NAME=syncsh

.PHONY: build test lint ci

build:
	go build -tags piv -o bin/$(BIN_NAME) ./cmd/syncsh

test:
	go test ./...

lint:
	golangci-lint run ./...

ci: test lint
