package core

import (
	"crypto/ed25519"
	"crypto/rand"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/enfec/agentmesh/protocols/agentapi"
	agentv1 "github.com/enfec/agentmesh/protocols/gen/go/agent/v1"
)

type fixture struct {
	priv ed25519.PrivateKey
	v    *verifier
	dir  string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	dir := t.TempDir()
	id := &Identity{DeviceID: "dev-1", OrgID: "org-1", CommandKeys: []agentapi.CommandKey{{KeyID: "k1", PublicKey: pub}}}
	return &fixture{priv: priv, dir: dir, v: &verifier{
		id: id, replay: newReplayGuard(filepath.Join(dir, seenFile)), offset: func() time.Duration { return 0 },
	}}
}

func (f *fixture) request(t *testing.T, mut func(*agentv1.CommandSpec)) *agentv1.CommandRequest {
	t.Helper()
	now := time.Now()
	spec := &agentv1.CommandSpec{CommandId: "cmd-" + time.Now().Format("150405.000000000"), OrgId: "org-1", DeviceId: "dev-1",
		Kind: "exec", Argv: []string{"echo", "hi"}, TimeoutS: 10,
		IssuedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(5 * time.Minute))}
	if mut != nil {
		mut(spec)
	}
	b, _ := proto.Marshal(spec)
	return &agentv1.CommandRequest{Spec: b, Signature: agentapi.SignCommand(f.priv, b), KeyId: "k1"}
}

func TestVerifierAcceptsValidCommand(t *testing.T) {
	f := newFixture(t)
	spec, err := f.v.verify(f.request(t, nil))
	if err != nil || spec.GetArgv()[1] != "hi" {
		t.Fatalf("verify: spec=%v err=%v", spec, err)
	}
}

func TestVerifierRejections(t *testing.T) {
	f := newFixture(t)
	cases := map[string]func(*agentv1.CommandRequest){
		"wrong device": nil, "wrong org": nil, "expired": nil, "future": nil,
		"bad signature": func(r *agentv1.CommandRequest) { r.Signature[0] ^= 0xff },
		"unknown key":   func(r *agentv1.CommandRequest) { r.KeyId = "other" },
		"tampered spec": func(r *agentv1.CommandRequest) { r.Spec[len(r.Spec)-1] ^= 0x01 },
	}
	specMut := map[string]func(*agentv1.CommandSpec){
		"wrong device": func(s *agentv1.CommandSpec) { s.DeviceId = "dev-2" },
		"wrong org":    func(s *agentv1.CommandSpec) { s.OrgId = "org-2" },
		"expired":      func(s *agentv1.CommandSpec) { s.ExpiresAt = timestamppb.New(time.Now().Add(-time.Hour)) },
		"future":       func(s *agentv1.CommandSpec) { s.IssuedAt = timestamppb.New(time.Now().Add(time.Hour)) },
	}
	for name, reqMut := range cases {
		t.Run(name, func(t *testing.T) {
			req := f.request(t, specMut[name])
			if reqMut != nil {
				reqMut(req)
			}
			if _, err := f.v.verify(req); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

func TestReplayProtectionPersists(t *testing.T) {
	f := newFixture(t)
	req := f.request(t, nil)
	if _, err := f.v.verify(req); err != nil {
		t.Fatal(err)
	}
	if _, err := f.v.verify(req); err != errReplay {
		t.Fatalf("second delivery: got %v, want errReplay", err)
	}
	// A restarted agent loads the persisted set.
	restarted := &verifier{id: f.v.id, replay: newReplayGuard(filepath.Join(f.dir, seenFile)), offset: f.v.offset}
	if _, err := restarted.verify(req); err != errReplay {
		t.Fatalf("after restart: got %v, want errReplay", err)
	}
}

func TestClockOffsetUsedForExpiry(t *testing.T) {
	f := newFixture(t)
	// Local clock is 1h behind the server: a command that expired on the
	// server's clock must be rejected even though it looks fresh locally.
	f.v.offset = func() time.Duration { return time.Hour }
	if _, err := f.v.verify(f.request(t, nil)); err == nil {
		t.Fatal("expected expiry based on server-adjusted clock")
	}
}

type captureEmitter struct {
	mu       sync.Mutex
	chunks   int
	bytes    int
	maxChunk int
}

func (c *captureEmitter) live(env *agentv1.Envelope) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.chunks++
	n := len(env.GetCommandOutput().GetData())
	c.bytes += n
	c.maxChunk = max(c.maxChunk, n)
}
func (c *captureEmitter) result(*agentv1.CommandResult) {}
func (c *captureEmitter) sendInventory()                {}

func TestStreamWriterCaps(t *testing.T) {
	e := &captureEmitter{}
	w := &streamWriter{id: "c", stream: agentv1.Stream_STREAM_STDOUT, emit: e}
	block := []byte(strings.Repeat("x", 100<<10))
	for i := 0; i < 20; i++ { // 2 MB total
		if _, err := w.Write(block); err != nil {
			t.Fatal(err)
		}
	}
	if w.buf.Len() != resultOutputCap || !w.trunc {
		t.Fatalf("stored %d bytes trunc=%v, want %d and true", w.buf.Len(), w.trunc, resultOutputCap)
	}
	if e.bytes != liveOutputCap {
		t.Fatalf("streamed %d bytes, want %d", e.bytes, liveOutputCap)
	}
	if e.maxChunk > outputChunk {
		t.Fatalf("largest chunk %d exceeds %d", e.maxChunk, outputChunk)
	}
}

func TestPolicyAdmission(t *testing.T) {
	no := false
	x := newExecutor(Policy{AllowExec: &no, DisabledActions: []string{"process.list"}}, &captureEmitter{}, nil)
	if r := x.admit(&agentv1.CommandSpec{Kind: "exec", Argv: []string{"id"}}); r == "" {
		t.Fatal("exec admitted despite local policy")
	}
	if r := x.admit(&agentv1.CommandSpec{Kind: "action", Action: "process.list"}); r == "" {
		t.Fatal("disabled action admitted")
	}
	if r := x.admit(&agentv1.CommandSpec{Kind: "action", Action: "ping"}); r != "" {
		t.Fatalf("ping rejected: %s", r)
	}
	for _, c := range x.capabilities() {
		if c == "exec" || c == "action.process.list" {
			t.Fatalf("capability %q advertised despite policy", c)
		}
	}
}

func TestBackoffBounds(t *testing.T) {
	var b backoff
	for i := 0; i < 50; i++ {
		d := b.next()
		if d <= 0 || d > backoffMax+time.Second {
			t.Fatalf("backoff out of range: %v", d)
		}
	}
	b.reset()
	if b.attempt != 0 {
		t.Fatal("reset failed")
	}
}

func TestConfigValidate(t *testing.T) {
	for url, ok := range map[string]bool{
		"https://mesh.example.com/": true, "http://localhost:8443": false, "ftp://x": false, "not a url": false,
	} {
		c := &Config{ServerURL: url}
		if err := c.Validate(); (err == nil) != ok {
			t.Errorf("Validate(%q) err=%v want ok=%v", url, err, ok)
		}
	}
	c := &Config{ServerURL: "http://localhost:8443", AllowInsecureHTTP: true}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}
