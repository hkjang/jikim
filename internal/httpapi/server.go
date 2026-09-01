package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/hkjang/jikim/internal/ids"
	"github.com/hkjang/jikim/internal/model"
	"github.com/hkjang/jikim/internal/store"
)

type Server struct {
	store             *store.Store
	logger            *slog.Logger
	httpClient        *http.Client
	aiClient          *http.Client
	static            http.Handler
	loginLimiter      *loginRateLimiter
	aiLimiter         *aiRequestLimiter
	webhookSlots      chan struct{}
	transitAuthorizer func(context.Context, model.User, string, string) (bool, error)
	secretAuthorizer  func(context.Context, model.User, string, string) (bool, error)
	transitDecryptor  func(context.Context, string, string) (string, error)
	auditRecorder     func(context.Context, model.AuditEvent) error
}

func New(st *store.Store, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Server{
		store:        st,
		logger:       logger,
		httpClient:   newOutboundHTTPClient(logger, 30*time.Second),
		aiClient:     newOutboundHTTPClient(logger, 0),
		loginLimiter: newLoginRateLimiter(),
		aiLimiter:    newAIRequestLimiter(),
		webhookSlots: make(chan struct{}, 16),
	}
	s.static = discoverStaticHandler(logger)
	mux := http.NewServeMux()
	s.routes(mux)
	return s.requestID(s.recoverer(s.securityHeaders(s.audit(mux))))
}

func (s *Server) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /readyz", s.ready)
	mux.HandleFunc("GET /api/v1/version", s.version)
	mux.HandleFunc("GET /api/v1/capabilities", s.capabilities)
	mux.HandleFunc("GET /api/openapi.json", s.openAPI)
	mux.HandleFunc("GET /api/v1/settings/public", s.publicSettings)
	mux.HandleFunc("GET /api/v1/session", s.sessionStatus)
	mux.HandleFunc("POST /api/v1/auth/login", s.login)
	mux.HandleFunc("GET /api/v1/oidc/config", s.oidcPublicConfig)
	mux.HandleFunc("GET /api/v1/oidc/login", s.oidcLogin)
	mux.HandleFunc("GET /api/v1/oidc/callback", s.oidcCallback)
	mux.HandleFunc("POST /api/v1/oidc/exchange", s.oidcExchange)
	mux.Handle("GET /api/v1/oidc/logout", s.withAuth(http.HandlerFunc(s.oidcLogout)))
	mux.Handle("POST /api/v1/oidc/test", s.requireRoles(http.HandlerFunc(s.oidcTest), "admin"))

	mux.Handle("GET /api/v1/me", s.withAuth(http.HandlerFunc(s.me)))
	mux.Handle("PATCH /api/v1/me", s.withAuth(http.HandlerFunc(s.updateMe)))
	mux.Handle("POST /api/v1/me/password", s.withAuth(http.HandlerFunc(s.changeMyPassword)))
	mux.Handle("POST /api/v1/auth/logout", s.withAuth(http.HandlerFunc(s.logout)))
	mux.Handle("GET /api/v1/dashboard", s.withAuth(http.HandlerFunc(s.dashboard)))

	mux.Handle("GET /api/v1/secrets", s.withAuth(http.HandlerFunc(s.listSecrets)))
	mux.Handle("POST /api/v1/secrets", s.withAuth(http.HandlerFunc(s.createSecret)))
	mux.Handle("GET /api/v1/secrets/{id}", s.withAuth(http.HandlerFunc(s.getSecret)))
	mux.Handle("PUT /api/v1/secrets/{id}", s.withAuth(http.HandlerFunc(s.updateSecret)))
	mux.Handle("PATCH /api/v1/secrets/{id}", s.withAuth(http.HandlerFunc(s.updateSecret)))
	mux.Handle("DELETE /api/v1/secrets/{id}", s.withAuth(http.HandlerFunc(s.deleteSecret)))
	mux.Handle("GET /api/v1/secrets/{id}/versions", s.withAuth(http.HandlerFunc(s.secretVersions)))
	mux.Handle("POST /api/v1/secrets/{id}/reveal", s.withAuth(http.HandlerFunc(s.revealSecret)))
	mux.Handle("POST /api/v1/secrets/{id}/rotate", s.withAuth(http.HandlerFunc(s.rotateSecret)))

	mux.Handle("GET /api/v1/applications", s.withAuth(http.HandlerFunc(s.listApplications)))
	mux.Handle("POST /api/v1/applications", s.requireRoles(http.HandlerFunc(s.createApplication), "admin", "manager"))
	mux.Handle("GET /api/v1/applications/{id}", s.withAuth(http.HandlerFunc(s.getApplication)))
	mux.Handle("PUT /api/v1/applications/{id}", s.requireRoles(http.HandlerFunc(s.updateApplication), "admin", "manager"))
	mux.Handle("PATCH /api/v1/applications/{id}", s.requireRoles(http.HandlerFunc(s.updateApplication), "admin", "manager"))
	mux.Handle("DELETE /api/v1/applications/{id}", s.requireRoles(http.HandlerFunc(s.deleteApplication), "admin", "manager"))

	mux.Handle("GET /api/v1/users", s.requireRoles(http.HandlerFunc(s.listUsers), "admin", "manager", "auditor"))
	mux.Handle("POST /api/v1/users", s.requireRoles(http.HandlerFunc(s.createUser), "admin"))
	mux.Handle("GET /api/v1/users/{id}", s.requireRoles(http.HandlerFunc(s.getUser), "admin", "manager", "auditor"))
	mux.Handle("PATCH /api/v1/users/{id}", s.requireRoles(http.HandlerFunc(s.updateUser), "admin"))
	mux.Handle("DELETE /api/v1/users/{id}", s.requireRoles(http.HandlerFunc(s.deleteUser), "admin"))

	mux.Handle("GET /api/v1/policies", s.requireRoles(http.HandlerFunc(s.listPolicies), "admin", "manager", "auditor"))
	mux.Handle("POST /api/v1/policies", s.requireRoles(http.HandlerFunc(s.createPolicy), "admin", "manager"))
	mux.Handle("GET /api/v1/policies/{id}", s.requireRoles(http.HandlerFunc(s.getPolicy), "admin", "manager", "auditor"))
	mux.Handle("PUT /api/v1/policies/{id}", s.requireRoles(http.HandlerFunc(s.updatePolicy), "admin", "manager"))
	mux.Handle("PATCH /api/v1/policies/{id}", s.requireRoles(http.HandlerFunc(s.updatePolicy), "admin", "manager"))
	mux.Handle("DELETE /api/v1/policies/{id}", s.requireRoles(http.HandlerFunc(s.deletePolicy), "admin", "manager"))
	mux.Handle("GET /api/v1/policies/{id}/users", s.requireRoles(http.HandlerFunc(s.getPolicyUsers), "admin", "manager", "auditor"))
	mux.Handle("PUT /api/v1/policies/{id}/users", s.requireRoles(http.HandlerFunc(s.setPolicyUsers), "admin", "manager"))
	mux.Handle("POST /api/v1/policies/simulate", s.requireRoles(http.HandlerFunc(s.simulatePolicy), "admin", "manager", "auditor"))

	mux.Handle("GET /api/v1/keys", s.withAuth(http.HandlerFunc(s.listKeys)))
	mux.Handle("POST /api/v1/keys", s.requireRoles(http.HandlerFunc(s.createKey), "admin", "manager"))
	mux.Handle("POST /api/v1/keys/{id}/rotate", s.withAuth(http.HandlerFunc(s.rotateKey)))
	mux.Handle("PATCH /api/v1/keys/{id}/permissions", s.withAuth(http.HandlerFunc(s.updateKeyPermissions)))
	mux.Handle("GET /api/v1/tokens", s.withAuth(http.HandlerFunc(s.listTokens)))
	mux.Handle("POST /api/v1/tokens", s.withAuth(http.HandlerFunc(s.createToken)))
	mux.Handle("POST /api/v1/tokens/{id}/revoke", s.withAuth(http.HandlerFunc(s.revokeToken)))
	mux.Handle("GET /api/v1/auth-methods", s.withAuth(http.HandlerFunc(s.authMethods)))

	mux.Handle("GET /api/v1/approvals", s.withAuth(http.HandlerFunc(s.listApprovals)))
	mux.Handle("POST /api/v1/approvals/{id}/approve", s.requireRoles(http.HandlerFunc(s.approve), "admin", "manager"))
	mux.Handle("POST /api/v1/approvals/{id}/reject", s.requireRoles(http.HandlerFunc(s.reject), "admin", "manager"))
	mux.Handle("GET /api/v1/audit", s.requireRoles(http.HandlerFunc(s.listAudit), "admin", "manager", "auditor"))
	mux.Handle("GET /api/v1/settings", s.requireRoles(http.HandlerFunc(s.settings), "admin"))
	mux.Handle("PATCH /api/v1/settings", s.requireRoles(http.HandlerFunc(s.updateSettings), "admin"))
	mux.Handle("POST /api/v1/ai/chat", s.withAuth(http.HandlerFunc(s.aiChat)))
	mux.Handle("POST /api/v1/integrations/ai/test", s.requireRoles(http.HandlerFunc(s.aiIntegrationTest), "admin"))
	mux.Handle("POST /api/v1/integrations/webhook/test", s.requireRoles(http.HandlerFunc(s.webhookTest), "admin"))
	mux.Handle("GET /api/v1/integrations/webhook/deliveries", s.requireRoles(http.HandlerFunc(s.listWebhookDeliveries), "admin", "auditor"))
	mux.Handle("POST /api/v1/integrations/webhook/deliveries/{id}/retry", s.requireRoles(http.HandlerFunc(s.retryWebhookDelivery), "admin"))

	mux.HandleFunc("GET /mcp", s.mcpGET)
	mux.Handle("POST /mcp", mcpOriginGuard(s.withAuth(http.HandlerFunc(s.mcp))))
	s.openBaoRoutes(mux)
	mux.HandleFunc("/", s.serveFrontend)
}

type contextKey string

const (
	requestIDKey contextKey = "request_id"
	sessionKey   contextKey = "session"
	tokenKey     contextKey = "token"
)

func sessionFrom(r *http.Request) (model.Session, bool) {
	session, ok := r.Context().Value(sessionKey).(model.Session)
	return session, ok
}

func requestIDFrom(r *http.Request) string {
	value, _ := r.Context().Value(requestIDKey).(string)
	return value
}

func (s *Server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := requestToken(r)
		if token == "" {
			writeError(w, r, http.StatusUnauthorized, "unauthorized", "로그인이 필요합니다")
			return
		}
		session, err := s.store.SessionByToken(r.Context(), token)
		if err != nil {
			writeError(w, r, http.StatusUnauthorized, "unauthorized", "세션이 만료되었거나 올바르지 않습니다")
			return
		}
		ctx := context.WithValue(r.Context(), sessionKey, session)
		ctx = context.WithValue(ctx, tokenKey, token)
		*r = *r.WithContext(ctx)
		next.ServeHTTP(w, r)
	})
}

func requestToken(r *http.Request) string {
	token := bearerToken(r)
	if token == "" {
		if cookie, err := r.Cookie("jikim_session"); err == nil {
			token = cookie.Value
		}
	}
	if token == "" {
		token = strings.TrimSpace(r.Header.Get("X-Vault-Token"))
	}
	return token
}

func (s *Server) requireRoles(next http.Handler, roles ...string) http.Handler {
	allowed := make(map[string]bool, len(roles))
	for _, role := range roles {
		allowed[role] = true
	}
	return s.withAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, _ := sessionFrom(r)
		if !allowed[session.User.Role] {
			writeError(w, r, http.StatusForbidden, "forbidden", "이 작업을 수행할 권한이 없습니다")
			return
		}
		next.ServeHTTP(w, r)
	}))
}

func bearerToken(r *http.Request) string {
	value := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(value) > 7 && strings.EqualFold(value[:7], "Bearer ") {
		return strings.TrimSpace(value[7:])
	}
	return ""
}

func (s *Server) requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if requestID == "" || len(requestID) > 128 {
			requestID = ids.RequestID()
		}
		w.Header().Set("X-Request-ID", requestID)
		*r = *r.WithContext(context.WithValue(r.Context(), requestIDKey, requestID))
		next.ServeHTTP(w, r)
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.Error("panic", "error", recovered, "stack", string(debug.Stack()), "request_id", requestIDFrom(r))
				writeError(w, r, http.StatusInternalServerError, "internal_error", "서버 내부 오류가 발생했습니다")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; font-src 'self' data:; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(data)
}

func (w *statusWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (s *Server) audit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		if !strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, "/v1/") && r.URL.Path != "/mcp" {
			return
		}
		if r.URL.Path == "/api/v1/audit" {
			return
		}
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		event := model.AuditEvent{RequestID: requestIDFrom(r), Action: auditAction(r), Resource: auditResource(r),
			Method: r.Method, Path: r.URL.Path, StatusCode: status, Success: status < 400,
			RemoteIP: remoteIP(r), UserAgent: r.UserAgent()}
		if session, ok := sessionFrom(r); ok {
			event.UserID = &session.User.ID
			event.Username = session.User.Username
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := s.store.RecordAudit(ctx, event); err != nil {
			s.logger.Warn("감사 로그 저장 실패", "error", err, "request_id", event.RequestID)
		}
	})
}

func auditAction(r *http.Request) string {
	path := strings.Trim(r.URL.Path, "/")
	if path == "" {
		return strings.ToLower(r.Method)
	}
	return strings.ToLower(r.Method) + "." + strings.ReplaceAll(path, "/", ".")
}

func auditResource(r *http.Request) string {
	if id := r.PathValue("id"); id != "" {
		return id
	}
	return r.URL.Path
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		writeError(w, r, http.StatusServiceUnavailable, "not_ready", "데이터베이스 연결을 확인할 수 없습니다")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ready"})
}

func errorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return http.StatusNotFound, "not_found"
	case errors.Is(err, store.ErrConflict):
		return http.StatusConflict, "conflict"
	case errors.Is(err, store.ErrUnauthorized):
		return http.StatusUnauthorized, "unauthorized"
	case errors.Is(err, store.ErrForbidden), errors.Is(err, store.ErrRequesterMatch):
		return http.StatusForbidden, "forbidden"
	case errors.Is(err, store.ErrInvalid):
		return http.StatusBadRequest, "invalid_request"
	default:
		return http.StatusInternalServerError, "internal_error"
	}
}

func (s *Server) storeError(w http.ResponseWriter, r *http.Request, err error) {
	status, code := errorStatus(err)
	message := err.Error()
	if status == http.StatusInternalServerError {
		s.logger.Error("request failed", "error", err, "request_id", requestIDFrom(r), "path", r.URL.Path)
		message = "요청을 처리하지 못했습니다"
	}
	writeError(w, r, status, code, message)
}

var _ = fmt.Sprintf
