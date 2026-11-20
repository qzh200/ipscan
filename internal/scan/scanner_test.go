package scan

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"ipscan/internal/model"
	"ipscan/internal/probe"
)

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

func TestScanOrdersResultsAndReportsProgress(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	live := ln.Addr().(*net.TCPAddr).Port
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	nodes := []model.Node{
		{Name: "a", Type: "trojan", Server: "127.0.0.1", Port: live},
		{Name: "b", Type: "trojan", Server: "127.0.0.1", Port: live},
		{Name: "c", Type: "trojan", Server: "127.0.0.1", Port: live},
	}
	prober := probe.New()
	prober.Options = probe.Options{Samples: 1, Timeout: 300 * time.Millisecond, DNS: time.Second}
	s := &Scanner{Prober: prober, Workers: 2}

	var seen []int
	results, err := s.Scan(context.Background(), nodes, func(p Progress) {
		if p.Total != len(nodes) || p.Index < 0 || p.Index >= len(nodes) {
			t.Errorf("bad progress %+v", p)
		}
		seen = append(seen, p.Index)
	})

	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(results) != len(nodes) {
		t.Fatalf("results = %d", len(results))
	}
	for i, r := range results {
		if !r.Reachable {
			t.Fatalf("node %d unreachable: %+v", i, r)
		}
	}
	if len(seen) != len(nodes) {
		t.Fatalf("progress calls = %d, want %d", len(seen), len(nodes))
	}
}

func TestScanCancel(t *testing.T) {
	nodes := []model.Node{
		{Name: "a", Type: "trojan", Server: "127.0.0.1", Port: freePort(t)},
	}
	prober := probe.New()
	prober.Options = probe.Options{Samples: 1, Timeout: 200 * time.Millisecond, DNS: time.Second}
	s := &Scanner{Prober: prober, Workers: 1}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	results, err := s.Scan(ctx, nodes, nil)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(results) != 1 || results[0].Reachable {
		t.Fatalf("canceled scan returned %+v", results)
	}
}

type failingGeo struct{}

func (failingGeo) Lookup(context.Context, []string) (map[string]probe.Geo, error) {
	return nil, errors.New("provider blocked")
}

func TestScanSurfacesGeoErrorButKeepsResults(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	nodes := []model.Node{{Name: "a", Type: "trojan", Server: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port}}
	prober := probe.New()
	prober.Options = probe.Options{Samples: 1, Timeout: time.Second, DNS: time.Second}
	s := &Scanner{Prober: prober, Geo: failingGeo{}, Workers: 1}

	results, geoErr := s.Scan(context.Background(), nodes, nil)
	if geoErr == nil {
		t.Fatal("expected the geo error to surface")
	}
	if len(results) != 1 || !results[0].Reachable {
		t.Fatalf("results = %+v", results)
	}
}
