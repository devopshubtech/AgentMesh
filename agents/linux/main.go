//go:build linux

// Command agentmesh-agent is the AgentMesh agent for Linux (systemd).
package main

import (
	"os"

	"github.com/enfec/agentmesh/agents/core"
)

func main() { os.Exit(core.Main(os.Args)) }
