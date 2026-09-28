//go:build e2e

// Package e2e exercises a running AgentMesh stack end to end: operator login,
// enrollment, a real agent (in-process) connecting over TLS, signed command
// execution, RBAC, revocation and audit-chain verification.
//
//	docker compose -f infrastructure/docker/docker-compose.yml up -d --build
//	go test -tags e2e -v ./tests/e2e
//
// Configuration (defaults match the compose stack and .env):
//
//	AGENTMESH_E2E_API      http://localhost:18080
//	AGENTMESH_E2E_GATEWAY  https://localhost:18443
//	AGENTMESH_E2E_CA       infrastructure/docker/certs/ca.pem
//	AM_ADMIN_EMAIL / AM_ADMIN_PASSWORD  (read from infrastructure/docker/.env if unset)
package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/enfec/agentmesh/agents/core"
)

var repoRoot = func() string { d, _ := filepath.Abs("../.."); return d }()

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func dotenv(t *testing.T) map[string]string {
	out := map[string]string{}
	f, err := os.Open(filepath.Join(repoRoot, "infrastructure", "docker", ".env"))
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if k, v, ok := strings.Cut(sc.Text(), "="); ok && !strings.HasPrefix(k, "#") {
			out[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}

type apiClient struct {
	t     *testing.T
	base  string
	token string
}

func (c *apiClient) do(method, path string, body any, out any) int {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.base+path, rd)
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if out != nil && resp.StatusCode/100 == 2 && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			c.t.Fatalf("decode %s: %v: %s", path, err, data)
		}
	}
	return resp.StatusCode
}

func (c *apiClient) must(method, path string, body, out any) {
	c.t.Helper()
	if code := c.do(method, path, body, out); code/100 != 2 {
		c.t.Fatalf("%s %s -> HTTP %d", method, path, code)
	}
}

func login(t *testing.T, base, email, password string) *apiClient {
	c := &apiClient{t: t, base: base}
	var res struct {
		AccessToken string `json:"access_token"`
	}
	c.must("POST", "/v1/auth/login", map[string]string{"email": email, "password": password}, &res)
	c.token = res.AccessToken
	return c
}

type device struct {
	ID           string   `json:"id"`
	Status       string   `json:"status"`
	Connectivity string   `json:"connectivity"`
	Capabilities []string `json:"capabilities"`
}

type command struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Result *struct {
		ExitCode *int    `json:"exit_code"`
		Stdout   string  `json:"stdout"`
		Error    *string `json:"error"`
	} `json:"result"`
}

func eventually(t *testing.T, what string, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func runCommand(t *testing.T, c *apiClient, deviceID string, body map[string]any) command {
	t.Helper()
	var cmd command
	c.must("POST", "/v1/devices/"+deviceID+"/commands", body, &cmd)
	eventually(t, "command "+cmd.ID+" to finish", 30*time.Second, func() bool {
		c.must("GET", "/v1/commands/"+cmd.ID, nil, &cmd)
		switch cmd.Status {
		case "queued", "sent", "acked", "running":
			return false
		}
		return true
	})
	return cmd
}

func TestEndToEnd(t *testing.T) {
	api := env("AGENTMESH_E2E_API", "http://localhost:18080")
	gateway := env("AGENTMESH_E2E_GATEWAY", "https://localhost:18443")
	ca := env("AGENTMESH_E2E_CA", filepath.Join(repoRoot, "infrastructure", "docker", "certs", "ca.pem"))
	de := dotenv(t)
	email, password := env("AM_ADMIN_EMAIL", de["AM_ADMIN_EMAIL"]), env("AM_ADMIN_PASSWORD", de["AM_ADMIN_PASSWORD"])
	if email == "" || password == "" {
		t.Skip("admin credentials not configured")
	}
	if resp, err := http.Get(api + "/readyz"); err != nil || resp.StatusCode != 200 {
		t.Skipf("stack not reachable at %s", api)
	}

	admin := login(t, api, email, password)

	// 1. Enrollment token (auto-approve so the device becomes active at once).
	var tok struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	admin.must("POST", "/v1/enrollment-tokens", map[string]any{"description": "e2e-test", "auto_approve": true, "max_uses": 1, "expires_in_s": 600}, &tok)

	// 2. A real agent enrolls and connects over TLS with the dev CA.
	cfg := core.NewConfig(t.TempDir(), gateway)
	cfg.CAFile = ca
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	id, status, err := core.Enroll(ctx, cfg, tok.Token, false)
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if status != "active" {
		t.Fatalf("enroll status %q, want active", status)
	}
	if _, _, err := core.Enroll(ctx, core.NewConfig(t.TempDir(), gateway), tok.Token, false); err == nil {
		t.Fatal("single-use enrollment token was accepted twice")
	}
	agent, err := core.NewAgent(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = agent.Run(ctx); close(done) }()

	var dev device
	eventually(t, "device online", 30*time.Second, func() bool {
		admin.must("GET", "/v1/devices/"+id.DeviceID, nil, &dev)
		return dev.Connectivity == "online" && len(dev.Capabilities) > 0
	})

	// 3. Signed commands.
	if c := runCommand(t, admin, dev.ID, map[string]any{"kind": "action", "action": "ping"}); c.Status != "succeeded" || !strings.Contains(c.Result.Stdout, "pong") {
		t.Fatalf("ping: %+v", c)
	}
	host, _ := os.Hostname()
	argv := []string{"hostname"}
	if c := runCommand(t, admin, dev.ID, map[string]any{"kind": "exec", "argv": argv}); c.Status != "succeeded" ||
		!strings.Contains(strings.ToLower(c.Result.Stdout), strings.ToLower(host)) {
		t.Fatalf("exec hostname: status=%s stdout=%q", c.Status, c.Result.Stdout)
	}
	// "exit 7" is valid in both /bin/sh and PowerShell.
	if c := runCommand(t, admin, dev.ID, map[string]any{"kind": "exec", "shell": true, "argv": []string{"exit 7"}}); c.Status != "failed" ||
		c.Result.ExitCode == nil || *c.Result.ExitCode != 7 {
		t.Fatalf("exit code propagation: %+v", c)
	}

	// 4. RBAC: a viewer can read but not execute.
	viewerEmail := fmt.Sprintf("e2e-viewer-%d@agentmesh.local", time.Now().UnixNano())
	admin.must("POST", "/v1/users", map[string]string{"email": viewerEmail, "display_name": "e2e viewer", "password": "e2e-viewer-password-1", "role": "viewer"}, nil)
	viewer := login(t, api, viewerEmail, "e2e-viewer-password-1")
	if code := viewer.do("GET", "/v1/devices/"+dev.ID, nil, nil); code != 200 {
		t.Fatalf("viewer read device: %d", code)
	}
	if code := viewer.do("POST", "/v1/devices/"+dev.ID+"/commands", map[string]any{"kind": "action", "action": "ping"}, nil); code != 403 {
		t.Fatalf("viewer ran a command: HTTP %d", code)
	}

	// 5. Revocation disconnects the agent immediately and it halts.
	admin.must("POST", "/v1/devices/"+dev.ID+"/revoke", nil, &dev)
	eventually(t, "agent to record revocation", 15*time.Second, func() bool {
		st, err := core.LoadIdentity(cfg)
		return err == nil && st.Revoked
	})
	admin.must("GET", "/v1/devices/"+dev.ID, nil, &dev)
	if dev.Status != "revoked" || dev.Connectivity != "offline" {
		t.Fatalf("after revoke: %+v", dev)
	}

	// 6. Every step above is in the tamper-evident audit chain.
	var audit struct {
		Items []struct {
			Action string `json:"action"`
		} `json:"items"`
	}
	admin.must("GET", "/v1/devices/"+dev.ID+"/activity?limit=50", nil, &audit)
	seen := map[string]bool{}
	for _, e := range audit.Items {
		seen[e.Action] = true
	}
	for _, want := range []string{"device.enroll", "command.create", "command.result", "device.revoke"} {
		if !seen[want] {
			t.Errorf("audit trail missing %s (have %v)", want, seen)
		}
	}
	var verify struct {
		OK bool `json:"ok"`
	}
	admin.must("GET", "/v1/audit/verify", nil, &verify)
	if !verify.OK {
		t.Fatal("audit hash chain verification failed")
	}
	cancel()
	<-done
}
