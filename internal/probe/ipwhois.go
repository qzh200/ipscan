package probe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// IPWhois is the HTTPS fallback provider: one request per address, run with
// bounded concurrency. It is used when ip-api's plain-HTTP batch is blocked.
type IPWhois struct {
	Client *http.Client
	// Endpoint is the scheme and host, e.g. "https://ipwho.is".
	Endpoint    string
	Concurrency int
}

// NewIPWhois returns a provider with a 5 second budget per address.
func NewIPWhois() *IPWhois {
	return &IPWhois{
		Client:      &http.Client{Timeout: 5 * time.Second},
		Endpoint:    "https://ipwho.is",
		Concurrency: 4,
	}
}

type ipwhoResponse struct {
	Success    bool   `json:"success"`
	IP         string `json:"ip"`
	Message    string `json:"message"`
	Country    string `json:"country"`
	Region     string `json:"region"`
	City       string `json:"city"`
	Connection struct {
		ASN int    `json:"asn"`
		Org string `json:"org"`
		ISP string `json:"isp"`
	} `json:"connection"`
	Security struct {
		Proxy   bool `json:"proxy"`
		VPN     bool `json:"vpn"`
		Tor     bool `json:"tor"`
		Hosting bool `json:"hosting"`
	} `json:"security"`
}

// Lookup resolves each public IP independently; one failure does not discard
// the others.
func (g *IPWhois) Lookup(ctx context.Context, ips []string) (map[string]Geo, error) {
	targets := publicIPs(ips)
	if len(targets) == 0 {
		return nil, nil
	}
	endpoint := strings.TrimRight(g.Endpoint, "/")
	if endpoint == "" {
		endpoint = "https://ipwho.is"
	}
	client := g.Client
	if client == nil {
		client = NewIPWhois().Client
	}
	workers := g.Concurrency
	if workers <= 0 {
		workers = 4
	}
	if workers > len(targets) {
		workers = len(targets)
	}

	var (
		mu      sync.Mutex
		out     = make(map[string]Geo, len(targets))
		failed  []string
		lastErr error
	)
	jobs := make(chan string)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ip := range jobs {
				geo, err := g.lookupOne(ctx, client, endpoint, ip)
				mu.Lock()
				if err != nil {
					failed = append(failed, ip)
					lastErr = err
				} else {
					out[ip] = geo
				}
				mu.Unlock()
			}
		}()
	}
fill:
	for _, ip := range targets {
		select {
		case <-ctx.Done():
			break fill
		case jobs <- ip:
		}
	}
	close(jobs)
	wg.Wait()

	if len(out) == 0 {
		if lastErr == nil {
			lastErr = errors.New("没有可用的归属信息")
		}
		return nil, fmt.Errorf("ipwho.is 查询失败: %w", lastErr)
	}
	return out, nil
}

func (g *IPWhois) lookupOne(ctx context.Context, client *http.Client, endpoint, ip string) (Geo, error) {
	link := endpoint + "/" + ip + "?lang=zh-CN"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return Geo{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return Geo{}, err
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 512<<10))
	resp.Body.Close()
	if readErr != nil {
		return Geo{}, readErr
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return Geo{}, errors.New("HTTP 429 限流")
	}
	if resp.StatusCode != http.StatusOK {
		return Geo{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var r ipwhoResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return Geo{}, fmt.Errorf("响应解析失败: %w", err)
	}
	if !r.Success {
		msg := r.Message
		if msg == "" {
			msg = "查询未成功"
		}
		return Geo{}, errors.New(msg)
	}
	asn := ""
	if r.Connection.ASN > 0 {
		asn = fmt.Sprintf("AS%d %s", r.Connection.ASN, r.Connection.Org)
	}
	return Geo{
		Country: r.Country,
		Region:  r.Region,
		City:    r.City,
		ISP:     firstNonEmpty(r.Connection.ISP, r.Connection.Org),
		ASN:     asn,
		Proxy:   r.Security.Proxy || r.Security.VPN || r.Security.Tor,
		Hosting: r.Security.Hosting,
	}, nil
}

// GeoChain tries providers in order, asking each only for the addresses still
// unresolved. Partial success is success; an error is returned only when no
// provider resolved anything.
type GeoChain struct {
	Providers []GeoProvider
}

// NewGeoChain returns the default provider order: ip-api batch, then ipwho.is
// over HTTPS.
func NewGeoChain() *GeoChain {
	return &GeoChain{Providers: []GeoProvider{NewIPAPI(), NewIPWhois()}}
}

// Lookup resolves as many addresses as the providers can.
func (c *GeoChain) Lookup(ctx context.Context, ips []string) (map[string]Geo, error) {
	wanted := publicIPs(ips)
	if len(wanted) == 0 {
		return nil, nil
	}
	out := make(map[string]Geo, len(wanted))
	var problems []string

	remaining := wanted
	for _, provider := range c.Providers {
		if ctx.Err() != nil || len(remaining) == 0 {
			break
		}
		got, err := provider.Lookup(ctx, remaining)
		if err != nil {
			problems = append(problems, err.Error())
		}
		var next []string
		for _, ip := range remaining {
			if geo, ok := got[ip]; ok {
				out[ip] = geo
			} else {
				next = append(next, ip)
			}
		}
		remaining = next
	}

	if len(out) == 0 {
		if len(problems) == 0 {
			problems = append(problems, "归属查询未返回结果")
		}
		return nil, errors.New(strings.Join(problems, "；"))
	}
	return out, nil
}
