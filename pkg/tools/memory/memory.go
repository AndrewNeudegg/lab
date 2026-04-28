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
	for _, t := range []tool.Tool{
		ListTool{store: store},
		ReadTool{store: store},
		RememberTool{store: store},
		UnlearnTool{store: store},
		ProposeWriteTool{store: store},
		CommitWriteTool{store: store},
	} {
		if err := reg.Register(t); err != nil {
			return err
		}
	}
	return nil
}

func schema(v string) json.RawMessage { return json.RawMessage(v) }

type ListTool struct{ store *memstore.Store }

func (ListTool) Name() string { return "memory.list" }
func (ListTool) Description() string {
	return "List markdown memory files and durable interaction lessons."
}
func (ListTool) Schema() json.RawMessage {
	return schema(`{"type":"object","properties":{"name":{"type":"string","description":"Optional markdown memory file for lesson entries; defaults to user.md."}}}`)
}
func (ListTool) Risk() tool.RiskLevel { return tool.RiskReadOnly }
func (t ListTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Name string `json:"name"`
	}
	if len(input) > 0 {
		if err := json.Unmarshal(input, &req); err != nil {
			return nil, err
		}
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	files, err := t.store.List()
	if err != nil {
		return nil, err
	}
	lessons, err := t.store.ListLessons(req.Name)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"files": files, "lessons": lessons})
}

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

type RememberTool struct{ store *memstore.Store }

func (RememberTool) Name() string { return "memory.remember" }
func (RememberTool) Description() string {
	return "Store a distilled durable interaction lesson or user preference."
}
func (RememberTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["content"],"properties":{"content":{"type":"string","description":"Short distilled lesson for future decisions, not a transcript."},"kind":{"type":"string","description":"Optional category such as preference, procedure, principle, or fact."},"source":{"type":"string","description":"Optional source label; defaults to chat."},"name":{"type":"string","description":"Optional markdown memory file; defaults to user.md."}}}`)
}
func (RememberTool) Risk() tool.RiskLevel { return tool.RiskLow }
func (t RememberTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Name    string `json:"name"`
		Content string `json:"content"`
		Kind    string `json:"kind"`
		Source  string `json:"source"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	lesson, err := t.store.RememberLesson(req.Name, memstore.Lesson{
		Content: req.Content,
		Kind:    req.Kind,
		Source:  req.Source,
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"remembered": lesson})
}

type UnlearnTool struct{ store *memstore.Store }

func (UnlearnTool) Name() string { return "memory.unlearn" }
func (UnlearnTool) Description() string {
	return "Remove durable interaction lessons by id or matching text."
}
func (UnlearnTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["selector"],"properties":{"selector":{"type":"string","description":"Memory id or distinctive text to remove."},"name":{"type":"string","description":"Optional markdown memory file; defaults to user.md."}}}`)
}
func (UnlearnTool) Risk() tool.RiskLevel { return tool.RiskLow }
func (t UnlearnTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Name     string `json:"name"`
		Selector string `json:"selector"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	removed, err := t.store.UnlearnLesson(req.Name, req.Selector)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"removed": removed})
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
