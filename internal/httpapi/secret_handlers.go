package httpapi

import (
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"

	"github.com/hkjang/jikim/internal/cryptox"
	"github.com/hkjang/jikim/internal/model"
)

func (s *Server) listSecrets(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePage(r)
	items, err := s.store.ListSecretsFiltered(r.Context(), r.URL.Query().Get("q"), r.URL.Query().Get("environment"), limit, offset)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	session, _ := sessionFrom(r)
	filtered := items[:0]
	for _, item := range items {
		allowed, accessErr := s.store.CanAccess(r.Context(), session.User, item.Path, "list")
		if accessErr != nil {
			s.storeError(w, r, accessErr)
			return
		}
		if allowed {
			filtered = append(filtered, item)
		}
	}
	writeData(w, http.StatusOK, filtered)
}

func (s *Server) createSecret(w http.ResponseWriter, r *http.Request) {
	var input model.SecretWrite
	if !decodeJSON(w, r, &input) {
		return
	}
	session, _ := sessionFrom(r)
	allowed, err := s.store.CanAccess(r.Context(), session.User, input.Path, "create")
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	if !allowed {
		writeError(w, r, http.StatusForbidden, "forbidden", "Secret 생성 권한이 없습니다")
		return
	}
	s.putOrRequestSecret(w, r, input, session.User.ID, http.StatusCreated)
}

func (s *Server) getSecret(w http.ResponseWriter, r *http.Request) {
	path, err := s.store.SecretPathByID(r.Context(), r.PathValue("id"))
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	session, _ := sessionFrom(r)
	allowed, err := s.store.CanAccess(r.Context(), session.User, path, "read")
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	if !allowed {
		writeError(w, r, http.StatusForbidden, "forbidden", "Secret 조회 권한이 없습니다")
		return
	}
	version, _ := strconv.Atoi(r.URL.Query().Get("version"))
	secret, err := s.store.GetSecret(r.Context(), path, version)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	secret.Data = nil
	writeData(w, http.StatusOK, secret)
}

func (s *Server) revealSecret(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Reason  string `json:"reason"`
		Version int    `json:"version,omitempty"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if len(input.Reason) < 3 || len(input.Reason) > 500 {
		writeError(w, r, http.StatusBadRequest, "reason_required", "Secret 조회 사유를 3~500자로 입력하세요")
		return
	}
	path, err := s.store.SecretPathByID(r.Context(), r.PathValue("id"))
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	session, _ := sessionFrom(r)
	allowed, err := s.store.CanAccess(r.Context(), session.User, path, "read")
	if err != nil || !allowed {
		if err != nil {
			s.storeError(w, r, err)
		} else {
			writeError(w, r, http.StatusForbidden, "forbidden", "Secret 값 조회 권한이 없습니다")
		}
		return
	}
	secret, err := s.store.GetSecret(r.Context(), path, input.Version)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if err := s.store.RecordAudit(r.Context(), model.AuditEvent{RequestID: requestIDFrom(r) + "-reveal",
		UserID: &session.User.ID, Username: session.User.Username, Action: "secret.reveal", Resource: path,
		Method: r.Method, Path: r.URL.Path, StatusCode: http.StatusOK, Success: true,
		RemoteIP: remoteIP(r), UserAgent: r.UserAgent(), Details: map[string]any{"reason": input.Reason, "version": secret.Version}}); err != nil {
		writeError(w, r, http.StatusServiceUnavailable, "audit_unavailable", "감사 로그를 저장할 수 없어 Secret 값을 표시하지 않습니다")
		return
	}
	writeData(w, http.StatusOK, map[string]any{"id": secret.ID, "path": secret.Path, "version": secret.Version, "data": secret.Data})
}

func (s *Server) rotateSecret(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Reason string         `json:"reason"`
		Data   map[string]any `json:"data,omitempty"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if strings.TrimSpace(input.Reason) == "" {
		writeError(w, r, http.StatusBadRequest, "reason_required", "회전 사유를 입력하세요")
		return
	}
	path, err := s.store.SecretPathByID(r.Context(), r.PathValue("id"))
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	session, _ := sessionFrom(r)
	allowed, err := s.store.CanAccess(r.Context(), session.User, path, "rotate")
	if err != nil || !allowed {
		if err != nil {
			s.storeError(w, r, err)
		} else {
			writeError(w, r, http.StatusForbidden, "forbidden", "Secret 회전 권한이 없습니다")
		}
		return
	}
	current, err := s.store.GetSecret(r.Context(), path, 0)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	data := input.Data
	if len(data) == 0 {
		data = cloneMap(current.Data)
		rotated := false
		for key, value := range data {
			lower := strings.ToLower(key)
			if _, ok := value.(string); ok && (strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "api_key") || strings.Contains(lower, "private_key")) {
				random, randomErr := cryptox.RandomKey()
				if randomErr != nil {
					s.storeError(w, r, randomErr)
					return
				}
				data[key] = base64.RawURLEncoding.EncodeToString(random)
				rotated = true
			}
		}
		if !rotated {
			writeError(w, r, http.StatusBadRequest, "rotation_value_required", "자동 회전 가능한 필드가 없어 새 data 값이 필요합니다")
			return
		}
	}
	metadata := cloneMap(current.Metadata)
	metadata["rotation_reason"] = strings.TrimSpace(input.Reason)
	write := model.SecretWrite{Path: path, Description: current.Description, ApplicationID: current.ApplicationID,
		OwnerUserID: current.OwnerUserID, Tags: current.Tags, Data: data, Metadata: metadata}
	s.putOrRequestSecret(w, r, write, session.User.ID, http.StatusOK)
}

func cloneMap(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func (s *Server) updateSecret(w http.ResponseWriter, r *http.Request) {
	path, err := s.store.SecretPathByID(r.Context(), r.PathValue("id"))
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	var input model.SecretWrite
	if !decodeJSON(w, r, &input) {
		return
	}
	input.Path = path
	session, _ := sessionFrom(r)
	allowed, err := s.store.CanAccess(r.Context(), session.User, path, "update")
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	if !allowed {
		writeError(w, r, http.StatusForbidden, "forbidden", "Secret 변경 권한이 없습니다")
		return
	}
	s.putOrRequestSecret(w, r, input, session.User.ID, http.StatusOK)
}

func (s *Server) putOrRequestSecret(w http.ResponseWriter, r *http.Request, input model.SecretWrite, actorID string, directStatus int) {
	enabled, err := s.store.ApprovalRequired(r.Context(), "secret_write")
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	if enabled {
		approval, err := s.store.CreateApproval(r.Context(), "secret.put", strings.Trim(input.Path, "/"), actorID, input)
		if err != nil {
			s.storeError(w, r, err)
			return
		}
		writeData(w, http.StatusAccepted, approval)
		return
	}
	secret, err := s.store.PutSecret(r.Context(), input, actorID)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeData(w, directStatus, secret)
}

func (s *Server) deleteSecret(w http.ResponseWriter, r *http.Request) {
	path, err := s.store.SecretPathByID(r.Context(), r.PathValue("id"))
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	session, _ := sessionFrom(r)
	allowed, err := s.store.CanAccess(r.Context(), session.User, path, "delete")
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	if !allowed {
		writeError(w, r, http.StatusForbidden, "forbidden", "Secret 삭제 권한이 없습니다")
		return
	}
	enabled, err := s.store.ApprovalRequired(r.Context(), "secret_delete")
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	if enabled {
		approval, err := s.store.CreateApproval(r.Context(), "secret.delete", path, session.User.ID, map[string]any{})
		if err != nil {
			s.storeError(w, r, err)
			return
		}
		writeData(w, http.StatusAccepted, approval)
		return
	}
	if err := s.store.DeleteSecret(r.Context(), path); err != nil {
		s.storeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) secretVersions(w http.ResponseWriter, r *http.Request) {
	path, err := s.store.SecretPathByID(r.Context(), r.PathValue("id"))
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	session, _ := sessionFrom(r)
	allowed, err := s.store.CanAccess(r.Context(), session.User, path, "read")
	if err != nil || !allowed {
		if err != nil {
			s.storeError(w, r, err)
		} else {
			writeError(w, r, http.StatusForbidden, "forbidden", "Secret 버전 조회 권한이 없습니다")
		}
		return
	}
	versions, err := s.store.SecretVersions(r.Context(), path)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, versions)
}
