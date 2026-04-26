package supervisor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/andrewneudegg/lab/pkg/config"
)

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
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/healthd/processes/heartbeat" {
			t.Fatalf("path = %q", req.URL.Path)
		}
		heartbeat <- struct{}{}
		rw.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager := NewManager(config.SupervisordConfig{
		HealthdURL:               server.URL,
		HeartbeatIntervalSeconds: 1,
	}, nil)

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
