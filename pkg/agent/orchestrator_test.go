package agent

import "testing"

func TestExtractJSONUsesFirstBalancedObject(t *testing.T) {
	input := `{"message":"List files","done":false,"tool_calls":[{"tool":"repo.list","args":{"workspace":"/tmp/workspaces/task"}}]} trailing prose with {"other":true}`
	got := extractJSON(input)
	want := `{"message":"List files","done":false,"tool_calls":[{"tool":"repo.list","args":{"workspace":"/tmp/workspaces/task"}}]}`
	if got != want {
		t.Fatalf("extractJSON() = %q, want %q", got, want)
	}
}

func TestExtractJSONHandlesBracesInStrings(t *testing.T) {
	input := `prefix {"message":"brace } inside string","done":true,"tool_calls":[]} suffix {"ignored":true}`
	got := extractJSON(input)
	want := `{"message":"brace } inside string","done":true,"tool_calls":[]}`
	if got != want {
		t.Fatalf("extractJSON() = %q, want %q", got, want)
	}
}

func TestNormalizeTaskSelectorRemovesNaturalFiller(t *testing.T) {
	got := normalizeTaskSelector("the hi task")
	if got != "hi" {
		t.Fatalf("normalizeTaskSelector() = %q, want hi", got)
	}
}
