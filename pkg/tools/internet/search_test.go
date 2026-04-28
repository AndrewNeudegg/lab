package internet

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSearchToolReturnsLimitedResults(t *testing.T) {
	var gotQuery string
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotQuery = r.URL.Query().Get("q")
		if r.URL.Query().Get("format") != "json" {
			t.Fatalf("expected json format query")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
			"Answer":"42",
			"AbstractText":"Example abstract",
			"AbstractURL":"https://example.com/abstract",
			"Results":[
				{"Text":"First result - useful context","FirstURL":"https://example.com/first"},
				{"Text":"Second result","FirstURL":"https://example.com/second"}
			],
			"RelatedTopics":[
				{"Text":"Third result","FirstURL":"https://example.com/third"}
			]
		}`)),
		}, nil
	})}

	raw, err := SearchTool{base: Base{Endpoint: "https://search.example/", SearchProvider: "duckduckgo", Client: client}}.Run(context.Background(), json.RawMessage(`{"query":"golang testing","max_results":2}`))
	if err != nil {
		t.Fatalf("run search: %v", err)
	}
	if gotQuery != "golang testing" {
		t.Fatalf("unexpected query: %q", gotQuery)
	}

	var result struct {
		Query       string              `json:"query"`
		Source      string              `json:"source"`
		Answer      string              `json:"answer"`
		Abstract    string              `json:"abstract"`
		AbstractURL string              `json:"abstract_url"`
		Results     []map[string]string `json:"results"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.Source != "duckduckgo" || result.Answer != "42" || result.Abstract != "Example abstract" || result.AbstractURL != "https://example.com/abstract" {
		t.Fatalf("unexpected metadata: %+v", result)
	}
	if len(result.Results) != 2 {
		t.Fatalf("expected 2 results, got %d: %+v", len(result.Results), result.Results)
	}
	if result.Results[0]["title"] != "First result" || result.Results[0]["url"] != "https://example.com/first" {
		t.Fatalf("unexpected first result: %+v", result.Results[0])
	}
}

func TestSearchToolUsesSearXNGByDefaultAndAggregatesInstances(t *testing.T) {
	requestsByHost := map[string]int{}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requestsByHost[r.URL.Host]++
		if r.URL.Path != "/search" {
			t.Fatalf("expected /search path, got %q", r.URL.Path)
		}
		if r.URL.Query().Get("format") != "json" {
			t.Fatalf("expected SearXNG json format")
		}
		if r.URL.Query().Get("q") != "golang docs" {
			t.Fatalf("unexpected query: %q", r.URL.Query().Get("q"))
		}
		if r.URL.Query().Get("time_range") != "month" {
			t.Fatalf("expected SearXNG time_range=month")
		}
		if r.URL.Query().Get("language") != "en" {
			t.Fatalf("expected SearXNG language=en")
		}
		body := `{"answers":["Go documentation"],"suggestions":["golang testing"],"results":[
			{"title":"Go docs","url":"https://go.dev/doc/","content":"Official Go documentation.","engine":"duckduckgo","engines":["duckduckgo","brave"],"category":"general","score":2.4},
			{"title":"Package testing","url":"https://pkg.go.dev/testing?utm_source=tracker","content":"Testing package docs.","engine":"brave","category":"general"}
		]}`
		if r.URL.Host == "two.example" {
			body = `{"results":[
				{"title":"Package testing duplicate","url":"https://pkg.go.dev/testing","content":"Duplicate without tracker."},
				{"title":"Effective Go","url":"https://go.dev/doc/effective_go","content":"Writing clear Go."}
			]}`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})}

	raw, err := SearchTool{base: Base{SearXNGInstances: []string{"https://one.example/", "https://two.example/"}, Client: client}}.Run(context.Background(), json.RawMessage(`{"query":"golang docs","max_results":3,"time_range":"month","language":"en"}`))
	if err != nil {
		t.Fatalf("run searxng search: %v", err)
	}
	if requestsByHost["one.example"] != 1 || requestsByHost["two.example"] != 1 {
		t.Fatalf("expected both SearXNG instances queried, got %+v", requestsByHost)
	}
	var result struct {
		Source      string           `json:"source"`
		Provider    string           `json:"provider"`
		Answer      string           `json:"answer"`
		Answers     []string         `json:"answers"`
		Suggestions []string         `json:"suggestions"`
		Results     []map[string]any `json:"results"`
		Instances   []string         `json:"instances"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.Source != "searxng" || result.Provider != "searxng" || result.Answer != "Go documentation" {
		t.Fatalf("unexpected metadata: %+v", result)
	}
	if len(result.Results) != 3 {
		t.Fatalf("expected 3 deduplicated results, got %d: %+v", len(result.Results), result.Results)
	}
	if result.Results[0]["source_instance"] != "one.example" || result.Results[2]["source_instance"] != "two.example" {
		t.Fatalf("expected source instance metadata, got %+v", result.Results)
	}
	if len(result.Instances) != 2 || len(result.Answers) != 1 || len(result.Suggestions) != 1 {
		t.Fatalf("expected instance and answer metadata, got %+v", result)
	}
}

func TestSearchToolFallsBackToSearXNGHTMLWhenJSONDisabled(t *testing.T) {
	var formats []string
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		formats = append(formats, r.URL.Query().Get("format"))
		if r.URL.Query().Get("format") == "json" {
			return &http.Response{
				StatusCode: http.StatusForbidden,
				Status:     "403 Forbidden",
				Header:     http.Header{"Content-Type": []string{"text/html"}},
				Body:       io.NopCloser(strings.NewReader("json disabled")),
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"text/html"}},
			Body: io.NopCloser(strings.NewReader(`<html><body>
				<article class="result result-default">
					<h3><a href="https://docs.example.com/guide">Docs &amp; guide</a></h3>
					<p class="content">Useful &amp; current documentation.</p>
				</article>
			</body></html>`)),
		}, nil
	})}

	raw, err := SearchTool{base: Base{SearXNGInstances: []string{"https://html.example/"}, Client: client}}.Run(context.Background(), json.RawMessage(`{"query":"docs","provider":"searxng"}`))
	if err != nil {
		t.Fatalf("run html fallback search: %v", err)
	}
	if len(formats) != 2 || formats[0] != "json" || formats[1] != "" {
		t.Fatalf("expected json request followed by html request, got %+v", formats)
	}
	var result struct {
		Results []map[string]any `json:"results"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(result.Results) != 1 || result.Results[0]["title"] != "Docs & guide" || result.Results[0]["url"] != "https://docs.example.com/guide" {
		t.Fatalf("unexpected html fallback result: %+v", result.Results)
	}
}

func TestSearchToolReturnsAcademicResults(t *testing.T) {
	var gotSearch string
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotSearch = r.URL.Query().Get("search")
		if r.URL.Query().Get("per-page") != "2" {
			t.Fatalf("expected per-page=2, got %q", r.URL.Query().Get("per-page"))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
			"results":[{
				"id":"https://openalex.org/W1",
				"doi":"https://doi.org/10.1000/example",
				"title":"Useful paper",
				"publication_year":2025,
				"cited_by_count":7,
				"primary_location":{
					"landing_page_url":"https://example.org/paper",
					"pdf_url":"https://example.org/paper.pdf",
					"source":{"display_name":"Example Journal"}
				},
				"authorships":[
					{"author":{"display_name":"Ada Lovelace"}},
					{"author":{"display_name":"Grace Hopper"}}
				],
				"abstract_inverted_index":{"This":[0],"helps":[1],"agents":[2]}
			}]
		}`)),
		}, nil
	})}

	raw, err := SearchTool{base: Base{AcademicEndpoint: "https://academic.example/works", Client: client}}.Run(context.Background(), json.RawMessage(`{"query":"agent web search","source":"academic","max_results":2}`))
	if err != nil {
		t.Fatalf("run academic search: %v", err)
	}
	if gotSearch != "agent web search" {
		t.Fatalf("unexpected search query: %q", gotSearch)
	}

	var result struct {
		Query   string           `json:"query"`
		Source  string           `json:"source"`
		Results []map[string]any `json:"results"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.Source != "openalex" || len(result.Results) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Results[0]["title"] != "Useful paper" || result.Results[0]["url"] != "https://example.org/paper" {
		t.Fatalf("unexpected academic result: %+v", result.Results[0])
	}
	if result.Results[0]["snippet"] != "This helps agents" {
		t.Fatalf("unexpected abstract snippet: %+v", result.Results[0]["snippet"])
	}
}

func TestSearchToolUsesBraveWhenConfigured(t *testing.T) {
	var gotToken string
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotToken = r.Header.Get("X-Subscription-Token")
		if r.URL.Query().Get("q") != "agent research" {
			t.Fatalf("unexpected brave query: %q", r.URL.Query().Get("q"))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
				"web":{"results":[{"title":"Brave result","url":"https://example.com/brave","description":"Useful brave snippet"}]}
			}`)),
		}, nil
	})}

	raw, err := SearchTool{base: Base{BraveEndpoint: "https://brave.example/search", BraveAPIKey: "token", Client: client}}.Run(context.Background(), json.RawMessage(`{"query":"agent research","provider":"brave"}`))
	if err != nil {
		t.Fatalf("run brave search: %v", err)
	}
	if gotToken != "token" {
		t.Fatalf("got token %q, want configured token", gotToken)
	}
	var result struct {
		Source   string              `json:"source"`
		Provider string              `json:"provider"`
		Results  []map[string]string `json:"results"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.Source != "brave" || result.Provider != "brave" || len(result.Results) != 1 {
		t.Fatalf("unexpected brave result: %+v", result)
	}
}

func TestResearchToolFansOutSearchesAndFetchesSources(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Host {
		case "search.example":
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body: io.NopCloser(strings.NewReader(`{
					"Results":[{"Text":"Official docs - implementation guidance","FirstURL":"https://docs.example.com/guide"}]
				}`)),
			}, nil
		case "academic.example":
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body: io.NopCloser(strings.NewReader(`{
					"results":[{
						"id":"https://openalex.org/W1",
						"title":"Research paper",
						"publication_year":2026,
						"primary_location":{"landing_page_url":"https://papers.example.org/paper","source":{"display_name":"Journal"}},
						"abstract_inverted_index":{"Agentic":[0],"research":[1],"works":[2]}
					}]
				}`)),
			}, nil
		case "docs.example.com", "papers.example.org":
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     http.Header{"Content-Type": []string{"text/html"}},
				Request:    r,
				Body:       io.NopCloser(strings.NewReader(`<html><head><title>Fetched</title></head><body><main>Fetched source evidence for research.</main></body></html>`)),
			}, nil
		default:
			t.Fatalf("unexpected host %q", r.URL.Host)
			return nil, nil
		}
	})}

	raw, err := ResearchTool{base: Base{Endpoint: "https://search.example/", AcademicEndpoint: "https://academic.example/works", SearchProvider: "duckduckgo", Client: client}}.Run(context.Background(), json.RawMessage(`{"query":"agent research","source":"all","depth":"standard","max_searches":2,"max_sources":3}`))
	if err != nil {
		t.Fatalf("run research: %v", err)
	}
	var result struct {
		Query      string           `json:"query"`
		Subqueries []string         `json:"subqueries"`
		Sources    []ResearchSource `json:"sources"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal research result: %v", err)
	}
	if result.Query != "agent research" || len(result.Subqueries) != 2 {
		t.Fatalf("unexpected research metadata: %+v", result)
	}
	if len(result.Sources) < 2 {
		t.Fatalf("expected web and academic sources, got %+v", result.Sources)
	}
	if !result.Sources[0].Fetched || !strings.Contains(result.Sources[0].Text, "Fetched source evidence") {
		t.Fatalf("expected fetched source text, got %+v", result.Sources[0])
	}
}

func TestSearchToolRequiresQuery(t *testing.T) {
	_, err := SearchTool{}.Run(context.Background(), json.RawMessage(`{"query":"   "}`))
	if err == nil {
		t.Fatalf("expected missing query error")
	}
}

func TestSearchToolReportsHTTPError(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Status:     "503 Service Unavailable",
			Body:       io.NopCloser(strings.NewReader("unavailable")),
		}, nil
	})}

	_, err := SearchTool{base: Base{Endpoint: "https://search.example/", SearchProvider: "duckduckgo", Timeout: time.Second, Client: client}}.Run(context.Background(), json.RawMessage(`{"query":"status"}`))
	if err == nil {
		t.Fatalf("expected http error")
	}
}

func TestFetchToolExtractsHTMLText(t *testing.T) {
	var gotUserAgent string
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotUserAgent = r.Header.Get("User-Agent")
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
			Request:    r,
			Body: io.NopCloser(strings.NewReader(`<!doctype html>
				<html><head><title>Example &amp; Test</title><style>.x{display:none}</style></head>
				<body><nav>skip navigation</nav><h1>Main heading</h1><p>Useful page text &amp; details.</p><script>alert("x")</script></body></html>`)),
		}, nil
	})}

	raw, err := FetchTool{base: Base{Client: client}}.Run(context.Background(), json.RawMessage(`{"url":"https://example.com/page","max_chars":1000}`))
	if err != nil {
		t.Fatalf("fetch html: %v", err)
	}
	if gotUserAgent == "" {
		t.Fatalf("expected user agent")
	}
	var result struct {
		FinalURL    string `json:"final_url"`
		Title       string `json:"title"`
		Text        string `json:"text"`
		ContentType string `json:"content_type"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal fetch result: %v", err)
	}
	if result.FinalURL != "https://example.com/page" || result.Title != "Example & Test" {
		t.Fatalf("unexpected metadata: %+v", result)
	}
	if !strings.Contains(result.Text, "Main heading Useful page text & details.") {
		t.Fatalf("expected extracted body text, got %q", result.Text)
	}
	if strings.Contains(result.Text, "skip navigation") || strings.Contains(result.Text, "alert") {
		t.Fatalf("expected noisy elements removed, got %q", result.Text)
	}
}

func TestFetchToolRejectsNonHTTPURL(t *testing.T) {
	_, err := FetchTool{}.Run(context.Background(), json.RawMessage(`{"url":"file:///etc/passwd"}`))
	if err == nil {
		t.Fatalf("expected non-http url error")
	}
}

func TestFetchToolRejectsPrivateHosts(t *testing.T) {
	for _, raw := range []string{
		`{"url":"http://localhost:8080"}`,
		`{"url":"http://127.0.0.1:8080"}`,
		`{"url":"http://10.0.0.5"}`,
		`{"url":"http://[::1]/"}`,
	} {
		if _, err := (FetchTool{}).Run(context.Background(), json.RawMessage(raw)); err == nil {
			t.Fatalf("expected private host error for %s", raw)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
