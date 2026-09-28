package commands

import (
	"crypto/ed25519"
	"crypto/rand"
	"reflect"
	"strings"
	"testing"

	"github.com/enfec/agentmesh/protocols/agentapi"
)

func TestValidate(t *testing.T) {
	cases := []struct {
		name string
		in   CreateInput
		ok   bool
	}{
		{"action ok", CreateInput{Kind: "action", Action: "ping"}, true},
		{"unknown action", CreateInput{Kind: "action", Action: "format-disk"}, false},
		{"action with argv", CreateInput{Kind: "action", Action: "ping", Argv: []string{"x"}}, false},
		{"exec ok", CreateInput{Kind: "exec", Argv: []string{"uname", "-a"}}, true},
		{"exec empty", CreateInput{Kind: "exec"}, false},
		{"exec empty program", CreateInput{Kind: "exec", Argv: []string{""}}, false},
		{"shell one arg", CreateInput{Kind: "exec", Shell: true, Argv: []string{"echo hi | wc -c"}}, true},
		{"shell two args", CreateInput{Kind: "exec", Shell: true, Argv: []string{"a", "b"}}, false},
		{"nul byte", CreateInput{Kind: "exec", Argv: []string{"echo", "a\x00b"}}, false},
		{"timeout too big", CreateInput{Kind: "exec", Argv: []string{"x"}, TimeoutS: 7200}, false},
		{"bad kind", CreateInput{Kind: "rm"}, false},
		{"huge argv", CreateInput{Kind: "exec", Argv: []string{"echo", strings.Repeat("a", 70<<10)}}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := c.in
			err := in.Validate()
			if (err == nil) != c.ok {
				t.Fatalf("Validate() err=%v, want ok=%v", err, c.ok)
			}
		})
	}
	in := CreateInput{Kind: "exec", Argv: []string{"x"}}
	_ = in.Validate()
	if in.TimeoutS != 60 {
		t.Fatalf("default timeout = %d, want 60", in.TimeoutS)
	}
}

func TestCapability(t *testing.T) {
	for in, want := range map[*CreateInput]string{
		{Kind: "exec"}:                   "exec",
		{Kind: "exec", Shell: true}:      "exec.shell",
		{Kind: "action", Action: "ping"}: "action.ping",
	} {
		if got := in.Capability(); got != want {
			t.Errorf("Capability(%+v) = %q, want %q", *in, got, want)
		}
	}
}

func TestRedactArgv(t *testing.T) {
	in := []string{"mysql", "-u", "root", "--password", "hunter2", "--token=abc123", "api_key=xyz", "curl", "-H", "Authorization: Bearer eyJhbGc.x.y", "plain"}
	want := []string{"mysql", "-u", "root", "--password", "***", "--token=***", "api_key=***", "curl", "-H", "Authorization: Bearer ***", "plain"}
	if got := RedactArgv(in); !reflect.DeepEqual(got, want) {
		t.Fatalf("RedactArgv:\n got  %q\n want %q", got, want)
	}
	if in[4] != "hunter2" {
		t.Fatal("RedactArgv must not mutate its input")
	}
}

func TestCommandSignature(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	spec := []byte("serialized-spec")
	sig := agentapi.SignCommand(priv, spec)
	if !agentapi.VerifyCommand(pub, spec, sig) {
		t.Fatal("valid signature rejected")
	}
	tampered := append([]byte(nil), spec...)
	tampered[0] ^= 1
	if agentapi.VerifyCommand(pub, tampered, sig) {
		t.Fatal("tampered spec accepted")
	}
	// A raw Ed25519 signature over the spec without the domain prefix must not verify.
	if agentapi.VerifyCommand(pub, spec, ed25519.Sign(priv, spec)) {
		t.Fatal("signature without domain separation accepted")
	}
}

func TestSanitizeOutput(t *testing.T) {
	s, trunc := sanitizeOutput([]byte("ok\x00\xff\xfe done"))
	if strings.ContainsRune(s, 0) || trunc {
		t.Fatalf("sanitize failed: %q %v", s, trunc)
	}
	big := make([]byte, MaxStoredOutput+10)
	if _, trunc := sanitizeOutput(big); !trunc {
		t.Fatal("expected truncation")
	}
}
