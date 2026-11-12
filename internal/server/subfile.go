package server

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"github.com/lonelyman0108/ferry/internal/auth"
	"github.com/lonelyman0108/ferry/internal/source"
	"github.com/lonelyman0108/ferry/internal/store"
	"github.com/lonelyman0108/ferry/internal/sub"
)

// handleSubscriptionFile 返回订阅文件，路径是 /sub/{token}/{文件名}。内容实时生成，
// 来源增删后客户端刷新订阅即可拿到新的 MITM 域名。
func (s *Server) handleSubscriptionFile(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	db := s.store().DB.WithContext(r.Context())
	var m store.Subscription
	err := db.Where("token_hash = ?", auth.HashToken(token)).First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && chi.URLParam(r, "file") != sub.FileName(m.Client)) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	p, err := s.subscriptionParams(r, m, token)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out, err := sub.Generate(p)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	// 记下客户端拿到的是哪一版 MITM 域名，界面据此显示“已安装”和“待更新”；时间按分钟更新。
	now := s.now()
	if hash := mitmHash(p.Sources); m.FileFetchedAt == nil || now.Sub(*m.FileFetchedAt) > time.Minute || m.FileMitmHash != hash {
		_ = db.Model(&m).Updates(map[string]any{"file_fetched_at": now, "file_mitm_hash": hash}).Error
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(out)
}

// handleSubscriptionPreview 给管理界面看订阅文件的内容和 MITM 域名；不算客户端下载，不改安装状态。
func (s *Server) handleSubscriptionPreview(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "ID 不合法")
		return
	}
	var m store.Subscription
	if err := s.store().DB.WithContext(r.Context()).First(&m, id).Error; err != nil {
		s.writeFindError(w, r, err, "订阅不存在")
		return
	}
	b, err := s.box()
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	token, err := b.Open(m.TokenSealed, subscriptionAAD(m.ID))
	if err != nil {
		s.internalError(w, r, fmt.Errorf("解密订阅令牌：%w", err))
		return
	}
	p, err := s.subscriptionParams(r, m, string(token))
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out, err := sub.Generate(p)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"file_name": sub.FileName(m.Client), "content": string(out), "hostnames": source.Hostnames(p.Sources)})
}

// subscriptionParams 准备生成订阅文件的参数；token 会写进明文订阅的脚本参数里。
func (s *Server) subscriptionParams(r *http.Request, m store.Subscription, token string) (sub.Params, error) {
	db := s.store().DB.WithContext(r.Context())
	var primary store.Endpoint
	if err := db.First(&primary, m.EndpointID).Error; err != nil {
		return sub.Params{}, err
	}
	p := sub.Params{Name: m.Name, Client: m.Client, BaseURL: primary.BaseURL, ScriptURL: m.ScriptURL, Token: token}
	if m.TransportMode == store.TransportSealed && m.TransportKID != nil {
		p.KID = *m.TransportKID
	}
	if m.FallbackID != nil {
		var fb store.Endpoint
		if err := db.First(&fb, *m.FallbackID).Error; err == nil {
			p.FallbackURL = fb.BaseURL
		}
	}
	var err error
	p.Sources, err = s.subscriptionDocs(r, m)
	return p, err
}
