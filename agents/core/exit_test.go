package core

import (
	"context"
	"errors"
	"net/netip"
	"testing"
)

func TestDestinationAllowed(t *testing.T) {
	cases := map[string][2]bool{ // addr: {allowed without LAN, allowed with LAN}
		"1.1.1.1":         {true, true},
		"8.8.8.8":         {true, true},
		"2606:4700::1111": {true, true},
		"127.0.0.1":       {false, false},
		"::1":             {false, false},
		"169.254.169.254": {false, false}, // cloud metadata
		"fe80::1":         {false, false},
		"0.0.0.0":         {false, false},
		"0.1.2.3":         {false, false},
		"224.0.0.1":       {false, false},
		"255.255.255.255": {false, false},
		"10.0.0.5":        {false, true},
		"192.168.1.1":     {false, true},
		"172.16.0.1":      {false, true},
		"100.64.1.1":      {false, true},
		"fd00::1":         {false, true},
		"::ffff:10.0.0.1": {false, true},
	}
	for s, want := range cases {
		ip := netip.MustParseAddr(s)
		if got := destinationAllowed(ip, false); got != want[0] {
			t.Errorf("%s (no LAN): got %v want %v", s, got, want[0])
		}
		if got := destinationAllowed(ip, true); got != want[1] {
			t.Errorf("%s (LAN): got %v want %v", s, got, want[1])
		}
	}
}

func TestExitDialerBlocksResolvedLoopback(t *testing.T) {
	// "localhost" resolves to loopback: the control hook must reject it after resolution.
	_, err := exitDialer(false).DialContext(context.Background(), "tcp", "localhost:1")
	if !errors.Is(err, errDestinationDenied) {
		t.Fatalf("expected errDestinationDenied, got %v", err)
	}
}

func TestExitNodeCapabilityOptIn(t *testing.T) {
	if hasCap(newExecutor(Policy{}, &captureEmitter{}, nil).capabilities(), "exit_node") {
		t.Fatal("exit_node advertised without opt-in")
	}
	yes := true
	if !hasCap(newExecutor(Policy{AllowExitNode: &yes}, &captureEmitter{}, nil).capabilities(), "exit_node") {
		t.Fatal("exit_node not advertised after opt-in")
	}
}

func hasCap(caps []string, c string) bool {
	for _, x := range caps {
		if x == c {
			return true
		}
	}
	return false
}
