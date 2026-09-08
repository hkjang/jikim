package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/hkjang/jikim/internal/model"
	"github.com/hkjang/jikim/internal/store"
	"github.com/hkjang/jikim/internal/version"
)

const (
	mcpProtocolLatest = "2025-11-25"
	mcpProtocolLegacy = "2025-06-18"
)

var supportedMCPProtocols = map[string]bool{
	mcpProtocolLatest: true,
	mcpProtocolLegacy: true,
}

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type mcpToolParams struct {
	Name      string
	Arguments map[string]any
}

func (s *Server) mcpGET(w http.ResponseWriter, r *http.Request) {
	if !validMCPOrigin(r) {
		mcpHTTPError(w, nil, http.StatusForbidden, -32600, "Invalid Origin")
		return
	}
	w.Header().Set("Allow", http.MethodPost)
	w.WriteHeader(http.StatusMethodNotAllowed)
}

func mcpOriginGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !validMCPOrigin(r) {
			mcpHTTPError(w, nil, http.StatusForbidden, -32600, "Invalid Origin")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) mcp(w http.ResponseWriter, r *http.Request) {
	if !validMCPOrigin(r) {
		mcpHTTPError(w, nil, http.StatusForbidden, -32600, "Invalid Origin")
		return
	}
	if !validMCPContentType(r) {
		mcpHTTPError(w, nil, http.StatusUnsupportedMediaType, -32600, "Content-Type must be application/json")
		return
	}
	request, parseCode, err := decodeMCPRequest(w, r)
	if err != nil {
		mcpHTTPError(w, nil, http.StatusBadRequest, parseCode, err.Error())
		return
	}
	if request.JSONRPC != "2.0" || request.Method == "" || (!mcpNotification(request) && !validMCPRequestID(request.ID)) {
		mcpHTTPError(w, request.ID, http.StatusBadRequest, -32600, "Invalid Request")
		return
	}
	if err := validateMCPProtocolHeader(r, request.Method == "initialize"); err != nil {
		mcpHTTPError(w, request.ID, http.StatusBadRequest, -32600, err.Error())
		return
	}
	if err := validateMCPAcceptHeader(r); err != nil {
		mcpHTTPError(w, request.ID, http.StatusNotAcceptable, -32600, err.Error())
		return
	}

	if mcpNotification(request) {
		// JSON-RPC notifications are one-way. Known and unknown notifications are
		// accepted without a JSON-RPC response body.
		w.WriteHeader(http.StatusAccepted)
		return
	}

	switch request.Method {
	case "initialize":
		requested, err := validateMCPInitializeParams(request.Params)
		if err != nil {
			mcpError(w, request.ID, -32602, "Invalid params")
			return
		}
		negotiated := mcpProtocolLatest
		if supportedMCPProtocols[requested] {
			negotiated = requested
		}
		mcpResult(w, request.ID, map[string]any{
			"protocolVersion": negotiated,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": "jikim", "title": "jikim Secrets Security Platform", "version": version.Version},
		})
	case "ping":
		if !validOptionalObjectParams(request.Params) {
			mcpError(w, request.ID, -32602, "Invalid params")
			return
		}
		mcpResult(w, request.ID, map[string]any{})
	case "tools/list":
		if !validMCPToolsListParams(request.Params) {
			mcpError(w, request.ID, -32602, "Invalid params")
			return
		}
		mcpResult(w, request.ID, map[string]any{"tools": mcpTools()})
	case "tools/call":
		params, err := decodeMCPToolParams(request.Params)
		if err != nil || validateMCPToolArguments(params.Name, params.Arguments) != nil {
			mcpError(w, request.ID, -32602, "Invalid params")
			return
		}
		s.mcpToolCall(w, r, request.ID, params)
	default:
		mcpError(w, request.ID, -32601, "Method not found")
	}
}

func mcpTools() []map[string]any {
	return []map[string]any{
		{"name": "dashboard.get", "description": "보안 대시보드 요약을 조회합니다", "inputSchema": objectSchema(nil, nil)},
		{"name": "secrets.list", "description": "Secret 값 없이 메타데이터 목록만 조회합니다", "inputSchema": objectSchema(map[string]any{"query": map[string]string{"type": "string"}}, nil)},
		{"name": "secrets.metadata", "description": "Secret 값 없이 버전 및 위험도 메타데이터를 조회합니다", "inputSchema": objectSchema(map[string]any{"path": map[string]string{"type": "string"}}, []string{"path"})},
		{"name": "policies.list", "description": "접근 정책을 조회합니다", "inputSchema": objectSchema(nil, nil)},
		{"name": "audit.search", "description": "권한이 있는 운영자가 감사 이벤트를 검색합니다", "inputSchema": objectSchema(map[string]any{"query": map[string]string{"type": "string"}}, nil)},
		{"name": "access.check", "description": "현재 로그인 사용자의 경로별 권한을 확인합니다", "inputSchema": objectSchema(map[string]any{
			"path": map[string]string{"type": "string"},
			"capability": map[string]any{"type": "string", "enum": []string{
				"create", "read", "update", "delete", "list", "rotate", "encrypt", "decrypt",
			}},
		}, []string{"path", "capability"})},
		{"name": "transit.encrypt", "description": "base64 평문을 이름 있는 Transit 키로 암호화합니다", "inputSchema": objectSchema(map[string]any{"key": map[string]string{"type": "string"}, "plaintext": map[string]string{"type": "string"}}, []string{"key", "plaintext"})},
		{"name": "transit.decrypt", "description": "Transit 암호문을 base64 평문으로 복호화합니다", "inputSchema": objectSchema(map[string]any{"key": map[string]string{"type": "string"}, "ciphertext": map[string]string{"type": "string"}}, []string{"key", "ciphertext"})},
	}
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	if properties == nil {
		properties = map[string]any{}
	}
	schema := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func (s *Server) mcpToolCall(w http.ResponseWriter, r *http.Request, id json.RawMessage, params mcpToolParams) {
	session, _ := sessionFrom(r)
	var result any
	var err error
	switch params.Name {
	case "dashboard.get":
		result, err = s.roleScopedDashboard(r.Context(), session)
	case "secrets.list":
		query, _ := params.Arguments["query"].(string)
		result, err = s.store.ListSecretsAuthorizedFiltered(r.Context(), session.User, query, "", 100, 0)
	case "secrets.metadata":
		path, _ := params.Arguments["path"].(string)
		allowed, accessErr := s.authorizeSecret(r.Context(), session.User, path, "read")
		if accessErr != nil || !allowed {
			err = storeForbidden()
			break
		}
		items, listErr := s.store.ListSecrets(r.Context(), path, 100, 0)
		if listErr != nil {
			err = listErr
			break
		}
		for _, item := range items {
			if item.Path == strings.Trim(path, "/") {
				versions, versionErr := s.store.SecretVersions(r.Context(), item.Path)
				if versionErr != nil {
					err = versionErr
				}
				result = map[string]any{"secret": item, "versions": versions}
				break
			}
		}
		if result == nil && err == nil {
			err = storeNotFound()
		}
	case "policies.list":
		if session.User.Role != "admin" && session.User.Role != "manager" && session.User.Role != "auditor" {
			err = storeForbidden()
			break
		}
		result, err = s.store.ListPolicies(r.Context(), 100, 0)
	case "audit.search":
		if session.User.Role != "admin" && session.User.Role != "manager" && session.User.Role != "auditor" {
			err = storeForbidden()
			break
		}
		query, _ := params.Arguments["query"].(string)
		result, err = s.store.ListAudit(r.Context(), query, "", 100, 0)
	case "access.check":
		path, _ := params.Arguments["path"].(string)
		capability, _ := params.Arguments["capability"].(string)
		result, err = s.store.SimulateAccess(r.Context(), session.User, strings.Trim(path, "/"), capability)
	case "transit.encrypt":
		key, _ := params.Arguments["key"].(string)
		plaintext, _ := params.Arguments["plaintext"].(string)
		allowed, accessErr := s.authorizeTransit(r, session.User, key, "encrypt")
		if accessErr != nil || !allowed {
			err = storeForbidden()
			break
		}
		var ciphertext string
		ciphertext, err = s.encryptTransit(r.Context(), key, plaintext, session.User.ID)
		result = map[string]any{"ciphertext": ciphertext}
	case "transit.decrypt":
		key, _ := params.Arguments["key"].(string)
		ciphertext, _ := params.Arguments["ciphertext"].(string)
		allowed, accessErr := s.authorizeTransit(r, session.User, key, "decrypt")
		if accessErr != nil || !allowed {
			err = storeForbidden()
			if auditErr := s.recordMCPTransitDecrypt(r, session.User, key, false, http.StatusForbidden); auditErr != nil {
				err = auditUnavailable()
			}
			break
		}
		var plaintext string
		plaintext, err = s.decryptTransit(r.Context(), key, ciphertext)
		status := http.StatusOK
		if err != nil {
			status, _ = mcpToolFailure(err)
		}
		if auditErr := s.recordMCPTransitDecrypt(r, session.User, key, err == nil, status); auditErr != nil {
			plaintext = ""
			err = auditUnavailable()
		}
		result = map[string]any{"plaintext": plaintext}
	default:
		mcpError(w, id, -32602, "Unknown tool")
		return
	}
	if err != nil {
		_, message := mcpToolFailure(err)
		mcpResult(w, id, map[string]any{"content": []map[string]any{{"type": "text", "text": message}}, "isError": true})
		return
	}
	mcpToolResult(w, id, result)
}

func (s *Server) authorizeSecret(ctx context.Context, user model.User, path, capability string) (bool, error) {
	if s.secretAuthorizer != nil {
		return s.secretAuthorizer(ctx, user, path, capability)
	}
	return s.store.CanAccess(ctx, user, path, capability)
}

func (s *Server) decryptTransit(ctx context.Context, key, ciphertext string) (string, error) {
	if s.transitDecryptor != nil {
		return s.transitDecryptor(ctx, key, ciphertext)
	}
	return s.store.TransitDecrypt(ctx, key, ciphertext)
}

func (s *Server) recordMCPTransitDecrypt(r *http.Request, user model.User, key string, success bool, status int) error {
	event := model.AuditEvent{
		RequestID:  requestIDFrom(r) + "-mcp-transit-decrypt",
		UserID:     &user.ID,
		Username:   user.Username,
		Action:     "mcp.transit.decrypt",
		Resource:   "transit/" + strings.Trim(key, "/"),
		Method:     r.Method,
		Path:       r.URL.Path,
		StatusCode: status,
		Success:    success,
		RemoteIP:   remoteIP(r),
		UserAgent:  r.UserAgent(),
		Details:    map[string]any{"tool": "transit.decrypt", "key": key},
	}
	if s.auditRecorder != nil {
		return s.auditRecorder(r.Context(), event)
	}
	return s.store.RecordAudit(r.Context(), event)
}

func mcpToolResult(w http.ResponseWriter, id json.RawMessage, result any) {
	encoded, err := json.Marshal(result)
	if err != nil {
		mcpError(w, id, -32603, "Internal error")
		return
	}
	payload := map[string]any{
		"content": []map[string]any{{"type": "text", "text": string(encoded)}},
		"isError": false,
	}
	var structured map[string]any
	if json.Unmarshal(encoded, &structured) == nil && structured != nil {
		payload["structuredContent"] = structured
	}
	mcpResult(w, id, payload)
}

func decodeMCPRequest(w http.ResponseWriter, r *http.Request) (mcpRequest, int, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	var request mcpRequest
	if err := decoder.Decode(&request); err != nil {
		return mcpRequest{}, -32700, errors.New("Parse error")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return mcpRequest{}, -32600, errors.New("Invalid Request")
	}
	return request, 0, nil
}

func validateMCPInitializeParams(raw json.RawMessage) (string, error) {
	params, err := rawObject(raw, true)
	if err != nil {
		return "", err
	}
	protocolVersion, err := rawString(params["protocolVersion"])
	if err != nil || strings.TrimSpace(protocolVersion) == "" {
		return "", errors.New("protocolVersion is required")
	}
	if _, err := rawObject(params["capabilities"], true); err != nil {
		return "", errors.New("capabilities is required")
	}
	clientInfo, err := rawObject(params["clientInfo"], true)
	if err != nil {
		return "", errors.New("clientInfo is required")
	}
	name, nameErr := rawString(clientInfo["name"])
	clientVersion, versionErr := rawString(clientInfo["version"])
	if nameErr != nil || versionErr != nil || strings.TrimSpace(name) == "" || strings.TrimSpace(clientVersion) == "" {
		return "", errors.New("clientInfo name and version are required")
	}
	return protocolVersion, nil
}

func validOptionalObjectParams(raw json.RawMessage) bool {
	if len(bytes.TrimSpace(raw)) == 0 {
		return true
	}
	_, err := rawObject(raw, true)
	return err == nil
}

func validMCPToolsListParams(raw json.RawMessage) bool {
	if len(bytes.TrimSpace(raw)) == 0 {
		return true
	}
	params, err := rawObject(raw, true)
	if err != nil {
		return false
	}
	for key, value := range params {
		if key == "_meta" {
			continue
		}
		if key != "cursor" {
			return false
		}
		if _, err := rawString(value); err != nil {
			return false
		}
	}
	return true
}

func decodeMCPToolParams(raw json.RawMessage) (mcpToolParams, error) {
	params, err := rawObject(raw, true)
	if err != nil {
		return mcpToolParams{}, err
	}
	for key := range params {
		if key != "name" && key != "arguments" && key != "_meta" {
			return mcpToolParams{}, errors.New("unknown tool param")
		}
	}
	name, err := rawString(params["name"])
	if err != nil || strings.TrimSpace(name) == "" {
		return mcpToolParams{}, errors.New("tool name is required")
	}
	arguments := make(map[string]any)
	if rawArguments, ok := params["arguments"]; ok {
		if len(bytes.TrimSpace(rawArguments)) == 0 || bytes.Equal(bytes.TrimSpace(rawArguments), []byte("null")) {
			return mcpToolParams{}, errors.New("arguments must be an object")
		}
		decoder := json.NewDecoder(bytes.NewReader(rawArguments))
		decoder.UseNumber()
		if err := decoder.Decode(&arguments); err != nil {
			return mcpToolParams{}, errors.New("arguments must be an object")
		}
	}
	return mcpToolParams{Name: name, Arguments: arguments}, nil
}

func validateMCPToolArguments(name string, arguments map[string]any) error {
	allowed := map[string]bool{}
	required := map[string]bool{}
	switch name {
	case "dashboard.get", "policies.list":
	case "secrets.list", "audit.search":
		allowed["query"] = true
	case "secrets.metadata":
		allowed["path"], required["path"] = true, true
	case "access.check":
		allowed["path"], allowed["capability"] = true, true
		required["path"], required["capability"] = true, true
	case "transit.encrypt":
		allowed["key"], allowed["plaintext"] = true, true
		required["key"], required["plaintext"] = true, true
	case "transit.decrypt":
		allowed["key"], allowed["ciphertext"] = true, true
		required["key"], required["ciphertext"] = true, true
	default:
		return errors.New("unknown tool")
	}
	for key, value := range arguments {
		if !allowed[key] {
			return errors.New("unknown argument")
		}
		if _, ok := value.(string); !ok {
			return errors.New("argument must be a string")
		}
	}
	for key := range required {
		value, ok := arguments[key].(string)
		if !ok || (key != "plaintext" && strings.TrimSpace(value) == "") {
			return errors.New("required argument is missing")
		}
	}
	if name == "access.check" && !validMCPAccessCapability(arguments["capability"].(string)) {
		return errors.New("unsupported capability")
	}
	return nil
}

func validMCPAccessCapability(value string) bool {
	switch value {
	case "create", "read", "update", "delete", "list", "rotate", "encrypt", "decrypt":
		return true
	default:
		return false
	}
}

func rawObject(raw json.RawMessage, required bool) (map[string]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		if required {
			return nil, errors.New("object is required")
		}
		return map[string]json.RawMessage{}, nil
	}
	var value map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &value); err != nil || value == nil {
		return nil, errors.New("value must be an object")
	}
	return value, nil
}

func rawString(raw json.RawMessage) (string, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return "", errors.New("string is required")
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", errors.New("value must be a string")
	}
	return value, nil
}

func mcpNotification(request mcpRequest) bool {
	return len(bytes.TrimSpace(request.ID)) == 0
}

func validMCPRequestID(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return false
	}
	switch value.(type) {
	case string, json.Number:
		return true
	default:
		return false
	}
}

func validateMCPProtocolHeader(r *http.Request, initialize bool) error {
	value := strings.TrimSpace(r.Header.Get("MCP-Protocol-Version"))
	if value == "" {
		if initialize {
			return nil
		}
		return errors.New("MCP-Protocol-Version header is required")
	}
	if !supportedMCPProtocols[value] {
		return errors.New("unsupported MCP-Protocol-Version")
	}
	return nil
}

func validMCPContentType(r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(r.Header.Get("Content-Type")))
	return err == nil && strings.EqualFold(mediaType, "application/json")
}

func validateMCPAcceptHeader(r *http.Request) error {
	wantsJSON := false
	wantsEvents := false
	for _, item := range strings.Split(strings.Join(r.Header.Values("Accept"), ","), ",") {
		mediaType, params, err := mime.ParseMediaType(strings.TrimSpace(item))
		if err != nil || !acceptedMCPMediaRange(params) {
			continue
		}
		switch strings.ToLower(mediaType) {
		case "application/json":
			wantsJSON = true
		case "text/event-stream":
			wantsEvents = true
		}
	}
	if !wantsJSON || !wantsEvents {
		return errors.New("Accept must include application/json and text/event-stream")
	}
	return nil
}

func acceptedMCPMediaRange(params map[string]string) bool {
	quality, ok := params["q"]
	if !ok {
		return true
	}
	value, err := strconv.ParseFloat(quality, 64)
	return err == nil && value > 0 && value <= 1
}

func validMCPOrigin(r *http.Request) bool {
	value := strings.TrimSpace(r.Header.Get("Origin"))
	if value == "" {
		return true
	}
	origin, err := url.Parse(value)
	if err != nil || origin.User != nil || origin.Host == "" || origin.RawQuery != "" || origin.Fragment != "" ||
		(origin.Path != "" && origin.Path != "/") {
		return false
	}
	scheme := "http"
	if requestIsHTTPS(r) {
		scheme = "https"
	}
	return strings.EqualFold(origin.Scheme, scheme) && strings.EqualFold(origin.Host, r.Host)
}

func mcpResult(w http.ResponseWriter, id json.RawMessage, result any) {
	writeJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "result": result})
}

func mcpError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	mcpHTTPError(w, id, http.StatusOK, code, message)
}

func mcpHTTPError(w http.ResponseWriter, id json.RawMessage, status, code int, message string) {
	var responseID any
	if validMCPRequestID(id) {
		responseID = json.RawMessage(id)
	}
	writeJSON(w, status, map[string]any{"jsonrpc": "2.0", "id": responseID, "error": map[string]any{"code": code, "message": message}})
}

// mcpSentinel is a failure the tool dispatch produced itself, so its message is
// already safe to show an MCP client and its status is already known.
type mcpSentinel struct {
	status  int
	message string
}

func (e mcpSentinel) Error() string { return e.message }
func storeForbidden() error         { return mcpSentinel{http.StatusForbidden, "권한이 없습니다"} }
func storeNotFound() error {
	return mcpSentinel{http.StatusNotFound, "대상을 찾을 수 없습니다"}
}
func auditUnavailable() error {
	return mcpSentinel{http.StatusInternalServerError, "감사 로그를 저장할 수 없어 복호화 결과를 표시하지 않습니다"}
}

// mcpToolFailure maps a tool failure to its status and the text an MCP client
// may see. Only the store sentinels carry a caller-safe reason; anything else
// is a server fault whose driver detail (SQLSTATE, DSN host) must not leave the
// server, so it collapses into one generic message.
func mcpToolFailure(err error) (int, string) {
	var sentinel mcpSentinel
	switch {
	case errors.As(err, &sentinel):
		return sentinel.status, sentinel.message
	case errors.Is(err, store.ErrInvalid):
		return http.StatusBadRequest, err.Error()
	case errors.Is(err, store.ErrNotFound):
		return http.StatusNotFound, store.ErrNotFound.Error()
	case errors.Is(err, store.ErrForbidden), errors.Is(err, store.ErrRequesterMatch):
		return http.StatusForbidden, store.ErrForbidden.Error()
	case errors.Is(err, store.ErrUnauthorized):
		return http.StatusUnauthorized, store.ErrUnauthorized.Error()
	case errors.Is(err, store.ErrConflict):
		return http.StatusConflict, store.ErrConflict.Error()
	default:
		return http.StatusInternalServerError, "도구를 실행할 수 없습니다"
	}
}
