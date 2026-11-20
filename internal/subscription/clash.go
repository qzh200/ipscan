package subscription

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"ipscan/internal/model"
)

// clashDoc is the slice of a Clash/Mihomo config the app needs.
type clashDoc struct {
	Proxies []clashProxy `yaml:"proxies"`
}

type clashProxy struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"`
	Server   string `yaml:"server"`
	Port     any    `yaml:"port"`
	UUID     string `yaml:"uuid"`
	Password string `yaml:"password"`
	Cipher   string `yaml:"cipher"`
	AlterID  any    `yaml:"alterId"`
	Flow     string `yaml:"flow"`

	// TLS is true/false in most files and a mapping in a few.
	TLS               any    `yaml:"tls"`
	SNI               string `yaml:"sni"`
	ServerName        string `yaml:"servername"`
	SkipCertVerify    bool   `yaml:"skip-cert-verify"`
	ClientFingerprint string `yaml:"client-fingerprint"`
	ALPN              []any  `yaml:"alpn"`

	Network  string        `yaml:"network"`
	WSOpts   clashWSOpts   `yaml:"ws-opts"`
	GrpcOpts clashGrpcOpts `yaml:"grpc-opts"`
	H2Opts   clashH2Opts   `yaml:"h2-opts"`
	HTTPOpts clashHTTPOpts `yaml:"http-opts"`
	Reality  clashReality  `yaml:"reality-opts"`

	Plugin     string         `yaml:"plugin"`
	PluginOpts map[string]any `yaml:"plugin-opts"`

	// hysteria v1
	Auth     string `yaml:"auth"`
	AuthStr  string `yaml:"auth-str"`
	Obfs     string `yaml:"obfs"`
	ObfsPass string `yaml:"obfs-password"`
	Protocol string `yaml:"protocol"`
	Up       string `yaml:"up"`
	Down     string `yaml:"down"`
	UpMbps   any    `yaml:"up-mbps"`
	DownMbps any    `yaml:"down-mbps"`

	// tuic
	CongestionControl string `yaml:"congestion-controller"`
	UDPRelayMode      string `yaml:"udp-relay-mode"`
}

type clashWSOpts struct {
	Path    string            `yaml:"path"`
	Headers map[string]string `yaml:"headers"`
}

type clashGrpcOpts struct {
	ServiceName string `yaml:"grpc-service-name"`
}

type clashH2Opts struct {
	Path string   `yaml:"path"`
	Host []string `yaml:"host"`
}

type clashHTTPOpts struct {
	Path []string `yaml:"path"`
	Host []string `yaml:"host"`
}

type clashReality struct {
	PublicKey string `yaml:"public-key"`
	ShortID   string `yaml:"short-id"`
}

func parseClash(data []byte) (*Document, error) {
	var doc clashDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("Clash YAML 解析失败: %w", err)
	}
	if len(doc.Proxies) == 0 {
		return nil, errors.New("Clash 配置里没有 proxies")
	}
	out := &Document{Format: "Clash YAML"}
	for _, p := range doc.Proxies {
		node, ok := clashToNode(p)
		if !ok {
			out.Warnings = append(out.Warnings, fmt.Sprintf("%s：暂不支持的代理类型 %s", p.Name, p.Type))
			continue
		}
		if err := node.Validate(); err != nil {
			out.Warnings = append(out.Warnings, err.Error())
			continue
		}
		out.Nodes = append(out.Nodes, node)
	}
	if len(out.Nodes) == 0 {
		return nil, errors.New("Clash 配置里没有可用的代理")
	}
	return out, nil
}

func clashToNode(p clashProxy) (model.Node, bool) {
	typ := strings.ToLower(strings.TrimSpace(p.Type))
	switch typ {
	case "vmess", "vless", "trojan", "ss", "ssr", "hysteria", "hysteria2", "tuic", "socks5", "http":
	case "wireguard", "anytls", "direct", "reject", "dns", "selector", "url-test", "fallback", "load-balance":
		return model.Node{}, false
	default:
		return model.Node{}, false
	}

	n := model.Node{
		Name:         strings.TrimSpace(p.Name),
		Type:         typ,
		Server:       strings.TrimSpace(p.Server),
		Port:         intOf(p.Port),
		UUID:         p.UUID,
		Password:     p.Password,
		Cipher:       p.Cipher,
		AlterID:      intOf(p.AlterID),
		Flow:         p.Flow,
		Network:      strings.ToLower(p.Network),
		ALPN:         joinAny(p.ALPN),
		Fingerprint:  p.ClientFingerprint,
		Insecure:     p.SkipCertVerify,
		Plugin:       p.Plugin,
		Obfs:         p.Obfs,
		ObfsPassword: p.ObfsPass,
		Extra:        map[string]string{},
	}
	if n.Name == "" {
		n.Name = n.Address()
	}

	// Transport details, in the order Clash picks them.
	switch {
	case p.WSOpts.Path != "" || len(p.WSOpts.Headers) > 0:
		n.Network = "ws"
		n.Path = p.WSOpts.Path
		for k, v := range p.WSOpts.Headers {
			if strings.EqualFold(k, "host") && n.Host == "" {
				n.Host = v
			}
		}
	case p.GrpcOpts.ServiceName != "":
		n.Network = "grpc"
		n.ServiceName = p.GrpcOpts.ServiceName
	case p.H2Opts.Path != "" || len(p.H2Opts.Host) > 0:
		n.Network = "h2"
		n.Path = p.H2Opts.Path
		n.Host = strings.Join(p.H2Opts.Host, ",")
	case len(p.HTTPOpts.Path) > 0 || len(p.HTTPOpts.Host) > 0:
		n.Network = "http"
		n.Path = strings.Join(p.HTTPOpts.Path, ",")
		n.Host = strings.Join(p.HTTPOpts.Host, ",")
	}
	if n.Network == "" {
		switch typ {
		case "hysteria", "hysteria2", "tuic":
			n.Network = "quic"
		default:
			n.Network = "tcp"
		}
	}

	// Security.
	n.Security = "none"
	if p.Reality.PublicKey != "" {
		n.Security = "reality"
		n.PublicKey = p.Reality.PublicKey
		n.ShortID = p.Reality.ShortID
	} else if tlsEnabled(p.TLS) || typ == "trojan" || typ == "hysteria" || typ == "hysteria2" || typ == "tuic" {
		n.Security = "tls"
	}
	n.ServerName = firstNonEmpty(p.SNI, p.ServerName, p.WSOpts.Headers["Host"], p.WSOpts.Headers["host"])
	if n.ServerName == "" && p.Reality.PublicKey != "" {
		n.ServerName = firstNonEmpty(p.SNI, p.ServerName)
	}

	switch typ {
	case "hysteria":
		n.Password = firstNonEmpty(p.Auth, p.AuthStr, p.Password)
		n.Extra["协议"] = p.Protocol
		if v := firstNonEmpty(p.Up, numString(p.UpMbps)); v != "" {
			n.Extra["上行"] = v
		}
		if v := firstNonEmpty(p.Down, numString(p.DownMbps)); v != "" {
			n.Extra["下行"] = v
		}
	case "tuic":
		n.Extra["拥塞控制"] = p.CongestionControl
		n.Extra["UDP 中继"] = p.UDPRelayMode
	case "ss", "ssr":
		if p.Plugin != "" {
			n.Extra["插件参数"] = pluginParams(p.PluginOpts)
		}
	}
	if n.PublicKey != "" {
		n.Extra["REALITY"] = "启用"
	}
	for k, v := range n.Extra {
		if v == "" {
			delete(n.Extra, k)
		}
	}
	if len(n.Extra) == 0 {
		n.Extra = nil
	}
	return n, true
}

func tlsEnabled(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return truthy(t) || strings.EqualFold(t, "tls")
	case map[string]any:
		return boolean(t, "enabled", "tls")
	}
	return false
}

func intOf(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(t))
		return n
	case uint64:
		return int(t)
	}
	return 0
}

func numString(v any) string {
	n := intOf(v)
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}

func joinAny(values []any) string {
	parts := make([]string, 0, len(values))
	for _, v := range values {
		if s := strings.TrimSpace(fmt.Sprint(v)); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ",")
}

func pluginParams(opts map[string]any) string {
	if len(opts) == 0 {
		return ""
	}
	var parts []string
	for _, key := range []string{"mode", "host", "obfs", "obfs-host"} {
		if v, ok := opts[key]; ok {
			parts = append(parts, fmt.Sprintf("%s=%v", key, v))
		}
	}
	return strings.Join(parts, ", ")
}
