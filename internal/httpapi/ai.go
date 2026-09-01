package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/hkjang/jikim/internal/model"
)

const maximumAITokens = 262144

type aiChatInput struct {
	Prompt    string          `json:"prompt,omitempty"`
	Messages  []aiMessage     `json:"messages,omitempty"`
	MaxTokens int             `json:"max_tokens,omitempty"`
	Context   json.RawMessage `json:"context,omitempty"` // accepted for compatibility; never forwarded
}

type aiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type aiContext struct {
	RequesterRole   string           `json:"requester_role,omitempty"`
	Inventory       map[string]int64 `json:"inventory,omitempty"`
	RiskSummary     map[string]int64 `json:"risk_summary,omitempty"`
	WorkflowSummary map[string]int64 `json:"workflow_summary,omitempty"`
}

var secretMaterialPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(password|passwd|secret|api[_-]?key|access[_-]?token|private[_-]?key)\s*[:=]\s*[^\s,;]{4,}`),
	regexp.MustCompile(`(?i)["']?(password|passwd|secret|api[_-]?key|access[_-]?token|private[_-]?key)["']?\s*:\s*["'][^"']{4,}["']`),
	regexp.MustCompile(`-----BEGIN (RSA |EC |OPENSSH )?PRIVATE KEY-----`),
	regexp.MustCompile(`\b(jks|hvs|vault):?[._-][A-Za-z0-9_-]{12,}`),
	regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~-]{12,}`),
	regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}\b`),
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`),
	regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://[^\s/:]+:[^\s/@]+@`),
	regexp.MustCompile(`\b[A-Za-z0-9+/_=-]{48,}\b`),
}

func ValidateAIInput(input aiChatInput) error {
	input.Prompt = strings.TrimSpace(input.Prompt)
	if input.Prompt == "" && len(input.Messages) == 0 {
		return errors.New("prompt 또는 messages가 필요합니다")
	}
	total := len(input.Prompt)
	combined := input.Prompt
	for _, message := range input.Messages {
		switch message.Role {
		case "user", "assistant", "system":
		default:
			return errors.New("messages role은 user, assistant, system만 허용됩니다")
		}
		if strings.TrimSpace(message.Content) == "" {
			return errors.New("빈 message는 허용되지 않습니다")
		}
		total += len(message.Content)
		combined += "\n" + message.Content
	}
	if total > 262144 {
		return errors.New("AI 요청 내용은 총 262144바이트 이하여야 합니다")
	}
	for _, pattern := range secretMaterialPatterns {
		if pattern.MatchString(combined) {
			return errors.New("Secret 평문으로 보이는 내용은 AI에 전달할 수 없습니다")
		}
	}
	if input.MaxTokens < 0 || input.MaxTokens > maximumAITokens {
		return fmt.Errorf("max_tokens는 최대 %d입니다", maximumAITokens)
	}
	return nil
}

func (s *Server) aiChat(w http.ResponseWriter, r *http.Request) {
	var input aiChatInput
	if !decodeJSON(w, r, &input) {
		return
	}
	if err := ValidateAIInput(input); err != nil {
		writeError(w, r, http.StatusBadRequest, "secret_material_rejected", err.Error())
		return
	}
	cfg, err := s.store.AIConfig(r.Context())
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	if !cfg.Enabled || strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.Model) == "" {
		writeError(w, r, http.StatusServiceUnavailable, "ai_disabled", "AI 기능이 설정되지 않았습니다")
		return
	}
	maxTokens := input.MaxTokens
	if maxTokens == 0 {
		maxTokens = cfg.MaxTokens
	}
	if maxTokens > cfg.MaxTokens {
		maxTokens = cfg.MaxTokens
	}
	if maxTokens > maximumAITokens {
		maxTokens = maximumAITokens
	}
	session, _ := sessionFrom(r)
	dashboard, err := s.store.Dashboard(r.Context())
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	contextJSON, _ := json.Marshal(serverAIContext(session.User.Role, dashboard))
	systemPrompt := "당신은 jikim 보안 운영 도우미입니다. 제공되는 정보는 메타데이터뿐입니다. 비밀값을 요청하거나 추측하지 말고 한국어로 답하세요."
	messages := []map[string]string{{"role": "system", "content": systemPrompt}}
	for _, message := range input.Messages {
		role := message.Role
		if role == "system" {
			role = "user"
		}
		messages = append(messages, map[string]string{"role": role, "content": message.Content})
	}
	if input.Prompt != "" {
		messages = append(messages, map[string]string{"role": "user", "content": input.Prompt})
	}
	messages = append(messages, map[string]string{"role": "user", "content": "메타데이터 컨텍스트: " + string(contextJSON)})
	payload := map[string]any{
		"model": cfg.Model, "stream": true, "max_tokens": maxTokens, "temperature": cfg.Temperature,
		"messages": messages,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	endpoint, err := chatCompletionsURL(cfg.BaseURL)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_ai_config", "AI API URL 설정이 올바르지 않습니다")
		return
	}
	upstreamCtx, cancel := context.WithTimeout(r.Context(), time.Duration(cfg.TimeoutSeconds)*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(upstreamCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")
	if cfg.APIKey != "" {
		request.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	response, err := s.aiClient.Do(request)
	if err != nil {
		writeError(w, r, http.StatusBadGateway, "ai_upstream_error", "AI API에 연결할 수 없습니다")
		return
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		writeError(w, r, http.StatusBadGateway, "ai_upstream_error", "AI API가 요청을 거부했습니다")
		return
	}
	if !strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream") {
		writeError(w, r, http.StatusBadGateway, "ai_invalid_stream", "AI API가 SSE 스트림을 반환하지 않았습니다")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, r, http.StatusInternalServerError, "stream_unsupported", "스트리밍을 지원할 수 없습니다")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	reader := bufio.NewReader(response.Body)
	buffer := make([]byte, 32<<10)
	for {
		n, readErr := reader.Read(buffer)
		if n > 0 {
			if _, writeErr := w.Write(buffer[:n]); writeErr != nil {
				return
			}
			flusher.Flush()
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				s.logger.Warn("AI SSE stream interrupted", "error", readErr, "request_id", requestIDFrom(r))
			}
			return
		}
	}
}

func serverAIContext(role string, dashboard model.Dashboard) aiContext {
	return aiContext{
		RequesterRole: role,
		Inventory: map[string]int64{
			"secrets": dashboard.Secrets, "applications": dashboard.Applications,
			"users": dashboard.Users, "policies": dashboard.Policies, "keys": dashboard.Keys,
		},
		RiskSummary:     map[string]int64{"high_risk_secrets": dashboard.HighRiskSecrets},
		WorkflowSummary: map[string]int64{"pending_approvals": dashboard.PendingApprovals},
	}
}

func chatCompletionsURL(base string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(base))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", errors.New("invalid base url")
	}
	path := strings.TrimRight(parsed.Path, "/")
	if strings.HasSuffix(path, "/chat/completions") {
		parsed.Path = path
	} else if strings.HasSuffix(path, "/v1") {
		parsed.Path = path + "/chat/completions"
	} else {
		parsed.Path = path + "/v1/chat/completions"
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}
