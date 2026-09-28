// Package agentapi defines the agent-facing HTTPS endpoints (enrollment and
// device authentication) and the exact byte strings that are signed.
//
// It is imported by both the agent and the gateway so the two can never
// disagree about what a signature covers.
package agentapi

import (
	"crypto/ed25519"
	"crypto/sha256"
)

// Paths served by agent-gateway.
const (
	PathEnroll    = "/v1/agent/enroll"
	PathChallenge = "/v1/agent/auth/challenge"
	PathToken     = "/v1/agent/auth/token"
	PathConnect   = "/v1/agent/connect"

	// Subprotocol negotiated on the WebSocket.
	Subprotocol = "agentmesh.v1"
	// ProtocolVersion is the agent protocol major version.
	ProtocolVersion = 1
	// MaxMessageBytes caps a single WebSocket message.
	MaxMessageBytes = 1 << 20
)

// Domain-separation prefixes: a signature for one purpose can never be
// replayed as a signature for another.
const (
	enrollDomain  = "agentmesh-enroll-v1\x00"
	authDomain    = "agentmesh-auth-v1\x00"
	commandDomain = "agentmesh-cmd-v1\x00"
	sessionDomain = "agentmesh-session-v1\x00"
)

// Relay endpoint (served by agent-gateway). Both the operator client and the
// agent connect here with a single-use ticket; the gateway pairs them and
// pipes bytes. The stream inside is multiplexed end to end (see exitproto).
const (
	PathRelay         = "/v1/relay"
	RelaySubprotocol  = "agentmesh.relay.v1"
	RelayTicketHeader = "X-AgentMesh-Ticket"
)

// SignSession signs a serialized SessionSpec.
func SignSession(priv ed25519.PrivateKey, spec []byte) []byte {
	return ed25519.Sign(priv, append([]byte(sessionDomain), spec...))
}

// VerifySession verifies a SessionSpec signature.
func VerifySession(pub ed25519.PublicKey, spec, sig []byte) bool {
	return len(pub) == ed25519.PublicKeySize && ed25519.Verify(pub, append([]byte(sessionDomain), spec...), sig)
}

// Error codes returned by the agent endpoints in {"error":{"code":...}}.
const (
	CodeDevicePending  = "device_pending"
	CodeDeviceDisabled = "device_disabled"
	CodeDeviceRevoked  = "device_revoked"
	CodeInvalidToken   = "invalid_enrollment_token"
	CodeRateLimited    = "rate_limited"
)

// Facts are self-reported device facts sent at enrollment.
type Facts struct {
	Hostname      string `json:"hostname"`
	Platform      string `json:"platform"`
	Arch          string `json:"arch"`
	OSName        string `json:"os_name"`
	OSVersion     string `json:"os_version"`
	AgentVersion  string `json:"agent_version"`
	MachineIDHash string `json:"machine_id_hash"`
}

// EnrollRequest registers a new device key using an enrollment token.
type EnrollRequest struct {
	Token     string `json:"token"`
	PublicKey []byte `json:"public_key"` // PKIX DER, ECDSA P-256 (base64 in JSON)
	Facts     Facts  `json:"facts"`
	Signature []byte `json:"signature"` // ASN.1 ECDSA over EnrollDigest
}

// CommandKey is a control-plane public key trusted for command signatures.
type CommandKey struct {
	KeyID     string `json:"key_id"`
	PublicKey []byte `json:"public_key"` // raw Ed25519
}

// EnrollResponse returns the new identity.
type EnrollResponse struct {
	DeviceID    string       `json:"device_id"`
	OrgID       string       `json:"org_id"`
	Status      string       `json:"status"`
	CommandKeys []CommandKey `json:"command_keys"`
}

// ChallengeRequest asks for a single-use nonce.
type ChallengeRequest struct {
	DeviceID string `json:"device_id"`
}

// ChallengeResponse carries the nonce.
type ChallengeResponse struct {
	Nonce     []byte `json:"nonce"`
	ExpiresIn int    `json:"expires_in"`
}

// TokenRequest proves possession of the device key.
type TokenRequest struct {
	DeviceID  string `json:"device_id"`
	Nonce     []byte `json:"nonce"`
	Signature []byte `json:"signature"` // ASN.1 ECDSA over AuthDigest
}

// TokenResponse carries a short-lived device access token.
type TokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

// ErrorBody is the error envelope used by all AgentMesh HTTP APIs.
type ErrorBody struct {
	Error struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
	} `json:"error"`
}

// EnrollDigest is the SHA-256 digest signed by the device key at enrollment.
func EnrollDigest(token string, publicKeyDER []byte) []byte {
	th := sha256.Sum256([]byte(token))
	h := sha256.New()
	h.Write([]byte(enrollDomain))
	h.Write(th[:])
	h.Write(publicKeyDER)
	return h.Sum(nil)
}

// AuthDigest is the SHA-256 digest signed by the device key to obtain a token.
func AuthDigest(deviceID string, nonce []byte) []byte {
	h := sha256.New()
	h.Write([]byte(authDomain))
	h.Write([]byte(deviceID))
	h.Write([]byte{0})
	h.Write(nonce)
	return h.Sum(nil)
}

// CommandSigningInput is the message signed (Ed25519) for a CommandSpec.
func CommandSigningInput(spec []byte) []byte {
	out := make([]byte, 0, len(commandDomain)+len(spec))
	out = append(out, commandDomain...)
	return append(out, spec...)
}

// SignCommand signs a serialized CommandSpec.
func SignCommand(priv ed25519.PrivateKey, spec []byte) []byte {
	return ed25519.Sign(priv, CommandSigningInput(spec))
}

// VerifyCommand verifies a CommandSpec signature.
func VerifyCommand(pub ed25519.PublicKey, spec, sig []byte) bool {
	return len(pub) == ed25519.PublicKeySize && ed25519.Verify(pub, CommandSigningInput(spec), sig)
}
