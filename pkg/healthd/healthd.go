package healthd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/andrewneudegg/lab/pkg/config"
	"github.com/andrewneudegg/lab/pkg/id"
)

const (
	StatusHealthy  = "healthy"
	StatusWarning  = "warning"
	StatusCritical = "critical"

	SeverityInfo = "info"
	SeverityWarn = "warn"
	SeverityPage = "page"
)

type Monitor struct {
	cfg       config.HealthdConfig
	startedAt time.Time
	client    *http.Client

	mu            sync.Mutex
	samples       []Sample
	checks        []CheckResult
	notifications []Notification
	previousCPU   cpuCounters
	lastStatus    string
	lastSLOStatus map[string]string
}

type Sample struct {
	Time                 time.Time `json:"time"`
	Good                 bool      `json:"good"`
	CPUUsagePercent      float64   `json:"cpu_usage_percent"`
	MemoryUsagePercent   float64   `json:"memory_usage_percent"`
	MemoryUsedBytes      uint64    `json:"memory_used_bytes"`
	MemoryTotalBytes     uint64    `json:"memory_total_bytes"`
	Load1                float64   `json:"load1"`
	Load5                float64   `json:"load5"`
	Load15               float64   `json:"load15"`
	SystemUptimeSeconds  float64   `json:"system_uptime_seconds"`
	ProcessUptimeSeconds float64   `json:"process_uptime_seconds"`
	Goroutines           int       `json:"goroutines"`
}

type CheckResult struct {
	Name                string    `json:"name"`
	Type                string    `json:"type"`
	Status              string    `json:"status"`
	Message             string    `json:"message"`
	LatencyMilliseconds int64     `json:"latency_ms"`
	LastChecked         time.Time `json:"last_checked"`
}

type SLOReport struct {
	Name                        string   `json:"name"`
	TargetPercent               float64  `json:"target_percent"`
	WindowSeconds               int      `json:"window_seconds"`
	GoodEvents                  int      `json:"good_events"`
	TotalEvents                 int      `json:"total_events"`
	SLIPercent                  float64  `json:"sli_percent"`
	ErrorBudgetRemainingPercent float64  `json:"error_budget_remaining_percent"`
	BurnRate                    float64  `json:"burn_rate"`
	Status                      string   `json:"status"`
	Violations                  []string `json:"violations,omitempty"`
}

type Notification struct {
	ID        string    `json:"id"`
	Time      time.Time `json:"time"`
	Severity  string    `json:"severity"`
	Status    string    `json:"status"`
	Source    string    `json:"source"`
	Message   string    `json:"message"`
	Delivered []string  `json:"delivered,omitempty"`
}

type Snapshot struct {
	Status        string         `json:"status"`
	StartedAt     time.Time      `json:"started_at"`
	UptimeSeconds float64        `json:"uptime_seconds"`
	WindowSeconds int            `json:"window_seconds"`
	Current       Sample         `json:"current"`
	Samples       []Sample       `json:"samples"`
	Checks        []CheckResult  `json:"checks"`
	SLOs          []SLOReport    `json:"slos"`
	Notifications []Notification `json:"notifications"`
}

func New(cfg config.HealthdConfig) *Monitor {
	if cfg.SampleIntervalSeconds <= 0 {
		cfg.SampleIntervalSeconds = 5
	}
	if cfg.RetentionSeconds <= 0 {
		cfg.RetentionSeconds = 300
	}
	if cfg.RequestTimeoutSeconds <= 0 {
		cfg.RequestTimeoutSeconds = 2
	}
	if cfg.SLOs == nil {
		cfg.SLOs = []config.HealthSLOConfig{{
			Name:            "availability",
			TargetPercent:   99.9,
			WindowSeconds:   cfg.RetentionSeconds,
			WarningBurnRate: 2,
			PageBurnRate:    10,
		}}
	}
	return &Monitor{
		cfg:           cfg,
		startedAt:     time.Now().UTC(),
		client:        &http.Client{Timeout: time.Duration(cfg.RequestTimeoutSeconds) * time.Second},
		lastStatus:    StatusHealthy,
		lastSLOStatus: make(map[string]string),
	}
}

func (m *Monitor) Start(ctx context.Context) {
	m.collect(ctx)
	ticker := time.NewTicker(time.Duration(m.cfg.SampleIntervalSeconds) * time.Second)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.collect(ctx)
			}
		}
	}()
}

func (m *Monitor) RunChecks(ctx context.Context) Snapshot {
	m.collect(ctx)
	return m.Snapshot(5 * time.Minute)
}

func (m *Monitor) Snapshot(window time.Duration) Snapshot {
	if window <= 0 {
		window = 5 * time.Minute
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	cutoff := time.Now().UTC().Add(-window)
	samples := samplesSince(m.samples, cutoff)
	current := Sample{Time: time.Now().UTC(), ProcessUptimeSeconds: time.Since(m.startedAt).Seconds(), Goroutines: runtime.NumGoroutine()}
	if len(m.samples) > 0 {
		current = m.samples[len(m.samples)-1]
	}
	checks := append([]CheckResult(nil), m.checks...)
	slos := evaluateSLOs(m.cfg.SLOs, m.samples, time.Now().UTC())
	notifications := append([]Notification(nil), m.notifications...)
	if samples == nil {
		samples = []Sample{}
	}
	if checks == nil {
		checks = []CheckResult{}
	}
	if slos == nil {
		slos = []SLOReport{}
	}
	if notifications == nil {
		notifications = []Notification{}
	}
	status := overallStatus(checks, slos)
	return Snapshot{
		Status:        status,
		StartedAt:     m.startedAt,
		UptimeSeconds: time.Since(m.startedAt).Seconds(),
		WindowSeconds: int(window.Seconds()),
		Current:       current,
		Samples:       samples,
		Checks:        checks,
		SLOs:          slos,
		Notifications: notifications,
	}
}

func (m *Monitor) collect(ctx context.Context) {
	now := time.Now().UTC()
	checks := m.runChecks(ctx, now)
	system, err := readSystemSample(m.startedAt, now, m.previousCPUSafe())
	if err != nil {
		checks = append(checks, CheckResult{
			Name:        "system",
			Type:        "local",
			Status:      StatusCritical,
			Message:     err.Error(),
			LastChecked: now,
		})
	}
	nextCPU, cpuErr := readCPUCounters()
	if cpuErr == nil {
		m.setPreviousCPU(nextCPU)
	}
	system.Time = now
	system.Good = checksHealthy(checks)

	m.mu.Lock()
	m.samples = append(m.samples, system)
	m.samples = trimSamples(m.samples, now.Add(-time.Duration(m.cfg.RetentionSeconds)*time.Second))
	m.checks = checks
	slos := evaluateSLOs(m.cfg.SLOs, m.samples, now)
	status := overallStatus(checks, slos)
	events := m.transitionEventsLocked(now, status, slos)
	m.mu.Unlock()

	for _, event := range events {
		m.emit(event)
	}
}

func (m *Monitor) runChecks(ctx context.Context, now time.Time) []CheckResult {
	if len(m.cfg.Checks) == 0 {
		return []CheckResult{{
			Name:        "healthd",
			Type:        "internal",
			Status:      StatusHealthy,
			Message:     "monitor loop is running",
			LastChecked: now,
		}}
	}
	results := make([]CheckResult, 0, len(m.cfg.Checks))
	for _, check := range m.cfg.Checks {
		results = append(results, m.runCheck(ctx, check, now))
	}
	return results
}

func (m *Monitor) runCheck(ctx context.Context, check config.HealthCheckConfig, now time.Time) CheckResult {
	if check.Name == "" {
		check.Name = check.URL
	}
	if check.Type == "" {
		check.Type = "http"
	}
	start := time.Now()
	result := CheckResult{
		Name:        check.Name,
		Type:        check.Type,
		Status:      StatusCritical,
		LastChecked: now,
	}
	switch check.Type {
	case "http":
		result = m.runHTTPCheck(ctx, check, now, start)
	default:
		result.Message = fmt.Sprintf("unsupported check type %q", check.Type)
	}
	return result
}

func (m *Monitor) runHTTPCheck(ctx context.Context, check config.HealthCheckConfig, now time.Time, start time.Time) CheckResult {
	method := check.Method
	if method == "" {
		method = http.MethodGet
	}
	result := CheckResult{Name: check.Name, Type: "http", Status: StatusCritical, LastChecked: now}
	if check.URL == "" {
		result.Message = "missing url"
		return result
	}
	timeout := time.Duration(m.cfg.RequestTimeoutSeconds) * time.Second
	if check.TimeoutSeconds > 0 {
		timeout = time.Duration(check.TimeoutSeconds) * time.Second
	}
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(checkCtx, method, check.URL, nil)
	if err != nil {
		result.Message = err.Error()
		return result
	}
	resp, err := m.client.Do(req)
	result.LatencyMilliseconds = time.Since(start).Milliseconds()
	if err != nil {
		result.Message = err.Error()
		return result
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	expect := check.ExpectStatus
	if expect == 0 {
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			result.Status = StatusHealthy
			result.Message = resp.Status
			return result
		}
		result.Message = resp.Status
		return result
	}
	if resp.StatusCode == expect {
		result.Status = StatusHealthy
	}
	result.Message = resp.Status
	return result
}

func (m *Monitor) transitionEventsLocked(now time.Time, status string, slos []SLOReport) []Notification {
	var events []Notification
	if status != m.lastStatus {
		severity := SeverityInfo
		state := "resolved"
		if status == StatusWarning {
			severity = SeverityWarn
			state = "firing"
		}
		if status == StatusCritical {
			severity = SeverityPage
			state = "firing"
		}
		events = append(events, Notification{
			ID:       id.New("health"),
			Time:     now,
			Severity: severity,
			Status:   state,
			Source:   "healthd",
			Message:  fmt.Sprintf("healthd status changed from %s to %s", m.lastStatus, status),
		})
		m.lastStatus = status
	}
	for _, slo := range slos {
		previous := m.lastSLOStatus[slo.Name]
		if previous == "" {
			previous = StatusHealthy
		}
		if slo.Status == previous {
			continue
		}
		severity := SeverityInfo
		state := "resolved"
		if slo.Status == StatusWarning {
			severity = SeverityWarn
			state = "firing"
		}
		if slo.Status == StatusCritical {
			severity = SeverityPage
			state = "firing"
		}
		events = append(events, Notification{
			ID:       id.New("slo"),
			Time:     now,
			Severity: severity,
			Status:   state,
			Source:   "slo:" + slo.Name,
			Message:  fmt.Sprintf("%s SLO changed from %s to %s; SLI %.3f%%, burn rate %.2fx", slo.Name, previous, slo.Status, slo.SLIPercent, slo.BurnRate),
		})
		m.lastSLOStatus[slo.Name] = slo.Status
	}
	for _, event := range events {
		m.notifications = append([]Notification{event}, m.notifications...)
	}
	if len(m.notifications) > 200 {
		m.notifications = m.notifications[:200]
	}
	return events
}

func (m *Monitor) emit(event Notification) {
	if len(m.cfg.Notifications) == 0 {
		return
	}
	delivered := make([]string, 0, len(m.cfg.Notifications))
	for _, target := range m.cfg.Notifications {
		if !severityAllowed(event.Severity, target.MinSeverity) {
			continue
		}
		if target.Type != "webhook" || target.URL == "" {
			continue
		}
		if err := postNotification(m.client, target.URL, event); err == nil {
			delivered = append(delivered, target.Name)
		}
	}
	if len(delivered) == 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.notifications {
		if m.notifications[i].ID == event.ID {
			m.notifications[i].Delivered = delivered
			return
		}
	}
}

func postNotification(client *http.Client, url string, event Notification) error {
	b, err := json.Marshal(event)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("notification webhook returned %s", resp.Status)
	}
	return nil
}

func evaluateSLOs(configs []config.HealthSLOConfig, samples []Sample, now time.Time) []SLOReport {
	reports := make([]SLOReport, 0, len(configs))
	for _, cfg := range configs {
		if cfg.Name == "" {
			cfg.Name = "availability"
		}
		if cfg.TargetPercent == 0 {
			cfg.TargetPercent = 99.9
		}
		if cfg.WindowSeconds == 0 {
			cfg.WindowSeconds = 300
		}
		if cfg.WarningBurnRate == 0 {
			cfg.WarningBurnRate = 2
		}
		if cfg.PageBurnRate == 0 {
			cfg.PageBurnRate = 10
		}
		cutoff := now.Add(-time.Duration(cfg.WindowSeconds) * time.Second)
		var total, good int
		for _, sample := range samples {
			if sample.Time.Before(cutoff) {
				continue
			}
			total++
			if sample.Good {
				good++
			}
		}
		report := SLOReport{
			Name:          cfg.Name,
			TargetPercent: cfg.TargetPercent,
			WindowSeconds: cfg.WindowSeconds,
			GoodEvents:    good,
			TotalEvents:   total,
			Status:        StatusHealthy,
		}
		if total == 0 {
			report.SLIPercent = 100
			report.ErrorBudgetRemainingPercent = 100
			reports = append(reports, report)
			continue
		}
		report.SLIPercent = (float64(good) / float64(total)) * 100
		errorBudget := 100 - cfg.TargetPercent
		errorPercent := 100 - report.SLIPercent
		if errorBudget <= 0 {
			report.ErrorBudgetRemainingPercent = 0
			report.BurnRate = 0
		} else {
			report.ErrorBudgetRemainingPercent = maxFloat(0, ((errorBudget-errorPercent)/errorBudget)*100)
			report.BurnRate = errorPercent / errorBudget
		}
		if report.BurnRate >= cfg.PageBurnRate {
			report.Status = StatusCritical
			report.Violations = append(report.Violations, fmt.Sprintf("burn rate %.2fx >= page %.2fx", report.BurnRate, cfg.PageBurnRate))
		} else if report.BurnRate >= cfg.WarningBurnRate {
			report.Status = StatusWarning
			report.Violations = append(report.Violations, fmt.Sprintf("burn rate %.2fx >= warning %.2fx", report.BurnRate, cfg.WarningBurnRate))
		}
		if report.SLIPercent < cfg.TargetPercent {
			report.Violations = append(report.Violations, fmt.Sprintf("SLI %.3f%% below target %.3f%%", report.SLIPercent, cfg.TargetPercent))
		}
		reports = append(reports, report)
	}
	return reports
}

func readSystemSample(startedAt, now time.Time, previous cpuCounters) (Sample, error) {
	current, err := readCPUCounters()
	if err != nil {
		return Sample{}, err
	}
	mem, err := readMemory()
	if err != nil {
		return Sample{}, err
	}
	load1, load5, load15, _ := readLoad()
	uptime, _ := readUptime()
	cpuUsage := 0.0
	if previous.total > 0 {
		cpuUsage = current.usageSince(previous)
	}
	memUsed := mem.total - mem.available
	memUsage := 0.0
	if mem.total > 0 {
		memUsage = (float64(memUsed) / float64(mem.total)) * 100
	}
	return Sample{
		Time:                 now,
		CPUUsagePercent:      cpuUsage,
		MemoryUsagePercent:   memUsage,
		MemoryUsedBytes:      memUsed,
		MemoryTotalBytes:     mem.total,
		Load1:                load1,
		Load5:                load5,
		Load15:               load15,
		SystemUptimeSeconds:  uptime,
		ProcessUptimeSeconds: time.Since(startedAt).Seconds(),
		Goroutines:           runtime.NumGoroutine(),
	}, nil
}

type cpuCounters struct {
	idle  uint64
	total uint64
}

func (c cpuCounters) usageSince(previous cpuCounters) float64 {
	totalDelta := c.total - previous.total
	idleDelta := c.idle - previous.idle
	if totalDelta == 0 {
		return 0
	}
	usage := (float64(totalDelta-idleDelta) / float64(totalDelta)) * 100
	if usage < 0 {
		return 0
	}
	if usage > 100 {
		return 100
	}
	return usage
}

func readCPUCounters() (cpuCounters, error) {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return cpuCounters{}, err
	}
	lines := strings.Split(string(b), "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "cpu ") {
		return cpuCounters{}, errors.New("missing cpu line in /proc/stat")
	}
	fields := strings.Fields(lines[0])
	if len(fields) < 5 {
		return cpuCounters{}, errors.New("malformed cpu line in /proc/stat")
	}
	var total uint64
	values := make([]uint64, 0, len(fields)-1)
	for _, field := range fields[1:] {
		value, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return cpuCounters{}, err
		}
		values = append(values, value)
		total += value
	}
	idle := values[3]
	if len(values) > 4 {
		idle += values[4]
	}
	return cpuCounters{idle: idle, total: total}, nil
}

type memoryCounters struct {
	total     uint64
	available uint64
}

func readMemory() (memoryCounters, error) {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return memoryCounters{}, err
	}
	var mem memoryCounters
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch strings.TrimSuffix(fields[0], ":") {
		case "MemTotal":
			mem.total = value * 1024
		case "MemAvailable":
			mem.available = value * 1024
		}
	}
	if mem.total == 0 {
		return memoryCounters{}, errors.New("MemTotal missing from /proc/meminfo")
	}
	if mem.available == 0 {
		return memoryCounters{}, errors.New("MemAvailable missing from /proc/meminfo")
	}
	return mem, nil
}

func readLoad() (float64, float64, float64, error) {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, 0, 0, err
	}
	fields := strings.Fields(string(b))
	if len(fields) < 3 {
		return 0, 0, 0, errors.New("malformed /proc/loadavg")
	}
	load1, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, 0, 0, err
	}
	load5, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return 0, 0, 0, err
	}
	load15, err := strconv.ParseFloat(fields[2], 64)
	if err != nil {
		return 0, 0, 0, err
	}
	return load1, load5, load15, nil
}

func readUptime() (float64, error) {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return 0, errors.New("malformed /proc/uptime")
	}
	return strconv.ParseFloat(fields[0], 64)
}

func (m *Monitor) previousCPUSafe() cpuCounters {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.previousCPU
}

func (m *Monitor) setPreviousCPU(counters cpuCounters) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.previousCPU = counters
}

func samplesSince(samples []Sample, cutoff time.Time) []Sample {
	out := make([]Sample, 0, len(samples))
	for _, sample := range samples {
		if !sample.Time.Before(cutoff) {
			out = append(out, sample)
		}
	}
	return out
}

func trimSamples(samples []Sample, cutoff time.Time) []Sample {
	idx := 0
	for idx < len(samples) && samples[idx].Time.Before(cutoff) {
		idx++
	}
	return append([]Sample(nil), samples[idx:]...)
}

func checksHealthy(checks []CheckResult) bool {
	for _, check := range checks {
		if check.Status != StatusHealthy {
			return false
		}
	}
	return true
}

func overallStatus(checks []CheckResult, slos []SLOReport) string {
	status := StatusHealthy
	for _, check := range checks {
		if check.Status == StatusCritical {
			return StatusCritical
		}
		if check.Status == StatusWarning {
			status = StatusWarning
		}
	}
	for _, slo := range slos {
		if slo.Status == StatusCritical {
			return StatusCritical
		}
		if slo.Status == StatusWarning {
			status = StatusWarning
		}
	}
	return status
}

func severityAllowed(value, minimum string) bool {
	return severityRank(value) >= severityRank(minimum)
}

func severityRank(value string) int {
	switch value {
	case SeverityPage:
		return 3
	case SeverityWarn:
		return 2
	default:
		return 1
	}
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
