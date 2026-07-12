# VHS-based demo runner for generating gif recordings
# Uses official VHS image which includes all dependencies (ttyd, ffmpeg, chromium)

FROM golang:1.25.5-bookworm AS builder

# Install build dependencies
RUN apt-get update && apt-get install -y --no-install-recommends git

WORKDIR /src

# Copy go mod files first for caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY . .
ARG VERSION=dev
RUN go build -ldflags "-X main.version=${VERSION}" -o /bin/nssh ./cmd/nssh

# Runtime image - VHS includes everything needed for recording
FROM ghcr.io/charmbracelet/vhs:latest

# Install additional runtime dependencies
USER root
RUN apt-get update && apt-get install -y --no-install-recommends \
        openssh-client \
    && rm -rf /var/lib/apt/lists/*

# Copy built nssh binary
COPY --from=builder /bin/nssh /usr/local/bin/nssh

# Copy VHS tapes and scripts
COPY test/vhs/*.tape /vhs/
COPY test/vhs/*.sh /vhs/
RUN chmod +x /vhs/*.sh

# Create demo user with proper home directory
RUN useradd -m -s /bin/bash demo \
    && mkdir -p /home/demo/.ssh /home/demo/.config /home/demo/.local/share /home/demo/.local/state \
    && chown -R demo:demo /home/demo

# Output directory for recordings
RUN mkdir -p /output && chown demo:demo /output

WORKDIR /home/demo
USER demo

ENV HOME=/home/demo
ENV TERM=xterm-256color
ENV PATH="/usr/local/bin:${PATH}"

ENTRYPOINT ["/bin/bash"]
