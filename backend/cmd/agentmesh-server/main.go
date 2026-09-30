// Command agentmesh-server is all backend commands in one binary, so the
// container image carries the shared code once instead of four times.
// It picks the command from its own name (control-api, agent-gateway,
// worker and amctl are symlinks to it) or from the first argument:
//
//	agentmesh-server control-api
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/enfec/agentmesh/backend/internal/cmds/amctlcmd"
	"github.com/enfec/agentmesh/backend/internal/cmds/controlapicmd"
	"github.com/enfec/agentmesh/backend/internal/cmds/gatewaycmd"
	"github.com/enfec/agentmesh/backend/internal/cmds/workercmd"
)

var commands = map[string]func(){
	"control-api":   controlapicmd.Main,
	"agent-gateway": gatewaycmd.Main,
	"worker":        workercmd.Main,
	"amctl":         amctlcmd.Main,
}

func main() {
	name := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	if _, ok := commands[name]; !ok && len(os.Args) > 1 {
		// Called as "agentmesh-server <command> [args]": drop our own name so
		// the command sees its usual arguments.
		name = os.Args[1]
		os.Args = os.Args[1:]
	}
	run, ok := commands[name]
	if !ok {
		fmt.Fprintln(os.Stderr, "usage: agentmesh-server control-api|agent-gateway|worker|amctl [args]")
		os.Exit(2)
	}
	run()
}
