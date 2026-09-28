# Convenience targets (Linux/macOS). On Windows use the equivalent commands in README.md.
COMPOSE := docker compose -f infrastructure/docker/docker-compose.yml
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.1.0-dev)
LDFLAGS := -s -w -X github.com/enfec/agentmesh/agents/core.Version=$(VERSION)

.PHONY: setup up down logs test e2e lint proto agents dashboard

setup:            ## generate .env, signing keys and dev TLS certs
	./scripts/dev-setup.sh

up:               ## build and start the local stack
	$(COMPOSE) up -d --build --wait

down:
	$(COMPOSE) down

logs:
	$(COMPOSE) logs -f control-api agent-gateway worker

test:             ## unit tests
	go test -race -count=1 ./...

e2e:              ## end-to-end test against the running stack
	go test -tags e2e -count=1 -v ./tests/e2e

lint:
	gofmt -l backend agents protocols/agentapi tests
	go vet ./...
	GOOS=windows go vet ./agents/...

proto:            ## regenerate protobuf code (needs buf + protoc-gen-go)
	buf lint && buf generate

agents:           ## cross-compile agents into ./dist
	CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/agentmesh-agent-linux-amd64 ./agents/linux
	CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/agentmesh-agent-linux-arm64 ./agents/linux
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/agentmesh-agent-windows-amd64.exe ./agents/windows

dashboard:
	cd dashboard && npm ci && npm run build
