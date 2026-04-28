package healthd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func PushHeartbeat(ctx context.Context, client *http.Client, addr string, heartbeat ProcessHeartbeat) error {
	if client == nil {
		client = http.DefaultClient
	}
	if heartbeat.Time.IsZero() {
		heartbeat.Time = time.Now().UTC()
	}
	b, err := json.Marshal(heartbeat)
	if err != nil {
		return err
	}
	endpoint := strings.TrimRight(normalizeHTTPAddr(addr), "/") + "/healthd/processes/heartbeat"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if len(body) > 0 {
		return fmt.Errorf("healthd heartbeat returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return fmt.Errorf("healthd heartbeat returned %s", resp.Status)
}

func PushErrors(ctx context.Context, client *http.Client, addr string, entries []ApplicationError) error {
	if len(entries) == 0 {
		return nil
	}
	if client == nil {
		client = http.DefaultClient
	}
	now := time.Now().UTC()
	for i := range entries {
		if entries[i].Time.IsZero() {
			entries[i].Time = now
		}
	}
	b, err := json.Marshal(map[string]any{"errors": entries})
	if err != nil {
		return err
	}
	endpoint := strings.TrimRight(normalizeHTTPAddr(addr), "/") + "/healthd/errors"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if len(body) > 0 {
		return fmt.Errorf("healthd errors returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return fmt.Errorf("healthd errors returned %s", resp.Status)
}

func normalizeHTTPAddr(addr string) string {
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return addr
	}
	return "http://" + addr
}
