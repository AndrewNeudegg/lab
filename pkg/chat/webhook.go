package chat

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/andrewneudegg/lab/pkg/id"
)

type Webhook struct {
	Addr   string
	Handle func(context.Context, ChatMessage) (string, error)
}

func (w Webhook) Name() string { return "webhook" }

func (w Webhook) Listen(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/message", func(rw http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost {
			http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var in struct {
			From    string `json:"from"`
			Content string `json:"content"`
			Message string `json:"message"`
		}
		if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
			http.Error(rw, err.Error(), http.StatusBadRequest)
			return
		}
		content := in.Content
		if content == "" {
			content = in.Message
		}
		msg := ChatMessage{ID: id.New("msg"), Time: time.Now().UTC(), From: in.From, Content: content}
		if msg.From == "" {
			msg.From = "webhook"
		}
		out, err := w.Handle(req.Context(), msg)
		if err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}
		rw.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(rw).Encode(map[string]any{"reply": out})
	})
	server := &http.Server{Addr: w.Addr, Handler: mux}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	err := server.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
