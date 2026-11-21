package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"ipscan/internal/model"
	"ipscan/internal/probe"
	"ipscan/internal/remote"
	"ipscan/internal/scan"
	"ipscan/internal/speedtest"
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

func (testScanner) Scan(ctx context.Context, nodes []model.Node, progress func(scan.Progress)) ([]probe.Result, error) {
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
	return out, nil
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

func (udpScanner) Scan(ctx context.Context, nodes []model.Node, progress func(scan.Progress)) ([]probe.Result, error) {
	r := probe.Result{UDPOnly: true, Grade: "未探测", Samples: 1}
	progress(scan.Progress{Done: 1, Total: 1, Index: 0, Result: r})
	return []probe.Result{r}, nil
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

type testPinger struct{ rep remote.Report }

func (p testPinger) Ping(context.Context, string, int) (remote.Report, error) { return p.rep, nil }

func TestCheckGlobalAttachesReport(t *testing.T) {
	a := New(testFetcher{byLink: map[string]string{"https://a": ""}}, testScanner{})
	a.Remote = testPinger{rep: remote.Report{
		Provider: "Globalping", Target: "1.1.1.1",
		Probes:    []remote.Probe{{Country: "FI", City: "Helsinki", OK: true, Latency: 10 * time.Millisecond}},
		Reachable: 1, Median: 10 * time.Millisecond, Min: 10 * time.Millisecond, Max: 10 * time.Millisecond,
	}}
	*a.Input = "https://a"
	a.Load()
	waitStatus(t, a, StatusDone)

	a.CheckGlobal(0)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snap := a.Snapshot()
		if len(snap.Rows) == 1 && snap.Rows[0].Remote != nil {
			if snap.Rows[0].Remote.Median != 10*time.Millisecond {
				t.Fatalf("report = %+v", snap.Rows[0].Remote)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("global report never attached")
}

type staticFetcher struct{ body string }

func (f staticFetcher) Fetch(context.Context, string) ([]byte, error) { return []byte(f.body), nil }

// countingScanner returns a different average on every call, so a re-check's
// new result is distinguishable from the original one.
type countingScanner struct {
	mu    sync.Mutex
	calls int
}

func (s *countingScanner) Scan(ctx context.Context, nodes []model.Node, _ func(scan.Progress)) ([]probe.Result, error) {
	s.mu.Lock()
	s.calls++
	call := s.calls
	s.mu.Unlock()
	out := make([]probe.Result, len(nodes))
	for i := range out {
		out[i] = probe.Result{
			IP: "1.1.1.1", Family: "IPv4", Reachable: true,
			Samples: 3, Succeeded: 3,
			Avg:   time.Duration(call) * time.Millisecond,
			Score: 60 + call, Grade: "良好",
		}
	}
	return out, nil
}

func TestReprobeNodeUpdatesOnlyThatRow(t *testing.T) {
	const body = `
proxies:
  - {name: 甲, type: trojan, server: a.example.com, port: 443, password: pw}
  - {name: 乙, type: trojan, server: b.example.com, port: 443, password: pw}
`
	sc := &countingScanner{}
	a := New(staticFetcher{body: body}, sc)
	*a.Input = "local"
	a.Load()
	snap := waitStatus(t, a, StatusDone)
	if len(snap.Rows) != 2 {
		t.Fatalf("rows = %+v", snap.Rows)
	}
	first := snap.Rows[0].Result.Avg
	firstScore := snap.Rows[0].Result.Score

	a.ReprobeNode(-1)
	a.ReprobeNode(2)
	if snap = a.Snapshot(); snap.Rows[0].Result.Avg != first {
		t.Fatalf("out-of-range reprobe changed a row: %+v", snap.Rows)
	}

	a.ReprobeNode(1)
	deadline := time.Now().Add(2 * time.Second)
	for {
		snap = a.Snapshot()
		if snap.Rows[1].Probed && !snap.Rows[1].Probing && snap.Rows[1].Result.Avg != first {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("single reprobe never finished: %+v", snap.Rows)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if snap.Status != StatusDone {
		t.Fatalf("status = %v, want done", snap.Status)
	}
	if snap.Rows[0].Result.Avg != first || snap.Rows[0].Result.Score != firstScore {
		t.Fatalf("row 0 changed: %+v", snap.Rows[0].Result)
	}
	sc.mu.Lock()
	calls := sc.calls
	sc.mu.Unlock()
	if calls != 2 {
		t.Fatalf("scanner calls = %d, want one load + one single-node check", calls)
	}
}

type fakeSpeedTester struct {
	mu       sync.Mutex
	measured int
}

func (f *fakeSpeedTester) Measure(context.Context, model.Node) (speedtest.Result, error) {
	f.mu.Lock()
	f.measured++
	f.mu.Unlock()
	return speedtest.Result{
		MBps: 12.5, Bytes: 10 << 20, Elapsed: time.Second,
		Connect: 40 * time.Millisecond, Target: "speed.example",
	}, nil
}

func TestSpeedTestsLeaveScanStateAlone(t *testing.T) {
	const body = `
proxies:
  - {name: 甲, type: trojan, server: a.example.com, port: 443, password: pw}
  - {name: 乙, type: trojan, server: b.example.com, port: 443, password: pw}
`
	speed := &fakeSpeedTester{}
	a := New(staticFetcher{body: body}, &countingScanner{})
	a.Speed = speed
	*a.Input = "local"
	a.Load()
	waitStatus(t, a, StatusDone)

	a.TestSpeed(1)
	waitRows := func() Snapshot {
		deadline := time.Now().Add(2 * time.Second)
		for {
			snap := a.Snapshot()
			if len(snap.Rows) == 2 && !snap.Rows[0].SpeedBusy && !snap.Rows[1].SpeedBusy &&
				snap.Rows[0].Speed != nil && snap.Rows[1].Speed != nil {
				return snap
			}
			if time.Now().After(deadline) {
				t.Fatalf("speed tests never finished: %+v", snap.Rows)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		snap := a.Snapshot()
		if snap.Rows[1].Speed != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("single speed test never finished: %+v", snap.Rows)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if snap := a.Snapshot(); snap.Rows[0].Speed != nil || snap.Rows[1].Speed.MBps != 12.5 {
		t.Fatalf("single speed result = %+v", snap.Rows)
	}

	a.TestAllSpeeds()
	snap := waitRows()
	if snap.Status != StatusDone {
		t.Fatalf("status = %v, want done", snap.Status)
	}
	for _, row := range snap.Rows {
		if row.Speed == nil || row.Speed.MBps != 12.5 {
			t.Fatalf("row speed = %+v", row.Speed)
		}
	}
	speed.mu.Lock()
	measured := speed.measured
	speed.mu.Unlock()
	if measured != 3 {
		t.Fatalf("measurements = %d, want 1 single + 2 batch", measured)
	}
}

type blockingSpeed struct{ started chan struct{} }

func (b *blockingSpeed) Measure(ctx context.Context, _ model.Node) (speedtest.Result, error) {
	close(b.started)
	<-ctx.Done()
	return speedtest.Result{}, ctx.Err()
}

func TestCancelSpeedClearsBusyState(t *testing.T) {
	a := New(staticFetcher{body: `
proxies:
  - {name: 甲, type: trojan, server: a.example.com, port: 443, password: pw}
`}, &countingScanner{})
	b := &blockingSpeed{started: make(chan struct{})}
	a.Speed = b
	*a.Input = "local"
	a.Load()
	waitStatus(t, a, StatusDone)

	a.TestSpeed(0)
	<-b.started
	a.CancelSpeed()
	snap := a.Snapshot()
	if snap.Rows[0].SpeedBusy || snap.Rows[0].SpeedErr != "" || snap.Rows[0].Speed != nil {
		t.Fatalf("row after cancel = %+v", snap.Rows[0])
	}
}
