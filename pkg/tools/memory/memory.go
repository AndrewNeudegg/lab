package memory

import (
	"context"
	"encoding/json"
	"fmt"

	memstore "github.com/andrewneudegg/lab/pkg/memory"
	"github.com/andrewneudegg/lab/pkg/tool"
)

type Toolset struct {
	store *memstore.Store
}

func Register(reg *tool.Registry, store *memstore.Store) error {
	for _, t := range []tool.Tool{ReadTool{store: store}, ProposeWriteTool{store: store}, CommitWriteTool{store: store}} {
		if err := reg.Register(t); err != nil {
			return err
		}
	}
	return nil
}

func schema(v string) json.RawMessage { return json.RawMessage(v) }

type ReadTool struct{ store *memstore.Store }

func (ReadTool) Name() string        { return "memory.read" }
func (ReadTool) Description() string { return "Read a markdown memory file." }
func (ReadTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["name"],"properties":{"name":{"type":"string"}}}`)
}
func (ReadTool) Risk() tool.RiskLevel { return tool.RiskReadOnly }
func (t ReadTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	content, err := t.store.Read(req.Name)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"name": req.Name, "content": content})
}

type ProposeWriteTool struct{ store *memstore.Store }

func (ProposeWriteTool) Name() string { return "memory.propose_write" }
func (ProposeWriteTool) Description() string {
	return "Write a memory proposal file; it does not commit memory."
}
func (ProposeWriteTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["name","content"],"properties":{"name":{"type":"string"},"content":{"type":"string"}}}`)
}
func (ProposeWriteTool) Risk() tool.RiskLevel { return tool.RiskLow }
func (t ProposeWriteTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	path, err := t.store.ProposeWrite(req.Name, req.Content)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"proposal": path})
}

type CommitWriteTool struct{ store *memstore.Store }

func (CommitWriteTool) Name() string        { return "memory.commit_write" }
func (CommitWriteTool) Description() string { return "Commit a memory write after approval." }
func (CommitWriteTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["name","content","target"],"properties":{"name":{"type":"string"},"content":{"type":"string"},"target":{"type":"string"}}}`)
}
func (CommitWriteTool) Risk() tool.RiskLevel { return tool.RiskHigh }
func (t CommitWriteTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if err := t.store.CommitWrite(req.Name, req.Content); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"committed": req.Name})
}
