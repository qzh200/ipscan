// Package probe measures a node's network quality: DNS, TCP handshake,
// latency, jitter, loss and the TLS handshake, plus optional IP reputation.
package probe

import (
	"context"
	"time"

	"ipscan/internal/model"
)

// TLSInfo is what the TLS handshake revealed.
type TLSInfo struct {
	Version    string
	Cipher     string
	CommonName string
	Issuer     string
	NotAfter   time.Time
	Verified   bool
	// Error is set when the handshake never completed; TCP metrics stay valid.
	Error string
}

// Geo is the IP's registry/geo information from an external provider.
type Geo struct {
	Country string
	Region  string
	City    string
	ISP     string
	ASN     string
	Proxy   bool
	Hosting bool
	Mobile  bool
}

// Result is one node's measured quality.
type Result struct {
	// IPs are every address the server name resolved to.
	IPs []string
	// IP is the address that was probed.
	IP     string
	Family string // IPv4 or IPv6

	DNS       time.Duration
	Reachable bool
	Samples   int
	Succeeded int
	Loss      float64 // 0..1

	Min    time.Duration
	Avg    time.Duration
	Max    time.Duration
	Jitter time.Duration

	TLS   *TLSInfo
	TLSMs time.Duration

	Geo *Geo
	// UDPOnly is true for QUIC-based nodes (hysteria2, tuic) whose TCP port is
	// normally closed; the TCP failure is not scored.
	UDPOnly bool

	Err   string
	Score int
	Grade string
}

// UDPTransport reports whether the protocol runs over QUIC instead of TCP.
func UDPTransport(n model.Node) bool {
	switch n.Type {
	case "hysteria", "hysteria2", "tuic":
		return true
	}
	return n.Network == "quic"
}

// GradeFromScore maps a score to a short label.
func GradeFromScore(score int, reachable bool) string {
	if !reachable {
		return "不可达"
	}
	switch {
	case score >= 85:
		return "优秀"
	case score >= 70:
		return "良好"
	case score >= 50:
		return "一般"
	default:
		return "较差"
	}
}

// TargetLabel is what the prober connected to.
func (r Result) TargetLabel() string {
	if r.IP == "" {
		return "未解析"
	}
	if r.Family == "" {
		return r.IP
	}
	return r.IP + " · " + r.Family
}

// GeoLabel is "中国 上海 · ASN · ISP", degrading to whatever is known.
func (r Result) GeoLabel() string {
	if r.Geo == nil {
		return "—"
	}
	place := r.Geo.Country
	if r.Geo.City != "" {
		if place != "" {
			place += " "
		}
		place += r.Geo.City
	}
	switch {
	case place != "" && r.Geo.ISP != "":
		return place + " · " + r.Geo.ISP
	case place != "":
		return place
	case r.Geo.ISP != "":
		return r.Geo.ISP
	}
	return "—"
}

// IPTypeLabel describes the IP's role: 机房 or 住宅/移动网络, plus risk flags.
func (r Result) IPTypeLabel() string {
	if r.Geo == nil {
		return ""
	}
	var parts []string
	switch {
	case r.Geo.Hosting:
		parts = append(parts, "机房 IP")
	case r.Geo.Mobile:
		parts = append(parts, "移动网络")
	default:
		parts = append(parts, "住宅/企业 IP")
	}
	if r.Geo.Proxy {
		parts = append(parts, "代理标记")
	}
	return joinParts(parts)
}

func joinParts(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " · "
		}
		out += p
	}
	return out
}

// scanContext keeps the whole probe bounded even with many samples.
func scanContext(parent context.Context, samples int, per time.Duration) (context.Context, context.CancelFunc) {
	total := time.Duration(samples)*per + per
	return context.WithTimeout(parent, total)
}
