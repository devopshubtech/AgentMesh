# syntax=docker/dockerfile:1
# Demo Linux device: runs the real Linux agent in a container so the full
# enroll → connect → command flow can be exercised without a VM.
# (On real machines the agent runs under systemd via `agentmesh-agent install`.)

FROM golang:1.26-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOTOOLCHAIN=local
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY protocols ./protocols
COPY agents ./agents
ARG VERSION=0.1.0-dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -ldflags "-s -w -X github.com/enfec/agentmesh/agents/core.Version=${VERSION}" \
      -o /out/agentmesh-agent ./agents/linux

FROM debian:12-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates procps iproute2 \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/agentmesh-agent /usr/local/bin/agentmesh-agent
COPY infrastructure/docker/agent-entrypoint.sh /usr/local/bin/agent-entrypoint.sh
RUN chmod 0755 /usr/local/bin/agent-entrypoint.sh
VOLUME ["/var/lib/agentmesh"]
ENTRYPOINT ["/usr/local/bin/agent-entrypoint.sh"]
