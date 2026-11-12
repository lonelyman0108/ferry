package server

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/lonelyman0108/ferry/internal/auth"
	"github.com/lonelyman0108/ferry/internal/store"
)

type tokenView struct {
	ID         uint       `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
}

func viewToken(t store.APIToken) tokenView {
	return tokenView{ID: t.ID, Name: t.Name, Prefix: t.Prefix, CreatedAt: t.CreatedAt, LastUsedAt: t.LastUsedAt, RevokedAt: t.RevokedAt}
}

func (s *Server) handleListTokens(w http.ResponseWriter, r *http.Request) {
	var ts []store.APIToken
	if err := s.store().DB.WithContext(r.Context()).Order("id DESC").Find(&ts).Error; err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]tokenView, 0, len(ts))
	for _, t := range ts {
		out = append(out, viewToken(t))
	}
	writeJSON(w, http.StatusOK, out)
}

type createTokenInput struct {
	Name string `json:"name"`
}

type createdToken struct {
	tokenView
	// Token 是令牌原文，只在创建时返回这一次。
	Token string `json:"token"`
}

func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	var in createTokenInput
	if !decodeJSON(w, r, &in) {
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > 64 {
		writeError(w, http.StatusBadRequest, "名称不能为空，且不超过 64 个字节")
		return
	}
	raw, hash, err := auth.NewToken(apiTokenPrefix)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	t := store.APIToken{Name: name, Prefix: raw[:len(apiTokenPrefix)+6], TokenHash: hash}
	if err := s.store().DB.WithContext(r.Context()).Create(&t).Error; err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(r, identityFrom(r.Context()).actor(), "token.create", actorToken(t.ID), name)
	writeJSON(w, http.StatusCreated, createdToken{tokenView: viewToken(t), Token: raw})
}

func (s *Server) handleRevokeToken(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "令牌 ID 不合法")
		return
	}
	res := s.store().DB.WithContext(r.Context()).Model(&store.APIToken{}).
		Where("id = ? AND revoked_at IS NULL", id).Update("revoked_at", s.now())
	if res.Error != nil {
		s.internalError(w, r, res.Error)
		return
	}
	if res.RowsAffected == 0 {
		writeError(w, http.StatusNotFound, "令牌不存在或已吊销")
		return
	}
	s.audit(r, identityFrom(r.Context()).actor(), "token.revoke", actorToken(uint(id)), "")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
