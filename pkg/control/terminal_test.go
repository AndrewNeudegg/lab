package control

import (
	"strings"
	"testing"
	"time"
)

func TestTerminalSessionRunsShellCommands(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	manager := newTerminalManager()
	session, err := manager.create("")
	if err != nil {
		t.Fatalf("create terminal session: %v", err)
	}
	defer manager.close(session.id)

	events := session.subscribe()
	defer session.unsubscribe(events)

	if err := session.write("printf web-terminal-ok\nexit\n"); err != nil {
		t.Fatalf("write terminal input: %v", err)
	}

	deadline := time.After(5 * time.Second)
	var output strings.Builder
	for {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatalf("terminal event stream closed before exit")
			}
			if event.Type == "output" {
				output.WriteString(event.Data)
			}
			if event.Type == "exit" {
				if !strings.Contains(output.String(), "web-terminal-ok") {
					t.Fatalf("terminal output missing command result: %q", output.String())
				}
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for terminal output: %q", output.String())
		}
	}
}
