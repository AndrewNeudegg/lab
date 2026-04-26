package supervisor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Server struct {
	Addr    string
	Manager *Manager
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
	mux.HandleFunc("/supervisord", s.withCORS(s.handleSnapshot))
	mux.HandleFunc("/supervisord/restart", s.withCORS(s.handleRestart))
	mux.HandleFunc("/supervisord/stop", s.withCORS(s.handleStop))
	mux.HandleFunc("/supervisord/apps", s.withCORS(s.handleApps))
	mux.HandleFunc("/supervisord/apps/", s.withCORS(s.handleAppAction))
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
	if s.Manager == nil {
		writeError(rw, http.StatusServiceUnavailable, "supervisor is not configured")
		return
	}
	writeJSON(rw, http.StatusOK, s.Manager.Snapshot())
}

func (s *Server) handleApps(rw http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		writeError(rw, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Manager == nil {
		writeError(rw, http.StatusServiceUnavailable, "supervisor is not configured")
		return
	}
	writeJSON(rw, http.StatusOK, map[string]any{"apps": s.Manager.Snapshot().Apps})
}

func (s *Server) handleRestart(rw http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		writeError(rw, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Manager == nil {
		writeError(rw, http.StatusServiceUnavailable, "supervisor is not configured")
		return
	}
	if err := s.Manager.RestartSelf(req.Context()); err != nil {
		writeError(rw, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(rw, http.StatusAccepted, map[string]any{"reply": "supervisord restart scheduled"})
}

func (s *Server) handleStop(rw http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		writeError(rw, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Manager == nil {
		writeError(rw, http.StatusServiceUnavailable, "supervisor is not configured")
		return
	}
	if err := s.Manager.StopSelf(); err != nil {
		writeError(rw, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(rw, http.StatusAccepted, map[string]any{"reply": "supervisord stop scheduled"})
}

func (s *Server) handleAppAction(rw http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		writeError(rw, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Manager == nil {
		writeError(rw, http.StatusServiceUnavailable, "supervisor is not configured")
		return
	}
	rest := strings.TrimPrefix(req.URL.Path, "/supervisord/apps/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) != 2 || parts[0] == "" {
		writeError(rw, http.StatusNotFound, "app action not found")
		return
	}
	name, err := url.PathUnescape(parts[0])
	if err != nil {
		writeError(rw, http.StatusBadRequest, err.Error())
		return
	}
	switch parts[1] {
	case "start":
		err = s.Manager.StartApp(req.Context(), name)
	case "stop":
		err = s.Manager.StopApp(req.Context(), name)
	case "restart":
		err = s.Manager.RestartApp(req.Context(), name)
	case "adopt":
		var in struct {
			PID int `json:"pid"`
		}
		if decodeErr := json.NewDecoder(req.Body).Decode(&in); decodeErr != nil {
			writeError(rw, http.StatusBadRequest, decodeErr.Error())
			return
		}
		err = s.Manager.AdoptApp(name, in.PID)
	default:
		writeError(rw, http.StatusNotFound, "unknown app action")
		return
	}
	if err != nil {
		writeError(rw, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(rw, http.StatusOK, s.Manager.Snapshot())
}

func writeJSON(rw http.ResponseWriter, status int, v any) {
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(status)
	_ = json.NewEncoder(rw).Encode(v)
}

func writeError(rw http.ResponseWriter, status int, message string) {
	writeJSON(rw, status, map[string]any{"error": message})
}
