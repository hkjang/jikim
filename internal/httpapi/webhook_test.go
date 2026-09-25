package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
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
