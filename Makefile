.PHONY: build build-hardware test test-hardware verify-builds vet fmt \
        linux recordings recordings-up recordings-down recordings-clean

# Output directory for builds (customize with: make build OUTPUT_DIR=/some/path)
OUTPUT_DIR ?= ~/Downloads

# Version from git tags or "dev" fallback
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"
BUILDFLAGS := -trimpath

# Build targets
build:
	go build $(LDFLAGS) $(BUILDFLAGS) -o $(OUTPUT_DIR)/nssh ./cmd/nssh

# Hardware-enabled build (requires PC/SC: libpcsclite-dev on Linux, PCSC.framework on macOS)
build-hardware:
	CGO_ENABLED=1 go build -tags hardware $(LDFLAGS) $(BUILDFLAGS) -o $(OUTPUT_DIR)/nssh-hardware ./cmd/nssh

# Cross-compilation target
linux:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) $(BUILDFLAGS) -o $(OUTPUT_DIR)/nssh-linux-amd64 ./cmd/nssh

# Verify both builds succeed
verify-builds: build build-hardware
	@echo "Both builds succeeded"
	@$(OUTPUT_DIR)/nssh -V
	@$(OUTPUT_DIR)/nssh-hardware -V

# Test targets
test:
	go test ./...

test-hardware:
	CGO_ENABLED=1 go test -tags hardware ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

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
	rm -f docs/examples/demo.gif docs/examples/demo.webm docs/examples/demo.mp4
