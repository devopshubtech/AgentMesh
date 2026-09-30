# syntax=docker/dockerfile:1
# Builds the backend as ONE multi-call binary (agentmesh-server) with
# control-api, agent-gateway, worker and amctl as symlinks to it: the four
# commands share almost all their code, so this keeps the image ~4x smaller.
# The compose/k8s manifest picks the command.

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
      -o /out/agentmesh-server ./backend/cmd/agentmesh-server \
    && for c in control-api agent-gateway worker amctl; do ln -s agentmesh-server /out/$c; done

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ /usr/local/bin/
USER nonroot:nonroot
ENTRYPOINT []
CMD ["control-api"]
