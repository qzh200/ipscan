// Package remote runs network checks from third-party vantage points, so a
// node can be judged from many countries instead of only this machine.
package remote

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Probe is one vantage point's measurement of the target.
type Probe struct {
	Country string
	City    string
	Network string
	ASN     string
	// Latency is the average round trip of the probe's packets.
	Latency time.Duration
	// Loss is 0..1.
	Loss  float64
	OK    bool
	Error string
}

// Report aggregates the probes of one measurement.
type Report struct {
	Target        string
	Provider      string
	MeasurementID string
	At            time.Time
	Probes        []Probe
	Reachable     int
	Median        time.Duration
	Min           time.Duration
	Max           time.Duration
	// Loss is the mean packet loss across all probes.
	Loss float64
}

// Pinger tests a host from several locations. limit is the probe count.
type Pinger interface {
	Ping(ctx context.Context, target string, limit int) (Report, error)
}

// Summary renders one line for the drawer.
func (r Report) Summary() string {
	if len(r.Probes) == 0 {
		return "没有探测点返回结果"
	}
	if r.Reachable == 0 {
		return "全部探测点均不可达"
	}
	return fmt.Sprintf("%d/%d 个探测点可达 · 中位 %s · 丢包 %s",
		r.Reachable, len(r.Probes), durationText(r.Median), percentText(r.Loss))
}

// LocationLabel is "Helsinki · Hetzner Online (AS24940)".
func (p Probe) LocationLabel() string {
	parts := make([]string, 0, 3)
	if p.City != "" && p.Country != "" {
		parts = append(parts, p.City+" · "+p.Country)
	} else if p.City != "" {
		parts = append(parts, p.City)
	} else if p.Country != "" {
		parts = append(parts, p.Country)
	}
	if p.Network != "" {
		parts = append(parts, p.Network)
	}
	label := strings.Join(parts, " · ")
	if p.ASN != "" {
		label += " (" + p.ASN + ")"
	}
	return label
}

// LatencyLabel prints the average, or the failure reason.
func (p Probe) LatencyLabel() string {
	if !p.OK {
		if p.Error != "" {
			return p.Error
		}
		return "不可达"
	}
	return durationText(p.Latency)
}

// LossLabel prints the probe's packet loss.
func (p Probe) LossLabel() string { return percentText(p.Loss) }

// summarize sorts probes for display and computes the aggregates.
func summarize(target, provider, id string, probes []Probe) Report {
	sort.SliceStable(probes, func(i, j int) bool {
		if probes[i].OK != probes[j].OK {
			return probes[i].OK
		}
		if probes[i].OK {
			return probes[i].Latency < probes[j].Latency
		}
		return probes[i].City < probes[j].City
	})

	rep := Report{
		Target:        target,
		Provider:      provider,
		MeasurementID: id,
		At:            time.Now(),
		Probes:        probes,
	}
	var latencies []time.Duration
	var lossSum float64
	for _, p := range probes {
		lossSum += p.Loss
		if p.OK {
			rep.Reachable++
			latencies = append(latencies, p.Latency)
		}
	}
	if len(probes) > 0 {
		rep.Loss = lossSum / float64(len(probes))
	}
	if len(latencies) > 0 {
		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		rep.Min = latencies[0]
		rep.Max = latencies[len(latencies)-1]
		mid := len(latencies) / 2
		if len(latencies)%2 == 0 {
			rep.Median = (latencies[mid-1] + latencies[mid]) / 2
		} else {
			rep.Median = latencies[mid]
		}
	}
	return rep
}

func durationText(d time.Duration) string {
	if d <= 0 {
		return "—"
	}
	if d < time.Millisecond {
		return "<1 ms"
	}
	return fmt.Sprintf("%d ms", d.Milliseconds())
}

func percentText(v float64) string {
	if v <= 0 {
		return "0%"
	}
	return fmt.Sprintf("%.0f%%", v*100)
}
