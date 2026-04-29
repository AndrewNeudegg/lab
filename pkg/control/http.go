package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/andrewneudegg/lab/pkg/agent"
	"github.com/andrewneudegg/lab/pkg/chat"
	"github.com/andrewneudegg/lab/pkg/eventlog"
	"github.com/andrewneudegg/lab/pkg/healthd"
	"github.com/andrewneudegg/lab/pkg/id"
	"github.com/andrewneudegg/lab/pkg/remoteagent"
	taskstore "github.com/andrewneudegg/lab/pkg/task"
)

type Server struct {
	Addr            string
	Orchestrator    *agent.Orchestrator
	ChatLogDir      string
	RemoteAgents    *remoteagent.Store
	AgentToken      string
	AgentStaleAfter time.Duration
	HealthdURL      string
	HealthdPush     func(context.Context, *http.Client, string, healthd.ProcessHeartbeat) error
	TerminalOnly    bool

	logMu      sync.Mutex
	terminalMu sync.Mutex
	terminal   *terminalManager
}

func (s *Server) Listen(ctx context.Context) error {
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

func (s *Server) register(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", s.withCORS(s.handleHealthz))
	if s.TerminalOnly {
		mux.HandleFunc("/terminal/sessions", s.withCORS(s.handleTerminalSessions))
		mux.HandleFunc("/terminal/sessions/", s.withCORS(s.handleTerminalSession))
		mux.HandleFunc("/api/terminal/sessions", s.withCORS(s.handleTerminalSessions))
		mux.HandleFunc("/api/terminal/sessions/", s.withCORS(s.handleTerminalSession))
		return
	}
	mux.HandleFunc("/message", s.withCORS(s.handleMessage))
	mux.HandleFunc("/tasks", s.withCORS(s.handleTasks))
	mux.HandleFunc("/tasks/", s.withCORS(s.handleTask))
	mux.HandleFunc("/workflows", s.withCORS(s.handleWorkflows))
	mux.HandleFunc("/workflows/", s.withCORS(s.handleWorkflow))
	mux.HandleFunc("/agents", s.withCORS(s.handleAgents))
	mux.HandleFunc("/agents/", s.withCORS(s.handleAgent))
	mux.HandleFunc("/approvals", s.withCORS(s.handleApprovals))
	mux.HandleFunc("/approvals/", s.withCORS(s.handleApproval))
	mux.HandleFunc("/events", s.withCORS(s.handleEvents))
	mux.HandleFunc("/terminal/sessions", s.withCORS(s.handleTerminalSessions))
	mux.HandleFunc("/terminal/sessions/", s.withCORS(s.handleTerminalSession))
	mux.HandleFunc("/api/terminal/sessions", s.withCORS(s.handleTerminalSessions))
	mux.HandleFunc("/api/terminal/sessions/", s.withCORS(s.handleTerminalSession))
}

func (s *Server) handleHealthz(rw http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		writeError(rw, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(rw, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) withCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Set("Access-Control-Allow-Origin", "*")
		rw.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		rw.Header().Set("Access-Control-Allow-Methods", "DELETE, GET, POST, OPTIONS")
		if req.Method == http.MethodOptions {
			rw.WriteHeader(http.StatusNoContent)
			return
		}
		next(rw, req)
	}
}

func (s *Server) terminals() *terminalManager {
	s.terminalMu.Lock()
	defer s.terminalMu.Unlock()
	if s.terminal == nil {
		s.terminal = newTerminalManager()
	}
	return s.terminal
}

func (s *Server) handleMessage(rw http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		writeError(rw, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var in struct {
		From        string                 `json:"from"`
		Content     string                 `json:"content"`
		Message     string                 `json:"message"`
		Attachments []taskstore.Attachment `json:"attachments,omitempty"`
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
	_ = s.appendChat("http", "in", from, "homelabd", content, true)
	result, err := s.Orchestrator.HandleDetailedWithAttachments(req.Context(), from, content, in.Attachments)
	if err != nil {
		_ = s.appendChat("http", "out", "homelabd", from, "error: "+err.Error(), true)
		writeError(rw, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.appendChat("http", "out", "homelabd", from, result.Reply, true)
	response := map[string]any{"id": id.New("msg"), "reply": result.Reply, "source": result.Source}
	if result.Stats.HasValues() {
		response["stats"] = result.Stats
	}
	writeJSON(rw, http.StatusOK, response)
}

func (s *Server) appendChat(adapter, direction, from, to, content string, addressed bool) error {
	if s.ChatLogDir == "" {
		return nil
	}
	s.logMu.Lock()
	defer s.logMu.Unlock()
	if err := os.MkdirAll(s.ChatLogDir, 0o755); err != nil {
		return err
	}
	record := map[string]any{
		"time":      time.Now().UTC(),
		"adapter":   adapter,
		"direction": direction,
		"from":      from,
		"to":        to,
		"content":   content,
		"addressed": addressed,
	}
	b, err := json.Marshal(record)
	if err != nil {
		return err
	}
	path := filepath.Join(s.ChatLogDir, time.Now().UTC().Format("2006-01-02")+".jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return err
}

func (s *Server) handleTasks(rw http.ResponseWriter, req *http.Request) {
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
			Goal        string                     `json:"goal"`
			Target      *taskstore.ExecutionTarget `json:"target,omitempty"`
			Attachments []taskstore.Attachment     `json:"attachments,omitempty"`
		}
		if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		reply, err := s.Orchestrator.CreateTaskWithTargetAndAttachments(req.Context(), in.Goal, in.Target, in.Attachments)
		if err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(rw, http.StatusCreated, map[string]any{"reply": reply})
	default:
		writeError(rw, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleTask(rw http.ResponseWriter, req *http.Request) {
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
	if len(parts) == 2 && parts[1] == "runs" && req.Method == http.MethodGet {
		runs, err := s.Orchestrator.ListTaskRuns(taskID)
		if err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, map[string]any{"runs": runs})
		return
	}
	if len(parts) == 2 && parts[1] == "diff" && req.Method == http.MethodGet {
		diff, err := s.Orchestrator.TaskDiff(req.Context(), taskID)
		if err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, diff)
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
		case "assign":
			var in struct {
				Target *taskstore.ExecutionTarget `json:"target"`
			}
			if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
				writeError(rw, http.StatusBadRequest, err.Error())
				return
			}
			reply, err = s.Orchestrator.AssignTaskTarget(req.Context(), taskID, in.Target)
		case "review":
			reply, err = s.Orchestrator.ReviewTask(req.Context(), taskID)
		case "merge-queue":
			var in struct {
				Direction string `json:"direction"`
			}
			if req.Body != nil {
				_ = json.NewDecoder(req.Body).Decode(&in)
			}
			reply, err = s.Orchestrator.MoveTaskInMergeQueue(req.Context(), taskID, in.Direction)
		case "accept":
			reply, err = s.Orchestrator.AcceptTask(req.Context(), taskID)
		case "restart":
			reply, err = s.Orchestrator.RestartTaskPostMerge(req.Context(), taskID)
		case "reopen":
			var in struct {
				Reason string `json:"reason"`
			}
			if req.Body != nil {
				_ = json.NewDecoder(req.Body).Decode(&in)
			}
			reply, err = s.Orchestrator.ReopenTask(req.Context(), taskID, in.Reason)
		case "cancel":
			reply, err = s.Orchestrator.CancelTask(req.Context(), taskID)
		case "retry":
			var in struct {
				Backend     string `json:"backend"`
				Instruction string `json:"instruction"`
			}
			if req.Body != nil {
				_ = json.NewDecoder(req.Body).Decode(&in)
			}
			reply, err = s.Orchestrator.RetryTask(req.Context(), taskID, in.Backend, in.Instruction)
		case "delete":
			reply, err = s.Orchestrator.DeleteTask(req.Context(), taskID)
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

func (s *Server) handleAgents(rw http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		if s.RemoteAgents == nil {
			writeJSON(rw, http.StatusOK, map[string]any{"agents": []remoteagent.Agent{}})
			return
		}
		agents, err := s.RemoteAgents.List(s.AgentStaleAfter, time.Now().UTC())
		if err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, map[string]any{"agents": agents})
	case http.MethodPost:
		if !s.authorizeAgent(rw, req) {
			return
		}
		var in remoteagent.Heartbeat
		if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		store := s.remoteAgentStore()
		if store == nil {
			writeError(rw, http.StatusServiceUnavailable, "remote agent store is not configured")
			return
		}
		agent, err := store.UpsertHeartbeat(in, time.Now().UTC())
		if err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		s.forwardAgentHeartbeat(req.Context(), agent)
		writeJSON(rw, http.StatusOK, map[string]any{"agent": agent})
	default:
		writeError(rw, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleAgent(rw http.ResponseWriter, req *http.Request) {
	rest := strings.TrimPrefix(req.URL.Path, "/agents/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(rw, http.StatusNotFound, "agent id required")
		return
	}
	agentID := parts[0]
	if len(parts) == 1 && req.Method == http.MethodGet {
		store := s.remoteAgentStore()
		if store == nil {
			writeError(rw, http.StatusNotFound, "agent not found")
			return
		}
		agent, err := store.Load(agentID)
		if err != nil {
			writeError(rw, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, agent)
		return
	}
	if !s.authorizeAgent(rw, req) {
		return
	}
	switch {
	case len(parts) == 2 && parts[1] == "heartbeat" && req.Method == http.MethodPost:
		var in remoteagent.Heartbeat
		if req.Body != nil {
			_ = json.NewDecoder(req.Body).Decode(&in)
		}
		in.ID = agentID
		store := s.remoteAgentStore()
		if store == nil {
			writeError(rw, http.StatusServiceUnavailable, "remote agent store is not configured")
			return
		}
		agent, err := store.UpsertHeartbeat(in, time.Now().UTC())
		if err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		s.forwardAgentHeartbeat(req.Context(), agent)
		writeJSON(rw, http.StatusOK, map[string]any{"agent": agent})
	case len(parts) == 2 && parts[1] == "claim" && req.Method == http.MethodPost:
		store := s.remoteAgentStore()
		if store == nil {
			writeError(rw, http.StatusServiceUnavailable, "remote agent store is not configured")
			return
		}
		agent, err := store.Load(agentID)
		if err != nil {
			writeError(rw, http.StatusNotFound, err.Error())
			return
		}
		var in struct {
			Backend string `json:"backend"`
		}
		if req.Body != nil {
			_ = json.NewDecoder(req.Body).Decode(&in)
		}
		assignment, err := s.Orchestrator.ClaimRemoteTask(req.Context(), agent, in.Backend)
		if err != nil {
			writeError(rw, http.StatusConflict, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, map[string]any{"assignment": assignment})
	case len(parts) == 4 && parts[1] == "tasks" && parts[3] == "complete" && req.Method == http.MethodPost:
		var in struct {
			Status string `json:"status"`
			Result string `json:"result"`
			Error  string `json:"error"`
		}
		if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		result := in.Result
		if strings.TrimSpace(result) == "" {
			result = in.Error
		}
		reply, err := s.Orchestrator.CompleteRemoteTask(req.Context(), agentID, parts[2], result, in.Status)
		if err != nil {
			writeError(rw, http.StatusConflict, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, map[string]any{"reply": reply})
	default:
		writeError(rw, http.StatusNotFound, "agent action not found")
	}
}

func (s *Server) forwardAgentHeartbeat(ctx context.Context, agent remoteagent.Agent) {
	if strings.TrimSpace(s.HealthdURL) == "" {
		return
	}
	ttl := int(s.AgentStaleAfter.Seconds())
	if ttl <= 0 {
		ttl = 30
	}
	metadata := map[string]string{
		"agent_id":            agent.ID,
		"machine":             agent.Machine,
		"service.name":        "homelab-agent",
		"service.instance.id": agent.ID,
		"workdirs":            strconv.Itoa(len(agent.Workdirs)),
	}
	if agent.CurrentTaskID != "" {
		metadata["current_task_id"] = agent.CurrentTaskID
	}
	if len(agent.Capabilities) > 0 {
		metadata["capabilities"] = strings.Join(agent.Capabilities, ",")
	}
	for key, value := range agent.Metadata {
		if strings.TrimSpace(key) == "" {
			continue
		}
		metadata[key] = value
	}
	client := &http.Client{Timeout: 2 * time.Second}
	heartbeat := healthd.ProcessHeartbeat{
		Name:       "remote-agent:" + agent.ID,
		Type:       "remote_agent",
		StartedAt:  agent.StartedAt,
		Time:       agent.LastSeen,
		TTLSeconds: ttl,
		Metadata:   metadata,
	}
	push := s.HealthdPush
	if push == nil {
		push = healthd.PushHeartbeat
	}
	if err := push(ctx, client, strings.TrimRight(s.HealthdURL, "/"), heartbeat); err != nil {
		// Health integration should never make task scheduling fail.
		return
	}
}

func (s *Server) remoteAgentStore() *remoteagent.Store {
	return s.RemoteAgents
}

func (s *Server) authorizeAgent(rw http.ResponseWriter, req *http.Request) bool {
	if s.AgentToken == "" {
		writeError(rw, http.StatusServiceUnavailable, "remote agent token is not configured")
		return false
	}
	auth := req.Header.Get("Authorization")
	token, ok := strings.CutPrefix(auth, "Bearer ")
	if !ok || token != s.AgentToken {
		writeError(rw, http.StatusUnauthorized, "invalid remote agent token")
		return false
	}
	return true
}

func (s *Server) handleApprovals(rw http.ResponseWriter, req *http.Request) {
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

func (s *Server) handleApproval(rw http.ResponseWriter, req *http.Request) {
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

func (s *Server) handleEvents(rw http.ResponseWriter, req *http.Request) {
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
	if value := req.URL.Query().Get("limit"); value != "" {
		limit, err := strconv.Atoi(value)
		if err != nil || limit < 1 {
			writeError(rw, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		if len(events) > limit {
			events = events[len(events)-limit:]
		}
	}
	writeJSON(rw, http.StatusOK, map[string]any{"events": events})
}

func (s *Server) handleTerminalSessions(rw http.ResponseWriter, req *http.Request) {
	if normalizeTerminalPath(req.URL.Path) != "/terminal/sessions" {
		writeError(rw, http.StatusNotFound, "terminal session not found")
		return
	}
	if req.Method != http.MethodPost {
		writeError(rw, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var in struct {
		CWD  string `json:"cwd"`
		Cols int    `json:"cols"`
		Rows int    `json:"rows"`
	}
	if req.Body != nil {
		_ = json.NewDecoder(req.Body).Decode(&in)
	}
	session, err := s.terminals().createWithSize(in.CWD, terminalSize{Cols: in.Cols, Rows: in.Rows})
	if err != nil {
		writeError(rw, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(rw, http.StatusCreated, terminalSessionResponse(session))
}

func (s *Server) handleTerminalSession(rw http.ResponseWriter, req *http.Request) {
	rest := strings.TrimPrefix(normalizeTerminalPath(req.URL.Path), "/terminal/sessions/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(rw, http.StatusNotFound, "terminal session id required")
		return
	}
	if len(parts) == 1 && req.Method == http.MethodDelete {
		if !s.terminals().close(parts[0]) {
			writeError(rw, http.StatusNotFound, "terminal session not found")
			return
		}
		writeJSON(rw, http.StatusOK, map[string]any{"closed": true})
		return
	}
	if len(parts) == 1 && req.Method == http.MethodGet {
		session, ok, err := s.terminals().getOrAttach(parts[0], terminalSize{Cols: defaultTerminalCols, Rows: defaultTerminalRows})
		if err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		if !ok {
			writeError(rw, http.StatusNotFound, "terminal session not found")
			return
		}
		writeJSON(rw, http.StatusOK, terminalSessionResponse(session))
		return
	}
	if len(parts) != 2 {
		writeError(rw, http.StatusNotFound, "terminal action not found")
		return
	}
	session, ok, err := s.terminals().getOrAttach(parts[0], terminalSize{Cols: defaultTerminalCols, Rows: defaultTerminalRows})
	if err != nil {
		writeError(rw, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeError(rw, http.StatusNotFound, "terminal session not found")
		return
	}
	switch parts[1] {
	case "ws":
		s.streamTerminalWebSocket(rw, req, session)
	case "events":
		if req.Method != http.MethodGet {
			writeError(rw, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.streamTerminalEvents(rw, req, session)
	case "input":
		if req.Method != http.MethodPost {
			writeError(rw, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var in struct {
			Data string `json:"data"`
		}
		if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		if err := session.write(in.Data); err != nil {
			writeError(rw, http.StatusConflict, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, map[string]any{"ok": true})
	case "signal":
		if req.Method != http.MethodPost {
			writeError(rw, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var in struct {
			Signal string `json:"signal"`
		}
		if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		if err := session.signal(in.Signal); err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, map[string]any{"ok": true})
	case "resize":
		if req.Method != http.MethodPost {
			writeError(rw, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var in struct {
			Cols int `json:"cols"`
			Rows int `json:"rows"`
		}
		if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		size := normalizeTerminalSize(terminalSize{Cols: in.Cols, Rows: in.Rows})
		if err := session.resize(size); err != nil {
			writeError(rw, http.StatusConflict, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, map[string]any{"ok": true, "cols": size.Cols, "rows": size.Rows})
	default:
		writeError(rw, http.StatusNotFound, "terminal action not found")
	}
}

func terminalSessionResponse(session *terminalSession) map[string]any {
	return map[string]any{
		"id":         session.id,
		"shell":      session.shell,
		"cwd":        session.cwd,
		"created_at": session.created,
		"persistent": session.tmux != "",
	}
}

func normalizeTerminalPath(path string) string {
	return strings.TrimPrefix(path, "/api")
}

func (s *Server) streamTerminalEvents(rw http.ResponseWriter, req *http.Request, session *terminalSession) {
	flusher, ok := rw.(http.Flusher)
	if !ok {
		writeError(rw, http.StatusInternalServerError, "streaming is not supported")
		return
	}
	rw.Header().Set("Content-Type", "text/event-stream")
	rw.Header().Set("Cache-Control", "no-cache")
	rw.Header().Set("Connection", "keep-alive")

	events := session.subscribeSince(terminalEventStreamAfter(req))
	defer session.unsubscribe(events)
	keepAlive := time.NewTicker(15 * time.Second)
	defer keepAlive.Stop()

	fmt.Fprintf(rw, "retry: 1000\nevent: ready\ndata: {}\n\n")
	flusher.Flush()
	for {
		select {
		case <-req.Context().Done():
			return
		case <-keepAlive.C:
			fmt.Fprintf(rw, ": keepalive\n\n")
			flusher.Flush()
		case event, ok := <-events:
			if !ok {
				return
			}
			b, err := json.Marshal(event)
			if err != nil {
				continue
			}
			if event.Seq > 0 {
				fmt.Fprintf(rw, "id: %d\n", event.Seq)
			}
			fmt.Fprintf(rw, "event: %s\ndata: %s\n\n", event.Type, b)
			flusher.Flush()
			if event.Type == "exit" {
				return
			}
		}
	}
}

func terminalEventStreamAfter(req *http.Request) int64 {
	for _, value := range []string{req.URL.Query().Get("after"), req.Header.Get("Last-Event-ID")} {
		if value == "" {
			continue
		}
		after, err := strconv.ParseInt(value, 10, 64)
		if err == nil && after > 0 {
			return after
		}
	}
	return 0
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
