package healthd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/andrewneudegg/lab/pkg/tool"
)

type Base struct {
	Addr   string
	Client *http.Client
}

func Register(reg *tool.Registry, base Base) error {
	return reg.Register(ErrorsTool{base: base})
}

type ErrorsTool struct {
	base Base
}

func (ErrorsTool) Name() string { return "health.errors" }
func (ErrorsTool) Description() string {
	return "Read recent application errors captured by healthd for diagnosis and follow-up task creation."
}
func (ErrorsTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"limit":{"type":"integer","minimum":1,"maximum":500},"app":{"type":"string"},"source":{"type":"string"}}}`)
}
func (ErrorsTool) Risk() tool.RiskLevel { return tool.RiskReadOnly }
func (t ErrorsTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Limit  int    `json:"limit"`
		App    string `json:"app"`
		Source string `json:"source"`
	}
	if len(input) > 0 {
		if err := json.Unmarshal(input, &req); err != nil {
			return nil, err
		}
	}
	query := url.Values{}
	if req.Limit > 0 {
		query.Set("limit", strconv.Itoa(req.Limit))
	}
	if strings.TrimSpace(req.App) != "" {
		query.Set("app", strings.TrimSpace(req.App))
	}
	if strings.TrimSpace(req.Source) != "" {
		query.Set("source", strings.TrimSpace(req.Source))
	}
	endpoint := strings.TrimRight(normalizeHTTPAddr(t.base.Addr), "/") + "/healthd/errors"
	if encoded := query.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}
	client := t.base.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("healthd errors returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func normalizeHTTPAddr(addr string) string {
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return addr
	}
	return "http://" + addr
}
