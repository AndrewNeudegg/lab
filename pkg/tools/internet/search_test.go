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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
