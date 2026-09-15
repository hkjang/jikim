package mail

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/hkjang/jikim/internal/ids"
)

// ConfigSource reads the stored relay configuration on every send, so a
// change in the settings screen applies without a restart.
type ConfigSource interface {
	MailConfig(context.Context) (Config, error)
}

// Directory resolves account identifiers to email addresses. The users table
// already knows every address, so mail keeps no roster of its own.
type Directory interface {
	UserEmails(context.Context, []string) (map[string]string, error)
}

// Journal records every attempt — queued, sent or failed — so an
// administrator can answer "did it go out?" without reading relay logs.
type Journal interface {
	RecordMailDelivery(context.Context, Delivery) error
	CompleteMailDelivery(ctx context.Context, id string, attempts int, cause error) error
}

// Delivery is one attempt to reach one address. It carries the subject and
// recipient but never the body: a delivery log that quotes secrets is a
// second place secrets leak from.
type Delivery struct {
	ID           string    `json:"id"`
	Event        string    `json:"event"`
	Recipient    string    `json:"recipient"`
	Subject      string    `json:"subject"`
	Resource     string    `json:"resource,omitempty"`
	ActorID      string    `json:"actor_id,omitempty"`
	Status       string    `json:"status"`
	Attempts     int       `json:"attempts"`
	ErrorMessage string    `json:"error_message,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Delivery statuses.
const (
	StatusQueued = "queued"
	StatusSent   = "sent"
	StatusFailed = "failed"
)

const (
	maxSubjectLength = 300
	maxErrorLength   = 500
	// deliverySlots bounds the goroutines waiting on a slow relay, the same
	// way webhook deliveries are bounded; beyond it a delivery is recorded as
	// failed rather than queued without limit.
	deliverySlots = 16
)

var errQueueFull = errors.New("메일 전송 대기열이 가득 찼습니다")

// retryPause separates the two attempts; tests shorten it.
var retryPause = 2 * time.Second

type Service struct {
	config    ConfigSource
	directory Directory
	journal   Journal
	logger    *slog.Logger
	now       func() time.Time
	send      func(context.Context, Config, Message) error
	newID     func() (string, error)
	slots     chan struct{}
	pending   sync.WaitGroup
}

func NewService(config ConfigSource, directory Directory, journal Journal, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		config: config, directory: directory, journal: journal, logger: logger,
		now:   func() time.Time { return time.Now().UTC() },
		send:  Deliver,
		newID: ids.UUID,
		slots: make(chan struct{}, deliverySlots),
	}
}

// SetSender replaces the transport, which lets tests drive the service
// without a real relay.
func (s *Service) SetSender(sender func(context.Context, Config, Message) error) { s.send = sender }

// Wait blocks until every background delivery has been recorded. Tests use
// it; a shutdown path may too.
func (s *Service) Wait() { s.pending.Wait() }

// Notify resolves the recipients and sends in the background so no request
// waits on a mail server. The actor never receives mail about their own
// action, and recipients without an address are skipped quietly.
func (s *Service) Notify(ctx context.Context, notification Notification, actorID string, recipients []string) {
	config, err := s.config.MailConfig(ctx)
	if err != nil {
		s.logger.Warn("메일 설정 조회 실패", "error", err, "event", notification.Event)
		return
	}
	if !config.Enabled || !config.Allows(notification.Event) {
		return
	}
	addresses := s.resolve(ctx, recipients, actorID)
	if len(addresses) == 0 {
		return
	}
	// A relay that can never be reached is recorded per recipient instead of
	// logged once, so the delivery screen — not the server log — says why
	// nothing arrived.
	configErr := config.Validate()
	body := notification.Render(config)
	for _, address := range addresses {
		delivery, ok := s.record(ctx, notification, actorID, address)
		if !ok {
			continue
		}
		if configErr != nil {
			s.complete(ctx, delivery, 0, configErr)
			continue
		}
		message := Message{To: address, Subject: notification.Subject, Body: body}
		select {
		case s.slots <- struct{}{}:
			s.pending.Add(1)
			go func() {
				defer s.pending.Done()
				defer func() { <-s.slots }()
				s.deliver(delivery, config, message)
			}()
		default:
			s.complete(ctx, delivery, 0, errQueueFull)
		}
	}
}

// SendNow delivers immediately and reports the outcome, which is what the
// administrator's test button needs.
func (s *Service) SendNow(ctx context.Context, notification Notification, actorID, recipient string) (Delivery, error) {
	config, err := s.config.MailConfig(ctx)
	if err != nil {
		return Delivery{}, err
	}
	if !config.Enabled {
		return Delivery{}, ErrDisabled
	}
	if err := config.Validate(); err != nil {
		return Delivery{}, err
	}
	delivery, ok := s.record(ctx, notification, actorID, recipient)
	if !ok {
		return Delivery{}, errors.New("발송 기록을 남기지 못했습니다")
	}
	sendContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), config.Timeout+5*time.Second)
	defer cancel()
	err = s.send(sendContext, config, Message{To: recipient, Subject: notification.Subject, Body: notification.Render(config)})
	return s.complete(sendContext, delivery, 1, err), err
}

// deliver retries once, because a relay that briefly refuses a connection is
// common and losing the notification is worse than a short wait.
func (s *Service) deliver(delivery Delivery, config Config, message Message) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*config.Timeout+15*time.Second)
	defer cancel()
	var err error
	attempts := 0
	for attempts < 2 {
		attempts++
		if err = s.send(ctx, config, message); err == nil {
			break
		}
		if attempts == 1 {
			select {
			case <-ctx.Done():
				attempts = 2
			case <-time.After(retryPause):
			}
		}
	}
	s.complete(ctx, delivery, attempts, err)
}

func (s *Service) record(ctx context.Context, notification Notification, actorID, address string) (Delivery, bool) {
	id, err := s.newID()
	if err != nil {
		s.logger.Warn("메일 발송 id 생성 실패", "error", err)
		return Delivery{}, false
	}
	now := s.now()
	delivery := Delivery{
		ID: id, Event: notification.Event, Recipient: address, Subject: trim(notification.Subject, maxSubjectLength),
		Resource: notification.Resource, ActorID: strings.TrimSpace(actorID), Status: StatusQueued,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.journal.RecordMailDelivery(ctx, delivery); err != nil {
		// Sending what cannot be recorded would leave the administrator
		// unable to say what left the building; the request itself is fine.
		s.logger.Warn("메일 발송 기록 실패", "error", err, "event", delivery.Event)
		return Delivery{}, false
	}
	return delivery, true
}

func (s *Service) complete(ctx context.Context, delivery Delivery, attempts int, cause error) Delivery {
	delivery.Status, delivery.ErrorMessage = StatusSent, ""
	delivery.Attempts = max(attempts, 1)
	delivery.UpdatedAt = s.now()
	if cause != nil {
		delivery.Status, delivery.ErrorMessage = StatusFailed, trim(cause.Error(), maxErrorLength)
		s.logger.Warn("메일 발송 실패", "event", delivery.Event, "recipient", delivery.Recipient, "error", cause)
	}
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.journal.CompleteMailDelivery(recordCtx, delivery.ID, delivery.Attempts, cause); err != nil {
		s.logger.Warn("메일 발송 결과 기록 실패", "error", err, "delivery_id", delivery.ID)
	}
	return delivery
}

// resolve turns account identifiers into unique addresses, dropping the
// actor so nobody is told about their own action. An identifier that is
// already an address needs no directory entry.
func (s *Service) resolve(ctx context.Context, recipients []string, actorID string) []string {
	actor := strings.TrimSpace(actorID)
	wanted := make([]string, 0, len(recipients))
	for _, recipient := range recipients {
		trimmed := strings.TrimSpace(recipient)
		if trimmed == "" || (actor != "" && strings.EqualFold(trimmed, actor)) {
			continue
		}
		wanted = append(wanted, trimmed)
	}
	if len(wanted) == 0 {
		return nil
	}
	emails := map[string]string{}
	if s.directory != nil {
		found, err := s.directory.UserEmails(ctx, wanted)
		if err != nil {
			s.logger.Warn("메일 수신자 조회 실패", "error", err)
			return nil
		}
		emails = found
	}
	seen, addresses := map[string]struct{}{}, make([]string, 0, len(wanted))
	for _, recipient := range wanted {
		address := strings.TrimSpace(emails[recipient])
		if address == "" && strings.Contains(recipient, "@") {
			address = recipient
		}
		if address == "" {
			continue
		}
		key := strings.ToLower(address)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		addresses = append(addresses, address)
	}
	return addresses
}

func trim(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
