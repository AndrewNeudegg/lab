package control

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/andrewneudegg/lab/pkg/agent"
	"github.com/andrewneudegg/lab/pkg/chat"
	"github.com/andrewneudegg/lab/pkg/eventlog"
	"github.com/andrewneudegg/lab/pkg/id"
)

type Server struct {
	Addr         string
	Orchestrator *agent.Orchestrator
}

func (s Server) Listen(ctx context.Context) error {
	mux := http.NewServeMux()
	s.register(mux)
	server := &http.Server{Addr: s.Addr, Handler: mux}
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

func (s Server) register(mux *http.ServeMux) {
	mux.HandleFunc("/message", s.handleMessage)
	mux.HandleFunc("/tasks", s.handleTasks)
	mux.HandleFunc("/tasks/", s.handleTask)
	mux.HandleFunc("/approvals", s.handleApprovals)
	mux.HandleFunc("/approvals/", s.handleApproval)
	mux.HandleFunc("/events", s.handleEvents)
}

func (s Server) handleMessage(rw http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		writeError(rw, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var in struct {
		From    string `json:"from"`
		Content string `json:"content"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
		writeError(rw, http.StatusBadRequest, err.Error())
		return
	}
	content := in.Content
	if content == "" {
		content = in.Message
	}
	from := in.From
	if from == "" {
		from = "webhook"
	}
	reply, err := s.Orchestrator.Handle(req.Context(), from, content)
	if err != nil {
		writeError(rw, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(rw, http.StatusOK, map[string]any{"id": id.New("msg"), "reply": reply})
}

func (s Server) handleTasks(rw http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		tasks, err := s.Orchestrator.ListTasks()
		if err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, map[string]any{"tasks": tasks})
	case http.MethodPost:
		var in struct {
			Goal string `json:"goal"`
		}
		if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		reply, err := s.Orchestrator.CreateTask(req.Context(), in.Goal)
		if err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(rw, http.StatusCreated, map[string]any{"reply": reply})
	default:
		writeError(rw, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s Server) handleTask(rw http.ResponseWriter, req *http.Request) {
	rest := strings.TrimPrefix(req.URL.Path, "/tasks/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(rw, http.StatusNotFound, "task id required")
		return
	}
	taskID := parts[0]
	if len(parts) == 1 && req.Method == http.MethodGet {
		task, err := s.Orchestrator.LoadTask(taskID)
		if err != nil {
			writeError(rw, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, task)
		return
	}
	if len(parts) == 2 && req.Method == http.MethodPost {
		var (
			reply string
			err   error
		)
		switch parts[1] {
		case "run":
			reply, err = s.Orchestrator.RunTask(req.Context(), taskID)
		case "review":
			reply, err = s.Orchestrator.ReviewTask(req.Context(), taskID)
		default:
			writeError(rw, http.StatusNotFound, "unknown task action")
			return
		}
		if err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, map[string]any{"reply": reply})
		return
	}
	writeError(rw, http.StatusMethodNotAllowed, "method not allowed")
}

func (s Server) handleApprovals(rw http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		writeError(rw, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	requests, err := s.Orchestrator.ListApprovals()
	if err != nil {
		writeError(rw, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(rw, http.StatusOK, map[string]any{"approvals": requests})
}

func (s Server) handleApproval(rw http.ResponseWriter, req *http.Request) {
	rest := strings.TrimPrefix(req.URL.Path, "/approvals/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) != 2 || req.Method != http.MethodPost {
		writeError(rw, http.StatusNotFound, "approval action not found")
		return
	}
	var grant bool
	switch parts[1] {
	case "approve":
		grant = true
	case "deny":
		grant = false
	default:
		writeError(rw, http.StatusNotFound, "unknown approval action")
		return
	}
	reply, err := s.Orchestrator.ResolveApproval(req.Context(), parts[0], grant)
	if err != nil {
		writeError(rw, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(rw, http.StatusOK, map[string]any{"reply": reply})
}

func (s Server) handleEvents(rw http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		writeError(rw, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	day := time.Now().UTC()
	if value := req.URL.Query().Get("date"); value != "" {
		parsed, err := time.Parse("2006-01-02", value)
		if err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		day = parsed
	}
	events, err := s.Orchestrator.ReadEvents(day)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeJSON(rw, http.StatusOK, map[string]any{"events": []json.RawMessage{}})
			return
		}
		writeError(rw, http.StatusNotFound, err.Error())
		return
	}
	if events == nil {
		events = []eventlog.Event{}
	}
	writeJSON(rw, http.StatusOK, map[string]any{"events": events})
}

func writeJSON(rw http.ResponseWriter, status int, v any) {
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(status)
	_ = json.NewEncoder(rw).Encode(v)
}

func writeError(rw http.ResponseWriter, status int, message string) {
	writeJSON(rw, status, map[string]any{"error": message})
}

func ChatHandler(orch *agent.Orchestrator) func(context.Context, chat.ChatMessage) (string, error) {
	return func(ctx context.Context, msg chat.ChatMessage) (string, error) {
		return orch.Handle(ctx, msg.From, msg.Content)
	}
}
