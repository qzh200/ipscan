// Package model holds the transport-agnostic description of one proxy node,
// shared by the subscription parsers, the prober and the UI.
package model

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
)

// Node is one proxy endpoint as a subscription declares it. Fields that a
// protocol does not use stay zero, so the UI can print whatever is set.
type Node struct {
	// Name is the display name the subscription gave the node.
	Name string
	// Type is the proxy protocol: vmess, vless, trojan, ss, ssr, hysteria,
	// hysteria2, tuic, socks5, http.
	Type string

	// Server is a host name or a literal IP; Port is the TCP/UDP port.
	Server string
	Port   int

	UUID     string
	Password string
	Cipher   string
	AlterID  int
	Flow     string

	// Transport describes how the payload travels inside the connection.
	Network     string // tcp, ws, grpc, h2, httpupgrade, quic, kcp
	Security    string // none, tls, reality
	ServerName  string // TLS SNI / REALITY server name
	Host        string // HTTP Host header
	Path        string
	ServiceName string // gRPC service name
	ALPN        string
	Fingerprint string // uTLS fingerprint
	PublicKey   string // REALITY public key
	ShortID     string // REALITY short id
	Insecure    bool   // skip certificate verification

	// Obfs is hysteria2's obfuscation mode, Plugin shadowsocks' SIP003 plugin.
	Obfs         string
	ObfsPassword string
	Plugin       string
	PluginOpts   string

	// Raw is the original URI or config fragment, kept for copying.
	Raw string
	// Extra carries protocol-specific parameters the UI prints verbatim.
	Extra map[string]string
}

// Address is server:port, with IPv6 literals bracketed.
func (n Node) Address() string {
	return net.JoinHostPort(n.Server, strconv.Itoa(n.Port))
}

// ProtocolLabel is the upper-case protocol name shown in a tag.
func (n Node) ProtocolLabel() string {
	switch n.Type {
	case "ss":
		return "SS"
	case "ssr":
		return "SSR"
	case "hysteria2":
		return "HY2"
	case "hysteria":
		return "HY"
	case "socks5":
		return "SOCKS"
	default:
		return strings.ToUpper(n.Type)
	}
}

// TransportLabel condenses TLS and transport into "TLS · WS".
func (n Node) TransportLabel() string {
	var parts []string
	if s := strings.ToUpper(n.Security); s != "" && s != "NONE" {
		parts = append(parts, s)
	}
	if t := strings.ToUpper(n.Network); t != "" && t != "TCP" {
		parts = append(parts, t)
	}
	if len(parts) == 0 {
		return "TCP"
	}
	return strings.Join(parts, " · ")
}

// Param is one labelled detail line.
type Param struct {
	Key, Value string
}

// Params returns the protocol and transport parameters worth inspecting in the
// detail panel, deterministically ordered: fixed fields first, then Extra.
func (n Node) Params() []Param {
	var out []Param
	add := func(k, v string) {
		if v != "" {
			out = append(out, Param{k, v})
		}
	}
	add("协议", n.ProtocolLabel()+" ("+n.Type+")")
	add("服务器", n.Server)
	add("端口", strconv.Itoa(n.Port))
	add("UUID", n.UUID)
	add("密码", n.Password)
	add("加密", n.Cipher)
	if n.AlterID > 0 {
		add("AlterId", strconv.Itoa(n.AlterID))
	}
	add("Flow", n.Flow)
	add("传输", n.Network)
	add("安全", n.Security)
	add("SNI", n.ServerName)
	add("Host", n.Host)
	add("路径", n.Path)
	add("gRPC 服务名", n.ServiceName)
	add("ALPN", n.ALPN)
	add("指纹", n.Fingerprint)
	add("REALITY 公钥", n.PublicKey)
	add("REALITY 短 ID", n.ShortID)
	if n.Insecure {
		add("跳过证书校验", "是")
	}
	add("混淆", n.Obfs)
	add("混淆密码", n.ObfsPassword)
	add("插件", n.Plugin)
	add("插件参数", n.PluginOpts)

	keys := make([]string, 0, len(n.Extra))
	for k := range n.Extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		add(k, n.Extra[k])
	}
	return out
}

// Validate reports why a parsed node cannot be probed.
func (n Node) Validate() error {
	if strings.TrimSpace(n.Server) == "" {
		return fmt.Errorf("节点 %q 缺少服务器地址", n.Name)
	}
	if n.Port <= 0 || n.Port > 65535 {
		return fmt.Errorf("节点 %q 端口无效: %d", n.Name, n.Port)
	}
	return nil
}
