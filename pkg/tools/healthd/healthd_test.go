package healthd

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/andrewneudegg/lab/pkg/tool"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestErrorsToolReadsHealthdErrors(t *testing.T) {
	var gotPath string
	var gotQuery string
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		gotPath = req.URL.Path
		gotQuery = req.URL.RawQuery
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader(`{"errors":[{"app":"dashboard","message":"boom"}]}`)),
		}, nil
	})}
	reg := tool.NewRegistry()
	if err := Register(reg, Base{Addr: "http://healthd.test", Client: client}); err != nil {
		t.Fatal(err)
	}
	raw, err := reg.Run(context.Background(), "health.errors", []byte(`{"limit":2,"app":"dashboard","source":"supervisord"}`))
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/healthd/errors" || gotQuery != "app=dashboard&limit=2&source=supervisord" {
		t.Fatalf("request = %s?%s, want /healthd/errors?app=dashboard&limit=2&source=supervisord", gotPath, gotQuery)
	}
	if !strings.Contains(string(raw), `"message":"boom"`) {
		t.Fatalf("raw = %s, want healthd payload", string(raw))
	}
}
