package agent

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	assistantstore "github.com/andrewneudegg/lab/pkg/assistant"
	taskstore "github.com/andrewneudegg/lab/pkg/task"
)

const (
	assistantSignalActionScore = 70
	assistantSignalHighScore   = 85
)

func (o *Orchestrator) assistantWatchlistSignals(snapshot assistantstore.RunSnapshot, now time.Time) []assistantstore.RunSignal {
	signals := assistantSignalCandidatesFromSnapshot(snapshot)
	store, err := o.assistantSignalStore()
	if err == nil {
		for index := range signals {
			applyAssistantSignalRecordToSignal(store, &signals[index], now)
		}
	} else if strings.TrimSpace(o.cfg.DataDir) != "" {
		o.log().Warn("assistant signal store unavailable", "error", err)
	}
	return rankAssistantSignals(signals)
}

func assistantSignalCandidatesFromSnapshot(snapshot assistantstore.RunSnapshot) []assistantstore.RunSignal {
	var signals []assistantstore.RunSignal
	attentionTaskIDs := map[string]bool{}
	for _, task := range snapshot.AttentionTasks {
		attentionTaskIDs[task.ID] = true
		score, severity := assistantTaskStatusSignalScore(task.Status)
		if score <= 0 {
			continue
		}
		title := assistantTaskSignalTitle(task.Status, task.Title)
		detail := firstNonEmptyString(task.Summary, "Task is "+labelAssistantSignalValue(task.Status)+".")
		taskGoal := strings.TrimSpace(strings.Join([]string{
			"Review the task that the proactive Assistant flagged.",
			"Task: " + task.Title,
			"Status: " + labelAssistantSignalValue(task.Status),
			"Summary: " + detail,
			"Decide whether to unblock, review, retry, accept, or leave it alone.",
		}, "\n"))
		signals = append(signals, newAssistantRunSignal(assistantstore.RunSignal{
			Kind:          "task_" + strings.ToLower(strings.TrimSpace(task.Status)),
			Title:         title,
			Detail:        detail,
			Severity:      severity,
			Surface:       "tasks",
			ObjectID:      task.ID,
			ObjectURL:     firstNonEmptyString(task.URL, dashboardTaskURL(task.ID)),
			Score:         score,
			ActionKind:    "task",
			Rationale:     "The task is in an operator attention state.",
			TaskGoal:      taskGoal,
			Fingerprint:   assistantSignalFingerprint("task", task.Status, task.ID, title),
			Confidence:    assistantSignalConfidence(score),
			Priority:      assistantSignalPriority(score),
			Suppressed:    false,
			SeenCount:     0,
			UsefulCount:   0,
			CreatedTaskID: "",
		}))
	}

	if snapshot.PendingApprovals > 0 {
		countLabel := fmt.Sprintf("%d pending approval", snapshot.PendingApprovals)
		if snapshot.PendingApprovals != 1 {
			countLabel += "s"
		}
		signals = append(signals, newAssistantRunSignal(assistantstore.RunSignal{
			Kind:        "pending_approvals",
			Title:       "Review pending approvals",
			Detail:      countLabel + " need an operator decision.",
			Severity:    "warning",
			Surface:     "tasks",
			ObjectID:    "pending_approvals",
			ObjectURL:   "/tasks",
			Score:       82,
			ActionKind:  "task",
			Rationale:   "Pending approvals block work until the operator decides.",
			TaskGoal:    "Review pending approvals in Tasks and approve, deny, or edit each request.",
			Fingerprint: assistantSignalFingerprint("tasks", "pending_approvals", "all", "Review pending approvals"),
		}))
	}

	for status, count := range snapshot.WorkflowCounts {
		if count <= 0 || !strings.EqualFold(status, "failed") {
			continue
		}
		signals = append(signals, newAssistantRunSignal(assistantstore.RunSignal{
			Kind:        "workflow_failed",
			Title:       "Review failed workflows",
			Detail:      fmt.Sprintf("%d workflows are failed.", count),
			Severity:    "warning",
			Surface:     "workflows",
			ObjectID:    "failed_workflows",
			ObjectURL:   "/workflows",
			Score:       74,
			ActionKind:  "task",
			Rationale:   "Failed workflows may mean a repeatable thinking path is stuck.",
			TaskGoal:    "Review failed workflows, identify the failing step, and decide whether to repair or retire them.",
			Fingerprint: assistantSignalFingerprint("workflows", "failed", "all", "Review failed workflows"),
		}))
	}

	for status, count := range snapshot.RemoteAgentCounts {
		if count <= 0 || !assistantRemoteAgentStatusNeedsAttention(status) {
			continue
		}
		signals = append(signals, newAssistantRunSignal(assistantstore.RunSignal{
			Kind:        "remote_agent_" + strings.ToLower(strings.TrimSpace(status)),
			Title:       "Check remote agent availability",
			Detail:      fmt.Sprintf("%d remote agents are %s.", count, labelAssistantSignalValue(status)),
			Severity:    "warning",
			Surface:     "agents",
			ObjectID:    "remote_agents_" + strings.ToLower(strings.TrimSpace(status)),
			Score:       70,
			ActionKind:  "task",
			Rationale:   "Unavailable remote agents reduce the harness capacity for delegated work.",
			TaskGoal:    "Inspect remote agent availability and decide whether to reconnect, disable, or reroute work.",
			Fingerprint: assistantSignalFingerprint("agents", status, "all", "Check remote agent availability"),
		}))
	}

	signals = append(signals, assistantHealthSignals(snapshot.Health)...)
	signals = append(signals, assistantSupervisorSignals(snapshot.Supervisor)...)
	signals = append(signals, assistantEventSignals(snapshot.RecentEvents, attentionTaskIDs)...)
	return signals
}

func assistantHealthSignals(snapshot assistantstore.RunSystemSnapshot) []assistantstore.RunSignal {
	if strings.TrimSpace(snapshot.Error) != "" {
		return []assistantstore.RunSignal{newAssistantRunSignal(assistantstore.RunSignal{
			Kind:        "health_unreachable",
			Title:       "Restore healthd visibility",
			Detail:      snapshot.Error,
			Severity:    "warning",
			Surface:     "healthd",
			ObjectID:    "healthd",
			ObjectURL:   "/healthd",
			Score:       78,
			ActionKind:  "task",
			Rationale:   "The Assistant cannot evaluate health signals while healthd is unreachable.",
			TaskGoal:    "Restore healthd visibility and verify the dashboard Health page reports current checks.",
			Fingerprint: assistantSignalFingerprint("healthd", "unreachable", "healthd", "Restore healthd visibility"),
		})}
	}
	status := strings.ToLower(strings.TrimSpace(snapshot.Status))
	if status == "" || status == "healthy" {
		return nil
	}
	score := 76
	severity := "warning"
	if status == "critical" || status == "unhealthy" || status == "failed" {
		score = 90
		severity = "critical"
	}
	detail := "healthd reported " + labelAssistantSignalValue(status) + "."
	if len(snapshot.Items) > 0 {
		detail = detail + " " + assistantSignalItemSummary(snapshot.Items, 3)
	}
	return []assistantstore.RunSignal{newAssistantRunSignal(assistantstore.RunSignal{
		Kind:        "health_" + status,
		Title:       "Review health warning",
		Detail:      detail,
		Severity:    severity,
		Surface:     "healthd",
		ObjectID:    "healthd",
		ObjectURL:   "/healthd",
		Score:       score,
		ActionKind:  "task",
		Rationale:   "Health warnings can become operational failures if left unresolved.",
		TaskGoal:    "Review Health, identify the failing checks, and decide whether to create repair work.",
		Fingerprint: assistantSignalFingerprint("healthd", status, "healthd", "Review health warning"),
	})}
}

func assistantSupervisorSignals(snapshot assistantstore.RunSystemSnapshot) []assistantstore.RunSignal {
	if strings.TrimSpace(snapshot.Error) != "" {
		return []assistantstore.RunSignal{newAssistantRunSignal(assistantstore.RunSignal{
			Kind:        "supervisor_unreachable",
			Title:       "Restore supervisord visibility",
			Detail:      snapshot.Error,
			Severity:    "warning",
			Surface:     "supervisord",
			ObjectID:    "supervisord",
			ObjectURL:   "/supervisord",
			Score:       84,
			ActionKind:  "task",
			Rationale:   "The Assistant cannot verify supervised components while supervisord is unreachable.",
			TaskGoal:    "Restore supervisord visibility and verify supervised component health.",
			Fingerprint: assistantSignalFingerprint("supervisord", "unreachable", "supervisord", "Restore supervisord visibility"),
		})}
	}
	var signals []assistantstore.RunSignal
	for _, item := range snapshot.Items {
		status := strings.ToLower(strings.TrimSpace(item.Status))
		if status == "" || status == "running" || assistantSupervisorItemDesiredStopped(item) {
			continue
		}
		score := 88
		if status == "failed" || status == "exited" {
			score = 92
		}
		title := "Restore supervised component: " + firstNonEmptyString(item.Title, "unknown")
		signals = append(signals, newAssistantRunSignal(assistantstore.RunSignal{
			Kind:        "supervisor_" + status,
			Title:       title,
			Detail:      firstNonEmptyString(item.Summary, "Component is "+labelAssistantSignalValue(status)+"."),
			Severity:    "warning",
			Surface:     "supervisord",
			ObjectID:    item.ID,
			ObjectURL:   firstNonEmptyString(item.URL, "/supervisord"),
			Score:       score,
			ActionKind:  "task",
			Rationale:   "A supervised component that should be running is not healthy.",
			TaskGoal:    "Inspect supervisord for " + firstNonEmptyString(item.Title, "the component") + " and decide whether to restart, repair, or update its desired state.",
			Fingerprint: assistantSignalFingerprint("supervisord", status, firstNonEmptyString(item.ID, item.Title), title),
		}))
	}
	return signals
}

func assistantEventSignals(events []assistantstore.RunEventRef, attentionTaskIDs map[string]bool) []assistantstore.RunSignal {
	var signals []assistantstore.RunSignal
	for index := len(events) - 1; index >= 0 && len(signals) < 4; index-- {
		event := events[index]
		if event.TaskID != "" && attentionTaskIDs[event.TaskID] {
			continue
		}
		score := assistantEventSignalScore(event.Type)
		if score <= 0 {
			continue
		}
		title := "Review recent event: " + labelAssistantSignalValue(event.Type)
		if event.TaskID != "" {
			title = title + " for " + taskShortID(event.TaskID)
		}
		signals = append(signals, newAssistantRunSignal(assistantstore.RunSignal{
			Kind:        "event_" + strings.ReplaceAll(strings.ToLower(event.Type), ".", "_"),
			Title:       title,
			Detail:      firstNonEmptyString(event.Summary, "Recent event "+event.Type+" was recorded."),
			Severity:    "warning",
			Surface:     "events",
			ObjectID:    firstNonEmptyString(event.TaskID, event.ID),
			ObjectURL:   assistantEventObjectURL(event),
			Score:       score,
			ActionKind:  "task",
			Rationale:   "A recent control-plane event may need operator follow-up.",
			TaskGoal:    "Review the recent event " + event.Type + " and decide whether follow-up work is needed.",
			Fingerprint: assistantSignalFingerprint("events", event.Type, firstNonEmptyString(event.TaskID, event.ID), title),
		}))
	}
	return signals
}

func newAssistantRunSignal(signal assistantstore.RunSignal) assistantstore.RunSignal {
	if signal.Fingerprint == "" {
		signal.Fingerprint = assistantSignalFingerprint(signal.Surface, signal.Kind, signal.ObjectID, signal.Title)
	}
	if signal.ID == "" {
		signal.ID = signal.Fingerprint
	}
	if signal.Confidence == "" {
		signal.Confidence = assistantSignalConfidence(signal.Score)
	}
	if signal.Priority == "" {
		signal.Priority = assistantSignalPriority(signal.Score)
	}
	if signal.ActionKind == "" && signal.Score >= assistantSignalActionScore {
		signal.ActionKind = "task"
	}
	return assistantstore.NormalizeRun(assistantstore.Run{
		Snapshot: assistantstore.RunSnapshot{Signals: []assistantstore.RunSignal{signal}},
	}).Snapshot.Signals[0]
}

func applyAssistantSignalRecordToSignal(store *assistantstore.SignalStore, signal *assistantstore.RunSignal, now time.Time) {
	if signal == nil || strings.TrimSpace(signal.Fingerprint) == "" {
		return
	}
	record, err := store.Load(signal.Fingerprint)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return
		}
		return
	}
	record = assistantstore.NormalizeSignalRecord(record, now)
	if record.SeenCount > 0 {
		signal.SeenCount = record.SeenCount
	}
	if record.UsefulCount > 0 {
		signal.UsefulCount = record.UsefulCount
		signal.Score += assistantMinInt(16, record.UsefulCount*8)
	}
	if record.SeenCount > 1 {
		signal.Score += assistantMinInt(12, (record.SeenCount-1)*3)
	}
	switch record.Status {
	case assistantstore.SignalStatusDismissed:
		signal.Suppressed = true
		signal.SuppressionReason = "Dismissed by operator feedback."
	case assistantstore.SignalStatusSnoozed:
		signal.Suppressed = true
		if !record.SnoozedUntil.IsZero() {
			signal.SuppressionReason = "Snoozed until " + record.SnoozedUntil.UTC().Format(time.RFC3339) + "."
			signal.SnoozedUntil = record.SnoozedUntil
		} else {
			signal.SuppressionReason = "Snoozed by operator feedback."
		}
	case assistantstore.SignalStatusCreatedTask:
		signal.Suppressed = true
		signal.SuppressionReason = "Task already created for this signal."
		signal.CreatedTaskID = record.CreatedTaskID
	}
	*signal = newAssistantRunSignal(*signal)
}

func rankAssistantSignals(signals []assistantstore.RunSignal) []assistantstore.RunSignal {
	for index := range signals {
		signals[index] = newAssistantRunSignal(signals[index])
	}
	sort.SliceStable(signals, func(i, j int) bool {
		if signals[i].Suppressed != signals[j].Suppressed {
			return !signals[i].Suppressed
		}
		if signals[i].Score != signals[j].Score {
			return signals[i].Score > signals[j].Score
		}
		return signals[i].Title < signals[j].Title
	})
	out := make([]assistantstore.RunSignal, 0, assistantMinInt(len(signals), 12))
	activeCount := 0
	suppressedCount := 0
	for _, signal := range signals {
		if signal.Suppressed {
			if signal.Score < assistantSignalActionScore || suppressedCount >= 4 {
				continue
			}
			out = append(out, signal)
			suppressedCount++
			continue
		}
		if signal.Score < 50 || activeCount >= 8 {
			continue
		}
		out = append(out, signal)
		activeCount++
	}
	return out
}

func assistantRunDecisionWithSignals(run assistantstore.Run, decision assistantRunDecision) assistantRunDecision {
	decision = normalizeAssistantRunDecision(decision)
	signals := assistantActiveSignals(run.Snapshot.Signals, 60)
	suppressed := assistantSuppressedSignalCount(run.Snapshot.Signals)
	decision.RecommendedActions = assistantAlignDecisionActionsWithSignals(decision.RecommendedActions, run.Snapshot.Signals)

	addedAction := false
	for _, signal := range signals {
		if signal.Score >= 65 && assistantDecisionFindingCount(decision) < 6 && !assistantDecisionHasFinding(decision, signal) {
			finding := assistantFindingFromSignal(signal)
			if signal.Severity == "info" && len(decision.Opportunities) < 6 {
				decision.Opportunities = append(decision.Opportunities, finding)
			} else if len(decision.Concerns) < 6 {
				decision.Concerns = append(decision.Concerns, finding)
			}
		}
		if signal.Score < assistantSignalActionScore || signal.ActionKind == "observe" {
			continue
		}
		if len(decision.RecommendedActions) >= 6 || assistantDecisionHasActionForSignal(decision, signal) {
			continue
		}
		if len(decision.RecommendedActions) == 0 || signal.Score >= assistantSignalHighScore {
			decision.RecommendedActions = append(decision.RecommendedActions, assistantActionFromSignal(signal, len(decision.RecommendedActions)))
			addedAction = true
		}
	}

	if len(signals) > 0 || suppressed > 0 {
		decision.Changed = appendUniqueAssistantChange(decision.Changed, assistantSignalChangeSummary(len(signals), suppressed))
	}
	if len(decision.RecommendedActions) > 0 || len(decision.Concerns) > 0 || len(decision.Opportunities) > 0 {
		wasNoop := decision.Decision == assistantstore.RunDecisionNoop
		if decision.Decision == assistantstore.RunDecisionNoop {
			decision.Decision = assistantstore.RunDecisionRecommend
		}
		if strings.TrimSpace(decision.Summary) == "" || strings.Contains(decision.Summary, "No urgent action") || wasNoop || addedAction {
			decision.Summary = assistantDecisionSummaryFromSignals(signals, suppressed)
		}
	} else if suppressed > 0 {
		decision.Decision = assistantstore.RunDecisionNoop
		decision.Summary = fmt.Sprintf("No new urgent action found; %d known signals are suppressed by prior feedback.", suppressed)
	}
	return normalizeAssistantRunDecision(decision)
}

func pruneAssistantSuppressedRunActions(run *assistantstore.Run) int {
	if run == nil || len(run.RecommendedActions) == 0 {
		return 0
	}
	kept := run.RecommendedActions[:0]
	suppressed := 0
	for _, action := range run.RecommendedActions {
		switch action.Status {
		case assistantstore.SignalStatusDismissed, assistantstore.SignalStatusSnoozed, assistantstore.SignalStatusCreatedTask:
			suppressed++
			continue
		default:
			kept = append(kept, action)
		}
	}
	run.RecommendedActions = kept
	if len(run.RecommendedActions) == 0 && run.Decision == assistantstore.RunDecisionRecommend && len(run.Concerns) == 0 && len(run.Opportunities) == 0 {
		run.Decision = assistantstore.RunDecisionNoop
	}
	return suppressed
}

func assistantActiveSignals(signals []assistantstore.RunSignal, minScore int) []assistantstore.RunSignal {
	out := make([]assistantstore.RunSignal, 0, len(signals))
	for _, signal := range signals {
		signal = newAssistantRunSignal(signal)
		if signal.Suppressed || signal.Score < minScore {
			continue
		}
		out = append(out, signal)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Title < out[j].Title
	})
	if len(out) > 6 {
		return out[:6]
	}
	return out
}

func assistantSuppressedSignalCount(signals []assistantstore.RunSignal) int {
	count := 0
	for _, signal := range signals {
		if signal.Suppressed {
			count++
		}
	}
	return count
}

func assistantAlignDecisionActionsWithSignals(actions []assistantstore.RunAction, signals []assistantstore.RunSignal) []assistantstore.RunAction {
	if len(actions) == 0 || len(signals) == 0 {
		return actions
	}
	for index := range actions {
		signal, ok := assistantBestSignalForAction(actions[index], signals)
		if !ok {
			continue
		}
		actions[index].Fingerprint = signal.Fingerprint
		actions[index].TargetSurface = firstNonEmptyString(actions[index].TargetSurface, signal.Surface)
		actions[index].Priority = firstNonEmptyString(actions[index].Priority, signal.Priority)
		actions[index].Risk = firstNonEmptyString(actions[index].Risk, "low")
		if strings.EqualFold(actions[index].Kind, "task") {
			actions[index].TaskGoal = firstNonEmptyString(actions[index].TaskGoal, signal.TaskGoal)
		}
	}
	return actions
}

func assistantBestSignalForAction(action assistantstore.RunAction, signals []assistantstore.RunSignal) (assistantstore.RunSignal, bool) {
	actionFingerprint := assistantstore.SignalFingerprint(action.Fingerprint)
	if strings.TrimSpace(action.Fingerprint) != "" {
		for _, signal := range signals {
			if actionFingerprint == assistantstore.SignalFingerprint(signal.Fingerprint) {
				return signal, true
			}
		}
	}
	bestScore := 0
	var best assistantstore.RunSignal
	actionText := strings.ToLower(strings.Join([]string{action.Title, action.Rationale, action.TaskGoal, action.KnowledgeQuery, action.WorkflowHint}, " "))
	for _, signal := range signals {
		score := 0
		if action.Kind != "" && strings.EqualFold(action.Kind, signal.ActionKind) {
			score += 2
		}
		if action.TargetSurface != "" && strings.EqualFold(action.TargetSurface, signal.Surface) {
			score += 2
		}
		if signal.ObjectID != "" && strings.Contains(actionText, strings.ToLower(signal.ObjectID)) {
			score += 4
		}
		if assistantTextSubstantiallyOverlaps(action.Title, signal.Title) {
			score += 4
		}
		if assistantTextSubstantiallyOverlaps(action.Rationale, signal.Detail) {
			score += 2
		}
		if score > bestScore {
			bestScore = score
			best = signal
		}
	}
	return best, bestScore >= 5
}

func assistantFindingFromSignal(signal assistantstore.RunSignal) assistantstore.RunFinding {
	return assistantstore.RunFinding{
		Title:     signal.Title,
		Detail:    firstNonEmptyString(signal.Detail, signal.Rationale),
		Severity:  signal.Severity,
		Surface:   signal.Surface,
		ObjectID:  signal.ObjectID,
		ObjectURL: signal.ObjectURL,
	}
}

func assistantActionFromSignal(signal assistantstore.RunSignal, index int) assistantstore.RunAction {
	action := assistantstore.RunAction{
		ID:            fmt.Sprintf("action_%d", index+1),
		Fingerprint:   signal.Fingerprint,
		Kind:          firstNonEmptyString(signal.ActionKind, "task"),
		Title:         signal.Title,
		Rationale:     firstNonEmptyString(signal.Rationale, signal.Detail),
		Priority:      signal.Priority,
		Risk:          "low",
		TargetSurface: signal.Surface,
		TaskGoal:      signal.TaskGoal,
		Status:        "recommended",
	}
	return assistantstore.NormalizeRun(assistantstore.Run{RecommendedActions: []assistantstore.RunAction{action}}).RecommendedActions[0]
}

func assistantDecisionHasFinding(decision assistantRunDecision, signal assistantstore.RunSignal) bool {
	for _, finding := range append(append([]assistantstore.RunFinding{}, decision.Concerns...), decision.Opportunities...) {
		if signal.ObjectID != "" && finding.ObjectID == signal.ObjectID && strings.EqualFold(finding.Surface, signal.Surface) {
			return true
		}
		if assistantTextSubstantiallyOverlaps(finding.Title, signal.Title) {
			return true
		}
	}
	return false
}

func assistantDecisionFindingCount(decision assistantRunDecision) int {
	return len(decision.Concerns) + len(decision.Opportunities)
}

func assistantDecisionHasActionForSignal(decision assistantRunDecision, signal assistantstore.RunSignal) bool {
	for _, action := range decision.RecommendedActions {
		if action.Fingerprint != "" && assistantstore.SignalFingerprint(action.Fingerprint) == signal.Fingerprint {
			return true
		}
		if assistantTextSubstantiallyOverlaps(action.Title, signal.Title) {
			return true
		}
	}
	return false
}

func assistantSignalChangeSummary(active, suppressed int) string {
	parts := []string{fmt.Sprintf("Scored proactive watchlist found %d actionable signals", active)}
	if suppressed > 0 {
		parts = append(parts, fmt.Sprintf("%d suppressed by feedback", suppressed))
	}
	return strings.Join(parts, ", ") + "."
}

func assistantDecisionSummaryFromSignals(signals []assistantstore.RunSignal, suppressed int) string {
	if len(signals) == 0 {
		if suppressed > 0 {
			return fmt.Sprintf("No new urgent action found; %d known signals are suppressed by prior feedback.", suppressed)
		}
		return "No urgent action found in the current homelabd state."
	}
	top := signals[0]
	return fmt.Sprintf("Scored watchlist found %s signal: %s.", firstNonEmptyString(top.Confidence, "actionable"), top.Title)
}

func appendUniqueAssistantChange(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if strings.EqualFold(strings.TrimSpace(existing), value) {
			return values
		}
	}
	if len(values) >= 6 {
		return values
	}
	return append(values, value)
}

func assistantTextSubstantiallyOverlaps(left, right string) bool {
	leftWords := assistantSignificantWords(left)
	rightWords := assistantSignificantWords(right)
	if len(leftWords) == 0 || len(rightWords) == 0 {
		return false
	}
	rightSet := map[string]bool{}
	for _, word := range rightWords {
		rightSet[word] = true
	}
	overlap := 0
	for _, word := range leftWords {
		if rightSet[word] {
			overlap++
		}
	}
	return overlap >= 2 || overlap >= assistantMinInt(len(leftWords), len(rightWords))
}

func assistantSignificantWords(value string) []string {
	fields := strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	})
	stop := map[string]bool{
		"the": true, "and": true, "for": true, "with": true, "that": true, "this": true,
		"task": true, "review": true, "assistant": true, "signal": true, "needs": true,
	}
	var out []string
	for _, field := range fields {
		if len(field) < 3 || stop[field] {
			continue
		}
		out = append(out, field)
	}
	return out
}

func assistantTaskStatusSignalScore(status string) (int, string) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case taskstore.StatusFailed:
		return 94, "critical"
	case taskstore.StatusBlocked, taskstore.StatusConflictResolution:
		return 90, "warning"
	case taskstore.StatusAwaitingApproval, taskstore.StatusAwaitingRestart:
		return 84, "warning"
	case taskstore.StatusAwaitingVerification:
		return 76, "info"
	case taskstore.StatusReadyForReview, taskstore.StatusNoChangeRequired:
		return 72, "info"
	default:
		return 0, ""
	}
}

func assistantTaskSignalTitle(status, title string) string {
	title = firstNonEmptyString(title, "task")
	switch strings.ToLower(strings.TrimSpace(status)) {
	case taskstore.StatusFailed:
		return "Review failed task: " + title
	case taskstore.StatusBlocked:
		return "Unblock task: " + title
	case taskstore.StatusConflictResolution:
		return "Resolve task conflict: " + title
	case taskstore.StatusAwaitingApproval:
		return "Decide task approval: " + title
	case taskstore.StatusAwaitingRestart:
		return "Complete restart gate: " + title
	case taskstore.StatusAwaitingVerification:
		return "Verify task outcome: " + title
	case taskstore.StatusReadyForReview:
		return "Review task result: " + title
	case taskstore.StatusNoChangeRequired:
		return "Accept or reopen no-change task: " + title
	default:
		return "Review task: " + title
	}
}

func assistantRemoteAgentStatusNeedsAttention(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "offline", "stale", "error", "unreachable":
		return true
	default:
		return false
	}
}

func assistantSupervisorItemDesiredStopped(item assistantstore.RunObjectRef) bool {
	summary := strings.ToLower(item.Summary)
	return strings.Contains(summary, "desired stopped") || strings.Contains(summary, "desired=stopped")
}

func assistantEventSignalScore(eventType string) int {
	switch eventType {
	case "task.review.failed", "task.restart.failed", "task.auto_recovery.exhausted", "approval.failed":
		return 86
	case "task.blocked", "task.conflict_resolution", "approval.requested":
		return 78
	case "task.awaiting_approval", "task.awaiting_restart", "task.awaiting_verification":
		return 72
	default:
		return 0
	}
}

func assistantEventObjectURL(event assistantstore.RunEventRef) string {
	if strings.TrimSpace(event.TaskID) != "" {
		return dashboardTaskURL(event.TaskID)
	}
	return "/tasks"
}

func assistantSignalConfidence(score int) string {
	switch {
	case score >= assistantSignalHighScore:
		return "high"
	case score >= 60:
		return "medium"
	default:
		return "low"
	}
}

func assistantSignalPriority(score int) string {
	switch {
	case score >= assistantSignalHighScore:
		return "high"
	case score >= assistantSignalActionScore:
		return "medium"
	default:
		return "low"
	}
}

func assistantSignalFingerprint(parts ...string) string {
	return assistantstore.SignalFingerprint("watchlist|" + strings.Join(parts, "|"))
}

func assistantSignalItemSummary(items []assistantstore.RunObjectRef, limit int) string {
	var parts []string
	for _, item := range items {
		if len(parts) >= limit {
			break
		}
		label := firstNonEmptyString(item.Title, item.ID)
		if item.Status != "" {
			label += " is " + labelAssistantSignalValue(item.Status)
		}
		if item.Summary != "" {
			label += ": " + item.Summary
		}
		parts = append(parts, label)
	}
	return strings.Join(parts, " ")
}

func labelAssistantSignalValue(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(value), "_", " "), ".", " ")
}

func assistantMinInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
