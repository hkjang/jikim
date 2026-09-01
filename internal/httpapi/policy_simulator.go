package httpapi

import (
	"net/http"
	"strings"
)

func (s *Server) simulatePolicy(w http.ResponseWriter, r *http.Request) {
	var input struct {
		UserID     string `json:"user_id"`
		Path       string `json:"path"`
		Capability string `json:"capability"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	input.UserID = strings.TrimSpace(input.UserID)
	input.Path = strings.Trim(strings.TrimSpace(input.Path), "/")
	input.Capability = strings.ToLower(strings.TrimSpace(input.Capability))
	if input.UserID == "" || input.Path == "" || input.Capability == "" {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "user_id, path, capability이 필요합니다")
		return
	}
	user, err := s.store.GetUser(r.Context(), input.UserID)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	result, err := s.store.SimulateAccess(r.Context(), user, input.Path, input.Capability)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, result)
}
