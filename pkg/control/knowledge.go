package control

import (
	"encoding/json"
	"net/http"
	"strings"

	knowledgestore "github.com/andrewneudegg/lab/pkg/knowledge"
)

func (s *Server) handleKnowledgeSpaces(rw http.ResponseWriter, req *http.Request) {
	if s.Orchestrator == nil {
		writeError(rw, http.StatusServiceUnavailable, "orchestrator is not configured")
		return
	}
	switch req.Method {
	case http.MethodGet:
		spaces, err := s.Orchestrator.ListKnowledgeSpaces()
		if err != nil {
			writeError(rw, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, map[string]any{"spaces": spaces})
	case http.MethodPost:
		var in knowledgestore.CreateSpaceRequest
		if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		space, reply, err := s.Orchestrator.CreateKnowledgeSpace(req.Context(), in)
		if err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(rw, http.StatusCreated, map[string]any{"space": space, "reply": reply})
	default:
		writeError(rw, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleKnowledgeSpace(rw http.ResponseWriter, req *http.Request) {
	if s.Orchestrator == nil {
		writeError(rw, http.StatusServiceUnavailable, "orchestrator is not configured")
		return
	}
	rest := strings.TrimPrefix(req.URL.Path, "/knowledge/spaces/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(rw, http.StatusNotFound, "knowledge space id required")
		return
	}
	spaceID := parts[0]
	if len(parts) == 1 && req.Method == http.MethodGet {
		space, err := s.Orchestrator.LoadKnowledgeSpace(spaceID)
		if err != nil {
			writeError(rw, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, space)
		return
	}
	if len(parts) == 2 && parts[1] == "sources" && req.Method == http.MethodPost {
		var in knowledgestore.AddSourceRequest
		if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		space, source, reply, err := s.Orchestrator.AddKnowledgeSource(req.Context(), spaceID, in)
		if err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(rw, http.StatusCreated, map[string]any{"space": space, "source": source, "reply": reply})
		return
	}
	if len(parts) == 2 && parts[1] == "research" && req.Method == http.MethodPost {
		var in knowledgestore.ResearchRequest
		if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		space, report, reply, err := s.Orchestrator.ResearchKnowledgeSpace(req.Context(), spaceID, in)
		if err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, map[string]any{"space": space, "report": report, "reply": reply})
		return
	}
	if len(parts) == 2 && parts[1] == "query" && req.Method == http.MethodPost {
		var in knowledgestore.QueryRequest
		if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		result, reply, err := s.Orchestrator.QueryKnowledgeSpace(req.Context(), spaceID, in)
		if err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, map[string]any{"result": result, "reply": reply})
		return
	}
	if len(parts) == 2 && parts[1] == "ask" && req.Method == http.MethodPost {
		var in knowledgestore.AskRequest
		if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		result, reply, err := s.Orchestrator.AskKnowledgeSpace(req.Context(), spaceID, in)
		if err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(rw, http.StatusOK, map[string]any{"result": result, "reply": reply})
		return
	}
	if len(parts) == 2 && parts[1] == "research-runs" && req.Method == http.MethodPost {
		var in knowledgestore.CreateResearchRunRequest
		if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		space, run, report, reply, err := s.Orchestrator.StartKnowledgeResearchRun(req.Context(), spaceID, in)
		if err != nil {
			writeError(rw, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(rw, http.StatusCreated, map[string]any{"space": space, "run": run, "report": report, "reply": reply})
		return
	}
	writeError(rw, http.StatusMethodNotAllowed, "method not allowed")
}
