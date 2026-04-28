package eventlog

import (
	"context"
	"encoding/json"
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
