package speedtest

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"ipscan/internal/model"
)

// wsConn carries a byte stream in binary WebSocket frames, the way the v2ray
// family does when a node uses network=ws.
type wsConn struct {
	conn net.Conn
	br   *bufio.Reader
	wmu  sync.Mutex
	rbuf []byte
}

func newWSClient(ctx context.Context, conn net.Conn, n model.Node) (net.Conn, error) {
	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		return nil, err
	}
	host := firstNonEmpty(n.Host, n.ServerName, n.Server)
	path := strings.TrimSpace(n.Path)
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	var request strings.Builder
	fmt.Fprintf(&request, "GET %s HTTP/1.1\r\n", path)
	fmt.Fprintf(&request, "Host: %s\r\n", host)
	request.WriteString("Upgrade: websocket\r\n")
	request.WriteString("Connection: Upgrade\r\n")
	fmt.Fprintf(&request, "Sec-WebSocket-Key: %s\r\n", base64.StdEncoding.EncodeToString(keyBytes))
	request.WriteString("Sec-WebSocket-Version: 13\r\n")
	request.WriteString("\r\n")
	if _, err := io.WriteString(conn, request.String()); err != nil {
		return nil, fmt.Errorf("WebSocket 握手失败: %w", err)
	}

	br := bufio.NewReaderSize(conn, 32<<10)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodGet})
	if err != nil {
		return nil, fmt.Errorf("WebSocket 握手失败: %w", err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("WebSocket 握手被拒绝: %s", resp.Status)
	}
	return &wsConn{conn: conn, br: br}, nil
}

func (w *wsConn) Write(p []byte) (int, error) {
	w.wmu.Lock()
	defer w.wmu.Unlock()
	written := 0
	for len(p) > 0 {
		n := min(len(p), 32<<10)
		if err := w.writeFrame(0x02, p[:n]); err != nil {
			return written, err
		}
		written += n
		p = p[n:]
	}
	return written, nil
}

func (w *wsConn) writeFrame(opcode byte, payload []byte) error {
	header := make([]byte, 0, 14)
	header = append(header, 0x80|opcode)
	length := len(payload)
	switch {
	case length < 126:
		header = append(header, 0x80|byte(length))
	case length <= 0xFFFF:
		header = append(header, 0x80|126, byte(length>>8), byte(length))
	default:
		header = append(header, 0x80|127)
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(length))
		header = append(header, ext[:]...)
	}
	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return err
	}
	header = append(header, mask[:]...)
	masked := make([]byte, len(payload))
	for i, b := range payload {
		masked[i] = b ^ mask[i%4]
	}
	if _, err := w.conn.Write(header); err != nil {
		return err
	}
	_, err := w.conn.Write(masked)
	return err
}

func (w *wsConn) Read(p []byte) (int, error) {
	for {
		if len(w.rbuf) > 0 {
			n := copy(p, w.rbuf)
			w.rbuf = w.rbuf[n:]
			return n, nil
		}
		opcode, payload, err := w.readFrame()
		if err != nil {
			return 0, err
		}
		switch opcode {
		case 0x00, 0x01, 0x02:
			// A full or partial data frame; continuation is fine because the
			// reader consumes a stream, not messages.
			w.rbuf = payload
		case 0x08:
			return 0, io.EOF
		case 0x09:
			w.wmu.Lock()
			err := w.writeFrame(0x0A, payload)
			w.wmu.Unlock()
			if err != nil {
				return 0, err
			}
		case 0x0A:
			// Pong: nothing to do.
		default:
			return 0, fmt.Errorf("未知 WebSocket 帧类型 %#x", opcode)
		}
	}
}

func (w *wsConn) readFrame() (byte, []byte, error) {
	var head [2]byte
	if _, err := io.ReadFull(w.br, head[:]); err != nil {
		return 0, nil, err
	}
	opcode := head[0] & 0x0F
	masked := head[1]&0x80 != 0
	length := int(head[1] & 0x7F)
	switch length {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(w.br, ext[:]); err != nil {
			return 0, nil, err
		}
		length = int(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(w.br, ext[:]); err != nil {
			return 0, nil, err
		}
		v := binary.BigEndian.Uint64(ext[:])
		if v > 1<<20 {
			return 0, nil, fmt.Errorf("WebSocket 帧过大: %d", v)
		}
		length = int(v)
	}
	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(w.br, mask[:]); err != nil {
			return 0, nil, err
		}
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(w.br, payload); err != nil {
		return 0, nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return opcode, payload, nil
}

func (w *wsConn) Close() error                       { return w.conn.Close() }
func (w *wsConn) LocalAddr() net.Addr                { return w.conn.LocalAddr() }
func (w *wsConn) RemoteAddr() net.Addr               { return w.conn.RemoteAddr() }
func (w *wsConn) SetDeadline(t time.Time) error      { return w.conn.SetDeadline(t) }
func (w *wsConn) SetReadDeadline(t time.Time) error  { return w.conn.SetReadDeadline(t) }
func (w *wsConn) SetWriteDeadline(t time.Time) error { return w.conn.SetWriteDeadline(t) }
