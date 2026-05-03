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
	SourceKindText  = "text"
	SourceKindURL   = "url"
	SourceKindFile  = "file"
	SourceKindNote  = "note"
	SourceKindEmail = "email"
	SourceKindMCP   = "mcp"

	ReportModeResearch = "research"
	ReportModeBrief    = "brief"
	ReportModeStudy    = "study"

	SourceStatusReady      = "ready"
	SourceStatusFailed     = "failed"
	SourceStatusProcessing = "processing"

	ResearchRunStatusQueued       = "queued"
	ResearchRunStatusPlanning     = "planning"
	ResearchRunStatusDiscovering  = "discovering"
	ResearchRunStatusRetrieving   = "retrieving"
	ResearchRunStatusReading      = "reading"
	ResearchRunStatusSynthesizing = "synthesizing"
	ResearchRunStatusReviewing    = "reviewing"
	ResearchRunStatusCompleted    = "completed"
	ResearchRunStatusFailed       = "failed"
	ResearchRunStatusCancelled    = "cancelled"
)

type Space struct {
	ID           string        `json:"id"`
	Title        string        `json:"title"`
	Description  string        `json:"description,omitempty"`
	Objective    string        `json:"objective,omitempty"`
	Sources      []Source      `json:"sources,omitempty"`
	Reports      []Report      `json:"reports,omitempty"`
	ResearchRuns []ResearchRun `json:"research_runs,omitempty"`
	Insight      SpaceInsight  `json:"insight"`
	CreatedBy    string        `json:"created_by,omitempty"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
}

type SpaceInsight struct {
	SourceCount        int       `json:"source_count"`
	WordCount          int       `json:"word_count"`
	KeyTerms           []string  `json:"key_terms,omitempty"`
	SuggestedQuestions []string  `json:"suggested_questions,omitempty"`
	UpdatedAt          time.Time `json:"updated_at,omitempty"`
}

type Source struct {
	ID          string           `json:"id"`
	Title       string           `json:"title"`
	Kind        string           `json:"kind"`
	URI         string           `json:"uri,omitempty"`
	Content     string           `json:"content"`
	Summary     string           `json:"summary"`
	KeyTerms    []string         `json:"key_terms,omitempty"`
	Questions   []string         `json:"questions,omitempty"`
	Claims      []SourceClaim    `json:"claims,omitempty"`
	Entities    []SourceEntity   `json:"entities,omitempty"`
	Reliability []string         `json:"reliability_notes,omitempty"`
	WordCount   int              `json:"word_count"`
	Provenance  SourceProvenance `json:"provenance,omitempty"`
	Ingestion   SourceIngestion  `json:"ingestion,omitempty"`
	Sections    []SourceSection  `json:"sections,omitempty"`
	Chunks      []SourceChunk    `json:"chunks,omitempty"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
}

type SourceClaim struct {
	ID         string `json:"id"`
	Text       string `json:"text"`
	Importance string `json:"importance,omitempty"`
}

type SourceEntity struct {
	Name        string `json:"name"`
	Type        string `json:"type,omitempty"`
	Description string `json:"description,omitempty"`
}

type SourceProvenance struct {
	URI          string    `json:"uri,omitempty"`
	CanonicalURI string    `json:"canonical_uri,omitempty"`
	ContentType  string    `json:"content_type,omitempty"`
	ContentHash  string    `json:"content_hash,omitempty"`
	ByteCount    int       `json:"byte_count,omitempty"`
	SnapshotPath string    `json:"snapshot_path,omitempty"`
	FetchedAt    time.Time `json:"fetched_at,omitempty"`
	Extractor    string    `json:"extractor,omitempty"`
}

type SourceIngestion struct {
	State       string    `json:"state,omitempty"`
	Stage       string    `json:"stage,omitempty"`
	Message     string    `json:"message,omitempty"`
	Error       string    `json:"error,omitempty"`
	StartedAt   time.Time `json:"started_at,omitempty"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
}

type SourceSection struct {
	ID          string   `json:"id"`
	SourceID    string   `json:"source_id"`
	SourceTitle string   `json:"source_title"`
	Index       int      `json:"index"`
	Heading     string   `json:"heading"`
	Text        string   `json:"text"`
	Terms       []string `json:"terms,omitempty"`
	WordCount   int      `json:"word_count"`
}

type SourceChunk struct {
	ID            string   `json:"id"`
	SourceID      string   `json:"source_id"`
	SourceTitle   string   `json:"source_title"`
	SectionID     string   `json:"section_id,omitempty"`
	SectionTitle  string   `json:"section_title,omitempty"`
	Index         int      `json:"index"`
	CitationLabel string   `json:"citation_label"`
	Text          string   `json:"text"`
	Terms         []string `json:"terms,omitempty"`
	SemanticTerms []string `json:"semantic_terms,omitempty"`
	WordCount     int      `json:"word_count"`
}

type Report struct {
	ID          string     `json:"id"`
	RunID       string     `json:"run_id,omitempty"`
	Question    string     `json:"question"`
	Mode        string     `json:"mode"`
	Answer      string     `json:"answer"`
	KeyFindings []string   `json:"key_findings,omitempty"`
	Evidence    []Evidence `json:"evidence,omitempty"`
	Gaps        []string   `json:"gaps,omitempty"`
	Provider    string     `json:"provider,omitempty"`
	Model       string     `json:"model,omitempty"`
	Usage       TokenUsage `json:"usage,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

type Evidence struct {
	ID            string   `json:"id"`
	SourceID      string   `json:"source_id"`
	SourceTitle   string   `json:"source_title"`
	SourceKind    string   `json:"source_kind,omitempty"`
	SourceURI     string   `json:"source_uri,omitempty"`
	ChunkID       string   `json:"chunk_id,omitempty"`
	SectionID     string   `json:"section_id,omitempty"`
	SectionTitle  string   `json:"section_title,omitempty"`
	CitationLabel string   `json:"citation_label"`
	Excerpt       string   `json:"excerpt"`
	Terms         []string `json:"terms,omitempty"`
	SourceSummary string   `json:"source_summary,omitempty"`
	Retrieval     string   `json:"retrieval,omitempty"`
	LexicalScore  int      `json:"lexical_score,omitempty"`
	SemanticScore int      `json:"semantic_score,omitempty"`
	Score         int      `json:"score"`
}

type QueryRequest struct {
	Query     string   `json:"query"`
	SourceIDs []string `json:"source_ids,omitempty"`
	Limit     int      `json:"limit,omitempty"`
}

type QueryResult struct {
	Query     string     `json:"query"`
	Terms     []string   `json:"terms,omitempty"`
	Evidence  []Evidence `json:"evidence"`
	CreatedAt time.Time  `json:"created_at"`
}

type RetrievalIndex struct {
	SpaceID   string                `json:"space_id"`
	UpdatedAt time.Time             `json:"updated_at"`
	Chunks    []RetrievalIndexChunk `json:"chunks"`
}

type RetrievalIndexChunk struct {
	SourceID      string   `json:"source_id"`
	SourceTitle   string   `json:"source_title"`
	SourceKind    string   `json:"source_kind,omitempty"`
	SourceURI     string   `json:"source_uri,omitempty"`
	SourceSummary string   `json:"source_summary,omitempty"`
	SectionID     string   `json:"section_id,omitempty"`
	SectionTitle  string   `json:"section_title,omitempty"`
	ChunkID       string   `json:"chunk_id"`
	CitationLabel string   `json:"citation_label"`
	TextHash      string   `json:"text_hash"`
	Terms         []string `json:"terms,omitempty"`
	SemanticTerms []string `json:"semantic_terms,omitempty"`
	WordCount     int      `json:"word_count"`
}

type RetrievalIndexer interface {
	Build(space Space, now time.Time) (RetrievalIndex, error)
}

type LocalRetrievalIndexer struct{}

type AskRequest struct {
	Question  string   `json:"question"`
	SourceIDs []string `json:"source_ids,omitempty"`
	Limit     int      `json:"limit,omitempty"`
}

type AskResult struct {
	Question    string     `json:"question"`
	Answer      string     `json:"answer"`
	KeyFindings []string   `json:"key_findings,omitempty"`
	Evidence    []Evidence `json:"evidence,omitempty"`
	Gaps        []string   `json:"gaps,omitempty"`
	Provider    string     `json:"provider,omitempty"`
	Model       string     `json:"model,omitempty"`
	Usage       TokenUsage `json:"usage,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

type TokenUsage struct {
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
	TotalTokens  int `json:"total_tokens,omitempty"`
}

type ResearchRun struct {
	ID              string             `json:"id"`
	Objective       string             `json:"objective"`
	Scope           string             `json:"scope,omitempty"`
	Depth           string             `json:"depth"`
	Status          string             `json:"status"`
	Question        string             `json:"question,omitempty"`
	Mode            string             `json:"mode"`
	Plan            ResearchPlan       `json:"plan,omitempty"`
	DiscoverSources bool               `json:"discover_sources,omitempty"`
	Candidates      []SourceCandidate  `json:"source_candidates,omitempty"`
	ResearchLoops   []ResearchLoop     `json:"research_loops,omitempty"`
	Coverage        []ResearchCoverage `json:"coverage,omitempty"`
	SourceIDs       []string           `json:"source_ids,omitempty"`
	ReportID        string             `json:"report_id,omitempty"`
	SourcesExamined int                `json:"sources_examined,omitempty"`
	EvidenceCount   int                `json:"evidence_count,omitempty"`
	Provider        string             `json:"provider,omitempty"`
	Model           string             `json:"model,omitempty"`
	Usage           TokenUsage         `json:"usage,omitempty"`
	WorkspacePath   string             `json:"workspace_path,omitempty"`
	Error           string             `json:"error,omitempty"`
	StopReason      string             `json:"stop_reason,omitempty"`
	Events          []ResearchRunEvent `json:"events,omitempty"`
	CreatedAt       time.Time          `json:"created_at"`
	UpdatedAt       time.Time          `json:"updated_at"`
	StartedAt       time.Time          `json:"started_at,omitempty"`
	FinishedAt      time.Time          `json:"finished_at,omitempty"`
}

type ResearchPlan struct {
	RewrittenObjective  string   `json:"rewritten_objective,omitempty"`
	ClarifyingQuestions []string `json:"clarifying_questions,omitempty"`
	SearchQueries       []string `json:"search_queries,omitempty"`
	Steps               []string `json:"steps,omitempty"`
	ExpectedOutputs     []string `json:"expected_outputs,omitempty"`
}

type ResearchLoop struct {
	ID              string     `json:"id"`
	Index           int        `json:"index"`
	Query           string     `json:"query"`
	Queries         []string   `json:"queries,omitempty"`
	Status          string     `json:"status"`
	Decision        string     `json:"decision,omitempty"`
	StopReason      string     `json:"stop_reason,omitempty"`
	CandidateIDs    []string   `json:"candidate_ids,omitempty"`
	SourceIDs       []string   `json:"source_ids,omitempty"`
	AcceptedCount   int        `json:"accepted_count,omitempty"`
	RejectedCount   int        `json:"rejected_count,omitempty"`
	FailedCount     int        `json:"failed_count,omitempty"`
	EvidenceCount   int        `json:"evidence_count,omitempty"`
	Coverage        []string   `json:"coverage,omitempty"`
	SupportedClaims []string   `json:"supported_claims,omitempty"`
	Gaps            []string   `json:"gaps,omitempty"`
	FollowUpQueries []string   `json:"follow_up_queries,omitempty"`
	Usage           TokenUsage `json:"usage,omitempty"`
	StartedAt       time.Time  `json:"started_at,omitempty"`
	FinishedAt      time.Time  `json:"finished_at,omitempty"`
}

type ResearchCoverage struct {
	ID            string   `json:"id"`
	Topic         string   `json:"topic"`
	Status        string   `json:"status"`
	SourceIDs     []string `json:"source_ids,omitempty"`
	EvidenceCount int      `json:"evidence_count,omitempty"`
	Notes         string   `json:"notes,omitempty"`
}

type SourceEvaluation struct {
	Decision        string   `json:"decision"`
	RelevanceScore  int      `json:"relevance_score"`
	Reason          string   `json:"reason"`
	Coverage        []string `json:"coverage,omitempty"`
	FollowUpQueries []string `json:"follow_up_queries,omitempty"`
}

type ResearchCoverageDecision struct {
	Decision        string   `json:"decision"`
	StopReason      string   `json:"stop_reason"`
	SupportedClaims []string `json:"supported_claims,omitempty"`
	Gaps            []string `json:"gaps,omitempty"`
	FollowUpQueries []string `json:"follow_up_queries,omitempty"`
	Coverage        []string `json:"coverage,omitempty"`
}

type SourceCandidate struct {
	ID                string   `json:"id"`
	Query             string   `json:"query,omitempty"`
	Kind              string   `json:"kind,omitempty"`
	Provider          string   `json:"provider,omitempty"`
	Title             string   `json:"title"`
	URL               string   `json:"url,omitempty"`
	Domain            string   `json:"domain,omitempty"`
	Snippet           string   `json:"snippet,omitempty"`
	ContentType       string   `json:"content_type,omitempty"`
	Fetched           bool     `json:"fetched,omitempty"`
	ExtractionState   string   `json:"extraction_state,omitempty"`
	ExtractionMessage string   `json:"extraction_message,omitempty"`
	WordCount         int      `json:"word_count,omitempty"`
	Usefulness        string   `json:"usefulness,omitempty"`
	RelevanceScore    int      `json:"relevance_score,omitempty"`
	Coverage          []string `json:"coverage,omitempty"`
	SourceID          string   `json:"source_id,omitempty"`
	Status            string   `json:"status"`
	Error             string   `json:"error,omitempty"`
}

type ResearchRunEvent struct {
	ID        string    `json:"id"`
	Stage     string    `json:"stage"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateSpaceRequest struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Objective   string `json:"objective,omitempty"`
	CreatedBy   string `json:"created_by,omitempty"`
}

type UpdateSpaceRequest struct {
	Title       *string `json:"title,omitempty"`
	Description *string `json:"description,omitempty"`
	Objective   *string `json:"objective,omitempty"`
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

type CreateResearchRunRequest struct {
	Objective       string   `json:"objective"`
	Scope           string   `json:"scope,omitempty"`
	Depth           string   `json:"depth,omitempty"`
	Question        string   `json:"question,omitempty"`
	Mode            string   `json:"mode,omitempty"`
	SourceIDs       []string `json:"source_ids,omitempty"`
	DiscoverSources bool     `json:"discover_sources,omitempty"`
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

func UpdateSpace(space Space, req UpdateSpaceRequest, now time.Time) (Space, error) {
	normalized, err := NormalizeSpace(space)
	if err != nil {
		return Space{}, err
	}
	changed := false
	if req.Title != nil {
		normalized.Title = strings.TrimSpace(*req.Title)
		changed = true
	}
	if req.Description != nil {
		normalized.Description = strings.TrimSpace(*req.Description)
		changed = true
	}
	if req.Objective != nil {
		normalized.Objective = strings.TrimSpace(*req.Objective)
		changed = true
	}
	if !changed {
		return Space{}, errors.New("knowledge space update is empty")
	}
	normalized.UpdatedAt = now
	return NormalizeSpace(normalized)
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
	for index := range space.ResearchRuns {
		space.ResearchRuns[index] = normalizeResearchRun(space.ResearchRuns[index])
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
		Content:   cleanSourceContent(req.Content),
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
	source.Content = cleanSourceContent(source.Content)
	source.Provenance = normalizeSourceProvenance(source.Provenance, source)
	source.Ingestion = normalizeSourceIngestion(source.Ingestion, source.Content != "")
	if source.Title == "" {
		source.Title = firstLine(source.Content)
	}
	if source.Title == "" {
		source.Title = source.URI
	}
	if source.Title == "" {
		return Source{}, errors.New("knowledge source title is required")
	}
	if source.Content == "" && source.Ingestion.State != SourceStatusFailed {
		return Source{}, errors.New("knowledge source content is required")
	}
	if source.Kind == SourceKindURL && source.URI == "" {
		source.URI = source.Title
	}
	if source.Provenance.URI == "" {
		source.Provenance.URI = source.URI
	}
	source.WordCount = len(contentWords(source.Content, true))
	source.Summary = strings.TrimSpace(source.Summary)
	source.KeyTerms = compactStrings(source.KeyTerms, 12)
	source.Questions = compactStrings(source.Questions, 8)
	source.Claims = normalizeSourceClaims(source.Claims)
	source.Entities = normalizeSourceEntities(source.Entities)
	source.Reliability = compactStrings(source.Reliability, 8)
	source.Sections = normalizeSourceSections(source)
	source.Chunks = normalizeSourceChunks(source)
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

func RemoveSource(space Space, sourceID string, now time.Time) (Space, Source, error) {
	normalized, err := NormalizeSpace(space)
	if err != nil {
		return Space{}, Source{}, err
	}
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return Space{}, Source{}, errors.New("knowledge source id is required")
	}
	next := make([]Source, 0, len(normalized.Sources))
	var removed Source
	for _, source := range normalized.Sources {
		if source.ID == sourceID {
			removed = source
			continue
		}
		next = append(next, source)
	}
	if removed.ID == "" {
		return Space{}, Source{}, fmt.Errorf("knowledge source %s not found", sourceID)
	}
	normalized.Sources = next
	normalized.UpdatedAt = now
	normalized.Insight = BuildSpaceInsight(normalized.Sources, now)
	return normalized, removed, nil
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

func AddResearchRun(space Space, run ResearchRun, now time.Time) (Space, error) {
	normalized, err := NormalizeSpace(space)
	if err != nil {
		return Space{}, err
	}
	run = normalizeResearchRun(run)
	if run.ID == "" {
		return Space{}, errors.New("knowledge research run id is required")
	}
	found := false
	for index, existing := range normalized.ResearchRuns {
		if existing.ID == run.ID {
			normalized.ResearchRuns[index] = run
			found = true
			break
		}
	}
	if !found {
		normalized.ResearchRuns = append([]ResearchRun{run}, normalized.ResearchRuns...)
	}
	if len(normalized.ResearchRuns) > 30 {
		normalized.ResearchRuns = normalized.ResearchRuns[:30]
	}
	normalized.UpdatedAt = now
	normalized.Insight = BuildSpaceInsight(normalized.Sources, now)
	return normalized, nil
}

func BuildSpaceInsight(sources []Source, now time.Time) SpaceInsight {
	var builder strings.Builder
	wordCount := 0
	var questions []string
	for _, source := range sources {
		if len(source.KeyTerms) > 0 {
			builder.WriteString(strings.Join(source.KeyTerms, " "))
		} else {
			builder.WriteString(source.Content)
		}
		builder.WriteByte(' ')
		questions = append(questions, source.Questions...)
		wordCount += source.WordCount
	}
	terms := topTerms(builder.String(), 12)
	return SpaceInsight{
		SourceCount:        len(sources),
		WordCount:          wordCount,
		KeyTerms:           terms,
		SuggestedQuestions: compactStrings(questions, 6),
		UpdatedAt:          now,
	}
}

func normalizeReport(report Report) Report {
	report.ID = strings.TrimSpace(report.ID)
	report.RunID = strings.TrimSpace(report.RunID)
	report.Question = strings.TrimSpace(report.Question)
	report.Mode = normalizeReportMode(report.Mode)
	report.Answer = strings.TrimSpace(report.Answer)
	report.KeyFindings = compactStrings(report.KeyFindings, 8)
	report.Gaps = compactStrings(report.Gaps, 8)
	report.Provider = strings.TrimSpace(report.Provider)
	report.Model = strings.TrimSpace(report.Model)
	for index := range report.Evidence {
		report.Evidence[index].ID = strings.TrimSpace(report.Evidence[index].ID)
		report.Evidence[index].SourceID = strings.TrimSpace(report.Evidence[index].SourceID)
		report.Evidence[index].SourceTitle = strings.TrimSpace(report.Evidence[index].SourceTitle)
		report.Evidence[index].SourceKind = strings.TrimSpace(report.Evidence[index].SourceKind)
		report.Evidence[index].SourceURI = strings.TrimSpace(report.Evidence[index].SourceURI)
		report.Evidence[index].ChunkID = strings.TrimSpace(report.Evidence[index].ChunkID)
		report.Evidence[index].SectionID = strings.TrimSpace(report.Evidence[index].SectionID)
		report.Evidence[index].SectionTitle = strings.TrimSpace(report.Evidence[index].SectionTitle)
		report.Evidence[index].CitationLabel = strings.TrimSpace(report.Evidence[index].CitationLabel)
		report.Evidence[index].Excerpt = strings.TrimSpace(report.Evidence[index].Excerpt)
		report.Evidence[index].Terms = compactStrings(report.Evidence[index].Terms, 8)
		report.Evidence[index].SourceSummary = strings.TrimSpace(report.Evidence[index].SourceSummary)
		report.Evidence[index].Retrieval = strings.TrimSpace(report.Evidence[index].Retrieval)
		if report.Evidence[index].LexicalScore < 0 {
			report.Evidence[index].LexicalScore = 0
		}
		if report.Evidence[index].SemanticScore < 0 {
			report.Evidence[index].SemanticScore = 0
		}
	}
	return report
}

func normalizeResearchRun(run ResearchRun) ResearchRun {
	run.ID = strings.TrimSpace(run.ID)
	run.Objective = strings.TrimSpace(run.Objective)
	run.Scope = strings.TrimSpace(run.Scope)
	run.Depth = normalizeResearchDepth(run.Depth)
	run.Status = normalizeResearchRunStatus(run.Status)
	run.Question = strings.TrimSpace(run.Question)
	run.Mode = normalizeReportMode(run.Mode)
	run.Plan = normalizeResearchPlan(run.Plan)
	run.Candidates = normalizeSourceCandidates(run.Candidates)
	run.ResearchLoops = normalizeResearchLoops(run.ResearchLoops)
	run.Coverage = normalizeResearchCoverage(run.Coverage)
	run.ReportID = strings.TrimSpace(run.ReportID)
	run.Provider = strings.TrimSpace(run.Provider)
	run.Model = strings.TrimSpace(run.Model)
	run.WorkspacePath = strings.TrimSpace(run.WorkspacePath)
	run.Error = strings.TrimSpace(run.Error)
	run.StopReason = strings.TrimSpace(run.StopReason)
	run.SourceIDs = compactStrings(run.SourceIDs, 200)
	for index := range run.Events {
		run.Events[index].ID = strings.TrimSpace(run.Events[index].ID)
		run.Events[index].Stage = strings.TrimSpace(run.Events[index].Stage)
		run.Events[index].Message = strings.TrimSpace(run.Events[index].Message)
	}
	return run
}

func normalizeResearchLoops(loops []ResearchLoop) []ResearchLoop {
	out := make([]ResearchLoop, 0, len(loops))
	for index, loop := range loops {
		loop.ID = strings.TrimSpace(loop.ID)
		if loop.ID == "" {
			loop.ID = fmt.Sprintf("kloop_%02d", index+1)
		}
		if loop.Index <= 0 {
			loop.Index = index + 1
		}
		loop.Query = strings.TrimSpace(loop.Query)
		loop.Queries = compactStrings(loop.Queries, 12)
		loop.Status = strings.ToLower(strings.TrimSpace(loop.Status))
		if loop.Status == "" {
			loop.Status = "completed"
		}
		loop.Decision = strings.ToLower(strings.TrimSpace(loop.Decision))
		loop.StopReason = strings.TrimSpace(loop.StopReason)
		loop.CandidateIDs = compactStrings(loop.CandidateIDs, 200)
		loop.SourceIDs = compactStrings(loop.SourceIDs, 200)
		loop.Coverage = compactStrings(loop.Coverage, 20)
		loop.SupportedClaims = compactStrings(loop.SupportedClaims, 20)
		loop.Gaps = compactStrings(loop.Gaps, 20)
		loop.FollowUpQueries = compactStrings(loop.FollowUpQueries, 12)
		if loop.Query != "" || len(loop.Queries) > 0 || len(loop.CandidateIDs) > 0 || len(loop.SourceIDs) > 0 {
			out = append(out, loop)
		}
	}
	return out
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
	case "email", "mail", "gmail":
		return SourceKindEmail
	case "mcp", "connector", "connected":
		return SourceKindMCP
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

func normalizeResearchDepth(depth string) string {
	switch strings.ToLower(strings.TrimSpace(depth)) {
	case "", "standard", "normal":
		return "standard"
	case "quick", "shallow":
		return "quick"
	case "deep", "long":
		return "deep"
	default:
		return "standard"
	}
}

func normalizeResearchRunStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case ResearchRunStatusQueued:
		return ResearchRunStatusQueued
	case ResearchRunStatusPlanning:
		return ResearchRunStatusPlanning
	case ResearchRunStatusDiscovering:
		return ResearchRunStatusDiscovering
	case ResearchRunStatusRetrieving:
		return ResearchRunStatusRetrieving
	case ResearchRunStatusReading:
		return ResearchRunStatusReading
	case ResearchRunStatusSynthesizing:
		return ResearchRunStatusSynthesizing
	case ResearchRunStatusReviewing:
		return ResearchRunStatusReviewing
	case ResearchRunStatusFailed:
		return ResearchRunStatusFailed
	case ResearchRunStatusCancelled:
		return ResearchRunStatusCancelled
	case "", ResearchRunStatusCompleted, "done", "ready":
		return ResearchRunStatusCompleted
	default:
		return strings.ToLower(strings.TrimSpace(status))
	}
}

func normalizeSourceClaims(claims []SourceClaim) []SourceClaim {
	out := make([]SourceClaim, 0, len(claims))
	for index, claim := range claims {
		claim.ID = strings.TrimSpace(claim.ID)
		if claim.ID == "" {
			claim.ID = fmt.Sprintf("claim_%02d", index+1)
		}
		claim.Text = strings.TrimSpace(claim.Text)
		claim.Importance = strings.TrimSpace(claim.Importance)
		if claim.Text != "" {
			out = append(out, claim)
		}
		if len(out) >= 12 {
			break
		}
	}
	return out
}

func normalizeSourceEntities(entities []SourceEntity) []SourceEntity {
	out := make([]SourceEntity, 0, len(entities))
	for _, entity := range entities {
		entity.Name = strings.TrimSpace(entity.Name)
		entity.Type = strings.TrimSpace(entity.Type)
		entity.Description = strings.TrimSpace(entity.Description)
		if entity.Name != "" {
			out = append(out, entity)
		}
		if len(out) >= 20 {
			break
		}
	}
	return out
}

func normalizeResearchPlan(plan ResearchPlan) ResearchPlan {
	plan.RewrittenObjective = strings.TrimSpace(plan.RewrittenObjective)
	plan.ClarifyingQuestions = compactStrings(plan.ClarifyingQuestions, 8)
	plan.SearchQueries = compactStrings(plan.SearchQueries, 12)
	plan.Steps = compactStrings(plan.Steps, 12)
	plan.ExpectedOutputs = compactStrings(plan.ExpectedOutputs, 8)
	return plan
}

func normalizeSourceCandidates(candidates []SourceCandidate) []SourceCandidate {
	out := make([]SourceCandidate, 0, len(candidates))
	for index, candidate := range candidates {
		candidate.ID = strings.TrimSpace(candidate.ID)
		if candidate.ID == "" {
			candidate.ID = fmt.Sprintf("candidate_%03d", index+1)
		}
		candidate.Query = strings.TrimSpace(candidate.Query)
		candidate.Kind = strings.TrimSpace(candidate.Kind)
		candidate.Provider = strings.TrimSpace(candidate.Provider)
		candidate.Title = strings.TrimSpace(candidate.Title)
		candidate.URL = strings.TrimSpace(candidate.URL)
		candidate.Domain = strings.TrimSpace(candidate.Domain)
		candidate.Snippet = strings.TrimSpace(candidate.Snippet)
		candidate.ContentType = strings.TrimSpace(candidate.ContentType)
		candidate.ExtractionState = strings.ToLower(strings.TrimSpace(candidate.ExtractionState))
		candidate.ExtractionMessage = strings.TrimSpace(candidate.ExtractionMessage)
		if candidate.WordCount < 0 {
			candidate.WordCount = 0
		}
		candidate.Usefulness = strings.ToLower(strings.TrimSpace(candidate.Usefulness))
		if candidate.RelevanceScore < 0 {
			candidate.RelevanceScore = 0
		}
		if candidate.RelevanceScore > 100 {
			candidate.RelevanceScore = 100
		}
		candidate.Coverage = compactStrings(candidate.Coverage, 12)
		candidate.SourceID = strings.TrimSpace(candidate.SourceID)
		candidate.Status = strings.ToLower(strings.TrimSpace(candidate.Status))
		if candidate.Status == "" {
			candidate.Status = "candidate"
		}
		candidate.Error = strings.TrimSpace(candidate.Error)
		if candidate.Title != "" || candidate.URL != "" || candidate.Snippet != "" {
			out = append(out, candidate)
		}
		if len(out) >= 100 {
			break
		}
	}
	return out
}

func normalizeResearchCoverage(items []ResearchCoverage) []ResearchCoverage {
	out := make([]ResearchCoverage, 0, len(items))
	for index, item := range items {
		item.ID = strings.TrimSpace(item.ID)
		if item.ID == "" {
			item.ID = fmt.Sprintf("coverage_%02d", index+1)
		}
		item.Topic = strings.TrimSpace(item.Topic)
		item.Status = strings.ToLower(strings.TrimSpace(item.Status))
		if item.Status == "" {
			item.Status = "planned"
		}
		item.SourceIDs = compactStrings(item.SourceIDs, 30)
		if item.EvidenceCount < 0 {
			item.EvidenceCount = 0
		}
		item.Notes = strings.TrimSpace(item.Notes)
		if item.Topic != "" {
			out = append(out, item)
		}
		if len(out) >= 24 {
			break
		}
	}
	return out
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
	source        Source
	chunk         SourceChunk
	section       SourceSection
	text          string
	terms         []string
	retrieval     string
	lexicalScore  int
	semanticScore int
	score         int
}

func rankEvidence(sources []Source, queryTerms []string, mode string, limit int) []Evidence {
	var candidates []evidenceCandidate
	for _, source := range sources {
		sourceCandidates := 0
		chunks := source.Chunks
		if len(chunks) == 0 {
			chunks = normalizeSourceChunks(source)
		}
		sectionByID := map[string]SourceSection{}
		for _, section := range source.Sections {
			sectionByID[section.ID] = section
		}
		for _, chunk := range chunks {
			lexicalScore, lexicalTerms := scoreText(chunk.Text, queryTerms)
			semanticScore, semanticTerms := scoreText(strings.Join(chunk.SemanticTerms, " "), queryTerms)
			if lexicalScore == 0 && semanticScore == 0 {
				continue
			}
			retrieval := "hybrid"
			if lexicalScore == 0 {
				retrieval = "semantic"
			} else if semanticScore == 0 {
				retrieval = "lexical"
			}
			sourceQuality := sourceRetrievalQuality(source)
			section := sectionByID[chunk.SectionID]
			candidates = append(candidates, evidenceCandidate{
				source:        source,
				chunk:         chunk,
				section:       section,
				text:          chunk.Text,
				terms:         compactStrings(append(lexicalTerms, semanticTerms...), 12),
				retrieval:     retrieval,
				lexicalScore:  lexicalScore,
				semanticScore: semanticScore,
				score:         lexicalScore*4 + semanticScore*3 + sourceQuality,
			})
			sourceCandidates++
		}
		if sourceCandidates == 0 && source.Summary != "" && source.Ingestion.State != SourceStatusFailed {
			semanticScore, matched := scoreText(source.Summary+" "+strings.Join(source.KeyTerms, " "), queryTerms)
			if semanticScore == 0 {
				continue
			}
			candidates = append(candidates, evidenceCandidate{
				source:        source,
				text:          source.Summary,
				terms:         compactStrings(append(source.KeyTerms, matched...), 12),
				retrieval:     "semantic-summary",
				semanticScore: semanticScore,
				score:         semanticScore*3 + sourceRetrievalQuality(source),
			})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score == candidates[j].score {
			if candidates[i].lexicalScore != candidates[j].lexicalScore {
				return candidates[i].lexicalScore > candidates[j].lexicalScore
			}
			if candidates[i].source.Title == candidates[j].source.Title {
				return candidates[i].text < candidates[j].text
			}
			return candidates[i].source.Title < candidates[j].source.Title
		}
		return candidates[i].score > candidates[j].score
	})
	if limit <= 0 {
		limit = 8
		if mode == ReportModeBrief {
			limit = 4
		}
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
			SourceKind:    candidate.source.Kind,
			SourceURI:     firstNonEmpty(candidate.source.Provenance.CanonicalURI, candidate.source.Provenance.URI, candidate.source.URI),
			ChunkID:       candidate.chunk.ID,
			SectionID:     candidate.chunk.SectionID,
			SectionTitle:  firstNonEmpty(candidate.chunk.SectionTitle, candidate.section.Heading),
			CitationLabel: fmt.Sprintf("S%d", index+1),
			Excerpt:       shorten(candidate.text, 420),
			Terms:         candidate.terms,
			SourceSummary: shorten(candidate.source.Summary, 300),
			Retrieval:     candidate.retrieval,
			LexicalScore:  candidate.lexicalScore,
			SemanticScore: candidate.semanticScore,
			Score:         candidate.score,
		})
	}
	return evidence
}

func sourceRetrievalQuality(source Source) int {
	score := 0
	if source.Ingestion.State == SourceStatusReady {
		score++
	}
	if source.Summary != "" {
		score += 2
	}
	if len(source.Claims) > 0 {
		score++
	}
	if len(source.Reliability) > 0 {
		score++
	}
	return score
}

func ResearchEvidence(space Space, run ResearchRun, limit int) ([]Evidence, error) {
	normalized, err := NormalizeSpace(space)
	if err != nil {
		return nil, err
	}
	run = normalizeResearchRun(run)
	if limit <= 0 {
		limit = 40
	}
	if limit > 80 {
		limit = 80
	}
	sources := selectedSources(normalized.Sources, run.SourceIDs)
	queries := []string{run.Objective, run.Question, run.Plan.RewrittenObjective}
	queries = append(queries, run.Plan.SearchQueries...)
	queries = append(queries, run.Plan.ExpectedOutputs...)
	queries = compactStrings(queries, 24)
	if len(queries) == 0 {
		queries = compactStrings([]string{normalized.Objective, normalized.Title}, 2)
	}
	seen := map[string]bool{}
	var evidence []Evidence
	appendEvidence := func(items []Evidence) {
		for _, item := range items {
			key := item.SourceID + "|" + item.ChunkID + "|" + item.Excerpt
			if strings.TrimSpace(key) == "||" || seen[key] {
				continue
			}
			seen[key] = true
			evidence = append(evidence, item)
			if len(evidence) >= limit {
				return
			}
		}
	}
	perQueryLimit := 5
	if run.Mode == ReportModeBrief {
		perQueryLimit = 3
	}
	for _, query := range queries {
		terms := topTerms(query, 12)
		if len(terms) == 0 {
			continue
		}
		appendEvidence(rankEvidence(sources, terms, run.Mode, perQueryLimit))
		if len(evidence) >= limit {
			break
		}
	}
	if len(evidence) < limit {
		appendEvidence(rankEvidence(sources, topTerms(strings.Join(queries, " "), 18), run.Mode, limit-len(evidence)))
	}
	return relabelEvidence(evidence), nil
}

func BuildResearchCoverage(run ResearchRun, evidence []Evidence) []ResearchCoverage {
	run = normalizeResearchRun(run)
	topics := append([]string{}, run.Plan.ExpectedOutputs...)
	topics = append(topics, run.Plan.SearchQueries...)
	topics = compactStrings(topics, 18)
	if len(topics) == 0 {
		topics = compactStrings([]string{firstNonEmpty(run.Question, run.Objective, run.Plan.RewrittenObjective)}, 1)
	}
	out := make([]ResearchCoverage, 0, len(topics))
	for index, topic := range topics {
		terms := topTerms(topic, 10)
		sourceIDs := map[string]bool{}
		count := 0
		for _, item := range evidence {
			if score, _ := scoreText(item.Excerpt+" "+item.SourceTitle+" "+strings.Join(item.Terms, " "), terms); score > 0 {
				count++
				if item.SourceID != "" {
					sourceIDs[item.SourceID] = true
				}
			}
		}
		ids := make([]string, 0, len(sourceIDs))
		for sourceID := range sourceIDs {
			ids = append(ids, sourceID)
		}
		sort.Strings(ids)
		status := "gap"
		notes := "No cited evidence matched this planned research topic."
		if count > 0 {
			status = "covered"
			notes = fmt.Sprintf("%d cited evidence chunk%s matched this topic.", count, plural(count))
		}
		out = append(out, ResearchCoverage{
			ID:            fmt.Sprintf("coverage_%02d", index+1),
			Topic:         topic,
			Status:        status,
			SourceIDs:     ids,
			EvidenceCount: count,
			Notes:         notes,
		})
	}
	return normalizeResearchCoverage(out)
}

func relabelEvidence(evidence []Evidence) []Evidence {
	out := make([]Evidence, 0, len(evidence))
	for index, item := range evidence {
		item.ID = fmt.Sprintf("evidence_%02d", index+1)
		item.CitationLabel = fmt.Sprintf("S%d", index+1)
		out = append(out, item)
	}
	return out
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

func plural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
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

func cleanSourceContent(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	var lines []string
	blank := false
	for _, line := range strings.Split(strings.TrimSpace(value), "\n") {
		line = spacePattern.ReplaceAllString(strings.TrimSpace(line), " ")
		if line == "" {
			if !blank && len(lines) > 0 {
				lines = append(lines, "")
			}
			blank = true
			continue
		}
		lines = append(lines, line)
		blank = false
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
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

func containsString(values []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
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
