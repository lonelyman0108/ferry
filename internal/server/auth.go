package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/lonelyman0108/ferry/internal/auth"
	"github.com/lonelyman0108/ferry/internal/buildinfo"
	"github.com/lonelyman0108/ferry/internal/store"
)

const (
	sessionCookie = "ferry_session"
	sessionTTL    = 30 * 24 * time.Hour
	// 会话的最近活动时间和过期时间最多每隔这么久写一次库，避免每个请求都写。
	sessionTouchEvery = 5 * time.Minute
	apiTokenPrefix    = "fy_"
)

func actorUser(id uint) string  { return "user:" + strconv.FormatUint(uint64(id), 10) }
func actorToken(id uint) string { return "token:" + strconv.FormatUint(uint64(id), 10) }

// identity 是通过认证的调用方：管理员会话或 API token，二者只会有一个。
type identity struct {
	User      *store.User
	SessionID string
	TokenID   uint
}

func (id identity) actor() string {
	if id.User != nil {
		return actorUser(id.User.ID)
	}
	return actorToken(id.TokenID)
}

type identityKey struct{}

func identityFrom(ctx context.Context) identity {
	id, _ := ctx.Value(identityKey{}).(identity)
	return id
}

// authenticate 校验管理员会话；allowToken 为 true 时也接受 Authorization: Bearer 的 API token。
func (s *Server) authenticate(allowToken bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var id identity
			var err error
			if raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
				if !allowToken {
					writeError(w, http.StatusForbidden, "该接口仅限管理员会话调用")
					return
				}
				id, err = s.lookupAPIToken(r.Context(), raw)
			} else if c, cerr := r.Cookie(sessionCookie); cerr == nil {
				// 借用 Cookie 的跨站请求发不出 JSON 正文，所以写操作必须是 JSON。
				if r.Method == http.MethodPost && !isJSON(r) {
					writeError(w, http.StatusUnsupportedMediaType, "请求正文须为 application/json")
					return
				}
				id, err = s.lookupSession(r.Context(), c.Value)
			} else {
				err = errUnauthenticated
			}
			if errors.Is(err, errUnauthenticated) {
				writeError(w, http.StatusUnauthorized, "未登录或登录已过期")
				return
			}
			if err != nil {
				s.internalError(w, r, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, id)))
		})
	}
}

var errUnauthenticated = errors.New("未认证")

func (s *Server) lookupSession(ctx context.Context, raw string) (identity, error) {
	db := s.store().DB.WithContext(ctx)
	now := s.now()
	var sess store.Session
	err := db.Preload("User").Where("id = ? AND expires_at > ?", auth.HashToken(raw), now).First(&sess).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return identity{}, errUnauthenticated
	}
	if err != nil {
		return identity{}, err
	}
	if now.Sub(sess.LastSeenAt) > sessionTouchEvery {
		err := db.Model(&store.Session{}).Where("id = ?", sess.ID).
			Updates(map[string]any{"last_seen_at": now, "expires_at": now.Add(sessionTTL)}).Error
		if err != nil {
			return identity{}, err
		}
	}
	return identity{User: &sess.User, SessionID: sess.ID}, nil
}

func (s *Server) lookupAPIToken(ctx context.Context, raw string) (identity, error) {
	db := s.store().DB.WithContext(ctx)
	var t store.APIToken
	err := db.Where("token_hash = ? AND revoked_at IS NULL", auth.HashToken(raw)).First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return identity{}, errUnauthenticated
	}
	if err != nil {
		return identity{}, err
	}
	now := s.now()
	if err := db.Model(&t).Update("last_used_at", now).Error; err != nil {
		return identity{}, err
	}
	return identity{TokenID: t.ID}, nil
}

func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
}

type loginInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type userView struct {
	ID       uint   `json:"id"`
	Username string `json:"username"`
	// Version 是服务的版本号，侧栏底部显示；只在 /auth/me 里给出。
	Version string `json:"version,omitempty"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var in loginInput
	if !decodeJSON(w, r, &in) {
		return
	}
	ip := clientIP(r)
	if !s.limiter.allow(ip, s.now()) {
		writeError(w, http.StatusTooManyRequests, "登录失败次数过多，请稍后重试")
		return
	}
	db := s.store().DB.WithContext(r.Context())
	var u store.User
	err := db.Where(&store.User{Username: strings.TrimSpace(in.Username)}).First(&u).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		s.internalError(w, r, err)
		return
	}
	ok := false
	if err == nil {
		ok = auth.CheckPassword(u.PasswordHash, in.Password)
	} else {
		auth.BurnPasswordCheck(in.Password)
	}
	now := s.now()
	if !ok {
		s.limiter.fail(ip, now)
		s.audit(r, "anonymous", "auth.login_failed", "user:"+strings.TrimSpace(in.Username), "")
		writeError(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	s.limiter.reset(ip)

	raw, hash, err := auth.NewToken("")
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	sess := store.Session{
		ID: hash, UserID: u.ID, ExpiresAt: now.Add(sessionTTL), LastSeenAt: now,
		IP: ip, UserAgent: truncate(r.UserAgent(), 255),
	}
	if err := db.Create(&sess).Error; err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(r, actorUser(u.ID), "auth.login", "", "")
	s.setSessionCookie(w, r, raw, int(sessionTTL/time.Second))
	writeJSON(w, http.StatusOK, userView{ID: u.ID, Username: u.Username, Version: buildinfo.Version})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	id := identityFrom(r.Context())
	if err := s.store().DB.WithContext(r.Context()).Delete(&store.Session{}, "id = ?", id.SessionID).Error; err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(r, id.actor(), "auth.logout", "", "")
	s.setSessionCookie(w, r, "", -1)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u := identityFrom(r.Context()).User
	writeJSON(w, http.StatusOK, userView{ID: u.ID, Username: u.Username, Version: buildinfo.Version})
}

type passwordInput struct {
	Old string `json:"old_password"`
	New string `json:"new_password"`
}

// handleChangePassword 改密码后注销这个用户的其他会话，只保留当前这一个。
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var in passwordInput
	if !decodeJSON(w, r, &in) {
		return
	}
	id := identityFrom(r.Context())
	if !auth.CheckPassword(id.User.PasswordHash, in.Old) {
		writeError(w, http.StatusBadRequest, "原密码不正确")
		return
	}
	hash, err := auth.HashPassword(in.New)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	err = s.store().DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(id.User).Update("password_hash", hash).Error; err != nil {
			return err
		}
		return tx.Where("user_id = ? AND id <> ?", id.User.ID, id.SessionID).Delete(&store.Session{}).Error
	})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(r, id.actor(), "auth.password_changed", "", "")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// loginLimiter 按来源 IP 统计失败次数，窗口内超过上限就拒绝登录。只在内存里，重启清零。
type loginLimiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	fails  map[string]failRecord
}

type failRecord struct {
	count int
	first time.Time
}

func newLoginLimiter(max int, window time.Duration) *loginLimiter {
	return &loginLimiter{max: max, window: window, fails: map[string]failRecord{}}
}

func (l *loginLimiter) allow(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	rec, ok := l.fails[ip]
	if !ok || now.Sub(rec.first) > l.window {
		return true
	}
	return rec.count < l.max
}

func (l *loginLimiter) fail(ip string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	rec, ok := l.fails[ip]
	if !ok || now.Sub(rec.first) > l.window {
		rec = failRecord{first: now}
	}
	rec.count++
	l.fails[ip] = rec
	// 顺手清掉过期记录，防止表无限增长。
	for k, v := range l.fails {
		if now.Sub(v.first) > l.window {
			delete(l.fails, k)
		}
	}
}

func (l *loginLimiter) reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, ip)
}
