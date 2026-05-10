package eventlog

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadDayHandlesLargeEvents(t *testing.T) {
	store := NewStore(t.TempDir())
	day := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	messageLength := 9 * 1024 * 1024
	payload := Payload(map[string]any{"message": strings.Repeat("x", messageLength)})
	event := Event{
		ID:      "evt_large",
		Time:    day,
		Type:    "agent.message",
		Actor:   "CoderAgent",
		Payload: payload,
	}
	if err := store.Append(context.Background(), event); err != nil {
		t.Fatal(err)
	}

	events, err := store.ReadDay(day)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	var got struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(events[0].Payload, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Message) != messageLength {
		t.Fatalf("message length = %d, want %d", len(got.Message), messageLength)
	}
}

func TestDeleteMatchingRewritesOnlyMatchingEvents(t *testing.T) {
	store := NewStore(t.TempDir())
	day := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	events := []Event{
		{ID: "keep", Time: day, Type: "task.created", Actor: "test", Payload: Payload(map[string]any{"message": "keep"})},
		{ID: "drop", Time: day, Type: "user.message", Actor: "test", Payload: Payload(map[string]any{"message": "drop"})},
		{ID: "keep-chat", Time: day, Type: "chat.reply", Actor: "test", Payload: Payload(map[string]any{"message": "keep", "conversation_id": "other"})},
	}
	for _, event := range events {
		if err := store.Append(context.Background(), event); err != nil {
			t.Fatal(err)
		}
	}

	removed, err := store.DeleteMatching(context.Background(), func(event Event) bool {
		return event.Type == "user.message"
	})
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}

	got, err := store.ReadDay(day)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("events = %d, want 2", len(got))
	}
	if got[0].ID != "keep" || got[1].ID != "keep-chat" {
		t.Fatalf("events = %#v, want kept records in order", got)
	}
}

func TestReadDayTailReturnsRecentEventsInOrder(t *testing.T) {
	store := NewStore(t.TempDir())
	day := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	for _, event := range []Event{
		{ID: "evt_1", Time: day, Type: "one", Actor: "test", Payload: Payload(map[string]any{"message": "one"})},
		{ID: "evt_2", Time: day, Type: "two", Actor: "test", Payload: Payload(map[string]any{"message": "two"})},
		{ID: "evt_3", Time: day, Type: "three", Actor: "test", Payload: Payload(map[string]any{"message": "three"})},
		{ID: "evt_4", Time: day, Type: "four", Actor: "test", Payload: Payload(map[string]any{"message": "four"})},
	} {
		if err := store.Append(context.Background(), event); err != nil {
			t.Fatal(err)
		}
	}

	got, err := store.ReadDayTail(day, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "evt_3" || got[1].ID != "evt_4" {
		t.Fatalf("tail = %#v, want last two events in chronological order", got)
	}
}

func TestReadDayTailHandlesFileWithoutTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	day := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	first, err := json.Marshal(Event{ID: "evt_1", Time: day, Type: "one", Actor: "test", Payload: Payload(map[string]any{"message": "one"})})
	if err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(Event{ID: "evt_2", Time: day, Type: "two", Actor: "test", Payload: Payload(map[string]any{"message": "two"})})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "2026-04-25.jsonl"), append(append(first, '\n'), second...), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := store.ReadDayTail(day, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "evt_2" {
		t.Fatalf("tail = %#v, want final event without trailing newline", got)
	}
}
