package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/andrewneudegg/lab/pkg/llm"
)

type providerQualityStats struct {
	Provider           string
	Model              string
	ModelTurns         int
	SchemaRejections   int
	SemanticRejections int
	OtherRejections    int
	Usage              llm.Usage
}

func (o *Orchestrator) providerQualitySummary(day time.Time) (string, error) {
	events, err := o.events.ReadDay(day)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "No LLM quality events for " + day.Format("2006-01-02") + ".", nil
		}
		return "", err
	}
	stats := map[string]*providerQualityStats{}
	validatorDenials := 0
	for _, event := range events {
		switch event.Type {
		case "agent.message":
			var payload struct {
				Provider string    `json:"provider"`
				Model    string    `json:"model"`
				Usage    llm.Usage `json:"usage"`
			}
			if json.Unmarshal(event.Payload, &payload) != nil {
				continue
			}
			key := providerQualityKey(payload.Provider, payload.Model)
			item := stats[key]
			if item == nil {
				item = &providerQualityStats{Provider: qualityLabel(payload.Provider), Model: qualityLabel(payload.Model)}
				stats[key] = item
			}
			item.ModelTurns++
			item.Usage.InputTokens += payload.Usage.InputTokens
			item.Usage.OutputTokens += payload.Usage.OutputTokens
			item.Usage.TotalTokens += payload.Usage.TotalTokens
		case "agent.response.rejected":
			var payload struct {
				Provider string `json:"provider"`
				Model    string `json:"model"`
				Stage    string `json:"stage"`
			}
			if json.Unmarshal(event.Payload, &payload) != nil {
				continue
			}
			key := providerQualityKey(payload.Provider, payload.Model)
			item := stats[key]
			if item == nil {
				item = &providerQualityStats{Provider: qualityLabel(payload.Provider), Model: qualityLabel(payload.Model)}
				stats[key] = item
			}
			switch strings.TrimSpace(payload.Stage) {
			case "schema":
				item.SchemaRejections++
			case "semantic":
				item.SemanticRejections++
			default:
				item.OtherRejections++
			}
		case "tool.call.denied":
			if event.Actor != "validator" {
				continue
			}
			validatorDenials++
		}
	}
	if len(stats) == 0 && validatorDenials == 0 {
		return "No LLM quality events for " + day.Format("2006-01-02") + ".", nil
	}
	items := make([]*providerQualityStats, 0, len(stats))
	for _, item := range stats {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Provider != items[j].Provider {
			return items[i].Provider < items[j].Provider
		}
		return items[i].Model < items[j].Model
	})
	var b strings.Builder
	fmt.Fprintf(&b, "LLM quality for %s:\n", day.Format("2006-01-02"))
	for _, item := range items {
		fmt.Fprintf(&b, "- %s/%s: turns=%d rejected_schema=%d rejected_semantic=%d rejected_other=%d tokens=%d/%d/%d\n",
			item.Provider,
			item.Model,
			item.ModelTurns,
			item.SchemaRejections,
			item.SemanticRejections,
			item.OtherRejections,
			item.Usage.InputTokens,
			item.Usage.OutputTokens,
			item.Usage.TotalTokens,
		)
	}
	if len(items) == 0 && validatorDenials > 0 {
		fmt.Fprintf(&b, "- no provider turn payloads\n")
	}
	if validatorDenials > 0 {
		fmt.Fprintf(&b, "Tool argument validator denials: %d\n", validatorDenials)
	}
	return strings.TrimSpace(b.String()), nil
}

func providerQualityKey(provider, model string) string {
	return qualityLabel(provider) + "\x00" + qualityLabel(model)
}

func qualityLabel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return value
}
