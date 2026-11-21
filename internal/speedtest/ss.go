package speedtest

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/chacha20poly1305"

	"ipscan/internal/model"
)

const (
	ssMaxChunk = 0x3FFF
	ssTagSize  = 16
)

func ssCipherSupported(method string) bool {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "aes-128-gcm", "aes-256-gcm", "chacha20-ietf-poly1305":
		return true
	}
	return false
}

func ssKeyLen(method string) int {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "aes-128-gcm":
		return 16
	case "aes-256-gcm", "chacha20-ietf-poly1305":
		return 32
	}
	return 0
}

func ssNewAEAD(method string, key []byte) (cipher.AEAD, error) {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "aes-128-gcm", "aes-256-gcm":
		block, err := aes.NewCipher(key)
		if err != nil {
			return nil, err
		}
		return cipher.NewGCM(block)
	case "chacha20-ietf-poly1305":
		return chacha20poly1305.New(key)
	}
	return nil, fmt.Errorf("不支持的 SS 加密方式 %s", method)
}

// ssMasterKey derives the master key the way Shadowsocks does: MD5 chains of
// the password (EVP_BytesToKey).
func ssMasterKey(password string, keyLen int) []byte {
	out := make([]byte, 0, keyLen)
	var prev []byte
	for len(out) < keyLen {
		h := md5.New()
		h.Write(prev)
		h.Write([]byte(password))
		prev = h.Sum(nil)
		out = append(out, prev...)
	}
	return out[:keyLen]
}

// hkdfSHA1 implements HKDF (RFC 5869) with SHA-1, which Shadowsocks AEAD uses
// to derive each direction's subkey.
func hkdfSHA1(secret, salt []byte, info string, length int) []byte {
	extract := hmac.New(sha1.New, salt)
	extract.Write(secret)
	prk := extract.Sum(nil)

	out := make([]byte, 0, length)
	var prev []byte
	for i := byte(1); len(out) < length; i++ {
		expand := hmac.New(sha1.New, prk)
		expand.Write(prev)
		expand.Write([]byte(info))
		expand.Write([]byte{i})
		prev = expand.Sum(nil)
		out = append(out, prev...)
	}
	return out[:length]
}

func incrementNonce(nonce []byte) {
	for i := range nonce {
		nonce[i]++
		if nonce[i] != 0 {
			return
		}
	}
}

// ssConn is a Shadowsocks AEAD TCP stream: length and payload chunks, each
// sealed with the direction's subkey.
type ssConn struct {
	conn   net.Conn
	method string
	master []byte

	readAEAD  cipher.AEAD
	readNonce [12]byte
	readSalt  bool
	rbuf      []byte

	writeAEAD  cipher.AEAD
	writeNonce [12]byte
	writeSalt  bool

	rmu, wmu sync.Mutex
}

func newSSConn(conn net.Conn, method, password string) (*ssConn, error) {
	keyLen := ssKeyLen(method)
	if keyLen == 0 {
		return nil, fmt.Errorf("不支持的 SS 加密方式 %s", method)
	}
	return &ssConn{conn: conn, method: method, master: ssMasterKey(password, keyLen)}, nil
}

func (s *ssConn) initWriteLocked() error {
	if s.writeSalt {
		return nil
	}
	keyLen := ssKeyLen(s.method)
	salt := make([]byte, keyLen)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	aead, err := ssNewAEAD(s.method, hkdfSHA1(s.master, salt, "ss-subkey", keyLen))
	if err != nil {
		return err
	}
	if _, err := s.conn.Write(salt); err != nil {
		return err
	}
	s.writeAEAD = aead
	s.writeSalt = true
	return nil
}

func (s *ssConn) initReadLocked() error {
	if s.readSalt {
		return nil
	}
	keyLen := ssKeyLen(s.method)
	salt := make([]byte, keyLen)
	if _, err := io.ReadFull(s.conn, salt); err != nil {
		return err
	}
	aead, err := ssNewAEAD(s.method, hkdfSHA1(s.master, salt, "ss-subkey", keyLen))
	if err != nil {
		return err
	}
	s.readAEAD = aead
	s.readSalt = true
	return nil
}

func (s *ssConn) Write(p []byte) (int, error) {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	if err := s.initWriteLocked(); err != nil {
		return 0, err
	}
	written := 0
	for len(p) > 0 {
		n := min(len(p), ssMaxChunk)
		chunk := p[:n]
		length := make([]byte, 2, 2+ssTagSize)
		length[0] = byte(n >> 8)
		length[1] = byte(n)
		sealedLen := s.writeAEAD.Seal(nil, s.writeNonce[:], length, nil)
		incrementNonce(s.writeNonce[:])
		sealed := s.writeAEAD.Seal(nil, s.writeNonce[:], chunk, nil)
		incrementNonce(s.writeNonce[:])
		if _, err := s.conn.Write(append(sealedLen, sealed...)); err != nil {
			return written, err
		}
		written += n
		p = p[n:]
	}
	return written, nil
}

func (s *ssConn) Read(p []byte) (int, error) {
	s.rmu.Lock()
	defer s.rmu.Unlock()
	for {
		if len(s.rbuf) > 0 {
			n := copy(p, s.rbuf)
			s.rbuf = s.rbuf[n:]
			return n, nil
		}
		if err := s.initReadLocked(); err != nil {
			return 0, err
		}
		header := make([]byte, 2+ssTagSize)
		if _, err := io.ReadFull(s.conn, header); err != nil {
			return 0, err
		}
		length, err := s.readAEAD.Open(header[:0], s.readNonce[:], header, nil)
		if err != nil {
			return 0, fmt.Errorf("SS 数据解密失败: %w", err)
		}
		incrementNonce(s.readNonce[:])
		size := int(length[0])<<8 | int(length[1])
		if size == 0 {
			return 0, io.EOF
		}
		if size > ssMaxChunk {
			return 0, errors.New("SS 数据块长度无效")
		}
		body := make([]byte, size+ssTagSize)
		if _, err := io.ReadFull(s.conn, body); err != nil {
			return 0, err
		}
		plain, err := s.readAEAD.Open(body[:0], s.readNonce[:], body, nil)
		if err != nil {
			return 0, fmt.Errorf("SS 数据解密失败: %w", err)
		}
		incrementNonce(s.readNonce[:])
		s.rbuf = plain
	}
}

func (s *ssConn) Close() error                       { return s.conn.Close() }
func (s *ssConn) LocalAddr() net.Addr                { return s.conn.LocalAddr() }
func (s *ssConn) RemoteAddr() net.Addr               { return s.conn.RemoteAddr() }
func (s *ssConn) SetDeadline(t time.Time) error      { return s.conn.SetDeadline(t) }
func (s *ssConn) SetReadDeadline(t time.Time) error  { return s.conn.SetReadDeadline(t) }
func (s *ssConn) SetWriteDeadline(t time.Time) error { return s.conn.SetWriteDeadline(t) }

// ssConnect opens the tunnel and sends the target address as the first
// encrypted chunk.
func ssConnect(conn net.Conn, n model.Node, host string, port int) (net.Conn, error) {
	ss, err := newSSConn(conn, n.Cipher, n.Password)
	if err != nil {
		return nil, err
	}
	if _, err := ss.Write(appendTarget(nil, host, port)); err != nil {
		return nil, fmt.Errorf("SS 握手失败: %w", err)
	}
	return ss, nil
}
