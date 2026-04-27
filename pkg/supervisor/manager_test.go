package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/andrewneudegg/lab/pkg/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestManagerStartsStopsAndRestartsApp(t *testing.T) {
	script := filepath.Join(t.TempDir(), "app.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntrap 'echo stopping; exit 0' TERM INT\nwhile true; do sleep 1; done\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(config.SupervisordConfig{
		ShutdownTimeoutSeconds: 2,
		Apps: []config.SupervisorAppConfig{{
			Name:               "test-app",
			Type:               "test",
			Command:            script,
			Restart:            "on_failure",
			ShutdownTimeoutSec: 2,
		}},
	}, nil)

	if err := manager.StartApp(context.Background(), "test-app"); err != nil {
		t.Fatal(err)
	}
	running := manager.Snapshot().Apps[0]
	if running.State != StateRunning || running.PID == 0 {
		t.Fatalf("status = %#v, want running with pid", running)
	}
	if err := manager.RestartApp(context.Background(), "test-app"); err != nil {
		t.Fatal(err)
	}
	restarted := manager.Snapshot().Apps[0]
	if restarted.State != StateRunning || restarted.PID == 0 || restarted.PID == running.PID {
		t.Fatalf("status = %#v, want restarted with new pid", restarted)
	}
	if err := manager.StopApp(context.Background(), "test-app"); err != nil {
		t.Fatal(err)
	}
	stopped := manager.Snapshot().Apps[0]
	if stopped.State != StateStopped {
		t.Fatalf("status = %#v, want stopped", stopped)
	}
}

func TestManagerPushesHealthdHeartbeat(t *testing.T) {
	heartbeat := make(chan struct{}, 1)
	manager := NewManager(config.SupervisordConfig{
		HealthdURL:               "http://healthd.test",
		HeartbeatIntervalSeconds: 1,
	}, nil)
	manager.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/healthd/processes/heartbeat" {
			t.Fatalf("path = %q", req.URL.Path)
		}
		heartbeat <- struct{}{}
		return &http.Response{
			StatusCode: http.StatusAccepted,
			Status:     "202 Accepted",
			Body:       http.NoBody,
		}, nil
	})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	manager.Start(ctx)
	select {
	case <-heartbeat:
	case <-time.After(2 * time.Second):
		t.Fatal("heartbeat not pushed")
	}
}

func TestManagerAdoptsPersistedRunningProcess(t *testing.T) {
	script := filepath.Join(t.TempDir(), "app.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntrap 'exit 0' TERM INT\nwhile true; do sleep 1; done\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(t.TempDir(), "state.json")
	cfg := config.SupervisordConfig{
		StatePath:              statePath,
		ShutdownTimeoutSeconds: 2,
		Apps: []config.SupervisorAppConfig{{
			Name:               "test-app",
			Type:               "test",
			Command:            script,
			Restart:            "on_failure",
			ShutdownTimeoutSec: 2,
		}},
	}
	manager := NewManager(cfg, nil)
	if err := manager.StartApp(context.Background(), "test-app"); err != nil {
		t.Fatal(err)
	}
	pid := manager.Snapshot().Apps[0].PID
	if pid == 0 {
		t.Fatal("managed app did not start")
	}

	adopted := NewManager(cfg, nil)
	status := adopted.Snapshot().Apps[0]
	if status.State != StateRunning || status.PID != pid || status.Message == "running" {
		t.Fatalf("status = %#v, want adopted running process", status)
	}
	if err := adopted.StopApp(context.Background(), "test-app"); err != nil {
		t.Fatal(err)
	}
	if processAlive(pid) {
		t.Fatalf("pid %d should be stopped", pid)
	}
}

func TestManagerStartsPersistedDesiredRunningProcess(t *testing.T) {
	script := filepath.Join(t.TempDir(), "app.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntrap 'exit 0' TERM INT\nwhile true; do sleep 1; done\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(t.TempDir(), "state.json")
	now := time.Now().UTC()
	state := persistedState{UpdatedAt: now, Apps: map[string]persistedApp{
		"test-app": {
			State:   StateStopped,
			Desired: StateRunning,
			Message: "stopped unexpectedly during restart",
		},
	}}
	b, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, b, 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager := NewManager(config.SupervisordConfig{
		StatePath:                statePath,
		HeartbeatIntervalSeconds: 1,
		ShutdownTimeoutSeconds:   2,
		HealthdURL:               "",
		Apps: []config.SupervisorAppConfig{{
			Name:               "test-app",
			Type:               "test",
			Command:            script,
			AutoStart:          false,
			Restart:            "on_failure",
			ShutdownTimeoutSec: 2,
		}},
	}, nil)

	manager.Start(ctx)
	t.Cleanup(func() { _ = manager.StopApp(context.Background(), "test-app") })
	running := waitForAppState(t, manager, "test-app", StateRunning)
	if running.PID == 0 || running.Desired != StateRunning {
		t.Fatalf("status = %#v, want running desired process", running)
	}
}

func TestManagerReconcilesStoppedDesiredRunningProcess(t *testing.T) {
	script := filepath.Join(t.TempDir(), "app.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntrap 'exit 0' TERM INT\nwhile true; do sleep 1; done\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(config.SupervisordConfig{
		HeartbeatIntervalSeconds: 1,
		ShutdownTimeoutSeconds:   2,
		HealthdURL:               "",
		Apps: []config.SupervisorAppConfig{{
			Name:               "test-app",
			Type:               "test",
			Command:            script,
			Restart:            "on_failure",
			ShutdownTimeoutSec: 2,
		}},
	}, nil)

	if err := manager.StartApp(context.Background(), "test-app"); err != nil {
		t.Fatal(err)
	}
	first := waitForAppState(t, manager, "test-app", StateRunning)
	if err := manager.stopApp(context.Background(), "test-app", StateRunning); err != nil {
		t.Fatal(err)
	}
	stopped := waitForAppState(t, manager, "test-app", StateStopped)
	if stopped.Desired != StateRunning {
		t.Fatalf("status = %#v, want stopped app still desired running", stopped)
	}

	manager.reconcileDesiredState(context.Background())
	t.Cleanup(func() { _ = manager.StopApp(context.Background(), "test-app") })
	recovered := waitForAppState(t, manager, "test-app", StateRunning)
	if recovered.PID == 0 || recovered.PID == first.PID {
		t.Fatalf("status = %#v, want recovered process with new pid", recovered)
	}
}

func TestManagerRestartsStaleRunningPID(t *testing.T) {
	script := filepath.Join(t.TempDir(), "app.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntrap 'exit 0' TERM INT\nwhile true; do sleep 1; done\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(config.SupervisordConfig{
		ShutdownTimeoutSeconds: 2,
		Apps: []config.SupervisorAppConfig{{
			Name:               "test-app",
			Type:               "test",
			Command:            script,
			Restart:            "always",
			ShutdownTimeoutSec: 2,
		}},
	}, nil)
	manager.mu.Lock()
	manager.apps["test-app"].status.State = StateRunning
	manager.apps["test-app"].status.Desired = StateRunning
	manager.apps["test-app"].status.PID = 99999999
	manager.mu.Unlock()

	if err := manager.StartApp(context.Background(), "test-app"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.StopApp(context.Background(), "test-app") })
	running := waitForAppState(t, manager, "test-app", StateRunning)
	if running.PID == 0 || running.PID == 99999999 {
		t.Fatalf("status = %#v, want restarted app with live pid", running)
	}
}

func TestManagerAdoptsExistingProcessByPID(t *testing.T) {
	script := filepath.Join(t.TempDir(), "app.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntrap 'exit 0' TERM INT\nwhile true; do sleep 1; done\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	process, err := os.StartProcess(script, []string{script}, &os.ProcAttr{
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
		Sys:   &syscall.SysProcAttr{Setpgid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	waited := make(chan error, 1)
	go func() {
		_, err := process.Wait()
		waited <- err
	}()
	defer func() { _ = process.Signal(os.Kill) }()
	manager := NewManager(config.SupervisordConfig{
		ShutdownTimeoutSeconds: 2,
		Apps: []config.SupervisorAppConfig{{
			Name:               "test-app",
			Type:               "test",
			Command:            script,
			Restart:            "on_failure",
			ShutdownTimeoutSec: 2,
		}},
	}, nil)

	if err := manager.AdoptApp("test-app", process.Pid); err != nil {
		t.Fatal(err)
	}
	status := manager.Snapshot().Apps[0]
	if status.State != StateRunning || status.PID != process.Pid {
		t.Fatalf("status = %#v, want adopted running app", status)
	}
	if err := manager.StopApp(context.Background(), "test-app"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-waited:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("pid %d should be stopped", process.Pid)
	}
}

func TestManagerHealthRecoveryRestartsTrackedProcessGroup(t *testing.T) {
	script := filepath.Join(t.TempDir(), "app.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntrap 'sleep 1; exit 0' TERM INT\nwhile true; do sleep 1; done\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(config.SupervisordConfig{
		ShutdownTimeoutSeconds: 2,
		Apps: []config.SupervisorAppConfig{{
			Name:               "dashboard",
			Type:               "web",
			Command:            script,
			Restart:            "on_failure",
			HealthURL:          "http://dashboard.test/chat",
			ShutdownTimeoutSec: 2,
		}},
	}, nil)
	manager.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("health check failed")
	})}

	if err := manager.StartApp(context.Background(), "dashboard"); err != nil {
		t.Fatal(err)
	}
	first := waitForAppState(t, manager, "dashboard", StateRunning)
	if first.PID == 0 {
		t.Fatal("dashboard did not start")
	}

	manager.checkAppHealth(context.Background())
	restarting := manager.Snapshot().Apps[0]
	if restarting.State != StateStopping || restarting.PID != first.PID {
		t.Fatalf("status = %#v, want health recovery to keep tracked pid while stopping", restarting)
	}
	recovered := waitForAppState(t, manager, "dashboard", StateRunning)
	t.Cleanup(func() { _ = manager.StopApp(context.Background(), "dashboard") })
	if recovered.PID == 0 || recovered.PID == first.PID {
		t.Fatalf("status = %#v, want restarted dashboard with a new pid", recovered)
	}
	if processAlive(first.PID) {
		t.Fatalf("old dashboard pid %d should have been stopped before restart", first.PID)
	}
}

func TestManagerStartAppDoesNotLaunchSecondProcessWhileStopping(t *testing.T) {
	script := filepath.Join(t.TempDir(), "app.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nwhile true; do sleep 1; done\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(config.SupervisordConfig{
		Apps: []config.SupervisorAppConfig{{
			Name:    "dashboard",
			Type:    "web",
			Command: script,
			Restart: "on_failure",
		}},
	}, nil)
	manager.mu.Lock()
	manager.apps["dashboard"].status.State = StateStopping
	manager.apps["dashboard"].status.Desired = StateRunning
	manager.apps["dashboard"].status.PID = 12345
	manager.mu.Unlock()

	if err := manager.StartApp(context.Background(), "dashboard"); err != nil {
		t.Fatal(err)
	}
	status := manager.Snapshot().Apps[0]
	if status.State != StateStopping || status.PID != 12345 || status.Message != "already stopping" {
		t.Fatalf("status = %#v, want no second process while stopping", status)
	}
}

func waitForAppState(t *testing.T, manager *Manager, name string, state string) AppStatus {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, app := range manager.Snapshot().Apps {
			if app.Name == name && app.State == state {
				return app
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("app %s did not reach state %s; snapshot = %#v", name, state, manager.Snapshot())
	return AppStatus{}
}
