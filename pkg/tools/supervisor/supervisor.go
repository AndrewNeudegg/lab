package supervisor

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/andrewneudegg/lab/pkg/tool"
)

func Register(reg *tool.Registry) error {
	for _, t := range []tool.Tool{StatusTool{}, RestartTool{}, ReloadConfigTool{}} {
		if err := reg.Register(t); err != nil {
			return err
		}
	}
	return nil
}

func schema(v string) json.RawMessage { return json.RawMessage(v) }

type StatusTool struct{}

func (StatusTool) Name() string        { return "service.status" }
func (StatusTool) Description() string { return "Report service status; scaffolded for v0.1." }
func (StatusTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["service"],"properties":{"service":{"type":"string"}}}`)
}
func (StatusTool) Risk() tool.RiskLevel { return tool.RiskReadOnly }
func (StatusTool) Run(context.Context, json.RawMessage) (json.RawMessage, error) {
	return json.Marshal(map[string]any{"status": "unknown", "note": "service status is not implemented in v0.1"})
}

type RestartTool struct{}

func (RestartTool) Name() string        { return "service.restart" }
func (RestartTool) Description() string { return "Restart an approved service; disabled in v0.1." }
func (RestartTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["service","target"],"properties":{"service":{"type":"string"},"target":{"type":"string"}}}`)
}
func (RestartTool) Risk() tool.RiskLevel { return tool.RiskHigh }
func (RestartTool) Run(context.Context, json.RawMessage) (json.RawMessage, error) {
	return nil, fmt.Errorf("service restart is not implemented in v0.1")
}

type ReloadConfigTool struct{}

func (ReloadConfigTool) Name() string { return "service.reload_config" }
func (ReloadConfigTool) Description() string {
	return "Reload service configuration; disabled in v0.1."
}
func (ReloadConfigTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["service","target"],"properties":{"service":{"type":"string"},"target":{"type":"string"}}}`)
}
func (ReloadConfigTool) Risk() tool.RiskLevel { return tool.RiskHigh }
func (ReloadConfigTool) Run(context.Context, json.RawMessage) (json.RawMessage, error) {
	return nil, fmt.Errorf("service reload is not implemented in v0.1")
}
