package supervisor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/andrewneudegg/lab/pkg/config"
)

const (
	StateStopped  = "stopped"
	StateStarting = "starting"
	StateRunning  = "running"
	StateStopping = "stopping"
	StateFailed   = "failed"
)

type AppStatus struct {
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	State       string            `json:"state"`
	Desired     string            `json:"desired"`
	PID         int               `json:"pid,omitempty"`
	Restarts    int               `json:"restarts"`
	ExitCode    int               `json:"exit_code,omitempty"`
	Message     string            `json:"message"`
	StartedAt   *time.Time        `json:"started_at,omitempty"`
	StoppedAt   *time.Time        `json:"stopped_at,omitempty"`
	UpdatedAt   time.Time         `json:"updated_at"`
	StartOrder  int               `json:"start_order"`
	Restart     string            `json:"restart"`
	HealthURL   string            `json:"health_url,omitempty"`
	LastHealth  string            `json:"last_health,omitempty"`
	LastOutput  string            `json:"last_output,omitempty"`
	WorkingDir  string            `json:"working_dir,omitempty"`
	Command     string            `json:"command"`
	Args        []string          `json:"args,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
}

type Snapshot struct {
	Status       string      `json:"status"`
	StartedAt    time.Time   `json:"started_at"`
	Restartable  bool        `json:"restartable"`
	RestartHint  string      `json:"restart_hint,omitempty"`
	RestartAfter time.Time   `json:"restart_after,omitempty"`
	Apps         []AppStatus `json:"apps"`
}

type Manager struct {
	cfg       config.SupervisordConfig
	startedAt time.Time
	client    *http.Client
	logger    *slog.Logger

	mu           sync.Mutex
	apps         map[string]*appRuntime
	restartAfter time.Time
}

type appRuntime struct {
	cfg     config.SupervisorAppConfig
	cmd     *exec.Cmd
	status  AppStatus
	stopped chan struct{}
}

func NewManager(cfg config.SupervisordConfig, logger *slog.Logger) *Manager {
	if cfg.HeartbeatIntervalSeconds <= 0 {
		cfg.HeartbeatIntervalSeconds = 5
	}
	if cfg.ShutdownTimeoutSeconds <= 0 {
		cfg.ShutdownTimeoutSeconds = 10
	}
	if cfg.HealthdURL == "" {
		cfg.HealthdURL = "http://127.0.0.1:18081"
	}
	if logger == nil {
		logger = slog.Default()
	}
	m := &Manager{
		cfg:       cfg,
		startedAt: time.Now().UTC(),
		client:    &http.Client{Timeout: 3 * time.Second},
		logger:    logger,
		apps:      make(map[string]*appRuntime),
	}
	for _, app := range cfg.Apps {
		if app.Type == "" {
			app.Type = "process"
		}
		if app.Restart == "" {
			app.Restart = "on_failure"
		}
		if app.ShutdownTimeoutSec <= 0 {
			app.ShutdownTimeoutSec = cfg.ShutdownTimeoutSeconds
		}
		now := time.Now().UTC()
		m.apps[app.Name] = &appRuntime{
			cfg: app,
			status: AppStatus{
				Name:        app.Name,
				Type:        app.Type,
				State:       StateStopped,
				Desired:     StateStopped,
				Message:     "not started",
				UpdatedAt:   now,
				StartOrder:  app.StartOrder,
				Restart:     app.Restart,
				HealthURL:   app.HealthURL,
				WorkingDir:  app.WorkingDir,
				Command:     app.Command,
				Args:        append([]string(nil), app.Args...),
				Environment: copyMap(app.Env),
			},
		}
	}
	m.loadState()
	return m
}

func (m *Manager) Start(ctx context.Context) {
	go m.heartbeatLoop(ctx)
	go m.healthCheckLoop(ctx)
	go func() {
		<-ctx.Done()
		m.logger.Info("supervisord shutting down managed apps")
		m.StopAll(context.Background())
	}()
	for _, app := range m.sortedApps() {
		if app.AutoStart || m.appDesiredRunning(app.Name) {
			if err := m.StartApp(ctx, app.Name); err != nil {
				m.logger.Error("failed to autostart app", "app", app.Name, "error", err)
			}
		}
	}
}

func (m *Manager) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	apps := make([]AppStatus, 0, len(m.apps))
	status := "healthy"
	for _, app := range m.apps {
		s := app.status
		apps = append(apps, s)
		if s.State == StateFailed {
			status = "critical"
		} else if status == "healthy" && s.State != StateRunning && s.Desired == StateRunning {
			status = "warning"
		}
	}
	sort.Slice(apps, func(i, j int) bool {
		if apps[i].StartOrder == apps[j].StartOrder {
			return apps[i].Name < apps[j].Name
		}
		return apps[i].StartOrder < apps[j].StartOrder
	})
	restartHint := ""
	restartable := strings.TrimSpace(m.cfg.RestartCommand) != ""
	if restartable {
		restartHint = strings.TrimSpace(m.cfg.RestartCommand + " " + strings.Join(m.cfg.RestartArgs, " "))
	}
	return Snapshot{Status: status, StartedAt: m.startedAt, Restartable: restartable, RestartHint: restartHint, RestartAfter: m.restartAfter, Apps: apps}
}

func (m *Manager) StartApp(ctx context.Context, name string) error {
	_ = ctx
	m.mu.Lock()
	app, ok := m.apps[name]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("unknown app %q", name)
	}
	if app.status.State == StateRunning || app.status.State == StateStarting {
		if app.status.PID <= 0 || !processAlive(app.status.PID) {
			app.status.State = StateFailed
			app.status.PID = 0
			app.status.Message = "recorded process is not running"
			app.status.UpdatedAt = time.Now().UTC()
			_ = m.saveStateLocked()
		} else {
			app.status.Desired = StateRunning
			app.status.Message = "already running"
			app.status.UpdatedAt = time.Now().UTC()
			_ = m.saveStateLocked()
			m.mu.Unlock()
			return nil
		}
	}
	if app.status.State == StateStarting {
		app.status.Desired = StateRunning
		app.status.Message = "already starting"
		app.status.UpdatedAt = time.Now().UTC()
		_ = m.saveStateLocked()
		m.mu.Unlock()
		return nil
	}
	if app.status.State == StateStopping {
		app.status.Desired = StateRunning
		app.status.Message = "already stopping"
		app.status.UpdatedAt = time.Now().UTC()
		_ = m.saveStateLocked()
		m.mu.Unlock()
		return nil
	}
	if app.cfg.Command == "" {
		m.mu.Unlock()
		return fmt.Errorf("app %q has no command", name)
	}
	now := time.Now().UTC()
	app.status.State = StateStarting
	app.status.Desired = StateRunning
	app.status.Message = "starting"
	app.status.UpdatedAt = now
	_ = m.saveStateLocked()
	app.stopped = make(chan struct{})
	m.mu.Unlock()

	cmd := exec.Command(app.cfg.Command, app.cfg.Args...)
	if app.cfg.WorkingDir != "" {
		cmd.Dir = app.cfg.WorkingDir
	}
	cmd.Env = append(os.Environ(), envList(app.cfg.Env)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var output lockedBuffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		m.updateFailed(name, fmt.Sprintf("start failed: %v", err), output.String())
		return err
	}

	m.mu.Lock()
	app.cmd = cmd
	app.status.State = StateRunning
	app.status.PID = cmd.Process.Pid
	startedAt := time.Now().UTC()
	app.status.StartedAt = &startedAt
	app.status.StoppedAt = nil
	app.status.UpdatedAt = startedAt
	app.status.Message = "running"
	_ = m.saveStateLocked()
	m.mu.Unlock()
	m.logger.Info("started app", "app", name, "pid", cmd.Process.Pid)

	go m.waitApp(name, cmd, &output)
	return nil
}

func (m *Manager) StopApp(ctx context.Context, name string) error {
	return m.stopApp(ctx, name, StateStopped)
}

func (m *Manager) RestartApp(ctx context.Context, name string) error {
	if err := m.stopApp(ctx, name, StateRunning); err != nil {
		return err
	}
	return m.StartApp(ctx, name)
}

func (m *Manager) AdoptApp(name string, pid int) error {
	if pid <= 0 {
		return fmt.Errorf("pid must be positive")
	}
	if !processAlive(pid) {
		return fmt.Errorf("pid %d is not running", pid)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	app, ok := m.apps[name]
	if !ok {
		return fmt.Errorf("unknown app %q", name)
	}
	now := time.Now().UTC()
	if app.status.StartedAt == nil {
		app.status.StartedAt = &now
	}
	app.cmd = nil
	app.status.State = StateRunning
	app.status.Desired = StateRunning
	app.status.PID = pid
	app.status.StoppedAt = nil
	app.status.Message = "adopted existing process"
	app.status.UpdatedAt = now
	return m.saveStateLocked()
}

func (m *Manager) StopAll(ctx context.Context) {
	for _, app := range m.sortedAppsReverse() {
		if err := m.StopApp(ctx, app.Name); err != nil {
			m.logger.Error("failed to stop app", "app", app.Name, "error", err)
		}
	}
}

func (m *Manager) stopApp(ctx context.Context, name, desired string) error {
	m.mu.Lock()
	app, ok := m.apps[name]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("unknown app %q", name)
	}
	app.status.Desired = desired
	if (app.status.State == StateStopped || app.status.State == StateFailed) && app.status.PID <= 0 {
		app.status.State = StateStopped
		app.status.Message = "stopped"
		app.status.UpdatedAt = time.Now().UTC()
		_ = m.saveStateLocked()
		m.mu.Unlock()
		return nil
	}
	cmd := app.cmd
	stopped := app.stopped
	timeout := time.Duration(app.cfg.ShutdownTimeoutSec) * time.Second
	app.status.State = StateStopping
	app.status.Message = "stopping"
	app.status.UpdatedAt = time.Now().UTC()
	m.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		pid := app.status.PID
		if err := m.stopAdoptedProcess(ctx, pid, stopped, timeout); err != nil {
			return err
		}
		m.markAppStopped(name, pid, "stopped")
		return nil
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err == nil {
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
	} else {
		_ = cmd.Process.Signal(syscall.SIGTERM)
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-stopped:
		return nil
	case <-timer.C:
		m.logger.Warn("app did not stop gracefully; killing", "app", name)
		if pgid, err := syscall.Getpgid(cmd.Process.Pid); err == nil {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		} else {
			_ = cmd.Process.Kill()
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) markAppStopped(name string, pid int, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	app := m.apps[name]
	if app == nil {
		return
	}
	if pid > 0 && app.status.PID != pid {
		return
	}
	stoppedAt := time.Now().UTC()
	app.status.State = StateStopped
	app.status.PID = 0
	app.status.Message = message
	app.status.StoppedAt = &stoppedAt
	app.status.UpdatedAt = stoppedAt
	if app.stopped != nil {
		close(app.stopped)
		app.stopped = nil
	}
	_ = m.saveStateLocked()
}

func (m *Manager) stopAdoptedProcess(ctx context.Context, pid int, stopped <-chan struct{}, timeout time.Duration) error {
	if pid <= 0 {
		return nil
	}
	if pgid, err := syscall.Getpgid(pid); err == nil {
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
	} else if process, err := os.FindProcess(pid); err == nil {
		_ = process.Signal(syscall.SIGTERM)
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stopped:
			return nil
		case <-deadline.C:
			if pgid, err := syscall.Getpgid(pid); err == nil {
				_ = syscall.Kill(-pgid, syscall.SIGKILL)
			} else if process, err := os.FindProcess(pid); err == nil {
				_ = process.Kill()
			}
			return nil
		case <-ticker.C:
			if !processAlive(pid) {
				return nil
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (m *Manager) waitApp(name string, cmd *exec.Cmd, output *lockedBuffer) {
	err := cmd.Wait()
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	shouldRestart := false
	m.mu.Lock()
	app := m.apps[name]
	if app != nil {
		if app.cmd == cmd {
			app.cmd = nil
		}
		app.status.PID = 0
		app.status.ExitCode = exitCode
		stoppedAt := time.Now().UTC()
		app.status.StoppedAt = &stoppedAt
		app.status.UpdatedAt = stoppedAt
		app.status.LastOutput = tail(output.String(), 4000)
		if app.status.State == StateStopping || app.status.Desired == StateStopped {
			app.status.State = StateStopped
			app.status.Message = "stopped"
		} else if exitCode == 0 {
			app.status.State = StateStopped
			app.status.Message = "exited cleanly"
			shouldRestart = app.cfg.Restart == "always" && app.status.Desired == StateRunning
		} else {
			app.status.State = StateFailed
			app.status.Message = fmt.Sprintf("exited with code %d", exitCode)
			shouldRestart = app.status.Desired == StateRunning && (app.cfg.Restart == "always" || app.cfg.Restart == "on_failure")
		}
		if shouldRestart {
			app.status.Restarts++
			app.status.Message += "; restarting"
		}
		if app.stopped != nil {
			close(app.stopped)
			app.stopped = nil
		}
		_ = m.saveStateLocked()
	}
	m.mu.Unlock()
	if shouldRestart {
		time.Sleep(time.Second)
		if err := m.StartApp(context.Background(), name); err != nil {
			m.logger.Error("restart failed", "app", name, "error", err)
		}
	}
}

func (m *Manager) updateFailed(name, message, output string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if app := m.apps[name]; app != nil {
		app.status.State = StateFailed
		app.status.Message = message
		app.status.LastOutput = tail(output, 4000)
		app.status.UpdatedAt = time.Now().UTC()
		_ = m.saveStateLocked()
	}
}

func (m *Manager) RestartSelf(ctx context.Context) error {
	_ = ctx
	m.mu.Lock()
	if err := m.saveStateLocked(); err != nil {
		m.mu.Unlock()
		return err
	}
	command := strings.TrimSpace(m.cfg.RestartCommand)
	args := append([]string(nil), m.cfg.RestartArgs...)
	workingDir := m.cfg.WorkingDir
	if command == "" {
		executable, err := os.Executable()
		if err != nil {
			m.mu.Unlock()
			return err
		}
		command = executable
		args = os.Args[1:]
	}
	m.restartAfter = time.Now().UTC()
	m.mu.Unlock()

	helperArgs := append([]string{"-c", `sleep 0.5; exec "$@"`, "supervisord-restart", command}, args...)
	cmd := exec.Command("sh", helperArgs...)
	if workingDir != "" {
		cmd.Dir = workingDir
	}
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if m.cfg.StatePath != "" {
		if err := os.MkdirAll(filepath.Dir(m.cfg.StatePath), 0o755); err == nil {
			if f, err := os.OpenFile(filepath.Join(filepath.Dir(m.cfg.StatePath), "restart.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
				cmd.Stdout = f
				cmd.Stderr = f
			}
		}
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	m.logger.Info("scheduled supervisord self restart", "command", command, "args", args)
	go func() {
		time.Sleep(100 * time.Millisecond)
		os.Exit(0)
	}()
	return nil
}

func (m *Manager) StopSelf() error {
	m.mu.Lock()
	if err := m.saveStateLocked(); err != nil {
		m.mu.Unlock()
		return err
	}
	m.mu.Unlock()
	m.logger.Info("scheduled supervisord self stop")
	go func() {
		time.Sleep(100 * time.Millisecond)
		os.Exit(0)
	}()
	return nil
}

func (m *Manager) heartbeatLoop(ctx context.Context) {
	m.pushHeartbeat(ctx)
	ticker := time.NewTicker(time.Duration(m.cfg.HeartbeatIntervalSeconds) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.pushHeartbeat(ctx)
		}
	}
}

func (m *Manager) healthCheckLoop(ctx context.Context) {
	m.checkAppHealth(ctx)
	ticker := time.NewTicker(time.Duration(m.cfg.HeartbeatIntervalSeconds) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.reconcileDesiredState(ctx)
			m.checkAppHealth(ctx)
		}
	}
}

func (m *Manager) appDesiredRunning(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	app := m.apps[name]
	return app != nil && app.status.Desired == StateRunning
}

func (m *Manager) reconcileDesiredState(ctx context.Context) {
	m.mu.Lock()
	var toStart []string
	for name, app := range m.apps {
		if app.status.Desired != StateRunning {
			continue
		}
		if (app.status.State == StateRunning || app.status.State == StateStarting) && app.status.PID > 0 && !processAlive(app.status.PID) {
			app.status.State = StateFailed
			app.status.PID = 0
			app.status.Message = "recorded process is not running; restarting"
			app.status.UpdatedAt = time.Now().UTC()
		}
		if app.status.State != StateStopped && app.status.State != StateFailed {
			continue
		}
		if app.cfg.Restart == "never" {
			continue
		}
		app.status.Restarts++
		app.status.Message = "desired running; restarting"
		app.status.UpdatedAt = time.Now().UTC()
		toStart = append(toStart, name)
	}
	if len(toStart) > 0 {
		_ = m.saveStateLocked()
	}
	m.mu.Unlock()

	for _, name := range toStart {
		m.logger.Warn("recovering app because desired state is running", "app", name)
		if err := m.StartApp(ctx, name); err != nil {
			m.logger.Error("desired state recovery failed", "app", name, "error", err)
		}
	}
}

type persistedState struct {
	UpdatedAt time.Time               `json:"updated_at"`
	Apps      map[string]persistedApp `json:"apps"`
}

type persistedApp struct {
	State      string     `json:"state"`
	Desired    string     `json:"desired"`
	PID        int        `json:"pid,omitempty"`
	Restarts   int        `json:"restarts"`
	ExitCode   int        `json:"exit_code,omitempty"`
	Message    string     `json:"message"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	StoppedAt  *time.Time `json:"stopped_at,omitempty"`
	LastOutput string     `json:"last_output,omitempty"`
}

func (m *Manager) loadState() {
	if m.cfg.StatePath == "" {
		return
	}
	b, err := os.ReadFile(m.cfg.StatePath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			m.logger.Warn("failed to read supervisord state", "path", m.cfg.StatePath, "error", err)
		}
		return
	}
	var state persistedState
	if err := json.Unmarshal(b, &state); err != nil {
		m.logger.Warn("failed to parse supervisord state", "path", m.cfg.StatePath, "error", err)
		return
	}
	now := time.Now().UTC()
	for name, saved := range state.Apps {
		app := m.apps[name]
		if app == nil {
			continue
		}
		app.status.Desired = saved.Desired
		app.status.Restarts = saved.Restarts
		app.status.ExitCode = saved.ExitCode
		app.status.StartedAt = saved.StartedAt
		app.status.StoppedAt = saved.StoppedAt
		app.status.LastOutput = saved.LastOutput
		app.status.UpdatedAt = now
		if saved.PID > 0 && processAlive(saved.PID) && saved.Desired == StateRunning {
			app.status.State = StateRunning
			app.status.PID = saved.PID
			app.status.Message = "adopted running process after supervisord restart"
			continue
		}
		app.status.State = StateStopped
		app.status.PID = 0
		if saved.Desired == StateRunning {
			app.status.Desired = StateRunning
			app.status.Message = "previous process was not running; ready to start"
		} else if saved.Message != "" {
			app.status.Message = saved.Message
		}
	}
}

func (m *Manager) saveStateLocked() error {
	if m.cfg.StatePath == "" {
		return nil
	}
	state := persistedState{UpdatedAt: time.Now().UTC(), Apps: make(map[string]persistedApp, len(m.apps))}
	for name, app := range m.apps {
		state.Apps[name] = persistedApp{
			State:      app.status.State,
			Desired:    app.status.Desired,
			PID:        app.status.PID,
			Restarts:   app.status.Restarts,
			ExitCode:   app.status.ExitCode,
			Message:    app.status.Message,
			StartedAt:  app.status.StartedAt,
			StoppedAt:  app.status.StoppedAt,
			LastOutput: app.status.LastOutput,
		}
	}
	if err := os.MkdirAll(filepath.Dir(m.cfg.StatePath), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.cfg.StatePath, append(b, '\n'), 0o644)
}

func (m *Manager) checkAppHealth(ctx context.Context) {
	m.mu.Lock()
	type target struct {
		name string
		url  string
	}
	var targets []target
	var toRestart []string
	for name, app := range m.apps {
		if app.status.State == StateRunning && app.cfg.HealthURL != "" {
			targets = append(targets, target{name: name, url: app.cfg.HealthURL})
		}
	}
	m.mu.Unlock()
	for _, t := range targets {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.url, nil)
		status := "unreachable"
		if err == nil {
			if resp, err := m.client.Do(req); err == nil {
				status = resp.Status
				_ = resp.Body.Close()
			}
		}
		m.mu.Lock()
		if app := m.apps[t.name]; app != nil {
			app.status.LastHealth = status
			app.status.UpdatedAt = time.Now().UTC()
			if status == "unreachable" &&
				app.status.Desired == StateRunning &&
				app.status.State == StateRunning &&
				app.status.PID > 0 &&
				app.cfg.Restart != "never" {
				app.status.State = StateStopping
				app.status.Message = "health check unreachable; restarting tracked process"
				app.status.Restarts++
				_ = m.saveStateLocked()
				toRestart = append(toRestart, t.name)
			}
		}
		m.mu.Unlock()
	}
	for _, name := range toRestart {
		go func(name string) {
			m.logger.Warn("restarting app because health check is unreachable", "app", name)
			if err := m.RestartApp(context.Background(), name); err != nil {
				m.logger.Error("health recovery failed", "app", name, "error", err)
			}
		}(name)
	}
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

func (m *Manager) pushHeartbeat(ctx context.Context) {
	if strings.TrimSpace(m.cfg.HealthdURL) == "" {
		return
	}
	snapshot := m.Snapshot()
	state := snapshot.Status
	body, _ := json.Marshal(map[string]any{
		"name":        "supervisord",
		"type":        "supervisor",
		"pid":         os.Getpid(),
		"addr":        m.cfg.Addr,
		"started_at":  m.startedAt,
		"ttl_seconds": m.cfg.HeartbeatIntervalSeconds * 3,
		"metadata": map[string]string{
			"status": state,
			"apps":   fmt.Sprintf("%d", len(snapshot.Apps)),
		},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(m.cfg.HealthdURL, "/")+"/healthd/processes/heartbeat", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.client.Do(req)
	if err != nil {
		m.logger.Debug("healthd heartbeat failed", "error", err)
		return
	}
	_ = resp.Body.Close()
}

func (m *Manager) sortedApps() []config.SupervisorAppConfig {
	apps := append([]config.SupervisorAppConfig(nil), m.cfg.Apps...)
	sort.Slice(apps, func(i, j int) bool {
		if apps[i].StartOrder == apps[j].StartOrder {
			return apps[i].Name < apps[j].Name
		}
		return apps[i].StartOrder < apps[j].StartOrder
	})
	return apps
}

func (m *Manager) sortedAppsReverse() []config.SupervisorAppConfig {
	apps := m.sortedApps()
	for i, j := 0, len(apps)-1; i < j; i, j = i+1, j-1 {
		apps[i], apps[j] = apps[j], apps[i]
	}
	return apps
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func envList(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	out := make([]string, 0, len(env))
	for key, value := range env {
		out = append(out, key+"="+value)
	}
	sort.Strings(out)
	return out
}

func copyMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func tail(s string, limit int) string {
	if limit <= 0 || len(s) <= limit {
		return s
	}
	return s[len(s)-limit:]
}
