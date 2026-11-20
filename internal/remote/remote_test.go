package remote

import (
	"strings"
	"testing"
	"time"
)

func probe(lat, lon float64, ms float64) Probe {
	return Probe{
		Country: "XX", City: "City", OK: true,
		Latitude: lat, Longitude: lon,
		Latency: time.Duration(ms * float64(time.Millisecond)),
	}
}

// Real Globalping output for an anycast target: 1 ms in Helsinki, 31 ms in Los
// Angeles, 1 ms in Singapore — physically impossible for one host.
func TestSummarizeFlagsAnycast(t *testing.T) {
	probes := []Probe{
		probe(60.17, 24.94, 1.28),
		probe(34.05, -118.24, 31.2),
		probe(1.29, 103.85, 1.28),
	}
	rep := summarize("example.com", "Globalping", "m1", probes)
	if rep.Shared != 3 {
		t.Fatalf("shared = %d, want 3", rep.Shared)
	}
	if rep.Median != 0 {
		t.Fatalf("median = %v, want 0 when every probe is an edge", rep.Median)
	}
	if !strings.Contains(rep.Summary(), "全部命中就近边缘") {
		t.Fatalf("summary = %q", rep.Summary())
	}
}

// A real cross-region route: the numbers are far above the physical floor.
func TestSummarizeKeepsGenuineRoute(t *testing.T) {
	probes := []Probe{
		probe(60.17, 24.94, 240),
		probe(34.05, -118.24, 180),
		probe(1.29, 103.85, 200),
	}
	rep := summarize("example.com", "Globalping", "m1", probes)
	if rep.Shared != 0 {
		t.Fatalf("shared = %d, want 0", rep.Shared)
	}
	if rep.Median != 200*time.Millisecond {
		t.Fatalf("median = %v", rep.Median)
	}
	if !strings.Contains(rep.Summary(), "中位 200 ms") || strings.Contains(rep.Summary(), "边缘") {
		t.Fatalf("summary = %q", rep.Summary())
	}
}

// Close probes are below minPairKM: coordinate rounding must not flag them.
func TestSummarizeIgnoresNearbyPairs(t *testing.T) {
	probes := []Probe{probe(50.48, 12.37, 1), probe(50.11, 8.68, 1)}
	rep := summarize("example.com", "Globalping", "m1", probes)
	if rep.Shared != 0 {
		t.Fatalf("shared = %d, want 0", rep.Shared)
	}
	if rep.Median != time.Millisecond {
		t.Fatalf("median = %v", rep.Median)
	}
}

func TestGreatCircle(t *testing.T) {
	if got := greatCircleKM(60.17, 24.94, 34.05, -118.24); got < 8900 || got > 9100 {
		t.Fatalf("Helsinki–Los Angeles = %.0f km", got)
	}
}
