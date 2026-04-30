package knowledge

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

const (
	SourceKindText = "text"
	SourceKindURL  = "url"
	SourceKindFile = "file"
	SourceKindNote = "note"

	ReportModeResearch = "research"
	ReportModeBrief    = "brief"
	ReportModeStudy    = "study"
)

type Space struct {
	ID          string       `json:"id"`
	Title       string       `json:"title"`
	Description string       `json:"description,omitempty"`
	Objective   string       `json:"objective,omitempty"`
	Sources     []Source     `json:"sources,omitempty"`
	Reports     []Report     `json:"reports,omitempty"`
	Insight     SpaceInsight `json:"insight"`
	CreatedBy   string       `json:"created_by,omitempty"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

type SpaceInsight struct {
	SourceCount        int       `json:"source_count"`
	WordCount          int       `json:"word_count"`
	KeyTerms           []string  `json:"key_terms,omitempty"`
	SuggestedQuestions []string  `json:"suggested_questions,omitempty"`
	UpdatedAt          time.Time `json:"updated_at,omitempty"`
}

type Source struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Kind      string    `json:"kind"`
	URI       string    `json:"uri,omitempty"`
	Content   string    `json:"content"`
	Summary   string    `json:"summary"`
	KeyTerms  []string  `json:"key_terms,omitempty"`
	Questions []string  `json:"questions,omitempty"`
	WordCount int       `json:"word_count"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Report struct {
	ID          string     `json:"id"`
	Question    string     `json:"question"`
	Mode        string     `json:"mode"`
	Answer      string     `json:"answer"`
	KeyFindings []string   `json:"key_findings,omitempty"`
	Evidence    []Evidence `json:"evidence,omitempty"`
	Gaps        []string   `json:"gaps,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

type Evidence struct {
	ID            string   `json:"id"`
	SourceID      string   `json:"source_id"`
	SourceTitle   string   `json:"source_title"`
	CitationLabel string   `json:"citation_label"`
	Excerpt       string   `json:"excerpt"`
	Terms         []string `json:"terms,omitempty"`
	Score         int      `json:"score"`
}

type CreateSpaceRequest struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Objective   string `json:"objective,omitempty"`
	CreatedBy   string `json:"created_by,omitempty"`
}

type AddSourceRequest struct {
	Title   string `json:"title"`
	Kind    string `json:"kind,omitempty"`
	URI     string `json:"uri,omitempty"`
	Content string `json:"content"`
}

type ResearchRequest struct {
	Question  string   `json:"question"`
	Mode      string   `json:"mode,omitempty"`
	SourceIDs []string `json:"source_ids,omitempty"`
}

var wordPattern = regexp.MustCompile(`[A-Za-z][A-Za-z0-9']*`)

var stopWords = map[string]bool{
	"about": true, "after": true, "again": true, "also": true, "among": true, "and": true,
	"are": true, "because": true, "been": true, "before": true, "being": true, "between": true,
	"both": true, "but": true, "can": true, "could": true, "did": true, "does": true,
	"each": true, "for": true, "from": true, "had": true, "has": true, "have": true,
	"how": true, "into": true, "its": true, "may": true, "more": true, "most": true,
	"must": true, "not": true, "our": true, "out": true, "over": true, "per": true,
	"should": true, "such": true, "than": true, "that": true, "the": true, "their": true,
	"then": true, "there": true, "these": true, "they": true, "this": true, "through": true,
	"under": true, "use": true, "used": true, "using": true, "was": true, "were": true,
	"what": true, "when": true, "where": true, "which": true, "while": true, "who": true,
	"why": true, "will": true, "with": true, "within": true, "would": true, "you": true,
	"your": true,
}

func NewSpace(req CreateSpaceRequest, spaceID string, now time.Time) (Space, error) {
	space := Space{
		ID:          strings.TrimSpace(spaceID),
		Title:       strings.TrimSpace(req.Title),
		Description: strings.TrimSpace(req.Description),
		Objective:   strings.TrimSpace(req.Objective),
		CreatedBy:   strings.TrimSpace(req.CreatedBy),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	return NormalizeSpace(space)
}

func NormalizeSpace(space Space) (Space, error) {
	space.ID = strings.TrimSpace(space.ID)
	if space.ID == "" {
		return Space{}, errors.New("knowledge space id is required")
	}
	space.Title = strings.TrimSpace(space.Title)
	space.Description = strings.TrimSpace(space.Description)
	space.Objective = strings.TrimSpace(space.Objective)
	if space.Title == "" {
		space.Title = firstLine(space.Objective)
	}
	if space.Title == "" {
		return Space{}, errors.New("knowledge space title is required")
	}
	space.CreatedBy = strings.TrimSpace(space.CreatedBy)
	for index := range space.Sources {
		source, err := NormalizeSource(space.Sources[index])
		if err != nil {
			return Space{}, err
		}
		space.Sources[index] = source
	}
	for index := range space.Reports {
		space.Reports[index] = normalizeReport(space.Reports[index])
	}
	insightAt := space.UpdatedAt
	if insightAt.IsZero() {
		insightAt = time.Now().UTC()
	}
	space.Insight = BuildSpaceInsight(space.Sources, insightAt)
	return space, nil
}

func NewSource(req AddSourceRequest, sourceID string, now time.Time) (Source, error) {
	source := Source{
		ID:        strings.TrimSpace(sourceID),
		Title:     strings.TrimSpace(req.Title),
		Kind:      normalizeSourceKind(req.Kind),
		URI:       strings.TrimSpace(req.URI),
		Content:   cleanWhitespace(req.Content),
		CreatedAt: now,
		UpdatedAt: now,
	}
	return NormalizeSource(source)
}

func NormalizeSource(source Source) (Source, error) {
	source.ID = strings.TrimSpace(source.ID)
	if source.ID == "" {
		return Source{}, errors.New("knowledge source id is required")
	}
	source.Title = strings.TrimSpace(source.Title)
	source.Kind = normalizeSourceKind(source.Kind)
	source.URI = strings.TrimSpace(source.URI)
	source.Content = cleanWhitespace(source.Content)
	if source.Title == "" {
		source.Title = firstLine(source.Content)
	}
	if source.Title == "" {
		return Source{}, errors.New("knowledge source title is required")
	}
	if source.Content == "" {
		return Source{}, errors.New("knowledge source content is required")
	}
	if source.Kind == SourceKindURL && source.URI == "" {
		source.URI = source.Title
	}
	source.WordCount = len(contentWords(source.Content, true))
	source.KeyTerms = topTerms(source.Content, 8)
	source.Questions = sourceQuestions(source.KeyTerms, source.Title)
	if source.Summary == "" {
		source.Summary = summarise(source.Content, source.KeyTerms, 2, 480)
	}
	return source, nil
}

func AddSource(space Space, source Source, now time.Time) (Space, error) {
	normalized, err := NormalizeSpace(space)
	if err != nil {
		return Space{}, err
	}
	source, err = NormalizeSource(source)
	if err != nil {
		return Space{}, err
	}
	found := false
	for index, existing := range normalized.Sources {
		if existing.ID == source.ID {
			normalized.Sources[index] = source
			found = true
			break
		}
	}
	if !found {
		normalized.Sources = append([]Source{source}, normalized.Sources...)
	}
	normalized.UpdatedAt = now
	normalized.Insight = BuildSpaceInsight(normalized.Sources, now)
	return normalized, nil
}

func GenerateReport(space Space, req ResearchRequest, reportID string, now time.Time) (Report, error) {
	normalized, err := NormalizeSpace(space)
	if err != nil {
		return Report{}, err
	}
	question := strings.TrimSpace(req.Question)
	if question == "" {
		return Report{}, errors.New("research question is required")
	}
	mode := normalizeReportMode(req.Mode)
	sources := selectedSources(normalized.Sources, req.SourceIDs)
	queryTerms := topTerms(question, 8)
	if len(queryTerms) == 0 {
		queryTerms = normalized.Insight.KeyTerms
	}
	evidence := rankEvidence(sources, queryTerms, mode)
	findings := findingsFromEvidence(evidence, mode)
	gaps := researchGaps(sources, queryTerms, evidence)
	report := Report{
		ID:          strings.TrimSpace(reportID),
		Question:    question,
		Mode:        mode,
		Answer:      buildAnswer(question, sources, evidence, findings),
		KeyFindings: findings,
		Evidence:    evidence,
		Gaps:        gaps,
		CreatedAt:   now,
	}
	return normalizeReport(report), nil
}

func AddReport(space Space, report Report, now time.Time) (Space, error) {
	normalized, err := NormalizeSpace(space)
	if err != nil {
		return Space{}, err
	}
	report = normalizeReport(report)
	if report.ID == "" {
		return Space{}, errors.New("knowledge report id is required")
	}
	normalized.Reports = append([]Report{report}, normalized.Reports...)
	if len(normalized.Reports) > 30 {
		normalized.Reports = normalized.Reports[:30]
	}
	normalized.UpdatedAt = now
	normalized.Insight = BuildSpaceInsight(normalized.Sources, now)
	return normalized, nil
}

func BuildSpaceInsight(sources []Source, now time.Time) SpaceInsight {
	var builder strings.Builder
	wordCount := 0
	for _, source := range sources {
		builder.WriteString(source.Content)
		builder.WriteByte(' ')
		wordCount += source.WordCount
	}
	terms := topTerms(builder.String(), 12)
	return SpaceInsight{
		SourceCount:        len(sources),
		WordCount:          wordCount,
		KeyTerms:           terms,
		SuggestedQuestions: sourceQuestions(terms, "this space"),
		UpdatedAt:          now,
	}
}

func normalizeReport(report Report) Report {
	report.ID = strings.TrimSpace(report.ID)
	report.Question = strings.TrimSpace(report.Question)
	report.Mode = normalizeReportMode(report.Mode)
	report.Answer = strings.TrimSpace(report.Answer)
	report.KeyFindings = compactStrings(report.KeyFindings, 8)
	report.Gaps = compactStrings(report.Gaps, 8)
	for index := range report.Evidence {
		report.Evidence[index].ID = strings.TrimSpace(report.Evidence[index].ID)
		report.Evidence[index].SourceID = strings.TrimSpace(report.Evidence[index].SourceID)
		report.Evidence[index].SourceTitle = strings.TrimSpace(report.Evidence[index].SourceTitle)
		report.Evidence[index].CitationLabel = strings.TrimSpace(report.Evidence[index].CitationLabel)
		report.Evidence[index].Excerpt = strings.TrimSpace(report.Evidence[index].Excerpt)
		report.Evidence[index].Terms = compactStrings(report.Evidence[index].Terms, 8)
	}
	return report
}

func normalizeSourceKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", "text", "paste":
		return SourceKindText
	case "url", "web", "link":
		return SourceKindURL
	case "file", "upload", "document":
		return SourceKindFile
	case "note":
		return SourceKindNote
	default:
		return SourceKindText
	}
}

func normalizeReportMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "research", "synthesis":
		return ReportModeResearch
	case "brief", "summary":
		return ReportModeBrief
	case "study", "questions":
		return ReportModeStudy
	default:
		return ReportModeResearch
	}
}

func selectedSources(sources []Source, ids []string) []Source {
	if len(ids) == 0 {
		return sources
	}
	allowed := make(map[string]bool, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" {
			allowed[id] = true
		}
	}
	var selected []Source
	for _, source := range sources {
		if allowed[source.ID] {
			selected = append(selected, source)
		}
	}
	return selected
}

type evidenceCandidate struct {
	source Source
	text   string
	terms  []string
	score  int
}

func rankEvidence(sources []Source, queryTerms []string, mode string) []Evidence {
	var candidates []evidenceCandidate
	for _, source := range sources {
		for _, chunk := range sourceChunks(source.Content) {
			score, matched := scoreText(chunk, queryTerms)
			if score == 0 {
				continue
			}
			candidates = append(candidates, evidenceCandidate{
				source: source,
				text:   chunk,
				terms:  matched,
				score:  score,
			})
		}
		if len(candidates) == 0 && source.Summary != "" {
			candidates = append(candidates, evidenceCandidate{
				source: source,
				text:   source.Summary,
				terms:  source.KeyTerms,
				score:  1,
			})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score == candidates[j].score {
			if candidates[i].source.Title == candidates[j].source.Title {
				return candidates[i].text < candidates[j].text
			}
			return candidates[i].source.Title < candidates[j].source.Title
		}
		return candidates[i].score > candidates[j].score
	})
	limit := 6
	if mode == ReportModeBrief {
		limit = 4
	}
	if len(candidates) < limit {
		limit = len(candidates)
	}
	evidence := make([]Evidence, 0, limit)
	for index := 0; index < limit; index++ {
		candidate := candidates[index]
		evidence = append(evidence, Evidence{
			ID:            fmt.Sprintf("evidence_%02d", index+1),
			SourceID:      candidate.source.ID,
			SourceTitle:   candidate.source.Title,
			CitationLabel: fmt.Sprintf("S%d", index+1),
			Excerpt:       shorten(candidate.text, 420),
			Terms:         candidate.terms,
			Score:         candidate.score,
		})
	}
	return evidence
}

func findingsFromEvidence(evidence []Evidence, mode string) []string {
	limit := 5
	if mode == ReportModeBrief {
		limit = 3
	}
	if len(evidence) < limit {
		limit = len(evidence)
	}
	findings := make([]string, 0, limit)
	for index := 0; index < limit; index++ {
		item := evidence[index]
		findings = append(findings, fmt.Sprintf("[%s] %s", item.CitationLabel, shorten(item.Excerpt, 240)))
	}
	return findings
}

func researchGaps(sources []Source, queryTerms []string, evidence []Evidence) []string {
	var gaps []string
	if len(sources) == 0 {
		return []string{"No sources are selected for this question."}
	}
	if len(evidence) == 0 {
		return []string{"No stored source text matched the question terms."}
	}
	matched := map[string]bool{}
	for _, item := range evidence {
		for _, term := range item.Terms {
			matched[term] = true
		}
	}
	var missing []string
	for _, term := range queryTerms {
		if !matched[term] {
			missing = append(missing, term)
		}
	}
	if len(missing) > 0 {
		gaps = append(gaps, "Source coverage is thin for: "+strings.Join(missing, ", ")+".")
	}
	gaps = append(gaps, "Only stored Knowledge Space sources were used for this report.")
	return gaps
}

func buildAnswer(question string, sources []Source, evidence []Evidence, findings []string) string {
	if len(sources) == 0 {
		return "No sources are available for this question. Add source text before running research."
	}
	if len(evidence) == 0 {
		return "The stored sources do not contain enough matching evidence to answer this question directly."
	}
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("Answering %q from %d stored source", question, len(sources)))
	if len(sources) == 1 {
		builder.WriteString(":")
	} else {
		builder.WriteString("s:")
	}
	for _, finding := range findings {
		builder.WriteString("\n- ")
		builder.WriteString(finding)
	}
	return builder.String()
}

func scoreText(text string, queryTerms []string) (int, []string) {
	if len(queryTerms) == 0 {
		return 1, nil
	}
	counts := termCountMap(text)
	score := 0
	var matched []string
	for _, term := range queryTerms {
		if count := counts[term]; count > 0 {
			score += count * 3
			matched = append(matched, term)
		}
	}
	return score, matched
}

func summarise(content string, keyTerms []string, sentenceLimit, charLimit int) string {
	sentences := splitSentences(content)
	if len(sentences) == 0 {
		return shorten(content, charLimit)
	}
	type scoredSentence struct {
		index int
		text  string
		score int
	}
	var scored []scoredSentence
	for index, sentence := range sentences {
		score, _ := scoreText(sentence, keyTerms)
		if score == 0 {
			score = 1
		}
		scored = append(scored, scoredSentence{index: index, text: sentence, score: score})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].index < scored[j].index
		}
		return scored[i].score > scored[j].score
	})
	if sentenceLimit <= 0 {
		sentenceLimit = 2
	}
	if len(scored) < sentenceLimit {
		sentenceLimit = len(scored)
	}
	selected := append([]scoredSentence(nil), scored[:sentenceLimit]...)
	sort.Slice(selected, func(i, j int) bool { return selected[i].index < selected[j].index })
	parts := make([]string, 0, len(selected))
	for _, item := range selected {
		parts = append(parts, item.text)
	}
	return shorten(strings.Join(parts, " "), charLimit)
}

func sourceChunks(content string) []string {
	sentences := splitSentences(content)
	if len(sentences) == 0 {
		return []string{shorten(content, 650)}
	}
	var chunks []string
	var current strings.Builder
	for _, sentence := range sentences {
		if current.Len() > 0 && current.Len()+len(sentence)+1 > 650 {
			chunks = append(chunks, current.String())
			current.Reset()
		}
		if current.Len() > 0 {
			current.WriteByte(' ')
		}
		current.WriteString(sentence)
	}
	if current.Len() > 0 {
		chunks = append(chunks, current.String())
	}
	return chunks
}

func splitSentences(content string) []string {
	content = cleanWhitespace(content)
	if content == "" {
		return nil
	}
	var sentences []string
	start := 0
	for index, value := range content {
		if value != '.' && value != '?' && value != '!' && value != '\n' {
			continue
		}
		part := strings.TrimSpace(content[start : index+len(string(value))])
		if len(part) > 20 {
			sentences = append(sentences, part)
		}
		start = index + len(string(value))
	}
	if start < len(content) {
		part := strings.TrimSpace(content[start:])
		if len(part) > 0 {
			sentences = append(sentences, part)
		}
	}
	if len(sentences) == 0 {
		return []string{content}
	}
	return sentences
}

func topTerms(content string, limit int) []string {
	counts := termCountMap(content)
	type termScore struct {
		term  string
		count int
	}
	var scores []termScore
	for term, count := range counts {
		scores = append(scores, termScore{term: term, count: count})
	}
	sort.Slice(scores, func(i, j int) bool {
		if scores[i].count == scores[j].count {
			return scores[i].term < scores[j].term
		}
		return scores[i].count > scores[j].count
	})
	if limit <= 0 || len(scores) < limit {
		limit = len(scores)
	}
	terms := make([]string, 0, limit)
	for index := 0; index < limit; index++ {
		terms = append(terms, scores[index].term)
	}
	return terms
}

func termCountMap(content string) map[string]int {
	counts := map[string]int{}
	for _, word := range contentWords(content, false) {
		counts[word]++
	}
	return counts
}

func contentWords(content string, includeStopWords bool) []string {
	matches := wordPattern.FindAllString(strings.ToLower(content), -1)
	words := make([]string, 0, len(matches))
	for _, match := range matches {
		match = strings.Trim(match, "'")
		if len(match) < 3 {
			continue
		}
		if !includeStopWords && stopWords[match] {
			continue
		}
		words = append(words, match)
	}
	return words
}

func sourceQuestions(terms []string, subject string) []string {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		subject = "this source"
	}
	limit := 3
	if len(terms) < limit {
		limit = len(terms)
	}
	questions := make([]string, 0, limit)
	for index := 0; index < limit; index++ {
		questions = append(questions, fmt.Sprintf("What does %s show about %s?", subject, terms[index]))
	}
	return questions
}

func cleanWhitespace(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func compactStrings(values []string, limit int) []string {
	var compact []string
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		compact = append(compact, value)
		if limit > 0 && len(compact) >= limit {
			break
		}
	}
	return compact
}

func firstLine(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	for index, char := range value {
		if char == '\n' || char == '\r' {
			return strings.TrimSpace(value[:index])
		}
	}
	return shorten(value, 80)
}

func shorten(value string, limit int) string {
	value = cleanWhitespace(value)
	runes := []rune(value)
	if limit <= 0 || len(runes) <= limit {
		return value
	}
	if limit <= 1 {
		return string(runes[:limit])
	}
	cut := limit - 1
	for cut > 0 && !unicode.IsSpace(runes[cut]) {
		cut--
	}
	if cut < limit/2 {
		cut = limit - 1
	}
	return strings.TrimSpace(string(runes[:cut])) + "..."
}
