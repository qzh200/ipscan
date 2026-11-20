package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"ipscan/internal/model"
	"ipscan/internal/probe"
	"ipscan/internal/scan"
)

type testFetcher struct {
	byLink map[string]string
	err    error
}

func (f testFetcher) Fetch(ctx context.Context, link string) ([]byte, error) {
	if f.err != nil {
		return nil, f.err
	}
	if strings.HasPrefix(link, "slow") {
		select {
		case <-time.After(200 * time.Millisecond):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if _, ok := f.byLink[link]; !ok {
		return nil, fmt.Errorf("no subscription for %s", link)
	}
	// A one-node subscription, named after the link, so a stale result is easy
	// to spot.
	return []byte(fmt.Sprintf("trojan://pw@1.1.1.1:443#%s\n", link)), nil
}

type testScanner struct{}

func (testScanner) Scan(ctx context.Context, nodes []model.Node, progress func(scan.Progress)) []probe.Result {
	out := make([]probe.Result, len(nodes))
	for i := range nodes {
		r := probe.Result{
			IP: "1.1.1.1", Family: "IPv4", Reachable: true, Samples: 3, Succeeded: 3,
			Avg: 100 * time.Millisecond, Jitter: 4 * time.Millisecond, Score: 80, Grade: "良好",
		}
		if strings.Contains(nodes[i].Name, "dead") {
			r = probe.Result{IP: "1.1.1.1", Family: "IPv4", Samples: 3, Err: "refused", Grade: "不可达"}
		}
		out[i] = r
		progress(scan.Progress{Done: i + 1, Total: len(nodes), Index: i, Result: r})
	}
	return out
}

func waitStatus(t *testing.T, a *App, want Status) Snapshot {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snap := a.Snapshot()
		if snap.Status == want {
			return snap
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("status never became %v; last: %+v", want, a.Snapshot())
	return Snapshot{}
}

func TestLoadReportsFetchError(t *testing.T) {
	a := New(testFetcher{err: errors.New("network down")}, testScanner{})
	*a.Input = "https://example.com/sub"
	a.Load()

	snap := waitStatus(t, a, StatusError)
	if !strings.Contains(snap.Message, "network down") {
		t.Fatalf("message = %q", snap.Message)
	}
}

func TestLoadComputesStatsAndSelection(t *testing.T) {
	a := New(testFetcher{byLink: map[string]string{"https://a": ""}}, testScanner{})
	*a.Input = "https://a"
	a.Load()
	snap := waitStatus(t, a, StatusDone)

	if snap.Stats.Total != 1 || snap.Stats.Reachable != 1 {
		t.Fatalf("stats = %+v", snap.Stats)
	}
	if snap.Stats.AvgDelay != 100*time.Millisecond || snap.Stats.AvgScore != 80 {
		t.Fatalf("averages = %+v", snap.Stats)
	}
	if snap.Rows[0].Node.Name != "https://a" {
		t.Fatalf("row = %+v", snap.Rows[0])
	}

	a.Select(0)
	if row, ok := a.SelectedRow(); !ok || row.Node.Name != "https://a" {
		t.Fatalf("selected = %+v ok=%v", row, ok)
	}
	a.Select(-1)
	if _, ok := a.SelectedRow(); ok {
		t.Fatal("selection should be cleared")
	}
}

func TestNewLoadCancelsStaleOne(t *testing.T) {
	a := New(testFetcher{byLink: map[string]string{
		"slow": "slow", "fast": "fast",
	}}, testScanner{})
	*a.Input = "slow"
	a.Load()
	*a.Input = "fast"
	a.Load()

	snap := waitStatus(t, a, StatusDone)
	if len(snap.Rows) != 1 || snap.Rows[0].Node.Name != "fast" {
		t.Fatalf("stale load won: %+v", snap.Rows)
	}
	time.Sleep(250 * time.Millisecond)
	snap = a.Snapshot()
	if len(snap.Rows) != 1 || snap.Rows[0].Node.Name != "fast" {
		t.Fatalf("stale load overwrote rows later: %+v", snap.Rows)
	}
}

func TestUDPNodeIsNotCountedReachable(t *testing.T) {
	a := New(testFetcher{byLink: map[string]string{"udp": ""}}, udpScanner{})
	*a.Input = "udp"
	a.Load()
	snap := waitStatus(t, a, StatusDone)

	if snap.Stats.Reachable != 0 {
		t.Fatalf("reachable = %d, want 0", snap.Stats.Reachable)
	}
	if !strings.Contains(snap.Message, "QUIC") {
		t.Fatalf("message = %q", snap.Message)
	}
}

type udpScanner struct{}

func (udpScanner) Scan(ctx context.Context, nodes []model.Node, progress func(scan.Progress)) []probe.Result {
	r := probe.Result{UDPOnly: true, Grade: "未探测", Samples: 1}
	progress(scan.Progress{Done: 1, Total: 1, Index: 0, Result: r})
	return []probe.Result{r}
}

func TestGeoFailureIsReportedWithoutFailingScan(t *testing.T) {
	a := New(testFetcher{byLink: map[string]string{"geo": ""}}, geoFailScanner{})
	*a.Input = "geo"
	a.Load()
	snap := waitStatus(t, a, StatusDone)

	if snap.GeoNote == "" {
		t.Fatal("expected the geo lookup failure to surface")
	}
	if len(snap.Rows) != 1 || !snap.Rows[0].Probed {
		t.Fatalf("rows should still be measured: %+v", snap.Rows)
	}
}

type geoFailScanner struct{}

func (geoFailScanner) Scan(ctx context.Context, nodes []model.Node, progress func(scan.Progress)) ([]probe.Result, error) {
	r := probe.Result{Reachable: true, Score: 88, Grade: "优秀", Avg: time.Millisecond}
	progress(scan.Progress{Done: 1, Total: 1, Index: 0, Result: r})
	return []probe.Result{r}, errors.New("IP 归属查询失败: blocked")
}
