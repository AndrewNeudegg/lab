package healthd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/andrewneudegg/lab/pkg/config"
)

func TestEndpointReturnsSnapshot(t *testing.T) {
	monitor := New(config.HealthdConfig{
		SampleIntervalSeconds: 5,
		RetentionSeconds:      300,
		RequestTimeoutSeconds: 1,
	})
	server := Server{Monitor: monitor}
	mux := http.NewServeMux()
	server.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/healthd?window=5m", nil)
	rw := httptest.NewRecorder()
	mux.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rw.Code, rw.Body.String())
	}
	if got := rw.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("expected json content type, got %q", got)
	}
}

func TestParseWindowFallsBackOnInvalidDuration(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthd?window=nope", nil)
	if got := parseWindow(req, 5*time.Minute); got != 5*time.Minute {
		t.Fatalf("expected fallback window, got %s", got)
	}
}

func TestProcessHeartbeatEndpointRegistersProcess(t *testing.T) {
	monitor := New(config.HealthdConfig{ProcessTimeoutSeconds: 15})
	server := Server{Monitor: monitor}
	mux := http.NewServeMux()
	server.Register(mux)

	body := `{"name":"homelabd","type":"daemon","pid":123,"ttl_seconds":15}`
	req := httptest.NewRequest(http.MethodPost, "/healthd/processes/heartbeat", strings.NewReader(body))
	rw := httptest.NewRecorder()
	mux.ServeHTTP(rw, req)

	if rw.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rw.Code, rw.Body.String())
	}
	var process ProcessStatus
	if err := json.NewDecoder(rw.Body).Decode(&process); err != nil {
		t.Fatalf("decode process: %v", err)
	}
	if process.Name != "homelabd" || process.Status != StatusHealthy {
		t.Fatalf("unexpected process: %+v", process)
	}

	req = httptest.NewRequest(http.MethodGet, "/healthd/processes", nil)
	rw = httptest.NewRecorder()
	mux.ServeHTTP(rw, req)
	if rw.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rw.Code, rw.Body.String())
	}
}

func TestErrorsEndpointRecordsAndFiltersErrors(t *testing.T) {
	monitor := New(config.HealthdConfig{})
	server := Server{Monitor: monitor}
	mux := http.NewServeMux()
	server.Register(mux)

	body := `{"errors":[{"source":"supervisord","app":"dashboard","severity":"warn","message":"vite failed","log_path":"data/supervisord/logs/dashboard.stderr.log"},{"source":"supervisord","app":"homelabd","message":"api failed"}]}`
	req := httptest.NewRequest(http.MethodPost, "/healthd/errors", strings.NewReader(body))
	rw := httptest.NewRecorder()
	mux.ServeHTTP(rw, req)
	if rw.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rw.Code, rw.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/healthd/errors?limit=1&app=dashboard", nil)
	rw = httptest.NewRecorder()
	mux.ServeHTTP(rw, req)
	if rw.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rw.Code, rw.Body.String())
	}
	var payload struct {
		Errors []ApplicationError `json:"errors"`
	}
	if err := json.NewDecoder(rw.Body).Decode(&payload); err != nil {
		t.Fatalf("decode errors: %v", err)
	}
	if len(payload.Errors) != 1 {
		t.Fatalf("errors = %#v, want one dashboard error", payload.Errors)
	}
	if payload.Errors[0].App != "dashboard" || payload.Errors[0].Message != "vite failed" || payload.Errors[0].ID == "" {
		t.Fatalf("error = %#v, want normalised dashboard error", payload.Errors[0])
	}
}

func TestErrorsEndpointReturnsNewestErrorsFirst(t *testing.T) {
	monitor := New(config.HealthdConfig{})
	server := Server{Monitor: monitor}
	mux := http.NewServeMux()
	server.Register(mux)

	body := `{"errors":[{"time":"2026-04-28T06:00:00Z","source":"supervisord","app":"dashboard","message":"old stderr"},{"time":"2026-04-28T06:01:00Z","source":"supervisord","app":"dashboard","message":"new stderr"}]}`
	req := httptest.NewRequest(http.MethodPost, "/healthd/errors", strings.NewReader(body))
	rw := httptest.NewRecorder()
	mux.ServeHTTP(rw, req)
	if rw.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rw.Code, rw.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/healthd/errors?limit=2&app=dashboard", nil)
	rw = httptest.NewRecorder()
	mux.ServeHTTP(rw, req)
	if rw.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rw.Code, rw.Body.String())
	}
	var payload struct {
		Errors []ApplicationError `json:"errors"`
	}
	if err := json.NewDecoder(rw.Body).Decode(&payload); err != nil {
		t.Fatalf("decode errors: %v", err)
	}
	if len(payload.Errors) != 2 {
		t.Fatalf("errors = %#v, want two dashboard errors", payload.Errors)
	}
	if payload.Errors[0].Message != "new stderr" || payload.Errors[1].Message != "old stderr" {
		t.Fatalf("errors = %#v, want newest error first", payload.Errors)
	}
}
