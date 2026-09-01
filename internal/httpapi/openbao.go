package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
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
	mux.Handle("GET /v1/auth/token/lookup-self", s.withBaoAuth(http.HandlerFunc(s.baoLookupSelf)))
	mux.Handle("POST /v1/auth/token/create", s.withBaoAuth(http.HandlerFunc(s.baoTokenCreate)))
	mux.Handle("PUT /v1/auth/token/create", s.withBaoAuth(http.HandlerFunc(s.baoTokenCreate)))
	mux.Handle("POST /v1/auth/token/revoke-self", s.withBaoAuth(http.HandlerFunc(s.baoRevokeSelf)))
	mux.Handle("PUT /v1/auth/token/revoke-self", s.withBaoAuth(http.HandlerFunc(s.baoRevokeSelf)))
	mux.Handle("GET /v1/secret/data/{path...}", s.withBaoAuth(http.HandlerFunc(s.baoKVRead)))
	mux.Handle("POST /v1/secret/data/{path...}", s.withBaoAuth(http.HandlerFunc(s.baoKVWrite)))
	mux.Handle("PUT /v1/secret/data/{path...}", s.withBaoAuth(http.HandlerFunc(s.baoKVWrite)))
	mux.Handle("DELETE /v1/secret/data/{path...}", s.withBaoAuth(http.HandlerFunc(s.baoKVDeleteLatest)))
	mux.Handle("POST /v1/secret/delete/{path...}", s.withBaoAuth(http.HandlerFunc(s.baoKVDeleteVersions)))
	mux.Handle("POST /v1/secret/undelete/{path...}", s.withBaoAuth(http.HandlerFunc(s.baoKVUndeleteVersions)))
	mux.Handle("PUT /v1/secret/destroy/{path...}", s.withBaoAuth(http.HandlerFunc(s.baoKVDestroyVersions)))
	mux.Handle("GET /v1/secret/metadata/{path...}", s.withBaoAuth(http.HandlerFunc(s.baoKVMetadata)))
	mux.Handle("LIST /v1/secret/metadata/{path...}", s.withBaoAuth(http.HandlerFunc(s.baoKVList)))
	mux.Handle("DELETE /v1/secret/metadata/{path...}", s.withBaoAuth(http.HandlerFunc(s.baoKVMetadataDelete)))
	mux.Handle("GET /v1/secret/metadata", s.withBaoAuth(http.HandlerFunc(s.baoKVMetadata)))
	mux.Handle("LIST /v1/secret/metadata", s.withBaoAuth(http.HandlerFunc(s.baoKVList)))
	mux.Handle("POST /v1/transit/encrypt/{key}", s.withBaoAuth(http.HandlerFunc(s.baoTransitEncrypt)))
	mux.Handle("PUT /v1/transit/encrypt/{key}", s.withBaoAuth(http.HandlerFunc(s.baoTransitEncrypt)))
	mux.Handle("POST /v1/transit/decrypt/{key}", s.withBaoAuth(http.HandlerFunc(s.baoTransitDecrypt)))
	mux.Handle("PUT /v1/transit/decrypt/{key}", s.withBaoAuth(http.HandlerFunc(s.baoTransitDecrypt)))
}

func (s *Server) withBaoAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := requestToken(r)
		if token == "" {
			baoError(w, http.StatusUnauthorized, "missing client token")
			return
		}
		session, err := s.store.SessionByToken(r.Context(), token)
		if err != nil {
			baoError(w, http.StatusForbidden, "permission denied")
			return
		}
		ctx := context.WithValue(r.Context(), sessionKey, session)
		ctx = context.WithValue(ctx, tokenKey, token)
		*r = *r.WithContext(ctx)
		next.ServeHTTP(w, r)
	})
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
	if !decodeBaoJSON(w, r, &input) {
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
	if r.ContentLength > 0 && !decodeBaoJSON(w, r, &input) {
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
	requestedVersion, err := requestedOpenBaoVersion(r.URL.Query().Get("version"))
	if err != nil {
		baoError(w, http.StatusBadRequest, "invalid version")
		return
	}
	metadata, err := s.store.OpenBaoKVMetadata(r.Context(), path)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			baoNotFound(w)
		} else if errors.Is(err, store.ErrInvalid) {
			baoError(w, http.StatusBadRequest, err.Error())
		} else {
			baoError(w, http.StatusInternalServerError, "failed to read secret metadata")
		}
		return
	}
	if requestedVersion == 0 {
		requestedVersion = metadata.CurrentVersion
	}
	version, ok := findOpenBaoVersion(metadata, requestedVersion)
	if !ok {
		baoNotFound(w)
		return
	}
	if version.Destroyed || version.DeletionTime != nil {
		writeJSON(w, http.StatusNotFound, baoResponse(requestIDFrom(r), map[string]any{
			"data": nil, "metadata": openBaoVersionData(metadata, version),
		}, nil))
		return
	}
	secret, err := s.store.GetSecret(r.Context(), path, requestedVersion)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			baoNotFound(w)
		} else {
			baoError(w, http.StatusInternalServerError, "failed to read secret")
		}
		return
	}
	if err := s.recordSensitiveDisclosure(r, session.User, "openbao.kv.read", path,
		map[string]any{"version": requestedVersion}); err != nil {
		baoError(w, http.StatusServiceUnavailable, "audit device unavailable; secret data withheld")
		return
	}
	writeJSON(w, http.StatusOK, baoResponse(requestIDFrom(r), map[string]any{
		"data":     secret.Data,
		"metadata": openBaoVersionData(metadata, version),
	}, nil))
}

func (s *Server) baoKVWrite(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(r.PathValue("path"), "/")
	var input struct {
		Data    map[string]any `json:"data"`
		Options struct {
			CAS *int `json:"cas,omitempty"`
		} `json:"options,omitempty"`
	}
	if !decodeBaoJSON(w, r, &input) {
		return
	}
	if input.Data == nil {
		baoError(w, http.StatusBadRequest, "no data provided")
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
	secret, err := s.store.PutOpenBaoSecret(r.Context(), path, input.Data, session.User.ID, input.Options.CAS)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrCheckAndSet):
			baoError(w, http.StatusBadRequest, store.ErrCheckAndSet.Error())
		case errors.Is(err, store.ErrInvalid):
			baoError(w, http.StatusBadRequest, err.Error())
		default:
			baoError(w, http.StatusInternalServerError, "failed to write secret")
		}
		return
	}
	metadata, err := s.store.OpenBaoKVMetadata(r.Context(), path)
	if err != nil {
		baoError(w, http.StatusInternalServerError, "failed to read written secret metadata")
		return
	}
	version, ok := findOpenBaoVersion(metadata, secret.Version)
	if !ok {
		baoError(w, http.StatusInternalServerError, "failed to read written secret version")
		return
	}
	writeJSON(w, http.StatusOK, baoResponse(requestIDFrom(r), openBaoVersionData(metadata, version), nil))
}

func (s *Server) baoKVDeleteLatest(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(r.PathValue("path"), "/")
	session, _ := sessionFrom(r)
	allowed, err := s.store.CanAccess(r.Context(), session.User, path, "delete")
	if err != nil || !allowed {
		baoError(w, http.StatusForbidden, "permission denied")
		return
	}
	if err := s.store.SoftDeleteLatestOpenBaoVersion(r.Context(), path); err != nil && !errors.Is(err, store.ErrNotFound) {
		if errors.Is(err, store.ErrInvalid) {
			baoError(w, http.StatusBadRequest, err.Error())
		} else {
			baoError(w, http.StatusInternalServerError, "failed to delete secret version")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type baoKVVersionsInput struct {
	Versions []int `json:"versions"`
}

func (s *Server) baoKVDeleteVersions(w http.ResponseWriter, r *http.Request) {
	s.baoKVMutateVersions(w, r, "delete")
}

func (s *Server) baoKVUndeleteVersions(w http.ResponseWriter, r *http.Request) {
	s.baoKVMutateVersions(w, r, "undelete")
}

func (s *Server) baoKVDestroyVersions(w http.ResponseWriter, r *http.Request) {
	s.baoKVMutateVersions(w, r, "destroy")
}

func (s *Server) baoKVMutateVersions(w http.ResponseWriter, r *http.Request, operation string) {
	path := strings.Trim(r.PathValue("path"), "/")
	var input baoKVVersionsInput
	if !decodeBaoJSON(w, r, &input) {
		return
	}
	if len(input.Versions) == 0 {
		baoError(w, http.StatusBadRequest, "No version number provided")
		return
	}
	session, _ := sessionFrom(r)
	allowed, err := s.store.CanAccess(r.Context(), session.User, path, "update")
	if err != nil || !allowed {
		baoError(w, http.StatusForbidden, "permission denied")
		return
	}
	switch operation {
	case "delete":
		err = s.store.SoftDeleteOpenBaoVersions(r.Context(), path, input.Versions)
	case "undelete":
		err = s.store.UndeleteOpenBaoVersions(r.Context(), path, input.Versions)
	case "destroy":
		err = s.store.DestroySecretVersions(r.Context(), path, input.Versions)
	default:
		err = store.ErrInvalid
	}
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		if errors.Is(err, store.ErrInvalid) {
			baoError(w, http.StatusBadRequest, err.Error())
		} else {
			baoError(w, http.StatusInternalServerError, "failed to update secret versions")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) baoKVMetadata(w http.ResponseWriter, r *http.Request) {
	if openBaoListFallback(r.URL.Query().Get("list")) {
		s.baoKVList(w, r)
		return
	}
	path := strings.Trim(r.PathValue("path"), "/")
	session, _ := sessionFrom(r)
	allowed, err := s.store.CanAccess(r.Context(), session.User, path, "read")
	if err != nil || !allowed {
		baoError(w, http.StatusForbidden, "permission denied")
		return
	}
	metadata, err := s.store.OpenBaoKVMetadata(r.Context(), path)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			baoNotFound(w)
		} else if errors.Is(err, store.ErrInvalid) {
			baoError(w, http.StatusBadRequest, err.Error())
		} else {
			baoError(w, http.StatusInternalServerError, "failed to read secret metadata")
		}
		return
	}
	writeJSON(w, http.StatusOK, baoResponse(requestIDFrom(r), openBaoMetadataData(metadata), nil))
}

func (s *Server) baoKVMetadataDelete(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(r.PathValue("path"), "/")
	session, _ := sessionFrom(r)
	allowed, err := s.store.CanAccess(r.Context(), session.User, path, "delete")
	if err != nil || !allowed {
		baoError(w, http.StatusForbidden, "permission denied")
		return
	}
	if err := s.store.DeleteOpenBaoMetadata(r.Context(), path); err != nil && !errors.Is(err, store.ErrNotFound) {
		if errors.Is(err, store.ErrInvalid) {
			baoError(w, http.StatusBadRequest, err.Error())
		} else {
			baoError(w, http.StatusInternalServerError, "failed to delete secret metadata")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
	if len(keys) == 0 {
		baoNotFound(w)
		return
	}
	writeJSON(w, http.StatusOK, baoResponse(requestIDFrom(r), map[string]any{"keys": keys}, nil))
}

func requestedOpenBaoVersion(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	version, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errors.New("invalid version")
	}
	if version < 1 {
		return 0, nil
	}
	return version, nil
}

func openBaoListFallback(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true":
		return true
	default:
		return false
	}
}

func findOpenBaoVersion(metadata store.OpenBaoKVMetadata, version int) (store.OpenBaoKVVersion, bool) {
	for _, candidate := range metadata.Versions {
		if candidate.Version == version {
			return candidate, true
		}
	}
	return store.OpenBaoKVVersion{}, false
}

func openBaoDeletionTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func openBaoVersionData(metadata store.OpenBaoKVMetadata, version store.OpenBaoKVVersion) map[string]any {
	return map[string]any{
		"created_time":    version.CreatedTime.UTC().Format(time.RFC3339Nano),
		"custom_metadata": metadata.CustomMetadata,
		"deletion_time":   openBaoDeletionTime(version.DeletionTime),
		"destroyed":       version.Destroyed,
		"version":         version.Version,
	}
}

func openBaoMetadataData(metadata store.OpenBaoKVMetadata) map[string]any {
	versions := make(map[string]any, len(metadata.Versions))
	for _, version := range metadata.Versions {
		versions[strconv.Itoa(version.Version)] = map[string]any{
			"created_time":  version.CreatedTime.UTC().Format(time.RFC3339Nano),
			"deletion_time": openBaoDeletionTime(version.DeletionTime),
			"destroyed":     version.Destroyed,
		}
	}
	return map[string]any{
		"cas_required":             false,
		"created_time":             metadata.CreatedTime.UTC().Format(time.RFC3339Nano),
		"current_metadata_version": 0,
		"current_version":          metadata.CurrentVersion,
		"custom_metadata":          metadata.CustomMetadata,
		"delete_version_after":     "0s",
		"max_versions":             0,
		"metadata_cas_required":    false,
		"oldest_version":           metadata.OldestVersion,
		"updated_time":             metadata.UpdatedTime.UTC().Format(time.RFC3339Nano),
		"versions":                 versions,
	}
}

func (s *Server) baoTransitEncrypt(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Plaintext string `json:"plaintext"`
	}
	if !decodeBaoJSON(w, r, &input) {
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
	if !decodeBaoJSON(w, r, &input) {
		return
	}
	session, _ := sessionFrom(r)
	allowed, err := s.authorizeTransit(r, session.User, r.PathValue("key"), "decrypt")
	if err != nil || !allowed {
		baoError(w, http.StatusForbidden, "permission denied")
		return
	}
	plaintext, err := s.decryptTransit(r.Context(), r.PathValue("key"), input.Ciphertext)
	if err != nil {
		baoError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Validate that output remains canonical base64.
	if _, err := base64.StdEncoding.DecodeString(plaintext); err != nil {
		baoError(w, http.StatusInternalServerError, "invalid plaintext")
		return
	}
	if err := s.recordSensitiveDisclosure(r, session.User, "openbao.transit.decrypt",
		"transit/"+strings.Trim(r.PathValue("key"), "/"), map[string]any{"key": r.PathValue("key")}); err != nil {
		baoError(w, http.StatusServiceUnavailable, "audit device unavailable; plaintext withheld")
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

func decodeBaoJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	if err := decoder.Decode(dst); err != nil {
		baoError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		baoError(w, http.StatusBadRequest, "request body must contain a single JSON value")
		return false
	}
	return true
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

func baoNotFound(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]any{"errors": []string{}})
}
