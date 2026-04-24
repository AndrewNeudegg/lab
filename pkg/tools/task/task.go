package task

import (
	"context"
	"encoding/json"
	"sort"

	taskstore "github.com/andrewneudegg/lab/pkg/task"
	"github.com/andrewneudegg/lab/pkg/tool"
)

func Register(reg *tool.Registry, store *taskstore.Store) error {
	return reg.Register(ListTool{store: store})
}

type ListTool struct {
	store *taskstore.Store
}

func (ListTool) Name() string        { return "task.list" }
func (ListTool) Description() string { return "List durable task records." }
func (ListTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (ListTool) Risk() tool.RiskLevel { return tool.RiskReadOnly }
func (t ListTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	tasks, err := t.store.List()
	if err != nil {
		return nil, err
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].CreatedAt.After(tasks[j].CreatedAt) })
	return json.Marshal(map[string]any{"tasks": tasks})
}
