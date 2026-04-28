package control

import (
	"encoding/json"
	"net/http"
	"strings"

	workflowstore "github.com/andrewneudegg/lab/pkg/workflow"
)

func (s *Server) handleWorkflows(rw http.ResponseWriter, req *http.Request) {
	if s.Orchestrator == nil {
		writeError(rw, http.StatusServiceUnavailable, "orchestrator is not configured")
		return
	}
	switch req.Method {
	case http.MethodGet:
		workflows, err := s.Orchestrator.ListWorkflows()
		if err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, map[string]any{"workflows": workflows})
	case http.MethodPost:
		var in workflowstore.CreateRequest
		if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		workflow, reply, err := s.Orchestrator.CreateWorkflow(req.Context(), in)
		if err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(rw, http.StatusCreated, map[string]any{"workflow": workflow, "reply": reply})
	default:
		writeError(rw, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleWorkflow(rw http.ResponseWriter, req *http.Request) {
	if s.Orchestrator == nil {
		writeError(rw, http.StatusServiceUnavailable, "orchestrator is not configured")
		return
	}
	rest := strings.TrimPrefix(req.URL.Path, "/workflows/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(rw, http.StatusNotFound, "workflow id required")
		return
	}
	workflowID := parts[0]
	if len(parts) == 1 && req.Method == http.MethodGet {
		workflow, err := s.Orchestrator.LoadWorkflow(workflowID)
		if err != nil {
			writeError(rw, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, workflow)
		return
	}
	if len(parts) == 2 && parts[1] == "run" && req.Method == http.MethodPost {
		workflow, reply, err := s.Orchestrator.RunWorkflow(req.Context(), workflowID)
		if err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, map[string]any{"workflow": workflow, "reply": reply})
		return
	}
	writeError(rw, http.StatusMethodNotAllowed, "method not allowed")
}
