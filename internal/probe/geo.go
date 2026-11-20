package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// GeoProvider looks up registry data for public IPs in bulk.
type GeoProvider interface {
	Lookup(ctx context.Context, ips []string) (map[string]Geo, error)
}

// IPAPI queries ip-api.com's free batch endpoint. It is only ever given public
// addresses, and a failure is non-fatal: the table just shows no geo data.
type IPAPI struct {
	Client   *http.Client
	Endpoint string
}

// NewIPAPI returns a provider with a 6 second budget per batch.
func NewIPAPI() *IPAPI {
	return &IPAPI{
		Client:   &http.Client{Timeout: 6 * time.Second},
		Endpoint: "http://ip-api.com/batch?fields=status,message,country,regionName,city,isp,org,as,proxy,hosting,mobile&lang=zh-CN",
	}
}

type ipapiEntry struct {
	Query      string `json:"query"`
	Status     string `json:"status"`
	Message    string `json:"message"`
	Country    string `json:"country"`
	RegionName string `json:"regionName"`
	City       string `json:"city"`
	ISP        string `json:"isp"`
	Org        string `json:"org"`
	AS         string `json:"as"`
	Proxy      bool   `json:"proxy"`
	Hosting    bool   `json:"hosting"`
	Mobile     bool   `json:"mobile"`
}

// Lookup asks for at most 100 addresses per request, as the free plan allows.
func (g *IPAPI) Lookup(ctx context.Context, ips []string) (map[string]Geo, error) {
	targets := publicIPs(ips)
	if len(targets) == 0 {
		return nil, nil
	}
	endpoint := g.Endpoint
	if endpoint == "" {
		endpoint = NewIPAPI().Endpoint
	}
	client := g.Client
	if client == nil {
		client = NewIPAPI().Client
	}

	out := make(map[string]Geo, len(targets))
	for start := 0; start < len(targets); start += 100 {
		end := min(start+100, len(targets))
		batch := targets[start:end]

		payload, err := json.Marshal(batch)
		if err != nil {
			return out, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
		if err != nil {
			return out, err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return out, fmt.Errorf("IP 归属查询失败: %w", err)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if readErr != nil {
			return out, readErr
		}
		if resp.StatusCode != http.StatusOK {
			return out, fmt.Errorf("IP 归属查询失败: HTTP %d", resp.StatusCode)
		}

		var entries []ipapiEntry
		if err := json.Unmarshal(body, &entries); err != nil {
			return out, fmt.Errorf("IP 归属响应解析失败: %w", err)
		}
		for _, e := range entries {
			if e.Status != "success" {
				continue
			}
			out[e.Query] = Geo{
				Country: e.Country,
				Region:  e.RegionName,
				City:    e.City,
				ISP:     firstNonEmpty(e.ISP, e.Org),
				ASN:     e.AS,
				Proxy:   e.Proxy,
				Hosting: e.Hosting,
				Mobile:  e.Mobile,
			}
		}
	}
	return out, nil
}

func publicIPs(ips []string) []string {
	seen := make(map[string]bool, len(ips))
	out := make([]string, 0, len(ips))
	for _, s := range ips {
		ip := net.ParseIP(strings.TrimSpace(s))
		if ip == nil || ip.IsPrivate() || ip.IsLoopback() || ip.IsUnspecified() ||
			ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
			continue
		}
		key := ip.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, key)
	}
	return out
}
