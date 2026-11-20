package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Globalping calls api.globalping.io, an open measurement API backed by probes
// in many countries. It has no key requirement and returns structured stats.
type Globalping struct {
	BaseURL      string
	Client       *http.Client
	PollInterval time.Duration
	Timeout      time.Duration
	// Packets is how many ICMP packets each probe sends.
	Packets int
}

// NewGlobalping returns a client with a 45 second measurement budget.
func NewGlobalping() *Globalping {
	return &Globalping{
		BaseURL:      "https://api.globalping.io",
		Client:       &http.Client{Timeout: 15 * time.Second},
		PollInterval: time.Second,
		Timeout:      45 * time.Second,
		Packets:      4,
	}
}

type gpCreate struct {
	Type      string              `json:"type"`
	Target    string              `json:"target"`
	Limit     int                 `json:"limit"`
	Locations []map[string]string `json:"locations"`
	Options   struct {
		Packets int `json:"packets"`
	} `json:"measurementOptions"`
}

type gpCreated struct {
	ID          string `json:"id"`
	ProbesCount int    `json:"probesCount"`
	Error       *struct {
		Type    string            `json:"type"`
		Message string            `json:"message"`
		Params  map[string]string `json:"params"`
	} `json:"error"`
}

type gpMeasurement struct {
	ID      string     `json:"id"`
	Type    string     `json:"type"`
	Status  string     `json:"status"`
	Target  string     `json:"target"`
	Results []gpResult `json:"results"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type gpResult struct {
	Probe struct {
		Continent string  `json:"continent"`
		Country   string  `json:"country"`
		City      string  `json:"city"`
		Network   string  `json:"network"`
		ASN       int     `json:"asn"`
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
	} `json:"probe"`
	Result struct {
		Status string `json:"status"`
		Error  string `json:"error"`
		Stats  *struct {
			Min   float64 `json:"min"`
			Max   float64 `json:"max"`
			Avg   float64 `json:"avg"`
			Total int     `json:"total"`
			Rcv   int     `json:"rcv"`
			Drop  int     `json:"drop"`
			Loss  float64 `json:"loss"`
		} `json:"stats"`
	} `json:"result"`
}

// Ping runs one Globalping ping measurement and waits for its results.
func (g *Globalping) Ping(ctx context.Context, target string, limit int) (Report, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return Report{}, errors.New("目标为空")
	}
	base := strings.TrimRight(g.BaseURL, "/")
	if base == "" {
		base = "https://api.globalping.io"
	}
	client := g.Client
	if client == nil {
		client = NewGlobalping().Client
	}
	timeout := g.Timeout
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	interval := g.PollInterval
	if interval <= 0 {
		interval = time.Second
	}
	packets := g.Packets
	if packets <= 0 {
		packets = 4
	}
	if limit <= 0 {
		limit = 8
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	id, err := g.create(ctx, client, base, target, limit, packets)
	if err != nil {
		return Report{}, err
	}

	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return Report{}, fmt.Errorf("等待全球探测结果超时（%s）", timeout)
		case <-timer.C:
		}

		m, err := g.fetch(ctx, client, base, id)
		if err != nil {
			return Report{}, err
		}
		switch m.Status {
		case "finished":
			return g.report(target, id, m), nil
		case "failed":
			msg := "测量失败"
			if m.Error != nil && m.Error.Message != "" {
				msg = m.Error.Message
			}
			return Report{}, errors.New(msg)
		}
		timer.Reset(interval)
	}
}

func (g *Globalping) create(ctx context.Context, client *http.Client, base, target string, limit, packets int) (string, error) {
	payload := gpCreate{
		Type:      "ping",
		Target:    target,
		Limit:     limit,
		Locations: []map[string]string{{"magic": "World"}},
	}
	payload.Options.Packets = packets
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/measurements", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "ipscan/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("Globalping 请求失败: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if err != nil {
		return "", err
	}

	var created gpCreated
	_ = json.Unmarshal(data, &created)
	if resp.StatusCode == http.StatusTooManyRequests {
		return "", errors.New("Globalping 限流（每小时 500 次），请稍后再试")
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg := strings.TrimSpace(string(data))
		if created.Error != nil && created.Error.Message != "" {
			msg = created.Error.Message
			if len(created.Error.Params) > 0 {
				for k, v := range created.Error.Params {
					msg += "：" + k + " " + v
				}
			}
		}
		return "", fmt.Errorf("Globalping 拒绝测量: %s", msg)
	}
	if created.ID == "" {
		return "", errors.New("Globalping 未返回测量 ID")
	}
	return created.ID, nil
}

func (g *Globalping) fetch(ctx context.Context, client *http.Client, base, id string) (gpMeasurement, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/measurements/"+id, nil)
	if err != nil {
		return gpMeasurement{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ipscan/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return gpMeasurement{}, fmt.Errorf("读取全球探测结果失败: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return gpMeasurement{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return gpMeasurement{}, fmt.Errorf("读取全球探测结果失败: HTTP %d", resp.StatusCode)
	}
	var m gpMeasurement
	if err := json.Unmarshal(data, &m); err != nil {
		return gpMeasurement{}, fmt.Errorf("全球探测结果解析失败: %w", err)
	}
	return m, nil
}

func (g *Globalping) report(target, id string, m gpMeasurement) Report {
	probes := make([]Probe, 0, len(m.Results))
	for _, r := range m.Results {
		p := Probe{
			Country:   r.Probe.Country,
			City:      r.Probe.City,
			Network:   r.Probe.Network,
			Latitude:  r.Probe.Latitude,
			Longitude: r.Probe.Longitude,
		}
		if r.Probe.ASN > 0 {
			p.ASN = fmt.Sprintf("AS%d", r.Probe.ASN)
		}
		stats := r.Result.Stats
		if stats != nil && stats.Rcv > 0 {
			p.OK = true
			p.Latency = milliseconds(stats.Avg)
			if p.Latency <= 0 {
				p.Latency = milliseconds(stats.Min)
			}
			switch {
			case stats.Total > 0:
				p.Loss = float64(stats.Total-stats.Rcv) / float64(stats.Total)
			case stats.Loss > 0 && stats.Loss <= 1:
				p.Loss = stats.Loss
			}
		} else {
			p.Error = r.Result.Error
			if p.Error == "" && r.Result.Status == "failed" {
				p.Error = "探测失败"
			}
			p.Loss = 1
		}
		probes = append(probes, p)
	}
	return summarize(target, "Globalping", id, probes)
}

func milliseconds(ms float64) time.Duration {
	if ms <= 0 {
		return 0
	}
	return time.Duration(ms * float64(time.Millisecond))
}
