package assistant

import "strings"

const (
	runListTextLimit       = 1200
	runListShortTextLimit  = 320
	runListCollectionLimit = 12
)

func SummarizeRunsForList(runs []Run) []Run {
	out := make([]Run, len(runs))
	for index, value := range runs {
		out[index] = SummarizeRunForList(value)
	}
	return out
}

func SummarizeRunForList(value Run) Run {
	value = NormalizeRun(value)
	value.Goal = truncateRunListText(value.Goal, runListTextLimit)
	value.Summary = truncateRunListText(value.Summary, runListTextLimit)
	value.Error = truncateRunListText(value.Error, runListTextLimit)
	value.Changed = truncateRunListStrings(value.Changed, runListCollectionLimit, runListShortTextLimit)
	value.Concerns = summarizeRunFindingsForList(value.Concerns)
	value.Opportunities = summarizeRunFindingsForList(value.Opportunities)
	value.RecommendedActions = summarizeRunActionsForList(value.RecommendedActions)
	value.Receipts = summarizeRunReceiptsForList(value.Receipts)
	value.Snapshot = summarizeRunSnapshotForList(value.Snapshot)
	if value.Route != nil {
		route := *value.Route
		route.Reason = truncateRunListText(route.Reason, runListShortTextLimit)
		route.NextStep = truncateRunListText(route.NextStep, runListShortTextLimit)
		value.Route = &route
	}
	if value.Compiler != nil {
		compiler := *value.Compiler
		compiler.Summary = truncateRunListText(compiler.Summary, runListTextLimit)
		compiler.Checks = truncateRunListStrings(compiler.Checks, runListCollectionLimit, runListShortTextLimit)
		compiler.PolicyHints = summarizeRunPolicyHintsForList(compiler.PolicyHints)
		compiler.Repairs = truncateRunListStrings(compiler.Repairs, runListCollectionLimit, runListShortTextLimit)
		compiler.Rejections = truncateRunListStrings(compiler.Rejections, runListCollectionLimit, runListShortTextLimit)
		value.Compiler = &compiler
	}
	return value
}

func summarizeRunFindingsForList(values []RunFinding) []RunFinding {
	if len(values) > runListCollectionLimit {
		values = values[:runListCollectionLimit]
	}
	out := make([]RunFinding, len(values))
	for index, value := range values {
		value.Title = truncateRunListText(value.Title, runListShortTextLimit)
		value.Detail = truncateRunListText(value.Detail, runListTextLimit)
		out[index] = value
	}
	return out
}

func summarizeRunActionsForList(values []RunAction) []RunAction {
	if len(values) > 24 {
		values = values[:24]
	}
	out := make([]RunAction, len(values))
	for index, value := range values {
		value.Title = truncateRunListText(value.Title, runListShortTextLimit)
		value.Rationale = truncateRunListText(value.Rationale, runListTextLimit)
		value.TaskGoal = truncateRunListText(value.TaskGoal, runListTextLimit)
		value.KnowledgeQuery = truncateRunListText(value.KnowledgeQuery, runListTextLimit)
		value.WorkflowHint = truncateRunListText(value.WorkflowHint, runListTextLimit)
		if value.Contract != nil {
			contract := *value.Contract
			contract.Explanation = truncateRunListText(contract.Explanation, runListTextLimit)
			contract.RequiredEvidence = truncateRunListStrings(contract.RequiredEvidence, runListCollectionLimit, runListShortTextLimit)
			contract.RequiredInputs = truncateRunListStrings(contract.RequiredInputs, runListCollectionLimit, runListShortTextLimit)
			value.Contract = &contract
		}
		if value.Plan != nil {
			plan := *value.Plan
			plan.Summary = truncateRunListText(plan.Summary, runListTextLimit)
			plan.Blockers = truncateRunListStrings(plan.Blockers, runListCollectionLimit, runListShortTextLimit)
			if len(plan.Steps) > runListCollectionLimit {
				plan.Steps = plan.Steps[:runListCollectionLimit]
			}
			for stepIndex := range plan.Steps {
				plan.Steps[stepIndex].Title = truncateRunListText(plan.Steps[stepIndex].Title, runListShortTextLimit)
			}
			if len(plan.Receipts) > runListCollectionLimit {
				plan.Receipts = plan.Receipts[:runListCollectionLimit]
			}
			for receiptIndex := range plan.Receipts {
				plan.Receipts[receiptIndex].Message = truncateRunListText(plan.Receipts[receiptIndex].Message, runListShortTextLimit)
			}
			value.Plan = &plan
		}
		out[index] = value
	}
	return out
}

func summarizeRunReceiptsForList(values []RunReceipt) []RunReceipt {
	if len(values) > runListCollectionLimit {
		values = values[:runListCollectionLimit]
	}
	out := make([]RunReceipt, len(values))
	for index, value := range values {
		value.Message = truncateRunListText(value.Message, runListShortTextLimit)
		out[index] = value
	}
	return out
}

func summarizeRunPolicyHintsForList(values []RunPolicyHint) []RunPolicyHint {
	if len(values) > runListCollectionLimit {
		values = values[:runListCollectionLimit]
	}
	out := make([]RunPolicyHint, len(values))
	for index, value := range values {
		value.Reason = truncateRunListText(value.Reason, runListShortTextLimit)
		out[index] = value
	}
	return out
}

func summarizeRunSnapshotForList(value RunSnapshot) RunSnapshot {
	value.Signals = nil
	value.Goals = nil
	value.AttentionTasks = summarizeRunObjectRefsForList(value.AttentionTasks)
	value.RecentWorkflows = summarizeRunObjectRefsForList(value.RecentWorkflows)
	value.KnowledgeSpaces = summarizeRunObjectRefsForList(value.KnowledgeSpaces)
	value.RecentEvents = summarizeRunEventRefsForList(value.RecentEvents)
	value.Health.Items = summarizeRunObjectRefsForList(value.Health.Items)
	value.Supervisor.Items = summarizeRunObjectRefsForList(value.Supervisor.Items)
	return value
}

func summarizeRunObjectRefsForList(values []RunObjectRef) []RunObjectRef {
	if len(values) > runListCollectionLimit {
		values = values[:runListCollectionLimit]
	}
	out := make([]RunObjectRef, len(values))
	for index, value := range values {
		value.Title = truncateRunListText(value.Title, runListShortTextLimit)
		value.Summary = truncateRunListText(value.Summary, runListShortTextLimit)
		out[index] = value
	}
	return out
}

func summarizeRunEventRefsForList(values []RunEventRef) []RunEventRef {
	if len(values) > runListCollectionLimit {
		values = values[:runListCollectionLimit]
	}
	out := make([]RunEventRef, len(values))
	for index, value := range values {
		value.Summary = truncateRunListText(value.Summary, runListShortTextLimit)
		out[index] = value
	}
	return out
}

func truncateRunListStrings(values []string, itemLimit, textLimit int) []string {
	if len(values) == 0 {
		return values
	}
	if itemLimit > 0 && len(values) > itemLimit {
		values = values[:itemLimit]
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := truncateRunListText(value, textLimit); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func truncateRunListText(value string, limit int) string {
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
