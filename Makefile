BINARY := bin/agent-test-inbox-mcp

.PHONY: build smoke test clean

build:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(BINARY) .

smoke: build
	./scripts/smoke.sh ./$(BINARY)

test:
	go test ./...

clean:
	rm -rf bin
