package httpapi

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/hkjang/jikim/internal/cryptox"
	"github.com/hkjang/jikim/internal/model"
	"github.com/hkjang/jikim/internal/store"
)

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	session, _ := sessionFrom(r)
	value, err := s.roleScopedDashboard(r.Context(), session)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, value)
}

func (s *Server) roleScopedDashboard(ctx context.Context, session model.Session) (model.Dashboard, error) {
	privileged := session.User.Role == "admin" || session.User.Role == "manager" || session.User.Role == "auditor"
	var value model.Dashboard
	var err error
	if privileged {
		value, err = s.store.Dashboard(ctx)
	} else {
		value, err = s.store.UserDashboard(ctx, session.User.ID)
	}
	if err != nil {
		return model.Dashboard{}, err
	}
	value = dashboardForRole(value, session.User.Role)
	cfg, err := s.store.ApprovalConfig(ctx)
	if err != nil {
		return model.Dashboard{}, err
	}
	if !approvalReviewerAllowed(cfg.ReviewerRole, session.User.Role) {
		value.PendingApprovals, err = s.store.CountPendingApprovalsForUser(ctx, session.User.ID)
		if err != nil {
			return model.Dashboard{}, err
		}
	}
	return value, nil
}

func dashboardForRole(value model.Dashboard, role string) model.Dashboard {
	if role != "admin" && role != "manager" && role != "auditor" {
		value.RecentAudit = nil
	}
	return value
}

func (s *Server) listApplications(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePage(r)
	items, err := s.store.ListApplications(r.Context(), limit, offset)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) createApplication(w http.ResponseWriter, r *http.Request) {
	var input model.Application
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.store.PutApplication(r.Context(), "", input)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, item)
}

func (s *Server) getApplication(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.GetApplication(r.Context(), r.PathValue("id"))
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (s *Server) updateApplication(w http.ResponseWriter, r *http.Request) {
	var input model.Application
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.store.PutApplication(r.Context(), r.PathValue("id"), input)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (s *Server) deleteApplication(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteApplication(r.Context(), r.PathValue("id")); err != nil {
		s.storeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePage(r)
	items, err := s.store.ListUsers(r.Context(), limit, offset)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var input store.UserInput
	if !decodeJSON(w, r, &input) {
		return
	}
	security, err := s.store.SecurityConfig(r.Context())
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	if len(input.Password) < security.PasswordMinLength {
		writeError(w, r, http.StatusBadRequest, "weak_password", "비밀번호가 최소 길이 정책보다 짧습니다")
		return
	}
	item, err := s.store.CreateUser(r.Context(), input)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, item)
}

func (s *Server) getUser(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.GetUser(r.Context(), r.PathValue("id"))
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	var input store.UserInput
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.Password != "" {
		security, err := s.store.SecurityConfig(r.Context())
		if err != nil {
			s.storeError(w, r, err)
			return
		}
		if len(input.Password) < security.PasswordMinLength {
			writeError(w, r, http.StatusBadRequest, "weak_password", "비밀번호가 최소 길이 정책보다 짧습니다")
			return
		}
	}
	session, _ := sessionFrom(r)
	item, err := s.store.UpdateUserAsAdmin(r.Context(), r.PathValue("id"), session.User.ID, input)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	session, _ := sessionFrom(r)
	if err := s.store.DeleteUser(r.Context(), r.PathValue("id"), session.User.ID); err != nil {
		s.storeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listPolicies(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePage(r)
	items, err := s.store.ListPolicies(r.Context(), limit, offset)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) createPolicy(w http.ResponseWriter, r *http.Request) {
	var input model.Policy
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.store.PutPolicy(r.Context(), "", input)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, item)
}

func (s *Server) getPolicy(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.GetPolicy(r.Context(), r.PathValue("id"))
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (s *Server) updatePolicy(w http.ResponseWriter, r *http.Request) {
	var input model.Policy
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.store.PutPolicy(r.Context(), r.PathValue("id"), input)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (s *Server) deletePolicy(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeletePolicy(r.Context(), r.PathValue("id")); err != nil {
		s.storeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) setPolicyUsers(w http.ResponseWriter, r *http.Request) {
	var input struct {
		UserIDs []string `json:"user_ids"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if err := s.store.SetPolicyUsers(r.Context(), r.PathValue("id"), input.UserIDs); err != nil {
		s.storeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"updated": true})
}

func (s *Server) getPolicyUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.PolicyUsers(r.Context(), r.PathValue("id"))
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	userIDs := make([]string, 0, len(users))
	for _, user := range users {
		userIDs = append(userIDs, user.ID)
	}
	writeData(w, http.StatusOK, map[string]any{"users": users, "user_ids": userIDs})
}

func (s *Server) listKeys(w http.ResponseWriter, r *http.Request) {
	session, _ := sessionFrom(r)
	mine := r.URL.Query().Get("mine") == "true"
	if !mine && session.User.Role != "admin" && session.User.Role != "manager" {
		writeError(w, r, http.StatusForbidden, "forbidden", "전체 키 인벤토리를 조회할 권한이 없습니다")
		return
	}
	all := !mine && (session.User.Role == "admin" || session.User.Role == "manager")
	items, err := s.store.ListKeyInventory(r.Context(), session.User.ID, all)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	if mine {
		personal := items[:0]
		for _, item := range items {
			if item.Type == "personal" && item.OwnerUserID == session.User.ID {
				personal = append(personal, item)
			}
		}
		items = personal
	}
	writeData(w, http.StatusOK, items)
}

func normalizePermissions(input any) (map[string]any, bool) {
	permissions := make(map[string]any)
	switch values := input.(type) {
	case nil:
		return permissions, true
	case map[string]any:
		for name, value := range values {
			if name == "admin" {
				name = "manage"
			}
			permissions[name] = value
		}
		return permissions, true
	case []any:
		for _, raw := range values {
			name, ok := raw.(string)
			if !ok {
				return nil, false
			}
			if name == "admin" {
				name = "manage"
			}
			permissions[name] = true
		}
		return permissions, true
	default:
		return nil, false
	}
}

func (s *Server) createKey(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name        string `json:"name"`
		Algorithm   string `json:"algorithm"`
		Owner       string `json:"owner,omitempty"`
		Permissions any    `json:"permissions,omitempty"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.Algorithm != "" && !strings.EqualFold(input.Algorithm, "AES-256-GCM") && !strings.EqualFold(input.Algorithm, "aes256-gcm96") {
		writeError(w, r, http.StatusBadRequest, "unsupported_algorithm", "현재 AES-256-GCM 키만 지원합니다")
		return
	}
	permissions, ok := normalizePermissions(input.Permissions)
	if !ok {
		writeError(w, r, http.StatusBadRequest, "invalid_permissions", "permissions는 객체 또는 문자열 배열이어야 합니다")
		return
	}
	session, _ := sessionFrom(r)
	item, err := s.store.CreateTransitKey(r.Context(), input.Name, session.User.ID, permissions)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, item)
}

func (s *Server) rotateKey(w http.ResponseWriter, r *http.Request) {
	session, _ := sessionFrom(r)
	item, err := s.store.RotateUserKey(r.Context(), r.PathValue("id"), session.User.ID, session.User.Role == "admin")
	if errors.Is(err, store.ErrNotFound) && (session.User.Role == "admin" || session.User.Role == "manager") {
		allowed := session.User.Role == "admin"
		var permissionErr error
		if !allowed {
			allowed, permissionErr = s.store.TransitPermission(r.Context(), r.PathValue("id"), "rotate")
		}
		if permissionErr != nil || !transitManagementAllowed(session.User.Role, allowed) {
			writeError(w, r, http.StatusForbidden, "forbidden", "Transit 키 회전 권한이 없습니다")
			return
		}
		transit, transitErr := s.store.RotateTransitKey(r.Context(), r.PathValue("id"))
		if transitErr != nil {
			s.storeError(w, r, transitErr)
			return
		}
		writeData(w, http.StatusOK, transit)
		return
	}
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (s *Server) updateKeyPermissions(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Permissions any `json:"permissions"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	permissions, ok := normalizePermissions(input.Permissions)
	if !ok {
		writeError(w, r, http.StatusBadRequest, "invalid_permissions", "permissions는 객체 또는 문자열 배열이어야 합니다")
		return
	}
	session, _ := sessionFrom(r)
	item, err := s.store.UpdateKeyPermissions(r.Context(), r.PathValue("id"), permissions, session.User.ID, session.User.Role == "admin")
	if errors.Is(err, store.ErrNotFound) && (session.User.Role == "admin" || session.User.Role == "manager") {
		allowed := session.User.Role == "admin"
		var permissionErr error
		if !allowed {
			allowed, permissionErr = s.store.TransitPermission(r.Context(), r.PathValue("id"), "manage")
		}
		if permissionErr != nil || !transitManagementAllowed(session.User.Role, allowed) {
			writeError(w, r, http.StatusForbidden, "forbidden", "Transit 키 권한 변경 권한이 없습니다")
			return
		}
		transit, transitErr := s.store.UpdateTransitPermissions(r.Context(), r.PathValue("id"), permissions)
		if transitErr != nil {
			s.storeError(w, r, transitErr)
			return
		}
		writeData(w, http.StatusOK, transit)
		return
	}
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func transitManagementAllowed(role string, storedPermission bool) bool {
	return role == "admin" || (role == "manager" && storedPermission)
}

func (s *Server) listTokens(w http.ResponseWriter, r *http.Request) {
	session, _ := sessionFrom(r)
	limit, offset := parsePage(r)
	all := session.User.Role == "admin"
	items, err := s.store.ListTokens(r.Context(), session.User.ID, all, limit, offset)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) createToken(w http.ResponseWriter, r *http.Request) {
	var input struct {
		UserID     string `json:"user_id,omitempty"`
		Name       string `json:"name"`
		TTLSeconds int    `json:"ttl_seconds,omitempty"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	session, _ := sessionFrom(r)
	userID := input.UserID
	if userID == "" {
		userID = session.User.ID
	}
	if userID != session.User.ID && session.User.Role != "admin" {
		writeError(w, r, http.StatusForbidden, "forbidden", "다른 사용자의 토큰을 만들 수 없습니다")
		return
	}
	ttl := time.Duration(input.TTLSeconds) * time.Second
	if input.TTLSeconds == 0 {
		ttl = 12 * time.Hour
	}
	plain, token, err := s.store.CreateSession(r.Context(), userID, "api", strings.TrimSpace(input.Name), session.User.ID, ttl)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"data": token, "token": plain})
}

func (s *Server) revokeToken(w http.ResponseWriter, r *http.Request) {
	session, _ := sessionFrom(r)
	all := session.User.Role == "admin"
	if err := s.store.RevokeSessionByID(r.Context(), r.PathValue("id"), session.User.ID, all); err != nil {
		s.storeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) authMethods(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.store.OIDCConfig(r.Context())
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		s.storeError(w, r, err)
		return
	}
	security, err := s.store.SecurityConfig(r.Context())
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, []map[string]any{
		{"type": "token", "enabled": true, "path": "token"},
		{"type": "userpass", "enabled": security.AllowLocalLogin, "admin_recovery": true, "path": "userpass"},
		{"type": "oidc", "enabled": cfg.Enabled, "path": "oidc", "issuer_url": cfg.IssuerURL},
	})
}

func (s *Server) listApprovals(w http.ResponseWriter, r *http.Request) {
	session, _ := sessionFrom(r)
	cfg, err := s.store.ApprovalConfig(r.Context())
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	canReview := approvalReviewerAllowed(cfg.ReviewerRole, session.User.Role)
	requestedScope := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("scope")))
	if requestedScope == "all" && !canReview {
		writeError(w, r, http.StatusForbidden, "forbidden", "전체 승인 요청을 조회할 권한이 없습니다")
		return
	}
	limit, offset := parsePage(r)
	all := canReview && requestedScope != "mine"
	items, err := s.store.ListApprovalsFor(r.Context(), r.URL.Query().Get("status"), session.User.ID, all, limit, offset)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) approve(w http.ResponseWriter, r *http.Request) { s.resolveApproval(w, r, "approved") }
func (s *Server) reject(w http.ResponseWriter, r *http.Request)  { s.resolveApproval(w, r, "rejected") }

func (s *Server) resolveApproval(w http.ResponseWriter, r *http.Request, decision string) {
	var input struct {
		Comment string `json:"comment"`
	}
	if r.ContentLength != 0 && !decodeJSON(w, r, &input) {
		return
	}
	session, _ := sessionFrom(r)
	cfg, err := s.store.ApprovalConfig(r.Context())
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	if !approvalReviewerAllowed(cfg.ReviewerRole, session.User.Role) {
		writeError(w, r, http.StatusForbidden, "forbidden", "설정된 검토자 역할이 아닙니다")
		return
	}
	item, err := s.store.ResolveApproval(r.Context(), r.PathValue("id"), session.User.ID, decision, input.Comment)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	eventType := "approval.rejected"
	if decision == "approved" {
		eventType = "approval.approved"
	}
	s.queueWebhook(r, eventType, item.Resource, map[string]any{"action": item.Action, "approval_id": item.ID})
	writeData(w, http.StatusOK, item)
}

func approvalReviewerAllowed(configuredRole, actualRole string) bool {
	if actualRole == "admin" {
		return true
	}
	return configuredRole == "manager" && actualRole == "manager"
}

func (s *Server) listAudit(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePage(r)
	items, err := s.store.ListAudit(r.Context(), r.URL.Query().Get("q"), r.URL.Query().Get("result"), limit, offset)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) settings(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListSettings(r.Context())
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	result := map[string]any{
		"general":       map[string]any{"service_name": "jikim"},
		"approval":      map[string]any{"approval_enabled": false},
		"oidc":          map[string]any{"enabled": false, "client_secret_configured": false},
		"ai":            map[string]any{"enabled": false, "api_key_configured": false},
		"security":      map[string]any{},
		"notifications": map[string]any{"enabled": false, "supported_events": store.SupportedWebhookEvents()},
	}
	for _, item := range items {
		switch item.Key {
		case "service":
			result["general"] = item.Value
		case "workflow":
			result["approval"] = item.Value
		case "oidc", "ai", "security", "notifications":
			result[item.Key] = item.Value
		case "oidc_client_secret":
			value, _ := result["oidc"].(map[string]any)
			value["client_secret_configured"] = item.Configured
		case "ai_api_key":
			value, _ := result["ai"].(map[string]any)
			value["api_key_configured"] = item.Configured
		case "notification_webhook":
			value, _ := result["notifications"].(map[string]any)
			value["webhook_configured"] = item.Configured
		case "notification_webhook_secret":
			value, _ := result["notifications"].(map[string]any)
			value["signing_secret_configured"] = item.Configured
		}
	}
	if approval, ok := result["approval"].(map[string]any); ok {
		approval["four_eyes"] = true
		approval["required_approvals"] = 1
		approval["supported_targets"] = []string{"secret_write", "secret_delete"}
	}
	if notifications, ok := result["notifications"].(map[string]any); ok {
		notifications["supported_events"] = store.SupportedWebhookEvents()
	}
	writeData(w, http.StatusOK, result)
}

func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request) {
	var input map[string]map[string]any
	if !decodeJSON(w, r, &input) {
		return
	}
	session, _ := sessionFrom(r)
	if value, ok := input["general"]; ok {
		input["service"] = value
		delete(input, "general")
	}
	if value, ok := input["approval"]; ok {
		input["workflow"] = value
		delete(input, "approval")
	}
	// Convenient nested secret fields are encrypted separately and never echoed back.
	type secretChange struct {
		key   string
		value string
		clear bool
	}
	changes := make([]secretChange, 0, 4)
	extractSecret := func(values map[string]any, field, clearField, key string) {
		change := secretChange{key: key}
		if value, ok := values[field].(string); ok {
			change.value = value
		}
		change.clear, _ = values[clearField].(bool)
		delete(values, field)
		delete(values, clearField)
		if strings.TrimSpace(change.value) != "" || change.clear {
			changes = append(changes, change)
		}
	}
	if oidc, ok := input["oidc"]; ok {
		extractSecret(oidc, "client_secret", "clear_client_secret", "oidc_client_secret")
	}
	if ai, ok := input["ai"]; ok {
		extractSecret(ai, "api_key", "clear_api_key", "ai_api_key")
	}
	if notifications, ok := input["notifications"]; ok {
		extractSecret(notifications, "webhook_url", "clear_webhook", "notification_webhook")
		extractSecret(notifications, "signing_secret", "clear_signing_secret", "notification_webhook_secret")
		rotateSigning, _ := notifications["rotate_signing_secret"].(bool)
		delete(notifications, "rotate_signing_secret")
		delete(notifications, "email_recipients") // SMTP delivery is not advertised or persisted.
		allowInsecure, _ := notifications["allow_insecure_http"].(bool)
		for _, change := range changes {
			if change.key == "notification_webhook" && strings.TrimSpace(change.value) != "" {
				if err := store.ValidateWebhookURL(strings.TrimSpace(change.value), allowInsecure); err != nil {
					s.storeError(w, r, err)
					return
				}
			}
		}
		configured, err := s.store.SettingConfigured(r.Context(), "notification_webhook_secret")
		if err != nil {
			s.storeError(w, r, err)
			return
		}
		signingProvided, signingCleared := false, false
		for _, change := range changes {
			if change.key == "notification_webhook_secret" {
				signingProvided = signingProvided || strings.TrimSpace(change.value) != ""
				signingCleared = signingCleared || change.clear
			}
		}
		if !signingProvided && (rotateSigning || signingCleared || !configured) {
			random, err := cryptox.RandomKey()
			if err != nil {
				s.storeError(w, r, err)
				return
			}
			changes = append(changes, secretChange{key: "notification_webhook_secret", value: base64.RawURLEncoding.EncodeToString(random)})
		}
	}
	for key, value := range input {
		delete(value, "client_secret_configured")
		delete(value, "api_key_configured")
		delete(value, "webhook_configured")
		delete(value, "signing_secret_configured")
		delete(value, "supported_events")
		if err := store.ValidateSetting(key, value); err != nil {
			s.storeError(w, r, err)
			return
		}
	}
	if notifications, ok := input["notifications"]; ok {
		enabled, _ := notifications["enabled"].(bool)
		configured, err := s.store.SettingConfigured(r.Context(), "notification_webhook")
		if err != nil {
			s.storeError(w, r, err)
			return
		}
		for _, change := range changes {
			if change.key == "notification_webhook" {
				if change.clear {
					configured = false
				}
				if strings.TrimSpace(change.value) != "" {
					configured = true
				}
			}
		}
		if enabled && !configured {
			writeError(w, r, http.StatusBadRequest, "webhook_not_configured", "알림을 사용하려면 Webhook URL이 필요합니다")
			return
		}
	}
	if ai, ok := input["ai"]; ok {
		enabled, _ := ai["enabled"].(bool)
		authType, _ := ai["auth_type"].(string)
		if authType == "" {
			authType = "bearer"
		}
		configured, err := s.store.SettingConfigured(r.Context(), "ai_api_key")
		if err != nil {
			s.storeError(w, r, err)
			return
		}
		for _, change := range changes {
			if change.key == "ai_api_key" {
				if change.clear {
					configured = false
				}
				if strings.TrimSpace(change.value) != "" {
					configured = true
				}
			}
		}
		if enabled && authType != "none" && !configured {
			writeError(w, r, http.StatusBadRequest, "ai_api_key_required", "선택한 AI 인증 방식에는 API Key가 필요합니다")
			return
		}
	}
	for _, change := range changes {
		if change.clear {
			if err := s.store.DeleteSetting(r.Context(), change.key); err != nil {
				s.storeError(w, r, err)
				return
			}
		}
		if strings.TrimSpace(change.value) != "" {
			if err := s.store.PutSetting(r.Context(), change.key, map[string]any{"value": change.value}, session.User.ID); err != nil {
				s.storeError(w, r, err)
				return
			}
		}
	}
	for key, value := range input {
		if err := s.store.PatchSetting(r.Context(), key, value, session.User.ID); err != nil {
			s.storeError(w, r, err)
			return
		}
	}
	s.settings(w, r)
}
