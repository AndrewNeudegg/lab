package task

import (
	"testing"
	"time"
)

func TestStoreDeleteRemovesTask(t *testing.T) {
	store := NewStore(t.TempDir())
	task := Task{ID: "task_test", Title: "test"}
	if err := store.Save(task); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(task.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(task.ID); err == nil {
		t.Fatalf("expected deleted task load to fail")
	}
}

func TestStoreTracksRunTimestamps(t *testing.T) {
	store := NewStore(t.TempDir())
	task := Task{ID: "task_running", Title: "test", Status: StatusRunning}
	beforeStart := time.Now().UTC()
	if err := store.Save(task); err != nil {
		t.Fatal(err)
	}
	running, err := store.Load(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	afterStart := time.Now().UTC()
	if running.StartedAt == nil {
		t.Fatal("expected running task to have started_at")
	}
	if running.StartedAt.Before(beforeStart) || running.StartedAt.After(afterStart) {
		t.Fatalf("started_at = %v, want between %v and %v", running.StartedAt, beforeStart, afterStart)
	}
	if running.StoppedAt != nil {
		t.Fatalf("stopped_at = %v, want nil while running", running.StoppedAt)
	}

	running.Status = StatusReadyForReview
	if err := store.Save(running); err != nil {
		t.Fatal(err)
	}
	stopped, err := store.Load(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.StartedAt == nil || !stopped.StartedAt.Equal(*running.StartedAt) {
		t.Fatalf("started_at = %v, want preserved %v", stopped.StartedAt, running.StartedAt)
	}
	if stopped.StoppedAt == nil {
		t.Fatal("expected stopped task to have stopped_at")
	}
	if stopped.StoppedAt.Before(*stopped.StartedAt) {
		t.Fatalf("stopped_at = %v before started_at = %v", stopped.StoppedAt, stopped.StartedAt)
	}
}

func TestStorePersistsAttachments(t *testing.T) {
	store := NewStore(t.TempDir())
	task := Task{
		ID:    "task_with_attachment",
		Title: "test",
		Attachments: []Attachment{{
			ID:          "att_1",
			Name:        "browser-context.json",
			ContentType: "application/json",
			Size:        17,
			Text:        `{"url":"/chat"}`,
		}},
	}
	if err := store.Save(task); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Attachments) != 1 {
		t.Fatalf("attachments = %d, want 1", len(loaded.Attachments))
	}
	if loaded.Attachments[0].Name != "browser-context.json" || loaded.Attachments[0].Text == "" {
		t.Fatalf("attachment = %#v, want stored context attachment", loaded.Attachments[0])
	}
}
