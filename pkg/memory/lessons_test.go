package memory

import (
	"strings"
	"testing"
	"time"
)

func TestRememberListPromptAndUnlearnLesson(t *testing.T) {
	store := NewStore(t.TempDir())
	createdAt := time.Date(2026, 4, 28, 20, 21, 58, 0, time.UTC)

	lesson, err := store.RememberLesson(DefaultLessonFile, Lesson{
		ID:        "mem_test",
		Kind:      "Preference",
		Source:    "Chat",
		Content:   "Prefer distilled decision rules over copied phrasing.",
		CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if lesson.Kind != "preference" || lesson.Source != "chat" {
		t.Fatalf("lesson = %#v, want normalised kind/source", lesson)
	}

	lessons, err := store.ListLessons(DefaultLessonFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(lessons) != 1 || lessons[0].ID != "mem_test" {
		t.Fatalf("lessons = %#v, want one stored lesson", lessons)
	}

	prompt, err := store.LessonPrompt(DefaultLessonFile, 12)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "Prefer distilled decision rules over copied phrasing.") {
		t.Fatalf("prompt = %q, want lesson content", prompt)
	}

	removed, err := store.UnlearnLesson(DefaultLessonFile, "copied phrasing")
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0].ID != "mem_test" {
		t.Fatalf("removed = %#v, want removed lesson", removed)
	}

	lessons, err = store.ListLessons(DefaultLessonFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(lessons) != 0 {
		t.Fatalf("lessons = %#v, want none after unlearn", lessons)
	}
}

func TestRememberLessonPreservesOtherMarkdownSections(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.CommitWrite(DefaultLessonFile, "# User Memory\n\n## Notes\n\nKeep this section.\n"); err != nil {
		t.Fatal(err)
	}

	if _, err := store.RememberLesson(DefaultLessonFile, Lesson{
		ID:      "mem_keep_notes",
		Content: "Keep unrelated markdown sections intact.",
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := store.Read(DefaultLessonFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, "## Notes\n\nKeep this section.") {
		t.Fatalf("raw memory = %q, want notes section preserved", raw)
	}
	if !strings.Contains(raw, "mem_keep_notes") {
		t.Fatalf("raw memory = %q, want lesson section added", raw)
	}
}
