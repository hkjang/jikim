package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hkjang/jikim/internal/model"
	"github.com/hkjang/jikim/internal/store"
	"github.com/hkjang/jikim/internal/version"
)

func (s *Server) openBaoRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/sys/health", s.baoHealth)
	mux.HandleFunc("POST /v1/auth/userpass/login/{username}", s.baoUserpassLogin)
	mux.HandleFunc("PUT /v1/auth/userpass/login/{username}", s.baoUserpassLogin)
	mux.Handle("GET /v1/auth/token/lookup-self", s.withAuth(http.HandlerFunc(s.baoLookupSelf)))
	mux.Handle("POST /v1/auth/token/create", s.withAuth(http.HandlerFunc(s.baoTokenCreate)))
	mux.Handle("PUT /v1/auth/token/create", s.withAuth(http.HandlerFunc(s.baoTokenCreate)))
	mux.Handle("POST /v1/auth/token/revoke-self", s.withAuth(http.HandlerFunc(s.baoRevokeSelf)))
	mux.Handle("PUT /v1/auth/token/revoke-self", s.withAuth(http.HandlerFunc(s.baoRevokeSelf)))
	mux.Handle("GET /v1/secret/data/{path...}", s.withAuth(http.HandlerFunc(s.baoKVRead)))
	mux.Handle("POST /v1/secret/data/{path...}", s.withAuth(http.HandlerFunc(s.baoKVWrite)))
	mux.Handle("PUT /v1/secret/data/{path...}", s.withAuth(http.HandlerFunc(s.baoKVWrite)))
	mux.Handle("DELETE /v1/secret/data/{path...}", s.withAuth(http.HandlerFunc(s.baoKVDelete)))
	mux.Handle("GET /v1/secret/metadata/{path...}", s.withAuth(http.HandlerFunc(s.baoKVMetadata)))
	mux.Handle("LIST /v1/secret/metadata/{path...}", s.withAuth(http.HandlerFunc(s.baoKVList)))
	mux.Handle("DELETE /v1/secret/metadata/{path...}", s.withAuth(http.HandlerFunc(s.baoKVDelete)))
	mux.Handle("POST /v1/transit/encrypt/{key}", s.withAuth(http.HandlerFunc(s.baoTransitEncrypt)))
	mux.Handle("PUT /v1/transit/encrypt/{key}", s.withAuth(http.HandlerFunc(s.baoTransitEncrypt)))
	mux.Handle("POST /v1/transit/decrypt/{key}", s.withAuth(http.HandlerFunc(s.baoTransitDecrypt)))
	mux.Handle("PUT /v1/transit/decrypt/{key}", s.withAuth(http.HandlerFunc(s.baoTransitDecrypt)))
}

func (s *Server) baoHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"initialized": true, "sealed": false, "standby": false, "performance_standby": false,
		"replication_performance_mode": "disabled", "replication_dr_mode": "disabled",
		"server_time_utc": time.Now().UTC().Unix(), "version": version.Version,
		"cluster_name": "jikim", "cluster_id": "jikim-postgres",
	})
}

func (s *Server) baoUserpassLogin(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	username := r.PathValue("username")
	rateKey := loginRateKey(r, username)
	if s.rejectRateLimitedLogin(w, r, rateKey, true) {
		return
	}
	user, err := s.store.Authenticate(r.Context(), username, input.Password)
	if err != nil {
		if s.loginLimiter != nil {
			s.loginLimiter.failed(rateKey)
		}
		baoError(w, http.StatusBadRequest, "invalid username or password")
		return
	}
	if s.loginLimiter != nil {
		s.loginLimiter.succeeded(rateKey)
	}
	security, err := s.store.SecurityConfig(r.Context())
	if err != nil || (!security.AllowLocalLogin && user.Role != "admin") {
		baoError(w, http.StatusForbidden, "local login is disabled")
		return
	}
	ttl := time.Duration(security.SessionTimeoutMinutes) * time.Minute
	token, session, err := s.store.CreateSession(r.Context(), user.ID, "openbao", "userpass", user.ID, ttl)
	if err != nil {
		baoError(w, http.StatusInternalServerError, "failed to create token")
		return
	}
	writeJSON(w, http.StatusOK, baoResponse(requestIDFrom(r), map[string]any{}, map[string]any{
		"client_token": token, "accessor": session.ID, "policies": []string{"default", user.Role},
		"token_policies": []string{"default", user.Role}, "metadata": map[string]any{"username": user.Username},
		"lease_duration": int(time.Until(session.ExpiresAt).Seconds()), "renewable": false,
		"entity_id": user.ID, "token_type": "service", "orphan": true,
	}))
}

func (s *Server) baoLookupSelf(w http.ResponseWriter, r *http.Request) {
	session, _ := sessionFrom(r)
	writeJSON(w, http.StatusOK, baoResponse(requestIDFrom(r), map[string]any{
		"id": session.ID, "accessor": session.ID, "display_name": session.User.Username,
		"entity_id": session.User.ID, "creation_time": session.CreatedAt.Unix(),
		"expire_time": session.ExpiresAt, "ttl": max(int(time.Until(session.ExpiresAt).Seconds()), 0),
		"policies": []string{"default", session.User.Role}, "path": "auth/token/create",
		"type": "service", "orphan": true,
	}, nil))
}

func (s *Server) baoTokenCreate(w http.ResponseWriter, r *http.Request) {
	session, _ := sessionFrom(r)
	if session.User.Role != "admin" && session.User.Role != "manager" {
		baoError(w, http.StatusForbidden, "permission denied")
		return
	}
	var input struct {
		TTL         string `json:"ttl"`
		DisplayName string `json:"display_name"`
	}
	if r.ContentLength > 0 && !decodeJSON(w, r, &input) {
		return
	}
	ttl := 12 * time.Hour
	if input.TTL != "" {
		parsed, err := time.ParseDuration(input.TTL)
		if err != nil || parsed <= 0 || parsed > 30*24*time.Hour {
			baoError(w, http.StatusBadRequest, "invalid ttl")
			return
		}
		ttl = parsed
	}
	token, child, err := s.store.CreateSession(r.Context(), session.User.ID, "openbao", input.DisplayName, session.User.ID, ttl)
	if err != nil {
		baoError(w, http.StatusInternalServerError, "failed to create token")
		return
	}
	writeJSON(w, http.StatusOK, baoResponse(requestIDFrom(r), nil, map[string]any{
		"client_token": token, "accessor": child.ID, "policies": []string{"default", session.User.Role},
		"token_policies": []string{"default", session.User.Role}, "lease_duration": int(ttl.Seconds()),
		"renewable": false, "entity_id": session.User.ID, "token_type": "service", "orphan": true,
	}))
}

func (s *Server) baoRevokeSelf(w http.ResponseWriter, r *http.Request) {
	token, _ := r.Context().Value(tokenKey).(string)
	_ = s.store.RevokeToken(r.Context(), token)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) baoKVRead(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(r.PathValue("path"), "/")
	session, _ := sessionFrom(r)
	allowed, err := s.store.CanAccess(r.Context(), session.User, path, "read")
	if err != nil || !allowed {
		baoError(w, http.StatusForbidden, "permission denied")
		return
	}
	requestedVersion, _ := strconv.Atoi(r.URL.Query().Get("version"))
	secret, err := s.store.GetSecret(r.Context(), path, requestedVersion)
	if err != nil {
		baoError(w, http.StatusNotFound, "no data found at path")
		return
	}
	writeJSON(w, http.StatusOK, baoResponse(requestIDFrom(r), map[string]any{
		"data": secret.Data,
		"metadata": map[string]any{"created_time": secret.CreatedAt.Format(time.RFC3339Nano),
			"deletion_time": "", "destroyed": false, "version": secret.Version, "custom_metadata": secret.Metadata},
	}, nil))
}

func (s *Server) baoKVWrite(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(r.PathValue("path"), "/")
	var input struct {
		Data    map[string]any `json:"data"`
		Options map[string]any `json:"options,omitempty"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	session, _ := sessionFrom(r)
	capability := "update"
	exists, existsErr := s.store.SecretExistsByPath(r.Context(), path)
	if existsErr != nil {
		baoError(w, http.StatusInternalServerError, "failed to inspect secret")
		return
	}
	if !exists {
		capability = "create"
	}
	allowed, err := s.store.CanAccess(r.Context(), session.User, path, capability)
	if err != nil || !allowed {
		baoError(w, http.StatusForbidden, "permission denied")
		return
	}
	write := model.SecretWrite{Path: path, Data: input.Data}
	requiresApproval, err := s.store.ApprovalRequired(r.Context(), "secret_write")
	if err != nil {
		baoError(w, http.StatusInternalServerError, "failed to inspect approval policy")
		return
	}
	if requiresApproval {
		approval, approvalErr := s.store.CreateApproval(r.Context(), "secret.put", path, session.User.ID, write)
		if approvalErr != nil {
			baoError(w, http.StatusInternalServerError, "failed to create approval request")
			return
		}
		writeJSON(w, http.StatusAccepted, baoResponse(requestIDFrom(r), map[string]any{
			"approval_id": approval.ID, "status": approval.Status,
		}, nil))
		return
	}
	secret, err := s.store.PutSecret(r.Context(), write, session.User.ID)
	if err != nil {
		baoError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, baoResponse(requestIDFrom(r), map[string]any{
		"created_time": secret.UpdatedAt.Format(time.RFC3339Nano), "version": secret.Version,
	}, nil))
}

func (s *Server) baoKVDelete(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(r.PathValue("path"), "/")
	session, _ := sessionFrom(r)
	allowed, err := s.store.CanAccess(r.Context(), session.User, path, "delete")
	if err != nil || !allowed {
		baoError(w, http.StatusForbidden, "permission denied")
		return
	}
	requiresApproval, err := s.store.ApprovalRequired(r.Context(), "secret_delete")
	if err != nil {
		baoError(w, http.StatusInternalServerError, "failed to inspect approval policy")
		return
	}
	if requiresApproval {
		approval, approvalErr := s.store.CreateApproval(r.Context(), "secret.delete", path, session.User.ID, map[string]any{})
		if approvalErr != nil {
			baoError(w, http.StatusInternalServerError, "failed to create approval request")
			return
		}
		writeJSON(w, http.StatusAccepted, baoResponse(requestIDFrom(r), map[string]any{
			"approval_id": approval.ID, "status": approval.Status,
		}, nil))
		return
	}
	if err := s.store.DeleteSecret(r.Context(), path); err != nil {
		baoError(w, http.StatusNotFound, "no data found at path")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) baoKVMetadata(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(r.PathValue("path"), "/")
	session, _ := sessionFrom(r)
	allowed, err := s.store.CanAccess(r.Context(), session.User, path, "read")
	if err != nil || !allowed {
		baoError(w, http.StatusForbidden, "permission denied")
		return
	}
	versions, err := s.store.SecretVersions(r.Context(), path)
	if err != nil {
		baoError(w, http.StatusNotFound, "no data found at path")
		return
	}
	versionMap := make(map[string]any)
	for _, value := range versions {
		versionMap[strconv.Itoa(value["version"].(int))] = value
	}
	writeJSON(w, http.StatusOK, baoResponse(requestIDFrom(r), map[string]any{
		"current_version": func() int {
			if len(versions) > 0 {
				return versions[0]["version"].(int)
			}
			return 0
		}(),
		"oldest_version": 1, "max_versions": 0, "cas_required": false, "delete_version_after": "0s",
		"versions": versionMap,
	}, nil))
}

func (s *Server) baoKVList(w http.ResponseWriter, r *http.Request) {
	prefix := strings.Trim(r.PathValue("path"), "/")
	session, _ := sessionFrom(r)
	allowed, err := s.store.CanAccess(r.Context(), session.User, strings.TrimSuffix(prefix+"/", "//"), "list")
	if err != nil || !allowed {
		baoError(w, http.StatusForbidden, "permission denied")
		return
	}
	keys, err := s.store.ListSecretChildren(r.Context(), prefix)
	if err != nil {
		baoError(w, http.StatusInternalServerError, "failed to list secrets")
		return
	}
	writeJSON(w, http.StatusOK, baoResponse(requestIDFrom(r), map[string]any{"keys": keys}, nil))
}

func (s *Server) baoTransitEncrypt(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Plaintext string `json:"plaintext"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	session, _ := sessionFrom(r)
	allowed, err := s.authorizeTransit(r, session.User, r.PathValue("key"), "encrypt")
	if err != nil || !allowed {
		baoError(w, http.StatusForbidden, "permission denied")
		return
	}
	ciphertext, err := s.store.TransitEncrypt(r.Context(), r.PathValue("key"), input.Plaintext, session.User.ID)
	if err != nil {
		baoError(w, http.StatusBadRequest, err.Error())
		return
	}
	keyVersion, err := transitCiphertextVersion(ciphertext)
	if err != nil {
		baoError(w, http.StatusInternalServerError, "invalid transit ciphertext")
		return
	}
	writeJSON(w, http.StatusOK, baoResponse(requestIDFrom(r), map[string]any{"ciphertext": ciphertext, "key_version": keyVersion}, nil))
}

func (s *Server) baoTransitDecrypt(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Ciphertext string `json:"ciphertext"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	session, _ := sessionFrom(r)
	allowed, err := s.authorizeTransit(r, session.User, r.PathValue("key"), "decrypt")
	if err != nil || !allowed {
		baoError(w, http.StatusForbidden, "permission denied")
		return
	}
	plaintext, err := s.store.TransitDecrypt(r.Context(), r.PathValue("key"), input.Ciphertext)
	if err != nil {
		baoError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Validate that output remains canonical base64.
	if _, err := base64.StdEncoding.DecodeString(plaintext); err != nil {
		baoError(w, http.StatusInternalServerError, "invalid plaintext")
		return
	}
	writeJSON(w, http.StatusOK, baoResponse(requestIDFrom(r), map[string]any{"plaintext": plaintext}, nil))
}

func (s *Server) authorizeTransit(r *http.Request, user model.User, key, operation string) (bool, error) {
	if s.transitAuthorizer != nil {
		return s.transitAuthorizer(r.Context(), user, key, operation)
	}
	allowed, err := s.store.CanAccess(r.Context(), user, "transit/"+strings.Trim(key, "/"), operation)
	if err != nil || !allowed {
		return false, err
	}
	permission, err := s.store.TransitPermission(r.Context(), key, operation)
	if errors.Is(err, store.ErrNotFound) && operation == "encrypt" {
		return s.store.CanAccess(r.Context(), user, "transit/"+strings.Trim(key, "/"), "create")
	}
	return permission, err
}

func transitCiphertextVersion(ciphertext string) (int, error) {
	parts := strings.SplitN(ciphertext, ":", 3)
	if len(parts) != 3 || parts[0] != "vault" || !strings.HasPrefix(parts[1], "v") {
		return 0, errors.New("invalid ciphertext")
	}
	version, err := strconv.Atoi(strings.TrimPrefix(parts[1], "v"))
	if err != nil || version < 1 {
		return 0, errors.New("invalid ciphertext version")
	}
	return version, nil
}

func baoResponse(requestID string, data, auth map[string]any) map[string]any {
	response := map[string]any{"request_id": requestID, "lease_id": "", "renewable": false, "lease_duration": 0, "wrap_info": nil, "warnings": nil, "mount_type": ""}
	if data != nil {
		response["data"] = data
	}
	if auth != nil {
		response["auth"] = auth
	}
	return response
}

func baoError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"errors": []string{message}})
}
