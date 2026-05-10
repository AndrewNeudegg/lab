package control

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/andrewneudegg/lab/pkg/assistant"
	"github.com/andrewneudegg/lab/pkg/eventlog"
	knowledgestore "github.com/andrewneudegg/lab/pkg/knowledge"
	taskstore "github.com/andrewneudegg/lab/pkg/task"
)

const (
	listTextLimit       = 1200
	listShortTextLimit  = 320
	listPayloadLimit    = 2048
	listCollectionLimit = 12
)

func fullDetailRequested(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "full", "true", "1", "yes":
		return true
	default:
		return false
	}
}

func truncateListText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit < 1 || len(value) <= limit {
		return value
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "..."
}

func truncateListStrings(values []string, itemLimit, textLimit int) []string {
	if len(values) == 0 {
		return values
	}
	if itemLimit > 0 && len(values) > itemLimit {
		values = values[:itemLimit]
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := truncateListText(value, textLimit); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func summarizeTasksForList(tasks []taskstore.Task) []taskstore.Task {
	out := make([]taskstore.Task, len(tasks))
	for index, value := range tasks {
		out[index] = taskstore.SummarizeForList(value)
	}
	return out
}

func summarizeTaskForList(value taskstore.Task) taskstore.Task {
	return taskstore.SummarizeForList(value)
}

func summarizeAssistantRunsForList(runs []assistant.Run) []assistant.Run {
	return assistant.SummarizeRunsForList(runs)
}

func summarizeAssistantRunForList(value assistant.Run) assistant.Run {
	return assistant.SummarizeRunForList(value)
}

func summarizeRunFindings(values []assistant.RunFinding) []assistant.RunFinding {
	if len(values) > listCollectionLimit {
		values = values[:listCollectionLimit]
	}
	out := make([]assistant.RunFinding, len(values))
	for index, value := range values {
		value.Title = truncateListText(value.Title, listShortTextLimit)
		value.Detail = truncateListText(value.Detail, listTextLimit)
		out[index] = value
	}
	return out
}

func summarizeRunActions(values []assistant.RunAction) []assistant.RunAction {
	if len(values) > 24 {
		values = values[:24]
	}
	out := make([]assistant.RunAction, len(values))
	for index, value := range values {
		value.Title = truncateListText(value.Title, listShortTextLimit)
		value.Rationale = truncateListText(value.Rationale, listTextLimit)
		value.TaskGoal = truncateListText(value.TaskGoal, listTextLimit)
		value.KnowledgeQuery = truncateListText(value.KnowledgeQuery, listTextLimit)
		value.WorkflowHint = truncateListText(value.WorkflowHint, listTextLimit)
		if value.Contract != nil {
			contract := *value.Contract
			contract.Explanation = truncateListText(contract.Explanation, listTextLimit)
			contract.RequiredEvidence = truncateListStrings(contract.RequiredEvidence, listCollectionLimit, listShortTextLimit)
			contract.RequiredInputs = truncateListStrings(contract.RequiredInputs, listCollectionLimit, listShortTextLimit)
			value.Contract = &contract
		}
		if value.Plan != nil {
			plan := *value.Plan
			plan.Summary = truncateListText(plan.Summary, listTextLimit)
			plan.Blockers = truncateListStrings(plan.Blockers, listCollectionLimit, listShortTextLimit)
			if len(plan.Steps) > listCollectionLimit {
				plan.Steps = plan.Steps[:listCollectionLimit]
			}
			for stepIndex := range plan.Steps {
				plan.Steps[stepIndex].Title = truncateListText(plan.Steps[stepIndex].Title, listShortTextLimit)
			}
			if len(plan.Receipts) > listCollectionLimit {
				plan.Receipts = plan.Receipts[:listCollectionLimit]
			}
			for receiptIndex := range plan.Receipts {
				plan.Receipts[receiptIndex].Message = truncateListText(plan.Receipts[receiptIndex].Message, listShortTextLimit)
			}
			value.Plan = &plan
		}
		out[index] = value
	}
	return out
}

func summarizeRunReceipts(values []assistant.RunReceipt) []assistant.RunReceipt {
	if len(values) > listCollectionLimit {
		values = values[:listCollectionLimit]
	}
	out := make([]assistant.RunReceipt, len(values))
	for index, value := range values {
		value.Message = truncateListText(value.Message, listShortTextLimit)
		out[index] = value
	}
	return out
}

func summarizePolicyHints(values []assistant.RunPolicyHint) []assistant.RunPolicyHint {
	if len(values) > listCollectionLimit {
		values = values[:listCollectionLimit]
	}
	out := make([]assistant.RunPolicyHint, len(values))
	for index, value := range values {
		value.Reason = truncateListText(value.Reason, listShortTextLimit)
		out[index] = value
	}
	return out
}

func summarizeRunSnapshot(value assistant.RunSnapshot) assistant.RunSnapshot {
	value.Signals = nil
	value.Goals = nil
	value.AttentionTasks = summarizeRunObjectRefs(value.AttentionTasks)
	value.RecentWorkflows = summarizeRunObjectRefs(value.RecentWorkflows)
	value.KnowledgeSpaces = summarizeRunObjectRefs(value.KnowledgeSpaces)
	value.RecentEvents = summarizeRunEventRefs(value.RecentEvents)
	value.Health.Items = summarizeRunObjectRefs(value.Health.Items)
	value.Supervisor.Items = summarizeRunObjectRefs(value.Supervisor.Items)
	return value
}

func summarizeRunObjectRefs(values []assistant.RunObjectRef) []assistant.RunObjectRef {
	if len(values) > listCollectionLimit {
		values = values[:listCollectionLimit]
	}
	out := make([]assistant.RunObjectRef, len(values))
	for index, value := range values {
		value.Title = truncateListText(value.Title, listShortTextLimit)
		value.Summary = truncateListText(value.Summary, listShortTextLimit)
		out[index] = value
	}
	return out
}

func summarizeRunEventRefs(values []assistant.RunEventRef) []assistant.RunEventRef {
	if len(values) > listCollectionLimit {
		values = values[:listCollectionLimit]
	}
	out := make([]assistant.RunEventRef, len(values))
	for index, value := range values {
		value.Summary = truncateListText(value.Summary, listShortTextLimit)
		out[index] = value
	}
	return out
}

func summarizeKnowledgeSpacesForList(spaces []knowledgestore.Space) []knowledgestore.Space {
	out := make([]knowledgestore.Space, len(spaces))
	for index, value := range spaces {
		out[index] = summarizeKnowledgeSpaceForList(value)
	}
	return out
}

func summarizeKnowledgeSpaceForList(value knowledgestore.Space) knowledgestore.Space {
	value.Description = truncateListText(value.Description, listTextLimit)
	value.Objective = truncateListText(value.Objective, listTextLimit)
	value.Sources = summarizeKnowledgeSources(value.Sources)
	value.Reports = summarizeKnowledgeReports(value.Reports)
	value.ResearchRuns = summarizeKnowledgeResearchRuns(value.ResearchRuns)
	value.Insight.SuggestedQuestions = truncateListStrings(value.Insight.SuggestedQuestions, listCollectionLimit, listShortTextLimit)
	value.Insight.KeyTerms = truncateListStrings(value.Insight.KeyTerms, listCollectionLimit, listShortTextLimit)
	return value
}

func summarizeKnowledgeSources(values []knowledgestore.Source) []knowledgestore.Source {
	out := make([]knowledgestore.Source, len(values))
	for index, value := range values {
		value.Content = ""
		value.Summary = truncateListText(value.Summary, listTextLimit)
		value.Questions = truncateListStrings(value.Questions, listCollectionLimit, listShortTextLimit)
		value.Reliability = truncateListStrings(value.Reliability, listCollectionLimit, listShortTextLimit)
		value.Sections = nil
		value.Chunks = nil
		if len(value.Claims) > listCollectionLimit {
			value.Claims = value.Claims[:listCollectionLimit]
		}
		for claimIndex := range value.Claims {
			value.Claims[claimIndex].Text = truncateListText(value.Claims[claimIndex].Text, listShortTextLimit)
		}
		if len(value.Entities) > listCollectionLimit {
			value.Entities = value.Entities[:listCollectionLimit]
		}
		for entityIndex := range value.Entities {
			value.Entities[entityIndex].Description = truncateListText(value.Entities[entityIndex].Description, listShortTextLimit)
		}
		out[index] = value
	}
	return out
}

func summarizeKnowledgeReports(values []knowledgestore.Report) []knowledgestore.Report {
	if len(values) > listCollectionLimit {
		values = values[:listCollectionLimit]
	}
	out := make([]knowledgestore.Report, len(values))
	for index, value := range values {
		value.Question = truncateListText(value.Question, listShortTextLimit)
		value.Answer = truncateListText(value.Answer, listTextLimit)
		value.KeyFindings = truncateListStrings(value.KeyFindings, listCollectionLimit, listShortTextLimit)
		value.Gaps = truncateListStrings(value.Gaps, listCollectionLimit, listShortTextLimit)
		value.Evidence = summarizeKnowledgeEvidence(value.Evidence)
		out[index] = value
	}
	return out
}

func summarizeKnowledgeEvidence(values []knowledgestore.Evidence) []knowledgestore.Evidence {
	if len(values) > listCollectionLimit {
		values = values[:listCollectionLimit]
	}
	out := make([]knowledgestore.Evidence, len(values))
	for index, value := range values {
		value.Excerpt = truncateListText(value.Excerpt, listShortTextLimit)
		value.SourceSummary = truncateListText(value.SourceSummary, listShortTextLimit)
		out[index] = value
	}
	return out
}

func summarizeKnowledgeResearchRuns(values []knowledgestore.ResearchRun) []knowledgestore.ResearchRun {
	if len(values) > listCollectionLimit {
		values = values[:listCollectionLimit]
	}
	out := make([]knowledgestore.ResearchRun, len(values))
	for index, value := range values {
		value.Objective = truncateListText(value.Objective, listTextLimit)
		value.Scope = truncateListText(value.Scope, listTextLimit)
		value.Error = truncateListText(value.Error, listTextLimit)
		value.StopReason = truncateListText(value.StopReason, listShortTextLimit)
		value.Plan.RewrittenObjective = truncateListText(value.Plan.RewrittenObjective, listTextLimit)
		value.Plan.ClarifyingQuestions = truncateListStrings(value.Plan.ClarifyingQuestions, listCollectionLimit, listShortTextLimit)
		value.Plan.SearchQueries = truncateListStrings(value.Plan.SearchQueries, listCollectionLimit, listShortTextLimit)
		value.Plan.Steps = truncateListStrings(value.Plan.Steps, listCollectionLimit, listShortTextLimit)
		value.Plan.ExpectedOutputs = truncateListStrings(value.Plan.ExpectedOutputs, listCollectionLimit, listShortTextLimit)
		value.Candidates = summarizeKnowledgeCandidates(value.Candidates)
		value.ResearchLoops = summarizeKnowledgeLoops(value.ResearchLoops)
		value.Coverage = summarizeKnowledgeCoverage(value.Coverage)
		value.Events = summarizeKnowledgeRunEvents(value.Events)
		out[index] = value
	}
	return out
}

func summarizeKnowledgeCandidates(values []knowledgestore.SourceCandidate) []knowledgestore.SourceCandidate {
	if len(values) > listCollectionLimit {
		values = values[:listCollectionLimit]
	}
	out := make([]knowledgestore.SourceCandidate, len(values))
	for index, value := range values {
		value.Snippet = truncateListText(value.Snippet, listShortTextLimit)
		value.Error = truncateListText(value.Error, listShortTextLimit)
		out[index] = value
	}
	return out
}

func summarizeKnowledgeLoops(values []knowledgestore.ResearchLoop) []knowledgestore.ResearchLoop {
	if len(values) > listCollectionLimit {
		values = values[:listCollectionLimit]
	}
	out := make([]knowledgestore.ResearchLoop, len(values))
	for index, value := range values {
		value.Queries = truncateListStrings(value.Queries, listCollectionLimit, listShortTextLimit)
		value.Coverage = truncateListStrings(value.Coverage, listCollectionLimit, listShortTextLimit)
		value.SupportedClaims = truncateListStrings(value.SupportedClaims, listCollectionLimit, listShortTextLimit)
		value.Gaps = truncateListStrings(value.Gaps, listCollectionLimit, listShortTextLimit)
		value.FollowUpQueries = truncateListStrings(value.FollowUpQueries, listCollectionLimit, listShortTextLimit)
		out[index] = value
	}
	return out
}

func summarizeKnowledgeCoverage(values []knowledgestore.ResearchCoverage) []knowledgestore.ResearchCoverage {
	if len(values) > listCollectionLimit {
		values = values[:listCollectionLimit]
	}
	out := make([]knowledgestore.ResearchCoverage, len(values))
	for index, value := range values {
		value.Notes = truncateListText(value.Notes, listShortTextLimit)
		out[index] = value
	}
	return out
}

func summarizeKnowledgeRunEvents(values []knowledgestore.ResearchRunEvent) []knowledgestore.ResearchRunEvent {
	if len(values) > listCollectionLimit {
		values = values[:listCollectionLimit]
	}
	out := make([]knowledgestore.ResearchRunEvent, len(values))
	for index, value := range values {
		value.Message = truncateListText(value.Message, listShortTextLimit)
		out[index] = value
	}
	return out
}

func summarizeEventsForList(events []eventlog.Event) []eventlog.Event {
	out := make([]eventlog.Event, len(events))
	for index, value := range events {
		out[index] = summarizeEventForList(value)
	}
	return out
}

func summarizeEventForList(value eventlog.Event) eventlog.Event {
	if len(value.Payload) <= listPayloadLimit {
		return value
	}
	summary := summarizePayloadText(value.Payload)
	if summary == "" {
		summary = value.Type
	}
	payload, err := json.Marshal(map[string]any{
		"summary":        truncateListText(summary, listTextLimit),
		"truncated":      true,
		"original_bytes": len(value.Payload),
	})
	if err != nil {
		value.Payload = json.RawMessage(`{"summary":"payload omitted","truncated":true}`)
		return value
	}
	value.Payload = payload
	return value
}

func summarizePayloadText(payload json.RawMessage) string {
	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return compactPayload(payload)
	}
	return summarizeDecodedPayload(decoded)
}

func summarizeDecodedPayload(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case map[string]any:
		for _, key := range []string{"summary", "message", "content", "reply", "result", "error", "reason", "command", "title", "goal"} {
			if value, ok := typed[key].(string); ok && strings.TrimSpace(value) != "" {
				return value
			}
		}
		return compactPayloadFromValue(typed)
	case []any:
		return compactPayloadFromValue(typed)
	default:
		return compactPayloadFromValue(typed)
	}
}

func compactPayload(payload json.RawMessage) string {
	var buf bytes.Buffer
	if err := json.Compact(&buf, payload); err != nil {
		return string(payload)
	}
	return buf.String()
}

func compactPayloadFromValue(value any) string {
	b, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return compactPayload(b)
}
