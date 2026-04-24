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

type Call struct {
	Tool string          `json:"tool"`
	Args json.RawMessage `json:"args"`
}
