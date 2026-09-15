package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hkjang/jikim/internal/mail"
	"github.com/hkjang/jikim/internal/model"
)

// mailFixture stands in for the store: it serves one configuration, one
// directory and keeps the delivery journal in memory.
type mailFixture struct {
	config   mail.Config
	emails   map[string]string
	mu       sync.Mutex
	recorded []mail.Delivery
	failed   map[string]string
}

func (f *mailFixture) MailConfig(context.Context) (mail.Config, error) { return f.config, nil }

func (f *mailFixture) UserEmails(_ context.Context, ids []string) (map[string]string, error) {
	found := map[string]string{}
	for _, id := range ids {
		if email, ok := f.emails[id]; ok {
			found[id] = email
		}
	}
	return found, nil
}

func (f *mailFixture) RecordMailDelivery(_ context.Context, delivery mail.Delivery) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recorded = append(f.recorded, delivery)
	return nil
}

func (f *mailFixture) CompleteMailDelivery(_ context.Context, id string, _ int, cause error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if cause != nil {
		f.failed[id] = cause.Error()
	}
	return nil
}

func mailServer(t *testing.T, enabled bool, sender func(context.Context, mail.Config, mail.Message) error) (*Server, *mailFixture) {
	t.Helper()
	fixture := &mailFixture{
		config: mail.ReadConfig(map[string]any{"enabled": enabled, "smtp_host": "relay.corp.example", "from_address": "jikim@corp.example"}, ""),
		emails: map[string]string{"u-alice": "alice@corp.example", "u-bob": "bob@corp.example"},
		failed: map[string]string{},
	}
	server := quietServer()
	server.mail = mail.NewService(fixture, fixture, fixture, server.logger)
	server.mail.SetSender(sender)
	return server, fixture
}

func adminRequest(method, path, body string) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	session := model.Session{User: model.User{ID: "u-admin", Username: "admin", DisplayName: "관리자", Role: "admin", Email: "admin@corp.example"}}
	return request.WithContext(context.WithValue(request.Context(), sessionKey, session))
}

func TestApprovalDecisionMailReachesTheRequesterWithoutBlockingTheRequest(t *testing.T) {
	release := make(chan struct{})
	var sent []mail.Message
	var mu sync.Mutex
	server, fixture := mailServer(t, true, func(_ context.Context, _ mail.Config, message mail.Message) error {
		<-release
		mu.Lock()
		defer mu.Unlock()
		sent = append(sent, message)
		return nil
	})
	approval := model.Approval{ID: "ap-1", Action: "secret.put", Resource: "apps/db", RequesterID: "u-alice", Status: "approved"}
	started := time.Now()
	server.notifyApprovalDecided(adminRequest(http.MethodPost, "/api/v1/approvals/ap-1/approve", ""), approval)
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("the handler waited on the relay for %s", elapsed)
	}
	close(release)
	server.mail.Wait()
	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 1 || sent[0].To != "alice@corp.example" {
		t.Fatalf("sent=%+v, want one mail to the requester", sent)
	}
	if !strings.Contains(sent[0].Body, "관리자 님에 의해 승인되어") || strings.Contains(sent[0].Body, "u-admin") {
		t.Fatalf("body names the reviewer by id or misses the decision:\n%s", sent[0].Body)
	}
	if len(fixture.recorded) != 1 || fixture.recorded[0].ActorID != "u-admin" || fixture.recorded[0].Resource != "ap-1" {
		t.Fatalf("recorded=%+v", fixture.recorded)
	}
}

func TestRotationFailureMailGoesToTheOwnerNotTheActor(t *testing.T) {
	var sent []mail.Message
	var mu sync.Mutex
	server, _ := mailServer(t, true, func(_ context.Context, _ mail.Config, message mail.Message) error {
		mu.Lock()
		defer mu.Unlock()
		sent = append(sent, message)
		return nil
	})
	server.reviewerLookup = func(context.Context, []string) ([]string, error) {
		t.Fatal("an owned secret must not fall back to the administrators")
		return nil, nil
	}
	owner := "u-bob"
	server.notifyRotationFailed(adminRequest(http.MethodPost, "/api/v1/secrets/s-1/rotate", ""), "apps/db", &owner, errors.New("pgx: SQLSTATE 08006 host=db.internal"))
	server.mail.Wait()
	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 1 || sent[0].To != "bob@corp.example" {
		t.Fatalf("sent=%+v, want one mail to the owner", sent)
	}
	if strings.Contains(sent[0].Body, "SQLSTATE") || strings.Contains(sent[0].Body, "db.internal") {
		t.Fatalf("driver internals leaked into the mail body:\n%s", sent[0].Body)
	}
	// The actor owning the secret hears nothing: they saw the error already.
	self := "u-admin"
	server.notifyRotationFailed(adminRequest(http.MethodPost, "/api/v1/secrets/s-1/rotate", ""), "apps/db", &self, errors.New("boom"))
	server.mail.Wait()
	if len(sent) != 1 {
		t.Fatalf("the actor was mailed about their own rotation: %+v", sent)
	}
}

func TestMailTestReportsRelayFailureInPlace(t *testing.T) {
	server, fixture := mailServer(t, true, func(context.Context, mail.Config, mail.Message) error {
		return errors.New("dial tcp 10.0.0.9:25: connection refused")
	})
	response := httptest.NewRecorder()
	server.mailTest(response, adminRequest(http.MethodPost, "/api/v1/integrations/mail/test", `{"recipient":"ops@corp.example"}`))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Data struct {
			OK         bool   `json:"ok"`
			Message    string `json:"message"`
			DeliveryID string `json:"delivery_id"`
			Recipient  string `json:"recipient"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data.OK || !strings.Contains(body.Data.Message, "connection refused") || body.Data.Recipient != "ops@corp.example" {
		t.Fatalf("result=%+v, want ok=false with the relay's reason", body.Data)
	}
	if len(fixture.recorded) != 1 || fixture.failed[body.Data.DeliveryID] == "" {
		t.Fatalf("the test send was not journaled as failed: recorded=%+v failed=%v", fixture.recorded, fixture.failed)
	}
}

func TestMailTestRefusesWhileDisabledAndDefaultsToTheAdminsAddress(t *testing.T) {
	server, _ := mailServer(t, false, func(context.Context, mail.Config, mail.Message) error { return nil })
	response := httptest.NewRecorder()
	server.mailTest(response, adminRequest(http.MethodPost, "/api/v1/integrations/mail/test", ""))
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "mail_disabled") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}

	var to string
	server, _ = mailServer(t, true, func(_ context.Context, _ mail.Config, message mail.Message) error {
		to = message.To
		return nil
	})
	response = httptest.NewRecorder()
	server.mailTest(response, adminRequest(http.MethodPost, "/api/v1/integrations/mail/test", ""))
	if response.Code != http.StatusOK || to != "admin@corp.example" {
		t.Fatalf("status=%d to=%q body=%s", response.Code, to, response.Body.String())
	}
}
