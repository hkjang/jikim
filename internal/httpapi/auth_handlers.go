package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/hkjang/jikim/internal/store"
	"github.com/hkjang/jikim/internal/version"
)

func (s *Server) version(w http.ResponseWriter, _ *http.Request) {
	writeData(w, http.StatusOK, version.Current())
}

func (s *Server) publicSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.PublicSettings(r.Context(), version.Version)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, settings)
}

// sessionStatus lets the SPA distinguish a guest from an authenticated user
// without producing an expected 401 in the browser console. Protected APIs,
// including /me, keep their normal authentication contract.
func (s *Server) sessionStatus(w http.ResponseWriter, r *http.Request) {
	token := requestToken(r)
	if token == "" {
		writeData(w, http.StatusOK, map[string]any{"authenticated": false})
		return
	}
	session, err := s.resolveSession(r.Context(), token)
	if err != nil {
		// "Not authenticated" is the answer for a token that no longer resolves.
		// A lookup that could not run has no answer, and reporting it as a guest
		// hides the outage behind a login screen.
		if !errors.Is(err, store.ErrUnauthorized) {
			s.storeError(w, r, err)
			return
		}
		writeData(w, http.StatusOK, map[string]any{"authenticated": false})
		return
	}
	writeData(w, http.StatusOK, map[string]any{"authenticated": true, "user": session.User})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	rateKey := loginRateKey(r, input.Username)
	if s.rejectRateLimitedLogin(w, r, rateKey, false) {
		return
	}
	user, err := s.authenticate(r.Context(), input.Username, input.Password)
	if err != nil {
		// A credential lookup that could not run says nothing about the
		// credential. Counting it as a failed attempt locks the account out of
		// the login window for an outage the user did not cause.
		if !errors.Is(err, store.ErrUnauthorized) {
			s.storeError(w, r, err)
			return
		}
		if s.loginLimiter != nil {
			s.loginLimiter.failed(rateKey)
		}
		// Login errors are intentionally indistinguishable.
		writeError(w, r, http.StatusUnauthorized, "invalid_credentials", "아이디 또는 비밀번호가 올바르지 않습니다")
		return
	}
	if s.loginLimiter != nil {
		s.loginLimiter.succeeded(rateKey)
	}
	security, err := s.store.SecurityConfig(r.Context())
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	if !security.AllowLocalLogin && user.Role != "admin" {
		writeError(w, r, http.StatusForbidden, "local_login_disabled", "로컬 로그인이 비활성화되었습니다")
		return
	}
	ttl := time.Duration(security.SessionTimeoutMinutes) * time.Minute
	token, session, err := s.store.CreateSession(r.Context(), user.ID, "session", "웹 로그인", user.ID, ttl)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	setSessionCookie(w, r, token, session.ExpiresAt)
	writeJSON(w, http.StatusOK, map[string]any{"user": user, "expires_at": session.ExpiresAt})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	token, _ := r.Context().Value(tokenKey).(string)
	if token != "" {
		_ = s.store.RevokeToken(r.Context(), token)
	}
	http.SetCookie(w, &http.Cookie{Name: "jikim_session", Value: "", Path: "/", HttpOnly: true,
		Secure: requestIsHTTPS(r), SameSite: http.SameSiteStrictMode, MaxAge: -1, Expires: time.Unix(0, 0)})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	session, _ := sessionFrom(r)
	writeData(w, http.StatusOK, session.User)
}

func (s *Server) updateMe(w http.ResponseWriter, r *http.Request) {
	var input struct {
		DisplayName string `json:"display_name"`
		Email       string `json:"email"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	session, _ := sessionFrom(r)
	user, err := s.store.UpdateProfile(r.Context(), session.User.ID, input.DisplayName, input.Email)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, user)
}

func (s *Server) changeMyPassword(w http.ResponseWriter, r *http.Request) {
	var input struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	session, _ := sessionFrom(r)
	security, err := s.store.SecurityConfig(r.Context())
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	if len(input.NewPassword) < security.PasswordMinLength {
		writeError(w, r, http.StatusBadRequest, "weak_password", "새 비밀번호가 관리자의 최소 길이 정책보다 짧습니다")
		return
	}
	if err := s.store.ChangePassword(r.Context(), session.User.ID, input.CurrentPassword, input.NewPassword, false); err != nil {
		s.storeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"changed": true})
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{Name: "jikim_session", Value: token, Path: "/", HttpOnly: true,
		Secure: requestIsHTTPS(r), SameSite: http.SameSiteStrictMode, Expires: expires, MaxAge: int(time.Until(expires).Seconds())})
}

func requestIsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]), "https")
}
