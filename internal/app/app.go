// Package app owns the application state and the background work that fills
// it. It knows nothing about drawing; the screen package maps a Snapshot to
// widgets.
package app

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"ipscan/internal/model"
	"ipscan/internal/probe"
	"ipscan/internal/remote"
	"ipscan/internal/scan"
	"ipscan/internal/subscription"
)

// Fetcher downloads a subscription payload.
type Fetcher interface {
	Fetch(ctx context.Context, link string) ([]byte, error)
}

// Pinger runs a network check from third-party locations.
type Pinger interface {
	Ping(ctx context.Context, target string, limit int) (remote.Report, error)
}

// Scanner probes parsed nodes and reports each finished one. The error, when
// non-nil, only describes the optional IP registry lookup.
type Scanner interface {
	Scan(ctx context.Context, nodes []model.Node, progress func(scan.Progress)) ([]probe.Result, error)
}

// Status is the phase the app is in.
type Status int

const (
	StatusIdle Status = iota
	StatusLoading
	StatusProbing
	StatusDone
	StatusError
)

// Row is one subscription entry with its latest measurement.
type Row struct {
	Node   model.Node
	Result probe.Result
	Probed bool
	// Remote is the latest worldwide ping, nil until requested.
	Remote     *remote.Report
	RemoteBusy bool
	RemoteErr  string
}

// Stats summarizes the visible result set.
type Stats struct {
	Total     int
	Probed    int
	Reachable int
	Healthy   int
	AvgDelay  time.Duration
	AvgScore  int
}

// Snapshot is a consistent copy of everything the view needs.
type Snapshot struct {
	Rows     []Row
	Format   string
	Warnings []string
	Status   Status
	Message  string
	Done     int
	Total    int
	Selected int
	Elapsed  time.Duration
	Stats    Stats
	// GeoNote explains why IP registry data is missing, empty when it worked.
	GeoNote string
}

// App holds the state. Methods are safe for concurrent use.
type App struct {
	Fetcher Fetcher
	Scanner Scanner
	// Remote runs the optional worldwide ping; nil hides the feature.
	Remote Pinger
	// Input is bound directly to the URL text field.
	Input *string

	mu       sync.Mutex
	notify   func()
	gen      int
	cancel   context.CancelFunc
	rows     []Row
	format   string
	warnings []string
	status   Status
	message  string
	done     int
	total    int
	selected int
	started  time.Time
	elapsed  time.Duration
	geoNote  string

	remote       map[int]remote.Report
	remoteErrs   map[int]string
	remoteBusy   int
	remoteGen    int
	remoteCancel context.CancelFunc
}

// New creates an idle app.
func New(f Fetcher, s Scanner) *App {
	return &App{
		Fetcher: f, Scanner: s, Input: new(string), selected: -1,
		remote: map[int]remote.Report{}, remoteErrs: map[int]string{}, remoteBusy: -1,
	}
}

// SetNotifier installs the function that wakes the UI, usually c.Invalidate.
func (a *App) SetNotifier(fn func()) {
	a.mu.Lock()
	a.notify = fn
	a.mu.Unlock()
}

func (a *App) wake() {
	a.mu.Lock()
	fn := a.notify
	a.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// Load fetches, parses and probes the subscription in the URL field. A new
// load cancels the previous one.
func (a *App) Load() {
	link := strings.TrimSpace(*a.Input)
	if link == "" {
		a.setError("请先填写订阅链接")
		return
	}
	a.mu.Lock()
	a.rows = nil
	a.format = ""
	a.warnings = nil
	a.status = StatusLoading
	a.message = "正在获取订阅…"
	a.geoNote = ""
	a.done, a.total = 0, 0
	a.selected = -1
	a.elapsed = 0
	a.started = time.Now()
	a.clearRemoteLocked()
	a.mu.Unlock()
	a.wake()

	a.start(func(ctx context.Context, gen int) {
		a.load(ctx, gen, link)
	})
}

// Reprobe re-runs the measurements without downloading the subscription again.
func (a *App) Reprobe() {
	a.mu.Lock()
	if len(a.rows) == 0 {
		a.mu.Unlock()
		return
	}
	nodes := make([]model.Node, len(a.rows))
	for i := range a.rows {
		nodes[i] = a.rows[i].Node
		a.rows[i].Probed = false
		a.rows[i].Result = probe.Result{}
	}
	a.status = StatusProbing
	a.message = "正在重新检测…"
	a.geoNote = ""
	a.done, a.total = 0, len(nodes)
	a.elapsed = 0
	a.started = time.Now()
	a.clearRemoteLocked()
	a.mu.Unlock()
	a.wake()

	a.start(func(ctx context.Context, gen int) {
		a.probe(ctx, gen, nodes)
	})
}

// Stop cancels a running load or probe.
func (a *App) Stop() {
	a.mu.Lock()
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
	if a.status == StatusLoading || a.status == StatusProbing {
		a.status = StatusDone
		a.message = "已停止"
	}
	a.clearRemoteLocked()
	a.mu.Unlock()
	a.wake()
}

// Select marks the row shown in the detail panel; -1 closes it.
func (a *App) Select(index int) {
	a.mu.Lock()
	a.selected = index
	a.mu.Unlock()
	a.wake()
}

// Snapshot returns a consistent copy of the state.
func (a *App) Snapshot() Snapshot {
	a.mu.Lock()
	defer a.mu.Unlock()

	rows := make([]Row, len(a.rows))
	copy(rows, a.rows)
	for i := range rows {
		if rep, ok := a.remote[i]; ok {
			r := rep
			rows[i].Remote = &r
		}
		rows[i].RemoteBusy = a.remoteBusy == i
		rows[i].RemoteErr = a.remoteErrs[i]
	}
	warnings := append([]string(nil), a.warnings...)

	stats := Summarize(rows)

	elapsed := a.elapsed
	if a.status == StatusLoading || a.status == StatusProbing {
		elapsed = time.Since(a.started)
	}
	return Snapshot{
		Rows:     rows,
		Format:   a.format,
		Warnings: warnings,
		Status:   a.status,
		Message:  a.message,
		Done:     a.done,
		Total:    a.total,
		Selected: a.selected,
		Elapsed:  elapsed,
		Stats:    stats,
		GeoNote:  a.geoNote,
	}
}

// Summarize computes the counts and averages shown by the summary cards.
// The screen passes the currently filtered rows, so the cards follow the
// subscription scope the table shows.
func Summarize(rows []Row) Stats {
	stats := Stats{Total: len(rows)}
	var delaySum time.Duration
	var delayCount, scoreSum, scoreCount int
	for _, r := range rows {
		if !r.Probed {
			continue
		}
		stats.Probed++
		if r.Result.Reachable {
			stats.Reachable++
			if r.Result.Avg > 0 {
				delaySum += r.Result.Avg
				delayCount++
			}
			if r.Result.Score > 0 {
				scoreSum += r.Result.Score
				scoreCount++
			}
			if r.Result.Score >= 70 {
				stats.Healthy++
			}
		}
	}
	if delayCount > 0 {
		stats.AvgDelay = delaySum / time.Duration(delayCount)
	}
	if scoreCount > 0 {
		stats.AvgScore = scoreSum / scoreCount
	}
	return stats
}

func (a *App) start(job func(ctx context.Context, gen int)) {
	a.mu.Lock()
	if a.cancel != nil {
		a.cancel()
	}
	a.gen++
	gen := a.gen
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.mu.Unlock()

	go func() {
		defer cancel()
		job(ctx, gen)
	}()
}

func (a *App) load(ctx context.Context, gen int, link string) {
	if a.Fetcher == nil {
		a.update(gen, func() { a.failLocked("未配置订阅下载器") })
		return
	}
	body, err := a.Fetcher.Fetch(ctx, link)
	if err != nil {
		a.update(gen, func() { a.failLocked(err.Error()) })
		return
	}
	doc, err := subscription.Parse(body)
	if err != nil {
		a.update(gen, func() { a.failLocked(err.Error()) })
		return
	}

	rows := make([]Row, len(doc.Nodes))
	for i := range doc.Nodes {
		rows[i].Node = doc.Nodes[i]
	}
	a.update(gen, func() {
		a.rows = rows
		a.format = doc.Format
		a.warnings = doc.Warnings
		a.status = StatusProbing
		a.message = fmt.Sprintf("已解析 %d 个节点，正在检测…", len(rows))
		a.total = len(rows)
		a.done = 0
		a.started = time.Now()
	})
	a.probe(ctx, gen, doc.Nodes)
}

func (a *App) probe(ctx context.Context, gen int, nodes []model.Node) {
	if a.Scanner == nil {
		a.update(gen, func() { a.failLocked("未配置探测器") })
		return
	}
	results, geoErr := a.Scanner.Scan(ctx, nodes, func(p scan.Progress) {
		a.update(gen, func() {
			if p.Index >= 0 && p.Index < len(a.rows) {
				a.rows[p.Index].Result = p.Result
				a.rows[p.Index].Probed = true
			}
			a.done = p.Done
			a.total = p.Total
		})
	})
	if ctx.Err() != nil {
		return
	}
	note := ""
	if geoErr != nil {
		note = geoErr.Error()
	}
	a.update(gen, func() {
		a.geoNote = note
		for i := range results {
			if i >= len(a.rows) {
				break
			}
			a.rows[i].Result = results[i]
			a.rows[i].Probed = true
		}
		a.status = StatusDone
		a.done = a.total
		a.elapsed = time.Since(a.started)
		reachable, udpOnly := 0, 0
		for _, r := range a.rows {
			if r.Result.Reachable {
				reachable++
			} else if r.Result.UDPOnly {
				udpOnly++
			}
		}
		unreachable := len(a.rows) - reachable - udpOnly
		a.message = fmt.Sprintf("检测完成：%d 个节点，%d 个 TCP 可达", len(a.rows), reachable)
		if udpOnly > 0 {
			a.message += fmt.Sprintf("，%d 个 QUIC 节点未做 TCP 握手", udpOnly)
		}
		if unreachable > 0 {
			a.message += fmt.Sprintf("，%d 个不可达", unreachable)
		}
	})
}

// update applies fn only when gen is still the newest run, then wakes the UI.
func (a *App) update(gen int, fn func()) {
	a.mu.Lock()
	if gen != a.gen {
		a.mu.Unlock()
		return
	}
	fn()
	a.mu.Unlock()
	a.wake()
}

func (a *App) failLocked(message string) {
	a.status = StatusError
	a.message = message
	a.elapsed = time.Since(a.started)
}

func (a *App) setError(message string) {
	a.mu.Lock()
	a.status = StatusError
	a.message = message
	a.mu.Unlock()
	a.wake()
}

// SelectedRow returns the row the detail panel shows, if any.
func (a *App) SelectedRow() (Row, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.selected < 0 || a.selected >= len(a.rows) {
		return Row{}, false
	}
	row := a.rows[a.selected]
	if rep, ok := a.remote[a.selected]; ok {
		r := rep
		row.Remote = &r
	}
	row.RemoteBusy = a.remoteBusy == a.selected
	row.RemoteErr = a.remoteErrs[a.selected]
	return row, true
}

// CheckGlobal asks the remote provider to ping one node from many countries.
// Only one such measurement runs at a time; a new one cancels the previous.
func (a *App) CheckGlobal(index int) {
	a.mu.Lock()
	if a.Remote == nil || index < 0 || index >= len(a.rows) {
		a.mu.Unlock()
		return
	}
	a.remoteGen++
	gen := a.remoteGen
	if a.remoteCancel != nil {
		a.remoteCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.remoteCancel = cancel
	a.remoteBusy = index
	delete(a.remoteErrs, index)
	delete(a.remote, index)
	target := a.rows[index].Node.Server
	a.mu.Unlock()
	a.wake()

	go func() {
		defer cancel()
		report, err := a.Remote.Ping(ctx, target, 8)
		a.mu.Lock()
		if gen != a.remoteGen {
			a.mu.Unlock()
			return
		}
		if a.remoteBusy == index {
			a.remoteBusy = -1
		}
		if err != nil {
			a.remoteErrs[index] = err.Error()
		} else {
			a.remote[index] = report
		}
		a.mu.Unlock()
		a.wake()
	}()
}

// clearRemoteLocked drops every worldwide result and cancels a running check.
func (a *App) clearRemoteLocked() {
	a.remoteGen++
	if a.remoteCancel != nil {
		a.remoteCancel()
		a.remoteCancel = nil
	}
	a.remote = map[int]remote.Report{}
	a.remoteErrs = map[int]string{}
	a.remoteBusy = -1
}
