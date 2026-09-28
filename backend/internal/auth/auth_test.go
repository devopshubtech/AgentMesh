package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPasswordHashRoundTrip(t *testing.T) {
	h, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$") {
		t.Fatalf("unexpected encoding %q", h)
	}
	ok, err := VerifyPassword("correct horse battery staple", h)
	if err != nil || !ok {
		t.Fatalf("expected match, got ok=%v err=%v", ok, err)
	}
	ok, err = VerifyPassword("wrong password!!", h)
	if err != nil || ok {
		t.Fatalf("expected mismatch, got ok=%v err=%v", ok, err)
	}
	h2, _ := HashPassword("correct horse battery staple")
	if h == h2 {
		t.Fatal("hashes must be salted")
	}
	if _, err := VerifyPassword("x", "$bcrypt$nope"); err == nil {
		t.Fatal("expected error for foreign hash format")
	}
}

func TestValidatePassword(t *testing.T) {
	if ValidatePassword("short") == "" {
		t.Fatal("short password accepted")
	}
	if ValidatePassword(strings.Repeat("a", 300)) == "" {
		t.Fatal("overlong password accepted")
	}
	if ValidatePassword("a-long-enough-pass") != "" {
		t.Fatal("valid password rejected")
	}
}

func newIssuer(t *testing.T, aud string, ttl time.Duration) *TokenIssuer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return NewTokenIssuer(priv, aud, ttl)
}

func TestTokenIssueVerify(t *testing.T) {
	iss := newIssuer(t, AudienceAPI, time.Minute)
	sub, org, sid := uuid.New(), uuid.New(), uuid.New()
	tok, exp, err := iss.Issue(sub, org, &sid)
	if err != nil {
		t.Fatal(err)
	}
	v, err := iss.Verify(tok)
	if err != nil {
		t.Fatal(err)
	}
	if v.Subject != sub || v.OrgID != org || v.SessionID == nil || *v.SessionID != sid {
		t.Fatalf("claims mismatch: %+v", v)
	}
	if v.ExpiresAt.Unix() != exp.Unix() {
		t.Fatalf("expiry mismatch")
	}
}

func TestTokenAudienceIsolation(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	api := NewTokenIssuer(priv, AudienceAPI, time.Minute)
	gw := NewTokenIssuer(priv, AudienceGateway, time.Minute)
	tok, _, _ := gw.Issue(uuid.New(), uuid.New(), nil)
	if _, err := api.Verify(tok); err == nil {
		t.Fatal("gateway token accepted by API issuer")
	}
}

func TestTokenWrongKeyAndExpiry(t *testing.T) {
	a := newIssuer(t, AudienceAPI, time.Minute)
	b := newIssuer(t, AudienceAPI, time.Minute)
	tok, _, _ := a.Issue(uuid.New(), uuid.New(), nil)
	if _, err := b.Verify(tok); err == nil {
		t.Fatal("token signed by another key accepted")
	}

	exp := newIssuer(t, AudienceAPI, time.Minute)
	tok, _, _ = exp.Issue(uuid.New(), uuid.New(), nil)
	exp.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	if _, err := exp.Verify(tok); !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("expected ErrTokenExpired, got %v", err)
	}
}

func TestTokenRejectsAlgNone(t *testing.T) {
	iss := newIssuer(t, AudienceAPI, time.Minute)
	// header {"alg":"none","typ":"JWT"} with a plausible payload and no signature
	forged := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJpc3MiOiJhZ2VudG1lc2giLCJzdWIiOiIwMDAwMDAwMC0wMDAwLTAwMDAtMDAwMC0wMDAwMDAwMDAwMDAiLCJhdWQiOlsiYWdlbnRtZXNoLWFwaSJdLCJleHAiOjk5OTk5OTk5OTl9."
	if _, err := iss.Verify(forged); err == nil {
		t.Fatal("alg=none token accepted")
	}
}

func TestPrincipalHoldsAll(t *testing.T) {
	p := &Principal{Permissions: map[string]bool{PermDevicesRead: true, PermCommandsRead: true}}
	if !p.HoldsAll([]string{PermDevicesRead}) || p.HoldsAll([]string{PermDevicesRead, PermUsersManage}) {
		t.Fatal("HoldsAll wrong")
	}
	var nilP *Principal
	if nilP.Can(PermDevicesRead) {
		t.Fatal("nil principal must not have permissions")
	}
}
