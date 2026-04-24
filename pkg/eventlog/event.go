package eventlog

import (
	"encoding/json"
	"time"
)

type Event struct {
	ID       string          `json:"id"`
	Time     time.Time       `json:"time"`
	Type     string          `json:"type"`
	Actor    string          `json:"actor"`
	TaskID   string          `json:"task_id,omitempty"`
	ParentID string          `json:"parent_id,omitempty"`
	Payload  json.RawMessage `json:"payload"`
}

func Payload(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{"error":"marshal payload failed"}`)
	}
	return b
}
