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
		session, err := s.resolveSession(r.Context(), token)
		if err != nil {
			status, message := baoSessionFailure(err)
			baoError(w, status, message)
			return
		}
		ctx := context.WithValue(r.Context(), sessionKey, session)
		ctx = context.WithValue(ctx, tokenKey, token)
		*r = *r.WithContext(ctx)
		next.ServeHTTP(w, r)
	})
}

// baoSessionFailure maps a failed token lookup to its OpenBao-facing status and
// message. A token that no longer resolves is the denial OpenBao reports, but a
// lookup that could not run is a server fault: calling it "permission denied"
// tells the caller its token was rejected while the real cause is an
// unreachable database, and the driver detail must not reach the client.
func baoSessionFailure(err error) (int, string) {
	if errors.Is(err, store.ErrUnauthorized) {
		return http.StatusForbidden, "permission denied"
	}
	return http.StatusInternalServerError, "failed to look up token"
}

// baoAllow reports whether a capability check let the request through and
// writes the OpenBao-shaped failure itself when it did not. A policy lookup
// that could not run is a server fault, not a denial: reporting it as
// "permission denied" sends the caller hunting a policy mistake while the real
// cause is an unreachable database.
func baoAllow(w http.ResponseWriter, allowed bool, err error) bool {
	if err != nil {
		status, message := baoAccessFailure(err)
		baoError(w, status, message)
		return false
	}
	if !allowed {
		baoError(w, http.StatusForbidden, "permission denied")
		return false
	}
	return true
}

// baoAccessFailure maps a failed capability lookup to its OpenBao-facing status
// and message. Only ErrInvalid carries a caller-safe explanation, the other
// store sentinels stay a denial as OpenBao reports it, and anything else is a
// server fault whose driver detail must not reach the client.
func baoAccessFailure(err error) (int, string) {
	switch {
	case errors.Is(err, store.ErrInvalid):
		return http.StatusBadRequest, err.Error()
	case errors.Is(err, store.ErrNotFound), errors.Is(err, store.ErrForbidden), errors.Is(err, store.ErrUnauthorized):
		return http.StatusForbidden, "permission denied"
	default:
		return http.StatusInternalServerError, "failed to check permissions"
	}
}

// baoHealthCode reads one of the status-code overrides an operator puts in a
// load balancer probe. An unparsable value is a request error, as OpenBao
// reports it: silently falling back to the default would leave the probe
// looking configured while it is not.
func baoHealthCode(raw string, fallback int) (int, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fallback, true
	}
	code, err := strconv.Atoi(trimmed)
	if err != nil || code < 100 || code > 599 {
		return 0, false
	}
	return code, true
}

func (s *Server) baoHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	activeCode, ok := baoHealthCode(r.URL.Query().Get("activecode"), http.StatusOK)
	if !ok {
		baoError(w, http.StatusBadRequest, "invalid activecode")
		return
	}
	sealedCode, ok := baoHealthCode(r.URL.Query().Get("sealedcode"), http.StatusServiceUnavailable)
	if !ok {
		baoError(w, http.StatusBadRequest, "invalid sealedcode")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	// jikim keeps every secret and transit key in PostgreSQL, so storage it
	// cannot reach is the operational state OpenBao calls sealed: the process
	// answers but can serve nothing. Reporting "unsealed and active" through an
	// outage keeps a load balancer routing traffic to a node whose every reply
	// is a fault, and hides the outage from the standard OpenBao probe.
	sealed := s.pingStorage(ctx) != nil
	status := activeCode
	if sealed {
		status = sealedCode
	}
	writeJSON(w, status, map[string]any{
		"initialized": true, "sealed": sealed, "standby": false, "performance_standby": false,
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
	user, err := s.authenticate(r.Context(), username, input.Password)
	if err != nil {
		// A credential lookup that could not run says nothing about the
		// credential. Counting it as a failed attempt locks the account out of
		// the login window for an outage the user did not cause.
		if !errors.Is(err, store.ErrUnauthorized) {
			baoError(w, http.StatusInternalServerError, "failed to authenticate")
			return
		}
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
	if err != nil {
		baoError(w, http.StatusInternalServerError, "failed to read security settings")
		return
	}
	if !security.AllowLocalLogin && user.Role != "admin" {
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
	if !baoAllow(w, allowed, err) {
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
	if !baoAllow(w, allowed, err) {
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
	if !baoAllow(w, allowed, err) {
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
	if !baoAllow(w, allowed, err) {
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
	if !baoAllow(w, allowed, err) {
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
	if !baoAllow(w, allowed, err) {
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
	if !baoAllow(w, allowed, err) {
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

// baoTransitBatchItem mirrors an OpenBao Transit batch_input entry. jikim
// implements plaintext, ciphertext and reference only; the remaining OpenBao
// parameters are reported as a per-item error instead of being ignored so a
// client never receives a result that silently dropped the derivation context
// or key version it asked for.
type baoTransitBatchItem struct {
	Plaintext      *string `json:"plaintext"`
	Ciphertext     *string `json:"ciphertext"`
	Reference      string  `json:"reference"`
	Context        *string `json:"context"`
	Nonce          *string `json:"nonce"`
	AssociatedData *string `json:"associated_data"`
	KeyVersion     *int    `json:"key_version"`
}

func (item baoTransitBatchItem) unsupportedParameter() string {
	switch {
	case item.Context != nil && *item.Context != "":
		return "context"
	case item.Nonce != nil && *item.Nonce != "":
		return "nonce"
	case item.AssociatedData != nil && *item.AssociatedData != "":
		return "associated_data"
	case item.KeyVersion != nil && *item.KeyVersion != 0:
		return "key_version"
	}
	return ""
}

func baoTransitBatchResult(item baoTransitBatchItem) map[string]any {
	result := map[string]any{}
	if item.Reference != "" {
		result["reference"] = item.Reference
	}
	return result
}

// baoTransitBatchStatus follows OpenBao: a batch that produced no successful
// item is a request error, otherwise per-item errors travel inside the body.
// An item that failed for a server-side reason is reported as a server fault
// instead of blaming the caller.
func baoTransitBatchStatus(succeeded int, serverFault bool) int {
	switch {
	case succeeded > 0:
		return http.StatusOK
	case serverFault:
		return http.StatusInternalServerError
	default:
		return http.StatusBadRequest
	}
}

// baoTransitFailure maps a Transit store failure to its OpenBao-facing status
// and message. Only ErrInvalid carries a caller-safe explanation; a missing key
// or version stays a request error as OpenBao reports it, and anything else is
// a server fault whose driver or crypto detail must not reach the client.
func baoTransitFailure(err error, operation string) (int, string) {
	switch {
	case errors.Is(err, store.ErrInvalid):
		return http.StatusBadRequest, err.Error()
	case errors.Is(err, store.ErrNotFound):
		return http.StatusBadRequest, "encryption key not found"
	default:
		return http.StatusInternalServerError, "failed to " + operation
	}
}

func (s *Server) baoTransitEncrypt(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Plaintext  string                 `json:"plaintext"`
		BatchInput *[]baoTransitBatchItem `json:"batch_input"`
	}
	if !decodeBaoJSON(w, r, &input) {
		return
	}
	key := r.PathValue("key")
	session, _ := sessionFrom(r)
	allowed, err := s.authorizeTransit(r, session.User, key, "encrypt")
	if !baoAllow(w, allowed, err) {
		return
	}
	if input.BatchInput != nil {
		s.baoTransitEncryptBatch(w, r, key, session.User.ID, *input.BatchInput)
		return
	}
	ciphertext, err := s.encryptTransit(r.Context(), key, input.Plaintext, session.User.ID)
	if err != nil {
		status, message := baoTransitFailure(err, "encrypt plaintext")
		baoError(w, status, message)
		return
	}
	keyVersion, err := transitCiphertextVersion(ciphertext)
	if err != nil {
		baoError(w, http.StatusInternalServerError, "invalid transit ciphertext")
		return
	}
	writeJSON(w, http.StatusOK, baoResponse(requestIDFrom(r), map[string]any{"ciphertext": ciphertext, "key_version": keyVersion}, nil))
}

func (s *Server) baoTransitEncryptBatch(w http.ResponseWriter, r *http.Request, key, actorID string, items []baoTransitBatchItem) {
	if len(items) == 0 {
		baoError(w, http.StatusBadRequest, "missing batch input to process")
		return
	}
	results := make([]map[string]any, 0, len(items))
	succeeded := 0
	serverFault := false
	for _, item := range items {
		result := baoTransitBatchResult(item)
		switch {
		case item.unsupportedParameter() != "":
			result["error"] = "unsupported transit parameter: " + item.unsupportedParameter()
		case item.Plaintext == nil:
			result["error"] = "missing plaintext to encrypt"
		default:
			ciphertext, encryptErr := s.encryptTransit(r.Context(), key, *item.Plaintext, actorID)
			if encryptErr != nil {
				status, message := baoTransitFailure(encryptErr, "encrypt plaintext")
				result["error"] = message
				serverFault = serverFault || status >= http.StatusInternalServerError
				break
			}
			keyVersion, versionErr := transitCiphertextVersion(ciphertext)
			if versionErr != nil {
				// jikim produced this ciphertext, so a malformed one is our fault.
				result["error"] = "invalid transit ciphertext"
				serverFault = true
				break
			}
			result["ciphertext"] = ciphertext
			result["key_version"] = keyVersion
			succeeded++
		}
		results = append(results, result)
	}
	writeJSON(w, baoTransitBatchStatus(succeeded, serverFault), baoResponse(requestIDFrom(r), map[string]any{"batch_results": results}, nil))
}

func (s *Server) baoTransitDecrypt(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Ciphertext string                 `json:"ciphertext"`
		BatchInput *[]baoTransitBatchItem `json:"batch_input"`
	}
	if !decodeBaoJSON(w, r, &input) {
		return
	}
	key := r.PathValue("key")
	session, _ := sessionFrom(r)
	allowed, err := s.authorizeTransit(r, session.User, key, "decrypt")
	if !baoAllow(w, allowed, err) {
		return
	}
	if input.BatchInput != nil {
		s.baoTransitDecryptBatch(w, r, key, session.User, *input.BatchInput)
		return
	}
	plaintext, err := s.decryptTransit(r.Context(), key, input.Ciphertext)
	if err != nil {
		status, message := baoTransitFailure(err, "decrypt ciphertext")
		baoError(w, status, message)
		return
	}
	// Validate that output remains canonical base64.
	if _, err := base64.StdEncoding.DecodeString(plaintext); err != nil {
		baoError(w, http.StatusInternalServerError, "invalid plaintext")
		return
	}
	if err := s.recordSensitiveDisclosure(r, session.User, "openbao.transit.decrypt",
		"transit/"+strings.Trim(key, "/"), map[string]any{"key": key}); err != nil {
		baoError(w, http.StatusServiceUnavailable, "audit device unavailable; plaintext withheld")
		return
	}
	writeJSON(w, http.StatusOK, baoResponse(requestIDFrom(r), map[string]any{"plaintext": plaintext}, nil))
}

func (s *Server) baoTransitDecryptBatch(w http.ResponseWriter, r *http.Request, key string, user model.User, items []baoTransitBatchItem) {
	if len(items) == 0 {
		baoError(w, http.StatusBadRequest, "missing batch input to process")
		return
	}
	results := make([]map[string]any, 0, len(items))
	succeeded := 0
	serverFault := false
	for _, item := range items {
		result := baoTransitBatchResult(item)
		switch {
		case item.unsupportedParameter() != "":
			result["error"] = "unsupported transit parameter: " + item.unsupportedParameter()
		case item.Ciphertext == nil:
			result["error"] = "missing ciphertext to decrypt"
		default:
			plaintext, decryptErr := s.decryptTransit(r.Context(), key, *item.Ciphertext)
			if decryptErr != nil {
				status, message := baoTransitFailure(decryptErr, "decrypt ciphertext")
				result["error"] = message
				serverFault = serverFault || status >= http.StatusInternalServerError
				break
			}
			// Validate that output remains canonical base64.
			if _, decodeErr := base64.StdEncoding.DecodeString(plaintext); decodeErr != nil {
				result["error"] = "invalid plaintext"
				serverFault = true
				break
			}
			result["plaintext"] = plaintext
			succeeded++
		}
		results = append(results, result)
	}
	// One audit event covers the batch; a failed audit withholds every plaintext.
	if succeeded > 0 {
		if err := s.recordSensitiveDisclosure(r, user, "openbao.transit.decrypt",
			"transit/"+strings.Trim(key, "/"),
			map[string]any{"key": key, "batch": len(items), "decrypted": succeeded}); err != nil {
			baoError(w, http.StatusServiceUnavailable, "audit device unavailable; plaintext withheld")
			return
		}
	}
	writeJSON(w, baoTransitBatchStatus(succeeded, serverFault), baoResponse(requestIDFrom(r), map[string]any{"batch_results": results}, nil))
}

func (s *Server) encryptTransit(ctx context.Context, key, plaintext, actorID string) (string, error) {
	if s.transitEncryptor != nil {
		return s.transitEncryptor(ctx, key, plaintext, actorID)
	}
	return s.store.TransitEncrypt(ctx, key, plaintext, actorID)
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
