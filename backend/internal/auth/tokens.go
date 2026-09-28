package auth

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/enfec/agentmesh/backend/internal/platform/keys"
)

// Token audiences. A token minted for one audience is useless for the other.
const (
	AudienceAPI     = "agentmesh-api"
	AudienceGateway = "agentmesh-gateway"
	issuer          = "agentmesh"
)

// ErrTokenExpired distinguishes expiry from other validation failures.
var ErrTokenExpired = errors.New("token expired")

// Claims are the JWT claims used for both users and devices.
type Claims struct {
	jwt.RegisteredClaims
	OrgID     string `json:"org"`
	SessionID string `json:"sid,omitempty"`
}

// TokenIssuer signs and verifies EdDSA JWTs for a single audience.
type TokenIssuer struct {
	priv     ed25519.PrivateKey
	pub      ed25519.PublicKey
	kid      string
	audience string
	ttl      time.Duration
	now      func() time.Time
}

// NewTokenIssuer builds an issuer. priv may be nil for a verify-only issuer.
func NewTokenIssuer(priv ed25519.PrivateKey, audience string, ttl time.Duration) *TokenIssuer {
	pub := priv.Public().(ed25519.PublicKey)
	return &TokenIssuer{priv: priv, pub: pub, kid: keys.ID(pub), audience: audience, ttl: ttl, now: time.Now}
}

// TTL returns the token lifetime.
func (t *TokenIssuer) TTL() time.Duration { return t.ttl }

// Issue mints a token for subject.
func (t *TokenIssuer) Issue(subject, orgID uuid.UUID, sessionID *uuid.UUID) (string, time.Time, error) {
	now := t.now()
	exp := now.Add(t.ttl)
	c := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   subject.String(),
			Audience:  jwt.ClaimStrings{t.audience},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now.Add(-30 * time.Second)),
			ExpiresAt: jwt.NewNumericDate(exp),
			ID:        uuid.Must(uuid.NewV7()).String(),
		},
		OrgID: orgID.String(),
	}
	if sessionID != nil {
		c.SessionID = sessionID.String()
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodEdDSA, c)
	tok.Header["kid"] = t.kid
	s, err := tok.SignedString(t.priv)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign token: %w", err)
	}
	return s, exp, nil
}

// Verified holds the parsed identity from a valid token.
type Verified struct {
	Subject   uuid.UUID
	OrgID     uuid.UUID
	SessionID *uuid.UUID
	ExpiresAt time.Time
}

// Verify validates signature, algorithm, issuer, audience and time claims.
func (t *TokenIssuer) Verify(raw string) (*Verified, error) {
	var c Claims
	_, err := jwt.ParseWithClaims(raw, &c, func(tok *jwt.Token) (any, error) { return t.pub, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
		jwt.WithIssuer(issuer),
		jwt.WithAudience(t.audience),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(t.now),
		jwt.WithLeeway(5*time.Second),
	)
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, fmt.Errorf("invalid token: %w", err)
	}
	sub, err := uuid.Parse(c.Subject)
	if err != nil {
		return nil, errors.New("invalid token subject")
	}
	org, err := uuid.Parse(c.OrgID)
	if err != nil {
		return nil, errors.New("invalid token org")
	}
	v := &Verified{Subject: sub, OrgID: org, ExpiresAt: c.ExpiresAt.Time}
	if c.SessionID != "" {
		sid, err := uuid.Parse(c.SessionID)
		if err != nil {
			return nil, errors.New("invalid token session")
		}
		v.SessionID = &sid
	}
	return v, nil
}
