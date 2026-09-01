package httpapi

import (
	"net/http"

	"github.com/hkjang/jikim/internal/model"
)

func (s *Server) recordSensitiveDisclosure(r *http.Request, user model.User, action, resource string, details map[string]any) error {
	event := model.AuditEvent{RequestID: requestIDFrom(r) + "-sensitive", UserID: &user.ID,
		Username: user.Username, Action: action, Resource: resource, Method: r.Method, Path: r.URL.Path,
		StatusCode: http.StatusOK, Success: true, RemoteIP: remoteIP(r), UserAgent: r.UserAgent(), Details: details}
	if s.auditRecorder != nil {
		return s.auditRecorder(r.Context(), event)
	}
	return s.store.RecordAudit(r.Context(), event)
}
