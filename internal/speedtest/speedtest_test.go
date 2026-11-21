package speedtest

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ipscan/internal/model"
)

// startTarget serves a fixed body for the download test.
func startTarget(t *testing.T, size int) *httptest.Server {
	t.Helper()
	body := bytes.Repeat([]byte("x"), size)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(size))
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// startProxy starts a fake node that relays a tunnel to the target after the
// protocol handshake read by handle.
func startProxy(t *testing.T, handle func(net.Conn) error) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				if err := handle(conn); err != nil {
					t.Logf("fake node: %v", err)
				}
			}()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

func relay(conn net.Conn, target string) error {
	up, err := net.Dial("tcp", target)
	if err != nil {
		return err
	}
	defer up.Close()
	done := make(chan struct{}, 1)
	go func() {
		_, _ = io.Copy(up, conn)
		done <- struct{}{}
	}()
	_, _ = io.Copy(conn, up)
	<-done
	return nil
}

func readExact(conn net.Conn, n int) ([]byte, error) {
	buf := make([]byte, n)
	_, err := io.ReadFull(conn, buf)
	return buf, err
}

func readTarget(conn net.Conn, vless bool, withPort bool) (string, int, error) {
	atyp, err := readExact(conn, 1)
	if err != nil {
		return "", 0, err
	}
	kind := atyp[0]
	if vless {
		switch kind {
		case 1:
			kind = 1
		case 2:
			kind = 3
		case 3:
			kind = 4
		}
	}
	var host string
	switch kind {
	case 1:
		raw, err := readExact(conn, 4)
		if err != nil {
			return "", 0, err
		}
		host = net.IP(raw).String()
	case 3:
		l, err := readExact(conn, 1)
		if err != nil {
			return "", 0, err
		}
		raw, err := readExact(conn, int(l[0]))
		if err != nil {
			return "", 0, err
		}
		host = string(raw)
	case 4:
		raw, err := readExact(conn, 16)
		if err != nil {
			return "", 0, err
		}
		host = net.IP(raw).String()
	default:
		return "", 0, fmt.Errorf("bad address type %d", kind)
	}
	if !withPort {
		return host, 0, nil
	}
	raw, err := readExact(conn, 2)
	if err != nil {
		return "", 0, err
	}
	return host, int(binary.BigEndian.Uint16(raw)), nil
}

func targetAddress(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	u := strings.TrimPrefix(srv.URL, "http://")
	if _, _, err := net.SplitHostPort(u); err != nil {
		t.Fatal(err)
	}
	return u
}

func measure(t *testing.T, n model.Node, srv *httptest.Server, size int64) Result {
	t.Helper()
	client := &Client{Options: Options{URL: srv.URL + "/speed", Bytes: size, Timeout: 5 * time.Second}}
	res, err := client.Measure(t.Context(), n)
	if err != nil {
		t.Fatalf("Measure: %v", err)
	}
	if res.Bytes != size {
		t.Fatalf("bytes = %d, want %d", res.Bytes, size)
	}
	if res.MBps <= 0 {
		t.Fatalf("MBps = %v", res.MBps)
	}
	return res
}

func TestMeasureThroughTrojan(t *testing.T) {
	srv := startTarget(t, 512<<10)
	target := targetAddress(t, srv)
	port := startProxy(t, func(conn net.Conn) error {
		hash, err := readExact(conn, 56)
		if err != nil {
			return err
		}
		if _, err := readExact(conn, 2); err != nil { // CRLF
			return err
		}
		want := sha224Hex("secret")
		if string(hash) != want {
			return fmt.Errorf("password hash = %q, want %q", hash, want)
		}
		if _, err := readExact(conn, 1); err != nil { // command
			return err
		}
		host, port, err := readTarget(conn, false, true)
		if err != nil {
			return err
		}
		if _, err := readExact(conn, 2); err != nil { // CRLF
			return err
		}
		if net.JoinHostPort(host, fmt.Sprint(port)) != target {
			return fmt.Errorf("target = %s:%d", host, port)
		}
		return relay(conn, target)
	})
	n := model.Node{Type: "trojan", Server: "127.0.0.1", Port: port, Password: "secret", Network: "tcp", Security: "none"}
	measure(t, n, srv, 256<<10)
}

func TestMeasureThroughVLESS(t *testing.T) {
	srv := startTarget(t, 512<<10)
	target := targetAddress(t, srv)
	port := startProxy(t, func(conn net.Conn) error {
		if _, err := readExact(conn, 1); err != nil { // version
			return err
		}
		if _, err := readExact(conn, 16); err != nil { // uuid
			return err
		}
		extra, err := readExact(conn, 1)
		if err != nil {
			return err
		}
		if _, err := readExact(conn, int(extra[0])); err != nil {
			return err
		}
		if _, err := readExact(conn, 1); err != nil { // command
			return err
		}
		rawPort, err := readExact(conn, 2)
		if err != nil {
			return err
		}
		host, port, err := readTarget(conn, true, false)
		if err != nil {
			return err
		}
		targetHost, targetPort, _ := net.SplitHostPort(target)
		if host != targetHost || int(binary.BigEndian.Uint16(rawPort)) != mustPort(targetPort) {
			return fmt.Errorf("target = %s:%d port=%d", host, port, binary.BigEndian.Uint16(rawPort))
		}
		return relay(conn, target)
	})
	n := model.Node{Type: "vless", Server: "127.0.0.1", Port: port,
		UUID: "11111111-1111-1111-1111-111111111111", Network: "tcp", Security: "none"}
	measure(t, n, srv, 256<<10)
}

func TestMeasureThroughShadowsocks(t *testing.T) {
	srv := startTarget(t, 512<<10)
	target := targetAddress(t, srv)
	port := startProxy(t, func(conn net.Conn) error {
		stream, err := newSSConn(conn, "aes-256-gcm", "secret")
		if err != nil {
			return err
		}
		host, port, err := readTarget(stream, false, true)
		if err != nil {
			return err
		}
		if net.JoinHostPort(host, fmt.Sprint(port)) != target {
			return fmt.Errorf("target = %s:%d", host, port)
		}
		return relay(stream, target)
	})
	n := model.Node{Type: "ss", Server: "127.0.0.1", Port: port,
		Cipher: "aes-256-gcm", Password: "secret", Network: "tcp", Security: "none"}
	measure(t, n, srv, 256<<10)
}

func TestWebSocketTransport(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		req, err := http.ReadRequest(bufio.NewReader(conn))
		if err != nil {
			return
		}
		if req.Header.Get("Upgrade") != "websocket" {
			return
		}
		fmt.Fprintf(conn, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", "x")
		// Echo one binary frame back, unmasked.
		header, err := readExact(conn, 2)
		if err != nil {
			return
		}
		mask := header[1]&0x80 != 0
		length := int(header[1] & 0x7F)
		var key []byte
		if mask {
			key, _ = readExact(conn, 4)
		}
		payload, err := readExact(conn, length)
		if err != nil {
			return
		}
		if mask {
			for i := range payload {
				payload[i] ^= key[i%4]
			}
		}
		frame := append([]byte{0x82, byte(len(payload))}, payload...)
		_, _ = conn.Write(frame)
		_, _ = conn.Write(frame)
	}()
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	ws, err := newWSClient(t.Context(), conn, model.Node{Server: "example.com", Host: "example.com", Path: "/ray"})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if _, err := ws.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 10)
	n, err := ws.Read(buf)
	if err != nil || string(buf[:n]) != "hello" {
		t.Fatalf("read = %q, %v", buf[:n], err)
	}
}

func TestUnsupportedNode(t *testing.T) {
	client := New()
	_, err := client.Measure(t.Context(), model.Node{Type: "vmess", Server: "127.0.0.1", Port: 1})
	if err == nil || !strings.Contains(err.Error(), "暂不支持") {
		t.Fatalf("err = %v", err)
	}
}

func sha224Hex(password string) string {
	sum := sha256.Sum224([]byte(password))
	return hex.EncodeToString(sum[:])
}

func mustPort(s string) int {
	p, _ := net.LookupPort("tcp", s)
	return p
}
