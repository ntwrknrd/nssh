.PHONY: build test \
        package-macos-arm64 \
        darwin-arm64 darwin-amd64 linux-amd64 linux-arm64 \
        recordings recordings-up recordings-down recordings-clean

# Output directory for builds (customize with: make build OUTPUT_DIR=/some/path)
OUTPUT_DIR ?= ./
# Output directory for copyable packages (customize with: make package-macos-arm64 PACKAGE_DIR=/some/path)
PACKAGE_DIR ?= $(HOME)/Downloads

# Version from git tags or "dev" fallback
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"
BUILDFLAGS := -trimpath -buildvcs=false

# Native build (current platform)
build:
	go build $(LDFLAGS) $(BUILDFLAGS) -o $(OUTPUT_DIR)/nssh ./cmd/nssh
	go build $(LDFLAGS) $(BUILDFLAGS) -o $(OUTPUT_DIR)/nssh-askpass ./cmd/nssh-askpass

# Cross-compilation targets
darwin-arm64:
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) $(BUILDFLAGS) -o $(OUTPUT_DIR)/nssh-darwin-arm64 ./cmd/nssh
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) $(BUILDFLAGS) -o $(OUTPUT_DIR)/nssh-askpass-darwin-arm64 ./cmd/nssh-askpass

darwin-amd64:
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) $(BUILDFLAGS) -o $(OUTPUT_DIR)/nssh-darwin-amd64 ./cmd/nssh
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) $(BUILDFLAGS) -o $(OUTPUT_DIR)/nssh-askpass-darwin-amd64 ./cmd/nssh-askpass

linux-amd64:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) $(BUILDFLAGS) -o $(OUTPUT_DIR)/nssh-linux-amd64 ./cmd/nssh
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) $(BUILDFLAGS) -o $(OUTPUT_DIR)/nssh-askpass-linux-amd64 ./cmd/nssh-askpass

linux-arm64:
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) $(BUILDFLAGS) -o $(OUTPUT_DIR)/nssh-linux-arm64 ./cmd/nssh
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) $(BUILDFLAGS) -o $(OUTPUT_DIR)/nssh-askpass-linux-arm64 ./cmd/nssh-askpass

package-macos-arm64:
	scripts/package-macos-arm64.sh --output-dir $(PACKAGE_DIR) --version $(VERSION)

# Test targets
test:
	go vet ./...
	gofmt -w .
	go test ./...

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
