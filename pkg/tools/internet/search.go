package internet

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/andrewneudegg/lab/pkg/tool"
)

const (
	defaultEndpoint         = "https://api.duckduckgo.com/"
	defaultAcademicEndpoint = "https://api.openalex.org/works"
	defaultTimeout          = 10 * time.Second
	defaultUserAgent        = "Mozilla/5.0 (compatible; homelabd/1.0; +https://github.com/andrewneudegg/lab)"
)

type Base struct {
	Endpoint         string
	AcademicEndpoint string
	Timeout          time.Duration
	UserAgent        string
	Client           *http.Client
}

func Register(reg *tool.Registry, base Base) error {
	if err := reg.Register(SearchTool{base: base}); err != nil {
		return err
	}
	return reg.Register(FetchTool{base: base})
}

func schema(v string) json.RawMessage { return json.RawMessage(v) }

type SearchTool struct {
	base Base
}

func (SearchTool) Name() string { return "internet.search" }
func (SearchTool) Description() string {
	return "Search the public internet or academic papers and return concise results with URLs."
}
func (SearchTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["query"],"properties":{"query":{"type":"string"},"source":{"type":"string","enum":["web","academic","all"],"description":"web searches public web snippets; academic searches scholarly papers; all returns both"},"max_results":{"type":"integer","minimum":1,"maximum":10}}}`)
}
func (SearchTool) Risk() tool.RiskLevel { return tool.RiskReadOnly }
func (t SearchTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Query      string `json:"query"`
		Source     string `json:"source"`
		MaxResults int    `json:"max_results"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	req.Query = strings.TrimSpace(req.Query)
	if req.Query == "" {
		return nil, fmt.Errorf("query is required")
	}
	limit := req.MaxResults
	if limit <= 0 {
		limit = 5
	}
	if limit > 10 {
		limit = 10
	}
	source := strings.ToLower(strings.TrimSpace(req.Source))
	if source == "" {
		source = "web"
	}
	switch source {
	case "web":
		return t.searchWeb(ctx, req.Query, limit)
	case "academic":
		return t.searchAcademic(ctx, req.Query, limit)
	case "all":
		return t.searchAll(ctx, req.Query, limit)
	default:
		return nil, fmt.Errorf("source must be web, academic, or all")
	}
}

func (t SearchTool) searchWeb(ctx context.Context, query string, limit int) (json.RawMessage, error) {
	endpoint := t.base.Endpoint
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("q", query)
	q.Set("format", "json")
	q.Set("no_html", "1")
	q.Set("skip_disambig", "1")
	u.RawQuery = q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("User-Agent", t.base.userAgent())

	resp, err := t.base.httpClient().Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("internet search failed: %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}

	var api duckDuckGoResponse
	if err := json.Unmarshal(body, &api); err != nil {
		return nil, err
	}
	results := api.results(limit)
	return json.Marshal(map[string]any{
		"query":        query,
		"source":       "duckduckgo",
		"answer":       strings.TrimSpace(api.Answer),
		"abstract":     strings.TrimSpace(api.AbstractText),
		"abstract_url": strings.TrimSpace(api.AbstractURL),
		"results":      results,
	})
}

func (t SearchTool) searchAcademic(ctx context.Context, query string, limit int) (json.RawMessage, error) {
	results, err := t.openAlexResults(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{
		"query":   query,
		"source":  "openalex",
		"results": results,
	})
}

func (t SearchTool) searchAll(ctx context.Context, query string, limit int) (json.RawMessage, error) {
	webRaw, webErr := t.searchWeb(ctx, query, limit)
	academic, academicErr := t.openAlexResults(ctx, query, limit)
	if webErr != nil && academicErr != nil {
		return nil, fmt.Errorf("web search failed: %v; academic search failed: %v", webErr, academicErr)
	}
	out := map[string]any{
		"query":  query,
		"source": "all",
	}
	if webErr != nil {
		out["web_error"] = webErr.Error()
	} else {
		var web map[string]any
		if err := json.Unmarshal(webRaw, &web); err == nil {
			out["web"] = web
		}
	}
	if academicErr != nil {
		out["academic_error"] = academicErr.Error()
	} else {
		out["academic"] = academic
	}
	return json.Marshal(out)
}

func (t SearchTool) openAlexResults(ctx context.Context, query string, limit int) ([]map[string]any, error) {
	endpoint := t.base.AcademicEndpoint
	if endpoint == "" {
		endpoint = defaultAcademicEndpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("search", query)
	q.Set("per-page", fmt.Sprintf("%d", limit))
	q.Set("sort", "relevance_score:desc")
	u.RawQuery = q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("User-Agent", t.base.userAgent())

	resp, err := t.base.httpClient().Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("academic search failed: %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	var api openAlexResponse
	if err := json.Unmarshal(body, &api); err != nil {
		return nil, err
	}
	results := make([]map[string]any, 0, minInt(limit, len(api.Results)))
	for _, item := range api.Results {
		title := strings.TrimSpace(firstNonEmpty(item.Title, item.DisplayName))
		if title == "" {
			continue
		}
		authors := make([]string, 0, minInt(3, len(item.Authorships)))
		for _, authorship := range item.Authorships {
			name := strings.TrimSpace(authorship.Author.DisplayName)
			if name != "" {
				authors = append(authors, name)
			}
			if len(authors) == 3 {
				break
			}
		}
		landingURL := firstNonEmpty(item.PrimaryLocation.LandingPageURL, item.ID)
		result := map[string]any{
			"kind":           "academic",
			"title":          title,
			"authors":        authors,
			"year":           item.PublicationYear,
			"venue":          strings.TrimSpace(item.PrimaryLocation.Source.DisplayName),
			"doi":            strings.TrimSpace(item.DOI),
			"url":            strings.TrimSpace(landingURL),
			"pdf_url":        strings.TrimSpace(item.PrimaryLocation.PDFURL),
			"cited_by_count": item.CitedByCount,
			"snippet":        abstractSnippet(item.AbstractInvertedIndex, 360),
		}
		results = append(results, result)
		if len(results) >= limit {
			break
		}
	}
	return results, nil
}

type FetchTool struct {
	base Base
}

func (FetchTool) Name() string { return "internet.fetch" }
func (FetchTool) Description() string {
	return "Fetch a public HTTP(S) page and return bounded extracted text, title, content type, and final URL."
}
func (FetchTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["url"],"properties":{"url":{"type":"string","format":"uri"},"max_chars":{"type":"integer","minimum":500,"maximum":20000}}}`)
}
func (FetchTool) Risk() tool.RiskLevel { return tool.RiskReadOnly }
func (t FetchTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		URL      string `json:"url"`
		MaxChars int    `json:"max_chars"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	rawURL := strings.TrimSpace(req.URL)
	if rawURL == "" {
		return nil, fmt.Errorf("url is required")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("only http and https URLs are supported")
	}
	if !publicHostAllowed(u.Hostname()) {
		return nil, fmt.Errorf("only public HTTP(S) hosts are supported")
	}
	maxChars := req.MaxChars
	if maxChars <= 0 {
		maxChars = 12000
	}
	if maxChars < 500 {
		maxChars = 500
	}
	if maxChars > 20000 {
		maxChars = 20000
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,text/plain;q=0.8,application/json;q=0.7,*/*;q=0.5")
	httpReq.Header.Set("Accept-Language", "en-US,en;q=0.8")
	httpReq.Header.Set("User-Agent", t.base.userAgent())

	resp, err := t.base.httpClient().Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("page fetch failed: %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = strings.ToLower(http.DetectContentType(body))
	}
	title := ""
	text := ""
	switch {
	case strings.Contains(contentType, "text/html") || strings.Contains(contentType, "application/xhtml+xml"):
		title = extractTitle(string(body))
		text = htmlToText(string(body))
	case strings.HasPrefix(contentType, "text/") ||
		strings.Contains(contentType, "json") ||
		strings.Contains(contentType, "xml"):
		text = compactWhitespace(string(body))
	default:
		text = fmt.Sprintf("Unsupported content type for text extraction: %s", firstNonEmpty(contentType, "unknown"))
	}
	truncated := false
	if len(text) > maxChars {
		text = strings.TrimSpace(text[:maxChars])
		truncated = true
	}
	finalURL := rawURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	return json.Marshal(map[string]any{
		"url":          rawURL,
		"final_url":    finalURL,
		"status":       resp.Status,
		"content_type": contentType,
		"title":        title,
		"text":         text,
		"truncated":    truncated,
	})
}

type duckDuckGoResponse struct {
	AbstractText  string            `json:"AbstractText"`
	AbstractURL   string            `json:"AbstractURL"`
	Answer        string            `json:"Answer"`
	Definition    string            `json:"Definition"`
	DefinitionURL string            `json:"DefinitionURL"`
	Results       []duckDuckGoTopic `json:"Results"`
	RelatedTopics []duckDuckGoTopic `json:"RelatedTopics"`
}

type duckDuckGoTopic struct {
	Text     string            `json:"Text"`
	FirstURL string            `json:"FirstURL"`
	Topics   []duckDuckGoTopic `json:"Topics"`
}

type openAlexResponse struct {
	Results []openAlexWork `json:"results"`
}

type openAlexWork struct {
	ID                    string           `json:"id"`
	DOI                   string           `json:"doi"`
	Title                 string           `json:"title"`
	DisplayName           string           `json:"display_name"`
	PublicationYear       int              `json:"publication_year"`
	Type                  string           `json:"type"`
	CitedByCount          int              `json:"cited_by_count"`
	AbstractInvertedIndex map[string][]int `json:"abstract_inverted_index"`
	PrimaryLocation       openAlexLocation `json:"primary_location"`
	Authorships           []openAlexAuthor `json:"authorships"`
}

type openAlexLocation struct {
	LandingPageURL string         `json:"landing_page_url"`
	PDFURL         string         `json:"pdf_url"`
	Source         openAlexSource `json:"source"`
}

type openAlexSource struct {
	DisplayName string `json:"display_name"`
}

type openAlexAuthor struct {
	Author struct {
		DisplayName string `json:"display_name"`
	} `json:"author"`
}

func (b Base) httpClient() *http.Client {
	if b.Client != nil {
		return b.Client
	}
	timeout := b.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &http.Client{Timeout: timeout}
}

func (b Base) userAgent() string {
	userAgent := strings.TrimSpace(b.UserAgent)
	if userAgent == "" {
		return defaultUserAgent
	}
	return userAgent
}

func (r duckDuckGoResponse) results(limit int) []map[string]string {
	results := make([]map[string]string, 0, limit)
	add := func(text, rawURL string) bool {
		text = strings.TrimSpace(text)
		rawURL = strings.TrimSpace(rawURL)
		if text == "" && rawURL == "" {
			return len(results) >= limit
		}
		results = append(results, map[string]string{"title": titleFromText(text), "snippet": text, "url": rawURL})
		return len(results) >= limit
	}
	if add(r.Definition, r.DefinitionURL) {
		return results
	}
	for _, topic := range r.Results {
		if collectTopic(topic, add) {
			return results
		}
	}
	for _, topic := range r.RelatedTopics {
		if collectTopic(topic, add) {
			return results
		}
	}
	return results
}

func collectTopic(topic duckDuckGoTopic, add func(string, string) bool) bool {
	if topic.Text != "" || topic.FirstURL != "" {
		if add(topic.Text, topic.FirstURL) {
			return true
		}
	}
	for _, child := range topic.Topics {
		if collectTopic(child, add) {
			return true
		}
	}
	return false
}

func titleFromText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if before, _, ok := strings.Cut(text, " - "); ok {
		return before
	}
	if len(text) <= 80 {
		return text
	}
	return strings.TrimSpace(text[:80]) + "..."
}

var (
	htmlCommentRE = regexp.MustCompile(`(?is)<!--.*?-->`)
	htmlDropRE    = regexp.MustCompile(`(?is)<(script|style|noscript|svg|canvas|template|iframe|head|nav|footer|form|button)\b[^>]*>.*?</\s*(script|style|noscript|svg|canvas|template|iframe|head|nav|footer|form|button)\s*>`)
	htmlTagRE     = regexp.MustCompile(`(?is)<[^>]+>`)
	htmlTitleRE   = regexp.MustCompile(`(?is)<title\b[^>]*>(.*?)</title>`)
)

func extractTitle(raw string) string {
	match := htmlTitleRE.FindStringSubmatch(raw)
	if len(match) < 2 {
		return ""
	}
	return compactWhitespace(html.UnescapeString(htmlTagRE.ReplaceAllString(match[1], " ")))
}

func htmlToText(raw string) string {
	raw = htmlCommentRE.ReplaceAllString(raw, " ")
	raw = htmlDropRE.ReplaceAllString(raw, " ")
	raw = strings.NewReplacer(
		"</p>", "\n",
		"</div>", "\n",
		"</section>", "\n",
		"</article>", "\n",
		"</h1>", "\n",
		"</h2>", "\n",
		"</h3>", "\n",
		"<br>", "\n",
		"<br/>", "\n",
		"<br />", "\n",
		"</li>", "\n",
	).Replace(raw)
	raw = htmlTagRE.ReplaceAllString(raw, " ")
	return compactWhitespace(html.UnescapeString(raw))
}

func compactWhitespace(s string) string {
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}

func abstractSnippet(index map[string][]int, maxChars int) string {
	if len(index) == 0 || maxChars <= 0 {
		return ""
	}
	type positionedWord struct {
		word string
		pos  int
	}
	var words []positionedWord
	for word, positions := range index {
		for _, position := range positions {
			words = append(words, positionedWord{word: word, pos: position})
		}
	}
	sort.Slice(words, func(i, j int) bool { return words[i].pos < words[j].pos })
	parts := make([]string, 0, len(words))
	for _, word := range words {
		parts = append(parts, word.word)
	}
	snippet := strings.Join(parts, " ")
	if len(snippet) <= maxChars {
		return snippet
	}
	return strings.TrimSpace(snippet[:maxChars]) + "..."
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func publicHostAllowed(host string) bool {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return true
	}
	return !(ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified())
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
