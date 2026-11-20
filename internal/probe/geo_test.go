package probe

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A real ip-api batch response only echoes "query" when the fields parameter
// asks for it; the provider must still key results by the requested address.
func TestIPAPIKeysWithoutQueryEcho(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ips []string
		_ = json.NewDecoder(r.Body).Decode(&ips)
		entries := make([]map[string]any, len(ips))
		for i := range ips {
			entries[i] = map[string]any{"status": "success", "country": "日本", "city": "东京"}
		}
		_ = json.NewEncoder(w).Encode(entries)
	}))
	defer srv.Close()

	api := &IPAPI{Endpoint: srv.URL, Client: srv.Client()}
	got, err := api.Lookup(context.Background(), []string{"45.192.249.150", "210.121.44.193"})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("map = %+v", got)
	}
	if geo, ok := got["210.121.44.193"]; !ok || geo.City != "东京" {
		t.Fatalf("keyed result = %+v", got)
	}
}

func TestIPWhoisLookup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := strings.TrimPrefix(r.URL.Path, "/")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true, "ip": ip, "country": "韩国", "city": "首尔",
			"connection": map[string]any{"asn": 9848, "org": "SEJONG NETWORKS", "isp": "Sejong"},
			"security":   map[string]any{"proxy": true, "hosting": true},
		})
	}))
	defer srv.Close()

	whois := &IPWhois{Endpoint: srv.URL, Client: srv.Client(), Concurrency: 2}
	got, err := whois.Lookup(context.Background(), []string{"210.121.44.193", "127.0.0.1"})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	geo, ok := got["210.121.44.193"]
	if !ok {
		t.Fatalf("missing result: %+v", got)
	}
	if geo.ASN != "AS9848 SEJONG NETWORKS" || !geo.Proxy || !geo.Hosting {
		t.Fatalf("geo = %+v", geo)
	}
}

type stubGeo struct {
	got []string
	res map[string]Geo
	err error
}

func (s *stubGeo) Lookup(_ context.Context, ips []string) (map[string]Geo, error) {
	s.got = append([]string(nil), ips...)
	return s.res, s.err
}

func TestGeoChainFallsBackForMissingIPs(t *testing.T) {
	first := &stubGeo{res: map[string]Geo{"1.2.3.4": {Country: "A"}}}
	second := &stubGeo{res: map[string]Geo{"5.6.7.8": {Country: "B"}}}
	chain := &GeoChain{Providers: []GeoProvider{first, second}}

	got, err := chain.Lookup(context.Background(), []string{"1.2.3.4", "5.6.7.8"})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got["1.2.3.4"].Country != "A" || got["5.6.7.8"].Country != "B" {
		t.Fatalf("merged = %+v", got)
	}
	if len(second.got) != 1 || second.got[0] != "5.6.7.8" {
		t.Fatalf("second provider asked for %v, want only the missing IP", second.got)
	}
}

func TestGeoChainReportsFailure(t *testing.T) {
	chain := &GeoChain{Providers: []GeoProvider{
		&stubGeo{err: errors.New("blocked")},
		&stubGeo{err: errors.New("rate limited")},
	}}
	if _, err := chain.Lookup(context.Background(), []string{"8.8.8.8"}); err == nil {
		t.Fatal("expected an error when no provider resolved anything")
	}
}
