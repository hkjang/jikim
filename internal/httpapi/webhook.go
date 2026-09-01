package httpapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/hkjang/jikim/internal/ids"
	"github.com/hkjang/jikim/internal/store"
)

type webhookEvent struct {
	DeliveryID string         `json:"delivery_id"`
	Event      string         `json:"event"`
	OccurredAt time.Time      `json:"occurred_at"`
	Resource   string         `json:"resource,omitempty"`
	ActorID    string         `json:"actor_id,omitempty"`
	RequestID  string         `json:"request_id,omitempty"`
	Data       map[string]any `json:"data,omitempty"`
}

func (s *Server) queueWebhook(r *http.Request, eventType, resource string, data map[string]any) {
	cfg, err := s.store.WebhookConfig(r.Context())
	if err != nil {
		s.logger.Warn("webhook 설정 조회 실패", "error", err, "request_id", requestIDFrom(r))
		return
	}
	if !cfg.Enabled || !cfg.Events[eventType] || cfg.URL == "" || cfg.SigningSecret == "" {
		return
	}
	session, _ := sessionFrom(r)
	actorID := session.User.ID
	id, err := ids.UUID()
	if err != nil {
		s.logger.Warn("webhook delivery id 생성 실패", "error", err)
		return
	}
	event := webhookEvent{DeliveryID: id, Event: eventType, OccurredAt: time.Now().UTC(),
		Resource: resource, ActorID: actorID, RequestID: requestIDFrom(r), Data: data}
	delivery, err := s.store.CreateWebhookDelivery(r.Context(), id, eventType, resource, &actorID, requestIDFrom(r), event)
	if err != nil {
		s.logger.Warn("webhook delivery 저장 실패", "error", err, "event", eventType)
		return
	}
	select {
	case s.webhookSlots <- struct{}{}:
		go func() {
			defer func() { <-s.webhookSlots }()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if _, err := s.deliverWebhook(ctx, cfg, delivery); err != nil {
				s.logger.Warn("webhook 전송 실패", "error", err, "delivery_id", delivery.ID, "event", eventType)
			}
		}()
	default:
		_ = s.store.CompleteWebhookDelivery(context.Background(), delivery.ID, 0, errors.New("webhook 전송 대기열이 가득 찼습니다"))
	}
}

func (s *Server) webhookTest(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.store.WebhookConfig(r.Context())
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	if cfg.URL == "" || cfg.SigningSecret == "" {
		writeError(w, r, http.StatusBadRequest, "webhook_not_configured", "Webhook URL과 서명 키를 먼저 저장하세요")
		return
	}
	session, _ := sessionFrom(r)
	id, err := ids.UUID()
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	event := webhookEvent{DeliveryID: id, Event: "integration.test", OccurredAt: time.Now().UTC(),
		Resource: "integrations/webhook", ActorID: session.User.ID, RequestID: requestIDFrom(r),
		Data: map[string]any{"message": "jikim webhook 연결 테스트"}}
	delivery, err := s.store.CreateWebhookDelivery(r.Context(), id, event.Event, event.Resource,
		&session.User.ID, event.RequestID, event)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	started := time.Now()
	statusCode, deliveryErr := s.deliverWebhook(r.Context(), cfg, delivery)
	result := map[string]any{"ok": deliveryErr == nil, "delivery_id": id,
		"status_code": statusCode, "latency_ms": time.Since(started).Milliseconds(),
		"signing": "hmac-sha256"}
	if deliveryErr != nil {
		result["message"] = "Webhook endpoint가 요청을 수락하지 않았습니다"
	} else {
		result["message"] = "서명된 테스트 이벤트를 전송했습니다"
	}
	writeData(w, http.StatusOK, result)
}

func (s *Server) listWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePage(r)
	items, err := s.store.ListWebhookDeliveries(r.Context(), r.URL.Query().Get("status"), limit, offset)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) retryWebhookDelivery(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.WebhookDelivery(r.Context(), r.PathValue("id"))
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	cfg, err := s.store.WebhookConfig(r.Context())
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	if cfg.URL == "" || cfg.SigningSecret == "" {
		writeError(w, r, http.StatusBadRequest, "webhook_not_configured", "Webhook URL과 서명 키를 먼저 저장하세요")
		return
	}
	statusCode, deliveryErr := s.deliverWebhook(r.Context(), cfg, item)
	updated, readErr := s.store.WebhookDelivery(r.Context(), item.ID)
	if readErr != nil {
		s.storeError(w, r, readErr)
		return
	}
	writeData(w, http.StatusOK, map[string]any{"ok": deliveryErr == nil, "status_code": statusCode, "delivery": updated})
}

func (s *Server) deliverWebhook(ctx context.Context, cfg store.WebhookConfig, delivery store.WebhookDelivery) (int, error) {
	timestamp := strconv.FormatInt(time.Now().UTC().Unix(), 10)
	signature := webhookSignature(cfg.SigningSecret, timestamp, delivery.Payload)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(delivery.Payload))
	if err != nil {
		_ = s.store.CompleteWebhookDelivery(context.Background(), delivery.ID, 0, err)
		return 0, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "jikim-webhook/1")
	request.Header.Set("X-Jikim-Delivery", delivery.ID)
	request.Header.Set("X-Jikim-Event", delivery.EventType)
	request.Header.Set("X-Jikim-Timestamp", timestamp)
	request.Header.Set("X-Jikim-Signature-256", signature)
	response, err := s.httpClient.Do(request)
	if err != nil {
		_ = s.store.CompleteWebhookDelivery(context.Background(), delivery.ID, 0, err)
		return 0, err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		err = fmt.Errorf("webhook 응답 상태 %d", response.StatusCode)
	}
	if updateErr := s.store.CompleteWebhookDelivery(context.Background(), delivery.ID, response.StatusCode, err); updateErr != nil {
		return response.StatusCode, updateErr
	}
	return response.StatusCode, err
}

func webhookSignature(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
