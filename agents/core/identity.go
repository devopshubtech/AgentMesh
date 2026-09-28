package core

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/enfec/agentmesh/protocols/agentapi"
)

// ErrNotEnrolled means no identity exists yet.
var ErrNotEnrolled = errors.New("agent is not enrolled")

// Identity is the device's credential material, stored in identity.json.
// The private key is protected with the platform secret store (DPAPI on
// Windows) and the file is readable by root/SYSTEM only.
type Identity struct {
	DeviceID    string                `json:"device_id"`
	OrgID       string                `json:"org_id"`
	ServerURL   string                `json:"server_url"`
	CommandKeys []agentapi.CommandKey `json:"command_keys"`
	EnrolledAt  time.Time             `json:"enrolled_at"`
	Revoked     bool                  `json:"revoked,omitempty"`
	// ProtectedKey is the PKCS#8 private key after protectSecret().
	ProtectedKey  []byte `json:"protected_key"`
	KeyProtection string `json:"key_protection"`

	key *ecdsa.PrivateKey
}

// Key returns the device private key.
func (id *Identity) Key() *ecdsa.PrivateKey { return id.key }

// commandKey returns the pinned control-plane key with keyID.
func (id *Identity) commandKey(keyID string) ed25519.PublicKey {
	for _, k := range id.CommandKeys {
		if k.KeyID == keyID && len(k.PublicKey) == ed25519.PublicKeySize {
			return ed25519.PublicKey(k.PublicKey)
		}
	}
	return nil
}

func newDeviceKey() (*ecdsa.PrivateKey, []byte, error) {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	pub, err := x509.MarshalPKIXPublicKey(&k.PublicKey)
	if err != nil {
		return nil, nil, err
	}
	return k, pub, nil
}

func (id *Identity) setKey(k *ecdsa.PrivateKey) error {
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		return err
	}
	prot, scheme, err := protectSecret(der)
	if err != nil {
		return fmt.Errorf("protect device key: %w", err)
	}
	id.ProtectedKey, id.KeyProtection, id.key = prot, scheme, k
	return nil
}

// LoadIdentity reads and unlocks identity.json.
func LoadIdentity(cfg *Config) (*Identity, error) {
	b, err := os.ReadFile(cfg.path(identityFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotEnrolled
	}
	if err != nil {
		return nil, err
	}
	var id Identity
	if err := json.Unmarshal(b, &id); err != nil {
		return nil, fmt.Errorf("parse identity: %w", err)
	}
	der, err := unprotectSecret(id.ProtectedKey, id.KeyProtection)
	if err != nil {
		return nil, fmt.Errorf("unlock device key: %w", err)
	}
	k, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, fmt.Errorf("parse device key: %w", err)
	}
	ek, ok := k.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("device key is not ECDSA")
	}
	id.key = ek
	return &id, nil
}

// Save writes identity.json atomically with owner-only permissions.
func (id *Identity) Save(cfg *Config) error {
	b, err := json.MarshalIndent(id, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(cfg.path(identityFile), b, 0o600)
}
