package screen

import (
	"context"
	"errors"
	"fmt"
	"image/png"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"ipscan/internal/app"
	"ipscan/internal/model"
	"ipscan/internal/probe"
	"ipscan/internal/scan"
	"ipscan/internal/subscription"
)

type fakeFetcher struct {
	body []byte
	err  error
}

func (f fakeFetcher) Fetch(context.Context, string) ([]byte, error) {
	return f.body, f.err
}

type fakeScanner struct{}

func (fakeScanner) Scan(ctx context.Context, nodes []model.Node, progress func(scan.Progress)) ([]probe.Result, error) {
	results := make([]probe.Result, len(nodes))
	for i, n := range nodes {
		r := probe.Result{
			IPs: []string{"203.0.113.10"}, IP: "203.0.113.10", Family: "IPv4",
			DNS: 12 * time.Millisecond, Reachable: true, Samples: 3, Succeeded: 3,
			Min: 38 * time.Millisecond, Avg: 42 * time.Millisecond, Max: 49 * time.Millisecond,
			Jitter: 6 * time.Millisecond, Score: 92, Grade: "优秀",
			Geo: &probe.Geo{Country: "美国", City: "洛杉矶", ISP: "Cloudflare", ASN: "AS13335", Hosting: true},
		}
		switch {
		case strings.Contains(n.Name, "香港"):
			r.Geo = &probe.Geo{Country: "中国", City: "香港", ISP: "CMI", ASN: "AS58453", Hosting: true}
			r.TLS = &probe.TLSInfo{Version: "TLS 1.3", Cipher: "TLS_AES_128_GCM_SHA256", CommonName: "hk.example.com", Issuer: "R10", NotAfter: time.Now().Add(60 * 24 * time.Hour), Verified: true}
			r.TLSMs = 55 * time.Millisecond
		case strings.Contains(n.Name, "美国"):
			r.Avg, r.Min, r.Max, r.Jitter = 168*time.Millisecond, 150*time.Millisecond, 190*time.Millisecond, 18*time.Millisecond
			r.Score, r.Grade = 74, "良好"
		case strings.Contains(n.Name, "日本"):
			r.Avg, r.Min, r.Max, r.Jitter = 96*time.Millisecond, 88*time.Millisecond, 110*time.Millisecond, 9*time.Millisecond
			r.Score, r.Grade = 82, "良好"
			r.Geo = &probe.Geo{Country: "日本", City: "东京", ISP: "NTT", ASN: "AS2914"}
		case strings.Contains(n.Name, "德国"):
			r.Reachable = false
			r.UDPOnly = true
			r.Err = "QUIC/UDP 协议，TCP 端口正常不监听，仅展示 IP 质量"
			r.Score, r.Grade = 0, "未探测"
			r.Geo = &probe.Geo{Country: "德国", City: "法兰克福", ISP: "Hetzner", ASN: "AS24940", Hosting: true, Proxy: true}
		}
		results[i] = r
		progress(scan.Progress{Done: i + 1, Total: len(nodes), Index: i, Result: r})
	}
	return results, nil
}

const fixture = `
proxies:
  - name: 香港 WS
    type: vmess
    server: hk.example.com
    port: 443
    uuid: 11111111-1111-1111-1111-111111111111
    cipher: auto
    tls: true
    network: ws
    ws-opts:
      path: /ray
      headers:
        Host: cdn.example.com
  - name: 美国 Reality
    type: vless
    server: us.example.com
    port: 8443
    uuid: 22222222-2222-2222-2222-222222222222
    flow: xtls-rprx-vision
    tls: true
    reality-opts:
      public-key: PUBLICKEY
      short-id: 1a2b
  - name: 日本 SS
    type: ss
    server: jp.example.com
    port: 8388
    cipher: aes-256-gcm
    password: sspass
  - name: 德国 HY2
    type: hysteria2
    server: de.example.com
    port: 443
    password: hypass
    sni: de.example.com
    skip-cert-verify: true
`

func newTestApp() *app.App {
	a := app.New(fakeFetcher{body: []byte(fixture)}, fakeScanner{})
	*a.Input = "https://example.com/sub"
	return a
}

func texts(t *ui.Tester) string {
	return strings.Join(t.Texts(), "\n")
}

func waitFor(t *testing.T, tt *ui.Tester, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		tt.Advance(20 * time.Millisecond)
		if strings.Contains(texts(tt), want) {
			return
		}
	}
	t.Fatalf("timed out waiting for %q; texts:\n%s", want, texts(tt))
}

func snapshot(t *testing.T, tt *ui.Tester, name string) string {
	t.Helper()
	path := filepath.Join(os.TempDir(), name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestViewShowsNodesAndDetail(t *testing.T) {
	a := newTestApp()
	st := &State{}
	tt := ui.NewTester(func(c *ui.Context) { View(c, a, st) }, 1280, 840)
	tt.Frame()

	if !strings.Contains(texts(tt), "粘贴订阅链接，开始节点体检") {
		t.Fatalf("empty state missing:\n%s", texts(tt))
	}

	a.Load()
	waitFor(t, tt, "3 个 TCP 可达")

	body := texts(tt)
	for _, want := range []string{"香港 WS", "美国 Reality", "日本 SS", "德国 HY2", "优秀", "TLS · WS", "中国 香港", "Cloudflare"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in view:\n%s", want, body)
		}
	}
	path := snapshot(t, tt, "ipscan-light.png")
	t.Logf("snapshot: %s", path)

	// Clicking a row opens the detail drawer.
	if err := tt.Click("香港 WS"); err != nil {
		t.Fatalf("click row: %v", err)
	}
	tt.Advance(400 * time.Millisecond)
	tt.Frame()
	t.Logf("elapsed=%v status=%v", a.Snapshot().Elapsed, a.Snapshot().Status)
	detail := texts(tt)
	for _, want := range []string{"连接质量", "节点参数", "证书 CN", "TLS 1.3", "/ray"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("detail missing %q:\n%s", want, detail)
		}
	}
	_ = snapshot(t, tt, "ipscan-detail.png")

	tt.SetDark(true)
	tt.Advance(400 * time.Millisecond)
	_ = snapshot(t, tt, "ipscan-dark.png")
}

func TestFilterAndSearch(t *testing.T) {
	a := newTestApp()
	st := &State{}
	tt := ui.NewTester(func(c *ui.Context) { View(c, a, st) }, 1280, 840)
	tt.Frame()
	a.Load()
	waitFor(t, tt, "3 个 TCP 可达")

	if err := tt.Click("需改善 1"); err != nil {
		t.Fatalf("click filter: %v", err)
	}
	tt.Frame()
	body := texts(tt)
	if !strings.Contains(body, "德国 HY2") || strings.Contains(body, "香港 WS") {
		t.Fatalf("filter did not narrow the table:\n%s", body)
	}

	st.Filter = 0
	st.Query = "日本"
	tt.Frame()
	body = texts(tt)
	if !strings.Contains(body, "日本 SS") || strings.Contains(body, "美国 Reality") {
		t.Fatalf("search did not narrow the table:\n%s", body)
	}
}

func TestNarrowWindowLongList(t *testing.T) {
	var yaml strings.Builder
	yaml.WriteString("proxies:\n")
	for i := 1; i <= 24; i++ {
		fmt.Fprintf(&yaml, "  - {name: 节点%02d, type: trojan, server: h%02d.example.com, port: 443, password: pw}\n", i, i)
	}
	a := app.New(fakeFetcher{body: []byte(yaml.String())}, fakeScanner{})
	*a.Input = "https://example.com/long"
	st := &State{}
	tt := ui.NewTester(func(c *ui.Context) { View(c, a, st) }, 1180, 700)
	tt.Frame()
	a.Load()
	waitFor(t, tt, "24 个 TCP 可达")

	_ = snapshot(t, tt, "ipscan-narrow.png")
	tt.Wheel(590, 440, 0, 600)
	tt.Advance(200 * time.Millisecond)
	_ = snapshot(t, tt, "ipscan-scrolled.png")

	if err := tt.Click("节点13"); err != nil {
		t.Fatalf("click row: %v", err)
	}
	tt.Advance(400 * time.Millisecond)
	if body := texts(tt); !strings.Contains(body, "节点参数") {
		t.Fatalf("detail did not open:\n%s", body[:min(len(body), 600)])
	}
}

func TestErrorState(t *testing.T) {
	a := app.New(fakeFetcher{err: errors.New("订阅下载失败: 连接超时")}, fakeScanner{})
	*a.Input = "https://example.com/sub"
	st := &State{}
	tt := ui.NewTester(func(c *ui.Context) { View(c, a, st) }, 1180, 700)
	tt.Frame()
	a.Load()
	waitFor(t, tt, "连接超时")
	_ = snapshot(t, tt, "ipscan-error.png")
}

type geoErrorScanner struct{}

func (geoErrorScanner) Scan(ctx context.Context, nodes []model.Node, progress func(scan.Progress)) ([]probe.Result, error) {
	results := make([]probe.Result, len(nodes))
	for i := range nodes {
		r := probe.Result{IP: "203.0.113.10", Family: "IPv4", Reachable: true, Samples: 3, Succeeded: 3,
			Avg: 80 * time.Millisecond, Score: 80, Grade: "良好"}
		results[i] = r
		progress(scan.Progress{Done: i + 1, Total: len(nodes), Index: i, Result: r})
	}
	return results, errors.New("ip-api 查询失败: HTTP 429")
}

func TestGeoFailureShowsReason(t *testing.T) {
	a := app.New(fakeFetcher{body: []byte(fixture)}, geoErrorScanner{})
	*a.Input = "https://example.com/sub"
	st := &State{}
	tt := ui.NewTester(func(c *ui.Context) { View(c, a, st) }, 1280, 840)
	tt.Frame()
	a.Load()
	waitFor(t, tt, "归属查询失败")
	if body := texts(tt); !strings.Contains(body, "检测完成：4 个节点") {
		t.Fatalf("scan result should still be shown:\n%s", body)
	}
}

// TestEndToEndLocalNode goes through the real fetcher, parser and prober
// against a loopback listener, so the wiring is covered without the network.
func TestEndToEndLocalNode(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port

	file := filepath.Join(t.TempDir(), "sub.txt")
	content := fmt.Sprintf("trojan://pw@127.0.0.1:%d?sni=local.test#本地节点\n", port)
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	prober := probe.New()
	prober.Options = probe.Options{Samples: 2, Timeout: 500 * time.Millisecond, DNS: time.Second}
	controller := app.New(subscription.NewFetcher(), &scan.Scanner{Prober: prober, Workers: 2})
	*controller.Input = file

	st := &State{}
	tt := ui.NewTester(func(c *ui.Context) { View(c, controller, st) }, 1180, 700)
	tt.Frame()
	controller.Load()
	waitFor(t, tt, "1 个 TCP 可达")

	body := texts(tt)
	for _, want := range []string{"本地节点", "127.0.0.1:" + fmt.Sprint(port), "TROJAN", "TLS"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q after real probe:\n%s", want, body)
		}
	}
}
