package probe

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"ipscan/internal/model"
)

func liveListener(t *testing.T) (int, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				close(done)
				return
			}
			_ = conn.Close()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, func() {
		_ = ln.Close()
		<-done
	}
}

func deadPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

func TestProbeReachable(t *testing.T) {
	port, closeFn := liveListener(t)
	defer closeFn()

	p := New()
	p.Options = Options{Samples: 3, Timeout: time.Second, DNS: time.Second}
	res := p.Probe(context.Background(), model.Node{Type: "vless", Server: "127.0.0.1", Port: port})

	if !res.Reachable || res.Succeeded != 3 || res.Loss != 0 {
		t.Fatalf("result = %+v", res)
	}
	if res.Avg <= 0 || res.Avg > time.Second {
		t.Fatalf("avg = %v", res.Avg)
	}
	if res.Family != "IPv4" || res.IP == "" {
		t.Fatalf("ip = %q family = %q", res.IP, res.Family)
	}
	if res.Score <= 0 || res.Grade == "" || res.Grade == "不可达" {
		t.Fatalf("score = %d grade = %q", res.Score, res.Grade)
	}
}

func TestProbeClosedPort(t *testing.T) {
	port := deadPort(t)
	p := New()
	p.Options = Options{Samples: 2, Timeout: 500 * time.Millisecond, DNS: time.Second}
	res := p.Probe(context.Background(), model.Node{Type: "trojan", Server: "127.0.0.1", Port: port})

	if res.Reachable {
		t.Fatalf("closed port reported reachable: %+v", res)
	}
	if res.Grade != "不可达" || res.Err == "" {
		t.Fatalf("grade = %q err = %q", res.Grade, res.Err)
	}
	if res.Score != 0 {
		t.Fatalf("score = %d", res.Score)
	}
}

func TestProbeUDPNodeNotScoredDown(t *testing.T) {
	port := deadPort(t)
	p := New()
	p.Options = Options{Samples: 1, Timeout: 500 * time.Millisecond, DNS: time.Second}
	res := p.Probe(context.Background(), model.Node{Type: "hysteria2", Server: "127.0.0.1", Port: port, Network: "quic"})

	if !res.UDPOnly || res.Grade != "未探测" {
		t.Fatalf("result = %+v", res)
	}
}

func TestProbeTLS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	host, portStr, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portStr)

	p := New()
	p.Options = Options{Samples: 1, Timeout: 2 * time.Second, DNS: time.Second}
	res := p.Probe(context.Background(), model.Node{
		Type: "trojan", Server: host, Port: port, Security: "tls", ServerName: "example.com",
	})

	if res.TLS == nil {
		t.Fatalf("no TLS info: %+v", res)
	}
	if res.TLS.Error != "" {
		t.Fatalf("tls error: %s", res.TLS.Error)
	}
	if res.TLS.Version == "" || res.TLS.CommonName == "" || res.TLS.NotAfter.IsZero() {
		t.Fatalf("tls info incomplete: %+v", res.TLS)
	}
	// httptest's certificate is issued for example.com, so hostname checking
	// must pass even though the chain is self-signed.
	if !res.TLS.Verified {
		t.Fatalf("expected hostname verification to pass: %+v", res.TLS)
	}
	_ = tls.VersionTLS12
}

func TestIPAPILookup(t *testing.T) {
	var requested []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ips []string
		_ = json.NewDecoder(r.Body).Decode(&ips)
		requested = ips
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"query": "8.8.8.8", "status": "success", "country": "美国",
			"city": "山景城", "isp": "Google LLC", "as": "AS15169 Google LLC",
			"proxy": false, "hosting": true, "mobile": false,
		}})
	}))
	defer srv.Close()

	api := &IPAPI{Endpoint: srv.URL, Client: srv.Client()}
	got, err := api.Lookup(context.Background(), []string{"8.8.8.8", "127.0.0.1", "8.8.8.8"})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if len(requested) != 1 || requested[0] != "8.8.8.8" {
		t.Fatalf("requested = %v", requested)
	}
	geo, ok := got["8.8.8.8"]
	if !ok || geo.Country != "美国" || geo.ASN == "" || !geo.Hosting {
		t.Fatalf("geo = %+v", geo)
	}
}
