package externalagent

import (
	"context"
	"encoding/json"

	agentrunner "github.com/andrewneudegg/lab/pkg/externalagent"
	"github.com/andrewneudegg/lab/pkg/tool"
)

func Register(reg *tool.Registry, runner *agentrunner.Runner) error {
	for _, t := range []tool.Tool{ListTool{runner: runner}, DelegateTool{runner: runner}} {
		if err := reg.Register(t); err != nil {
			return err
		}
	}
	return nil
}

type ListTool struct {
	runner *agentrunner.Runner
}

func (ListTool) Name() string        { return "agent.list" }
func (ListTool) Description() string { return "List configured external worker agent backends." }
func (ListTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (ListTool) Risk() tool.RiskLevel { return tool.RiskReadOnly }
func (t ListTool) Run(context.Context, json.RawMessage) (json.RawMessage, error) {
	return json.Marshal(map[string]any{"agents": t.runner.List()})
}

type DelegateTool struct {
	runner *agentrunner.Runner
}

func (DelegateTool) Name() string { return "agent.delegate" }
func (DelegateTool) Description() string {
	return "Delegate explicit work to an external CLI agent in a task workspace."
}
func (DelegateTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","required":["backend","task_id","workspace","instruction"],"properties":{"backend":{"type":"string"},"task_id":{"type":"string"},"workspace":{"type":"string"},"instruction":{"type":"string"}}}`)
}
func (DelegateTool) Risk() tool.RiskLevel { return tool.RiskMedium }
func (t DelegateTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req agentrunner.RunRequest
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	result, err := t.runner.Run(ctx, req)
	b, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, marshalErr
	}
	return b, err
}
