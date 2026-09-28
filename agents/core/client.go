package core

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/enfec/agentmesh/protocols/agentapi"
)

// APIError is an error response from the gateway.
type APIError struct {
	Status     int
	Code       string
	Message    string
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	return fmt.Sprintf("server returned %d %s: %s", e.Status, e.Code, e.Message)
}

// IsCode reports whether err is an APIError with code.
func IsCode(err error, code string) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Code == code
}

// client talks to the agent-gateway HTTPS endpoints.
type client struct {
	base string
	http *http.Client
}

func newHTTPClient(cfg *Config) (*http.Client, error) {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if cfg.CAFile != "" {
		pem, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read CA file: %w", err)
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("CA file contains no certificates")
		}
	}
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment, // corporate proxies via HTTPS_PROXY
		DialContext:           (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSClientConfig:       &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		MaxIdleConns:          4,
		IdleConnTimeout:       90 * time.Second,
		ForceAttemptHTTP2:     false, // WebSocket upgrade needs HTTP/1.1
	}
	return &http.Client{Transport: tr, Timeout: 60 * time.Second}, nil
}

func newClient(cfg *Config) (*client, error) {
	hc, err := newHTTPClient(cfg)
	if err != nil {
		return nil, err
	}
	return &client{base: cfg.ServerURL, http: hc}, nil
}

func (c *client) post(ctx context.Context, path string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "agentmesh-agent/"+Version)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		return parseAPIError(resp, data)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(data, out)
}

func parseAPIError(resp *http.Response, data []byte) error {
	ae := &APIError{Status: resp.StatusCode, Code: "http_error", Message: http.StatusText(resp.StatusCode)}
	var eb agentapi.ErrorBody
	if json.Unmarshal(data, &eb) == nil && eb.Error.Code != "" {
		ae.Code, ae.Message = eb.Error.Code, eb.Error.Message
	}
	if ra := resp.Header.Get("Retry-After"); ra != "" {
		if d, err := time.ParseDuration(ra + "s"); err == nil {
			ae.RetryAfter = d
		}
	}
	return ae
}

// token performs the challenge/response and returns a device access token.
func (c *client) token(ctx context.Context, deviceID string, key *ecdsa.PrivateKey) (*agentapi.TokenResponse, error) {
	var ch agentapi.ChallengeResponse
	if err := c.post(ctx, agentapi.PathChallenge, agentapi.ChallengeRequest{DeviceID: deviceID}, &ch); err != nil {
		return nil, err
	}
	sig, err := ecdsa.SignASN1(rand.Reader, key, agentapi.AuthDigest(deviceID, ch.Nonce))
	if err != nil {
		return nil, err
	}
	var tok agentapi.TokenResponse
	if err := c.post(ctx, agentapi.PathToken, agentapi.TokenRequest{DeviceID: deviceID, Nonce: ch.Nonce, Signature: sig}, &tok); err != nil {
		return nil, err
	}
	return &tok, nil
}
