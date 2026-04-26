package healthd

import (
	"testing"
	"time"

	"github.com/andrewneudegg/lab/pkg/config"
)

func TestEvaluateSLOsTracksBurnRate(t *testing.T) {
	now := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	samples := []Sample{
		{Time: now.Add(-4 * time.Minute), Good: true},
		{Time: now.Add(-3 * time.Minute), Good: false},
		{Time: now.Add(-2 * time.Minute), Good: true},
		{Time: now.Add(-1 * time.Minute), Good: true},
	}
	reports := evaluateSLOs([]config.HealthSLOConfig{{
		Name:            "availability",
		TargetPercent:   99,
		WindowSeconds:   300,
		WarningBurnRate: 2,
		PageBurnRate:    10,
	}}, samples, now)

	if len(reports) != 1 {
		t.Fatalf("expected one report, got %d", len(reports))
	}
	report := reports[0]
	if report.GoodEvents != 3 || report.TotalEvents != 4 {
		t.Fatalf("unexpected event counts: good=%d total=%d", report.GoodEvents, report.TotalEvents)
	}
	if report.Status != StatusCritical {
		t.Fatalf("expected critical burn status, got %q", report.Status)
	}
	if report.BurnRate != 25 {
		t.Fatalf("expected 25x burn rate, got %.2f", report.BurnRate)
	}
}

func TestOverallStatusPrioritizesCritical(t *testing.T) {
	status := overallStatus(
		[]CheckResult{{Status: StatusHealthy}},
		[]SLOReport{{Status: StatusWarning}, {Status: StatusCritical}},
	)
	if status != StatusCritical {
		t.Fatalf("expected critical, got %q", status)
	}
}

func TestProcessHeartbeatBecomesCriticalWhenStale(t *testing.T) {
	now := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	monitor := New(config.HealthdConfig{ProcessTimeoutSeconds: 10})
	_, err := monitor.RecordHeartbeat(now, ProcessHeartbeat{
		Name:       "homelabd",
		Type:       "daemon",
		Time:       now,
		TTLSeconds: 10,
	})
	if err != nil {
		t.Fatalf("record heartbeat: %v", err)
	}

	checks := monitor.processChecks(now.Add(11 * time.Second))
	if len(checks) != 1 {
		t.Fatalf("expected one process check, got %d", len(checks))
	}
	if checks[0].Status != StatusCritical {
		t.Fatalf("expected stale process to be critical, got %q", checks[0].Status)
	}
}
