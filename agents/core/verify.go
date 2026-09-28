package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/enfec/agentmesh/protocols/agentapi"
	agentv1 "github.com/enfec/agentmesh/protocols/gen/go/agent/v1"
)

const (
	clockSkewTolerance = 30 * time.Second
	maxFutureIssue     = 5 * time.Minute
	maxSeenEntries     = 10000
)

// errReplay marks a command that was already accepted once.
var errReplay = errors.New("command already received")

// replayGuard persists accepted command ids until their expiry, so a
// captured CommandRequest cannot be executed twice — even across restarts.
type replayGuard struct {
	mu   sync.Mutex
	path string
	seen map[string]time.Time
}

func newReplayGuard(path string) *replayGuard {
	g := &replayGuard{path: path, seen: map[string]time.Time{}}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &g.seen)
	}
	g.pruneLocked(time.Now())
	return g
}

func (g *replayGuard) pruneLocked(now time.Time) {
	for id, exp := range g.seen {
		if now.After(exp.Add(clockSkewTolerance)) {
			delete(g.seen, id)
		}
	}
}

// accept records id; it fails if id was seen before or the store is full.
func (g *replayGuard) accept(id string, expires time.Time) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.seen[id]; ok {
		return errReplay
	}
	g.pruneLocked(time.Now())
	if len(g.seen) >= maxSeenEntries {
		return errors.New("replay store full")
	}
	g.seen[id] = expires
	b, err := json.Marshal(g.seen)
	if err != nil {
		return err
	}
	// Persist before executing: if this write fails, the command is refused.
	if err := writeFileAtomic(g.path, b, 0o600); err != nil {
		delete(g.seen, id)
		return fmt.Errorf("persist replay state: %w", err)
	}
	return nil
}

// verifier checks every CommandRequest before anything runs.
type verifier struct {
	id     *Identity
	replay *replayGuard
	offset func() time.Duration // server time minus local time
}

// verify returns the authenticated spec, or an error describing why the
// command must be rejected. The returned spec is nil when the request is too
// malformed to even identify the command.
func (v *verifier) verify(req *agentv1.CommandRequest) (*agentv1.CommandSpec, error) {
	pub := v.id.commandKey(req.GetKeyId())
	if pub == nil {
		return v.peek(req), fmt.Errorf("unknown signing key %q", req.GetKeyId())
	}
	if !agentapi.VerifyCommand(pub, req.GetSpec(), req.GetSignature()) {
		return v.peek(req), errors.New("invalid command signature")
	}
	var spec agentv1.CommandSpec
	if err := proto.Unmarshal(req.GetSpec(), &spec); err != nil {
		return nil, errors.New("malformed command spec")
	}
	if spec.GetDeviceId() != v.id.DeviceID || spec.GetOrgId() != v.id.OrgID {
		return &spec, errors.New("command is addressed to a different device")
	}
	now := time.Now().Add(v.offset())
	exp := spec.GetExpiresAt().AsTime()
	if spec.GetExpiresAt() == nil || now.After(exp.Add(clockSkewTolerance)) {
		return &spec, errors.New("command has expired")
	}
	if spec.GetIssuedAt() == nil || spec.GetIssuedAt().AsTime().After(now.Add(maxFutureIssue)) {
		return &spec, errors.New("command issued in the future")
	}
	if err := v.replay.accept(spec.GetCommandId(), exp); err != nil {
		return &spec, err
	}
	return &spec, nil
}

// peek extracts the command id from an unverified spec, only so a rejection
// can be reported against it. Nothing from an unverified spec is executed.
func (v *verifier) peek(req *agentv1.CommandRequest) *agentv1.CommandSpec {
	var spec agentv1.CommandSpec
	if proto.Unmarshal(req.GetSpec(), &spec) != nil {
		return nil
	}
	return &agentv1.CommandSpec{CommandId: spec.GetCommandId()}
}
