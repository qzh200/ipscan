package remote

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGlobalpingPing(t *testing.T) {
	polls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/measurements":
			var req gpCreate
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode request: %v", err)
			}
			if req.Target != "example.com" || req.Limit != 8 || req.Type != "ping" {
				t.Errorf("request = %+v", req)
			}
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "m1", "probesCount": 2})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/measurements/m1":
			polls++
			status := "in-progress"
			var results []any
			if polls > 1 {
				status = "finished"
				results = []any{
					map[string]any{
						"probe": map[string]any{"country": "FI", "city": "Helsinki", "network": "Hetzner", "asn": 24940},
						"result": map[string]any{
							"status": "finished",
							"stats":  map[string]any{"min": 9.0, "max": 11.0, "avg": 10.0, "total": 4, "rcv": 4, "loss": 0},
						},
					},
					map[string]any{
						"probe": map[string]any{"country": "DE", "city": "Falkenstein", "network": "Hetzner", "asn": 24940},
						"result": map[string]any{
							"status": "finished",
							"stats":  map[string]any{"min": 20.0, "max": 40.0, "avg": 30.0, "total": 4, "rcv": 2, "loss": 0.5},
						},
					},
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "m1", "type": "ping", "status": status, "results": results})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	g := &Globalping{BaseURL: srv.URL, Client: srv.Client(), PollInterval: time.Millisecond, Timeout: 2 * time.Second, Packets: 4}
	rep, err := g.Ping(context.Background(), "example.com", 8)
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if len(rep.Probes) != 2 || rep.Reachable != 2 {
		t.Fatalf("report = %+v", rep)
	}
	if rep.Median != 20*time.Millisecond || rep.Min != 10*time.Millisecond || rep.Max != 30*time.Millisecond {
		t.Fatalf("latencies: median=%v min=%v max=%v", rep.Median, rep.Min, rep.Max)
	}
	if rep.Loss != 0.25 {
		t.Fatalf("loss = %v, want mean of 0 and 0.5", rep.Loss)
	}
	if rep.Probes[0].LocationLabel() != "Helsinki · FI · Hetzner (AS24940)" {
		t.Fatalf("location = %q", rep.Probes[0].LocationLabel())
	}
	if rep.Probes[0].LatencyLabel() != "10 ms" {
		t.Fatalf("latency label = %q", rep.Probes[0].LatencyLabel())
	}
	if polls < 2 {
		t.Fatalf("polls = %d", polls)
	}
}

func TestGlobalpingRateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer srv.Close()

	g := &Globalping{BaseURL: srv.URL, Client: srv.Client(), PollInterval: time.Millisecond, Timeout: time.Second}
	_, err := g.Ping(context.Background(), "example.com", 4)
	if err == nil || !strings.Contains(err.Error(), "限流") {
		t.Fatalf("err = %v", err)
	}
}

func TestGlobalpingValidationError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":{"message":"Parameter validation failed."}}`))
	}))
	defer srv.Close()

	g := &Globalping{BaseURL: srv.URL, Client: srv.Client(), PollInterval: time.Millisecond, Timeout: time.Second}
	_, err := g.Ping(context.Background(), "bad host!!", 4)
	if err == nil || !strings.Contains(err.Error(), "Parameter validation failed") {
		t.Fatalf("err = %v", err)
	}
}
