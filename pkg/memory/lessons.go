package memory

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/andrewneudegg/lab/pkg/id"
)

const (
	DefaultLessonFile = "user.md"

	maxLessonContentRunes = 500
)

type Lesson struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Source    string    `json:"source"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Store) RememberLesson(name string, lesson Lesson) (Lesson, error) {
	name = defaultLessonName(name)
	lesson = normaliseLesson(lesson)
	if lesson.Content == "" {
		return Lesson{}, errors.New("lesson content is required")
	}

	raw, err := s.readOptional(name)
	if err != nil {
		return Lesson{}, err
	}
	lessons := parseLessons(raw)
	for _, existing := range lessons {
		if strings.EqualFold(existing.Content, lesson.Content) {
			return existing, nil
		}
	}
	lessons = append(lessons, lesson)
	if err := s.CommitWrite(name, mergeLessonSection(raw, renderLessonSection(lessons))); err != nil {
		return Lesson{}, err
	}
	return lesson, nil
}

func (s *Store) UnlearnLesson(name, selector string) ([]Lesson, error) {
	name = defaultLessonName(name)
	selector = strings.ToLower(strings.TrimSpace(selector))
	if selector == "" {
		return nil, errors.New("selector is required")
	}

	raw, err := s.readOptional(name)
	if err != nil {
		return nil, err
	}
	lessons := parseLessons(raw)
	kept := lessons[:0]
	var removed []Lesson
	for _, lesson := range lessons {
		if strings.EqualFold(lesson.ID, selector) ||
			strings.Contains(strings.ToLower(lesson.Content), selector) {
			removed = append(removed, lesson)
			continue
		}
		kept = append(kept, lesson)
	}
	if len(removed) == 0 {
		return nil, fmt.Errorf("no memory lesson matches %q", selector)
	}
	if err := s.CommitWrite(name, mergeLessonSection(raw, renderLessonSection(kept))); err != nil {
		return nil, err
	}
	return removed, nil
}

func (s *Store) ListLessons(name string) ([]Lesson, error) {
	raw, err := s.readOptional(defaultLessonName(name))
	if err != nil {
		return nil, err
	}
	return parseLessons(raw), nil
}

func (s *Store) LessonPrompt(name string, limit int) (string, error) {
	lessons, err := s.ListLessons(name)
	if err != nil {
		return "", err
	}
	if len(lessons) == 0 {
		return "No durable interaction lessons recorded.", nil
	}
	if limit > 0 && len(lessons) > limit {
		lessons = lessons[len(lessons)-limit:]
	}
	var b strings.Builder
	b.WriteString("Durable interaction lessons (soft guidance; current instructions, user requests, and repo state win):")
	for _, lesson := range lessons {
		fmt.Fprintf(&b, "\n- %s [%s, %s]", lesson.Content, lesson.ID, lesson.Kind)
	}
	return b.String(), nil
}

func (s *Store) readOptional(name string) (string, error) {
	raw, err := s.Read(name)
	if err == nil {
		return raw, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return "", err
}

func defaultLessonName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return DefaultLessonFile
	}
	return name
}

func normaliseLesson(lesson Lesson) Lesson {
	lesson.Content = cleanLessonText(lesson.Content)
	if lesson.ID == "" {
		lesson.ID = id.New("mem")
	}
	if lesson.Kind == "" {
		lesson.Kind = "lesson"
	}
	lesson.Kind = cleanLessonToken(lesson.Kind)
	if lesson.Source == "" {
		lesson.Source = "chat"
	}
	lesson.Source = cleanLessonToken(lesson.Source)
	if lesson.CreatedAt.IsZero() {
		lesson.CreatedAt = time.Now().UTC()
	} else {
		lesson.CreatedAt = lesson.CreatedAt.UTC()
	}
	return lesson
}

func cleanLessonText(value string) string {
	value = strings.ReplaceAll(value, "|", "/")
	value = strings.Join(strings.Fields(value), " ")
	if utf8.RuneCountInString(value) > maxLessonContentRunes {
		runes := []rune(value)
		value = strings.TrimSpace(string(runes[:maxLessonContentRunes]))
	}
	return value
}

func cleanLessonToken(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "|", "-")
	value = strings.Join(strings.Fields(value), "-")
	if value == "" {
		return "lesson"
	}
	return value
}

func parseLessons(raw string) []Lesson {
	var lessons []Lesson
	for _, line := range strings.Split(raw, "\n") {
		lesson, ok := parseLessonLine(line)
		if ok {
			lessons = append(lessons, lesson)
		}
	}
	return lessons
}

func parseLessonLine(line string) (Lesson, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "- ") {
		return Lesson{}, false
	}
	parts := strings.SplitN(strings.TrimSpace(strings.TrimPrefix(line, "- ")), " | ", 5)
	if len(parts) != 5 || !strings.HasPrefix(parts[0], "mem_") {
		return Lesson{}, false
	}
	createdAt, err := time.Parse(time.RFC3339, strings.TrimSpace(parts[3]))
	if err != nil {
		createdAt = time.Time{}
	}
	return normaliseLesson(Lesson{
		ID:        strings.TrimSpace(parts[0]),
		Kind:      strings.TrimSpace(parts[1]),
		Source:    strings.TrimSpace(parts[2]),
		CreatedAt: createdAt,
		Content:   strings.TrimSpace(parts[4]),
	}), true
}

func renderLessonSection(lessons []Lesson) string {
	var b strings.Builder
	b.WriteString("## Lessons\n\n")
	if len(lessons) == 0 {
		b.WriteString("No durable interaction lessons recorded yet.\n")
		return b.String()
	}
	for _, lesson := range lessons {
		lesson = normaliseLesson(lesson)
		fmt.Fprintf(&b, "- %s | %s | %s | %s | %s\n",
			lesson.ID,
			lesson.Kind,
			lesson.Source,
			lesson.CreatedAt.Format(time.RFC3339),
			lesson.Content,
		)
	}
	return b.String()
}

func mergeLessonSection(raw, section string) string {
	raw = strings.TrimRight(raw, "\n")
	if strings.TrimSpace(raw) == "" {
		return defaultLessonPreamble() + "\n\n" + strings.TrimRight(section, "\n") + "\n"
	}

	lines := strings.Split(raw, "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "## Lessons" {
			start = i
			break
		}
	}
	if start == -1 {
		prefix := cleanLegacyLessonPlaceholder(raw)
		if strings.TrimSpace(prefix) == "" {
			prefix = defaultLessonPreamble()
		}
		return strings.TrimRight(prefix, "\n") + "\n\n" + strings.TrimRight(section, "\n") + "\n"
	}

	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "## ") {
			end = i
			break
		}
	}

	prefix := strings.TrimRight(strings.Join(lines[:start], "\n"), "\n")
	if strings.TrimSpace(prefix) == "" {
		prefix = defaultLessonPreamble()
	}
	suffix := strings.TrimLeft(strings.Join(lines[end:], "\n"), "\n")

	out := strings.TrimRight(prefix, "\n") + "\n\n" + strings.TrimRight(section, "\n")
	if strings.TrimSpace(suffix) != "" {
		out += "\n\n" + strings.TrimRight(suffix, "\n")
	}
	return out + "\n"
}

func cleanLegacyLessonPlaceholder(raw string) string {
	var kept []string
	for _, line := range strings.Split(raw, "\n") {
		switch strings.TrimSpace(line) {
		case "No durable user preferences recorded yet.", "No durable interaction lessons recorded yet.":
			continue
		default:
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

func defaultLessonPreamble() string {
	return strings.Join([]string{
		"# User Memory",
		"",
		"Durable preferences and interaction lessons. Use them as soft guidance; current instructions, user requests, and repo state take precedence.",
	}, "\n")
}
