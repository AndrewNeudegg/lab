package tool

import (
	"context"
	"encoding/json"
)

type RiskLevel string

const (
	RiskReadOnly RiskLevel = "read_only"
	RiskLow      RiskLevel = "low"
	RiskMedium   RiskLevel = "medium"
	RiskHigh     RiskLevel = "high"
	RiskCritical RiskLevel = "critical"
)

type Tool interface {
	Name() string
	Description() string
	Schema() json.RawMessage
	Risk() RiskLevel
	Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error)
}

type InputRiskTool interface {
	RiskFor(input json.RawMessage) RiskLevel
}

func EffectiveRisk(t Tool, input json.RawMessage) RiskLevel {
	if t == nil {
		return ""
	}
	if rt, ok := t.(InputRiskTool); ok {
		return rt.RiskFor(input)
	}
	return t.Risk()
}

type Call struct {
	Tool string          `json:"tool"`
	Args json.RawMessage `json:"args"`
}
