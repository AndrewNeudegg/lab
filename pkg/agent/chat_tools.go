package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/andrewneudegg/lab/pkg/eventlog"
	"github.com/andrewneudegg/lab/pkg/id"
)

const (
	defaultChatHistoryLimit = 40
	defaultChatSearchLimit  = 50
	defaultChatSearchDays   = 7
	maxChatToolLimit        = 200
	maxChatToolDays         = 30
)

type chatHistoryToolRequest struct {
	Date                     string `json:"date,omitempty"`
	Days                     int    `json:"days,omitempty"`
	Limit                    int    `json:"limit,omitempty"`
	ExcludeEventID           string `json:"-"`
	ExcludeLatestUserMessage string `json:"-"`
	ConversationID           string `json:"-"`
}

type chatSearchToolRequest struct {
	Query                    string `json:"query"`
	Date                     string `json:"date,omitempty"`
	Days                     int    `json:"days,omitempty"`
	Limit                    int    `json:"limit,omitempty"`
	ExcludeEventID           string `json:"-"`
	ExcludeLatestUserMessage string `json:"-"`
	ConversationID           string `json:"-"`
}

type chatSendToolRequest struct {
	Message string `json:"message"`
	To      string `json:"to,omitempty"`
}

type chatMetaEntry struct {
	EventID         string `json:"event_id"`
	Time            string `json:"time"`
	Type            string `json:"type"`
	Role            string `json:"role"`
	Actor           string `json:"actor"`
	ConversationID  string `json:"conversation_id,omitempty"`
	To              string `json:"to,omitempty"`
	TaskID          string `json:"task_id,omitempty"`
	Message         string `json:"message"`
	AttachmentCount int    `json:"attachment_count,omitempty"`
}

func (o *Orchestrator) handleChatMetaCommand(ctx context.Context, fields []string, message string) (string, error) {
	if len(fields) < 2 {
		return "usage: chat <history|search>", nil
	}
	switch commandWord(fields[1]) {
	case "history", "log", "stream":
		req := chatHistoryToolRequest{Limit: chatCommandLimit(fields[2:], defaultChatHistoryLimit), ExcludeLatestUserMessage: message}
		applyCurrentChatTurn(ctx, &req.ExcludeEventID, &req.ConversationID)
		return o.formatChatHistoryReply(req)
	case "search", "find":
		query := strings.TrimSpace(strings.TrimPrefix(message, fields[0]+" "+fields[1]))
		if query == "" {
			return "usage: chat search <text>", nil
		}
		req := chatSearchToolRequest{Query: query, ExcludeLatestUserMessage: message}
		applyCurrentChatTurn(ctx, &req.ExcludeEventID, &req.ConversationID)
		return o.formatChatSearchReply(req)
	default:
		return "usage: chat <history|search>", nil
	}
}

func applyCurrentChatTurn(ctx context.Context, excludeEventID, conversationID *string) {
	if turn, ok := currentChatTurnFromContext(ctx); ok {
		*excludeEventID = turn.UserEventID
		*conversationID = turn.ConversationID
	}
}

func (o *Orchestrator) executeChatTool(ctx context.Context, actor, name string, raw json.RawMessage) toolExecution {
	result := toolExecution{Tool: name, Allowed: true}
	var payload json.RawMessage
	var err error
	switch name {
	case "chat.history":
		var req chatHistoryToolRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			return toolExecution{Tool: name, Allowed: false, Error: err.Error()}
		}
		applyCurrentChatTurn(ctx, &req.ExcludeEventID, &req.ConversationID)
		payload, err = o.chatHistoryPayload(req)
	case "chat.search":
		var req chatSearchToolRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			return toolExecution{Tool: name, Allowed: false, Error: err.Error()}
		}
		applyCurrentChatTurn(ctx, &req.ExcludeEventID, &req.ConversationID)
		payload, err = o.chatSearchPayload(req)
	case "chat.send":
		var req chatSendToolRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			return toolExecution{Tool: name, Allowed: false, Error: err.Error()}
		}
		payload, err = o.chatSendPayload(ctx, req)
	default:
		return toolExecution{Tool: name, Allowed: false, Error: "chat tool not registered"}
	}
	if err != nil {
		result.Error = err.Error()
	} else {
		result.Result = payload
	}
	if o.events != nil {
		_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "tool.result", Actor: actor, Payload: eventlog.Payload(result)})
	}
	return result
}

func (o *Orchestrator) formatChatHistoryReply(req chatHistoryToolRequest) (string, error) {
	payload, err := o.chatHistoryPayload(req)
	if err != nil {
		return "", err
	}
	var result struct {
		Entries []chatMetaEntry `json:"entries"`
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		return "", err
	}
	if len(result.Entries) == 0 {
		return "No recorded chat messages found.", nil
	}
	lines := []string{"Recent chat history:"}
	for _, entry := range result.Entries {
		lines = append(lines, chatMetaEntryLine(entry))
	}
	return strings.Join(lines, "\n"), nil
}

func (o *Orchestrator) formatChatSearchReply(req chatSearchToolRequest) (string, error) {
	payload, err := o.chatSearchPayload(req)
	if err != nil {
		return "", err
	}
	var result struct {
		Query        string          `json:"query"`
		Entries      []chatMetaEntry `json:"entries"`
		TotalMatches int             `json:"total_matches"`
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		return "", err
	}
	if len(result.Entries) == 0 {
		return fmt.Sprintf("No recorded chat messages matched %q.", result.Query), nil
	}
	lines := []string{fmt.Sprintf("Chat matches for %q:", result.Query)}
	for _, entry := range result.Entries {
		lines = append(lines, chatMetaEntryLine(entry))
	}
	if result.TotalMatches > len(result.Entries) {
		lines = append(lines, fmt.Sprintf("... %d older match(es) omitted.", result.TotalMatches-len(result.Entries)))
	}
	return strings.Join(lines, "\n"), nil
}

func (o *Orchestrator) chatHistoryPayload(req chatHistoryToolRequest) (json.RawMessage, error) {
	entries, days, limit, date, err := o.chatMetaEntries(req.Date, req.Days, req.Limit, defaultChatHistoryLimit, 1)
	if err != nil {
		return nil, err
	}
	entries = filterChatEntriesByConversation(entries, req.ConversationID)
	entries = excludeCurrentChatTurn(entries, req.ExcludeEventID, req.ExcludeLatestUserMessage)
	total := len(entries)
	if len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}
	return eventlog.Payload(map[string]any{
		"date":      date.Format("2006-01-02"),
		"days":      days,
		"limit":     limit,
		"count":     len(entries),
		"total":     total,
		"truncated": total > len(entries),
		"entries":   entries,
	}), nil
}

func (o *Orchestrator) chatSearchPayload(req chatSearchToolRequest) (json.RawMessage, error) {
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return nil, errors.New("query is required")
	}
	entries, days, limit, date, err := o.chatMetaEntries(req.Date, req.Days, req.Limit, defaultChatSearchLimit, defaultChatSearchDays)
	if err != nil {
		return nil, err
	}
	entries = filterChatEntriesByConversation(entries, req.ConversationID)
	entries = excludeCurrentChatTurn(entries, req.ExcludeEventID, req.ExcludeLatestUserMessage)
	needle := strings.ToLower(query)
	var matches []chatMetaEntry
	for _, entry := range entries {
		if strings.Contains(strings.ToLower(entry.Message), needle) ||
			strings.Contains(strings.ToLower(entry.Actor), needle) ||
			strings.Contains(strings.ToLower(entry.To), needle) {
			matches = append(matches, entry)
		}
	}
	total := len(matches)
	if len(matches) > limit {
		matches = matches[len(matches)-limit:]
	}
	return eventlog.Payload(map[string]any{
		"query":         query,
		"date":          date.Format("2006-01-02"),
		"days":          days,
		"limit":         limit,
		"count":         len(matches),
		"total_matches": total,
		"truncated":     total > len(matches),
		"entries":       matches,
	}), nil
}

func (o *Orchestrator) chatSendPayload(ctx context.Context, req chatSendToolRequest) (json.RawMessage, error) {
	message := strings.TrimSpace(req.Message)
	if message == "" {
		return nil, errors.New("message is required")
	}
	to := strings.TrimSpace(req.To)
	if to == "" {
		to = "chat"
	}
	o.appendChatReply(ctx, to, message)
	return eventlog.Payload(map[string]any{"message": message, "to": to}), nil
}

func (o *Orchestrator) chatMetaEntries(dateText string, requestedDays, requestedLimit, defaultLimit, defaultDays int) ([]chatMetaEntry, int, int, time.Time, error) {
	if o.events == nil {
		return nil, 0, 0, time.Time{}, errors.New("event log is not configured")
	}
	date, err := chatToolDate(dateText)
	if err != nil {
		return nil, 0, 0, time.Time{}, err
	}
	days := clampChatToolValue(requestedDays, defaultDays, maxChatToolDays)
	limit := clampChatToolValue(requestedLimit, defaultLimit, maxChatToolLimit)
	start := date.AddDate(0, 0, -(days - 1))
	var entries []chatMetaEntry
	for day := start; !day.After(date); day = day.AddDate(0, 0, 1) {
		events, err := o.events.ReadDay(day)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, 0, 0, time.Time{}, err
		}
		for _, event := range events {
			entry, ok := chatMetaEntryFromEvent(event)
			if ok {
				entries = append(entries, entry)
			}
		}
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Time < entries[j].Time })
	return entries, days, limit, date, nil
}

func chatMetaEntryFromEvent(event eventlog.Event) (chatMetaEntry, bool) {
	var role string
	switch event.Type {
	case "user.message":
		role = "user"
	case "chat.reply":
		role = "assistant"
	default:
		return chatMetaEntry{}, false
	}
	var payload struct {
		Message        string            `json:"message"`
		Content        string            `json:"content"`
		Reply          string            `json:"reply"`
		To             string            `json:"to"`
		ConversationID string            `json:"conversation_id"`
		Attachments    []json.RawMessage `json:"attachments"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return chatMetaEntry{}, false
	}
	message := strings.TrimSpace(payload.Message)
	if message == "" {
		message = strings.TrimSpace(payload.Content)
	}
	if message == "" {
		message = strings.TrimSpace(payload.Reply)
	}
	if message == "" {
		return chatMetaEntry{}, false
	}
	return chatMetaEntry{
		EventID:         event.ID,
		Time:            event.Time.UTC().Format(time.RFC3339),
		Type:            event.Type,
		Role:            role,
		Actor:           event.Actor,
		ConversationID:  strings.TrimSpace(payload.ConversationID),
		To:              strings.TrimSpace(payload.To),
		TaskID:          event.TaskID,
		Message:         message,
		AttachmentCount: len(payload.Attachments),
	}, true
}

func filterChatEntriesByConversation(entries []chatMetaEntry, conversationID string) []chatMetaEntry {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return entries
	}
	filtered := make([]chatMetaEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.ConversationID == conversationID {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func chatEventConversationID(event eventlog.Event) string {
	var payload struct {
		ConversationID string `json:"conversation_id"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.ConversationID)
}

func excludeLatestUserMessage(entries []chatMetaEntry, message string) []chatMetaEntry {
	message = strings.TrimSpace(message)
	if message == "" || len(entries) == 0 {
		return entries
	}
	last := entries[len(entries)-1]
	if last.Role != "user" || strings.TrimSpace(last.Message) != message {
		return entries
	}
	return entries[:len(entries)-1]
}

func excludeCurrentChatTurn(entries []chatMetaEntry, eventID, fallbackMessage string) []chatMetaEntry {
	eventID = strings.TrimSpace(eventID)
	if eventID != "" {
		filtered := make([]chatMetaEntry, 0, len(entries))
		for _, entry := range entries {
			if entry.EventID == eventID {
				continue
			}
			filtered = append(filtered, entry)
		}
		return filtered
	}
	return excludeLatestUserMessage(entries, fallbackMessage)
}

func chatToolDate(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		now := time.Now().UTC()
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC), nil
	}
	day, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}, fmt.Errorf("date must use YYYY-MM-DD: %w", err)
	}
	return day, nil
}

func clampChatToolValue(value, fallback, max int) int {
	if fallback <= 0 {
		fallback = 1
	}
	if value <= 0 {
		value = fallback
	}
	if value > max {
		return max
	}
	return value
}

func chatCommandLimit(fields []string, fallback int) int {
	if len(fields) == 0 {
		return fallback
	}
	value, err := strconv.Atoi(strings.TrimSpace(fields[0]))
	if err != nil {
		return fallback
	}
	return clampChatToolValue(value, fallback, maxChatToolLimit)
}

func isChatSearchRequest(message string) bool {
	normalized := normalizeIntentText(message)
	return strings.HasPrefix(normalized, "search chat ") ||
		strings.HasPrefix(normalized, "search the chat ") ||
		strings.HasPrefix(normalized, "search chat history ") ||
		strings.HasPrefix(normalized, "search conversation ") ||
		strings.HasPrefix(normalized, "search the conversation ")
}

func chatSearchQueryFromCommand(fields []string) string {
	var kept []string
	for _, field := range fields {
		word := strings.ToLower(strings.Trim(field, " \t\r\n.,!?"))
		if len(kept) == 0 {
			switch word {
			case "the", "chat", "conversation", "history", "messages", "for":
				continue
			}
		}
		kept = append(kept, field)
	}
	if len(kept) > 0 && strings.EqualFold(strings.Trim(kept[0], ".,!?"), "for") {
		kept = kept[1:]
	}
	return strings.TrimSpace(strings.Join(kept, " "))
}

func chatMetaEntryLine(entry chatMetaEntry) string {
	when := entry.Time
	if parsed, err := time.Parse(time.RFC3339, entry.Time); err == nil {
		when = parsed.UTC().Format("2006-01-02 15:04")
	}
	label := entry.Role
	if entry.Actor != "" {
		label += " " + entry.Actor
	}
	if entry.To != "" {
		label += " -> " + entry.To
	}
	return fmt.Sprintf("- %s %s: %s", when, label, chatPreview(entry.Message))
}

func chatPreview(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if len(value) <= 240 {
		return value
	}
	return strings.TrimSpace(value[:240]) + "..."
}
