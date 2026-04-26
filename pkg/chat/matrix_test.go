package chat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestMatrixSyncUsesTimelineLimitFilter(t *testing.T) {
	var gotFilter string
	var gotSince string
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/_matrix/client/v3/sync" {
			t.Fatalf("unexpected path %q", req.URL.Path)
		}
		gotFilter = req.URL.Query().Get("filter")
		gotSince = req.URL.Query().Get("since")
		_ = json.NewEncoder(rw).Encode(map[string]any{
			"next_batch": "next",
			"rooms":      map[string]any{},
		})
	}))
	defer server.Close()

	matrix := NewMatrix(MatrixConfig{Homeserver: server.URL, AccessToken: "token"})
	if _, err := matrix.sync(context.Background(), "batch-one", 30000); err != nil {
		t.Fatal(err)
	}
	if gotSince != "batch-one" {
		t.Fatalf("since = %q, want batch-one", gotSince)
	}
	decoded, err := url.QueryUnescape(gotFilter)
	if err != nil {
		t.Fatal(err)
	}
	if decoded != matrixSyncFilter {
		t.Fatalf("filter = %q, want %q", decoded, matrixSyncFilter)
	}
}
