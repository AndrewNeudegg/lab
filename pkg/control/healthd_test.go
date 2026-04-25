package control

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/andrewneudegg/lab/pkg/config"
	"github.com/andrewneudegg/lab/pkg/healthd"
)

func TestHealthdEndpointReturnsSnapshot(t *testing.T) {
	monitor := healthd.New(config.HealthdConfig{
		SampleIntervalSeconds: 5,
		RetentionSeconds:      300,
		RequestTimeoutSeconds: 1,
	})
	server := Server{Healthd: monitor}
	mux := http.NewServeMux()
	server.register(mux)

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
