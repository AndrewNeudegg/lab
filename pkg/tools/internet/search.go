package internet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	knowledgestore "github.com/andrewneudegg/lab/pkg/knowledge"
	"github.com/andrewneudegg/lab/pkg/tool"
)

const (
	defaultEndpoint             = "https://api.duckduckgo.com/"
	defaultSearXNGDiscoveryURL  = "https://searx.space/data/instances.json"
	defaultAcademicEndpoint     = "https://api.openalex.org/works"
	defaultBraveEndpoint        = "https://api.search.brave.com/res/v1/web/search"
	defaultTavilyEndpoint       = "https://api.tavily.com/search"
	defaultTimeout              = 10 * time.Second
	defaultUserAgent            = "Mozilla/5.0 (compatible; homelabd/1.0; +https://github.com/andrewneudegg/lab)"
	maxSearXNGInstancesPerQuery = 6
	searxNGDiscoveryTTL         = 6 * time.Hour
	httpRetryAttempts           = 4
)

type Base struct {
	Endpoint            string
	SearXNGEndpoint     string
	SearXNGInstances    []string
	SearXNGDiscoveryURL string
	AcademicEndpoint    string
	SearchProvider      string
	BraveEndpoint       string
	BraveAPIKey         string
	TavilyEndpoint      string
	TavilyAPIKey        string
	Timeout             time.Duration
	UserAgent           string
	Client              *http.Client
	Extraction          knowledgestore.TextExtractionOptions
}

var defaultSearXNGInstances = []string{
	"https://searxng.website/",
}

func Register(reg *tool.Registry, base Base) error {
	if err := reg.Register(SearchTool{base: base}); err != nil {
		return err
	}
	if err := reg.Register(ResearchTool{base: base}); err != nil {
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
	return "Search the public internet through SearXNG, or search academic papers, and return multiple concise results with URLs. Supports explicit Brave, Tavily, or DuckDuckGo fallback overrides."
}
func (SearchTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["query"],"properties":{"query":{"type":"string"},"source":{"type":"string","enum":["web","academic","all"],"description":"web searches public web snippets; academic searches scholarly papers; all returns both"},"provider":{"type":"string","enum":["auto","searxng","brave","tavily","duckduckgo"],"description":"optional web search backend override; auto defaults to SearXNG"},"max_results":{"type":"integer","minimum":1,"maximum":20},"time_range":{"type":"string","enum":["day","month","year"],"description":"optional SearXNG time range for web searches"},"language":{"type":"string","description":"optional SearXNG language code such as en or en-US"}}}`)
}
func (SearchTool) Risk() tool.RiskLevel { return tool.RiskReadOnly }
func (t SearchTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Query      string `json:"query"`
		Source     string `json:"source"`
		Provider   string `json:"provider"`
		MaxResults int    `json:"max_results"`
		TimeRange  string `json:"time_range"`
		Language   string `json:"language"`
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
		limit = 8
	}
	if limit > 20 {
		limit = 20
	}
	source := strings.ToLower(strings.TrimSpace(req.Source))
	if source == "" {
		source = "web"
	}
	options := webSearchOptions{
		Provider:  req.Provider,
		TimeRange: req.TimeRange,
		Language:  req.Language,
	}
	switch source {
	case "web":
		return t.searchWeb(ctx, req.Query, limit, options)
	case "academic":
		return t.searchAcademic(ctx, req.Query, limit)
	case "all":
		return t.searchAll(ctx, req.Query, limit, options)
	default:
		return nil, fmt.Errorf("source must be web, academic, or all")
	}
}

type webSearchOptions struct {
	Provider  string
	TimeRange string
	Language  string
}

func (t SearchTool) searchWeb(ctx context.Context, query string, limit int, options webSearchOptions) (json.RawMessage, error) {
	provider := t.base.webSearchProvider(options.Provider)
	forcedProvider := t.base.explicitWebSearchProvider(options.Provider)
	switch provider {
	case "searxng":
		raw, err := t.searchSearXNG(ctx, query, limit, options)
		if err == nil {
			return raw, nil
		}
		if forcedProvider {
			return nil, err
		}
		if t.base.braveAPIKey() != "" {
			if raw, fallbackErr := t.searchBrave(ctx, query, limit); fallbackErr == nil {
				return raw, nil
			}
		}
		if t.base.tavilyAPIKey() != "" {
			if raw, fallbackErr := t.searchTavily(ctx, query, limit); fallbackErr == nil {
				return raw, nil
			}
		}
		return t.searchDuckDuckGo(ctx, query, limit)
	case "brave":
		raw, err := t.searchBrave(ctx, query, limit)
		if err == nil {
			return raw, nil
		}
		if forcedProvider {
			return nil, err
		}
	case "tavily":
		raw, err := t.searchTavily(ctx, query, limit)
		if err == nil {
			return raw, nil
		}
		if forcedProvider {
			return nil, err
		}
	case "duckduckgo":
		return t.searchDuckDuckGo(ctx, query, limit)
	}
	return t.searchSearXNG(ctx, query, limit, options)
}

func (t SearchTool) searchSearXNG(ctx context.Context, query string, limit int, options webSearchOptions) (json.RawMessage, error) {
	instances := t.searxNGInstances(ctx)
	if len(instances) == 0 {
		return nil, fmt.Errorf("no SearXNG instances configured or discovered")
	}
	if len(instances) > maxSearXNGInstancesPerQuery {
		instances = instances[:maxSearXNGInstancesPerQuery]
	}
	minInstances := minInt(2, len(instances))
	results := make([]map[string]any, 0, limit)
	answers := make([]string, 0, 2)
	suggestions := make([]string, 0, 4)
	seenResults := map[string]bool{}
	seenAnswers := map[string]bool{}
	seenSuggestions := map[string]bool{}
	var attempted []string
	var successful []string
	var errors []string

	for i, instance := range instances {
		attempted = append(attempted, instance)
		pageResults, meta, err := t.fetchSearXNGPage(ctx, instance, query, 1, options)
		if err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", searxNGDisplayInstance(instance), err))
		} else {
			successful = append(successful, instance)
			for _, answer := range meta.Answers {
				addUniqueString(&answers, seenAnswers, answer)
			}
			for _, suggestion := range meta.Suggestions {
				addUniqueString(&suggestions, seenSuggestions, suggestion)
			}
			for _, result := range pageResults {
				if addSearchResult(&results, seenResults, result) && len(results) >= limit && i+1 >= minInstances {
					break
				}
			}
		}
		if i+1 >= minInstances && len(results) >= limit {
			break
		}
		if limit > 10 && err == nil && len(results) < limit {
			pageResults, meta, err := t.fetchSearXNGPage(ctx, instance, query, 2, options)
			if err != nil {
				errors = append(errors, fmt.Sprintf("%s page 2: %v", searxNGDisplayInstance(instance), err))
			} else {
				for _, answer := range meta.Answers {
					addUniqueString(&answers, seenAnswers, answer)
				}
				for _, suggestion := range meta.Suggestions {
					addUniqueString(&suggestions, seenSuggestions, suggestion)
				}
				for _, result := range pageResults {
					addSearchResult(&results, seenResults, result)
					if len(results) >= limit {
						break
					}
				}
			}
		}
	}
	if len(results) == 0 && len(answers) == 0 {
		return nil, fmt.Errorf("searxng search returned no usable results: %s", strings.Join(errors, "; "))
	}
	if len(results) > limit {
		results = results[:limit]
	}
	out := map[string]any{
		"query":               query,
		"source":              "searxng",
		"provider":            "searxng",
		"results":             results,
		"answers":             answers,
		"suggestions":         suggestions,
		"attempted_instances": attempted,
		"instances":           successful,
	}
	if len(answers) > 0 {
		out["answer"] = answers[0]
	}
	if len(errors) > 0 {
		out["errors"] = errors
	}
	return json.Marshal(out)
}

type searxNGPageMeta struct {
	Answers     []string
	Suggestions []string
}

func (t SearchTool) fetchSearXNGPage(ctx context.Context, endpoint, query string, page int, options webSearchOptions) ([]map[string]any, searxNGPageMeta, error) {
	raw, status, contentType, err := t.getSearXNG(ctx, endpoint, query, page, options, true)
	if err != nil {
		return nil, searxNGPageMeta{}, err
	}
	if status >= 200 && status < 300 {
		results, meta, err := searxNGJSONResults(raw, endpoint, page)
		if err == nil {
			return results, meta, nil
		}
		if !strings.Contains(strings.ToLower(contentType), "html") && !looksLikeHTML(raw) {
			return nil, searxNGPageMeta{}, err
		}
	}
	if status != http.StatusForbidden && status != http.StatusNotAcceptable && status < 300 {
		return nil, searxNGPageMeta{}, fmt.Errorf("search failed: HTTP %d", status)
	}
	raw, status, _, err = t.getSearXNG(ctx, endpoint, query, page, options, false)
	if err != nil {
		return nil, searxNGPageMeta{}, err
	}
	if status < 200 || status >= 300 {
		return nil, searxNGPageMeta{}, fmt.Errorf("search failed: HTTP %d", status)
	}
	return searxNGHTMLResults(raw, endpoint, page), searxNGPageMeta{}, nil
}

func (t SearchTool) getSearXNG(ctx context.Context, endpoint, query string, page int, options webSearchOptions, jsonFormat bool) ([]byte, int, string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, 0, "", err
	}
	q := u.Query()
	q.Set("q", query)
	q.Set("pageno", fmt.Sprintf("%d", page))
	if jsonFormat {
		q.Set("format", "json")
	}
	timeRange := strings.ToLower(strings.TrimSpace(options.TimeRange))
	if timeRange == "day" || timeRange == "month" || timeRange == "year" {
		q.Set("time_range", timeRange)
	}
	if language := strings.TrimSpace(options.Language); language != "" {
		q.Set("language", language)
	}
	u.RawQuery = q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, 0, "", err
	}
	if jsonFormat {
		httpReq.Header.Set("Accept", "application/json")
	} else {
		httpReq.Header.Set("Accept", "text/html,application/xhtml+xml")
	}
	httpReq.Header.Set("Accept-Language", searchAcceptLanguage(options.Language))
	httpReq.Header.Set("User-Agent", t.base.userAgent())
	result, err := t.base.doReadRequestWithRetry(ctx, httpReq, 2<<20)
	if err != nil {
		return nil, 0, "", err
	}
	return result.Body, result.StatusCode, result.ContentType, nil
}

func searchAcceptLanguage(language string) string {
	language = strings.TrimSpace(language)
	if language == "" {
		return "en-US,en;q=0.8"
	}
	root := strings.ToLower(language)
	if index := strings.IndexAny(root, "-_"); index >= 0 {
		root = root[:index]
	}
	if root == "" || strings.EqualFold(language, "en") || strings.EqualFold(language, "en-US") {
		return "en-US,en;q=0.8"
	}
	if strings.EqualFold(root, language) {
		return language + ",en;q=0.4"
	}
	return language + "," + root + ";q=0.8,en;q=0.4"
}

func (t SearchTool) searchDuckDuckGo(ctx context.Context, query string, limit int) (json.RawMessage, error) {
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
		"provider":     "duckduckgo",
		"answer":       strings.TrimSpace(api.Answer),
		"abstract":     strings.TrimSpace(api.AbstractText),
		"abstract_url": strings.TrimSpace(api.AbstractURL),
		"results":      results,
	})
}

func (t SearchTool) searchBrave(ctx context.Context, query string, limit int) (json.RawMessage, error) {
	apiKey := t.base.braveAPIKey()
	if apiKey == "" {
		return nil, fmt.Errorf("BRAVE_SEARCH_API_KEY is not configured")
	}
	endpoint := firstNonEmpty(t.base.BraveEndpoint, defaultBraveEndpoint)
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("q", query)
	q.Set("count", fmt.Sprintf("%d", limit))
	u.RawQuery = q.Encode()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("User-Agent", t.base.userAgent())
	httpReq.Header.Set("X-Subscription-Token", apiKey)
	resp, err := t.base.httpClient().Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("brave search failed: %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	var api braveResponse
	if err := json.Unmarshal(body, &api); err != nil {
		return nil, err
	}
	results := make([]map[string]string, 0, limit)
	for _, item := range api.Web.Results {
		title := compactWhitespace(html.UnescapeString(item.Title))
		snippet := compactWhitespace(html.UnescapeString(item.Description))
		rawURL := strings.TrimSpace(item.URL)
		if title == "" && snippet == "" && rawURL == "" {
			continue
		}
		results = append(results, map[string]string{"title": firstNonEmpty(title, rawURL), "snippet": snippet, "url": rawURL})
		if len(results) >= limit {
			break
		}
	}
	return json.Marshal(map[string]any{
		"query":    query,
		"source":   "brave",
		"provider": "brave",
		"results":  results,
	})
}

func (t SearchTool) searchTavily(ctx context.Context, query string, limit int) (json.RawMessage, error) {
	apiKey := t.base.tavilyAPIKey()
	if apiKey == "" {
		return nil, fmt.Errorf("TAVILY_API_KEY is not configured")
	}
	endpoint := firstNonEmpty(t.base.TavilyEndpoint, defaultTavilyEndpoint)
	payload := map[string]any{
		"api_key":        apiKey,
		"query":          query,
		"search_depth":   "advanced",
		"max_results":    limit,
		"include_answer": true,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", t.base.userAgent())
	resp, err := t.base.httpClient().Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("tavily search failed: %s", resp.Status)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	var api tavilyResponse
	if err := json.Unmarshal(raw, &api); err != nil {
		return nil, err
	}
	results := make([]map[string]string, 0, limit)
	for _, item := range api.Results {
		title := strings.TrimSpace(item.Title)
		snippet := compactWhitespace(item.Content)
		rawURL := strings.TrimSpace(item.URL)
		if title == "" && snippet == "" && rawURL == "" {
			continue
		}
		results = append(results, map[string]string{"title": firstNonEmpty(title, rawURL), "snippet": snippet, "url": rawURL})
		if len(results) >= limit {
			break
		}
	}
	return json.Marshal(map[string]any{
		"query":    query,
		"source":   "tavily",
		"provider": "tavily",
		"answer":   strings.TrimSpace(api.Answer),
		"results":  results,
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

func (t SearchTool) searchAll(ctx context.Context, query string, limit int, options webSearchOptions) (json.RawMessage, error) {
	webRaw, webErr := t.searchWeb(ctx, query, limit, options)
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

type ResearchTool struct {
	base Base
}

func (ResearchTool) Name() string { return "internet.research" }
func (ResearchTool) Description() string {
	return "Run a multi-query research fan-out: plan subqueries, search web and/or academic sources, fetch public pages, deduplicate evidence, and return a source bundle for the LLM to evaluate and synthesize."
}
func (ResearchTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["query"],"properties":{"query":{"type":"string"},"queries":{"type":"array","items":{"type":"string"},"description":"optional explicit fan-out search queries; when present these replace generated subqueries"},"source":{"type":"string","enum":["web","academic","all"]},"depth":{"type":"string","enum":["quick","standard","deep"]},"provider":{"type":"string","enum":["auto","searxng","brave","tavily","duckduckgo"]},"time_range":{"type":"string","enum":["day","month","year"],"description":"optional SearXNG time range for web fan-out searches"},"language":{"type":"string","description":"optional SearXNG language code such as en or en-US"},"max_searches":{"type":"integer","minimum":1,"maximum":8},"fetch":{"type":"boolean"},"trusted_domains":{"type":"array","items":{"type":"string"},"description":"optional preferred domains; adds site: fan-out queries"}}}`)
}
func (ResearchTool) Risk() tool.RiskLevel { return tool.RiskReadOnly }
func (t ResearchTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Query          string   `json:"query"`
		Queries        []string `json:"queries"`
		Source         string   `json:"source"`
		Depth          string   `json:"depth"`
		Provider       string   `json:"provider"`
		TimeRange      string   `json:"time_range"`
		Language       string   `json:"language"`
		MaxSearches    int      `json:"max_searches"`
		Fetch          *bool    `json:"fetch"`
		TrustedDomains []string `json:"trusted_domains"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	req.Query = strings.TrimSpace(req.Query)
	if req.Query == "" {
		return nil, fmt.Errorf("query is required")
	}
	source := strings.ToLower(strings.TrimSpace(req.Source))
	if source == "" {
		source = "all"
	}
	if source != "web" && source != "academic" && source != "all" {
		return nil, fmt.Errorf("source must be web, academic, or all")
	}
	depth := strings.ToLower(strings.TrimSpace(req.Depth))
	if depth == "" {
		depth = "standard"
	}
	if depth != "quick" && depth != "standard" && depth != "deep" {
		return nil, fmt.Errorf("depth must be quick, standard, or deep")
	}
	maxSearches, fetchPages := researchDefaults(depth)
	if req.MaxSearches > 0 {
		maxSearches = minInt(req.MaxSearches, 8)
	}
	if req.Fetch != nil {
		fetchPages = *req.Fetch
	}

	subqueries := compactResearchQueries(req.Queries, maxSearches)
	if len(subqueries) == 0 {
		subqueries = researchSubqueries(req.Query, source, req.TrustedDomains, maxSearches)
	}
	search := SearchTool{base: t.base}
	options := webSearchOptions{Provider: req.Provider, TimeRange: req.TimeRange, Language: req.Language}
	candidateLimit := researchCandidateLimitForDepth(depth, source)
	candidates, searchErrors := t.collectResearchCandidates(ctx, search, subqueries, source, options, candidateLimit)
	sources := candidates
	if len(sources) > candidateLimit {
		sources = sources[:candidateLimit]
	}
	if fetchPages {
		t.fetchResearchSources(ctx, sources, fetchCharsForDepth(depth))
	}
	return json.Marshal(map[string]any{
		"query":             req.Query,
		"source":            source,
		"depth":             depth,
		"method":            "plan -> fan-out search -> deduplicate URLs -> fetch top sources -> return evidence bundle for synthesis",
		"search_provider":   search.base.webSearchProvider(req.Provider),
		"plan":              researchPlan(source, fetchPages),
		"subqueries":        subqueries,
		"sources":           sources,
		"search_errors":     searchErrors,
		"follow_up_queries": followUpQueries(req.Query),
		"notes": []string{
			"Prefer official, primary, standards, or maintainer documentation when sources disagree.",
			"Use this result as evidence input; the LLM should still cite fetched source URLs when giving advice.",
		},
	})
}

type ResearchSource struct {
	Query       string `json:"query"`
	Kind        string `json:"kind"`
	Provider    string `json:"provider"`
	Title       string `json:"title"`
	URL         string `json:"url,omitempty"`
	LandingURL  string `json:"landing_url,omitempty"`
	PDFURL      string `json:"pdf_url,omitempty"`
	Domain      string `json:"domain,omitempty"`
	Snippet     string `json:"snippet,omitempty"`
	Year        int    `json:"year,omitempty"`
	Fetched     bool   `json:"fetched"`
	FetchError  string `json:"fetch_error,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	PageTitle   string `json:"page_title,omitempty"`
	Text        string `json:"text,omitempty"`
	Truncated   bool   `json:"truncated,omitempty"`
	Extractor   string `json:"extractor,omitempty"`
}

func (t ResearchTool) collectResearchCandidates(ctx context.Context, search SearchTool, subqueries []string, source string, options webSearchOptions, maxSources int) ([]*ResearchSource, []string) {
	type searchJob struct {
		query  string
		source string
	}
	var jobs []searchJob
	for _, query := range subqueries {
		if source == "web" || source == "all" {
			jobs = append(jobs, searchJob{query: query, source: "web"})
		}
		if source == "academic" || source == "all" {
			jobs = append(jobs, searchJob{query: query, source: "academic"})
		}
	}
	type searchResult struct {
		index   int
		sources []*ResearchSource
		err     string
	}
	results := make(chan searchResult, len(jobs))
	sem := make(chan struct{}, 2)
	var wg sync.WaitGroup
	for index, job := range jobs {
		index := index
		job := job
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results <- searchResult{index: index, err: ctx.Err().Error()}
				return
			}
			var raw json.RawMessage
			var err error
			if job.source == "web" {
				raw, err = search.searchWeb(ctx, job.query, minInt(maxSources, 20), options)
			} else {
				raw, err = search.searchAcademic(ctx, job.query, minInt(maxSources, 20))
			}
			if err != nil {
				results <- searchResult{index: index, err: fmt.Sprintf("%s search for %q failed: %v", job.source, job.query, err)}
				return
			}
			results <- searchResult{index: index, sources: researchSourcesFromSearchRaw(job.query, job.source, raw)}
		}()
	}
	wg.Wait()
	close(results)

	ordered := make([]searchResult, 0, len(jobs))
	for result := range results {
		ordered = append(ordered, result)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].index < ordered[j].index
	})
	seen := map[string]bool{}
	var out []*ResearchSource
	var errors []string
	for _, result := range ordered {
		if result.err != "" {
			errors = append(errors, result.err)
			continue
		}
		for _, source := range result.sources {
			key := strings.ToLower(firstNonEmpty(source.URL, source.Title+"|"+source.Snippet))
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, source)
		}
	}
	return out, errors
}

func (t ResearchTool) fetchResearchSources(ctx context.Context, sources []*ResearchSource, maxChars int) {
	fetcher := FetchTool{base: t.base}
	sem := make(chan struct{}, 3)
	var wg sync.WaitGroup
	for _, source := range sources {
		if source.URL == "" || !safePublicURL(source.URL) {
			continue
		}
		source := source
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				source.FetchError = ctx.Err().Error()
				return
			}
			fetched, err := t.fetchResearchSourceText(ctx, fetcher, source.URL, maxChars)
			if err != nil {
				primaryErr := err
				if source.Kind == "academic" && source.LandingURL != "" && !sameURL(source.URL, source.LandingURL) {
					landing, landingErr := t.fetchResearchSourceText(ctx, fetcher, source.LandingURL, maxChars)
					if landingErr != nil {
						source.FetchError = fmt.Sprintf("%v; landing page fetch failed: %v", primaryErr, landingErr)
						return
					}
					fetched = landing
					source.URL = source.LandingURL
					source.Domain = domainForURL(source.URL)
				} else {
					source.FetchError = primaryErr.Error()
					return
				}
			}
			if source.Kind == "academic" && fetched.Extractor == "html" {
				if linked, linkedURL, linkedErr := t.fetchFirstAcademicPDFLink(ctx, fetcher, fetched.PDFLinks, maxChars); linkedErr == nil {
					fetched = linked
					source.URL = linkedURL
					source.PDFURL = firstNonEmpty(source.PDFURL, linkedURL)
					source.Domain = domainForURL(linkedURL)
				}
			}
			applyFetchedResearchText(source, fetched)
		}()
	}
	wg.Wait()
}

type researchFetchedText struct {
	URL         string   `json:"url"`
	FinalURL    string   `json:"final_url"`
	Title       string   `json:"title"`
	Text        string   `json:"text"`
	ContentType string   `json:"content_type"`
	Extractor   string   `json:"extractor"`
	Truncated   bool     `json:"truncated"`
	PDFLinks    []string `json:"pdf_links"`
}

func (t ResearchTool) fetchResearchSourceText(ctx context.Context, fetcher FetchTool, rawURL string, maxChars int) (researchFetchedText, error) {
	input, _ := json.Marshal(map[string]any{"url": rawURL, "max_chars": maxChars})
	raw, err := fetcher.Run(ctx, input)
	if err != nil {
		return researchFetchedText{}, err
	}
	var fetched researchFetchedText
	if err := json.Unmarshal(raw, &fetched); err != nil {
		return researchFetchedText{}, err
	}
	return fetched, nil
}

func (t ResearchTool) fetchFirstAcademicPDFLink(ctx context.Context, fetcher FetchTool, links []string, maxChars int) (researchFetchedText, string, error) {
	var lastErr error
	for _, link := range links {
		link = strings.TrimSpace(link)
		if link == "" || !safePublicURL(link) {
			continue
		}
		fetched, err := t.fetchResearchSourceText(ctx, fetcher, link, maxChars)
		if err != nil {
			lastErr = err
			continue
		}
		if fetched.Extractor == "pdf" || strings.Contains(strings.ToLower(fetched.ContentType), "pdf") {
			return fetched, link, nil
		}
		lastErr = fmt.Errorf("linked source was not a PDF: %s", firstNonEmpty(fetched.ContentType, fetched.Extractor, "unknown"))
	}
	if lastErr != nil {
		return researchFetchedText{}, "", lastErr
	}
	return researchFetchedText{}, "", fmt.Errorf("no linked PDF candidates")
}

func applyFetchedResearchText(source *ResearchSource, fetched researchFetchedText) {
	source.Fetched = true
	source.PageTitle = strings.TrimSpace(fetched.Title)
	source.Text = strings.TrimSpace(fetched.Text)
	source.ContentType = strings.TrimSpace(fetched.ContentType)
	source.Extractor = strings.TrimSpace(fetched.Extractor)
	source.Truncated = fetched.Truncated
}

func researchSourcesFromSearchRaw(query, kind string, raw json.RawMessage) []*ResearchSource {
	var out struct {
		Source   string           `json:"source"`
		Provider string           `json:"provider"`
		Results  []map[string]any `json:"results"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	provider := firstNonEmpty(out.Provider, out.Source)
	sources := make([]*ResearchSource, 0, len(out.Results))
	for _, result := range out.Results {
		landingURL := stringFromMap(result, "landing_url", "url")
		pdfURL := stringFromMap(result, "pdf_url")
		rawURL := landingURL
		if kind == "academic" {
			rawURL = firstNonEmpty(pdfURL, rawURL)
		} else if rawURL == "" {
			rawURL = pdfURL
		}
		title := stringFromMap(result, "title", "display_name")
		snippet := stringFromMap(result, "snippet", "text", "content")
		year := intFromAny(result["year"])
		if rawURL == "" && title == "" && snippet == "" {
			continue
		}
		sources = append(sources, &ResearchSource{
			Query:      query,
			Kind:       kind,
			Provider:   provider,
			Title:      firstNonEmpty(title, rawURL, snippet),
			URL:        rawURL,
			LandingURL: landingURL,
			PDFURL:     pdfURL,
			Domain:     domainForURL(rawURL),
			Snippet:    snippet,
			Year:       year,
		})
	}
	return sources
}

func researchDefaults(depth string) (int, bool) {
	switch depth {
	case "quick":
		return 2, false
	case "deep":
		return 8, true
	default:
		return 4, true
	}
}

func fetchCharsForDepth(depth string) int {
	switch depth {
	case "deep":
		return 50000
	case "quick":
		return 8000
	default:
		return 25000
	}
}

func researchCandidateLimitForDepth(depth, source string) int {
	academicOnly := strings.EqualFold(strings.TrimSpace(source), "academic")
	switch depth {
	case "quick":
		if academicOnly {
			return 8
		}
		return 10
	case "deep":
		if academicOnly {
			return 24
		}
		return 32
	default:
		if academicOnly {
			return 16
		}
		return 20
	}
}

func researchSubqueries(query, source string, trustedDomains []string, limit int) []string {
	candidates := []string{
		query,
		query + " official documentation",
		query + " best practices current guidance",
		query + " examples implementation guide",
		query + " security reliability considerations",
		query + " changelog release notes",
	}
	if source == "academic" || source == "all" {
		candidates = append(candidates, query+" academic papers", query+" benchmark evaluation")
	}
	for _, domain := range trustedDomains {
		domain = strings.TrimSpace(strings.TrimPrefix(domain, "site:"))
		if domain != "" {
			candidates = append(candidates, "site:"+domain+" "+query)
		}
	}
	return compactResearchQueries(candidates, limit)
}

func compactResearchQueries(candidates []string, limit int) []string {
	seen := map[string]bool{}
	var out []string
	for _, candidate := range candidates {
		candidate = compactWhitespace(candidate)
		key := strings.ToLower(candidate)
		if candidate == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, candidate)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func researchPlan(source string, fetch bool) []string {
	steps := []string{
		"Break the user question into source-finding subqueries.",
		"Search across the requested source classes and deduplicate URLs.",
	}
	if fetch {
		steps = append(steps, "Fetch bounded text from top public sources for evidence.")
	}
	steps = append(steps, "Return source bundle, errors, and follow-up queries for multi-turn synthesis.")
	return steps
}

func followUpQueries(query string) []string {
	return []string{
		query + " official docs",
		query + " known limitations",
		query + " recent changes",
	}
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
		landingURL := firstNonEmpty(item.BestOALocation.LandingPageURL, firstOpenAlexLocationLandingURL(item.Locations), item.OpenAccess.OAURL, item.PrimaryLocation.LandingPageURL, item.ID)
		pdfURL := firstNonEmpty(item.BestOALocation.PDFURL, firstOpenAlexLocationPDFURL(item.Locations), item.PrimaryLocation.PDFURL)
		result := map[string]any{
			"kind":           "academic",
			"title":          title,
			"authors":        authors,
			"year":           item.PublicationYear,
			"venue":          strings.TrimSpace(item.PrimaryLocation.Source.DisplayName),
			"doi":            strings.TrimSpace(item.DOI),
			"url":            strings.TrimSpace(landingURL),
			"landing_url":    strings.TrimSpace(landingURL),
			"pdf_url":        strings.TrimSpace(pdfURL),
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

func firstOpenAlexLocationPDFURL(locations []openAlexLocation) string {
	for _, location := range locations {
		if value := strings.TrimSpace(location.PDFURL); value != "" {
			return value
		}
	}
	return ""
}

func firstOpenAlexLocationLandingURL(locations []openAlexLocation) string {
	for _, location := range locations {
		if value := strings.TrimSpace(location.LandingPageURL); value != "" {
			return value
		}
	}
	return ""
}

type FetchTool struct {
	base Base
}

func (FetchTool) Name() string { return "internet.fetch" }
func (FetchTool) Description() string {
	return "Fetch a public HTTP(S) page and return bounded extracted text, title, content type, and final URL."
}
func (FetchTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["url"],"properties":{"url":{"type":"string","format":"uri"},"max_chars":{"type":"integer","minimum":500,"maximum":50000}}}`)
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
	if maxChars > 50000 {
		maxChars = 50000
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,text/plain;q=0.8,application/json;q=0.7,*/*;q=0.5")
	httpReq.Header.Set("Accept-Language", "en-US,en;q=0.8")
	httpReq.Header.Set("User-Agent", t.base.userAgent())

	fetched, err := t.base.doReadRequestWithRetry(ctx, httpReq, 25<<20)
	if err != nil {
		return nil, err
	}
	if fetched.StatusCode < 200 || fetched.StatusCode >= 300 {
		return nil, fmt.Errorf("page fetch failed: %s", fetched.Status)
	}
	body := fetched.Body
	contentType := strings.ToLower(fetched.ContentType)
	if contentType == "" {
		contentType = strings.ToLower(http.DetectContentType(body))
	}
	title := ""
	text := ""
	extractor := ""
	var pdfLinks []string
	switch {
	case strings.Contains(contentType, "pdf") || looksLikePDFBytes(body):
		extractedTitle, extractedText, extractedBy, err := knowledgestore.ExtractFetchedText(ctx, body, contentType, t.base.Extraction)
		if err != nil {
			return nil, err
		}
		title = extractedTitle
		text = extractedText
		extractor = extractedBy
	case strings.Contains(contentType, "text/html") || strings.Contains(contentType, "application/xhtml+xml"):
		title = extractTitle(string(body))
		text = htmlToText(string(body))
		extractor = "html"
		pdfLinks = extractPDFLinksFromHTML(string(body), fetched.FinalURL, 6)
	case strings.HasPrefix(contentType, "text/") ||
		strings.Contains(contentType, "json") ||
		strings.Contains(contentType, "xml"):
		text = compactWhitespace(string(body))
		extractor = "plain-text"
	default:
		return nil, fmt.Errorf("unsupported content type for text extraction: %s", firstNonEmpty(contentType, "unknown"))
	}
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("page fetch did not contain extractable text")
	}
	truncated := false
	if len(text) > maxChars {
		text = strings.TrimSpace(text[:maxChars])
		truncated = true
	}
	return json.Marshal(map[string]any{
		"url":          rawURL,
		"final_url":    fetched.FinalURL,
		"status":       fetched.Status,
		"content_type": contentType,
		"extractor":    extractor,
		"title":        title,
		"text":         text,
		"truncated":    truncated,
		"pdf_links":    pdfLinks,
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

type searxNGResponse struct {
	Answers     []string        `json:"answers"`
	Suggestions []string        `json:"suggestions"`
	Corrections []string        `json:"corrections"`
	Results     []searxNGResult `json:"results"`
}

type searxNGResult struct {
	URL           string   `json:"url"`
	Title         string   `json:"title"`
	Content       string   `json:"content"`
	Engine        string   `json:"engine"`
	Engines       []string `json:"engines"`
	Category      string   `json:"category"`
	PublishedDate string   `json:"publishedDate"`
	Score         float64  `json:"score"`
}

type duckDuckGoTopic struct {
	Text     string            `json:"Text"`
	FirstURL string            `json:"FirstURL"`
	Topics   []duckDuckGoTopic `json:"Topics"`
}

type openAlexResponse struct {
	Results []openAlexWork `json:"results"`
}

type braveResponse struct {
	Web struct {
		Results []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Description string `json:"description"`
		} `json:"results"`
	} `json:"web"`
}

type tavilyResponse struct {
	Answer  string `json:"answer"`
	Results []struct {
		Title   string `json:"title"`
		URL     string `json:"url"`
		Content string `json:"content"`
	} `json:"results"`
}

type openAlexWork struct {
	ID                    string             `json:"id"`
	DOI                   string             `json:"doi"`
	Title                 string             `json:"title"`
	DisplayName           string             `json:"display_name"`
	PublicationYear       int                `json:"publication_year"`
	Type                  string             `json:"type"`
	CitedByCount          int                `json:"cited_by_count"`
	AbstractInvertedIndex map[string][]int   `json:"abstract_inverted_index"`
	PrimaryLocation       openAlexLocation   `json:"primary_location"`
	BestOALocation        openAlexLocation   `json:"best_oa_location"`
	Locations             []openAlexLocation `json:"locations"`
	OpenAccess            struct {
		OAURL string `json:"oa_url"`
	} `json:"open_access"`
	Authorships []openAlexAuthor `json:"authorships"`
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

type searxSpaceResponse struct {
	Instances map[string]searxSpaceInstance `json:"instances"`
}

type searxSpaceInstance struct {
	Analytics   bool   `json:"analytics"`
	Main        bool   `json:"main"`
	NetworkType string `json:"network_type"`
	Generator   string `json:"generator"`
	HTTP        struct {
		StatusCode int `json:"status_code"`
	} `json:"http"`
	Timing struct {
		Initial searxSpaceTiming `json:"initial"`
		Search  searxSpaceTiming `json:"search"`
	} `json:"timing"`
}

type searxSpaceTiming struct {
	SuccessPercentage float64 `json:"success_percentage"`
	All               struct {
		Value float64 `json:"value"`
	} `json:"all"`
}

type searxNGDiscoveryCache struct {
	mu        sync.Mutex
	fetchedAt time.Time
	instances []string
}

var globalSearXNGDiscoveryCache searxNGDiscoveryCache

type httpReadResult struct {
	Body        []byte
	StatusCode  int
	Status      string
	ContentType string
	FinalURL    string
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

func (b Base) doReadRequestWithRetry(ctx context.Context, req *http.Request, maxBytes int64) (httpReadResult, error) {
	if maxBytes <= 0 {
		maxBytes = 2 << 20
	}
	var last httpReadResult
	for attempt := 0; attempt < httpRetryAttempts; attempt++ {
		attemptReq := req.Clone(ctx)
		resp, err := b.httpClient().Do(attemptReq)
		if err != nil {
			if attempt+1 < httpRetryAttempts && retryableHTTPError(err) {
				if err := waitHTTPRetry(ctx, "", attempt); err != nil {
					return httpReadResult{}, err
				}
				continue
			}
			return httpReadResult{}, err
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
		_ = resp.Body.Close()
		result := httpReadResult{
			Body:        body,
			StatusCode:  resp.StatusCode,
			Status:      resp.Status,
			ContentType: resp.Header.Get("Content-Type"),
		}
		if resp.Request != nil && resp.Request.URL != nil {
			result.FinalURL = resp.Request.URL.String()
		} else {
			result.FinalURL = req.URL.String()
		}
		last = result
		if readErr != nil {
			return result, readErr
		}
		if int64(len(body)) > maxBytes {
			return result, fmt.Errorf("response exceeds %d byte limit", maxBytes)
		}
		if retryableHTTPStatus(resp.StatusCode) && attempt+1 < httpRetryAttempts {
			if err := waitHTTPRetry(ctx, resp.Header.Get("Retry-After"), attempt); err != nil {
				return httpReadResult{}, err
			}
			continue
		}
		return result, nil
	}
	return last, nil
}

func (b Base) webSearchProvider(override string) string {
	provider := strings.ToLower(strings.TrimSpace(override))
	switch provider {
	case "auto":
		provider = ""
	case "searxng", "brave", "tavily", "duckduckgo":
		return provider
	}
	provider = strings.ToLower(strings.TrimSpace(firstNonEmpty(b.SearchProvider, os.Getenv("HOMELABD_SEARCH_PROVIDER"))))
	switch provider {
	case "auto":
		return "searxng"
	case "searxng", "brave", "tavily", "duckduckgo":
		return provider
	}
	return "searxng"
}

func (b Base) explicitWebSearchProvider(override string) bool {
	if explicitProvider(override) {
		return true
	}
	return explicitProvider(firstNonEmpty(b.SearchProvider, os.Getenv("HOMELABD_SEARCH_PROVIDER")))
}

func (b Base) braveAPIKey() string {
	return strings.TrimSpace(firstNonEmpty(b.BraveAPIKey, os.Getenv("BRAVE_SEARCH_API_KEY")))
}

func (b Base) tavilyAPIKey() string {
	return strings.TrimSpace(firstNonEmpty(b.TavilyAPIKey, os.Getenv("TAVILY_API_KEY")))
}

func (b Base) userAgent() string {
	userAgent := strings.TrimSpace(b.UserAgent)
	if userAgent == "" {
		return defaultUserAgent
	}
	return userAgent
}

func (t SearchTool) searxNGInstances(ctx context.Context) []string {
	configured := t.base.configuredSearXNGInstances()
	if len(configured) > 0 {
		return configured
	}
	instances := append([]string{}, defaultSearXNGInstances...)
	if discovered := t.discoverSearXNGInstances(ctx); len(discovered) > 0 {
		instances = append(instances, discovered...)
	}
	return normalizeSearXNGInstances(instances)
}

func (b Base) configuredSearXNGInstances() []string {
	var values []string
	if b.SearXNGEndpoint != "" {
		values = append(values, b.SearXNGEndpoint)
	}
	values = append(values, b.SearXNGInstances...)
	for _, value := range splitList(os.Getenv("HOMELABD_SEARXNG_INSTANCES")) {
		values = append(values, value)
	}
	if endpoint := strings.TrimSpace(os.Getenv("HOMELABD_SEARXNG_ENDPOINT")); endpoint != "" {
		values = append(values, endpoint)
	}
	return normalizeSearXNGInstances(values)
}

func (t SearchTool) discoverSearXNGInstances(ctx context.Context) []string {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("HOMELABD_SEARXNG_DISCOVERY")), "0") ||
		strings.EqualFold(strings.TrimSpace(os.Getenv("HOMELABD_SEARXNG_DISCOVERY")), "false") {
		return nil
	}
	discoveryURL := firstNonEmpty(t.base.SearXNGDiscoveryURL, os.Getenv("HOMELABD_SEARXNG_DISCOVERY_URL"), defaultSearXNGDiscoveryURL)
	globalSearXNGDiscoveryCache.mu.Lock()
	if time.Since(globalSearXNGDiscoveryCache.fetchedAt) < searxNGDiscoveryTTL && len(globalSearXNGDiscoveryCache.instances) > 0 {
		instances := append([]string{}, globalSearXNGDiscoveryCache.instances...)
		globalSearXNGDiscoveryCache.mu.Unlock()
		return instances
	}
	globalSearXNGDiscoveryCache.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", t.base.userAgent())
	resp, err := t.base.httpClient().Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil
	}
	var data searxSpaceResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return nil
	}
	candidates := make([]searxSpaceCandidate, 0, len(data.Instances))
	for endpoint, instance := range data.Instances {
		endpoint = strings.TrimSpace(endpoint)
		if endpoint == "" || !strings.HasPrefix(strings.ToLower(endpoint), "https://") {
			continue
		}
		if instance.Analytics || instance.NetworkType != "normal" || instance.HTTP.StatusCode != http.StatusOK {
			continue
		}
		success := instance.Timing.Search.SuccessPercentage
		if success <= 0 {
			continue
		}
		candidates = append(candidates, searxSpaceCandidate{
			Endpoint: endpoint,
			Success:  success,
			Latency:  firstPositiveFloat(instance.Timing.Search.All.Value, instance.Timing.Initial.All.Value),
			Main:     instance.Main,
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Success != candidates[j].Success {
			return candidates[i].Success > candidates[j].Success
		}
		if candidates[i].Main != candidates[j].Main {
			return candidates[i].Main
		}
		return candidates[i].Latency < candidates[j].Latency
	})
	instances := make([]string, 0, minInt(24, len(candidates)))
	for _, candidate := range candidates {
		instances = append(instances, candidate.Endpoint)
		if len(instances) >= 24 {
			break
		}
	}
	instances = normalizeSearXNGInstances(instances)
	if len(instances) > 0 {
		globalSearXNGDiscoveryCache.mu.Lock()
		globalSearXNGDiscoveryCache.fetchedAt = time.Now()
		globalSearXNGDiscoveryCache.instances = append([]string{}, instances...)
		globalSearXNGDiscoveryCache.mu.Unlock()
	}
	return instances
}

type searxSpaceCandidate struct {
	Endpoint string
	Success  float64
	Latency  float64
	Main     bool
}

func searxNGJSONResults(raw []byte, instance string, page int) ([]map[string]any, searxNGPageMeta, error) {
	var api searxNGResponse
	if err := json.Unmarshal(raw, &api); err != nil {
		return nil, searxNGPageMeta{}, err
	}
	results := make([]map[string]any, 0, len(api.Results))
	for index, item := range api.Results {
		rawURL := strings.TrimSpace(item.URL)
		title := compactWhitespace(html.UnescapeString(item.Title))
		snippet := compactWhitespace(html.UnescapeString(item.Content))
		if rawURL == "" && title == "" && snippet == "" {
			continue
		}
		result := map[string]any{
			"title":           firstNonEmpty(title, rawURL, snippet),
			"snippet":         snippet,
			"url":             rawURL,
			"domain":          domainForURL(rawURL),
			"provider":        "searxng",
			"source_instance": searxNGDisplayInstance(instance),
			"rank":            ((page - 1) * 10) + index + 1,
		}
		if item.Engine != "" {
			result["engine"] = strings.TrimSpace(item.Engine)
		}
		if len(item.Engines) > 0 {
			result["engines"] = cleanStringSlice(item.Engines)
		}
		if item.Category != "" {
			result["category"] = strings.TrimSpace(item.Category)
		}
		if item.PublishedDate != "" {
			result["published_date"] = strings.TrimSpace(item.PublishedDate)
		}
		if item.Score != 0 {
			result["score"] = item.Score
		}
		results = append(results, result)
	}
	meta := searxNGPageMeta{
		Answers:     cleanStringSlice(api.Answers),
		Suggestions: append(cleanStringSlice(api.Suggestions), cleanStringSlice(api.Corrections)...),
	}
	return results, meta, nil
}

func searxNGHTMLResults(raw []byte, instance string, page int) []map[string]any {
	baseURL, _ := url.Parse(instance)
	matches := searxNGHTMLResultRE.FindAllSubmatch(raw, -1)
	results := make([]map[string]any, 0, len(matches))
	for index, match := range matches {
		if len(match) < 2 {
			continue
		}
		body := string(match[1])
		link := searxNGHTMLLinkRE.FindStringSubmatch(body)
		if len(link) < 3 {
			continue
		}
		rawURL := cleanSearXNGResultURL(html.UnescapeString(link[1]), baseURL)
		title := htmlToText(link[2])
		snippet := ""
		if content := searxNGHTMLContentRE.FindStringSubmatch(body); len(content) >= 2 {
			snippet = htmlToText(content[1])
		}
		if rawURL == "" && title == "" && snippet == "" {
			continue
		}
		results = append(results, map[string]any{
			"title":           firstNonEmpty(title, rawURL, snippet),
			"snippet":         snippet,
			"url":             rawURL,
			"domain":          domainForURL(rawURL),
			"provider":        "searxng",
			"source_instance": searxNGDisplayInstance(instance),
			"rank":            ((page - 1) * 10) + index + 1,
		})
	}
	return results
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
	htmlCommentRE        = regexp.MustCompile(`(?is)<!--.*?-->`)
	htmlDropRE           = regexp.MustCompile(`(?is)<(script|style|noscript|svg|canvas|template|iframe|head|nav|footer|form|button)\b[^>]*>.*?</\s*(script|style|noscript|svg|canvas|template|iframe|head|nav|footer|form|button)\s*>`)
	htmlTagRE            = regexp.MustCompile(`(?is)<[^>]+>`)
	htmlTitleRE          = regexp.MustCompile(`(?is)<title\b[^>]*>(.*?)</title>`)
	htmlMetaTagRE        = regexp.MustCompile(`(?is)<meta\b[^>]*>`)
	htmlAttrRE           = regexp.MustCompile(`(?is)\s([a-zA-Z_:.-]+)\s*=\s*("([^"]*)"|'([^']*)'|([^\s"'>]+))`)
	searxNGHTMLResultRE  = regexp.MustCompile(`(?is)<article\b[^>]*class=["'][^"']*\bresult\b[^"']*["'][^>]*>(.*?)</article>`)
	searxNGHTMLLinkRE    = regexp.MustCompile(`(?is)<a\b[^>]*href=["']([^"']+)["'][^>]*>(.*?)</a>`)
	searxNGHTMLContentRE = regexp.MustCompile(`(?is)<p\b[^>]*class=["'][^"']*\bcontent\b[^"']*["'][^>]*>(.*?)</p>`)
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

func extractPDFLinksFromHTML(raw, baseURL string, limit int) []string {
	if limit <= 0 {
		limit = 6
	}
	seen := map[string]bool{}
	var links []string
	add := func(candidate string, force bool) {
		if len(links) >= limit {
			return
		}
		link := absolutePublicURL(candidate, baseURL)
		if link == "" {
			return
		}
		if !force && !looksLikePDFLink(link) {
			return
		}
		key := strings.ToLower(link)
		if seen[key] {
			return
		}
		seen[key] = true
		links = append(links, link)
	}
	for _, match := range htmlMetaTagRE.FindAllString(raw, -1) {
		attrs := htmlAttributes(match)
		name := strings.ToLower(firstNonEmpty(attrs["name"], attrs["property"]))
		content := attrs["content"]
		add(content, name == "citation_pdf_url")
	}
	for _, match := range searxNGHTMLLinkRE.FindAllStringSubmatch(raw, -1) {
		if len(match) < 3 {
			continue
		}
		href := match[1]
		label := strings.ToLower(htmlToText(match[2]))
		add(href, strings.Contains(label, "pdf"))
	}
	return links
}

func htmlAttributes(tag string) map[string]string {
	attrs := map[string]string{}
	for _, match := range htmlAttrRE.FindAllStringSubmatch(tag, -1) {
		if len(match) < 6 {
			continue
		}
		value := firstNonEmpty(match[3], match[4], match[5])
		attrs[strings.ToLower(match[1])] = strings.TrimSpace(html.UnescapeString(value))
	}
	return attrs
}

func absolutePublicURL(candidate, baseURL string) string {
	candidate = strings.TrimSpace(html.UnescapeString(candidate))
	if candidate == "" {
		return ""
	}
	parsed, err := url.Parse(candidate)
	if err != nil {
		return ""
	}
	if !parsed.IsAbs() {
		base, err := url.Parse(baseURL)
		if err != nil {
			return ""
		}
		parsed = base.ResolveReference(parsed)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	if !publicHostAllowed(parsed.Hostname()) {
		return ""
	}
	return parsed.String()
}

func looksLikePDFLink(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	lowerPath := strings.ToLower(parsed.EscapedPath())
	lowerQuery := strings.ToLower(parsed.RawQuery)
	return strings.Contains(lowerPath, ".pdf") ||
		strings.Contains(lowerQuery, ".pdf") ||
		strings.Contains(lowerQuery, "type=printable") ||
		strings.Contains(lowerQuery, "download=1")
}

func compactWhitespace(s string) string {
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}

func sameURL(a, b string) bool {
	ua, errA := url.Parse(strings.TrimSpace(a))
	ub, errB := url.Parse(strings.TrimSpace(b))
	if errA != nil || errB != nil {
		return strings.TrimRight(strings.TrimSpace(a), "/") == strings.TrimRight(strings.TrimSpace(b), "/")
	}
	return strings.EqualFold(ua.Scheme, ub.Scheme) &&
		strings.EqualFold(ua.Host, ub.Host) &&
		strings.TrimRight(ua.EscapedPath(), "/") == strings.TrimRight(ub.EscapedPath(), "/") &&
		ua.RawQuery == ub.RawQuery
}

func looksLikePDFBytes(body []byte) bool {
	return len(body) >= 5 && string(body[:5]) == "%PDF-"
}

func retryableHTTPStatus(status int) bool {
	switch status {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func retryableHTTPError(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func waitHTTPRetry(ctx context.Context, retryAfter string, attempt int) error {
	delay := retryAfterDelay(retryAfter)
	if delay <= 0 {
		delay = time.Duration(150*(1<<attempt)) * time.Millisecond
	}
	if delay > 3*time.Second {
		delay = 3 * time.Second
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func retryAfterDelay(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil {
		return time.Until(at)
	}
	return 0
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

func explicitProvider(provider string) bool {
	provider = strings.ToLower(strings.TrimSpace(provider))
	return provider != "" && provider != "auto"
}

func firstPositiveFloat(values ...float64) float64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 999
}

func splitList(value string) []string {
	value = strings.NewReplacer("\n", ",", "\t", ",", " ", ",", ";", ",").Replace(value)
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func normalizeSearXNGInstances(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		endpoint := normalizeSearXNGEndpoint(value)
		if endpoint == "" || seen[endpoint] {
			continue
		}
		seen[endpoint] = true
		out = append(out, endpoint)
	}
	return out
}

func normalizeSearXNGEndpoint(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return ""
	}
	u.RawQuery = ""
	u.Fragment = ""
	path := strings.TrimRight(u.Path, "/")
	if path == "" {
		path = "/search"
	} else if !strings.HasSuffix(path, "/search") {
		path += "/search"
	}
	u.Path = path
	return u.String()
}

func searxNGDisplayInstance(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return strings.TrimSpace(endpoint)
	}
	return u.Host
}

func cleanSearXNGResultURL(raw string, baseURL *url.URL) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "#") {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if baseURL != nil {
		u = baseURL.ResolveReference(u)
	}
	if u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	if baseURL != nil && strings.EqualFold(u.Host, baseURL.Host) {
		if target := u.Query().Get("url"); target != "" {
			return cleanSearXNGResultURL(target, nil)
		}
		return ""
	}
	u.Fragment = ""
	return u.String()
}

func cleanStringSlice(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = compactWhitespace(html.UnescapeString(value))
		key := strings.ToLower(value)
		if value == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
	}
	return out
}

func looksLikeHTML(raw []byte) bool {
	trimmed := strings.TrimSpace(string(raw))
	return strings.HasPrefix(trimmed, "<!doctype") ||
		strings.HasPrefix(trimmed, "<html") ||
		strings.HasPrefix(trimmed, "<body") ||
		strings.HasPrefix(trimmed, "<article")
}

func addUniqueString(values *[]string, seen map[string]bool, value string) {
	value = compactWhitespace(html.UnescapeString(value))
	key := strings.ToLower(value)
	if value == "" || seen[key] {
		return
	}
	seen[key] = true
	*values = append(*values, value)
}

func addSearchResult(results *[]map[string]any, seen map[string]bool, result map[string]any) bool {
	key := searchResultKey(result)
	if key == "" || seen[key] {
		return false
	}
	seen[key] = true
	*results = append(*results, result)
	return true
}

func searchResultKey(result map[string]any) string {
	rawURL := stringFromAny(result["url"])
	if normalized := normalizeResultURL(rawURL); normalized != "" {
		return normalized
	}
	title := strings.ToLower(firstNonEmpty(stringFromAny(result["title"]), stringFromAny(result["snippet"])))
	return compactWhitespace(title)
}

func normalizeResultURL(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Hostname() == "" {
		return ""
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	if u.Path == "" {
		u.Path = "/"
	}
	q := u.Query()
	for key := range q {
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "utm_") ||
			lower == "fbclid" ||
			lower == "gclid" ||
			lower == "dclid" ||
			lower == "mc_cid" ||
			lower == "mc_eid" {
			q.Del(key)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func stringFromMap(values map[string]any, keys ...string) string {
	for _, key := range keys {
		value := stringFromAny(values[key])
		if value != "" {
			return value
		}
	}
	return ""
}

func stringFromAny(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	default:
		return ""
	}
}

func intFromAny(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	default:
		return 0
	}
}

func safePublicURL(rawURL string) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && publicHostAllowed(u.Hostname())
}

func domainForURL(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.ToLower(u.Hostname()))
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
