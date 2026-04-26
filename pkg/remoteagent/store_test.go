package remoteagent

import (
	"errors"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestStoreUpsertHeartbeatNormalizesAgent(t *testing.T) {
	store := NewStore(t.TempDir())
	now := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	started := now.Add(-time.Hour)

	agent, err := store.UpsertHeartbeat(Heartbeat{
		ID:        " desk ",
		Name:      " Desk ",
		Machine:   " workstation ",
		Version:   "v1",
		StartedAt: started,
		Capabilities: []string{
			"codex",
			"codex",
			" ",
			"directory-context",
		},
		Workdirs: []Workdir{
			{ID: "repo", Path: "/srv/repo", Label: " Repo "},
			{ID: "repo", Path: "/srv/repo", Label: "duplicate"},
			{Path: "/srv/other"},
			{ID: "empty"},
		},
		CurrentTaskID: " task_1 ",
		Metadata:      map[string]string{"zone": "office"},
	}, now)
	if err != nil {
		t.Fatal(err)
	}

	if agent.ID != "desk" {
		t.Fatalf("agent id = %q, want trimmed stable id", agent.ID)
	}
	if agent.Name != "Desk" || agent.Machine != "workstation" || agent.Status != StatusOnline {
		t.Fatalf("agent identity = %#v, want trimmed online agent", agent)
	}
	if !agent.LastSeen.Equal(now) || !agent.StartedAt.Equal(started) {
		t.Fatalf("agent times = last %s started %s", agent.LastSeen, agent.StartedAt)
	}
	if !reflect.DeepEqual(agent.Capabilities, []string{"codex", "directory-context"}) {
		t.Fatalf("capabilities = %#v", agent.Capabilities)
	}
	if len(agent.Workdirs) != 2 {
		t.Fatalf("workdirs = %#v, want deduped non-empty paths", agent.Workdirs)
	}
	if agent.Workdirs[1].ID != "/srv/other" {
		t.Fatalf("second workdir id = %q, want path fallback", agent.Workdirs[1].ID)
	}
	if agent.CurrentTaskID != "task_1" {
		t.Fatalf("current task = %q", agent.CurrentTaskID)
	}

	loaded, err := store.Load("desk")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Name != agent.Name {
		t.Fatalf("loaded = %#v, want saved agent", loaded)
	}
}

func TestStoreListMarksStaleAgentsOfflineAndSortsOnlineFirst(t *testing.T) {
	store := NewStore(t.TempDir())
	now := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	if _, err := store.UpsertHeartbeat(Heartbeat{ID: "stale", Name: "Stale"}, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertHeartbeat(Heartbeat{ID: "fresh", Name: "Fresh"}, now); err != nil {
		t.Fatal(err)
	}

	agents, err := store.List(30*time.Second, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 2 {
		t.Fatalf("agent count = %d, want 2", len(agents))
	}
	if agents[0].ID != "fresh" || agents[0].Status != StatusOnline {
		t.Fatalf("first agent = %#v, want fresh online first", agents[0])
	}
	if agents[1].ID != "stale" || agents[1].Status != StatusOffline {
		t.Fatalf("second agent = %#v, want stale offline", agents[1])
	}
}

func TestStoreCurrentTaskLifecycle(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.SetCurrentTask("missing", "task_1"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("SetCurrentTask missing error = %v, want os.ErrNotExist", err)
	}
	if _, err := store.UpsertHeartbeat(Heartbeat{ID: "desk"}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := store.SetCurrentTask("desk", "task_1"); err != nil {
		t.Fatal(err)
	}
	agent, err := store.Load("desk")
	if err != nil {
		t.Fatal(err)
	}
	if agent.CurrentTaskID != "task_1" {
		t.Fatalf("current task = %q, want task_1", agent.CurrentTaskID)
	}
	if err := store.ClearCurrentTask("desk", "other"); err != nil {
		t.Fatal(err)
	}
	agent, _ = store.Load("desk")
	if agent.CurrentTaskID != "task_1" {
		t.Fatalf("current task cleared by wrong task id: %#v", agent)
	}
	if err := store.ClearCurrentTask("desk", "task_1"); err != nil {
		t.Fatal(err)
	}
	agent, _ = store.Load("desk")
	if agent.CurrentTaskID != "" {
		t.Fatalf("current task = %q, want cleared", agent.CurrentTaskID)
	}
}

func TestStoreRejectsMissingAgentID(t *testing.T) {
	store := NewStore(t.TempDir())
	if _, err := store.UpsertHeartbeat(Heartbeat{Name: "bad"}, time.Now().UTC()); err == nil {
		t.Fatal("UpsertHeartbeat missing id succeeded, want error")
	}
}
