//go:build darwin

// Command agentmesh-agent is the AgentMesh agent for macOS (launchd daemon).
package main

import (
	"os"

	"github.com/enfec/agentmesh/agents/core"
)

func main() { os.Exit(core.Main(os.Args)) }
