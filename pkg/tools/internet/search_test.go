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

	raw, err := SearchTool{base: Base{Endpoint: "https://search.example/", Client: client}}.Run(context.Background(), json.RawMessage(`{"query":"golang testing","max_results":2}`))
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

	_, err := SearchTool{base: Base{Endpoint: "https://search.example/", Timeout: time.Second, Client: client}}.Run(context.Background(), json.RawMessage(`{"query":"status"}`))
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
