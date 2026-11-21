// Package speedtest measures how fast a node downloads data. It speaks a
// small, native subset of the proxy protocols instead of starting a proxy
// core: Trojan, Shadowsocks AEAD and VLESS (encryption=none) over TCP or
// WebSocket, optionally with TLS. Everything else reports unsupported.
package speedtest

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"ipscan/internal/model"
)

// DefaultURL is the download used unless Options.URL overrides it. Cloudflare's
// endpoint serves exactly the requested number of bytes.
const DefaultURL = "https://speed.cloudflare.com/__down?bytes=10485760"

// Options tunes one measurement. Zero values fall back to the defaults.
type Options struct {
	// URL is downloaded through the node.
	URL string
	// Bytes caps the download; it is also the default URL's byte count.
	Bytes int64
	// Timeout bounds the whole measurement.
	Timeout time.Duration
	// TLSConfig overrides the target site's TLS settings; nil verifies the
	// certificate normally.
	TLSConfig *tls.Config
}

const (
	defaultBytes   = 10 << 20
	defaultTimeout = 30 * time.Second
)

func (o Options) normalized() Options {
	if o.URL == "" {
		o.URL = DefaultURL
	}
	if o.Bytes <= 0 {
		o.Bytes = defaultBytes
	}
	if o.Timeout <= 0 {
		o.Timeout = defaultTimeout
	}
	return o
}

// Result is one node's download measurement.
type Result struct {
	// MBps is mebibytes per second over the body transfer.
	MBps float64
	// Bytes is how many body bytes arrived.
	Bytes int64
	// Elapsed is the body transfer time.
	Elapsed time.Duration
	// Connect is the time spent opening the proxy stream and the target TLS.
	Connect time.Duration
	// Target is the host that was downloaded.
	Target string
}

// Client measures nodes. It holds no per-node state and is safe for concurrent
// use.
type Client struct {
	Options Options
}

// New returns a client with the default options.
func New() *Client { return &Client{} }

// Measure downloads Options.URL through the node and reports the throughput.
func (c *Client) Measure(ctx context.Context, n model.Node) (Result, error) {
	o := c.options()
	u, err := url.Parse(o.URL)
	if err != nil || u.Hostname() == "" {
		return Result{}, fmt.Errorf("测速地址无效: %q", o.URL)
	}
	host := u.Hostname()
	port := u.Port()
	scheme := u.Scheme
	if scheme != "http" && scheme != "https" {
		return Result{}, fmt.Errorf("测速地址仅支持 http/https: %q", o.URL)
	}
	if port == "" {
		if scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	portNum, err := net.LookupPort("tcp", port)
	if err != nil {
		return Result{}, fmt.Errorf("测速地址端口无效: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()

	start := time.Now()
	stream, err := openStream(ctx, n, host, portNum)
	if err != nil {
		return Result{}, err
	}
	defer stream.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = stream.SetDeadline(deadline)
	}

	var conn net.Conn = stream
	if scheme == "https" {
		cfg := &tls.Config{ServerName: host}
		if o.TLSConfig != nil {
			cfg = o.TLSConfig.Clone()
			if cfg.ServerName == "" {
				cfg.ServerName = host
			}
		}
		tc := tls.Client(stream, cfg)
		if err := tc.HandshakeContext(ctx); err != nil {
			return Result{}, fmt.Errorf("目标站点 TLS 握手失败: %w", err)
		}
		conn = tc
	}
	connect := time.Since(start)

	path := u.RequestURI()
	if path == "" {
		path = "/"
	}
	req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUser-Agent: ipscan-speedtest\r\nAccept: */*\r\nConnection: close\r\n\r\n", path, u.Host)
	if _, err := io.WriteString(conn, req); err != nil {
		return Result{}, fmt.Errorf("发送测速请求失败: %w", err)
	}
	br := bufio.NewReaderSize(conn, 64<<10)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		return Result{}, fmt.Errorf("读取测速响应失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("测速地址返回 %s", resp.Status)
	}

	start = time.Now()
	read, readErr := io.CopyN(io.Discard, resp.Body, o.Bytes)
	elapsed := time.Since(start)
	if read == 0 {
		if readErr == nil || errors.Is(readErr, io.EOF) {
			readErr = errors.New("没有收到测速数据")
		}
		return Result{}, readErr
	}
	// A short body is normal when the server serves less than requested.
	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return Result{}, fmt.Errorf("下载测速数据失败: %w", readErr)
	}
	if elapsed <= 0 {
		elapsed = time.Nanosecond
	}
	return Result{
		MBps:    float64(read) / (1 << 20) / elapsed.Seconds(),
		Bytes:   read,
		Elapsed: elapsed,
		Connect: connect,
		Target:  u.Host,
	}, nil
}

func (c *Client) options() Options {
	o := c.Options
	if o == (Options{}) {
		return o.normalized()
	}
	return o.normalized()
}
