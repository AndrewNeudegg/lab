package text

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/andrewneudegg/lab/pkg/llm"
	"github.com/andrewneudegg/lab/pkg/tool"
)

const defaultSummaryMaxCharacters = 84

func RegisterSummarizer(reg *tool.Registry, provider llm.Provider, model string) error {
	return reg.Register(SummarizeTool{provider: provider, model: model})
}

type SummarizeTool struct {
	provider llm.Provider
	model    string
}

func NewSummarizeTool(provider llm.Provider, model string) SummarizeTool {
	return SummarizeTool{provider: provider, model: model}
}

func (SummarizeTool) Name() string { return "text.summarize" }
func (SummarizeTool) Description() string {
	return "Summarise user text into a concise label, optionally using the configured LLM provider."
}
func (SummarizeTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["text"],"properties":{"text":{"type":"string"},"purpose":{"type":"string","enum":["task_title","generic"],"description":"task_title drops workflow boilerplate and keeps the concrete work visible"},"max_characters":{"type":"integer","minimum":12,"maximum":200}}}`)
}
func (SummarizeTool) Risk() tool.RiskLevel { return tool.RiskReadOnly }
func (t SummarizeTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Text          string `json:"text"`
		Purpose       string `json:"purpose"`
		MaxCharacters int    `json:"max_characters"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	text := compactWhitespace(req.Text)
	if text == "" {
		return nil, fmt.Errorf("text is required")
	}
	if utf8.RuneCountInString(text) > 8000 {
		return nil, fmt.Errorf("text must be 8000 characters or fewer")
	}
	purpose := strings.ToLower(strings.TrimSpace(req.Purpose))
	if purpose == "" {
		purpose = "generic"
	}
	if purpose != "task_title" && purpose != "generic" {
		return nil, fmt.Errorf("purpose must be task_title or generic")
	}
	maxCharacters := req.MaxCharacters
	if maxCharacters <= 0 {
		maxCharacters = defaultSummaryMaxCharacters
	}
	if maxCharacters < 12 {
		return nil, fmt.Errorf("max_characters must be at least 12")
	}
	if maxCharacters > 200 {
		return nil, fmt.Errorf("max_characters must be 200 or fewer")
	}

	result := SummaryResult{
		Text:          text,
		Purpose:       purpose,
		MaxCharacters: maxCharacters,
	}
	if t.provider == nil || strings.TrimSpace(t.model) == "" {
		result.Summary = fallbackSummary(text, maxCharacters)
		result.Fallback = true
		result.Notes = []string{"No LLM provider was configured for text.summarize; used extractive fallback."}
		return json.Marshal(result)
	}

	summary, providerName, modelName, err := t.llmSummary(ctx, text, purpose, maxCharacters)
	if err != nil {
		result.Summary = fallbackSummary(text, maxCharacters)
		result.Fallback = true
		result.Provider = providerName
		result.Model = modelName
		result.Notes = []string{"LLM summarisation failed: " + err.Error(), "Used extractive fallback."}
		return json.Marshal(result)
	}
	result.Summary = summary
	result.Provider = providerName
	result.Model = modelName
	return json.Marshal(result)
}

type SummaryResult struct {
	Text          string   `json:"text"`
	Summary       string   `json:"summary"`
	Purpose       string   `json:"purpose"`
	MaxCharacters int      `json:"max_characters"`
	Provider      string   `json:"provider,omitempty"`
	Model         string   `json:"model,omitempty"`
	Fallback      bool     `json:"fallback,omitempty"`
	Notes         []string `json:"notes,omitempty"`
}

func (t SummarizeTool) llmSummary(ctx context.Context, text, purpose string, maxCharacters int) (string, string, string, error) {
	resp, err := t.provider.Complete(ctx, llm.CompletionRequest{
		Model:       t.model,
		Temperature: 0,
		MaxTokens:   128,
		Messages: []llm.Message{
			{
				Role: "system",
				Content: strings.Join([]string{
					"You summarise user text into compact UI labels.",
					"Return exactly one JSON object and no prose: {\"summary\":\"...\"}",
					"Preserve concrete product names, code identifiers, and the user's requested outcome.",
					"Remove workflow boilerplate such as instructions to inspect, edit, test, or summarise.",
					"Do not use markdown, surrounding quotes, or trailing punctuation unless it is part of a name.",
				}, "\n"),
			},
			{
				Role:    "user",
				Content: fmt.Sprintf("Purpose: %s\nMaximum characters: %d\nText:\n%s", purpose, maxCharacters, text),
			},
		},
	})
	providerName := strings.TrimSpace(resp.Provider)
	if providerName == "" && t.provider != nil {
		providerName = t.provider.Name()
	}
	modelName := strings.TrimSpace(resp.Model)
	if modelName == "" {
		modelName = strings.TrimSpace(t.model)
	}
	if err != nil {
		return "", providerName, modelName, err
	}
	summary := cleanSummaryText(parseSummaryContent(resp.Message.Content), maxCharacters)
	if summary == "" {
		return "", providerName, modelName, fmt.Errorf("model returned an empty summary")
	}
	return summary, providerName, modelName, nil
}

func parseSummaryContent(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	var parsed struct {
		Summary string `json:"summary"`
		Title   string `json:"title"`
	}
	if err := json.Unmarshal([]byte(content), &parsed); err == nil {
		if strings.TrimSpace(parsed.Summary) != "" {
			return parsed.Summary
		}
		return parsed.Title
	}
	if object := extractFirstJSONObject(content); object != "" {
		if err := json.Unmarshal([]byte(object), &parsed); err == nil {
			if strings.TrimSpace(parsed.Summary) != "" {
				return parsed.Summary
			}
			return parsed.Title
		}
	}
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)
	if idx := strings.IndexAny(content, "\r\n"); idx >= 0 {
		content = content[:idx]
	}
	return content
}

func extractFirstJSONObject(s string) string {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return ""
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(s); i++ {
		ch := s[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			switch ch {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

func fallbackSummary(text string, maxCharacters int) string {
	text = removeTaskTitleBoilerplate(text)
	if sentenceEnd := strings.IndexAny(text, ".!?"); sentenceEnd >= 24 {
		text = text[:sentenceEnd]
	}
	return cleanSummaryText(text, maxCharacters)
}

func cleanSummaryText(value string, maxCharacters int) string {
	value = compactWhitespace(value)
	value = strings.Trim(value, " \t\r\n\"'`")
	for {
		lower := strings.ToLower(value)
		switch {
		case strings.HasPrefix(lower, "summary:"):
			value = strings.TrimSpace(value[len("summary:"):])
		case strings.HasPrefix(lower, "title:"):
			value = strings.TrimSpace(value[len("title:"):])
		default:
			return clipSummary(value, maxCharacters)
		}
	}
}

func removeTaskTitleBoilerplate(value string) string {
	value = compactWhitespace(value)
	for _, prefix := range []string{
		"Work this task to completion if possible.",
		"Inspect the task workspace before editing.",
		"Make a minimal patch that satisfies the task goal.",
	} {
		value = strings.TrimSpace(strings.TrimPrefix(value, prefix))
	}
	for _, marker := range []string{"Task goal:", "Goal:"} {
		if idx := strings.Index(strings.ToLower(value), strings.ToLower(marker)); idx >= 0 {
			value = strings.TrimSpace(value[idx+len(marker):])
			break
		}
	}
	return value
}

func clipSummary(value string, maxCharacters int) string {
	value = compactWhitespace(value)
	if maxCharacters <= 0 || utf8.RuneCountInString(value) <= maxCharacters {
		return value
	}
	runes := []rune(value)
	if maxCharacters <= 3 {
		return string(runes[:maxCharacters])
	}
	limit := maxCharacters - 3
	clipped := strings.TrimSpace(string(runes[:limit]))
	if boundary := strings.LastIndex(clipped, " "); boundary >= limit*3/5 {
		clipped = clipped[:boundary]
	}
	clipped = strings.TrimRight(strings.TrimSpace(clipped), ".,;:-")
	if clipped == "" {
		return strings.TrimSpace(string(runes[:maxCharacters]))
	}
	return clipped + "..."
}
