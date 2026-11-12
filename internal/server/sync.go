package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/lonelyman0108/ferry/internal/ingest"
	"github.com/lonelyman0108/ferry/internal/store"
)

// ingest 用当前的数据库和主密钥构造回传处理服务。
func (s *Server) ingest() (*ingest.Service, error) {
	b, err := s.box()
	if err != nil {
		return nil, err
	}
	return &ingest.Service{DB: s.store().DB, Box: b, Bus: s.Bus, Now: s.now, HTTP: s.HTTP}, nil
}

func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	var in ingest.SyncInput
	if !decodeJSON(w, r, &in) {
		return
	}
	m := r.Context().Value(subscriptionKey{}).(store.Subscription)
	status, body, err := s.syncReply(r, m, in)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, status, body)
}

// syncReply 处理一次回传，返回状态码和正文；明文和加密两个入口共用。返回 err 时调用方按内部错误处理。
func (s *Server) syncReply(r *http.Request, m store.Subscription, in ingest.SyncInput) (int, any, error) {
	svc, err := s.ingest()
	if err != nil {
		return 0, nil, err
	}
	res, err := svc.Sync(r.Context(), m, in)
	var bad *ingest.BadInputError
	switch {
	case errors.Is(err, ingest.ErrUnknownSource):
		return http.StatusNotFound, errorBody{Error: err.Error()}, nil
	case errors.As(err, &bad):
		return http.StatusBadRequest, errorBody{Error: bad.Error()}, nil
	case err != nil:
		return 0, nil, err
	}
	if res.Result != ingest.ResultUnchanged {
		// 审计只记字段名和结果，不记值。
		keys := make([]string, 0, len(in.Fields))
		for k := range in.Fields {
			keys = append(keys, k)
		}
		detail := joinSorted(keys)
		// 收下了新值、但有字段没通过时，结果记为 accepted，没通过的字段单独写出来。
		if res.Result == ingest.ResultAccepted && len(res.Rejected) > 0 {
			bad := make([]string, 0, len(res.Rejected))
			for k := range res.Rejected {
				bad = append(bad, k)
			}
			detail += "；未通过校验：" + joinSorted(bad)
		}
		s.audit(r, "subscription:"+strconv.FormatUint(uint64(m.ID), 10), "sync."+res.Result, "source:"+in.Source, detail)
	}
	return http.StatusOK, res, nil
}

// RunWorkers 在后台定时处理到期的回传批次，每天按保留策略清理一次旧版本，直到 ctx 结束。未初始化时跳过。
func (s *Server) RunWorkers(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	var lastCleanup, lastExpiryCheck, lastNonceCleanup time.Time
	for {
		if s.store() != nil && time.Since(lastNonceCleanup) > 10*time.Minute {
			if _, err := s.cleanupNonces(ctx); err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("清理过期的 nonce 记录失败", "err", err)
			}
			lastNonceCleanup = time.Now()
		}
		if s.store() != nil && time.Since(lastExpiryCheck) > time.Hour {
			if svc, err := s.ingest(); err == nil {
				if _, err := svc.CheckExpiring(ctx); err != nil {
					slog.Error("检查即将过期的值失败", "err", err)
				}
				lastExpiryCheck = time.Now()
			}
		}
		if s.store() != nil && time.Since(lastCleanup) > 24*time.Hour {
			if svc, err := s.ingest(); err == nil {
				if n, err := svc.Cleanup(ctx); err != nil {
					slog.Error("清理旧版本失败", "err", err)
				} else if n > 0 {
					slog.Info("按保留策略清理了旧版本", "count", n)
				}
				lastCleanup = time.Now()
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if s.store() == nil {
				continue
			}
			svc, err := s.ingest()
			if err != nil {
				continue
			}
			if _, err := svc.ProcessDue(ctx); err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("处理回传批次失败", "err", err)
			}
			if p, err := s.pusher(); err == nil {
				if _, err := p.ProcessDue(ctx); err != nil && !errors.Is(err, context.Canceled) {
					slog.Error("执行推送任务失败", "err", err)
				}
			}
		}
	}
}

// rateLimiter 是按键计数的固定窗口限流，只在内存里。
type rateLimiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	counts map[string]windowCount
}

type windowCount struct {
	start time.Time
	n     int
}

func newRateLimiter(max int, window time.Duration) *rateLimiter {
	return &rateLimiter{max: max, window: window, counts: map[string]windowCount{}}
}

func (l *rateLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	c := l.counts[key]
	if now.Sub(c.start) >= l.window {
		c = windowCount{start: now}
		if len(l.counts) > 10000 {
			for k, v := range l.counts {
				if now.Sub(v.start) >= l.window {
					delete(l.counts, k)
				}
			}
		}
	}
	c.n++
	l.counts[key] = c
	return c.n <= l.max
}

// limitByIP 按来源 IP 限流手机端接口。
func (s *Server) limitByIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.phoneLimiter.allow(clientIP(r), s.now()) {
			writeError(w, http.StatusTooManyRequests, "请求过于频繁")
			return
		}
		next.ServeHTTP(w, r)
	})
}
