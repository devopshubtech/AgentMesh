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

# Alpine: the agent is a static Go binary and needs no system tools; the
# shell is only for the enroll-then-run entrypoint. CA certificates are in the base.
FROM alpine:3.22
COPY --from=build /out/agentmesh-agent /usr/local/bin/agentmesh-agent
COPY --chmod=0755 infrastructure/docker/agent-entrypoint.sh /usr/local/bin/agent-entrypoint.sh
VOLUME ["/var/lib/agentmesh"]
ENTRYPOINT ["/usr/local/bin/agent-entrypoint.sh"]
