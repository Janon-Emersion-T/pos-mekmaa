package main

import (
	"golang.org/x/crypto/bcrypt"
	"net"
	"net/http"
	"os"
	"strings"
)

func (s *Server) loginAllowed(r *http.Request, email string) bool {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	if os.Getenv("TRUST_PROXY") == "true" {
		if parsed := net.ParseIP(strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0])); parsed != nil {
			ip = parsed.String()
		}
	}
	for _, rule := range []struct {
		key string
		max int
	}{{"email:" + tokenHash(strings.ToLower(strings.TrimSpace(email))), 10}, {"ip:" + tokenHash(ip), 60}} {
		var attempts int
		err = s.db.QueryRowContext(r.Context(), `INSERT INTO login_limits(key,attempts,started_at) VALUES($1,1,now()) ON CONFLICT(key) DO UPDATE SET attempts=CASE WHEN login_limits.started_at<now()-interval '15 minutes' THEN 1 ELSE login_limits.attempts+1 END,started_at=CASE WHEN login_limits.started_at<now()-interval '15 minutes' THEN now() ELSE login_limits.started_at END RETURNING attempts`, rule.key).Scan(&attempts)
		if err != nil || attempts > rule.max {
			return false
		}
	}
	// Expire unused keys so arbitrary account names do not accumulate indefinitely.
	_, _ = s.db.ExecContext(r.Context(), `DELETE FROM login_limits WHERE started_at<now()-interval '1 day'`)
	return true
}
func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Current string `json:"currentPassword"`
		New     string `json:"newPassword"`
	}
	if !decode(w, r, &req) {
		return
	}
	if len(req.New) < 12 || len(req.New) > 72 || req.New == req.Current {
		problem(w, 400, "Choose a different password of 12–72 bytes")
		return
	}
	if !s.loginAllowed(r, "password-change:"+principal(r).Email) {
		w.Header().Set("Retry-After", "900")
		problem(w, 429, "Too many attempts. Try again in 15 minutes.")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		problem(w, 500, "Could not change password")
		return
	}
	defer tx.Rollback()
	var hash string
	if err = tx.QueryRowContext(r.Context(), `SELECT password_hash FROM users WHERE id=$1 FOR UPDATE`, principal(r).ID).Scan(&hash); err != nil {
		problem(w, 500, "Could not load account")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Current)) != nil {
		problem(w, 400, "Current password is incorrect")
		return
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte(req.New), bcrypt.DefaultCost)
	if err != nil {
		problem(w, 500, "Could not secure password")
		return
	}
	if auditActor(r.Context(), tx, principal(r)) != nil {
		problem(w, 500, "Could not record actor")
		return
	}
	_, err = tx.ExecContext(r.Context(), `UPDATE users SET password_hash=$1,must_change_password=false WHERE id=$2`, string(newHash), principal(r).ID)
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `DELETE FROM auth_sessions WHERE user_id=$1`, principal(r).ID)
	}
	if err != nil || tx.Commit() != nil {
		problem(w, 500, "Could not change password")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "counter_session", Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: s.cookieSecure(r), MaxAge: -1})
	respond(w, 200, map[string]bool{"ok": true})
}
