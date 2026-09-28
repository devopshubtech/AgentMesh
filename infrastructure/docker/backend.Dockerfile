# syntax=docker/dockerfile:1
# Builds all backend binaries (control-api, agent-gateway, worker, amctl)
# into one small distroless image; the compose/k8s manifest picks the command.

FROM golang:1.26-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOTOOLCHAIN=local
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY protocols ./protocols
COPY backend ./backend
ARG VERSION=0.1.0-dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -ldflags "-s -w -X github.com/enfec/agentmesh/backend/internal/api.Version=${VERSION}" \
      -o /out/ ./backend/cmd/...

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ /usr/local/bin/
USER nonroot:nonroot
ENTRYPOINT []
CMD ["control-api"]
