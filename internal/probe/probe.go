package probe

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"

	"ipscan/internal/model"
)

// Options tunes one probe. Zero values fall back to the defaults.
type Options struct {
	// Samples is how many TCP handshakes are timed (3).
	Samples int
	// Timeout bounds one TCP connect or TLS handshake (3s).
	Timeout time.Duration
	// DNS bounds the name lookup (5s).
	DNS time.Duration
}

func (o Options) normalized() Options {
	if o.Samples <= 0 {
		o.Samples = 3
	}
	if o.Timeout <= 0 {
		o.Timeout = 3 * time.Second
	}
	if o.DNS <= 0 {
		o.DNS = 5 * time.Second
	}
	return o
}

// Prober measures nodes. It holds no per-node state and is safe for
// concurrent use.
type Prober struct {
	Options  Options
	Resolver *net.Resolver
	Dialer   *net.Dialer
}

// New returns a Prober with sensible defaults.
func New() *Prober {
	return &Prober{Options: Options{}.normalized()}
}

func (p *Prober) options() Options {
	o := p.Options
	if o.Timeout <= 0 && o.Samples <= 0 && o.DNS <= 0 {
		return Options{}.normalized()
	}
	return o.normalized()
}

// Probe resolves, handshakes and scores one node. It never returns an error:
// every failure lands in Result.Err so a table can show it.
func (p *Prober) Probe(ctx context.Context, n model.Node) Result {
	o := p.options()
	var res Result

	pctx, cancel := scanContext(ctx, o.Samples, o.Timeout)
	defer cancel()

	ips, dnsDur, err := p.resolve(pctx, n.Server, o.DNS)
	res.DNS = dnsDur
	if err != nil {
		res.Err = "DNS 解析失败: " + err.Error()
		res.Grade = "不可达"
		return res
	}
	res.IPs = ips
	res.IP, res.Family = pickIP(ips)
	if res.IP == "" {
		res.Err = "未解析到可用 IP"
		res.Grade = "不可达"
		return res
	}

	addr := net.JoinHostPort(res.IP, strconv.Itoa(n.Port))
	samples := o.Samples
	res.Samples = samples

	var rtts []time.Duration
	var lastErr error
	for range samples {
		if pctx.Err() != nil {
			break
		}
		start := time.Now()
		conn, dialErr := p.dialer().DialContext(pctx, "tcp", addr)
		elapsed := time.Since(start)
		if dialErr != nil {
			lastErr = dialErr
			continue
		}
		_ = conn.Close()
		rtts = append(rtts, elapsed)
	}

	res.Succeeded = len(rtts)
	res.Loss = float64(samples-len(rtts)) / float64(samples)
	if len(rtts) > 0 {
		res.Reachable = true
		res.Min, res.Avg, res.Max, res.Jitter = stats(rtts)
	}

	if !res.Reachable {
		if UDPTransport(n) {
			res.UDPOnly = true
			res.Err = "QUIC/UDP 协议，TCP 端口正常不监听，仅展示 IP 质量"
			res.Grade = "未探测"
			return res
		}
		if lastErr == nil {
			lastErr = errors.New("连接被取消")
		}
		res.Err = "TCP 连接失败: " + lastErr.Error()
		res.Grade = "不可达"
		return res
	}

	if n.Security == "tls" || n.Security == "reality" {
		res.TLS, res.TLSMs = p.probeTLS(pctx, n, res.IP, o.Timeout)
	}

	evaluate(&res, n)
	return res
}

func (p *Prober) dialer() *net.Dialer {
	if p.Dialer != nil {
		return p.Dialer
	}
	o := p.options()
	return &net.Dialer{Timeout: o.Timeout}
}

func (p *Prober) resolve(ctx context.Context, host string, timeout time.Duration) ([]string, time.Duration, error) {
	host = strings.TrimSpace(host)
	if ip := net.ParseIP(host); ip != nil {
		return []string{ip.String()}, 0, nil
	}
	start := time.Now()
	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resolver := p.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	addrs, err := resolver.LookupIPAddr(rctx, host)
	elapsed := time.Since(start)
	if err != nil {
		return nil, elapsed, err
	}
	// IPv4 first: most proxy servers and most networks handle it better.
	var v4, v6 []string
	seen := map[string]bool{}
	for _, a := range addrs {
		ip := a.IP.String()
		if seen[ip] {
			continue
		}
		seen[ip] = true
		if a.IP.To4() != nil {
			v4 = append(v4, ip)
		} else {
			v6 = append(v6, ip)
		}
	}
	out := append(v4, v6...)
	if len(out) == 0 {
		return nil, elapsed, errors.New("没有 A/AAAA 记录")
	}
	return out, elapsed, nil
}

func pickIP(ips []string) (string, string) {
	for _, s := range ips {
		if ip := net.ParseIP(s); ip != nil && ip.To4() != nil {
			return s, "IPv4"
		}
	}
	for _, s := range ips {
		if ip := net.ParseIP(s); ip != nil {
			return s, "IPv6"
		}
	}
	return "", ""
}

func stats(rtts []time.Duration) (min, avg, max, jitter time.Duration) {
	min = rtts[0]
	var sum time.Duration
	for _, r := range rtts {
		if r < min {
			min = r
		}
		if r > max {
			max = r
		}
		sum += r
	}
	avg = sum / time.Duration(len(rtts))
	if len(rtts) > 1 {
		var diffs time.Duration
		for i := 1; i < len(rtts); i++ {
			d := rtts[i] - rtts[i-1]
			if d < 0 {
				d = -d
			}
			diffs += d
		}
		jitter = diffs / time.Duration(len(rtts)-1)
	}
	return
}

func (p *Prober) probeTLS(ctx context.Context, n model.Node, ip string, timeout time.Duration) (*TLSInfo, time.Duration) {
	info := &TLSInfo{}
	serverName := firstNonEmpty(n.ServerName, n.Host)
	if serverName == "" && net.ParseIP(n.Server) == nil {
		serverName = n.Server
	}
	cfg := &tls.Config{
		ServerName:         serverName,
		InsecureSkipVerify: true, //#nosec G402 -- we verify the leaf ourselves and only measure
		MinVersion:         tls.VersionTLS12,
	}
	if n.ALPN != "" {
		cfg.NextProtos = strings.Split(n.ALPN, ",")
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	d := &tls.Dialer{NetDialer: &net.Dialer{Timeout: timeout}, Config: cfg}
	start := time.Now()
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(ip, strconv.Itoa(n.Port)))
	elapsed := time.Since(start)
	if err != nil {
		info.Error = trimErr(err)
		return info, elapsed
	}
	defer conn.Close()

	tc, ok := conn.(*tls.Conn)
	if !ok {
		info.Error = "TLS 连接类型异常"
		return info, elapsed
	}
	state := tc.ConnectionState()
	info.Version = tls.VersionName(state.Version)
	info.Cipher = tls.CipherSuiteName(state.CipherSuite)
	if len(state.PeerCertificates) > 0 {
		leaf := state.PeerCertificates[0]
		info.CommonName = leaf.Subject.CommonName
		if info.CommonName == "" && len(leaf.DNSNames) > 0 {
			info.CommonName = leaf.DNSNames[0]
		}
		info.Issuer = leaf.Issuer.CommonName
		info.NotAfter = leaf.NotAfter
		if serverName != "" {
			info.Verified = leaf.VerifyHostname(serverName) == nil
		}
	}
	return info, elapsed
}

func trimErr(err error) string {
	msg := err.Error()
	if len(msg) > 160 {
		msg = msg[:160] + "…"
	}
	return msg
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
