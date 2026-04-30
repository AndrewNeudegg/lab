package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/andrewneudegg/lab/pkg/llm"
)

type agentResponseStrategy string

const (
	agentResponseStrategyNativeSchema    agentResponseStrategy = "native_json_schema"
	agentResponseStrategyFinalSubmitTool agentResponseStrategy = "final_submit_tool"
	agentResponseStrategyPromptJSON      agentResponseStrategy = "prompt_json"
)

const agentResponseJSONSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "additionalProperties": false,
  "required": ["message", "done", "tool_calls"],
  "properties": {
    "message": {
      "type": "string",
      "minLength": 1,
      "description": "Required. Short user-facing status while using tools, or the final concrete answer/result/blocker. Do not put a plan, promise, placeholder, or label without content here."
    },
    "done": {
      "type": "boolean",
      "description": "Required. true only when this response is the final answer and no more tool calls are needed."
    },
    "tool_calls": {
      "type": "array",
      "description": "Required. Use [] when done is true. When done is false, include at least one tool call.",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["tool", "args"],
        "properties": {
          "tool": {
            "type": "string",
            "minLength": 1,
            "description": "Exact tool name from the available tool catalogue. Use the key \"tool\", not \"name\"."
          },
          "args": {
            "type": "object",
            "description": "JSON object matching that tool's argument schema. Use {} only when the tool takes no arguments."
          }
        }
      }
    }
  },
  "allOf": [
    {
      "if": {"properties": {"done": {"const": true}}},
      "then": {"properties": {"tool_calls": {"maxItems": 0}}}
    },
    {
      "if": {"properties": {"done": {"const": false}}},
      "then": {"properties": {"tool_calls": {"minItems": 1}}}
    }
  ]
}`

const agentResponseProviderJSONSchema = `{
  "type": "object",
  "required": ["message", "done", "tool_calls"],
  "properties": {
    "message": {
      "type": "string",
      "description": "A concrete status or final answer. Never return a placeholder, joke, capability statement, or prose outside this JSON object."
    },
    "done": {
      "type": "boolean",
      "description": "true only for the final answer. false when tool_calls contains work to run."
    },
    "tool_calls": {
      "type": "array",
      "description": "Use [] only when done is true. When done is false, include at least one tool call.",
      "items": {
        "type": "object",
        "required": ["tool", "args"],
        "properties": {
          "tool": {
            "type": "string",
            "description": "Exact tool name from the catalogue. The key must be tool, not name."
          },
          "args": {
            "type": "object",
            "description": "A JSON object matching the selected tool schema. Use {} only for tools with no required arguments."
          }
        }
      }
    }
  }
}`

func agentResponseFormat() *llm.ResponseFormat {
	return &llm.ResponseFormat{
		Name:        "homelabd_agent_response",
		Description: "Strict homelabd agent response envelope for tool use and final answers.",
		Schema:      json.RawMessage(agentResponseProviderJSONSchema),
		// Tool args are dynamic per tool, so provider-native schemas can only constrain
		// the envelope. The runtime validator below remains the strict authority.
		Strict: false,
	}
}

func agentResponseStrategyFor(provider llm.Provider) agentResponseStrategy {
	caps := llm.CapabilitiesOf(provider)
	if caps.NativeJSONSchema {
		return agentResponseStrategyNativeSchema
	}
	if caps.ToolCalling {
		return agentResponseStrategyFinalSubmitTool
	}
	return agentResponseStrategyPromptJSON
}

func agentCompletionRequest(provider llm.Provider, model string, maxTokens int, messages []llm.Message) llm.CompletionRequest {
	strategy := agentResponseStrategyFor(provider)
	req := llm.CompletionRequest{
		Model:       model,
		Temperature: 0,
		MaxTokens:   maxTokens,
		Messages:    append([]llm.Message(nil), messages...),
	}
	switch strategy {
	case agentResponseStrategyNativeSchema:
		req.ResponseFormat = agentResponseFormat()
	case agentResponseStrategyFinalSubmitTool:
		req.Tools = []llm.ToolSpec{finalSubmitToolSpec()}
		req.Messages = append(req.Messages, llm.Message{Role: "system", Content: finalSubmitToolPrompt()})
	}
	return req
}

func finalSubmitToolSpec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "final.submit",
		Description: "Submit the final concrete answer when no more tool calls are needed. Use the JSON envelope for non-final tool work.",
		Schema: json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["message"],
  "properties": {
    "message": {
      "type": "string",
      "minLength": 1,
      "description": "The final concrete answer or task summary. Do not submit placeholders, jokes, or promises to do work later."
    }
  }
}`),
	}
}

func finalSubmitToolPrompt() string {
	return strings.Join([]string{
		"Provider tool-call fallback:",
		"- When your answer is final and no more homelabd tools are needed, call final.submit with {\"message\":\"...\"}.",
		"- For non-final work that needs homelabd tools, return the normal JSON envelope with done=false and tool_calls.",
		"- Do not call final.submit for a placeholder, joke, capability statement, or promise to act later.",
	}, "\n")
}

type agentResponseValidationError struct {
	Issues []string
}

func (e agentResponseValidationError) Error() string {
	issues := compactIssueList(e.Issues)
	if len(issues) == 0 {
		return "invalid agent response: response does not match the required JSON schema"
	}
	return "invalid agent response: " + strings.Join(issues, "; ")
}

func agentResponseSchemaPrompt() string {
	return strings.Join([]string{
		"Response contract:",
		"- Return exactly one raw JSON object. Do not wrap it in Markdown fences and do not add prose before or after it.",
		"- The JSON object must validate against this JSON Schema:",
		agentResponseJSONSchema,
		"Important validation rules:",
		"- Include all three top-level keys exactly: message, done, tool_calls.",
		"- Unknown keys are rejected.",
		"- done=true requires tool_calls=[].",
		"- done=false requires at least one tool call.",
		"- Every tool call must use {\"tool\":\"name\",\"args\":{...}}. The key \"name\" is invalid.",
		"- Use the key \"tool\", not \"name\".",
		"- args must always be a JSON object, never a string, array, null, or omitted.",
	}, "\n")
}

func agentResponseValidationRetryPrompt(err error) string {
	detail := "response does not match the required schema"
	if err != nil {
		detail = err.Error()
	}
	return strings.Join([]string{
		"The previous candidate response was rejected before any tool execution.",
		"Validation error: " + detail,
		"Regenerate the response now using exactly the required JSON response contract.",
		agentResponseSchemaPrompt(),
	}, "\n")
}

func parseAgentResponse(content string) (agentResponse, error) {
	raw, err := strictAgentResponseJSON(content)
	if err != nil {
		return agentResponse{}, err
	}
	var response agentResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return agentResponse{}, err
	}
	if err := validateAgentResponseRaw(raw, response); err != nil {
		return agentResponse{}, err
	}
	return response, nil
}

func parseAgentResponseFromCompletion(resp llm.CompletionResponse, strategy agentResponseStrategy) (agentResponse, error) {
	parsed, err := parseAgentResponse(resp.Message.Content)
	if err == nil {
		return parsed, nil
	}
	if strategy == agentResponseStrategyFinalSubmitTool && len(resp.ToolCalls) > 0 {
		toolParsed, toolErr := parseFinalSubmitToolCall(resp.ToolCalls)
		if toolErr == nil {
			return toolParsed, nil
		}
		return agentResponse{}, agentResponseValidationError{Issues: []string{err.Error(), toolErr.Error()}}
	}
	return agentResponse{}, err
}

func parseFinalSubmitToolCall(calls []llm.ToolCall) (agentResponse, error) {
	var issues []string
	var finalCall *llm.ToolCall
	for i := range calls {
		call := calls[i]
		switch strings.TrimSpace(call.Name) {
		case "final.submit":
			if finalCall != nil {
				issues = append(issues, "final.submit may be called at most once")
				continue
			}
			finalCall = &call
		case "":
			issues = append(issues, "provider returned a tool call without a name")
		default:
			issues = append(issues, fmt.Sprintf("provider returned unsupported tool call %q", call.Name))
		}
	}
	if finalCall == nil {
		issues = append(issues, "provider did not call final.submit")
	}
	if len(issues) > 0 {
		return agentResponse{}, agentResponseValidationError{Issues: issues}
	}
	issues = append(issues, validateJSONAgainstSchema("final.submit args", finalCall.Args, finalSubmitToolSpec().Schema)...)
	if len(issues) > 0 {
		return agentResponse{}, agentResponseValidationError{Issues: issues}
	}
	var args struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(finalCall.Args, &args); err != nil {
		return agentResponse{}, agentResponseValidationError{Issues: []string{"final.submit args are not valid JSON: " + err.Error()}}
	}
	message := strings.TrimSpace(args.Message)
	if message == "" {
		return agentResponse{}, agentResponseValidationError{Issues: []string{"final.submit.message must be non-empty"}}
	}
	return agentResponse{Message: message, Done: true, ToolCalls: []proposedToolCall{}}, nil
}

func strictAgentResponseJSON(content string) ([]byte, error) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return nil, agentResponseValidationError{Issues: []string{"response is empty"}}
	}
	if strings.HasPrefix(trimmed, "```") {
		return nil, agentResponseValidationError{Issues: []string{"response must be a raw JSON object, not a Markdown code fence"}}
	}
	if !strings.HasPrefix(trimmed, "{") {
		return nil, agentResponseValidationError{Issues: []string{"response must start with a JSON object"}}
	}
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.UseNumber()
	var value json.RawMessage
	if err := decoder.Decode(&value); err != nil {
		return nil, agentResponseValidationError{Issues: []string{"response is not valid JSON: " + err.Error()}}
	}
	var trailing struct{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, agentResponseValidationError{Issues: []string{"response must contain exactly one JSON object and no trailing prose"}}
	}
	if len(value) == 0 || !bytes.HasPrefix(bytes.TrimSpace(value), []byte("{")) {
		return nil, agentResponseValidationError{Issues: []string{"response must be a JSON object"}}
	}
	return bytes.TrimSpace(value), nil
}

func validateAgentResponseRaw(raw []byte, response agentResponse) error {
	var issues []string
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return agentResponseValidationError{Issues: []string{"response is not a JSON object"}}
	}
	allowedTop := map[string]bool{"message": true, "done": true, "tool_calls": true}
	for key := range top {
		if !allowedTop[key] {
			issues = append(issues, fmt.Sprintf("unknown top-level key %q", key))
		}
	}
	for _, key := range []string{"message", "done", "tool_calls"} {
		if _, ok := top[key]; !ok {
			issues = append(issues, fmt.Sprintf("missing required key %q", key))
		}
	}
	if rawMessage, ok := top["message"]; ok {
		var message string
		if err := json.Unmarshal(rawMessage, &message); err != nil {
			issues = append(issues, "message must be a string")
		} else if strings.TrimSpace(message) == "" {
			issues = append(issues, "message must be a non-empty string")
		}
	}
	if rawDone, ok := top["done"]; ok {
		var done bool
		if err := json.Unmarshal(rawDone, &done); err != nil {
			issues = append(issues, "done must be a boolean")
		}
	}
	if rawCalls, ok := top["tool_calls"]; ok {
		var calls []json.RawMessage
		if err := json.Unmarshal(rawCalls, &calls); err != nil {
			issues = append(issues, "tool_calls must be an array")
		} else {
			if response.Done && len(calls) > 0 {
				issues = append(issues, "done=true requires tool_calls to be []")
			}
			if !response.Done && len(calls) == 0 {
				issues = append(issues, "done=false requires at least one tool call")
			}
			for i, rawCall := range calls {
				issues = append(issues, validateAgentToolCallRaw(i, rawCall)...)
			}
		}
	}
	if len(issues) > 0 {
		return agentResponseValidationError{Issues: issues}
	}
	return nil
}

func validateAgentToolCallRaw(index int, raw json.RawMessage) []string {
	prefix := fmt.Sprintf("tool_calls[%d]", index)
	var issues []string
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return []string{prefix + " must be an object"}
	}
	if len(obj) == 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("{}")) {
		return []string{prefix + " must be an object"}
	}
	allowed := map[string]bool{"tool": true, "args": true}
	for key := range obj {
		if !allowed[key] {
			issues = append(issues, fmt.Sprintf("%s has unknown key %q", prefix, key))
		}
	}
	rawTool, ok := obj["tool"]
	if !ok {
		issues = append(issues, prefix+`.tool is required; use "tool", not "name"`)
	} else {
		var name string
		if err := json.Unmarshal(rawTool, &name); err != nil {
			issues = append(issues, prefix+".tool must be a string")
		} else if strings.TrimSpace(name) == "" {
			issues = append(issues, prefix+".tool must be a non-empty string")
		}
	}
	rawArgs, ok := obj["args"]
	if !ok {
		issues = append(issues, prefix+".args is required and must be an object")
	} else if !bytes.HasPrefix(bytes.TrimSpace(rawArgs), []byte("{")) {
		issues = append(issues, prefix+".args must be a JSON object")
	} else {
		var args map[string]json.RawMessage
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			issues = append(issues, prefix+".args must be a valid JSON object")
		}
	}
	return issues
}

func compactIssueList(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
