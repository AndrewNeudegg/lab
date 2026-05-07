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
	signals = append(signals, o.assistantSubmittedSignalCandidates(now)...)
	signals = mergeAssistantSignalCandidates(signals)
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

func (o *Orchestrator) assistantSubmittedSignalCandidates(now time.Time) []assistantstore.RunSignal {
	store, err := o.assistantSignalCandidateStore()
	if err != nil {
		if strings.TrimSpace(o.cfg.DataDir) != "" {
			o.log().Warn("assistant signal candidate store unavailable", "error", err)
		}
		return nil
	}
	candidates, err := store.ListActive(now)
	if err != nil {
		o.log().Warn("assistant signal candidates unavailable", "error", err)
		return nil
	}
	out := make([]assistantstore.RunSignal, 0, len(candidates))
	for _, candidate := range candidates {
		signal := candidate.ToRunSignal()
		if strings.TrimSpace(signal.Surface) == "" {
			signal.Surface = candidate.Source
		}
		out = append(out, newAssistantRunSignal(signal))
	}
	return out
}

func mergeAssistantSignalCandidates(signals []assistantstore.RunSignal) []assistantstore.RunSignal {
	if len(signals) < 2 {
		return signals
	}
	indexByFingerprint := map[string]int{}
	out := make([]assistantstore.RunSignal, 0, len(signals))
	for _, signal := range signals {
		signal = newAssistantRunSignal(signal)
		if strings.TrimSpace(signal.Fingerprint) == "" {
			out = append(out, signal)
			continue
		}
		if existingIndex, ok := indexByFingerprint[signal.Fingerprint]; ok {
			out[existingIndex] = mergeAssistantRunSignals(out[existingIndex], signal)
			continue
		}
		indexByFingerprint[signal.Fingerprint] = len(out)
		out = append(out, signal)
	}
	return out
}

func mergeAssistantRunSignals(existing, incoming assistantstore.RunSignal) assistantstore.RunSignal {
	base := existing
	other := incoming
	if incoming.Score > existing.Score {
		base = incoming
		other = existing
	}
	base.Evidence = mergeAssistantRunSignalEvidence(base.Evidence, other.Evidence, 8)
	base.SafeActions = normalizeAssistantSignalSafeActions(append(append([]string{}, base.SafeActions...), other.SafeActions...))
	if other.SeenCount > base.SeenCount {
		base.SeenCount = other.SeenCount
	}
	if other.UsefulCount > base.UsefulCount {
		base.UsefulCount = other.UsefulCount
	}
	if strings.TrimSpace(base.CreatedTaskID) == "" {
		base.CreatedTaskID = strings.TrimSpace(other.CreatedTaskID)
	}
	if base.SnoozedUntil.IsZero() {
		base.SnoozedUntil = other.SnoozedUntil
	}
	if !base.Suppressed && other.Suppressed {
		base.Suppressed = true
		base.SuppressionReason = other.SuppressionReason
	}
	base.WhyNow = firstNonEmptyString(base.WhyNow, other.WhyNow)
	base.SuggestedNextStep = firstNonEmptyString(base.SuggestedNextStep, other.SuggestedNextStep)
	return newAssistantRunSignal(base)
}

func mergeAssistantRunSignalEvidence(primary, secondary []assistantstore.RunSignalEvidence, limit int) []assistantstore.RunSignalEvidence {
	if limit <= 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]assistantstore.RunSignalEvidence, 0, limit)
	for _, values := range [][]assistantstore.RunSignalEvidence{primary, secondary} {
		for _, value := range values {
			key := strings.ToLower(strings.Join([]string{
				strings.TrimSpace(value.Source),
				strings.TrimSpace(value.Kind),
				strings.TrimSpace(value.Title),
				strings.TrimSpace(value.ObjectID),
			}, "|"))
			if key == "|||" || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, value)
			if len(out) >= limit {
				return out
			}
		}
	}
	return out
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
		whyNow := "The task is in an operator attention state."
		suggestedNextStep := "Review the task and decide whether to unblock, review, retry, accept, or leave it alone."
		taskGoal := strings.TrimSpace(strings.Join([]string{
			"Review the task that the proactive Assistant flagged.",
			"Task: " + task.Title,
			"Status: " + labelAssistantSignalValue(task.Status),
			"Summary: " + detail,
			"Decide whether to unblock, review, retry, accept, or leave it alone.",
		}, "\n"))
		signals = append(signals, newAssistantRunSignal(assistantstore.RunSignal{
			Kind:              "task_" + strings.ToLower(strings.TrimSpace(task.Status)),
			Title:             title,
			Detail:            detail,
			WhyNow:            whyNow,
			Severity:          severity,
			Surface:           "tasks",
			ObjectID:          task.ID,
			ObjectURL:         firstNonEmptyString(task.URL, dashboardTaskURL(task.ID)),
			Score:             score,
			ActionKind:        "task",
			Rationale:         whyNow,
			TaskGoal:          taskGoal,
			Evidence:          []assistantstore.RunSignalEvidence{assistantObjectEvidence("tasks", "task_status", task, score)},
			SuggestedNextStep: suggestedNextStep,
			Fingerprint:       assistantSignalFingerprint("task", task.Status, task.ID, title),
			Confidence:        assistantSignalConfidence(score),
			Priority:          assistantSignalPriority(score),
			Suppressed:        false,
			SeenCount:         0,
			UsefulCount:       0,
			CreatedTaskID:     "",
		}))
	}

	if snapshot.PendingApprovals > 0 {
		countLabel := fmt.Sprintf("%d pending approval", snapshot.PendingApprovals)
		if snapshot.PendingApprovals != 1 {
			countLabel += "s"
		}
		signals = append(signals, newAssistantRunSignal(assistantstore.RunSignal{
			Kind:              "pending_approvals",
			Title:             "Review pending approvals",
			Detail:            countLabel + " need an operator decision.",
			WhyNow:            "Pending approvals block work until the operator decides.",
			Severity:          "warning",
			Surface:           "tasks",
			ObjectID:          "pending_approvals",
			ObjectURL:         "/tasks",
			Score:             82,
			ActionKind:        "task",
			Rationale:         "Pending approvals block work until the operator decides.",
			TaskGoal:          "Review pending approvals in Tasks and approve, deny, or edit each request.",
			Evidence:          []assistantstore.RunSignalEvidence{assistantSignalEvidence("tasks", "count", countLabel, "Approval requests awaiting an operator decision.", "pending_approvals", "/tasks", time.Time{}, 82)},
			SuggestedNextStep: "Open Tasks and decide each pending approval.",
			Fingerprint:       assistantSignalFingerprint("tasks", "pending_approvals", "all", "Review pending approvals"),
		}))
	}

	for status, count := range snapshot.WorkflowCounts {
		if count <= 0 || !strings.EqualFold(status, "failed") {
			continue
		}
		signals = append(signals, newAssistantRunSignal(assistantstore.RunSignal{
			Kind:              "workflow_failed",
			Title:             "Review failed workflows",
			Detail:            fmt.Sprintf("%d workflows are failed.", count),
			WhyNow:            "Failed workflows may mean a repeatable thinking path is stuck.",
			Severity:          "warning",
			Surface:           "workflows",
			ObjectID:          "failed_workflows",
			ObjectURL:         "/workflows",
			Score:             74,
			ActionKind:        "task",
			Rationale:         "Failed workflows may mean a repeatable thinking path is stuck.",
			TaskGoal:          "Review failed workflows, identify the failing step, and decide whether to repair or retire them.",
			Evidence:          []assistantstore.RunSignalEvidence{assistantSignalEvidence("workflows", "count", "Failed workflows", fmt.Sprintf("%d failed workflow records are present.", count), "failed_workflows", "/workflows", time.Time{}, 74)},
			SuggestedNextStep: "Open Workflows and inspect the latest failed run before deciding whether to repair or retire it.",
			Fingerprint:       assistantSignalFingerprint("workflows", "failed", "all", "Review failed workflows"),
		}))
	}

	for status, count := range snapshot.RemoteAgentCounts {
		if count <= 0 || !assistantRemoteAgentStatusNeedsAttention(status) {
			continue
		}
		signals = append(signals, newAssistantRunSignal(assistantstore.RunSignal{
			Kind:              "remote_agent_" + strings.ToLower(strings.TrimSpace(status)),
			Title:             "Check remote agent availability",
			Detail:            fmt.Sprintf("%d remote agents are %s.", count, labelAssistantSignalValue(status)),
			WhyNow:            "Unavailable remote agents reduce the harness capacity for delegated work.",
			Severity:          "warning",
			Surface:           "agents",
			ObjectID:          "remote_agents_" + strings.ToLower(strings.TrimSpace(status)),
			Score:             70,
			ActionKind:        "task",
			Rationale:         "Unavailable remote agents reduce the harness capacity for delegated work.",
			TaskGoal:          "Inspect remote agent availability and decide whether to reconnect, disable, or reroute work.",
			Evidence:          []assistantstore.RunSignalEvidence{assistantSignalEvidence("agents", "count", "Remote agent availability", fmt.Sprintf("%d remote agents are %s.", count, labelAssistantSignalValue(status)), "remote_agents_"+strings.ToLower(strings.TrimSpace(status)), "", time.Time{}, 70)},
			SuggestedNextStep: "Inspect remote agent availability and decide whether to reconnect, disable, or reroute work.",
			Fingerprint:       assistantSignalFingerprint("agents", status, "all", "Check remote agent availability"),
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
			Kind:              "health_unreachable",
			Title:             "Restore healthd visibility",
			Detail:            snapshot.Error,
			WhyNow:            "The Assistant cannot evaluate source signals while healthd is unreachable.",
			Severity:          "warning",
			Surface:           "healthd",
			ObjectID:          "healthd",
			ObjectURL:         "/healthd",
			Score:             78,
			ActionKind:        "task",
			Rationale:         "The Assistant cannot evaluate source signals while healthd is unreachable.",
			TaskGoal:          "Restore healthd visibility and verify the dashboard Health page reports current checks.",
			Evidence:          []assistantstore.RunSignalEvidence{assistantSignalEvidence("healthd", "source_error", "healthd unreachable", snapshot.Error, "healthd", "/healthd", time.Time{}, 78)},
			SuggestedNextStep: "Restore healthd visibility, then re-run the proactive check.",
			Fingerprint:       assistantSignalFingerprint("healthd", "unreachable", "healthd", "Restore healthd visibility"),
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
	evidence := []assistantstore.RunSignalEvidence{
		assistantSignalEvidence("healthd", "status", "healthd status", "healthd reported "+labelAssistantSignalValue(status)+".", "healthd", "/healthd", time.Time{}, score),
	}
	for _, item := range snapshot.Items {
		evidence = append(evidence, assistantObjectEvidence("healthd", "check", item, score))
	}
	return []assistantstore.RunSignal{newAssistantRunSignal(assistantstore.RunSignal{
		Kind:              "health_" + status,
		Title:             "Review health warning",
		Detail:            detail,
		WhyNow:            "A monitored source reported a non-healthy status.",
		Severity:          severity,
		Surface:           "healthd",
		ObjectID:          "healthd",
		ObjectURL:         "/healthd",
		Score:             score,
		ActionKind:        "task",
		Rationale:         "Health warnings can become operational failures if left unresolved.",
		TaskGoal:          "Review Health, identify the failing checks, and decide whether to create repair work.",
		Evidence:          evidence,
		SuggestedNextStep: "Open Health, inspect the failing checks, and decide whether to create repair work.",
		Fingerprint:       assistantSignalFingerprint("healthd", status, "healthd", "Review health warning"),
	})}
}

func assistantSupervisorSignals(snapshot assistantstore.RunSystemSnapshot) []assistantstore.RunSignal {
	if strings.TrimSpace(snapshot.Error) != "" {
		return []assistantstore.RunSignal{newAssistantRunSignal(assistantstore.RunSignal{
			Kind:              "supervisor_unreachable",
			Title:             "Restore supervisord visibility",
			Detail:            snapshot.Error,
			WhyNow:            "The Assistant cannot verify source signals while supervisord is unreachable.",
			Severity:          "warning",
			Surface:           "supervisord",
			ObjectID:          "supervisord",
			ObjectURL:         "/supervisord",
			Score:             84,
			ActionKind:        "task",
			Rationale:         "The Assistant cannot verify supervised components while supervisord is unreachable.",
			TaskGoal:          "Restore supervisord visibility and verify supervised component health.",
			Evidence:          []assistantstore.RunSignalEvidence{assistantSignalEvidence("supervisord", "source_error", "supervisord unreachable", snapshot.Error, "supervisord", "/supervisord", time.Time{}, 84)},
			SuggestedNextStep: "Restore supervisord visibility, then verify supervised component health.",
			Fingerprint:       assistantSignalFingerprint("supervisord", "unreachable", "supervisord", "Restore supervisord visibility"),
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
			Kind:              "supervisor_" + status,
			Title:             title,
			Detail:            firstNonEmptyString(item.Summary, "Component is "+labelAssistantSignalValue(status)+"."),
			WhyNow:            "A supervised component that should be running is not healthy.",
			Severity:          "warning",
			Surface:           "supervisord",
			ObjectID:          item.ID,
			ObjectURL:         firstNonEmptyString(item.URL, "/supervisord"),
			Score:             score,
			ActionKind:        "task",
			Rationale:         "A supervised component that should be running is not healthy.",
			TaskGoal:          "Inspect supervisord for " + firstNonEmptyString(item.Title, "the component") + " and decide whether to restart, repair, or update its desired state.",
			Evidence:          []assistantstore.RunSignalEvidence{assistantObjectEvidence("supervisord", "component_status", item, score)},
			SuggestedNextStep: "Inspect the component and decide whether to restart, repair, or update its desired state.",
			Fingerprint:       assistantSignalFingerprint("supervisord", status, firstNonEmptyString(item.ID, item.Title), title),
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
		detail := firstNonEmptyString(event.Summary, "Recent event "+event.Type+" was recorded.")
		signals = append(signals, newAssistantRunSignal(assistantstore.RunSignal{
			Kind:              "event_" + strings.ReplaceAll(strings.ToLower(event.Type), ".", "_"),
			Title:             title,
			Detail:            detail,
			WhyNow:            "A recent control-plane event may need operator follow-up.",
			Severity:          "warning",
			Surface:           "events",
			ObjectID:          firstNonEmptyString(event.TaskID, event.ID),
			ObjectURL:         assistantEventObjectURL(event),
			Score:             score,
			ActionKind:        "task",
			Rationale:         "A recent control-plane event may need operator follow-up.",
			TaskGoal:          "Review the recent event " + event.Type + " and decide whether follow-up work is needed.",
			Evidence:          []assistantstore.RunSignalEvidence{assistantSignalEvidence("events", event.Type, title, detail, firstNonEmptyString(event.TaskID, event.ID), assistantEventObjectURL(event), event.Time, score)},
			SuggestedNextStep: "Review the event context and decide whether follow-up work is needed.",
			Fingerprint:       assistantSignalFingerprint("events", event.Type, firstNonEmptyString(event.TaskID, event.ID), title),
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
	if strings.TrimSpace(signal.WhyNow) == "" {
		signal.WhyNow = firstNonEmptyString(signal.Rationale, signal.Detail)
	}
	if strings.TrimSpace(signal.SuggestedNextStep) == "" {
		signal.SuggestedNextStep = firstNonEmptyString(signal.TaskGoal, signal.Rationale, signal.Detail)
	}
	if len(signal.SafeActions) == 0 {
		signal.SafeActions = assistantSignalSafeActions(signal.ActionKind)
	}
	if len(signal.Evidence) == 0 {
		signal.Evidence = []assistantstore.RunSignalEvidence{
			assistantSignalEvidence(
				firstNonEmptyString(signal.Surface, "assistant"),
				signal.Kind,
				signal.Title,
				signal.Detail,
				signal.ObjectID,
				signal.ObjectURL,
				time.Time{},
				signal.Score,
			),
		}
	}
	return assistantstore.NormalizeRun(assistantstore.Run{
		Snapshot: assistantstore.RunSnapshot{Signals: []assistantstore.RunSignal{signal}},
	}).Snapshot.Signals[0]
}

func assistantSignalEvidence(source, kind, title, detail, objectID, objectURL string, observedAt time.Time, weight int) assistantstore.RunSignalEvidence {
	var observedAtRef *time.Time
	if !observedAt.IsZero() {
		observedAt = observedAt.UTC()
		observedAtRef = &observedAt
	}
	return assistantstore.RunSignalEvidence{
		Source:     source,
		Kind:       kind,
		Title:      title,
		Detail:     detail,
		ObjectID:   objectID,
		ObjectURL:  objectURL,
		ObservedAt: observedAtRef,
		Weight:     weight,
	}
}

func assistantObjectEvidence(source, kind string, item assistantstore.RunObjectRef, weight int) assistantstore.RunSignalEvidence {
	detailParts := []string{}
	if strings.TrimSpace(item.Status) != "" {
		detailParts = append(detailParts, "Status: "+labelAssistantSignalValue(item.Status))
	}
	if strings.TrimSpace(item.Summary) != "" {
		detailParts = append(detailParts, item.Summary)
	}
	return assistantSignalEvidence(
		source,
		kind,
		firstNonEmptyString(item.Title, item.ID, "Observed item"),
		strings.Join(detailParts, ". "),
		item.ID,
		item.URL,
		time.Time{},
		weight,
	)
}

func assistantSignalSafeActions(actionKind string) []string {
	switch strings.ToLower(strings.TrimSpace(actionKind)) {
	case "task":
		return []string{"create_task", "useful", "snooze", "dismiss"}
	default:
		return []string{"useful", "snooze", "dismiss"}
	}
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
	case assistantstore.SignalStatusUseful:
		signal.Suppressed = true
		signal.SuppressionReason = "Marked useful; cleared from the active inbox until a new sighting arrives."
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
		if !assistantSignalAllowsRecommendation(signal, signal.ActionKind) {
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
	kept := actions[:0]
	for index := range actions {
		signal, ok := assistantBestSignalForAction(actions[index], signals)
		if ok {
			if !assistantSignalAllowsRecommendation(signal, actions[index].Kind) {
				continue
			}
			actions[index].Fingerprint = signal.Fingerprint
			actions[index].TargetSurface = firstNonEmptyString(actions[index].TargetSurface, signal.Surface)
			actions[index].Priority = firstNonEmptyString(actions[index].Priority, signal.Priority)
			actions[index].Risk = firstNonEmptyString(actions[index].Risk, "low")
			if strings.EqualFold(actions[index].Kind, "task") {
				actions[index].TaskGoal = firstNonEmptyString(actions[index].TaskGoal, signal.TaskGoal, signal.SuggestedNextStep)
			}
		}
		kept = append(kept, actions[index])
	}
	return kept
}

func assistantSignalAllowsRecommendation(signal assistantstore.RunSignal, kind string) bool {
	if len(signal.SafeActions) == 0 {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "task":
		return assistantSignalHasSafeAction(signal, "create_task")
	case "observe", "watch", "":
		return true
	default:
		return assistantSignalHasSafeAction(signal, "useful")
	}
}

func assistantSignalHasSafeAction(signal assistantstore.RunSignal, want string) bool {
	for _, value := range signal.SafeActions {
		if strings.EqualFold(strings.TrimSpace(value), want) {
			return true
		}
	}
	return false
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
		Detail:    firstNonEmptyString(signal.Detail, signal.WhyNow, signal.Rationale),
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
		Rationale:     firstNonEmptyString(signal.Rationale, signal.WhyNow, signal.Detail),
		Priority:      signal.Priority,
		Risk:          "low",
		TargetSurface: signal.Surface,
		TaskGoal:      firstNonEmptyString(signal.TaskGoal, signal.SuggestedNextStep),
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
