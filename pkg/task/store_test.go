package task

import (
	"errors"
	"os"
	"strings"
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

func TestStoreListSummariesOmitsHeavyFields(t *testing.T) {
	store := NewStore(t.TempDir())
	task := Task{
		ID:         "task_heavy",
		Title:      "heavy",
		Goal:       strings.Repeat("goal ", 400),
		Result:     strings.Repeat("result ", 400),
		RemoteDiff: strings.Repeat("diff", 5000),
		Attachments: []Attachment{{
			ID:   "att_1",
			Name: "context.txt",
			Text: strings.Repeat("attachment", 5000),
		}},
		DiffSnapshot: &TaskDiffSnapshot{
			RawDiff: strings.Repeat("raw", 5000),
			Files:   make([]TaskDiffSnapshotFile, 100),
		},
	}
	if err := store.Save(task); err != nil {
		t.Fatal(err)
	}

	summaries, err := store.ListSummaries()
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("summaries = %d, want 1", len(summaries))
	}
	summary := summaries[0]
	if !summary.SummaryOnly {
		t.Fatal("summary_only = false, want true")
	}
	if summary.RemoteDiff != "" || summary.DiffSnapshot == nil || summary.DiffSnapshot.RawDiff != "" {
		t.Fatalf("summary kept heavy diff fields: %#v", summary)
	}
	if len(summary.DiffSnapshot.Files) != 80 {
		t.Fatalf("summary diff files = %d, want 80", len(summary.DiffSnapshot.Files))
	}
	if len(summary.Attachments) != 1 || summary.Attachments[0].Text != "" {
		t.Fatalf("summary attachment = %#v, want metadata without text", summary.Attachments)
	}

	loaded, err := store.Load(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SummaryOnly || loaded.RemoteDiff == "" || loaded.DiffSnapshot.RawDiff == "" || loaded.Attachments[0].Text == "" {
		t.Fatalf("full task lost detail: %#v", loaded)
	}
}

func TestStoreListSummariesBackfillsMissingIndex(t *testing.T) {
	store := NewStore(t.TempDir())
	task := Task{ID: "task_legacy", Title: "legacy", RemoteDiff: strings.Repeat("diff", 1000)}
	if err := store.Save(task); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(store.summaryPath(task.ID)); err != nil {
		t.Fatal(err)
	}

	summaries, err := store.ListSummaries()
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || !summaries[0].SummaryOnly || summaries[0].RemoteDiff != "" {
		t.Fatalf("summaries = %#v, want regenerated lightweight summary", summaries)
	}
	if _, err := os.Stat(store.summaryPath(task.ID)); err != nil {
		t.Fatalf("summary index was not regenerated: %v", err)
	}
}

func TestStoreListSummariesDoesNotWaitBehindConcurrentReaders(t *testing.T) {
	store := NewStore(t.TempDir())
	task := Task{ID: "task_fresh", Title: "fresh summary", RemoteDiff: strings.Repeat("diff", 1000)}
	if err := store.Save(task); err != nil {
		t.Fatal(err)
	}

	store.mu.RLock()
	defer store.mu.RUnlock()

	done := make(chan error, 1)
	go func() {
		summaries, err := store.ListSummaries()
		if err != nil {
			done <- err
			return
		}
		if len(summaries) != 1 || summaries[0].ID != task.ID || !summaries[0].SummaryOnly {
			done <- errors.New("fresh summaries were not returned while another reader held the store")
			return
		}
		done <- nil
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("ListSummaries waited behind another reader")
	}
}
