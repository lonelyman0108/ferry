// Package server 组装 HTTP 路由，并持有配置和数据库这些运行时状态。
// 未初始化时只开放初始化向导；向导完成后无需重启即切换到正常模式。
package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/lonelyman0108/ferry/internal/auth"
	"github.com/lonelyman0108/ferry/internal/config"
	"github.com/lonelyman0108/ferry/internal/events"
	"github.com/lonelyman0108/ferry/internal/ingest"
	"github.com/lonelyman0108/ferry/internal/store"
	"github.com/lonelyman0108/ferry/script"
)

// Options 是创建 Server 所需的参数。
type Options struct {
	// ConfigPath 是初始化向导写入配置的位置。
	ConfigPath string
	// Config 是已加载的配置；未初始化时可以只有 Listen 等部分字段。
	Config *config.Config
	// Store 是已连接并迁移好的数据库；没有配置时为 nil，由向导创建。
	Store *store.Store
}

// Server 是 Ferry 的 HTTP 服务。
type Server struct {
	configPath string

	mu         sync.RWMutex
	cfg        *config.Config
	st         *store.Store
	ownsStore  bool
	setupToken string

	// setupMu 保证同一时刻只有一个初始化请求在执行。
	setupMu sync.Mutex
	limiter *loginLimiter
	// phoneLimiter 限制手机端接口（规则、回传）的请求频率。
	phoneLimiter *rateLimiter
	// sealedFailLimiter 限制加密请求解不开时写审计的频率。
	sealedFailLimiter *rateLimiter
	// Bus 发出回传处理产生的事件，目的地推送和通知订阅它。
	Bus *events.Bus
	// HTTP 用于访问目的地和主动探测；为 nil 时用各自的默认客户端。测试里可以替换。
	HTTP *http.Client
	now  func() time.Time
}

// New 创建 Server。需要初始化时会生成一次性的向导令牌，调用方应把它打印到日志。
func New(o Options) (*Server, error) {
	cfg := o.Config
	if cfg == nil {
		cfg = &config.Config{}
	}
	s := &Server{
		configPath:        o.ConfigPath,
		cfg:               cfg,
		st:                o.Store,
		limiter:           newLoginLimiter(5, 15*time.Minute),
		phoneLimiter:      newRateLimiter(120, time.Minute),
		sealedFailLimiter: newRateLimiter(1, 10*time.Minute),
		Bus:               &events.Bus{},
		now:               time.Now,
	}
	// 当前值变化时排推送任务；所有事件按通知规则发通知。未初始化时直接忽略。
	s.Bus.Subscribe(func(e events.Event) {
		if p, err := s.pusher(); err == nil {
			p.OnEvent(e)
		}
	})
	s.Bus.Subscribe(func(e events.Event) {
		if n, err := s.notifier(); err == nil {
			n.OnEvent(e)
		}
	})
	st, err := s.setupState(context.Background())
	if err != nil {
		return nil, err
	}
	if st.NeedsConfig || st.NeedsAdmin {
		raw, _, err := auth.NewToken("")
		if err != nil {
			return nil, err
		}
		s.setupToken = raw
	}
	return s, nil
}

// SetupToken 返回初始化向导令牌；已初始化时为空。
func (s *Server) SetupToken() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.setupToken
}

// Close 关闭由向导打开的数据库连接；调用方传入的 Store 由调用方自己关闭。
func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ownsStore && s.st != nil {
		return s.st.Close()
	}
	return nil
}

func (s *Server) store() *store.Store {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.st
}

// Handler 返回挂好全部路由的 handler。
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	// 不用 middleware.RealIP：它无条件信任 X-Forwarded-For，会让登录限流被伪造的头绕过。
	r.Use(middleware.Recoverer)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	})

	r.With(s.requireReady).Get("/sub/{token}/{file}", s.handleSubscriptionFile)
	r.Get("/script/ferry.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=300")
		_, _ = w.Write(script.FerryJS)
	})

	ui := uiHandler()
	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		// 接口路径不存在时返回 JSON 404，其余交给管理界面。
		if strings.HasPrefix(req.URL.Path, "/api/") {
			writeError(w, http.StatusNotFound, "接口不存在")
			return
		}
		ui.ServeHTTP(w, req)
	})

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/setup/status", s.handleSetupStatus)
		r.Group(func(r chi.Router) {
			r.Use(s.requireSetupToken)
			r.Post("/setup/database", s.handleSetupTestDatabase)
			r.Post("/setup", s.handleSetup)
		})

		r.Group(func(r chi.Router) {
			r.Use(s.requireReady)
			r.Post("/auth/login", s.handleLogin)
			r.Group(func(r chi.Router) {
				r.Use(s.authenticate(false))
				r.Post("/auth/logout", s.handleLogout)
				r.Get("/auth/me", s.handleMe)
				r.Post("/auth/password", s.handleChangePassword)
				r.Get("/tokens", s.handleListTokens)
				r.Post("/tokens", s.handleCreateToken)
				r.Delete("/tokens/{id}", s.handleRevokeToken)
			})
			r.Group(func(r chi.Router) {
				r.Use(s.authenticate(false))
				r.Post("/sources", s.handleCreateSource)
				r.Post("/sources/import", s.handleImportSource)
				r.Put("/sources/{id}", s.handleUpdateSource)
				r.Delete("/sources/{id}", s.handleDeleteSource)
				r.Post("/sources/try", s.handleTrySource)
				r.Post("/examples/{key}/import", s.handleImportExample)
				r.Post("/endpoints", s.handleCreateEndpoint)
				r.Put("/endpoints/{id}", s.handleUpdateEndpoint)
				r.Delete("/endpoints/{id}", s.handleDeleteEndpoint)
				r.Get("/subscriptions", s.handleListSubscriptions)
				r.Post("/subscriptions", s.handleCreateSubscription)
				r.Get("/subscriptions/{id}", s.handleGetSubscription)
				r.Get("/subscriptions/{id}/preview", s.handleSubscriptionPreview)
				r.Put("/subscriptions/{id}", s.handleUpdateSubscription)
				r.Delete("/subscriptions/{id}", s.handleDeleteSubscription)
				r.Post("/subscriptions/{id}/rotate", s.handleRotateSubscription)
				r.Post("/subscriptions/{id}/transport-key/reveal", s.handleRevealTransportKey)
				r.Post("/subscriptions/{id}/transport-key/rotate", s.handleRotateTransportKey)
				r.Post("/versions/{id}/reveal", s.handleRevealVersion)
				r.Get("/destinations", s.handleListDestinations)
				r.Post("/destinations", s.handleCreateDestination)
				r.Put("/destinations/{id}", s.handleUpdateDestination)
				r.Delete("/destinations/{id}", s.handleDeleteDestination)
				r.Post("/destinations/{id}/test", s.handleTestDestination)
				r.Get("/bindings", s.handleListBindings)
				r.Post("/bindings", s.handleCreateBinding)
				r.Put("/bindings/{id}", s.handleUpdateBinding)
				r.Delete("/bindings/{id}", s.handleDeleteBinding)
				r.Get("/pushes", s.handleListPushes)
				r.Post("/pushes", s.handleManualPush)
				r.Post("/pushes/{id}/retry", s.handleRetryPush)
				r.Get("/identities/{id}/pushes", s.handleIdentityPushes)
				r.Get("/channels", s.handleListChannels)
				r.Post("/channels", s.handleCreateChannel)
				r.Put("/channels/{id}", s.handleUpdateChannel)
				r.Delete("/channels/{id}", s.handleDeleteChannel)
				r.Post("/channels/{id}/test", s.handleTestChannel)
				r.Get("/notify-rules", s.handleListRules)
				r.Post("/notify-rules", s.handleCreateRule)
				r.Put("/notify-rules/{id}", s.handleUpdateRule)
				r.Delete("/notify-rules/{id}", s.handleDeleteRule)
				r.Post("/versions/{id}/approve", s.versionAction("version.approve", func(svc *ingest.Service, r *http.Request, id uint) error { return svc.Approve(r.Context(), id) }))
				r.Post("/versions/{id}/reject", s.versionAction("version.reject", func(svc *ingest.Service, r *http.Request, id uint) error { return svc.Reject(r.Context(), id) }))
				r.Post("/versions/{id}/activate", s.versionAction("version.activate", func(svc *ingest.Service, r *http.Request, id uint) error { return svc.Activate(r.Context(), id) }))
				r.Post("/batches/{id}/process", s.handleProcessBatch)
			})
			r.Group(func(r chi.Router) {
				r.Use(s.authenticate(true))
				r.Get("/audit", s.handleListAudit)
				r.Get("/overview", s.handleOverview)
				r.Get("/flow", s.handleFlow)
				r.Get("/activity", s.handleActivity)
				r.Get("/sources/{id}/lane", s.handleSourceLane)
				r.Get("/sources", s.handleListSources)
				r.Get("/sources/{id}", s.handleGetSource)
				r.Get("/sources/{id}/export", s.handleExportSource)
				r.Get("/examples", s.handleListExamples)
				r.Get("/endpoints", s.handleListEndpoints)
				r.Get("/identities", s.handleListIdentities)
				r.Get("/identities/{id}", s.handleGetIdentity)
				r.Get("/identities/{id}/versions", s.handleListVersions)
				r.Get("/batches", s.handleListBatches)
			})
			r.Group(func(r chi.Router) {
				r.Use(s.limitByIP, s.authenticateSubscription)
				r.Get("/rules", s.handleRules)
				r.Post("/sync", s.handleSync)
			})
			r.With(s.limitByIP).Post("/sealed", s.handleSealed)
		})
	})
	return r
}

// requireReady 在初始化完成前拒绝除向导以外的接口。
func (s *Server) requireReady(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		st, err := s.setupState(r.Context())
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		if st.NeedsConfig || st.NeedsAdmin {
			writeError(w, http.StatusServiceUnavailable, "尚未初始化，需完成初始化向导")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) internalError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, context.Canceled) {
		return
	}
	slog.Error("处理请求出错", "method", r.Method, "path", r.URL.Path, "err", err)
	writeError(w, http.StatusInternalServerError, "服务器内部错误")
}
