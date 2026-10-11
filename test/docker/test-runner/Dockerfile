FROM golang:1.25.5-bookworm

RUN apt-get update \
    && apt-get install -y --no-install-recommends git make openssh-client \
    && apt-get clean \
    && useradd --create-home --uid 1000 nssh \
    && install -d -o nssh -g nssh -m 0700 /home/nssh/.run /workspace

ENV CGO_ENABLED=0 \
    XDG_RUNTIME_DIR=/home/nssh/.run

USER nssh
WORKDIR /workspace

COPY --chown=nssh:nssh go.mod go.sum ./
RUN go mod download
COPY --chown=nssh:nssh . .

CMD ["make", "build", "test"]
