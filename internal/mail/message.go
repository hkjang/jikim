package mail

import (
	"fmt"
	"mime"
	"strings"
	"time"
)

// compose builds a MIME message. Korean subjects and bodies are encoded so
// relays and clients that predate UTF-8 headers still show them correctly.
func compose(config Config, message Message, now time.Time) string {
	var builder strings.Builder
	builder.WriteString("From: " + encodeAddress(config.Address()) + "\r\n")
	builder.WriteString("To: " + strings.TrimSpace(message.To) + "\r\n")
	builder.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", message.Subject) + "\r\n")
	builder.WriteString("Date: " + now.Format(time.RFC1123Z) + "\r\n")
	builder.WriteString("MIME-Version: 1.0\r\n")
	builder.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	builder.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	builder.WriteString("Auto-Submitted: auto-generated\r\n")
	builder.WriteString("X-Jikim-Notification: 1\r\n")
	builder.WriteString("\r\n")
	builder.WriteString(normalizeBody(message.Body))
	return builder.String()
}

func encodeAddress(address string) string {
	open := strings.LastIndex(address, "<")
	if open <= 0 {
		return address
	}
	return mime.QEncoding.Encode("utf-8", strings.TrimSpace(address[:open])) + " " + address[open:]
}

// normalizeBody uses CRLF line endings and escapes a leading dot so a line of
// text can never terminate the DATA command early.
func normalizeBody(body string) string {
	body = strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n")
	if strings.HasPrefix(body, ".") {
		body = "." + body
	}
	body = strings.ReplaceAll(body, "\r\n.", "\r\n..")
	if !strings.HasSuffix(body, "\r\n") {
		body += "\r\n"
	}
	return body
}

// Notification is the content of one event mail before recipients are
// resolved. Resource is what the mail is about (an approval id, a secret
// path) and is recorded with the delivery; the body never is.
type Notification struct {
	Event    string
	Subject  string
	Lines    []string
	Resource string
	Link     string
}

// Render turns a notification into the message body, appending the link and
// a footer that says why the mail arrived.
func (n Notification) Render(config Config) string {
	lines := append([]string{}, n.Lines...)
	if link := n.absoluteLink(config); link != "" {
		lines = append(lines, "", "바로 열기: "+link)
	}
	lines = append(lines, "", "—", "이 메일은 jikim 메일 알림 설정에 따라 자동으로 발송되었습니다. 관리자가 서비스 설정에서 종류별로 끌 수 있습니다.")
	return strings.Join(lines, "\n")
}

func (n Notification) absoluteLink(config Config) string {
	base := strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	if n.Link == "" || base == "" {
		return ""
	}
	return base + "/" + strings.TrimLeft(n.Link, "/")
}

// approvalLabel names an approval action the way the screen does.
func approvalLabel(action string) string {
	switch action {
	case "secret.put":
		return "Secret 생성·변경"
	case "secret.delete":
		return "Secret 폐기"
	default:
		return action
	}
}

// ApprovalRequested tells reviewers that somebody is waiting on them.
func ApprovalRequested(actor, action, resource, approvalID string) Notification {
	return Notification{
		Event:    EventApprovalRequested,
		Subject:  fmt.Sprintf("[jikim] 승인 요청: %s — %s", approvalLabel(action), resource),
		Resource: approvalID,
		Link:     "/approvals",
		Lines: []string{
			fmt.Sprintf("%s 님이 '%s'에 대한 %s 승인을 요청했습니다.", actor, resource, approvalLabel(action)),
			"검토자가 승인하거나 반려할 때까지 요청은 대기 상태로 남습니다.",
		},
	}
}

// ApprovalDecided tells the requester what the reviewer decided.
func ApprovalDecided(reviewer, action, resource, approvalID, decision, comment string) Notification {
	result := "반려되었습니다"
	if decision == "approved" {
		result = "승인되어 적용되었습니다"
	}
	lines := []string{fmt.Sprintf("'%s'에 대한 %s 요청이 %s 님에 의해 %s.", resource, approvalLabel(action), reviewer, result)}
	if strings.TrimSpace(comment) != "" {
		lines = append(lines, "", quote(comment))
	}
	return Notification{
		Event:    EventApprovalDecided,
		Subject:  fmt.Sprintf("[jikim] 승인 요청 처리됨: %s — %s", approvalLabel(action), resource),
		Resource: approvalID,
		Link:     "/approvals",
		Lines:    lines,
	}
}

// RotationFailed tells the secret owner that a rotation stopped, because a
// rotation nobody notices is a credential that quietly ages past its policy.
func RotationFailed(actor, path, reason string) Notification {
	lines := []string{fmt.Sprintf("%s 님이 시작한 '%s' 회전이 실패했습니다.", actor, path)}
	if strings.TrimSpace(reason) != "" {
		lines = append(lines, fmt.Sprintf("원인: %s", strings.TrimSpace(reason)))
	}
	lines = append(lines, "", "기존 값은 그대로 남아 있습니다. 새 값은 이 메일에 담지 않습니다.")
	return Notification{
		Event:    EventRotationFailed,
		Subject:  fmt.Sprintf("[jikim] 회전 실패: %s", path),
		Resource: path,
		Link:     "/secrets",
		Lines:    lines,
	}
}

// TestMessage proves the relay works from the settings screen.
func TestMessage() Notification {
	return Notification{
		Event:   EventTest,
		Subject: "[jikim] SMTP 발송 테스트",
		Lines:   []string{"jikim 관리자 화면에서 보낸 테스트 메일입니다.", "이 메일을 받았다면 SMTP 설정이 정상입니다."},
	}
}

func quote(body string) string {
	trimmed := strings.TrimSpace(body)
	if len([]rune(trimmed)) > 500 {
		trimmed = string([]rune(trimmed)[:500]) + "…"
	}
	lines := strings.Split(trimmed, "\n")
	for index, line := range lines {
		lines[index] = "> " + line
	}
	return strings.Join(lines, "\n")
}
