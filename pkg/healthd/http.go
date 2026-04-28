package healthd

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

type Server struct {
	Addr    string
	Monitor *Monitor
}

func (s *Server) Listen(ctx context.Context) error {
	mux := http.NewServeMux()
	s.Register(mux)
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

func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("/healthd", s.withCORS(s.handleSnapshot))
	mux.HandleFunc("/healthd/samples", s.withCORS(s.handleSamples))
	mux.HandleFunc("/healthd/notifications", s.withCORS(s.handleNotifications))
	mux.HandleFunc("/healthd/errors", s.withCORS(s.handleErrors))
	mux.HandleFunc("/healthd/processes", s.withCORS(s.handleProcesses))
	mux.HandleFunc("/healthd/processes/heartbeat", s.withCORS(s.handleProcessHeartbeat))
	mux.HandleFunc("/healthd/checks/run", s.withCORS(s.handleRunChecks))
}

func (s *Server) withCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Set("Access-Control-Allow-Origin", "*")
		rw.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		rw.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if req.Method == http.MethodOptions {
			rw.WriteHeader(http.StatusNoContent)
			return
		}
		next(rw, req)
	}
}

func (s *Server) handleSnapshot(rw http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		writeError(rw, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Monitor == nil {
		writeError(rw, http.StatusServiceUnavailable, "healthd monitor is not configured")
		return
	}
	writeJSON(rw, http.StatusOK, s.Monitor.Snapshot(parseWindow(req, 5*time.Minute)))
}

func (s *Server) handleSamples(rw http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		writeError(rw, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Monitor == nil {
		writeError(rw, http.StatusServiceUnavailable, "healthd monitor is not configured")
		return
	}
	snapshot := s.Monitor.Snapshot(parseWindow(req, 5*time.Minute))
	writeJSON(rw, http.StatusOK, map[string]any{
		"window_seconds": snapshot.WindowSeconds,
		"samples":        snapshot.Samples,
	})
}

func (s *Server) handleNotifications(rw http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		writeError(rw, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Monitor == nil {
		writeError(rw, http.StatusServiceUnavailable, "healthd monitor is not configured")
		return
	}
	snapshot := s.Monitor.Snapshot(parseWindow(req, 5*time.Minute))
	writeJSON(rw, http.StatusOK, map[string]any{"notifications": snapshot.Notifications})
}

func (s *Server) handleErrors(rw http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		if s.Monitor == nil {
			writeError(rw, http.StatusServiceUnavailable, "healthd monitor is not configured")
			return
		}
		filter := ErrorFilter{
			Limit:  parseLimit(req, 50),
			Source: req.URL.Query().Get("source"),
			App:    req.URL.Query().Get("app"),
		}
		writeJSON(rw, http.StatusOK, map[string]any{"errors": s.Monitor.Errors(filter)})
	case http.MethodPost:
		if s.Monitor == nil {
			writeError(rw, http.StatusServiceUnavailable, "healthd monitor is not configured")
			return
		}
		var in struct {
			Errors []ApplicationError `json:"errors"`
		}
		if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		recorded := s.Monitor.RecordErrors(time.Now().UTC(), in.Errors)
		writeJSON(rw, http.StatusAccepted, map[string]any{"errors": recorded})
	default:
		writeError(rw, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleProcesses(rw http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		writeError(rw, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Monitor == nil {
		writeError(rw, http.StatusServiceUnavailable, "healthd monitor is not configured")
		return
	}
	snapshot := s.Monitor.Snapshot(parseWindow(req, 5*time.Minute))
	writeJSON(rw, http.StatusOK, map[string]any{"processes": snapshot.Processes})
}

func (s *Server) handleProcessHeartbeat(rw http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		writeError(rw, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Monitor == nil {
		writeError(rw, http.StatusServiceUnavailable, "healthd monitor is not configured")
		return
	}
	var heartbeat ProcessHeartbeat
	if err := json.NewDecoder(req.Body).Decode(&heartbeat); err != nil {
		writeError(rw, http.StatusBadRequest, err.Error())
		return
	}
	status, err := s.Monitor.RecordHeartbeat(time.Now().UTC(), heartbeat)
	if err != nil {
		writeError(rw, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(rw, http.StatusAccepted, status)
}

func (s *Server) handleRunChecks(rw http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		writeError(rw, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Monitor == nil {
		writeError(rw, http.StatusServiceUnavailable, "healthd monitor is not configured")
		return
	}
	writeJSON(rw, http.StatusOK, s.Monitor.RunChecks(req.Context()))
}

func parseWindow(req *http.Request, fallback time.Duration) time.Duration {
	value := req.URL.Query().Get("window")
	if value == "" {
		return fallback
	}
	window, err := time.ParseDuration(value)
	if err != nil || window <= 0 {
		return fallback
	}
	return window
}

func parseLimit(req *http.Request, fallback int) int {
	value := req.URL.Query().Get("limit")
	if value == "" {
		return fallback
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit <= 0 {
		return fallback
	}
	if limit > 500 {
		return 500
	}
	return limit
}

func writeJSON(rw http.ResponseWriter, status int, v any) {
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(status)
	_ = json.NewEncoder(rw).Encode(v)
}

func writeError(rw http.ResponseWriter, status int, message string) {
	writeJSON(rw, status, map[string]any{"error": message})
}
