.PHONY: build test check verify check-go check-deps check-format lint cross-build \
        container-build container-test \
        package-macos-arm64 \
        darwin-arm64 darwin-amd64 linux-amd64 linux-arm64 \
        recordings recordings-up recordings-down recordings-clean

# Output directory for builds (customize with: make build OUTPUT_DIR=/some/path)
OUTPUT_DIR ?= ./
# Output directory for copyable packages (customize with: make package-macos-arm64 PACKAGE_DIR=/some/path)
PACKAGE_DIR ?= $(HOME)/Downloads

# Docker-compatible CLI: docker, podman, or nerdctl (for containerd)
CONTAINER_RUNTIME ?= docker
CONTAINER_IMAGE ?= nssh-test
export GO_VERSION := $(shell cat .go-version)
GOLANGCI_LINT_VERSION := 2.6.2
GOLANGCI_LINT_SCHEMA ?= https://raw.githubusercontent.com/golangci/golangci-lint/v$(GOLANGCI_LINT_VERSION)/jsonschema/golangci.jsonschema.json
export CGO_ENABLED ?= 0

# Version from git tags or "dev" fallback
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"
BUILDFLAGS ?= -trimpath -buildvcs=false

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

# Read-only checks shared by local verification and CI.
check-go:
	@test "$$(go env GOVERSION)" = "go$(GO_VERSION)" || { printf 'Expected Go %s\n' '$(GO_VERSION)'; exit 1; }

check-deps: check-go
	go mod tidy -diff

check-format:
	@files="$$(gofmt -l .)" || exit $$?; \
	if [ -n "$$files" ]; then printf 'Unformatted Go files:\n%s\n' "$$files"; exit 1; fi

test: check-deps check-format
	go vet ./...
	go test ./...

check: test build
	@version="$$("$(OUTPUT_DIR)/nssh" -V)" || exit $$?; \
	printf '%s\n' "$$version"; \
	case "$$version" in *'['*) printf 'Unexpected optional features\n'; exit 1;; esac

lint:
	@golangci-lint version | grep -F 'version $(GOLANGCI_LINT_VERSION) '
	golangci-lint config verify --schema $(GOLANGCI_LINT_SCHEMA)
	golangci-lint run

cross-build:
	mkdir -p bin/check
	$(MAKE) darwin-arm64 darwin-amd64 linux-amd64 linux-arm64 OUTPUT_DIR=bin/check

verify: check lint cross-build

container-build:
	$(CONTAINER_RUNTIME) build -f test/docker/test-runner/Dockerfile \
		--build-arg GO_VERSION=$(GO_VERSION) --build-arg GOLANGCI_LINT_VERSION=$(GOLANGCI_LINT_VERSION) \
		-t $(CONTAINER_IMAGE) .

container-test: container-build
	$(CONTAINER_RUNTIME) run --rm $(CONTAINER_IMAGE)

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
