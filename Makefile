.PHONY: build build-hardware test test-hardware \
        darwin-arm64 darwin-amd64 linux-amd64 linux-arm64 \
        recordings recordings-up recordings-down recordings-clean

# Output directory for builds (customize with: make build OUTPUT_DIR=/some/path)
OUTPUT_DIR ?= ./

# Version from git tags or "dev" fallback
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"
BUILDFLAGS := -trimpath

# Native build (current platform)
build:
	go build $(LDFLAGS) $(BUILDFLAGS) -o $(OUTPUT_DIR)/nssh ./cmd/nssh

# Hardware-enabled build (requires PC/SC: libpcsclite-dev on Linux, PCSC.framework on macOS)
build-hardware:
	CGO_ENABLED=1 go build -tags hardware $(LDFLAGS) $(BUILDFLAGS) -o $(OUTPUT_DIR)/nssh-hardware ./cmd/nssh

# Cross-compilation targets (software-only, no CGO/hardware support)
darwin-arm64:
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) $(BUILDFLAGS) -o $(OUTPUT_DIR)/nssh-darwin-arm64 ./cmd/nssh

darwin-amd64:
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) $(BUILDFLAGS) -o $(OUTPUT_DIR)/nssh-darwin-amd64 ./cmd/nssh

linux-amd64:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) $(BUILDFLAGS) -o $(OUTPUT_DIR)/nssh-linux-amd64 ./cmd/nssh

linux-arm64:
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) $(BUILDFLAGS) -o $(OUTPUT_DIR)/nssh-linux-arm64 ./cmd/nssh

# Test targets
test:
	go vet ./...
	gofmt -w .
	go test ./...

test-hardware:
	go vet ./...
	gofmt -w .
	CGO_ENABLED=1 go test -tags hardware ./...

# Recording targets (VHS-based) - generates gif recordings
recordings: recordings-up
	docker compose -f test/docker/docker-compose.yml exec -T vhs-runner /vhs/setup-env.sh
	docker compose -f test/docker/docker-compose.yml exec -T vhs-runner vhs -o /output/demo.gif /vhs/full-demo.tape
	$(MAKE) recordings-down

recordings-up:
	VERSION=dev docker compose -f test/docker/docker-compose.yml up -d --build vhs-runner sshd-pwd sshd-key

recordings-down:
	docker compose -f test/docker/docker-compose.yml down

recordings-clean:
	docker compose -f test/docker/docker-compose.yml down -v --rmi local
	rm -f docs/examples/demo.gif
