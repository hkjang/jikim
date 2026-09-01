package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/hkjang/jikim/internal/model"
	"github.com/hkjang/jikim/internal/version"
)

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func (s *Server) mcp(w http.ResponseWriter, r *http.Request) {
	var request mcpRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if request.JSONRPC != "2.0" || request.Method == "" {
		mcpError(w, request.ID, -32600, "Invalid Request")
		return
	}
	switch request.Method {
	case "initialize":
		mcpResult(w, request.ID, map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": "jikim", "version": version.Version},
		})
	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
	case "ping":
		mcpResult(w, request.ID, map[string]any{})
	case "tools/list":
		mcpResult(w, request.ID, map[string]any{"tools": mcpTools()})
	case "tools/call":
		s.mcpToolCall(w, r, request)
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
		{"name": "transit.encrypt", "description": "base64 평문을 이름 있는 Transit 키로 암호화합니다", "inputSchema": objectSchema(map[string]any{"key": map[string]string{"type": "string"}, "plaintext": map[string]string{"type": "string"}}, []string{"key", "plaintext"})},
		{"name": "transit.decrypt", "description": "Transit 암호문을 base64 평문으로 복호화합니다", "inputSchema": objectSchema(map[string]any{"key": map[string]string{"type": "string"}, "ciphertext": map[string]string{"type": "string"}}, []string{"key", "ciphertext"})},
	}
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	if properties == nil {
		properties = map[string]any{}
	}
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}

func (s *Server) mcpToolCall(w http.ResponseWriter, r *http.Request, request mcpRequest) {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(request.Params, &params); err != nil {
		mcpError(w, request.ID, -32602, "Invalid params")
		return
	}
	session, _ := sessionFrom(r)
	var result any
	var err error
	switch params.Name {
	case "dashboard.get":
		result, err = s.store.Dashboard(r.Context())
		if err == nil {
			result = dashboardForRole(result.(model.Dashboard), session.User.Role)
		}
	case "secrets.list":
		query, _ := params.Arguments["query"].(string)
		items, listErr := s.store.ListSecrets(r.Context(), query, 100, 0)
		if listErr != nil {
			err = listErr
			break
		}
		filtered := items[:0]
		for _, item := range items {
			allowed, accessErr := s.store.CanAccess(r.Context(), session.User, item.Path, "list")
			if accessErr != nil {
				err = accessErr
				break
			}
			if allowed {
				filtered = append(filtered, item)
			}
		}
		result = filtered
	case "secrets.metadata":
		path, _ := params.Arguments["path"].(string)
		allowed, accessErr := s.store.CanAccess(r.Context(), session.User, path, "list")
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
	case "transit.encrypt":
		key, _ := params.Arguments["key"].(string)
		plaintext, _ := params.Arguments["plaintext"].(string)
		allowed, accessErr := s.authorizeTransit(r, session.User, key, "encrypt")
		if accessErr != nil || !allowed {
			err = storeForbidden()
			break
		}
		var ciphertext string
		ciphertext, err = s.store.TransitEncrypt(r.Context(), key, plaintext, session.User.ID)
		result = map[string]any{"ciphertext": ciphertext}
	case "transit.decrypt":
		key, _ := params.Arguments["key"].(string)
		ciphertext, _ := params.Arguments["ciphertext"].(string)
		allowed, accessErr := s.authorizeTransit(r, session.User, key, "decrypt")
		if accessErr != nil || !allowed {
			err = storeForbidden()
			break
		}
		var plaintext string
		plaintext, err = s.store.TransitDecrypt(r.Context(), key, ciphertext)
		result = map[string]any{"plaintext": plaintext}
	default:
		mcpError(w, request.ID, -32602, "Unknown tool")
		return
	}
	if err != nil {
		mcpResult(w, request.ID, map[string]any{"content": []map[string]any{{"type": "text", "text": err.Error()}}, "isError": true})
		return
	}
	encoded, _ := json.Marshal(result)
	mcpResult(w, request.ID, map[string]any{"content": []map[string]any{{"type": "text", "text": string(encoded)}}, "structuredContent": result, "isError": false})
}

func mcpResult(w http.ResponseWriter, id json.RawMessage, result any) {
	writeJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "result": result})
}

func mcpError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	writeJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "error": map[string]any{"code": code, "message": message}})
}

type mcpSentinel string

func (e mcpSentinel) Error() string { return string(e) }
func storeForbidden() error         { return mcpSentinel("권한이 없습니다") }
func storeNotFound() error          { return mcpSentinel("대상을 찾을 수 없습니다") }

var _ model.User
