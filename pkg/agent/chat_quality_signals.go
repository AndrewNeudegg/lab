package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	assistantstore "github.com/andrewneudegg/lab/pkg/assistant"
	"github.com/andrewneudegg/lab/pkg/llm"
)

const chatQualitySignalTTLSeconds = 3 * 24 * 60 * 60

func (o *Orchestrator) maybeSubmitChatQualitySignal(ctx context.Context, req HandleRequest, userEventID string, result HandleResult) {
	signal, ok := o.chatQualitySignal(ctx, req, userEventID, result)
	if !ok {
		return
	}
	if _, err := o.SubmitAssistantSignal(ctx, signal); err != nil && !assistantSignalSourceControlError(err) {
		o.log().Warn("assistant chat signal submission failed", "error", err)
	}
}

func (o *Orchestrator) chatQualitySignal(ctx context.Context, req HandleRequest, userEventID string, result HandleResult) (assistantstore.SignalSubmitRequest, bool) {
	message := strings.TrimSpace(req.Message)
	if message == "" {
		return assistantstore.SignalSubmitRequest{}, false
	}
	now := time.Now().UTC()
	conversationID := strings.TrimSpace(req.ConversationID)
	previousAssistant := lastAssistantMessage(o.recentChatHistoryBeforeCurrent(ctx, now, 12))
	currentReply := strings.TrimSpace(result.Reply)
	objectID := firstNonEmptyString(userEventID, conversationID)
	objectURL := "/chat"
	statsEvidence := assistantSignalEvidence("chat", "response_stats", "Current response stats", chatInteractionStatsDetail(result), objectID, objectURL, now, 52)

	if chatMessageLooksLikeNegativeFeedback(message) {
		score := 88
		evidence := []assistantstore.RunSignalEvidence{
			assistantSignalEvidence("chat", "user_feedback", "Operator feedback", truncateAssistantRunText(message, 260), objectID, objectURL, now, score),
			statsEvidence,
		}
		if previousAssistant != "" {
			evidence = append(evidence, assistantSignalEvidence("chat", "assistant_reply", "Previous Assistant reply", truncateAssistantRunText(previousAssistant, 320), objectID, objectURL, now, score-8))
		}
		return assistantstore.SignalSubmitRequest{
			Source:            "chat",
			Kind:              "chat_quality_feedback",
			Title:             "Review subpar chat answer",
			Detail:            "Operator feedback suggests a recent chat answer was wrong, unhelpful, or incomplete.",
			WhyNow:            "The operator gave direct feedback that the Assistant response did not meet the need.",
			Severity:          "warning",
			Surface:           "chat",
			ObjectID:          objectID,
			ObjectURL:         objectURL,
			Score:             score,
			ActionKind:        "task",
			Rationale:         "Chat quality regressions are signals that the harness may need better tools, memory, workflow guidance, or task follow-up.",
			TaskGoal:          "Review the referenced chat exchange, identify why the Assistant response was subpar, and propose a source-neutral improvement to tools, memory, workflows, prompts, or task routing.",
			Evidence:          evidence,
			SafeActions:       []string{"create_task", "useful", "snooze", "dismiss"},
			SuggestedNextStep: "Create follow-up work to inspect the exchange and improve the response path.",
			TTLSeconds:        chatQualitySignalTTLSeconds,
			Fingerprint:       assistantSignalFingerprint("chat", "quality_feedback", firstNonEmptyString(conversationID, objectID), previousAssistant, message),
		}, true
	}

	if strings.EqualFold(result.Source, "program") && strings.HasPrefix(strings.ToLower(currentReply), "error:") {
		score := 84
		return assistantstore.SignalSubmitRequest{
			Source:     "chat",
			Kind:       "chat_action_failed",
			Title:      "Review failed chat action",
			Detail:     "A chat command returned an operational error.",
			WhyNow:     "The operator hit a failure while asking the Assistant to act.",
			Severity:   "warning",
			Surface:    "chat",
			ObjectID:   objectID,
			ObjectURL:  objectURL,
			Score:      score,
			ActionKind: "task",
			Rationale:  "Failed chat actions can reveal broken commands, missing state, or unclear operator recovery paths.",
			TaskGoal:   "Review the failed chat action, reproduce the command path if needed, and decide whether to fix the command, add guidance, or create clearer recovery output.",
			Evidence: []assistantstore.RunSignalEvidence{
				assistantSignalEvidence("chat", "user_message", "User message", truncateAssistantRunText(message, 260), objectID, objectURL, now, score),
				assistantSignalEvidence("chat", "program_error", "Program error", truncateAssistantRunText(currentReply, 320), objectID, objectURL, now, score),
				statsEvidence,
			},
			SafeActions:       []string{"create_task", "useful", "snooze", "dismiss"},
			SuggestedNextStep: "Review the failed command path and decide whether follow-up work is needed.",
			TTLSeconds:        chatQualitySignalTTLSeconds,
			Fingerprint:       assistantSignalFingerprint("chat", "action_failed", firstNonEmptyString(conversationID, objectID), message, currentReply),
		}, true
	}

	if !strings.EqualFold(result.Source, "program") && result.Stats.ModelTurns > 0 && result.Stats.ToolCalls == 0 && chatMessageLikelyNeededTools(message) {
		score := 64
		return assistantstore.SignalSubmitRequest{
			Source:     "chat",
			Kind:       "chat_tool_light_response",
			Title:      "Check tool-light chat response",
			Detail:     "A toolable chat request was answered without tool calls.",
			WhyNow:     "The user asked about state that may need repository, task, knowledge, workflow, or current-data inspection.",
			Severity:   "info",
			Surface:    "chat",
			ObjectID:   objectID,
			ObjectURL:  objectURL,
			Score:      score,
			ActionKind: "observe",
			Rationale:  "Tool-light answers to stateful requests can be useful weak signals for improving routing and tool use.",
			Evidence: []assistantstore.RunSignalEvidence{
				assistantSignalEvidence("chat", "user_message", "User message", truncateAssistantRunText(message, 260), objectID, objectURL, now, score),
				assistantSignalEvidence("chat", "assistant_reply", "Assistant reply", truncateAssistantRunText(currentReply, 320), objectID, objectURL, now, score-8),
				statsEvidence,
			},
			SafeActions:       []string{"useful", "snooze", "dismiss"},
			SuggestedNextStep: "Observe this pattern; create work only if similar weak signals repeat or operator feedback confirms the answer was poor.",
			TTLSeconds:        chatQualitySignalTTLSeconds,
			Fingerprint:       assistantSignalFingerprint("chat", "tool_light", firstNonEmptyString(conversationID, objectID), message),
		}, true
	}

	return assistantstore.SignalSubmitRequest{}, false
}

func lastAssistantMessage(history []llm.Message) string {
	for index := len(history) - 1; index >= 0; index-- {
		if history[index].Role == "assistant" && strings.TrimSpace(history[index].Content) != "" {
			return strings.TrimSpace(history[index].Content)
		}
	}
	return ""
}

func chatMessageLooksLikeNegativeFeedback(message string) bool {
	normalised := " " + strings.ToLower(strings.Join(strings.Fields(message), " ")) + " "
	phrases := []string{
		" that was wrong ",
		" that's wrong ",
		" that is wrong ",
		" this is wrong ",
		" you were wrong ",
		" incorrect ",
		" not right ",
		" not useful ",
		" unhelpful ",
		" bad answer ",
		" poor answer ",
		" subpar ",
		" you missed ",
		" you didn't ",
		" you did not ",
		" try again ",
	}
	for _, phrase := range phrases {
		if strings.Contains(normalised, phrase) {
			return true
		}
	}
	return false
}

func chatMessageLikelyNeededTools(message string) bool {
	normalised := " " + strings.ToLower(strings.Join(strings.Fields(message), " ")) + " "
	phrases := []string{
		" current ",
		" latest ",
		" status ",
		" task ",
		" tasks ",
		" approval ",
		" approvals ",
		" diff ",
		" commit ",
		" branch ",
		" file ",
		" files ",
		" logs ",
		" error ",
		" health ",
		" supervisor ",
		" workflow ",
		" workflows ",
		" knowledge ",
		" where is ",
		" what changed ",
		" show me ",
	}
	for _, phrase := range phrases {
		if strings.Contains(normalised, phrase) {
			return true
		}
	}
	return false
}

func chatInteractionStatsDetail(result HandleResult) string {
	parts := []string{"source " + firstNonEmptyString(result.Source, "unknown")}
	if result.Stats.ModelTurns > 0 {
		parts = append(parts, fmt.Sprintf("%d model turns", result.Stats.ModelTurns))
	}
	if result.Stats.ToolCalls > 0 {
		parts = append(parts, fmt.Sprintf("%d tool calls", result.Stats.ToolCalls))
	} else if result.Stats.ModelTurns > 0 {
		parts = append(parts, "0 tool calls")
	}
	if result.Stats.TotalTokens > 0 {
		parts = append(parts, fmt.Sprintf("%d total tokens", result.Stats.TotalTokens))
	}
	if result.Stats.ElapsedMilliseconds > 0 {
		parts = append(parts, fmt.Sprintf("%d ms elapsed", result.Stats.ElapsedMilliseconds))
	}
	return strings.Join(parts, ", ") + "."
}

func assistantSignalSourceControlError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "assistant signal source") &&
		(strings.Contains(message, "disabled") || strings.Contains(message, "below min_score"))
}
