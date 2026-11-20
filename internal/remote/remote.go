// Package remote runs network checks from third-party vantage points, so a
// node can be judged from many countries instead of only this machine.
package remote

import (
	"context"
	"fmt"
	"math"
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

	// Latitude and Longitude are the probe's own coordinates.
	Latitude, Longitude float64
	// Shared marks a round trip that is too short to have reached a single
	// host: the probe hit a nearby CDN/anycast edge instead, so the number is
	// not the node's own cross-region latency.
	Shared bool
}

// hasLocation reports whether the probe carries plausible coordinates.
func (p Probe) hasLocation() bool {
	return (p.Latitude != 0 || p.Longitude != 0) && p.Latitude >= -90 && p.Latitude <= 90
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
	// Shared counts probes that hit a nearby CDN/anycast edge. Their latency is
	// excluded from Median/Min/Max.
	Shared int
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
	loss := "丢包 " + percentText(r.Loss)
	switch {
	case r.Shared > 0 && r.Median <= 0:
		return fmt.Sprintf("%d/%d 个探测点可达 · %s · 全部命中就近边缘（Anycast/CDN）",
			r.Reachable, len(r.Probes), loss)
	case r.Shared > 0:
		return fmt.Sprintf("%d/%d 个探测点可达 · 中位 %s · %s · %d 个命中就近边缘",
			r.Reachable, len(r.Probes), durationText(r.Median), loss, r.Shared)
	}
	return fmt.Sprintf("%d/%d 个探测点可达 · 中位 %s · %s",
		r.Reachable, len(r.Probes), durationText(r.Median), loss)
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
	rep.Shared = markSharedEdges(probes)
	var latencies []time.Duration
	var lossSum float64
	for _, p := range probes {
		lossSum += p.Loss
		if p.OK {
			rep.Reachable++
			if !p.Shared {
				latencies = append(latencies, p.Latency)
			}
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

// fiberKMperMS is how far light travels in one millisecond inside fibre, about
// 200 km. Any round trip needs at least 2×distance/fiberKMperMS ms.
const fiberKMperMS = 200

// minPairKM is the shortest probe pair worth checking; below it, city-level
// coordinate rounding swamps the physical bound.
const minPairKM = 2500

// sharedTolerance keeps clock and coordinate noise from marking a genuine
// route: a measured sum below this fraction of the physical floor means the
// probes cannot have reached the same distant host.
const sharedTolerance = 0.8

// markSharedEdges flags probes whose pair-wise round trips are physically too
// short to have reached one common host. A proxy on a CDN or anycast network
// answers each probe from a nearby edge, which produces exactly this pattern:
// low RTT with zero loss from every continent at once.
func markSharedEdges(probes []Probe) int {
	for i := 0; i < len(probes); i++ {
		if !probes[i].OK || !probes[i].hasLocation() {
			continue
		}
		for j := i + 1; j < len(probes); j++ {
			if !probes[j].OK || !probes[j].hasLocation() {
				continue
			}
			km := greatCircleKM(probes[i].Latitude, probes[i].Longitude,
				probes[j].Latitude, probes[j].Longitude)
			if km < minPairKM {
				continue
			}
			floor := 2 * km / fiberKMperMS // milliseconds
			measured := (probes[i].Latency + probes[j].Latency).Seconds() * 1000
			if measured < floor*sharedTolerance {
				probes[i].Shared = true
				probes[j].Shared = true
			}
		}
	}
	shared := 0
	for _, p := range probes {
		if p.Shared {
			shared++
		}
	}
	return shared
}

// greatCircleKM is the haversine distance between two coordinates.
func greatCircleKM(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusKM = 6371.0
	rad := math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLon := (lon2 - lon1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthRadiusKM * math.Asin(math.Min(1, math.Sqrt(a)))
}
