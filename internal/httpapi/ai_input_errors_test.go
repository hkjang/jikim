package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hkjang/jikim/internal/model"
)

// aiRejectionServer wires the real requestID middleware and withAuth in front of
// aiChat with only the session lookup injected. The store stays nil on purpose:
// every input rejection returns before s.store.AIConfig is reached, so this is a
// real round trip through the production handler without a database.
func aiRejectionServer(t *testing.T) *Server {
	t.Helper()
	server := quietServer()
	server.sessionResolver = func(context.Context, string) (model.Session, error) {
		return model.Session{User: model.User{ID: "user-1", Role: "admin", AuthSource: "local"}}, nil
	}
	return server
}

func postAIChat(t *testing.T, server *Server, body map[string]any) (int, map[string]any) {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("요청 본문 직렬화 실패: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/ai/chat", bytes.NewReader(payload))
	request.Header.Set("X-Vault-Token", "hvs.session-token")
	recorder := httptest.NewRecorder()
	handler := server.requestID(server.withAuth(http.HandlerFunc(server.aiChat)))
	handler.ServeHTTP(recorder, request)

	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("응답 JSON 해석 실패: %v (본문 %q)", err, recorder.Body.String())
	}
	errorObject, ok := response["error"].(map[string]any)
	if !ok {
		t.Fatalf("error 객체가 없습니다: %v", response)
	}
	return recorder.Code, errorObject
}

// A rejected AI request must name its own cause. Reporting a malformed request
// as Secret plaintext tells the client and the operator to look for a leak that
// never happened, and hides the five input rules that were actually broken.
func TestAIChatRejectionCodesNameTheirOwnCause(t *testing.T) {
	// Spaces keep the padding away from the long-token Secret matcher, so the
	// size rule is the branch this input actually hits.
	oversized := strings.Repeat("가 ", 70000)
	if len(oversized) <= 262144 {
		t.Fatalf("크기 초과 입력이 한도를 넘지 않습니다: %d 바이트", len(oversized))
	}

	cases := []struct {
		name string
		body map[string]any
		code string
	}{
		{
			name: "empty input",
			body: map[string]any{"prompt": "   "},
			code: "invalid_ai_request",
		},
		{
			name: "disallowed role",
			body: map[string]any{"messages": []map[string]string{{"role": "root", "content": "안녕하세요"}}},
			code: "invalid_ai_request",
		},
		{
			name: "empty message content",
			body: map[string]any{"messages": []map[string]string{{"role": "user", "content": "   "}}},
			code: "invalid_ai_request",
		},
		{
			name: "total size over limit",
			body: map[string]any{"prompt": oversized},
			code: "invalid_ai_request",
		},
		{
			name: "max_tokens out of range",
			body: map[string]any{"prompt": "안녕하세요", "max_tokens": maximumAITokens + 1},
			code: "invalid_ai_request",
		},
		{
			name: "secret plaintext",
			body: map[string]any{"prompt": "password: hunter2000 으로 접속해 주세요"},
			code: "secret_material_rejected",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			server := aiRejectionServer(t)
			status, errorObject := postAIChat(t, server, testCase.body)
			if status != http.StatusBadRequest {
				t.Fatalf("status=%d, want %d", status, http.StatusBadRequest)
			}
			if errorObject["code"] != testCase.code {
				t.Fatalf("code=%v, want %q (message %v)", errorObject["code"], testCase.code, errorObject["message"])
			}
			if requestID, _ := errorObject["request_id"].(string); requestID == "" {
				t.Fatalf("request_id가 비어 있습니다: %v", errorObject)
			}
		})
	}
}

// The Secret branch is the one an operator may have alerting on, so its code and
// message have to stay byte-for-byte what they were before the split.
func TestAIChatSecretRejectionMessageIsUnchanged(t *testing.T) {
	server := aiRejectionServer(t)
	status, errorObject := postAIChat(t, server, map[string]any{
		"messages": []map[string]string{{"role": "user", "content": "-----BEGIN RSA PRIVATE KEY-----"}},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("status=%d, want %d", status, http.StatusBadRequest)
	}
	if errorObject["code"] != "secret_material_rejected" {
		t.Fatalf("code=%v, want \"secret_material_rejected\"", errorObject["code"])
	}
	if errorObject["message"] != "Secret 평문으로 보이는 내용은 AI에 전달할 수 없습니다" {
		t.Fatalf("message=%q 가 기존 문구와 다릅니다", errorObject["message"])
	}
}
