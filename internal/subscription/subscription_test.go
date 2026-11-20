package subscription

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustParse(t *testing.T, payload string) *Document {
	t.Helper()
	doc, err := Parse([]byte(payload))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return doc
}

func TestParseBase64URIList(t *testing.T) {
	vmessJSON, _ := json.Marshal(map[string]any{
		"v": "2", "ps": "东京 01", "add": "jp.example.com", "port": "443",
		"id": "11111111-1111-1111-1111-111111111111", "aid": "0", "scy": "auto",
		"net": "ws", "type": "none", "host": "jp.example.com", "path": "/ws",
		"tls": "tls", "sni": "jp.example.com", "fp": "chrome", "alpn": "h2,http/1.1",
	})
	lines := []string{
		"vmess://" + base64.StdEncoding.EncodeToString(vmessJSON),
		"vless://22222222-2222-2222-2222-222222222222@example.com:443?encryption=none&security=reality&sni=www.microsoft.com&fp=chrome&pbk=PUBKEY&sid=abcd&type=grpc&serviceName=gs#香港 02",
		"trojan://secret@1.2.3.4:8443?sni=t.example.com&allowInsecure=1#TJ",
		"ss://" + base64.StdEncoding.EncodeToString([]byte("aes-256-gcm:pw")) + "@1.2.3.4:8388#SS-1",
		"hysteria2://pw@hy.example.com:443?sni=hy.example.com&insecure=1&obfs=salamander&obfs-password=xyz#HY2",
	}
	payload := base64.StdEncoding.EncodeToString([]byte(strings.Join(lines, "\n")))
	doc := mustParse(t, payload)

	if doc.Format != "base64 → URI 列表" {
		t.Fatalf("format = %q", doc.Format)
	}
	if len(doc.Nodes) != 5 {
		t.Fatalf("nodes = %d, want 5 (warnings: %v)", len(doc.Nodes), doc.Warnings)
	}

	vm := doc.Nodes[0]
	if vm.Type != "vmess" || vm.Server != "jp.example.com" || vm.Port != 443 ||
		vm.Network != "ws" || vm.Security != "tls" || vm.Path != "/ws" ||
		vm.Fingerprint != "chrome" || vm.UUID == "" {
		t.Fatalf("vmess parsed wrong: %+v", vm)
	}

	vl := doc.Nodes[1]
	if vl.Security != "reality" || vl.PublicKey != "PUBKEY" || vl.ShortID != "abcd" ||
		vl.ServiceName != "gs" || vl.Network != "grpc" || vl.ServerName != "www.microsoft.com" {
		t.Fatalf("vless parsed wrong: %+v", vl)
	}

	tr := doc.Nodes[2]
	if tr.Password != "secret" || tr.Security != "tls" || !tr.Insecure || tr.ServerName != "t.example.com" {
		t.Fatalf("trojan parsed wrong: %+v", tr)
	}

	ss := doc.Nodes[3]
	if ss.Cipher != "aes-256-gcm" || ss.Password != "pw" || ss.Port != 8388 {
		t.Fatalf("ss parsed wrong: %+v", ss)
	}

	hy := doc.Nodes[4]
	if hy.Type != "hysteria2" || hy.Obfs != "salamander" || hy.ObfsPassword != "xyz" || !hy.Insecure {
		t.Fatalf("hysteria2 parsed wrong: %+v", hy)
	}
	if hy.Network != "quic" {
		t.Fatalf("hysteria2 network = %q", hy.Network)
	}
}

func TestParseLegacySSAndSSR(t *testing.T) {
	legacy := "ss://" + base64.RawURLEncoding.EncodeToString([]byte("aes-128-gcm:p@ss@5.6.7.8:443")) + "#旧版"
	doc := mustParse(t, legacy)
	n := doc.Nodes[0]
	if n.Server != "5.6.7.8" || n.Port != 443 || n.Password != "p@ss" || n.Name != "旧版" {
		t.Fatalf("legacy ss parsed wrong: %+v", n)
	}

	inner := "9.9.9.9:8989:origin:rc4-md5:plain:" + base64.RawURLEncoding.EncodeToString([]byte("secret")) + "/?remarks=" + base64.RawURLEncoding.EncodeToString([]byte("日本 SSR"))
	ssr := "ssr://" + base64.RawURLEncoding.EncodeToString([]byte(inner))
	doc = mustParse(t, ssr)
	n = doc.Nodes[0]
	if n.Type != "ssr" || n.Server != "9.9.9.9" || n.Port != 8989 || n.Password != "secret" || n.Name != "日本 SSR" {
		t.Fatalf("ssr parsed wrong: %+v", n)
	}
}

func TestParseClashYAML(t *testing.T) {
	payload := `
port: 7890
proxies:
  - name: 香港 WS
    type: vmess
    server: hk.example.com
    port: 443
    uuid: 33333333-3333-3333-3333-333333333333
    alterId: 0
    cipher: auto
    tls: true
    servername: hk.example.com
    network: ws
    ws-opts:
      path: /ray
      headers:
        Host: cdn.example.com
    skip-cert-verify: true
  - name: 美国 Reality
    type: vless
    server: us.example.com
    port: 8443
    uuid: 44444444-4444-4444-4444-444444444444
    flow: xtls-rprx-vision
    tls: true
    client-fingerprint: chrome
    reality-opts:
      public-key: REALITYKEY
      short-id: 1a2b
    network: tcp
  - name: 新加坡 SS
    type: ss
    server: sg.example.com
    port: 8388
    cipher: chacha20-ietf-poly1305
    password: sspass
    udp: true
  - name: 日本 HY2
    type: hysteria2
    server: jp.example.com
    port: 443
    password: hypass
    sni: jp.example.com
    skip-cert-verify: true
    obfs: salamander
    obfs-password: obfspw
  - name: 直连
    type: direct
proxy-groups:
  - name: auto
    type: url-test
`
	doc := mustParse(t, payload)
	if doc.Format != "Clash YAML" {
		t.Fatalf("format = %q", doc.Format)
	}
	if len(doc.Nodes) != 4 {
		t.Fatalf("nodes = %d: %+v", len(doc.Nodes), doc.Nodes)
	}

	hk := doc.Nodes[0]
	if hk.Type != "vmess" || hk.Network != "ws" || hk.Path != "/ray" || hk.Host != "cdn.example.com" ||
		hk.Security != "tls" || !hk.Insecure || hk.ServerName != "hk.example.com" {
		t.Fatalf("clash vmess wrong: %+v", hk)
	}

	us := doc.Nodes[1]
	if us.Security != "reality" || us.PublicKey != "REALITYKEY" || us.ShortID != "1a2b" || us.Flow != "xtls-rprx-vision" {
		t.Fatalf("clash vless wrong: %+v", us)
	}

	sg := doc.Nodes[2]
	if sg.Cipher != "chacha20-ietf-poly1305" || sg.Password != "sspass" {
		t.Fatalf("clash ss wrong: %+v", sg)
	}

	hy := doc.Nodes[3]
	if hy.Type != "hysteria2" || hy.Network != "quic" || hy.Obfs != "salamander" || hy.ObfsPassword != "obfspw" {
		t.Fatalf("clash hysteria2 wrong: %+v", hy)
	}
}

func TestParseXrayJSON(t *testing.T) {
	payload := `{
	  "outbounds": [
	    {
	      "tag": "reality-node",
	      "protocol": "vless",
	      "settings": {"vnext": [{"address": "x.example.com", "port": 443,
	        "users": [{"id": "55555555-5555-5555-5555-555555555555", "flow": "xtls-rprx-vision", "encryption": "none"}]}]},
	      "streamSettings": {
	        "network": "tcp",
	        "security": "reality",
	        "realitySettings": {"serverName": "www.apple.com", "publicKey": "PK", "shortId": "cc", "fingerprint": "chrome"}
	      }
	    },
	    {
	      "tag": "ws-node",
	      "protocol": "vmess",
	      "settings": {"vnext": [{"address": "y.example.com", "port": 8080,
	        "users": [{"id": "66666666-6666-6666-6666-666666666666", "alterId": 0, "security": "auto"}]}]},
	      "streamSettings": {"network": "ws", "security": "tls",
	        "tlsSettings": {"serverName": "y.example.com", "allowInsecure": true, "alpn": ["h2"]},
	        "wsSettings": {"path": "/vm", "headers": {"Host": "y.example.com"}}}
	    },
	    {"tag": "direct", "protocol": "freedom"}
	  ]
	}`
	doc := mustParse(t, payload)
	if doc.Format != "Xray JSON" {
		t.Fatalf("format = %q", doc.Format)
	}
	if len(doc.Nodes) != 2 {
		t.Fatalf("nodes = %d", len(doc.Nodes))
	}
	r := doc.Nodes[0]
	if r.Type != "vless" || r.Server != "x.example.com" || r.Port != 443 || r.Security != "reality" ||
		r.ServerName != "www.apple.com" || r.PublicKey != "PK" || r.Flow != "xtls-rprx-vision" {
		t.Fatalf("xray vless wrong: %+v", r)
	}
	w := doc.Nodes[1]
	if w.Type != "vmess" || w.Network != "ws" || w.Path != "/vm" || !w.Insecure || w.ALPN != "h2" {
		t.Fatalf("xray vmess wrong: %+v", w)
	}
}

func TestParseSingBoxJSON(t *testing.T) {
	payload := `{
	  "outbounds": [
	    {"type": "vless", "tag": "sb-vless", "server": "s.example.com", "server_port": 443,
	     "uuid": "77777777-7777-7777-7777-777777777777",
	     "tls": {"enabled": true, "server_name": "s.example.com", "insecure": true,
	       "alpn": ["h2", "http/1.1"], "utls": {"enabled": true, "fingerprint": "chrome"},
	       "reality": {"enabled": true, "public_key": "SBPK", "short_id": "12"}},
	     "transport": {"type": "grpc", "service_name": "grpcsvc"}},
	    {"type": "shadowsocks", "tag": "sb-ss", "server": "ss.example.com", "server_port": 8388,
	     "method": "aes-256-gcm", "password": "sspw"},
	    {"type": "direct", "tag": "direct"}
	  ]
	}`
	doc := mustParse(t, payload)
	if doc.Format != "sing-box JSON" {
		t.Fatalf("format = %q", doc.Format)
	}
	if len(doc.Nodes) != 2 {
		t.Fatalf("nodes = %d", len(doc.Nodes))
	}
	v := doc.Nodes[0]
	if v.Security != "reality" || v.PublicKey != "SBPK" || v.ShortID != "12" ||
		v.Network != "grpc" || v.ServiceName != "grpcsvc" || v.ALPN != "h2,http/1.1" ||
		v.Fingerprint != "chrome" {
		t.Fatalf("sing-box vless wrong: %+v", v)
	}
	if doc.Nodes[1].Type != "ss" || doc.Nodes[1].Cipher != "aes-256-gcm" {
		t.Fatalf("sing-box ss wrong: %+v", doc.Nodes[1])
	}
}

func TestParseWrappedBase64(t *testing.T) {
	plain := "trojan://pw@1.2.3.4:443#wrapped\n" +
		"vless://55555555-5555-5555-5555-555555555555@5.6.7.8:443?security=tls&sni=a.example.com#second"
	encoded := base64.StdEncoding.EncodeToString([]byte(plain))
	// Providers wrap the payload at 76 columns; whitespace must not defeat detection.
	var wrapped strings.Builder
	for len(encoded) > 76 {
		wrapped.WriteString(encoded[:76])
		wrapped.WriteString("\r\n")
		encoded = encoded[76:]
	}
	wrapped.WriteString(encoded)

	doc := mustParse(t, wrapped.String())
	if len(doc.Nodes) != 2 {
		t.Fatalf("nodes = %d, warnings = %v", len(doc.Nodes), doc.Warnings)
	}
	if doc.Nodes[0].Server != "1.2.3.4" || doc.Nodes[1].Server != "5.6.7.8" {
		t.Fatalf("nodes = %+v", doc.Nodes)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, err := Parse([]byte("hello world, not a subscription")); err == nil {
		t.Fatal("expected an error for unrecognized content")
	}
	if _, err := Parse([]byte("   ")); err == nil {
		t.Fatal("expected an error for empty content")
	}
}

func TestFetchLocalFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub.txt")
	content := "trojan://pw@1.1.1.1:443#local\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	f := NewFetcher()
	got, err := f.Fetch(context.Background(), path)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if string(got) != content {
		t.Fatalf("content = %q", got)
	}
}
