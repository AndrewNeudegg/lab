package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/andrewneudegg/lab/pkg/eventlog"
	"github.com/andrewneudegg/lab/pkg/id"
)

func (o *Orchestrator) recordToolCheckpoint(ctx context.Context, actor, taskID, name string, raw json.RawMessage) {
	sum := sha256.Sum256(bytes.TrimSpace(raw))
	_ = o.events.Append(ctx, eventlog.Event{
		ID:     id.New("evt"),
		Type:   "tool.call.checkpoint",
		Actor:  actor,
		TaskID: taskID,
		Payload: eventlog.Payload(map[string]any{
			"tool":        name,
			"args_sha256": hex.EncodeToString(sum[:]),
		}),
	})
}
