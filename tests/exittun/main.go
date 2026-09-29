//go:build linux

// Command exittun exercises the Android exit-node engine on Linux with a real
// TUN device: it logs in, opens an exit session to a device, routes selected
// destinations into the TUN and runs real traffic through the agent.
//
// Run in a container with NET_ADMIN and /dev/net/tun (see README).
package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/enfec/agentmesh/mobile/tunnel"
)

func must(err error, what string) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "FAIL %s: %v\n", what, err)
		os.Exit(1)
	}
}

func post(url, token string, body any, out any) error {
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := apiClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		var e map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&e)
		return fmt.Errorf("HTTP %d: %v", resp.StatusCode, e)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

var apiClient = http.DefaultClient

func openTun(name string) (int, error) {
	fd, err := unix.Open("/dev/net/tun", unix.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}
	var ifr [unix.IFNAMSIZ + 64]byte
	copy(ifr[:], name)
	*(*uint16)(unsafe.Pointer(&ifr[unix.IFNAMSIZ])) = unix.IFF_TUN | unix.IFF_NO_PI
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.TUNSETIFF), uintptr(unsafe.Pointer(&ifr[0]))); errno != 0 {
		unix.Close(fd)
		return -1, errno
	}
	return fd, nil
}

func sh(args ...string) (string, error) {
	out, err := exec.Command(args[0], args[1:]...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func main() {
	api := flag.String("api", "http://control-api:8080", "control-api URL")
	email := flag.String("email", os.Getenv("AM_ADMIN_EMAIL"), "operator email")
	password := flag.String("password", os.Getenv("AM_ADMIN_PASSWORD"), "operator password")
	device := flag.String("device", "", "exit-node device id")
	relay := flag.String("relay", "", "override relay URL returned by the API")
	caFile := flag.String("ca", "", "CA PEM for the gateway")
	lanProbe := flag.String("lan-probe", "192.168.30.1", "private address that the agent must refuse")
	dnsServer := flag.String("dns", "1.1.1.1", "public DNS server to query through the tunnel (must be routed into the TUN)")
	hold := flag.Duration("hold", 0, "keep the session connected this long after the checks (for UI demos)")
	link := flag.String("link", "", "connect link (.../join.html#k=...&r=...): use the no-login connect-key flow like the Android app")
	stale := flag.Bool("stale-server", false, "with -link: start from a dead address to exercise the rendezvous lookup")
	bench := flag.String("bench", "", "host whose /__down endpoint is downloaded through the tunnel to measure throughput (e.g. speed.cloudflare.com)")
	flag.Parse()
	if *caFile != "" {
		pem, err := os.ReadFile(*caFile)
		must(err, "read CA")
		pool := x509.NewCertPool()
		pool.AppendCertsFromPEM(pem)
		apiClient = &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}}
	}

	var sess struct {
		Session struct {
			ID string `json:"id"`
		} `json:"session"`
		RelayURL string `json:"relay_url"`
		Ticket   string `json:"ticket"`
	}
	if *link != "" {
		server, relayURL, ticket, id := connectWithLink(*link, *stale)
		sess.Session.ID, sess.RelayURL, sess.Ticket = id, relayURL, ticket
		fmt.Println("connected via link to", server)
	} else {
		var login struct {
			AccessToken string `json:"access_token"`
		}
		must(post(*api+"/v1/auth/login", "", map[string]string{"email": *email, "password": *password, "client": "mobile"}, &login), "login")
		must(post(*api+"/v1/devices/"+*device+"/exit-sessions", login.AccessToken, map[string]string{"client_label": "Android Samsung SM-S918B (test)"}, &sess), "create exit session")
	}
	if *relay != "" {
		sess.RelayURL = *relay
	}
	fmt.Println("session", sess.Session.ID, "relay", sess.RelayURL)

	fd, err := openTun("amtun0")
	must(err, "open tun")
	for _, cmd := range [][]string{
		{"ip", "addr", "add", "10.111.0.2/24", "dev", "amtun0"},
		{"ip", "link", "set", "amtun0", "up", "mtu", "1500"},
		{"ip", "route", "add", "1.1.1.1/32", "dev", "amtun0"},
		{"ip", "route", "add", "8.8.8.8/32", "dev", "amtun0"},
		{"ip", "route", "add", *lanProbe + "/32", "dev", "amtun0"},
	} {
		out, err := sh(cmd...)
		must(err, strings.Join(cmd, " ")+": "+out)
	}
	ca := ""
	if *caFile != "" {
		b, err := os.ReadFile(*caFile)
		must(err, "read CA")
		ca = string(b)
	}
	eng := tunnel.NewEngine()
	must(eng.Start(fd, sess.RelayURL, sess.Ticket, ca, 1500), "start engine")
	fmt.Println("tunnel up")

	ok := true
	check := func(name string, cond bool, detail string) {
		mark := "ok  "
		if !cond {
			mark, ok = "FAIL", false
		}
		fmt.Printf("%s %s: %s\n", mark, name, detail)
	}
	trace, err := sh("curl", "-s", "--max-time", "15", "https://1.1.1.1/cdn-cgi/trace")
	ip := ""
	for _, l := range strings.Split(trace, "\n") {
		if strings.HasPrefix(l, "ip=") {
			ip = strings.TrimPrefix(l, "ip=")
		}
	}
	check("TCP/TLS via exit node (Cloudflare sees this IP)", err == nil && ip != "", ip)
	dns, err := sh("nslookup", "example.com", *dnsServer)
	check("UDP DNS via exit node", err == nil && strings.Contains(dns, "Address"), firstLine(dns, "Address:", 2)+" | raw: "+strings.ReplaceAll(dns, "\n", " / "))
	_, err = sh("curl", "-s", "--max-time", "6", "http://"+*lanProbe+"/")
	check("private LAN destination refused by agent", err != nil, fmt.Sprintf("failed_flows=%d", eng.FailedFlows()))
	check("counters", eng.BytesDown() > 0 && eng.Flows() >= 2,
		fmt.Sprintf("flows=%d up=%dB down=%dB", eng.Flows(), eng.BytesUp(), eng.BytesDown()))

	if *bench != "" {
		ips, _ := net.LookupHost(*bench)
		for _, ip := range ips {
			if strings.Contains(ip, ".") {
				_, _ = sh("ip", "route", "add", ip+"/32", "dev", "amtun0")
			}
		}
		for i := 0; i < 2; i++ {
			out, err := sh("curl", "-s", "--max-time", "60", "-o", "/dev/null", "-w", "%{speed_download} %{time_total}", "https://"+*bench+"/__down?bytes=25000000")
			var bps, secs float64
			fmt.Sscanf(out, "%f %f", &bps, &secs)
			check("throughput via exit node", err == nil && bps > 0, fmt.Sprintf("%.1f Mbit/s (25 MB in %.1fs)", bps*8/1e6, secs))
		}
	}
	if *hold > 0 {
		fmt.Println("holding session with background load for", *hold)
		stop := time.Now().Add(*hold)
		var okN, errN atomic.Int64
		var wg sync.WaitGroup
		for w := 0; w < 20; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for time.Now().Before(stop) {
					if _, err := sh("curl", "-s", "--max-time", "10", "-o", "/dev/null", "https://1.1.1.1/cdn-cgi/trace"); err != nil {
						errN.Add(1)
					} else {
						okN.Add(1)
					}
					time.Sleep(200 * time.Millisecond)
				}
			}()
		}
		wg.Wait()
		check("sustained load through relay", eng.IsRunning() && errN.Load() == 0, fmt.Sprintf("requests ok=%d failed=%d engine_running=%v last_error=%q", okN.Load(), errN.Load(), eng.IsRunning(), eng.LastError()))
	}
	eng.Stop()
	time.Sleep(time.Second)
	check("engine stopped", !eng.IsRunning(), "last_error="+eng.LastError())
	if !ok {
		os.Exit(1)
	}
	fmt.Println("ALL CHECKS PASSED")
}

func firstLine(s, prefix string, nth int) string {
	n := 0
	for _, l := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), prefix) {
			n++
			if n == nth {
				return strings.TrimSpace(l)
			}
		}
	}
	return ""
}

// connectWithLink mirrors the Android app: open a session with the link's
// connect key; if the saved address is dead, look up the current one via the
// rendezvous URL (GitHub gist API) and retry.
func connectWithLink(raw string, stale bool) (server, relayURL, ticket, sessionID string) {
	u, err := url.Parse(raw)
	must(err, "parse link")
	frag, _ := url.ParseQuery(u.Fragment)
	key, rdv := frag.Get("k"), frag.Get("r")
	server = "https://" + u.Host
	if stale {
		server = "https://agentmesh-stale-address-test.trycloudflare.com"
	}
	open := func(srv string) (map[string]any, error) {
		req, _ := http.NewRequest("POST", srv+"/v1/connect/session", strings.NewReader(`{"client_label":"Android OnePlus (link test)"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-AgentMesh-Connect-Key", key)
		c := &http.Client{Timeout: 20 * time.Second}
		resp, err := c.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		if resp.StatusCode != 201 {
			return out, fmt.Errorf("HTTP %d %v", resp.StatusCode, out)
		}
		return out, nil
	}
	out, err := open(server)
	if err != nil {
		fmt.Println("saved address failed (", err, ") -> rendezvous lookup")
		req, _ := http.NewRequest("GET", rdv, nil)
		req.Header.Set("Accept", "application/vnd.github+json")
		resp, rerr := http.DefaultClient.Do(req)
		must(rerr, "rendezvous")
		var g struct {
			Files map[string]struct {
				Content string `json:"content"`
			} `json:"files"`
		}
		must(json.NewDecoder(resp.Body).Decode(&g), "decode rendezvous")
		resp.Body.Close()
		var doc struct {
			URL string `json:"url"`
		}
		must(json.Unmarshal([]byte(g.Files["agentmesh-endpoint.json"].Content), &doc), "decode endpoint")
		fmt.Println("rendezvous says current address is", doc.URL)
		server = doc.URL
		out, err = open(server)
	}
	must(err, "open session with connect key")
	return server, server + "/v1/relay", out["ticket"].(string), out["session_id"].(string)
}
