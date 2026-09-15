package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/hkjang/jikim/internal/mail"
	"github.com/hkjang/jikim/internal/model"
	"github.com/hkjang/jikim/internal/store"
)

// The events that go out by mail are the ones somebody is actually waiting
// on: a reviewer who does not know a request is pending, a requester who
// keeps reloading the approvals screen, an owner whose rotation stopped.
// Plain "something changed" events stay on the webhook.

// notifyMail hands one event to the mail service. Every caller is on a
// request path, so nothing here can fail the request: the service records
// the attempt and sends in the background.
func (s *Server) notifyMail(r *http.Request, notification mail.Notification, recipients []string) {
	if s.mail == nil || len(recipients) == 0 {
		return
	}
	session, _ := sessionFrom(r)
	// The response may already be written by the time the delivery row is
	// inserted, so the record must outlive the request context.
	s.mail.Notify(context.WithoutCancel(r.Context()), notification, session.User.ID, recipients)
}

// actorLabel is the name people recognise in a mail body.
func actorLabel(user model.User) string {
	if name := strings.TrimSpace(user.DisplayName); name != "" {
		return name
	}
	if name := strings.TrimSpace(user.Username); name != "" {
		return name
	}
	return "알 수 없는 사용자"
}

// approvalReviewers lists everyone who may decide a request: administrators
// always, managers when the workflow names them as the reviewer role.
func (s *Server) approvalReviewers(ctx context.Context) ([]string, error) {
	cfg, err := s.store.ApprovalConfig(ctx)
	if err != nil {
		return nil, err
	}
	roles := []string{"admin"}
	if cfg.ReviewerRole == "manager" {
		roles = append(roles, "manager")
	}
	return s.reviewerIDs(ctx, roles)
}

func (s *Server) notifyApprovalRequested(r *http.Request, approval model.Approval) {
	if s.mail == nil {
		return
	}
	reviewers, err := s.approvalReviewers(r.Context())
	if err != nil {
		s.logger.Warn("승인 검토자 조회 실패", "error", err, "approval_id", approval.ID)
		return
	}
	session, _ := sessionFrom(r)
	s.notifyMail(r, mail.ApprovalRequested(actorLabel(session.User), approval.Action, approval.Resource, approval.ID), reviewers)
}

func (s *Server) notifyApprovalDecided(r *http.Request, approval model.Approval) {
	session, _ := sessionFrom(r)
	s.notifyMail(r, mail.ApprovalDecided(actorLabel(session.User), approval.Action, approval.Resource, approval.ID, approval.Status, approval.Comment),
		[]string{approval.RequesterID})
}

// notifyRotationFailed tells the secret owner. A secret without an owner
// falls back to the administrators, because a rotation nobody hears about
// is the failure this mail exists to surface.
func (s *Server) notifyRotationFailed(r *http.Request, path string, owner *string, cause error) {
	if s.mail == nil {
		return
	}
	recipients := []string{}
	if owner != nil && strings.TrimSpace(*owner) != "" {
		recipients = append(recipients, *owner)
	} else {
		admins, err := s.reviewerIDs(r.Context(), []string{"admin"})
		if err != nil {
			s.logger.Warn("관리자 조회 실패", "error", err, "path", path)
			return
		}
		recipients = admins
	}
	session, _ := sessionFrom(r)
	s.notifyMail(r, mail.RotationFailed(actorLabel(session.User), path, publicReason(cause)), recipients)
}

func (s *Server) reviewerIDs(ctx context.Context, roles []string) ([]string, error) {
	if s.reviewerLookup != nil {
		return s.reviewerLookup(ctx, roles)
	}
	return s.store.ActiveUserIDsByRole(ctx, roles)
}

// publicReason keeps store internals (driver strings, SQLSTATEs) out of a
// mail body the same way the HTTP error mapping keeps them out of responses.
func publicReason(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, store.ErrInvalid) || errors.Is(err, store.ErrConflict) || errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrForbidden) {
		return err.Error()
	}
	return "저장소 오류로 새 값을 기록하지 못했습니다"
}

// mailTest sends one real message with the saved settings and reports the
// outcome in place, because relay settings are rarely right the first time.
func (s *Server) mailTest(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Recipient string `json:"recipient"`
	}
	if r.ContentLength != 0 && !decodeJSON(w, r, &input) {
		return
	}
	if s.mail == nil {
		writeError(w, r, http.StatusServiceUnavailable, "mail_unavailable", "메일 서비스가 구성되지 않았습니다")
		return
	}
	session, _ := sessionFrom(r)
	recipient := strings.TrimSpace(input.Recipient)
	if recipient == "" {
		recipient = strings.TrimSpace(session.User.Email)
	}
	if !strings.Contains(recipient, "@") {
		writeError(w, r, http.StatusBadRequest, "recipient_required", "받는 사람 메일 주소를 입력하세요. 프로필에 이메일이 없으면 직접 적어야 합니다")
		return
	}
	started := time.Now()
	delivery, err := s.mail.SendNow(r.Context(), mail.TestMessage(), session.User.ID, recipient)
	switch {
	case errors.Is(err, mail.ErrDisabled):
		writeError(w, r, http.StatusBadRequest, "mail_disabled", "메일 알림을 켜고 저장한 뒤 시험 발송하세요")
		return
	case errors.Is(err, mail.ErrInvalid) && delivery.ID == "":
		writeError(w, r, http.StatusBadRequest, "mail_not_configured", strings.TrimPrefix(err.Error(), mail.ErrInvalid.Error()+": "))
		return
	case err != nil && delivery.ID == "":
		s.storeError(w, r, err)
		return
	}
	result := map[string]any{"ok": err == nil, "delivery_id": delivery.ID, "recipient": recipient,
		"latency_ms": time.Since(started).Milliseconds()}
	if err != nil {
		result["message"] = "릴레이가 메일을 받지 않았습니다: " + delivery.ErrorMessage
	} else {
		result["message"] = recipient + " 주소로 테스트 메일을 보냈습니다"
	}
	writeData(w, http.StatusOK, result)
}

func (s *Server) listMailDeliveries(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePage(r)
	items, err := s.store.ListMailDeliveries(r.Context(), r.URL.Query().Get("status"), limit, offset)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}
