package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hkjang/jikim/internal/store"
)

func TestWebhookSignatureUsesTimestampAndBody(t *testing.T) {
	first := webhookSignature("01234567890123456789012345678901", "1700000000", []byte(`{"event":"secret.rotated"}`))
	if first != "sha256=fe1edb4066e10ba1db327adf23217271f1a43c6566ba03e99b22aa5cef8bb44d" {
		t.Fatalf("unexpected signature: %s", first)
	}
	if second := webhookSignature("01234567890123456789012345678901", "1700000001", []byte(`{"event":"secret.rotated"}`)); second == first {
		t.Fatal("timestamp was not covered by the signature")
	}
}

// webhookServer wires a Server the way New does for the outbound path — the
// production HTTP client and the real deliverWebhook — with the store reached
// through the seams so the handlers run without a database.
func webhookServer(t *testing.T, endpointURL string, completeErr error) (*Server, *[]error) {
	t.Helper()
	server := quietServer()
	server.httpClient = newOutboundHTTPClient(slog.New(slog.NewTextHandler(io.Discard, nil)), 5*time.Second)
	server.webhookConfigLoader = func(context.Context) (store.WebhookConfig, error) {
		return store.WebhookConfig{Enabled: true, URL: endpointURL,
			SigningSecret: "01234567890123456789012345678901",
			Events:        map[string]bool{"integration.test": true}}, nil
	}
	server.webhookCreator = func(_ context.Context, id, eventType, resource string,
		actorID *string, requestID string, payload any) (store.WebhookDelivery, error) {
		raw, err := json.Marshal(payload)
		if err != nil {
			return store.WebhookDelivery{}, err
		}
		return store.WebhookDelivery{ID: id, EventType: eventType, Resource: resource,
			ActorID: actorID, RequestID: requestID, Status: "pending", Payload: raw}, nil
	}
	recorded := new([]error)
	server.webhookCompleter = func(_ context.Context, _ string, _ int, deliveryErr error) error {
		*recorded = append(*recorded, deliveryErr)
		return completeErr
	}
	return server, recorded
}

// queueWebhook은 자리가 있으면 goroutine으로 전송하므로 기록 호출과 로그는
// 테스트 goroutine과 동시에 일어난다. 채널과 뮤텍스로 받아 -race에서도 안전하게 읽는다.
type webhookCompletion struct {
	id         string
	statusCode int
	err        error
}

func webhookCompletions(server *Server, completeErr error) chan webhookCompletion {
	calls := make(chan webhookCompletion, 4)
	server.webhookCompleter = func(_ context.Context, id string, statusCode int, deliveryErr error) error {
		calls <- webhookCompletion{id: id, statusCode: statusCode, err: deliveryErr}
		return completeErr
	}
	return calls
}

func awaitWebhookCompletion(t *testing.T, calls chan webhookCompletion) webhookCompletion {
	t.Helper()
	select {
	case call := <-calls:
		return call
	case <-time.After(5 * time.Second):
		t.Fatal("delivery 기록이 호출되지 않았습니다")
		return webhookCompletion{}
	}
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func captureWebhookLog(server *Server) *syncBuffer {
	logs := &syncBuffer{}
	server.logger = slog.New(slog.NewJSONHandler(logs, nil))
	return logs
}

func webhookWarnings(t *testing.T, logs *syncBuffer) []map[string]any {
	t.Helper()
	var records []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("로그를 읽지 못했습니다: %v (%s)", err, line)
		}
		records = append(records, record)
	}
	return records
}

func decodeWebhookResult(t *testing.T, body string) map[string]any {
	t.Helper()
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		t.Fatalf("응답을 읽지 못했습니다: %v (%s)", err, body)
	}
	return envelope.Data
}

// A delivery the endpoint accepted stays a success even when jikim cannot
// record it. Reporting the storage outage as an endpoint rejection sends the
// administrator to fix a URL that is working.
func TestWebhookTestSucceedsWhenOnlyRecordingFails(t *testing.T) {
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Jikim-Signature-256") == "" {
			t.Error("서명 헤더가 없습니다")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer endpoint.Close()
	server, recorded := webhookServer(t, endpoint.URL, driverFailure)

	response := httptest.NewRecorder()
	server.webhookTest(response, httptest.NewRequest("POST", "/api/v1/integrations/webhook/test", nil))

	result := decodeWebhookResult(t, response.Body.String())
	if result["ok"] != true {
		t.Fatalf("기록 실패가 전송 실패로 보고되었습니다: %s", response.Body.String())
	}
	if result["status_code"] != float64(http.StatusOK) {
		t.Fatalf("status_code=%v", result["status_code"])
	}
	if result["message"] != "서명된 테스트 이벤트를 전송했습니다" {
		t.Fatalf("message=%v", result["message"])
	}
	if strings.Contains(response.Body.String(), "postgres.internal") || strings.Contains(response.Body.String(), "SQLSTATE") {
		t.Fatalf("드라이버 원문이 응답에 새어 나왔습니다: %s", response.Body.String())
	}
	if len(*recorded) != 1 || (*recorded)[0] != nil {
		t.Fatalf("기록에 남긴 delivery 오류=%v", *recorded)
	}
}

// When the endpoint rejects, the storage failure must not overwrite the
// delivery error: the administrator still needs to see the 500.
func TestWebhookTestReportsEndpointRejectionWhenRecordingAlsoFails(t *testing.T) {
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer endpoint.Close()
	server, recorded := webhookServer(t, endpoint.URL, driverFailure)

	response := httptest.NewRecorder()
	server.webhookTest(response, httptest.NewRequest("POST", "/api/v1/integrations/webhook/test", nil))

	result := decodeWebhookResult(t, response.Body.String())
	if result["ok"] != false {
		t.Fatalf("거절한 엔드포인트가 성공으로 보고되었습니다: %s", response.Body.String())
	}
	if result["status_code"] != float64(http.StatusInternalServerError) {
		t.Fatalf("status_code=%v", result["status_code"])
	}
	if result["message"] != "Webhook endpoint가 요청을 수락하지 않았습니다" {
		t.Fatalf("message=%v", result["message"])
	}
	if len(*recorded) != 1 || (*recorded)[0] == nil ||
		!strings.Contains((*recorded)[0].Error(), "webhook 응답 상태 500") {
		t.Fatalf("기록에 남긴 delivery 오류=%v", *recorded)
	}
}

func TestDeliverWebhookKeepsTransportErrorWhenRecordingFails(t *testing.T) {
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer endpoint.Close()
	server, _ := webhookServer(t, endpoint.URL, driverFailure)
	cfg, err := server.webhookConfig(context.Background())
	if err != nil {
		t.Fatalf("설정을 읽지 못했습니다: %v", err)
	}
	delivery, err := server.createWebhookDelivery(context.Background(), "delivery-1", "integration.test",
		"integrations/webhook", nil, "request-1", map[string]any{"message": "t"})
	if err != nil {
		t.Fatalf("delivery 생성 실패: %v", err)
	}

	statusCode, deliveryErr := server.deliverWebhook(context.Background(), cfg, delivery)

	if statusCode != http.StatusInternalServerError {
		t.Fatalf("status=%d", statusCode)
	}
	if deliveryErr == nil || !strings.Contains(deliveryErr.Error(), "webhook 응답 상태 500") {
		t.Fatalf("저장소 오류가 전송 오류를 덮어썼습니다: %v", deliveryErr)
	}
	if errors.Is(deliveryErr, driverFailure) {
		t.Fatalf("저장소 오류가 반환되었습니다: %v", deliveryErr)
	}
}

// A connection that never reached the endpoint keeps reporting itself as a
// delivery failure with no status code.
func TestDeliverWebhookReportsUnreachableEndpoint(t *testing.T) {
	endpoint := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	closedURL := endpoint.URL
	endpoint.Close()
	server, recorded := webhookServer(t, closedURL, nil)
	cfg, err := server.webhookConfig(context.Background())
	if err != nil {
		t.Fatalf("설정을 읽지 못했습니다: %v", err)
	}
	delivery, err := server.createWebhookDelivery(context.Background(), "delivery-2", "integration.test",
		"integrations/webhook", nil, "request-2", map[string]any{"message": "t"})
	if err != nil {
		t.Fatalf("delivery 생성 실패: %v", err)
	}

	statusCode, deliveryErr := server.deliverWebhook(context.Background(), cfg, delivery)

	if statusCode != 0 || deliveryErr == nil {
		t.Fatalf("status=%d err=%v", statusCode, deliveryErr)
	}
	if len(*recorded) != 1 || (*recorded)[0] == nil {
		t.Fatalf("전송 실패가 기록되지 않았습니다: %v", *recorded)
	}
}

// 대기열이 가득 차면 전송은 포기하지만 delivery 행은 실패로 닫아야 한다.
// 그 기록마저 실패하면 행은 pending으로 영원히 남으므로, 관리자가 원인을
// 찾을 수 있도록 경고 한 줄은 반드시 남아야 한다.
func TestQueueWebhookLogsWhenSaturatedRecordingFails(t *testing.T) {
	var requests atomic.Int64
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer endpoint.Close()
	server, _ := webhookServer(t, endpoint.URL, nil)
	logs := captureWebhookLog(server)
	calls := webhookCompletions(server, driverFailure)
	server.webhookSlots = make(chan struct{}, 1)
	server.webhookSlots <- struct{}{}

	request := httptest.NewRequest("POST", "/api/v1/secrets", nil)
	request = request.WithContext(context.WithValue(request.Context(), requestIDKey, "request-saturated"))
	server.queueWebhook(request, "integration.test", "secrets/db", map[string]any{"name": "db"})

	call := awaitWebhookCompletion(t, calls)
	if call.statusCode != 0 {
		t.Fatalf("status_code=%d", call.statusCode)
	}
	if call.err == nil || !strings.Contains(call.err.Error(), "대기열이 가득 찼습니다") {
		t.Fatalf("기록에 남긴 delivery 오류=%v", call.err)
	}
	if len(calls) != 0 {
		t.Fatalf("기록이 두 번 이상 호출되었습니다: %d", len(calls)+1)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("포화 상태에서 엔드포인트로 요청이 갔습니다: %d", got)
	}

	records := webhookWarnings(t, logs)
	if len(records) != 1 {
		t.Fatalf("경고가 한 줄이 아닙니다: %v", records)
	}
	record := records[0]
	if record["level"] != "WARN" {
		t.Fatalf("level=%v", record["level"])
	}
	if record["delivery_id"] != call.id || record["event"] != "integration.test" ||
		record["request_id"] != "request-saturated" || record["error"] == nil {
		t.Fatalf("경고 필드가 부족합니다: %v", record)
	}
	if body := logs.String(); strings.Contains(body, "payload") ||
		strings.Contains(body, "01234567890123456789012345678901") || strings.Contains(body, `"db"`) {
		t.Fatalf("로그에 원문이 새어 나왔습니다: %s", body)
	}
}

// 자리가 있으면 기존 동작 그대로 — 엔드포인트가 서명된 요청을 받고 포화 경고는 없다.
func TestQueueWebhookDeliversWhenQueueHasRoom(t *testing.T) {
	received := make(chan string, 4)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- r.Header.Get("X-Jikim-Event")
		w.WriteHeader(http.StatusOK)
	}))
	defer endpoint.Close()
	server, _ := webhookServer(t, endpoint.URL, nil)
	logs := captureWebhookLog(server)
	calls := webhookCompletions(server, nil)
	server.webhookSlots = make(chan struct{}, 16)

	request := httptest.NewRequest("POST", "/api/v1/secrets", nil)
	request = request.WithContext(context.WithValue(request.Context(), requestIDKey, "request-room"))
	server.queueWebhook(request, "integration.test", "secrets/db", map[string]any{"name": "db"})

	select {
	case event := <-received:
		if event != "integration.test" {
			t.Fatalf("X-Jikim-Event=%s", event)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("엔드포인트가 요청을 받지 못했습니다")
	}
	call := awaitWebhookCompletion(t, calls)
	if call.statusCode != http.StatusOK || call.err != nil {
		t.Fatalf("status=%d err=%v", call.statusCode, call.err)
	}
	if records := webhookWarnings(t, logs); len(records) != 0 {
		t.Fatalf("정상 전송에 경고가 남았습니다: %v", records)
	}
}

// 3xx는 성공이 아니다 — outbound 클라이언트가 리다이렉트를 따라가지 않으므로
// 두 번째 URL로는 요청이 가지 않고 302가 그대로 실패로 보고된다.
func TestWebhookTestDoesNotFollowRedirect(t *testing.T) {
	var followed atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		followed.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusFound)
	}))
	defer endpoint.Close()
	server, recorded := webhookServer(t, endpoint.URL, nil)

	response := httptest.NewRecorder()
	server.webhookTest(response, httptest.NewRequest("POST", "/api/v1/integrations/webhook/test", nil))

	result := decodeWebhookResult(t, response.Body.String())
	if result["ok"] != false {
		t.Fatalf("302가 성공으로 보고되었습니다: %s", response.Body.String())
	}
	if result["status_code"] != float64(http.StatusFound) {
		t.Fatalf("status_code=%v", result["status_code"])
	}
	if got := followed.Load(); got != 0 {
		t.Fatalf("리다이렉트를 따라갔습니다: %d", got)
	}
	if len(*recorded) != 1 || (*recorded)[0] == nil ||
		!strings.Contains((*recorded)[0].Error(), "webhook 응답 상태 302") {
		t.Fatalf("기록에 남긴 delivery 오류=%v", *recorded)
	}
}
