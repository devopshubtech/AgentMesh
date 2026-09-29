// Command quiccheck makes one HTTP/3 (QUIC) request and prints the response
// body. Used by tests/exittun to verify that QUIC works through the exit node
// the way Chrome uses it. Separate module so quic-go stays out of the main one.
//
//	quiccheck https://cloudflare.com/cdn-cgi/trace
package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/quic-go/quic-go/http3"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: quiccheck URL")
		os.Exit(2)
	}
	rt := &http3.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13}}
	defer rt.Close()
	c := &http.Client{Transport: rt, Timeout: 20 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, os.Args[1], nil)
	resp, err := c.Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "http3 request failed:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	fmt.Printf("proto=%s status=%d\n%s", resp.Proto, resp.StatusCode, b)
}
