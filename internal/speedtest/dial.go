package speedtest

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strings"

	"ipscan/internal/model"
)

// openStream connects to the node and opens a tunnel to host:port inside it.
func openStream(ctx context.Context, n model.Node, host string, port int) (net.Conn, error) {
	if err := checkSupported(n); err != nil {
		return nil, err
	}
	network := strings.ToLower(n.Network)
	if network == "" {
		network = "tcp"
	}
	security := strings.ToLower(n.Security)
	if security == "" {
		security = "none"
	}

	d := &net.Dialer{}
	conn, err := d.DialContext(ctx, "tcp", n.Address())
	if err != nil {
		return nil, fmt.Errorf("连接节点失败: %w", err)
	}
	if security == "tls" {
		cfg := &tls.Config{
			ServerName:         tlsServerName(n),
			InsecureSkipVerify: n.Insecure, //#nosec G402 -- the node's certificate is not the measurement
		}
		if network == "ws" {
			cfg.NextProtos = []string{"http/1.1"}
		} else if n.ALPN != "" {
			cfg.NextProtos = splitList(n.ALPN)
		}
		tc := tls.Client(conn, cfg)
		if err := tc.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("节点 TLS 握手失败: %w", err)
		}
		conn = tc
	}
	if network == "ws" {
		ws, err := newWSClient(ctx, conn, n)
		if err != nil {
			_ = conn.Close()
			return nil, err
		}
		conn = ws
	}

	switch n.Type {
	case "trojan":
		err = trojanConnect(conn, n, host, port)
	case "vless":
		err = vlessConnect(conn, n, host, port)
	case "ss":
		conn, err = ssConnect(conn, n, host, port)
	}
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

// checkSupported reports why a node cannot be measured natively. The reasons
// are user-facing: the UI shows them in place of a speed.
func checkSupported(n model.Node) error {
	switch n.Type {
	case "trojan", "vless", "ss":
	default:
		return fmt.Errorf("测速暂不支持 %s 协议", protocolLabel(n))
	}
	network := strings.ToLower(n.Network)
	if network == "" {
		network = "tcp"
	}
	if network != "tcp" && network != "ws" {
		return fmt.Errorf("测速暂不支持 %s 传输", strings.ToUpper(network))
	}
	security := strings.ToLower(n.Security)
	if security == "" {
		security = "none"
	}
	switch security {
	case "none", "tls":
	default:
		return fmt.Errorf("测速暂不支持 %s 加密", strings.ToUpper(security))
	}
	switch n.Type {
	case "ss":
		if network != "tcp" || security != "none" {
			return fmt.Errorf("测速暂不支持 SS 的 %s 传输", strings.ToUpper(network))
		}
		if n.Plugin != "" {
			return fmt.Errorf("测速暂不支持 SS 插件 %s", n.Plugin)
		}
		if !ssCipherSupported(n.Cipher) {
			return fmt.Errorf("测速暂不支持 SS 加密方式 %s", n.Cipher)
		}
	case "vless":
		if enc := strings.TrimSpace(n.Extra["encryption"]); enc != "" && enc != "none" {
			return fmt.Errorf("测速暂不支持 VLESS encryption=%s", enc)
		}
		if n.Flow != "" {
			return fmt.Errorf("测速暂不支持 VLESS flow=%s", n.Flow)
		}
	}
	return nil
}

func tlsServerName(n model.Node) string {
	name := firstNonEmpty(n.ServerName, n.Host, n.Server)
	return name
}

func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func protocolLabel(n model.Node) string {
	if label := n.ProtocolLabel(); label != "" {
		return label
	}
	return strings.ToUpper(n.Type)
}
