package screen

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"ipscan/internal/app"
	"ipscan/internal/model"
	"ipscan/internal/probe"
)

func testRow(index int, name string, score int, avg time.Duration, loss float64, reachable bool) filteredRow {
	return filteredRow{index: index, row: app.Row{
		Node:   model.Node{Name: name, Server: "s.example.com", Port: 443},
		Probed: true,
		Result: probe.Result{
			Reachable: reachable, Score: score, Avg: avg, Jitter: avg / 4, Loss: loss,
			IP: "1.1.1." + string(rune('1'+index)),
		},
	}}
}

func names(rows []filteredRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.row.Node.Name
	}
	return out
}

func assertOrder(t *testing.T, rows []filteredRow, want ...string) {
	t.Helper()
	got := names(rows)
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestSortFiltered(t *testing.T) {
	base := func() []filteredRow {
		return []filteredRow{
			testRow(0, "slow", 40, 300*time.Millisecond, .2, true),
			testRow(1, "fast", 90, 20*time.Millisecond, 0, true),
			testRow(2, "mid", 70, 80*time.Millisecond, .1, true),
			testRow(3, "down", 0, 0, 0, false),
		}
	}

	rows := base()
	sortFiltered(rows, ui.SortOrder{Column: "score", Descending: true})
	assertOrder(t, rows, "fast", "mid", "slow", "down")

	rows = base()
	sortFiltered(rows, ui.SortOrder{Column: "score"})
	assertOrder(t, rows, "slow", "mid", "fast", "down")

	rows = base()
	sortFiltered(rows, ui.SortOrder{Column: "delay"})
	assertOrder(t, rows, "fast", "mid", "slow", "down")

	rows = base()
	sortFiltered(rows, ui.SortOrder{Column: "delay", Descending: true})
	assertOrder(t, rows, "slow", "mid", "fast", "down")

	rows = base()
	sortFiltered(rows, ui.SortOrder{Column: "loss"})
	assertOrder(t, rows, "fast", "mid", "slow", "down")

	rows = base()
	sortFiltered(rows, ui.SortOrder{Column: "name"})
	assertOrder(t, rows, "down", "fast", "mid", "slow")

	rows = base()
	sortFiltered(rows, ui.SortOrder{})
	assertOrder(t, rows, "slow", "fast", "mid", "down")
}

func TestSortMisplacesNothingOnTies(t *testing.T) {
	rows := []filteredRow{
		testRow(0, "a", 80, 10*time.Millisecond, 0, true),
		testRow(1, "b", 80, 10*time.Millisecond, 0, true),
		testRow(2, "c", 80, 10*time.Millisecond, 0, true),
	}
	sortFiltered(rows, ui.SortOrder{Column: "score", Descending: true})
	assertOrder(t, rows, "a", "b", "c")
}

func TestMatchesIPType(t *testing.T) {
	rows := []app.Row{
		{Result: probe.Result{Geo: nil}},
		{Result: probe.Result{Geo: &probe.Geo{Hosting: true}}},
		{Result: probe.Result{Geo: &probe.Geo{}}},
		{Result: probe.Result{Geo: &probe.Geo{Mobile: true}}},
	}
	if !matchesIPType(rows[0], 0) {
		t.Fatal("all should include unknown geo")
	}
	if matchesIPType(rows[0], 1) || matchesIPType(rows[0], 2) || matchesIPType(rows[0], 3) {
		t.Fatal("unknown geo must not match a role filter")
	}
	if matchesIPType(rows[1], 1) || !matchesIPType(rows[1], 2) {
		t.Fatal("hosting row should only match datacenter")
	}
	if !matchesIPType(rows[2], 1) || matchesIPType(rows[2], 2) {
		t.Fatal("plain geo should count as residential/enterprise")
	}
	if !matchesIPType(rows[3], 3) || matchesIPType(rows[3], 1) {
		t.Fatal("mobile row should only match mobile")
	}
}
