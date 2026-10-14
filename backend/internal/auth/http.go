package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"campusclaw/internal/config"
)

const bearerScheme = "Bearer"

type LoginResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	ClassID  *int64 `json:"class_id"`
}
type Service struct {
	DB      *sql.DB
	Config  config.Config
	Limiter *Limiter
	dummy   string
}
type userKey struct{}

func Current(r *http.Request) *User { u, _ := r.Context().Value(userKey{}).(*User); return u }
func New(db *sql.DB, c config.Config) (*Service, error) {
	dummy, err := HashPassword("non-user-fixture-password")
	if err != nil {
		return nil, err
	}
	return &Service{db, c, NewLimiter(c.MaxFailures, time.Duration(c.WindowSeconds)*time.Second, time.Duration(c.LockSeconds)*time.Second), dummy}, nil
}
func JSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func Error(w http.ResponseWriter, status int, code string) {
	JSON(w, status, map[string]string{"error": code})
}
func (s *Service) key(id string) string {
	mac := hmac.New(sha256.New, []byte(s.Config.SessionSecret))
	_, _ = mac.Write([]byte(id))
	return hex.EncodeToString(mac.Sum(nil))
}
func bearerToken(r *http.Request) (string, bool) {
	values := r.Header.Values("Authorization")
	if len(values) != 1 {
		return "", false
	}
	parts := strings.Fields(values[0])
	if len(parts) != 2 || !strings.EqualFold(parts[0], bearerScheme) {
		return "", false
	}
	if len(parts[1]) != 43 {
		return "", false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(decoded) != 32 {
		return "", false
	}
	return parts[1], true
}
func (s *Service) Load(r *http.Request) (*User, error) {
	token, ok := bearerToken(r)
	if !ok {
		return nil, nil
	}
	var u User
	err := s.DB.QueryRowContext(r.Context(), `SELECT u.id,u.username,u.role,u.class_id FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.id=? AND s.expires_at>?`, s.key(token), time.Now().Unix()).Scan(&u.ID, &u.Username, &u.Role, &u.ClassID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if u.Role != "teacher" && u.Role != "student" {
		return nil, nil
	}
	return &u, nil
}
func (s *Service) Require(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, err := s.Load(r)
		if err != nil {
			Error(w, 503, "service_unavailable")
			return
		}
		if u == nil {
			w.Header().Set("WWW-Authenticate", bearerScheme)
			Error(w, 401, "authentication_required")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userKey{}, u)))
	}
}
func (s *Service) Class(next http.HandlerFunc) http.HandlerFunc {
	return s.Require(func(w http.ResponseWriter, r *http.Request) {
		if Current(r).ClassID == nil {
			Error(w, 403, "class_membership_required")
			return
		}
		next(w, r)
	})
}
func (s *Service) Teacher(next http.HandlerFunc) http.HandlerFunc {
	return s.Class(func(w http.ResponseWriter, r *http.Request) {
		if Current(r).Role != "teacher" {
			Error(w, 403, "teacher_role_required")
			return
		}
		next(w, r)
	})
}
func (s *Service) IP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return "unknown"
	}
	for _, prefix := range s.Config.TrustedProxies {
		if prefix.Contains(ip.Unmap()) {
			if forwarded, err := netip.ParseAddr(r.Header.Get("X-Real-IP")); err == nil {
				return forwarded.Unmap().String()
			}
		}
	}
	return ip.Unmap().String()
}
// @Route POST /api/login
// @Summary 登录并返回新的不透明Bearer会话令牌
// @Role public
// @Request LoginRequest
// @Response 200 LoginResponse
// @Error 400 invalid_login_request
// @Error 401 invalid_credentials
// @Error 500 internal_error
// @Error 503 service_unavailable
func (s *Service) Login(w http.ResponseWriter, r *http.Request) {
	contentType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if contentType != "application/json" {
		Error(w, 400, "invalid_login_request")
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
	if err := decoder.Decode(&input); err != nil {
		Error(w, 400, "invalid_login_request")
		return
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF || strings.TrimSpace(input.Username) == "" || strings.TrimSpace(input.Password) == "" {
		Error(w, 400, "invalid_login_request")
		return
	}
	username := strings.TrimSpace(input.Username)
	key := username + "\x00" + s.IP(r)
	now := time.Now()
	var id int64
	var hash, role string
	err := s.DB.QueryRowContext(r.Context(), "SELECT id,password_hash,role FROM users WHERE username=?", username).Scan(&id, &hash, &role)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		Error(w, 503, "service_unavailable")
		return
	}
	blocked := s.Limiter.Blocked(key, now)
	valid := false
	if err == nil && !blocked && (role == "teacher" || role == "student") {
		valid = VerifyPassword(hash, input.Password)
	} else {
		_ = VerifyPassword(s.dummy, input.Password)
	}
	if !valid || blocked {
		s.Limiter.Fail(key, now)
		Error(w, 401, "invalid_credentials")
		return
	}
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		Error(w, 500, "internal_error")
		return
	}
	session := base64.RawURLEncoding.EncodeToString(raw)
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		Error(w, 503, "service_unavailable")
		return
	}
	defer tx.Rollback()
	old := ""
	if token, ok := bearerToken(r); ok {
		old = s.key(token)
	}
	if _, err = tx.ExecContext(r.Context(), "DELETE FROM sessions WHERE expires_at<=? OR id=?", now.Unix(), old); err != nil {
		Error(w, 503, "service_unavailable")
		return
	}
	if _, err = tx.ExecContext(r.Context(), "INSERT INTO sessions(id,user_id,created_at,expires_at) VALUES(?,?,?,?)", s.key(session), id, now.Unix(), now.Unix()+int64(s.Config.SessionTTL)); err != nil {
		Error(w, 503, "service_unavailable")
		return
	}
	if NeedsUpgrade(hash, input.Password) {
		upgraded, e := HashPassword(input.Password)
		if e != nil {
			Error(w, 500, "internal_error")
			return
		}
		if _, err = tx.ExecContext(r.Context(), "UPDATE users SET password_hash=? WHERE id=? AND password_hash=?", upgraded, id, hash); err != nil {
			Error(w, 503, "service_unavailable")
			return
		}
	}
	if err = tx.Commit(); err != nil {
		Error(w, 503, "service_unavailable")
		return
	}
	s.Limiter.Reset(key)
	JSON(w, 200, LoginResponse{AccessToken: session, TokenType: bearerScheme, ExpiresIn: s.Config.SessionTTL})
}
// @Route GET /api/me
// @Summary 当前身份
// @Role authenticated
// @Response 200 User
func (s *Service) Me(w http.ResponseWriter, r *http.Request) { JSON(w, 200, Current(r)) }
// @Route POST /api/logout
// @Summary 撤销当前令牌
// @Role authenticated
// @Response 200 Status
func (s *Service) Logout(w http.ResponseWriter, r *http.Request) {
	u, err := s.Load(r)
	if err != nil {
		Error(w, 503, "service_unavailable")
		return
	}
	if u == nil {
		w.Header().Set("WWW-Authenticate", bearerScheme)
		Error(w, 401, "authentication_required")
		return
	}
	token, _ := bearerToken(r)
	if _, err = s.DB.ExecContext(r.Context(), "DELETE FROM sessions WHERE id=?", s.key(token)); err != nil {
		Error(w, 503, "service_unavailable")
		return
	}
	JSON(w, 200, map[string]string{"status": "ok"})
}
