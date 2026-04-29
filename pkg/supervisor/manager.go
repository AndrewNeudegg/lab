package supervisor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/andrewneudegg/lab/pkg/config"
	"github.com/andrewneudegg/lab/pkg/healthd"
	"github.com/andrewneudegg/lab/pkg/id"
)

const (
	StateStopped  = "stopped"
	StateStarting = "starting"
	StateRunning  = "running"
	StateStopping = "stopping"
	StateFailed   = "failed"
)

type AppStatus struct {
	Name               string            `json:"name"`
	Type               string            `json:"type"`
	State              string            `json:"state"`
	Desired            string            `json:"desired"`
	PID                int               `json:"pid,omitempty"`
	Restarts           int               `json:"restarts"`
	ExitCode           int               `json:"exit_code,omitempty"`
	Message            string            `json:"message"`
	StartedAt          *time.Time        `json:"started_at,omitempty"`
	StoppedAt          *time.Time        `json:"stopped_at,omitempty"`
	UpdatedAt          time.Time         `json:"updated_at"`
	StartOrder         int               `json:"start_order"`
	Restart            string            `json:"restart"`
	HealthURL          string            `json:"health_url,omitempty"`
	LastHealth         string            `json:"last_health,omitempty"`
	LastOutput         string            `json:"last_output,omitempty"`
	LastError          string            `json:"last_error,omitempty"`
	LogPath            string            `json:"log_path,omitempty"`
	ErrorLogPath       string            `json:"error_log_path,omitempty"`
	WorkingDir         string            `json:"working_dir,omitempty"`
	Command            string            `json:"command"`
	Args               []string          `json:"args,omitempty"`
	Environment        map[string]string `json:"environment,omitempty"`
	PreStartCommand    string            `json:"pre_start_command,omitempty"`
	PreStartArgs       []string          `json:"pre_start_args,omitempty"`
	PreStartWorkingDir string            `json:"pre_start_working_dir,omitempty"`
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

	mu            sync.Mutex
	apps          map[string]*appRuntime
	restartAfter  time.Time
	pendingErrors []healthd.ApplicationError
}

type appRuntime struct {
	opMu    sync.Mutex
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
	if cfg.LogDir == "" && cfg.StatePath != "" {
		cfg.LogDir = filepath.Join(filepath.Dir(cfg.StatePath), "logs")
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
		logPath, errorLogPath := appLogPaths(cfg.LogDir, app.Name)
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
				Name:               app.Name,
				Type:               app.Type,
				State:              StateStopped,
				Desired:            StateStopped,
				Message:            "not started",
				UpdatedAt:          now,
				StartOrder:         app.StartOrder,
				Restart:            app.Restart,
				HealthURL:          app.HealthURL,
				LogPath:            logPath,
				ErrorLogPath:       errorLogPath,
				WorkingDir:         app.WorkingDir,
				Command:            app.Command,
				Args:               append([]string(nil), app.Args...),
				Environment:        copyMap(app.Env),
				PreStartCommand:    app.PreStartCommand,
				PreStartArgs:       append([]string(nil), app.PreStartArgs...),
				PreStartWorkingDir: app.PreStartWorkingDir,
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

func (m *Manager) appForOperation(name string) (*appRuntime, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	app := m.apps[name]
	if app == nil {
		return nil, fmt.Errorf("unknown app %q", name)
	}
	return app, nil
}

func (m *Manager) StartApp(ctx context.Context, name string) error {
	app, err := m.appForOperation(name)
	if err != nil {
		return err
	}
	app.opMu.Lock()
	defer app.opMu.Unlock()
	return m.startApp(ctx, name)
}

func (m *Manager) startApp(ctx context.Context, name string) error {
	_ = ctx
	m.mu.Lock()
	app, ok := m.apps[name]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("unknown app %q", name)
	}
	if app.status.State == StateStarting {
		app.status.Desired = StateRunning
		app.status.Message = "already starting"
		app.status.UpdatedAt = time.Now().UTC()
		_ = m.saveStateLocked()
		m.mu.Unlock()
		return nil
	}
	if app.status.State == StateRunning {
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
	if app.status.PID > 0 {
		if processAlive(app.status.PID) {
			pid := app.status.PID
			state := app.status.State
			m.mu.Unlock()
			return fmt.Errorf("app %q still has live pid %d while %s", name, pid, state)
		}
		app.status.PID = 0
	}
	now := time.Now().UTC()
	app.status.State = StateStarting
	app.status.Desired = StateRunning
	app.status.Message = "starting"
	if app.cfg.PreStartCommand != "" {
		app.status.Message = "running pre-start command"
	}
	app.status.LastOutput = ""
	app.status.LastError = ""
	app.status.UpdatedAt = now
	_ = m.saveStateLocked()
	app.stopped = make(chan struct{})
	m.mu.Unlock()

	if app.cfg.PreStartCommand != "" {
		output, err := m.runPreStart(app.cfg)
		if err != nil {
			message := fmt.Sprintf("pre-start failed: %v", err)
			m.updateFailed(name, message, output, output)
			return fmt.Errorf("app %q %s", name, message)
		}
		m.mu.Lock()
		if runningApp := m.apps[name]; runningApp != nil {
			runningApp.status.Message = "starting"
			runningApp.status.UpdatedAt = time.Now().UTC()
			_ = m.saveStateLocked()
		}
		m.mu.Unlock()
	}

	cmd := exec.Command(app.cfg.Command, app.cfg.Args...)
	if app.cfg.WorkingDir != "" {
		cmd.Dir = app.cfg.WorkingDir
	}
	cmd.Env = append(os.Environ(), envList(app.cfg.Env)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var output lockedBuffer
	var errorOutput lockedBuffer
	stdoutLog, stderrLog, logPath, errorLogPath, err := m.openAppLogs(name)
	if err != nil {
		m.updateFailed(name, fmt.Sprintf("open log files failed: %v", err), output.String(), errorOutput.String())
		return err
	}
	stdoutWriter := &appOutputWriter{buffer: &output, file: stdoutLog}
	stderrWriter := &appErrorWriter{manager: m, app: name, output: &output, errors: &errorOutput, file: stderrLog, logPath: errorLogPath}
	cmd.Stdout = stdoutWriter
	cmd.Stderr = stderrWriter
	if err := cmd.Start(); err != nil {
		closeFiles(stdoutLog, stderrLog)
		m.updateFailed(name, fmt.Sprintf("start failed: %v", err), output.String(), errorOutput.String())
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
	app.status.LastOutput = ""
	app.status.LastError = ""
	app.status.LogPath = logPath
	app.status.ErrorLogPath = errorLogPath
	_ = m.saveStateLocked()
	m.mu.Unlock()
	m.logger.Info("started app", "app", name, "pid", cmd.Process.Pid)

	go m.waitApp(name, cmd, &output, &errorOutput, stderrWriter, stdoutLog, stderrLog)
	return nil
}

func (m *Manager) runPreStart(app config.SupervisorAppConfig) (string, error) {
	timeout := time.Duration(app.PreStartTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, app.PreStartCommand, app.PreStartArgs...)
	if app.PreStartWorkingDir != "" {
		cmd.Dir = app.PreStartWorkingDir
	} else if app.WorkingDir != "" {
		cmd.Dir = app.WorkingDir
	}
	cmd.Env = append(os.Environ(), envList(app.Env)...)
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(output), fmt.Errorf("%s timed out after %s", preStartLabel(app), timeout)
	}
	if err != nil {
		return string(output), fmt.Errorf("%s failed: %w", preStartLabel(app), err)
	}
	return string(output), nil
}

func (m *Manager) StopApp(ctx context.Context, name string) error {
	return m.stopApp(ctx, name, StateStopped)
}

func (m *Manager) RestartApp(ctx context.Context, name string) error {
	app, err := m.appForOperation(name)
	if err != nil {
		return err
	}
	app.opMu.Lock()
	defer app.opMu.Unlock()
	if err := m.stopAppOperation(ctx, name, StateRunning); err != nil {
		return err
	}
	return m.startApp(ctx, name)
}

func (m *Manager) restartAppIfDesiredRunning(ctx context.Context, name string) error {
	app, err := m.appForOperation(name)
	if err != nil {
		return err
	}
	app.opMu.Lock()
	defer app.opMu.Unlock()

	m.mu.Lock()
	if app.status.Desired != StateRunning {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	if err := m.stopAppOperation(ctx, name, StateRunning); err != nil {
		return err
	}
	return m.startApp(ctx, name)
}

func (m *Manager) AdoptApp(name string, pid int) error {
	if pid <= 0 {
		return fmt.Errorf("pid must be positive")
	}
	if !processAlive(pid) {
		return fmt.Errorf("pid %d is not running", pid)
	}
	app, err := m.appForOperation(name)
	if err != nil {
		return err
	}
	app.opMu.Lock()
	defer app.opMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
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
	app, err := m.appForOperation(name)
	if err != nil {
		return err
	}
	app.opMu.Lock()
	defer app.opMu.Unlock()
	return m.stopAppOperation(ctx, name, desired)
}

func (m *Manager) stopAppOperation(ctx context.Context, name, desired string) error {
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
	pid := app.status.PID
	timeout := time.Duration(app.cfg.ShutdownTimeoutSec) * time.Second
	app.status.State = StateStopping
	app.status.Message = "stopping"
	app.status.UpdatedAt = time.Now().UTC()
	_ = m.saveStateLocked()
	m.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		if err := m.stopProcessSet(ctx, name, pid, stopped, timeout, false); err != nil {
			return err
		}
		m.markAppStopped(name, pid, "stopped")
		return nil
	}
	return m.stopProcessSet(ctx, name, cmd.Process.Pid, stopped, timeout, true)
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

func (m *Manager) stopProcessSet(ctx context.Context, name string, pid int, stopped <-chan struct{}, timeout time.Duration, includeGroup bool) error {
	if pid <= 0 {
		return nil
	}
	processes := newProcessSet(pid, includeGroup)
	processes.signal(syscall.SIGTERM)
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stopped:
			if processes.alive() {
				childGrace := time.NewTimer(250 * time.Millisecond)
				select {
				case <-childGrace.C:
				case <-ctx.Done():
					childGrace.Stop()
					return ctx.Err()
				}
				if processes.alive() {
					m.logger.Warn("app process exited but child processes remained; killing", "app", name, "pid", pid)
					return m.forceKillProcessSet(ctx, name, processes, nil)
				}
			}
			return nil
		case <-deadline.C:
			m.logger.Warn("app did not stop gracefully; killing", "app", name, "pid", pid)
			return m.forceKillProcessSet(ctx, name, processes, stopped)
		case <-ticker.C:
			if stopped == nil && !processes.alive() {
				return nil
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (m *Manager) forceKillProcessSet(ctx context.Context, name string, processes *processSet, stopped <-chan struct{}) error {
	processes.signal(syscall.SIGKILL)
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stopped:
			stopped = nil
			if !processes.alive() {
				return nil
			}
		case <-ticker.C:
			if processes.alive() {
				processes.signal(syscall.SIGKILL)
				continue
			}
			return nil
		case <-deadline.C:
			if !processes.alive() {
				return nil
			}
			return fmt.Errorf("app %q pid %d did not exit after SIGKILL", name, processes.root)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (m *Manager) waitApp(name string, cmd *exec.Cmd, output, errorOutput *lockedBuffer, stderrWriter *appErrorWriter, logFiles ...*os.File) {
	err := cmd.Wait()
	if stderrWriter != nil {
		stderrWriter.Flush()
	}
	closeFiles(logFiles...)
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
		app.status.LastError = lastErrorLine(errorOutput.String(), 4000)
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

func (m *Manager) updateFailed(name, message, output, lastError string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if app := m.apps[name]; app != nil {
		app.status.State = StateFailed
		app.status.Message = message
		app.status.LastOutput = tail(output, 4000)
		app.status.LastError = lastErrorLine(lastError, 4000)
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
	LastError  string     `json:"last_error,omitempty"`
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
		app.status.LastError = saved.LastError
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
			LastError:  app.status.LastError,
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
		statusCode := 0
		if err == nil {
			if resp, err := m.client.Do(req); err == nil {
				status = resp.Status
				statusCode = resp.StatusCode
				_ = resp.Body.Close()
			}
		}
		m.mu.Lock()
		if app := m.apps[t.name]; app != nil {
			app.status.LastHealth = status
			app.status.UpdatedAt = time.Now().UTC()
			if unhealthyHealthStatus(status, statusCode) &&
				app.status.Desired == StateRunning &&
				app.status.State == StateRunning &&
				app.status.PID > 0 &&
				app.cfg.Restart != "never" {
				app.status.State = StateStopping
				app.status.Message = "health check failed; restarting tracked process"
				app.status.Restarts++
				_ = m.saveStateLocked()
				toRestart = append(toRestart, t.name)
			}
		}
		m.mu.Unlock()
	}
	for _, name := range toRestart {
		go func(name string) {
			m.logger.Warn("restarting app because health check failed", "app", name)
			if err := m.restartAppIfDesiredRunning(context.Background(), name); err != nil {
				m.logger.Error("health recovery failed", "app", name, "error", err)
			}
		}(name)
	}
}

func unhealthyHealthStatus(status string, statusCode int) bool {
	if status == "unreachable" {
		return true
	}
	return statusCode < 200 || statusCode >= 300
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	if _, state, ok := procStat(pid); ok && state == "Z" {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

type processSet struct {
	root         int
	includeGroup bool
	pgid         int
	seen         map[int]struct{}
}

func newProcessSet(root int, includeGroup bool) *processSet {
	ps := &processSet{
		root:         root,
		includeGroup: includeGroup,
		seen:         map[int]struct{}{root: {}},
	}
	if includeGroup {
		if pgid, err := syscall.Getpgid(root); err == nil {
			ps.pgid = pgid
		}
	}
	ps.refresh()
	return ps
}

func (ps *processSet) signal(sig syscall.Signal) {
	ps.refresh()
	if ps.includeGroup && ps.pgid > 0 {
		_ = syscall.Kill(-ps.pgid, sig)
	}
	for pid := range ps.seen {
		if pid == os.Getpid() {
			continue
		}
		if process, err := os.FindProcess(pid); err == nil {
			_ = process.Signal(sig)
		}
	}
}

func (ps *processSet) alive() bool {
	ps.refresh()
	for pid := range ps.seen {
		if processAlive(pid) {
			return true
		}
	}
	return false
}

func (ps *processSet) refresh() {
	if ps.root <= 0 {
		return
	}
	for _, pid := range descendantPIDs(ps.root) {
		ps.seen[pid] = struct{}{}
	}
	if ps.includeGroup && ps.pgid > 0 {
		for _, pid := range processGroupPIDs(ps.pgid) {
			ps.seen[pid] = struct{}{}
		}
	}
}

func descendantPIDs(root int) []int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	children := make(map[int][]int)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		ppid, _, ok := procStat(pid)
		if !ok {
			continue
		}
		children[ppid] = append(children[ppid], pid)
	}
	var out []int
	queue := append([]int(nil), children[root]...)
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		out = append(out, pid)
		queue = append(queue, children[pid]...)
	}
	return out
}

func processGroupPIDs(pgid int) []int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var out []int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		got, err := syscall.Getpgid(pid)
		if err == nil && got == pgid {
			out = append(out, pid)
		}
	}
	return out
}

func procStat(pid int) (ppid int, state string, ok bool) {
	b, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, "", false
	}
	stat := string(b)
	end := strings.LastIndex(stat, ")")
	if end < 0 || end+2 >= len(stat) {
		return 0, "", false
	}
	fields := strings.Fields(stat[end+2:])
	if len(fields) < 2 {
		return 0, "", false
	}
	ppid, err = strconv.Atoi(fields[1])
	if err != nil {
		return 0, "", false
	}
	return ppid, fields[0], true
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
	m.pushPendingErrors(ctx)
}

func (m *Manager) pushPendingErrors(ctx context.Context) {
	entries := m.pendingAppErrors()
	if len(entries) == 0 {
		return
	}
	if err := healthd.PushErrors(ctx, m.client, m.cfg.HealthdURL, entries); err != nil {
		m.logger.Debug("healthd error push failed", "error", err)
		return
	}
	m.markAppErrorsPushed(entries)
}

func (m *Manager) pendingAppErrors() []healthd.ApplicationError {
	m.mu.Lock()
	defer m.mu.Unlock()
	return healthdCopyErrors(m.pendingErrors)
}

func (m *Manager) markAppErrorsPushed(entries []healthd.ApplicationError) {
	if len(entries) == 0 {
		return
	}
	pushed := make(map[string]bool, len(entries))
	for _, entry := range entries {
		pushed[entry.ID] = true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	remaining := m.pendingErrors[:0]
	for _, entry := range m.pendingErrors {
		if !pushed[entry.ID] {
			remaining = append(remaining, entry)
		}
	}
	m.pendingErrors = remaining
}

func (m *Manager) recordAppError(name, message, logPath string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	now := time.Now().UTC()
	entry := healthd.ApplicationError{
		ID:       id.New("apperr"),
		Time:     now,
		Source:   "supervisord",
		App:      name,
		Severity: healthd.SeverityWarn,
		Message:  tail(message, 2000),
		LogPath:  logPath,
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if app := m.apps[name]; app != nil {
		app.status.LastError = entry.Message
		app.status.ErrorLogPath = logPath
		app.status.UpdatedAt = now
	}
	m.pendingErrors = append(m.pendingErrors, entry)
	if len(m.pendingErrors) > 500 {
		m.pendingErrors = append([]healthd.ApplicationError(nil), m.pendingErrors[len(m.pendingErrors)-500:]...)
	}
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

func (m *Manager) openAppLogs(name string) (*os.File, *os.File, string, string, error) {
	logPath, errorLogPath := appLogPaths(m.cfg.LogDir, name)
	if m.cfg.LogDir == "" {
		return nil, nil, "", "", nil
	}
	if err := os.MkdirAll(m.cfg.LogDir, 0o755); err != nil {
		return nil, nil, "", "", err
	}
	stdoutLog, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, nil, "", "", err
	}
	stderrLog, err := os.OpenFile(errorLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		_ = stdoutLog.Close()
		return nil, nil, "", "", err
	}
	return stdoutLog, stderrLog, logPath, errorLogPath, nil
}

func appLogPaths(logDir, name string) (string, string) {
	if logDir == "" {
		return "", ""
	}
	base := safeLogName(name)
	return filepath.Join(logDir, base+".stdout.log"), filepath.Join(logDir, base+".stderr.log")
}

func safeLogName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	if b.Len() == 0 {
		return "app"
	}
	return b.String()
}

func closeFiles(files ...*os.File) {
	for _, file := range files {
		if file != nil {
			_ = file.Close()
		}
	}
}

func healthdCopyErrors(entries []healthd.ApplicationError) []healthd.ApplicationError {
	out := make([]healthd.ApplicationError, len(entries))
	copy(out, entries)
	return out
}

type appOutputWriter struct {
	buffer io.Writer
	file   *os.File
}

func (w *appOutputWriter) Write(p []byte) (int, error) {
	if w.buffer != nil {
		_, _ = w.buffer.Write(p)
	}
	if w.file != nil {
		_, _ = w.file.Write(p)
	}
	return len(p), nil
}

type appErrorWriter struct {
	manager *Manager
	app     string
	output  io.Writer
	errors  io.Writer
	file    *os.File
	logPath string

	mu      sync.Mutex
	partial string
}

func (w *appErrorWriter) Write(p []byte) (int, error) {
	if w.output != nil {
		_, _ = w.output.Write(p)
	}
	if w.errors != nil {
		_, _ = w.errors.Write(p)
	}
	if w.file != nil {
		_, _ = w.file.Write(p)
	}
	w.captureLines(string(p))
	return len(p), nil
}

func (w *appErrorWriter) Flush() {
	w.mu.Lock()
	partial := w.partial
	w.partial = ""
	w.mu.Unlock()
	w.record(partial)
}

func (w *appErrorWriter) captureLines(chunk string) {
	w.mu.Lock()
	text := w.partial + chunk
	lines := strings.SplitAfter(text, "\n")
	if strings.HasSuffix(text, "\n") {
		w.partial = ""
	} else {
		w.partial = lines[len(lines)-1]
		lines = lines[:len(lines)-1]
	}
	w.mu.Unlock()
	for _, line := range lines {
		w.record(line)
	}
}

func (w *appErrorWriter) record(line string) {
	if w.manager == nil {
		return
	}
	w.manager.recordAppError(w.app, line, w.logPath)
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

func preStartLabel(app config.SupervisorAppConfig) string {
	parts := append([]string{app.PreStartCommand}, app.PreStartArgs...)
	return "pre-start command " + strconv.Quote(strings.Join(parts, " "))
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

func lastErrorLine(s string, limit int) string {
	s = strings.TrimSpace(tail(s, limit))
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			return line
		}
	}
	return ""
}
