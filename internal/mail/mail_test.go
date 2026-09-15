package mail

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeJournal records what the service would have written to
// mail_deliveries, keyed by delivery id.
type fakeJournal struct {
	mu        sync.Mutex
	recorded  []Delivery
	completed map[string]struct {
		attempts int
		cause    error
	}
	recordErr error
}

func newFakeJournal() *fakeJournal {
	return &fakeJournal{completed: map[string]struct {
		attempts int
		cause    error
	}{}}
}

func (j *fakeJournal) RecordMailDelivery(_ context.Context, delivery Delivery) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.recordErr != nil {
		return j.recordErr
	}
	j.recorded = append(j.recorded, delivery)
	return nil
}

func (j *fakeJournal) CompleteMailDelivery(_ context.Context, id string, attempts int, cause error) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.completed[id] = struct {
		attempts int
		cause    error
	}{attempts, cause}
	return nil
}

func (j *fakeJournal) outcomes() (sent, failed int) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, result := range j.completed {
		if result.cause == nil {
			sent++
		} else {
			failed++
		}
	}
	return sent, failed
}

type fakeDirectory map[string]string

func (d fakeDirectory) UserEmails(_ context.Context, ids []string) (map[string]string, error) {
	found := map[string]string{}
	for _, id := range ids {
		if email, ok := d[id]; ok {
			found[id] = email
		}
	}
	return found, nil
}

type configFunc func(context.Context) (Config, error)

func (f configFunc) MailConfig(ctx context.Context) (Config, error) { return f(ctx) }

func enabledValues() map[string]any {
	return map[string]any{"enabled": true, "smtp_host": "relay.corp.example", "from_address": "jikim@corp.example"}
}

type sentMail struct {
	mu   sync.Mutex
	list []Message
}

func (s *sentMail) sender(err error) func(context.Context, Config, Message) error {
	return func(_ context.Context, _ Config, message Message) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.list = append(s.list, message)
		return err
	}
}

func (s *sentMail) recipients() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.list))
	for _, message := range s.list {
		out = append(out, message.To)
	}
	return out
}

func testService(values map[string]any, journal *fakeJournal, directory Directory) *Service {
	retryPause = 0
	source := configFunc(func(context.Context) (Config, error) { return ReadConfig(values, ""), nil })
	return NewService(source, directory, journal, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

var testDirectory = fakeDirectory{"u-alice": "alice@corp.example", "u-bob": "bob@corp.example", "u-noaddress": ""}

func TestDisabledSendsNothingAndRecordsNothing(t *testing.T) {
	journal, sent := newFakeJournal(), &sentMail{}
	values := enabledValues()
	values["enabled"] = false
	service := testService(values, journal, testDirectory)
	service.SetSender(sent.sender(nil))
	service.Notify(context.Background(), TestMessage(), "u-bob", []string{"u-alice"})
	service.Wait()
	if len(sent.list) != 0 || len(journal.recorded) != 0 {
		t.Fatalf("disabled mail still did something: sent=%d recorded=%d", len(sent.list), len(journal.recorded))
	}
	// A fresh install has no setting at all; that is the same "off".
	if ReadConfig(nil, "").Enabled {
		t.Fatal("a missing setting must read as disabled")
	}
}

func TestEnabledWithoutHostRecordsTheReasonInsteadOfSending(t *testing.T) {
	journal, sent := newFakeJournal(), &sentMail{}
	values := enabledValues()
	delete(values, "smtp_host")
	service := testService(values, journal, testDirectory)
	service.SetSender(sent.sender(nil))
	service.Notify(context.Background(), TestMessage(), "u-bob", []string{"u-alice"})
	service.Wait()
	if len(sent.list) != 0 {
		t.Fatalf("sent %d messages with no relay configured", len(sent.list))
	}
	if len(journal.recorded) != 1 {
		t.Fatalf("recorded=%d, want one failed delivery that names the missing host", len(journal.recorded))
	}
	result := journal.completed[journal.recorded[0].ID]
	if result.cause == nil || !strings.Contains(result.cause.Error(), "smtp_host") {
		t.Fatalf("failure reason=%v, want the missing smtp_host", result.cause)
	}
}

func TestDeadRelayDoesNotBlockTheCallerAndIsRecordedAsFailed(t *testing.T) {
	journal := newFakeJournal()
	service := testService(enabledValues(), journal, testDirectory)
	release := make(chan struct{})
	service.SetSender(func(context.Context, Config, Message) error {
		<-release
		return errors.New("dial tcp: connection refused")
	})
	started := time.Now()
	service.Notify(context.Background(), ApprovalRequested("bob", "secret.put", "apps/db", "ap-1"), "u-bob", []string{"u-alice"})
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("Notify waited on the relay for %s", elapsed)
	}
	close(release)
	service.Wait()
	if len(journal.recorded) != 1 || journal.recorded[0].Status != StatusQueued {
		t.Fatalf("recorded=%+v, want one queued delivery", journal.recorded)
	}
	result := journal.completed[journal.recorded[0].ID]
	if result.cause == nil || result.attempts != 2 {
		t.Fatalf("completed=%+v, want a failure after the one retry", result)
	}
}

func TestActorIsNeverToldAboutTheirOwnAction(t *testing.T) {
	journal, sent := newFakeJournal(), &sentMail{}
	service := testService(enabledValues(), journal, testDirectory)
	service.SetSender(sent.sender(nil))
	service.Notify(context.Background(), ApprovalDecided("alice", "secret.put", "apps/db", "ap-1", "approved", ""), "u-alice", []string{"u-alice", "u-bob", "u-bob", "u-noaddress", "u-missing"})
	service.Wait()
	if got := sent.recipients(); len(got) != 1 || got[0] != "bob@corp.example" {
		t.Fatalf("recipients=%v, want only bob (actor dropped, duplicates folded, no-address skipped)", got)
	}
}

func TestEventSwitchStopsOnlyThatKind(t *testing.T) {
	journal, sent := newFakeJournal(), &sentMail{}
	values := enabledValues()
	values["notify_rotation_failed"] = false
	service := testService(values, journal, testDirectory)
	service.SetSender(sent.sender(nil))
	service.Notify(context.Background(), RotationFailed("bob", "apps/db", "저장소 오류"), "u-bob", []string{"u-alice"})
	service.Notify(context.Background(), ApprovalRequested("bob", "secret.put", "apps/db", "ap-1"), "u-bob", []string{"u-alice"})
	service.Wait()
	if len(sent.list) != 1 || sent.list[0].Subject != ApprovalRequested("bob", "secret.put", "apps/db", "ap-1").Subject {
		t.Fatalf("sent=%+v, want only the approval request", sent.list)
	}
}

func TestJournalKeepsBothOutcomesAndNoBody(t *testing.T) {
	journal := newFakeJournal()
	service := testService(enabledValues(), journal, testDirectory)
	var calls int
	var mu sync.Mutex
	service.SetSender(func(_ context.Context, _ Config, message Message) error {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if message.To == "bob@corp.example" {
			return errors.New("550 mailbox unavailable")
		}
		return nil
	})
	notification := RotationFailed("carol", "apps/db", "저장소 오류")
	service.Notify(context.Background(), notification, "u-carol", []string{"u-alice", "u-bob"})
	service.Wait()
	sent, failed := journal.outcomes()
	if sent != 1 || failed != 1 {
		t.Fatalf("sent=%d failed=%d, want one of each", sent, failed)
	}
	for _, delivery := range journal.recorded {
		if delivery.Subject != notification.Subject || delivery.Event != EventRotationFailed || delivery.ActorID != "u-carol" {
			t.Fatalf("delivery=%+v lost its subject, event or actor", delivery)
		}
		if raw, _ := json.Marshal(delivery); strings.Contains(string(raw), "회전이 실패했습니다") {
			t.Fatalf("delivery record carries the body: %s", raw)
		}
	}
}

func TestUnrecordableDeliveryIsNotSent(t *testing.T) {
	journal, sent := newFakeJournal(), &sentMail{}
	journal.recordErr = errors.New("database is down")
	service := testService(enabledValues(), journal, testDirectory)
	service.SetSender(sent.sender(nil))
	service.Notify(context.Background(), TestMessage(), "u-bob", []string{"u-alice"})
	service.Wait()
	if len(sent.list) != 0 {
		t.Fatal("a delivery that could not be recorded was still sent")
	}
}

func TestSendNowReportsTheRelayOutcome(t *testing.T) {
	journal := newFakeJournal()
	service := testService(enabledValues(), journal, testDirectory)
	service.SetSender(func(context.Context, Config, Message) error { return errors.New("connection refused") })
	delivery, err := service.SendNow(context.Background(), TestMessage(), "u-admin", "ops@corp.example")
	if err == nil || delivery.Status != StatusFailed || !strings.Contains(delivery.ErrorMessage, "connection refused") {
		t.Fatalf("delivery=%+v err=%v, want a failed delivery with the relay's reason", delivery, err)
	}
	values := enabledValues()
	values["enabled"] = false
	off := testService(values, journal, testDirectory)
	if _, err := off.SendNow(context.Background(), TestMessage(), "u-admin", "ops@corp.example"); !errors.Is(err, ErrDisabled) {
		t.Fatalf("err=%v, want ErrDisabled", err)
	}
}

func TestConfigNeverSerializesThePassword(t *testing.T) {
	config := ReadConfig(map[string]any{"enabled": true, "smtp_host": "relay", "username": "svc"}, "hunter2")
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "hunter2") || strings.Contains(string(raw), "svc") {
		t.Fatalf("credentials serialized: %s", raw)
	}
}

func TestReadConfigDefaultsMatchAnInternalRelay(t *testing.T) {
	config := ReadConfig(map[string]any{"smtp_host": "relay.corp.example"}, "")
	if config.Port != 25 || config.Security != "auto" || config.Username != "" || config.SkipVerify || config.Timeout != 10*time.Second {
		t.Fatalf("defaults=%+v, want port 25, auto security, no credentials, verified TLS, 10s", config)
	}
	if config.FromAddress != "jikim@relay.corp.example" {
		t.Fatalf("from=%q, want a sender derived from the relay host", config.FromAddress)
	}
	if !config.Allows(EventApprovalRequested) || !config.Allows("some.future.event") {
		t.Fatal("events default to on")
	}
	if got := ReadConfig(map[string]any{"smtp_host": "relay", "smtp_port": float64(465)}, "").Security; got != "tls" {
		t.Fatalf("port 465 security=%q, want implicit tls", got)
	}
}

func TestComposeEncodesKoreanAndDotStuffs(t *testing.T) {
	config := ReadConfig(map[string]any{"smtp_host": "relay", "from_address": "jikim@corp.example", "from_name": "지킴"}, "")
	raw := compose(config, Message{To: "alice@corp.example", Subject: "승인 요청", Body: ".시작\n.\n끝"}, time.Date(2026, 9, 16, 3, 0, 0, 0, time.UTC))
	for _, want := range []string{"Subject: =?utf-8?q?", "From: =?utf-8?q?", " <jikim@corp.example>\r\n", "\r\n\r\n..시작\r\n..\r\n끝\r\n", "Auto-Submitted: auto-generated\r\n"} {
		if !strings.Contains(raw, want) {
			t.Fatalf("message lacks %q:\n%s", want, raw)
		}
	}
}

func TestRenderAppendsLinkOnlyWithBaseURL(t *testing.T) {
	notification := ApprovalRequested("bob", "secret.put", "apps/db", "ap-1")
	without := notification.Render(ReadConfig(enabledValues(), ""))
	if strings.Contains(without, "바로 열기") {
		t.Fatalf("link rendered without a base URL:\n%s", without)
	}
	values := enabledValues()
	values["base_url"] = "https://jikim.corp.example/"
	with := notification.Render(ReadConfig(values, ""))
	if !strings.Contains(with, "바로 열기: https://jikim.corp.example/approvals") {
		t.Fatalf("link missing:\n%s", with)
	}
}
