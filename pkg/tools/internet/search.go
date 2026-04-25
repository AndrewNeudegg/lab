package internet

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/andrewneudegg/lab/pkg/tool"
)

const defaultEndpoint = "https://api.duckduckgo.com/"

type Base struct {
	Endpoint string
	Timeout  time.Duration
	Client   *http.Client
}

func Register(reg *tool.Registry, base Base) error {
	return reg.Register(SearchTool{base: base})
}

func schema(v string) json.RawMessage { return json.RawMessage(v) }

type SearchTool struct {
	base Base
}

func (SearchTool) Name() string { return "internet.search" }
func (SearchTool) Description() string {
	return "Search the public internet and return concise results with URLs."
}
func (SearchTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["query"],"properties":{"query":{"type":"string"},"max_results":{"type":"integer","minimum":1,"maximum":10}}}`)
}
func (SearchTool) Risk() tool.RiskLevel { return tool.RiskReadOnly }
func (t SearchTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Query      string `json:"query"`
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

	endpoint := t.base.Endpoint
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("q", req.Query)
	q.Set("format", "json")
	q.Set("no_html", "1")
	q.Set("skip_disambig", "1")
	u.RawQuery = q.Encode()

	client := t.base.Client
	if client == nil {
		timeout := t.base.Timeout
		if timeout <= 0 {
			timeout = 10 * time.Second
		}
		client = &http.Client{Timeout: timeout}
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("User-Agent", "homelabd/1.0")

	resp, err := client.Do(httpReq)
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
		"query":        req.Query,
		"source":       "duckduckgo",
		"answer":       strings.TrimSpace(api.Answer),
		"abstract":     strings.TrimSpace(api.AbstractText),
		"abstract_url": strings.TrimSpace(api.AbstractURL),
		"results":      results,
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
