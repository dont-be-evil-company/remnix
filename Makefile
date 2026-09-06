BIN_NAME=syncsh
ATTACH_NAME=syncsh-attach

.PHONY: build test lint ci

build: bin/$(BIN_NAME) bin/$(ATTACH_NAME)

bin/$(BIN_NAME):
	go build -tags piv -o bin/$(BIN_NAME) ./cmd/syncsh

bin/$(ATTACH_NAME): cmd/syncsh-attach/main.odin
	mkdir -p bin
	odin build cmd/syncsh-attach -out:bin/$(ATTACH_NAME) -o:speed

test:
	go test ./...

lint:
	golangci-lint run ./...

ci: test lint
