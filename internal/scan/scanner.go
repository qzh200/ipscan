// Package scan runs probes across a node list with bounded concurrency and
// enriches the results with IP registry data once the addresses are known.
package scan

import (
	"context"
	"sync"

	"ipscan/internal/model"
	"ipscan/internal/probe"
)

// Progress reports that one node finished probing. Index is its position in
// the input slice, so the caller can update a row in place.
type Progress struct {
	Done, Total int
	Index       int
	Result      probe.Result
}

// Scanner probes nodes in parallel. A nil Geo disables the IP registry lookup.
type Scanner struct {
	Prober  *probe.Prober
	Geo     probe.GeoProvider
	Workers int
}

// New returns a scanner with 8 workers.
func New() *Scanner {
	return &Scanner{Prober: probe.New(), Workers: 8}
}

// Scan probes every node and returns results in input order. progress may be
// called from several goroutines; it is serialized here.
func (s *Scanner) Scan(ctx context.Context, nodes []model.Node, progress func(Progress)) []probe.Result {
	results := make([]probe.Result, len(nodes))
	if len(nodes) == 0 {
		return results
	}

	workers := s.Workers
	if workers <= 0 {
		workers = 8
	}
	if workers > len(nodes) {
		workers = len(nodes)
	}
	prober := s.Prober
	if prober == nil {
		prober = probe.New()
	}

	var (
		next     int
		mu       sync.Mutex
		done     int
		callback sync.Mutex
		wg       sync.WaitGroup
	)
	// A single counter allocates indices, which is cheaper and fairer than a
	// channel of jobs when the list is short.
	take := func() (int, bool) {
		mu.Lock()
		defer mu.Unlock()
		if next >= len(nodes) || ctx.Err() != nil {
			return 0, false
		}
		i := next
		next++
		return i, true
	}

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i, ok := take()
				if !ok {
					return
				}
				res := prober.Probe(ctx, nodes[i])
				results[i] = res
				if progress != nil {
					callback.Lock()
					done++
					progress(Progress{Done: done, Total: len(nodes), Index: i, Result: res})
					callback.Unlock()
				}
			}
		}()
	}
	wg.Wait()

	if s.Geo != nil && ctx.Err() == nil {
		s.enrich(ctx, results, nodes)
	}
	return results
}

func (s *Scanner) enrich(ctx context.Context, results []probe.Result, nodes []model.Node) {
	var ips []string
	for i := range results {
		for _, ip := range results[i].IPs {
			ips = append(ips, ip)
		}
	}
	if len(ips) == 0 {
		return
	}
	lookup, err := s.Geo.Lookup(ctx, ips)
	if err != nil || len(lookup) == 0 {
		return
	}
	for i := range results {
		for _, ip := range results[i].IPs {
			if geo, ok := lookup[ip]; ok {
				g := geo
				results[i].Geo = &g
				break
			}
		}
	}
}
