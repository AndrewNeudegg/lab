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
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/andrewneudegg/lab/pkg/tool"
)

const (
	defaultEndpoint         = "https://api.duckduckgo.com/"
	defaultAcademicEndpoint = "https://api.openalex.org/works"
	defaultBraveEndpoint    = "https://api.search.brave.com/res/v1/web/search"
	defaultTavilyEndpoint   = "https://api.tavily.com/search"
	defaultTimeout          = 10 * time.Second
	defaultUserAgent        = "Mozilla/5.0 (compatible; homelabd/1.0; +https://github.com/andrewneudegg/lab)"
)

type Base struct {
	Endpoint         string
	AcademicEndpoint string
	SearchProvider   string
	BraveEndpoint    string
	BraveAPIKey      string
	TavilyEndpoint   string
	TavilyAPIKey     string
	Timeout          time.Duration
	UserAgent        string
	Client           *http.Client
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
	return "Search the public internet or academic papers and return concise results with URLs. Supports Brave or Tavily via environment keys, with DuckDuckGo fallback."
}
func (SearchTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["query"],"properties":{"query":{"type":"string"},"source":{"type":"string","enum":["web","academic","all"],"description":"web searches public web snippets; academic searches scholarly papers; all returns both"},"provider":{"type":"string","enum":["auto","brave","tavily","duckduckgo"],"description":"optional web search backend override"},"max_results":{"type":"integer","minimum":1,"maximum":10}}}`)
}
func (SearchTool) Risk() tool.RiskLevel { return tool.RiskReadOnly }
func (t SearchTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Query      string `json:"query"`
		Source     string `json:"source"`
		Provider   string `json:"provider"`
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
		return t.searchWeb(ctx, req.Query, limit, req.Provider)
	case "academic":
		return t.searchAcademic(ctx, req.Query, limit)
	case "all":
		return t.searchAll(ctx, req.Query, limit, req.Provider)
	default:
		return nil, fmt.Errorf("source must be web, academic, or all")
	}
}

func (t SearchTool) searchWeb(ctx context.Context, query string, limit int, providerOverride string) (json.RawMessage, error) {
	provider := t.base.webSearchProvider(providerOverride)
	switch provider {
	case "brave":
		raw, err := t.searchBrave(ctx, query, limit)
		if err == nil {
			return raw, nil
		}
		if strings.TrimSpace(providerOverride) != "" {
			return nil, err
		}
	case "tavily":
		raw, err := t.searchTavily(ctx, query, limit)
		if err == nil {
			return raw, nil
		}
		if strings.TrimSpace(providerOverride) != "" {
			return nil, err
		}
	case "duckduckgo":
		return t.searchDuckDuckGo(ctx, query, limit)
	}
	return t.searchDuckDuckGo(ctx, query, limit)
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

func (t SearchTool) searchAll(ctx context.Context, query string, limit int, providerOverride string) (json.RawMessage, error) {
	webRaw, webErr := t.searchWeb(ctx, query, limit, providerOverride)
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
	return "Run a bounded multi-query research fan-out: plan subqueries, search web and/or academic sources, fetch top pages, deduplicate evidence, and return a source bundle for the LLM to synthesize."
}
func (ResearchTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["query"],"properties":{"query":{"type":"string"},"source":{"type":"string","enum":["web","academic","all"]},"depth":{"type":"string","enum":["quick","standard","deep"]},"provider":{"type":"string","enum":["auto","brave","tavily","duckduckgo"]},"max_searches":{"type":"integer","minimum":1,"maximum":8},"max_sources":{"type":"integer","minimum":1,"maximum":20},"fetch":{"type":"boolean"},"trusted_domains":{"type":"array","items":{"type":"string"},"description":"optional preferred domains; adds site: fan-out queries"}}}`)
}
func (ResearchTool) Risk() tool.RiskLevel { return tool.RiskReadOnly }
func (t ResearchTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Query          string   `json:"query"`
		Source         string   `json:"source"`
		Depth          string   `json:"depth"`
		Provider       string   `json:"provider"`
		MaxSearches    int      `json:"max_searches"`
		MaxSources     int      `json:"max_sources"`
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
	maxSearches, maxSources, fetchPages := researchDefaults(depth)
	if req.MaxSearches > 0 {
		maxSearches = minInt(req.MaxSearches, 8)
	}
	if req.MaxSources > 0 {
		maxSources = minInt(req.MaxSources, 20)
	}
	if req.Fetch != nil {
		fetchPages = *req.Fetch
	}

	subqueries := researchSubqueries(req.Query, source, req.TrustedDomains, maxSearches)
	search := SearchTool{base: t.base}
	candidates, searchErrors := t.collectResearchCandidates(ctx, search, subqueries, source, req.Provider, maxSources)
	sources := candidates
	if len(sources) > maxSources {
		sources = sources[:maxSources]
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
	Domain      string `json:"domain,omitempty"`
	Snippet     string `json:"snippet,omitempty"`
	Year        int    `json:"year,omitempty"`
	Fetched     bool   `json:"fetched"`
	FetchError  string `json:"fetch_error,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	PageTitle   string `json:"page_title,omitempty"`
	Text        string `json:"text,omitempty"`
	Truncated   bool   `json:"truncated,omitempty"`
}

func (t ResearchTool) collectResearchCandidates(ctx context.Context, search SearchTool, subqueries []string, source, provider string, maxSources int) ([]*ResearchSource, []string) {
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
		sources []*ResearchSource
		err     string
	}
	results := make(chan searchResult, len(jobs))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for _, job := range jobs {
		job := job
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results <- searchResult{err: ctx.Err().Error()}
				return
			}
			var raw json.RawMessage
			var err error
			if job.source == "web" {
				raw, err = search.searchWeb(ctx, job.query, minInt(maxSources, 10), provider)
			} else {
				raw, err = search.searchAcademic(ctx, job.query, minInt(maxSources, 10))
			}
			if err != nil {
				results <- searchResult{err: fmt.Sprintf("%s search for %q failed: %v", job.source, job.query, err)}
				return
			}
			results <- searchResult{sources: researchSourcesFromSearchRaw(job.query, job.source, raw)}
		}()
	}
	wg.Wait()
	close(results)

	seen := map[string]bool{}
	var out []*ResearchSource
	var errors []string
	for result := range results {
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
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind == "web"
		}
		return out[i].Title < out[j].Title
	})
	return out, errors
}

func (t ResearchTool) fetchResearchSources(ctx context.Context, sources []*ResearchSource, maxChars int) {
	fetcher := FetchTool{base: t.base}
	sem := make(chan struct{}, 4)
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
			input, _ := json.Marshal(map[string]any{"url": source.URL, "max_chars": maxChars})
			raw, err := fetcher.Run(ctx, input)
			if err != nil {
				source.FetchError = err.Error()
				return
			}
			var fetched struct {
				Title       string `json:"title"`
				Text        string `json:"text"`
				ContentType string `json:"content_type"`
				Truncated   bool   `json:"truncated"`
			}
			if err := json.Unmarshal(raw, &fetched); err != nil {
				source.FetchError = err.Error()
				return
			}
			source.Fetched = true
			source.PageTitle = strings.TrimSpace(fetched.Title)
			source.Text = strings.TrimSpace(fetched.Text)
			source.ContentType = strings.TrimSpace(fetched.ContentType)
			source.Truncated = fetched.Truncated
		}()
	}
	wg.Wait()
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
		rawURL := stringFromMap(result, "url", "pdf_url")
		title := stringFromMap(result, "title", "display_name")
		snippet := stringFromMap(result, "snippet", "text", "content")
		year := intFromAny(result["year"])
		if rawURL == "" && title == "" && snippet == "" {
			continue
		}
		sources = append(sources, &ResearchSource{
			Query:    query,
			Kind:     kind,
			Provider: provider,
			Title:    firstNonEmpty(title, rawURL, snippet),
			URL:      rawURL,
			Domain:   domainForURL(rawURL),
			Snippet:  snippet,
			Year:     year,
		})
	}
	return sources
}

func researchDefaults(depth string) (int, int, bool) {
	switch depth {
	case "quick":
		return 2, 4, false
	case "deep":
		return 8, 16, true
	default:
		return 4, 8, true
	}
}

func fetchCharsForDepth(depth string) int {
	switch depth {
	case "deep":
		return 6000
	case "quick":
		return 1500
	default:
		return 3000
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

func (b Base) webSearchProvider(override string) string {
	provider := strings.ToLower(strings.TrimSpace(override))
	switch provider {
	case "brave", "tavily", "duckduckgo":
		return provider
	}
	provider = strings.ToLower(strings.TrimSpace(firstNonEmpty(b.SearchProvider, os.Getenv("HOMELABD_SEARCH_PROVIDER"))))
	switch provider {
	case "brave", "tavily", "duckduckgo":
		return provider
	}
	if b.braveAPIKey() != "" {
		return "brave"
	}
	if b.tavilyAPIKey() != "" {
		return "tavily"
	}
	return "duckduckgo"
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
