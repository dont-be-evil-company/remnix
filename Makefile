BIN_NAME=remnix
ATTACH_NAME=remnix-attach

.PHONY: build test test-attach lint ci

build: bin/$(BIN_NAME) bin/$(ATTACH_NAME)

bin/$(BIN_NAME):
	go build -tags piv -o bin/$(BIN_NAME) ./cmd/remnix

bin/$(ATTACH_NAME): cmd/remnix-attach/*.odin
	mkdir -p bin
	odin build cmd/remnix-attach -out:bin/$(ATTACH_NAME) -o:speed

test:
	go test ./...

# Attach parser tests (Odin). CI is Go-only; run locally or in release image.
test-attach:
	odin test cmd/remnix-attach

lint:
	golangci-lint run ./...

ci: test lint
