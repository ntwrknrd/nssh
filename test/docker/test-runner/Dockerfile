ARG GO_VERSION
ARG GOLANGCI_LINT_VERSION
FROM golangci/golangci-lint:v${GOLANGCI_LINT_VERSION} AS lint
FROM golang:${GO_VERSION}-bookworm

ARG GOLANGCI_LINT_VERSION
ADD https://raw.githubusercontent.com/golangci/golangci-lint/v${GOLANGCI_LINT_VERSION}/jsonschema/golangci.jsonschema.json /usr/local/share/golangci-schema.json
COPY --from=lint /usr/bin/golangci-lint /usr/local/bin/golangci-lint

RUN apt-get update \
    && apt-get install -y --no-install-recommends git make openssh-client \
    && apt-get clean \
    && chmod 0644 /usr/local/share/golangci-schema.json \
    && useradd --create-home --uid 1000 nssh \
    && install -d -o nssh -g nssh -m 0700 /home/nssh/.run /workspace

ENV CGO_ENABLED=0 \
    GOLANGCI_LINT_SCHEMA=file:///usr/local/share/golangci-schema.json \
    XDG_RUNTIME_DIR=/home/nssh/.run

USER nssh
WORKDIR /workspace

COPY --chown=nssh:nssh .go-version go.mod go.sum ./
RUN go mod download
COPY --chown=nssh:nssh . .

CMD ["make", "verify"]
