package speedtest

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"strings"

	"ipscan/internal/model"
)

// appendTarget appends a SOCKS-style address: type, address, big-endian port.
func appendTarget(buf []byte, host string, port int) []byte {
	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			buf = append(buf, 0x01)
			buf = append(buf, v4...)
		} else {
			buf = append(buf, 0x04)
			buf = append(buf, ip.To16()...)
		}
	} else {
		if len(host) > 255 {
			host = host[:255]
		}
		buf = append(buf, 0x03, byte(len(host)))
		buf = append(buf, host...)
	}
	return binary.BigEndian.AppendUint16(buf, uint16(port))
}

// trojanConnect sends Trojan's password hash and target request. The server
// stays silent on success; the caller's next bytes go to the target.
func trojanConnect(conn net.Conn, n model.Node, host string, port int) error {
	sum := sha256.Sum224([]byte(n.Password))
	request := make([]byte, 0, 128)
	request = append(request, hex.EncodeToString(sum[:])...)
	request = append(request, '\r', '\n', 0x01)
	request = appendTarget(request, host, port)
	request = append(request, '\r', '\n')
	if _, err := conn.Write(request); err != nil {
		return fmt.Errorf("Trojan 握手失败: %w", err)
	}
	return nil
}

// vlessConnect sends a VLESS TCP request with no encryption.
func vlessConnect(conn net.Conn, n model.Node, host string, port int) error {
	uuid, err := parseUUID(n.UUID)
	if err != nil {
		return err
	}
	request := make([]byte, 0, 64)
	request = append(request, 0x00) // version
	request = append(request, uuid[:]...)
	request = append(request, 0x00) // no addons
	request = append(request, 0x01) // TCP
	request = binary.BigEndian.AppendUint16(request, uint16(port))
	// VLESS address types differ from SOCKS: IPv4=1, domain=2, IPv6=3.
	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			request = append(request, 0x01)
			request = append(request, v4...)
		} else {
			request = append(request, 0x03)
			request = append(request, ip.To16()...)
		}
	} else {
		if len(host) > 255 {
			host = host[:255]
		}
		request = append(request, 0x02, byte(len(host)))
		request = append(request, host...)
	}
	if _, err := conn.Write(request); err != nil {
		return fmt.Errorf("VLESS 握手失败: %w", err)
	}
	return nil
}

func parseUUID(s string) ([16]byte, error) {
	var out [16]byte
	clean := strings.ReplaceAll(strings.TrimSpace(s), "-", "")
	if len(clean) != 32 {
		return out, fmt.Errorf("VLESS UUID 无效: %q", s)
	}
	raw, err := hex.DecodeString(clean)
	if err != nil {
		return out, fmt.Errorf("VLESS UUID 无效: %w", err)
	}
	copy(out[:], raw)
	return out, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
