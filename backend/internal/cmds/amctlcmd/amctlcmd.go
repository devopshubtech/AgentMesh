// Package amctlcmd implements the amctl command, which is the AgentMesh operator CLI: key generation, development
// certificates and database migrations.
package amctlcmd

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/enfec/agentmesh/backend/internal/enrollment"
	"github.com/enfec/agentmesh/backend/internal/platform/db"
	"github.com/enfec/agentmesh/backend/internal/platform/keys"
	"github.com/enfec/agentmesh/backend/internal/platform/logging"
	"github.com/enfec/agentmesh/backend/internal/users"
	"github.com/enfec/agentmesh/backend/migrations"
)

const usage = `amctl — AgentMesh operator CLI

Usage:
  amctl keygen                         print fresh signing keys as env lines
  amctl dev-certs -out DIR [-hosts h1,h2]
                                       create a local CA and gateway TLS certificate
  amctl migrate up|status              apply / show migrations (uses AGENTMESH_DATABASE_URL)
  amctl set-admin                      create a super admin, or reset its password if the email exists
  amctl local-enroll-token [DESC]      print a single-use, auto-approved 15-minute enrollment token
                                       (the server installer uses it to make this machine an exit node)
                                       (AGENTMESH_ADMIN_EMAIL, AGENTMESH_ADMIN_PASSWORD, AGENTMESH_DATABASE_URL)
  amctl health URL                     exit 0 if URL answers 200 (container health checks)
`

func health(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: amctl health URL")
	}
	c := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{
		// Health checks target the container itself; the dev CA is not in the image.
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
	}}
	resp, err := c.Get(args[0])
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

// Main runs the command (called from its own binary or the combined agentmesh-server).
func Main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "keygen":
		err = keygen()
	case "dev-certs":
		err = devCerts(os.Args[2:])
	case "local-enroll-token":
		err = localEnrollToken(os.Args[2:])
	case "set-admin":
		err = setAdmin()
	case "migrate":
		err = migrate(os.Args[2:])
	case "health":
		err = health(os.Args[2:])
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func keygen() error {
	userSeed, _, err := keys.Generate()
	if err != nil {
		return err
	}
	deviceSeed, _, err := keys.Generate()
	if err != nil {
		return err
	}
	cmdSeed, cmdPub, err := keys.Generate()
	if err != nil {
		return err
	}
	fmt.Println("# AgentMesh signing keys — generated", time.Now().UTC().Format(time.RFC3339))
	fmt.Println("# Keep these secret. Rotating AGENTMESH_COMMAND_SIGNING_KEY requires re-enrolling agents (they pin its public key).")
	fmt.Println("AGENTMESH_USER_TOKEN_KEY=" + userSeed)
	fmt.Println("AGENTMESH_DEVICE_TOKEN_KEY=" + deviceSeed)
	fmt.Println("AGENTMESH_COMMAND_SIGNING_KEY=" + cmdSeed)
	fmt.Println("AGENTMESH_COMMAND_PUBLIC_KEY=" + cmdPub)
	return nil
}

func writePEM(path, typ string, der []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer f.Close()
	return pem.Encode(f, &pem.Block{Type: typ, Bytes: der})
}

func devCerts(args []string) error {
	fs := flag.NewFlagSet("dev-certs", flag.ExitOnError)
	out := fs.String("out", "certs", "output directory")
	hosts := fs.String("hosts", "localhost,127.0.0.1,::1,host.docker.internal,agent-gateway", "comma-separated SANs")
	_ = fs.Parse(args)
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}

	serial := func() *big.Int { n, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127)); return n }
	now := time.Now()
	// Reuse an existing CA so already-enrolled agents (which copied ca.pem)
	// keep trusting the re-issued gateway certificate.
	caKey, ca, reused, err := loadCA(*out)
	if err != nil {
		return err
	}
	if !reused {
		if caKey, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader); err != nil {
			return err
		}
		caTmpl := &x509.Certificate{
			SerialNumber: serial(), Subject: pkix.Name{CommonName: "AgentMesh Dev CA", Organization: []string{"AgentMesh (development)"}},
			NotBefore: now.Add(-time.Hour), NotAfter: now.AddDate(2, 0, 0),
			KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign, BasicConstraintsValid: true, IsCA: true, MaxPathLenZero: true,
		}
		caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
		if err != nil {
			return err
		}
		ca, _ = x509.ParseCertificate(caDER)
	}

	srvKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	srvTmpl := &x509.Certificate{
		SerialNumber: serial(), Subject: pkix.Name{CommonName: "agentmesh-gateway"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.AddDate(1, 0, 0),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	for _, h := range strings.Split(*hosts, ",") {
		h = strings.TrimSpace(h)
		if ip := net.ParseIP(h); ip != nil {
			srvTmpl.IPAddresses = append(srvTmpl.IPAddresses, ip)
		} else if h != "" {
			srvTmpl.DNSNames = append(srvTmpl.DNSNames, h)
		}
	}
	srvDER, err := x509.CreateCertificate(rand.Reader, srvTmpl, ca, &srvKey.PublicKey, caKey)
	if err != nil {
		return err
	}
	srvKeyDER, _ := x509.MarshalECPrivateKey(srvKey)
	if !reused {
		caKeyDER, _ := x509.MarshalECPrivateKey(caKey)
		if err := writePEM(filepath.Join(*out, "ca.pem"), "CERTIFICATE", ca.Raw, 0o644); err != nil {
			return err
		}
		if err := writePEM(filepath.Join(*out, "ca-key.pem"), "EC PRIVATE KEY", caKeyDER, 0o600); err != nil {
			return err
		}
	}
	if err := writePEM(filepath.Join(*out, "gateway.pem"), "CERTIFICATE", srvDER, 0o644); err != nil {
		return err
	}
	if err := writePEM(filepath.Join(*out, "gateway-key.pem"), "EC PRIVATE KEY", srvKeyDER, 0o644); err != nil {
		return err
	}
	state := "created new CA"
	if reused {
		state = "reused existing CA"
	}
	fp := sha256.Sum256(ca.Raw)
	fmt.Printf("wrote %s/gateway.pem (SANs: %s); %s\n", *out, *hosts, state)
	fmt.Printf("CA SHA-256 fingerprint: %s\n", colonHex(fp[:]))
	fmt.Println("agents must be given ca.pem via --ca-file; the Android app shows this fingerprint when trusting the server")
	return nil
}

func colonHex(b []byte) string {
	parts := make([]string, len(b))
	for i, x := range b {
		parts[i] = fmt.Sprintf("%02X", x)
	}
	return strings.Join(parts, ":")
}

func loadCA(dir string) (*ecdsa.PrivateKey, *x509.Certificate, bool, error) {
	certPEM, err1 := os.ReadFile(filepath.Join(dir, "ca.pem"))
	keyPEM, err2 := os.ReadFile(filepath.Join(dir, "ca-key.pem"))
	if err1 != nil || err2 != nil {
		return nil, nil, false, nil
	}
	cb, _ := pem.Decode(certPEM)
	kb, _ := pem.Decode(keyPEM)
	if cb == nil || kb == nil {
		return nil, nil, false, fmt.Errorf("existing CA files in %s are not valid PEM", dir)
	}
	cert, err := x509.ParseCertificate(cb.Bytes)
	if err != nil {
		return nil, nil, false, err
	}
	key, err := x509.ParseECPrivateKey(kb.Bytes)
	if err != nil {
		return nil, nil, false, err
	}
	return key, cert, true, nil
}

func localEnrollToken(args []string) error {
	url := os.Getenv("AGENTMESH_DATABASE_URL")
	if url == "" {
		return fmt.Errorf("AGENTMESH_DATABASE_URL is not set")
	}
	desc := "This server (installer)"
	if len(args) > 0 {
		desc = strings.Join(args, " ")
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, url)
	if err != nil {
		return err
	}
	defer pool.Close()
	secret, err := enrollment.CreateLocalToken(ctx, pool, desc)
	if err != nil {
		return err
	}
	fmt.Println(secret)
	return nil
}

// setAdmin reads the credentials from the environment, not argv, so the
// password does not show up in the process list.
func setAdmin() error {
	url := os.Getenv("AGENTMESH_DATABASE_URL")
	email, password := os.Getenv("AGENTMESH_ADMIN_EMAIL"), os.Getenv("AGENTMESH_ADMIN_PASSWORD")
	if url == "" || email == "" || password == "" {
		return fmt.Errorf("set AGENTMESH_DATABASE_URL, AGENTMESH_ADMIN_EMAIL and AGENTMESH_ADMIN_PASSWORD")
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, url)
	if err != nil {
		return err
	}
	defer pool.Close()
	created, err := users.SetAdmin(ctx, pool, email, password)
	if err != nil {
		return err
	}
	if created {
		fmt.Println("created super admin", email)
	} else {
		fmt.Println("reset the password of", email, "(super admin; other sessions signed out)")
	}
	return nil
}

func migrate(args []string) error {
	if len(args) != 1 || (args[0] != "up" && args[0] != "status") {
		return fmt.Errorf("usage: amctl migrate up|status")
	}
	url := os.Getenv("AGENTMESH_DATABASE_URL")
	if url == "" {
		return fmt.Errorf("AGENTMESH_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, url)
	if err != nil {
		return err
	}
	defer pool.Close()
	if args[0] == "up" {
		return migrations.Up(ctx, pool, logging.New("amctl", "info"))
	}
	cur, latest, err := migrations.Status(ctx, pool)
	if err != nil {
		return err
	}
	fmt.Printf("schema version %d (latest available %d)\n", cur, latest)
	return nil
}
