//go:build windows

// Command agentmesh-agent is the AgentMesh agent for Windows (Windows Service).
package main

import (
	"os"

	"github.com/enfec/agentmesh/agents/core"
)

func main() { os.Exit(core.Main(os.Args)) }
