package control

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/andrewneudegg/lab/pkg/assistant"
	"github.com/andrewneudegg/lab/pkg/eventlog"
	knowledgestore "github.com/andrewneudegg/lab/pkg/knowledge"
	taskstore "github.com/andrewneudegg/lab/pkg/task"
)

func TestSummarizeTaskForListRemovesHeavyArtefacts(t *testing.T) {
	task := taskstore.Task{
		ID:         "task_1",
		Goal:       strings.Repeat("goal ", 400),
		Result:     strings.Repeat("result ", 400),
		RemoteDiff: strings.Repeat("diff", 2000),
		Attachments: []taskstore.Attachment{
			{ID: "att_1", Name: "context.txt", Text: "secret", DataURL: "data:text/plain;base64,abc"},
		},
		DiffSnapshot: &taskstore.TaskDiffSnapshot{
			RawDiff: strings.Repeat("raw", 2000),
			Summary: taskstore.TaskDiffSnapshotSummary{
				Files:     1,
				Additions: 2,
				Deletions: 3,
			},
			CapturedAt: time.Now().UTC(),
		},
	}

	summary := summarizeTaskForList(task)

	if summary.RemoteDiff != "" {
		t.Fatalf("remote diff should be omitted from task lists")
	}
	if !summary.SummaryOnly {
		t.Fatalf("task list items should advertise that full detail is available separately")
	}
	if summary.DiffSnapshot == nil || summary.DiffSnapshot.RawDiff != "" {
		t.Fatalf("raw diff should be omitted from task lists")
	}
	if summary.Attachments[0].Text != "" || summary.Attachments[0].DataURL != "" {
		t.Fatalf("attachment bodies should be omitted from task lists")
	}
	if len(summary.Result) >= len(task.Result) {
		t.Fatalf("task result should be truncated in task lists")
	}
}

func TestSummarizeAssistantRunForListRemovesSnapshotBody(t *testing.T) {
	run := assistant.Run{
		ID:      "run_1",
		Summary: strings.Repeat("summary ", 400),
		Snapshot: assistant.RunSnapshot{
			GeneratedAt: time.Now().UTC(),
			TaskCounts:  map[string]int{"done": 3},
			Signals: []assistant.RunSignal{
				{ID: "sig_1", Title: "Signal", Detail: strings.Repeat("detail ", 400)},
			},
			AttentionTasks: []assistant.RunObjectRef{
				{ID: "task_1", Title: "Task", Summary: strings.Repeat("attention ", 400)},
			},
		},
		RecommendedActions: []assistant.RunAction{
			{ID: "action_1", Kind: "task", Title: "Build", Rationale: strings.Repeat("rationale ", 400)},
		},
	}

	summary := summarizeAssistantRunForList(run)

	if len(summary.Snapshot.Signals) != 0 {
		t.Fatalf("snapshot signals should be omitted from assistant run lists")
	}
	if summary.Snapshot.TaskCounts["done"] != 3 {
		t.Fatalf("snapshot counts should remain available in assistant run lists")
	}
	if len(summary.RecommendedActions[0].Rationale) >= len(run.RecommendedActions[0].Rationale) {
		t.Fatalf("recommended action rationale should be truncated in assistant run lists")
	}
	if len(summary.Snapshot.AttentionTasks[0].Summary) >= len(run.Snapshot.AttentionTasks[0].Summary) {
		t.Fatalf("snapshot refs should be truncated in assistant run lists")
	}
}

func TestSummarizeKnowledgeSpaceForListRemovesSourceBodies(t *testing.T) {
	space := knowledgestore.Space{
		ID:    "space_1",
		Title: "Research",
		Sources: []knowledgestore.Source{
			{
				ID:       "source_1",
				Title:    "Source",
				Content:  strings.Repeat("content ", 400),
				Summary:  strings.Repeat("summary ", 400),
				Sections: []knowledgestore.SourceSection{{ID: "section_1", Text: "body"}},
				Chunks:   []knowledgestore.SourceChunk{{ID: "chunk_1", Text: "body"}},
			},
		},
		Reports: []knowledgestore.Report{
			{ID: "report_1", Answer: strings.Repeat("answer ", 400)},
		},
	}

	summary := summarizeKnowledgeSpaceForList(space)

	if summary.Sources[0].Content != "" {
		t.Fatalf("source content should be omitted from knowledge space lists")
	}
	if len(summary.Sources[0].Sections) != 0 || len(summary.Sources[0].Chunks) != 0 {
		t.Fatalf("source sections and chunks should be omitted from knowledge space lists")
	}
	if len(summary.Reports[0].Answer) >= len(space.Reports[0].Answer) {
		t.Fatalf("report answers should be truncated in knowledge space lists")
	}
}

func TestSummarizeEventForListCollapsesLargePayload(t *testing.T) {
	original := map[string]string{"result": strings.Repeat("completed ", 600)}
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	event := eventlog.Event{
		ID:      "event_1",
		Type:    "task.completed",
		Payload: raw,
	}

	summary := summarizeEventForList(event)

	if len(summary.Payload) >= len(event.Payload) {
		t.Fatalf("large event payload should be collapsed for event lists")
	}
	var payload map[string]any
	if err := json.Unmarshal(summary.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["truncated"] != true {
		t.Fatalf("summarized event payload should be marked as truncated")
	}
	if payload["summary"] == "" {
		t.Fatalf("summarized event payload should include a readable summary")
	}
}
