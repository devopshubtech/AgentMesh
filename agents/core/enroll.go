package core

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/enfec/agentmesh/protocols/agentapi"
)

// Enroll registers this device with the control plane using a one-time
// enrollment token and persists the resulting identity.
func Enroll(ctx context.Context, cfg *Config, token string, force bool) (*Identity, string, error) {
	token = strings.TrimSpace(token)
	if !strings.HasPrefix(token, "am_enr_") {
		return nil, "", errors.New("enrollment token must start with am_enr_")
	}
	if err := cfg.Validate(); err != nil {
		return nil, "", err
	}
	if err := ensureStateDir(cfg.stateDir); err != nil {
		return nil, "", fmt.Errorf("create state dir: %w", err)
	}
	if _, err := os.Stat(cfg.path(identityFile)); err == nil && !force {
		return nil, "", errors.New("this agent is already enrolled (use --force to re-enroll with a new identity)")
	}

	key, pubDER, err := newDeviceKey()
	if err != nil {
		return nil, "", err
	}
	sig, err := ecdsa.SignASN1(rand.Reader, key, agentapi.EnrollDigest(token, pubDER))
	if err != nil {
		return nil, "", err
	}
	osi := osInfo(ctx)
	req := agentapi.EnrollRequest{
		Token: token, PublicKey: pubDER, Signature: sig,
		Facts: agentapi.Facts{
			Hostname: hostname(), Platform: runtime.GOOS, Arch: runtime.GOARCH, OSName: osi.name, OSVersion: osi.version,
			AgentVersion: Version, MachineIDHash: machineIDHash(ctx),
		},
	}
	cl, err := newClient(cfg)
	if err != nil {
		return nil, "", err
	}
	var resp agentapi.EnrollResponse
	if err := cl.post(ctx, agentapi.PathEnroll, req, &resp); err != nil {
		return nil, "", fmt.Errorf("enroll: %w", err)
	}
	if resp.DeviceID == "" || len(resp.CommandKeys) == 0 {
		return nil, "", errors.New("enroll: server response is missing device id or command keys")
	}
	for _, k := range resp.CommandKeys {
		if len(k.PublicKey) != ed25519.PublicKeySize || k.KeyID == "" {
			return nil, "", errors.New("enroll: server returned an invalid command key")
		}
	}
	id := &Identity{DeviceID: resp.DeviceID, OrgID: resp.OrgID, ServerURL: cfg.ServerURL, CommandKeys: resp.CommandKeys,
		EnrolledAt: time.Now().UTC()}
	if err := id.setKey(key); err != nil {
		return nil, "", err
	}
	if err := cfg.Save(); err != nil {
		return nil, "", fmt.Errorf("save config: %w", err)
	}
	if err := id.Save(cfg); err != nil {
		return nil, "", fmt.Errorf("save identity: %w", err)
	}
	_ = os.Remove(cfg.path(seenFile))
	return id, resp.Status, nil
}
