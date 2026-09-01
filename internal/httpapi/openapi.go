package httpapi

import (
	"net/http"
	"strings"

	"github.com/hkjang/jikim/internal/version"
)

func (s *Server) capabilities(w http.ResponseWriter, _ *http.Request) {
	writeData(w, http.StatusOK, map[string]any{
		"service": "jikim", "version": version.Version,
		"api": map[string]any{
			"management": "/api/v1", "openapi": "/api/openapi.json",
			"openbao": map[string]any{
				"base": "/v1", "target": "OpenBao 2.6 KV v2 compatibility subset",
				"complete": false,
				"features": []string{"sys.health", "token.lookup-self", "kv-v2.data", "kv-v2.metadata", "kv-v2.cas", "kv-v2.delete", "kv-v2.undelete", "kv-v2.destroy", "transit.encrypt", "transit.decrypt"},
			},
		},
		"mcp": map[string]any{
			"endpoint": "/mcp", "transport": "streamable-http-stateless",
			"protocol_versions": []string{"2025-11-25", "2025-06-18"},
			"tools": []string{"dashboard.get", "secrets.list", "secrets.metadata", "policies.list",
				"audit.search", "access.check", "transit.encrypt", "transit.decrypt"},
		},
		"integrations": map[string]any{
			"oidc":    map[string]any{"provider": "Keycloak/OIDC discovery", "rp_initiated_logout": true},
			"ai":      map[string]any{"profile": "OpenAI Chat Completions SSE", "streaming": true, "max_tokens": maximumAITokens},
			"webhook": map[string]any{"delivery": true, "signing": "HMAC-SHA256", "history": true, "manual_retry": true},
		},
	})
}

func (s *Server) openAPI(w http.ResponseWriter, _ *http.Request) {
	paths := map[string]any{}
	add := func(path, method, summary string, secured bool) {
		item, _ := paths[path].(map[string]any)
		if item == nil {
			item = map[string]any{}
			paths[path] = item
		}
		operation := map[string]any{"summary": summary, "responses": map[string]any{
			"200": map[string]any{"description": "성공"},
			"400": map[string]any{"$ref": "#/components/responses/Error"},
			"401": map[string]any{"$ref": "#/components/responses/Error"},
			"403": map[string]any{"$ref": "#/components/responses/Error"},
		}}
		if secured {
			operation["security"] = []any{map[string]any{"cookieAuth": []string{}}, map[string]any{"bearerAuth": []string{}}}
		}
		for remaining := path; ; {
			start := strings.IndexByte(remaining, '{')
			if start < 0 {
				break
			}
			end := strings.IndexByte(remaining[start:], '}')
			if end < 0 {
				break
			}
			name := remaining[start+1 : start+end]
			operation["parameters"] = append(operationParameters(operation), map[string]any{
				"name": name, "in": "path", "required": true,
				"schema": map[string]any{"type": "string", "minLength": 1},
			})
			remaining = remaining[start+end+1:]
		}
		if method == "get" && strings.HasPrefix(path, "/v1/secret/metadata") {
			operation["parameters"] = append(operationParameters(operation), map[string]any{
				"name": "list", "in": "query", "required": false,
				"description": "OpenBao LIST 메서드의 GET fallback. true 또는 1",
				"schema":      map[string]any{"type": "string", "enum": []string{"true", "1"}},
			})
			operation["x-openbao-list-method"] = true
		}
		item[method] = operation
	}
	for _, endpoint := range []struct {
		path, method, summary string
		secured               bool
	}{
		{"/healthz", "get", "Liveness 확인", false},
		{"/readyz", "get", "Readiness 확인", false},
		{"/api/openapi.json", "get", "OpenAPI 3.1 계약", false},
		{"/api/v1/version", "get", "서비스 버전", false},
		{"/api/v1/capabilities", "get", "지원 API와 연동 프로필", false},
		{"/api/v1/settings/public", "get", "로그인용 공개 설정", false},
		{"/api/v1/session", "get", "현재 세션 상태", false},
		{"/api/v1/auth/login", "post", "로컬 로그인", false},
		{"/api/v1/auth/logout", "post", "세션 로그아웃", true},
		{"/api/v1/oidc/config", "get", "OIDC 공개 설정", false},
		{"/api/v1/oidc/login", "get", "OIDC 인증 시작", false},
		{"/api/v1/oidc/callback", "get", "OIDC callback", false},
		{"/api/v1/oidc/exchange", "post", "OIDC 일회용 코드 교환", false},
		{"/api/v1/oidc/logout", "get", "OIDC RP 로그아웃", true},
		{"/api/v1/oidc/test", "post", "OIDC discovery 연결 테스트", true},
		{"/api/v1/me", "get", "내 프로필", true},
		{"/api/v1/me", "patch", "내 프로필 변경", true},
		{"/api/v1/me/password", "post", "내 비밀번호 변경", true},
		{"/api/v1/dashboard", "get", "역할별 운영 지표", true},
		{"/api/v1/secrets", "get", "권한별 Secret 목록", true},
		{"/api/v1/secrets", "post", "Secret 생성", true},
		{"/api/v1/secrets/{id}", "get", "Secret 메타데이터", true},
		{"/api/v1/secrets/{id}", "put", "Secret 변경", true},
		{"/api/v1/secrets/{id}", "patch", "Secret 부분 변경", true},
		{"/api/v1/secrets/{id}", "delete", "Secret 격리", true},
		{"/api/v1/secrets/{id}/versions", "get", "Secret 버전", true},
		{"/api/v1/secrets/{id}/reveal", "post", "감사 필수 Secret 값 조회", true},
		{"/api/v1/secrets/{id}/rotate", "post", "Secret 회전", true},
		{"/api/v1/applications", "get", "애플리케이션 목록", true},
		{"/api/v1/applications", "post", "애플리케이션 생성", true},
		{"/api/v1/applications/{id}", "get", "애플리케이션 상세", true},
		{"/api/v1/applications/{id}", "put", "애플리케이션 변경", true},
		{"/api/v1/applications/{id}", "patch", "애플리케이션 부분 변경", true},
		{"/api/v1/applications/{id}", "delete", "애플리케이션 삭제", true},
		{"/api/v1/users", "get", "사용자 목록", true},
		{"/api/v1/users", "post", "사용자 생성", true},
		{"/api/v1/users/{id}", "get", "사용자 상세", true},
		{"/api/v1/users/{id}", "patch", "사용자 변경", true},
		{"/api/v1/users/{id}", "delete", "사용자 삭제", true},
		{"/api/v1/policies", "get", "정책 목록", true},
		{"/api/v1/policies", "post", "정책 생성", true},
		{"/api/v1/policies/{id}", "get", "정책 상세", true},
		{"/api/v1/policies/{id}", "put", "정책 변경", true},
		{"/api/v1/policies/{id}", "patch", "정책 부분 변경", true},
		{"/api/v1/policies/{id}", "delete", "정책 삭제", true},
		{"/api/v1/policies/{id}/users", "get", "정책 사용자 목록", true},
		{"/api/v1/policies/{id}/users", "put", "정책 사용자 할당", true},
		{"/api/v1/policies/simulate", "post", "정책 결과 시뮬레이션", true},
		{"/api/v1/keys", "get", "키 목록", true},
		{"/api/v1/keys", "post", "키 생성", true},
		{"/api/v1/keys/{id}/rotate", "post", "키 회전", true},
		{"/api/v1/keys/{id}/permissions", "patch", "키 권한 변경", true},
		{"/api/v1/tokens", "get", "토큰 목록", true},
		{"/api/v1/tokens", "post", "토큰 생성", true},
		{"/api/v1/tokens/{id}/revoke", "post", "토큰 폐기", true},
		{"/api/v1/auth-methods", "get", "인증 방식 목록", true},
		{"/api/v1/approvals", "get", "승인 요청", true},
		{"/api/v1/approvals/{id}/approve", "post", "요청 승인", true},
		{"/api/v1/approvals/{id}/reject", "post", "요청 반려", true},
		{"/api/v1/audit", "get", "감사 이벤트", true},
		{"/api/v1/settings", "get", "관리 설정", true},
		{"/api/v1/settings", "patch", "관리 설정 변경", true},
		{"/api/v1/ai/chat", "post", "AI SSE 대화", true},
		{"/api/v1/integrations/ai/test", "post", "AI SSE 연결 테스트", true},
		{"/api/v1/integrations/webhook/test", "post", "서명 Webhook 테스트", true},
		{"/api/v1/integrations/webhook/deliveries", "get", "Webhook 전송 이력", true},
		{"/api/v1/integrations/webhook/deliveries/{id}/retry", "post", "Webhook 재전송", true},
		{"/mcp", "get", "MCP stateless endpoint의 405 계약", false},
		{"/mcp", "post", "MCP JSON-RPC", true},
		{"/v1/sys/health", "get", "OpenBao health 호환", false},
		{"/v1/auth/userpass/login/{username}", "post", "OpenBao Userpass 로그인", false},
		{"/v1/auth/userpass/login/{username}", "put", "OpenBao Userpass 로그인", false},
		{"/v1/auth/token/lookup-self", "get", "OpenBao Token self 조회", true},
		{"/v1/auth/token/create", "post", "OpenBao Token 생성", true},
		{"/v1/auth/token/create", "put", "OpenBao Token 생성", true},
		{"/v1/auth/token/revoke-self", "post", "OpenBao Token self 폐기", true},
		{"/v1/auth/token/revoke-self", "put", "OpenBao Token self 폐기", true},
		{"/v1/secret/data/{path}", "get", "OpenBao KV v2 읽기", true},
		{"/v1/secret/data/{path}", "post", "OpenBao KV v2 CAS 쓰기", true},
		{"/v1/secret/data/{path}", "put", "OpenBao KV v2 CAS 쓰기", true},
		{"/v1/secret/data/{path}", "delete", "OpenBao KV v2 최신 버전 soft delete", true},
		{"/v1/secret/delete/{path}", "post", "OpenBao KV v2 버전 soft delete", true},
		{"/v1/secret/undelete/{path}", "post", "OpenBao KV v2 버전 복원", true},
		{"/v1/secret/destroy/{path}", "put", "OpenBao KV v2 버전 영구 파기", true},
		{"/v1/secret/metadata/{path}", "get", "OpenBao KV v2 metadata", true},
		{"/v1/secret/metadata/{path}", "delete", "OpenBao KV v2 metadata 영구 삭제", true},
		{"/v1/secret/metadata", "get", "OpenBao KV v2 루트 목록(list=true)", true},
		{"/v1/transit/encrypt/{key}", "post", "OpenBao Transit encrypt", true},
		{"/v1/transit/encrypt/{key}", "put", "OpenBao Transit encrypt", true},
		{"/v1/transit/decrypt/{key}", "post", "OpenBao Transit decrypt", true},
		{"/v1/transit/decrypt/{key}", "put", "OpenBao Transit decrypt", true},
	} {
		add(endpoint.path, endpoint.method, endpoint.summary, endpoint.secured)
	}
	// OpenBao-compatible operations authenticate with X-Vault-Token. They do
	// not inherit the management API cookie/Bearer profile.
	for path, raw := range paths {
		if path == "/v1/sys/health" || path == "/v1/auth/userpass/login/{username}" || len(path) < 4 || path[:4] != "/v1/" {
			continue
		}
		for _, operation := range raw.(map[string]any) {
			operation.(map[string]any)["security"] = []any{map[string]any{"vaultToken": []string{}}}
		}
	}
	if item, ok := paths["/mcp"].(map[string]any); ok {
		if operation, ok := item["get"].(map[string]any); ok {
			operation["responses"] = map[string]any{"405": map[string]any{"description": "POST만 지원"}}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{"title": "jikim API", "version": version.Version,
			"description": "jikim 관리 API, MCP, OpenBao 호환 하위 집합의 기계 판독 계약입니다."},
		"servers": []any{map[string]any{"url": "/"}},
		"paths":   paths,
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"cookieAuth": map[string]any{"type": "apiKey", "in": "cookie", "name": "jikim_session"},
				"bearerAuth": map[string]any{"type": "http", "scheme": "bearer"},
				"vaultToken": map[string]any{"type": "apiKey", "in": "header", "name": "X-Vault-Token"},
			},
			"responses": map[string]any{"Error": map[string]any{"description": "오류 응답"}},
		},
	})
}

func operationParameters(operation map[string]any) []any {
	parameters, _ := operation["parameters"].([]any)
	return parameters
}
