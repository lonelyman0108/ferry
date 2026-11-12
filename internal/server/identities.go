package server

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/lonelyman0108/ferry/internal/ingest"
	"github.com/lonelyman0108/ferry/internal/store"
)

func joinSorted(keys []string) string {
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

// mask 只露出开头和结尾各 4 个字符；太短的值整体打码。
func mask(v string) string {
	if utf8.RuneCountInString(v) <= 12 {
		return "****"
	}
	r := []rune(v)
	return string(r[:4]) + "…" + string(r[len(r)-4:])
}

type versionView struct {
	ID          uint       `json:"id"`
	Field       string     `json:"field"`
	Value       string     `json:"value"`
	Masked      bool       `json:"masked"`
	Status      string     `json:"status"`
	Reason      string     `json:"reason,omitempty"`
	Current     bool       `json:"current"`
	ExpiresAt   *time.Time `json:"expires_at"`
	SeenCount   int        `json:"seen_count"`
	LastSeenAt  time.Time  `json:"last_seen_at"`
	ActivatedAt *time.Time `json:"activated_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

func (s *Server) viewVersion(v store.Version, f store.Field) (versionView, error) {
	out := versionView{
		ID: v.ID, Field: f.Key, Status: v.Status, Reason: v.Reason, Current: v.Current, ExpiresAt: v.ExpiresAt,
		SeenCount: v.SeenCount, LastSeenAt: v.LastSeenAt, ActivatedAt: v.ActivatedAt, CreatedAt: v.CreatedAt,
	}
	svc, err := s.ingest()
	if err != nil {
		return out, err
	}
	val, err := svc.Reveal(v)
	if err != nil {
		return out, err
	}
	if f.Sensitive {
		out.Value, out.Masked = mask(val), true
	} else {
		out.Value = val
	}
	return out, nil
}

type identityView struct {
	ID         uint      `json:"id"`
	SourceID   uint      `json:"source_id"`
	SourceKey  string    `json:"source_key"`
	SourceName string    `json:"source_name"`
	Key        string    `json:"key"`
	LastSeenAt time.Time `json:"last_seen_at"`
	CreatedAt  time.Time `json:"created_at"`
	// Pending 是待审核的版本数，ExpiresAt 是当前值里最早的过期时间；只在列表里给出。
	Pending   int64      `json:"pending,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	// PushStatus 是推送状态（取值见 pushState，另有 none），来源没有可用的推送规则时为空；PushedAt 是最近一次推送成功的时间。
	// 只在列表里给出。
	PushStatus string          `json:"push_status,omitempty"`
	PushedAt   *time.Time      `json:"pushed_at,omitempty"`
	Fields     []identityField `json:"fields,omitempty"`
}

type identityField struct {
	Key       string       `json:"key"`
	Name      string       `json:"name"`
	Sensitive bool         `json:"sensitive"`
	Current   *versionView `json:"current"`
	Pending   int64        `json:"pending"`
}

func (s *Server) handleListIdentities(w http.ResponseWriter, r *http.Request) {
	db := s.store().DB.WithContext(r.Context())
	q := db.Order("id")
	if key := r.URL.Query().Get("source"); key != "" {
		var src store.Source
		if err := db.Where(map[string]any{"key": key}).First(&src).Error; err != nil {
			s.writeFindError(w, r, err, "来源不存在")
			return
		}
		q = q.Where("source_id = ?", src.ID)
	}
	var idents []store.Identity
	if err := q.Find(&idents).Error; err != nil {
		s.internalError(w, r, err)
		return
	}
	var srcs []store.Source
	if err := db.Find(&srcs).Error; err != nil {
		s.internalError(w, r, err)
		return
	}
	byID := map[uint]store.Source{}
	for _, x := range srcs {
		byID[x.ID] = x
	}
	type identityCount struct {
		IdentityID uint
		N          int64
	}
	var pending []identityCount
	if err := db.Model(&store.Version{}).Select("identity_id, COUNT(*) AS n").Where("status = ? AND identity_id IS NOT NULL", store.VersionPendingReview).
		Group("identity_id").Scan(&pending).Error; err != nil {
		s.internalError(w, r, err)
		return
	}
	pendingOf := map[uint]int64{}
	for _, c := range pending {
		pendingOf[c.IdentityID] = c.N
	}
	var current []store.Version
	if err := db.Select("id", "identity_id", "expires_at").Where("current = ? AND expires_at IS NOT NULL AND identity_id IS NOT NULL", true).Find(&current).Error; err != nil {
		s.internalError(w, r, err)
		return
	}
	expiresOf := map[uint]*time.Time{}
	for _, v := range current {
		if e := expiresOf[*v.IdentityID]; e == nil || v.ExpiresAt.Before(*e) {
			expiresOf[*v.IdentityID] = v.ExpiresAt
		}
	}
	pushStates, pushedAt, err := identityPushStates(db, idents)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]identityView, 0, len(idents))
	for _, a := range idents {
		src := byID[a.SourceID]
		out = append(out, identityView{ID: a.ID, SourceID: a.SourceID, SourceKey: src.Key, SourceName: src.Name, Key: a.Key, LastSeenAt: a.LastSeenAt, CreatedAt: a.CreatedAt,
			Pending: pendingOf[a.ID], ExpiresAt: expiresOf[a.ID], PushStatus: pushStates[a.ID], PushedAt: pushedAt[a.ID]})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetIdentity(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "ID 不合法")
		return
	}
	db := s.store().DB.WithContext(r.Context())
	var a store.Identity
	if err := db.First(&a, id).Error; err != nil {
		s.writeFindError(w, r, err, "身份不存在")
		return
	}
	view, err := s.identityDetail(db, a)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// identityDetail 是身份详情：每个字段的当前值（敏感字段打码）和待审核的版本数。
func (s *Server) identityDetail(db *gorm.DB, a store.Identity) (identityView, error) {
	var src store.Source
	if err := db.Preload("Fields", func(tx *gorm.DB) *gorm.DB { return tx.Order("position") }).First(&src, a.SourceID).Error; err != nil {
		return identityView{}, err
	}
	view := identityView{ID: a.ID, SourceID: src.ID, SourceKey: src.Key, SourceName: src.Name, Key: a.Key, LastSeenAt: a.LastSeenAt, CreatedAt: a.CreatedAt, Fields: []identityField{}}
	for _, f := range src.Fields {
		af := identityField{Key: f.Key, Name: f.Name, Sensitive: f.Sensitive}
		var cur store.Version
		if err := db.Where("identity_id = ? AND field_id = ? AND current = ?", a.ID, f.ID, true).Limit(1).Find(&cur).Error; err != nil {
			return view, err
		}
		if cur.ID != 0 {
			v, err := s.viewVersion(cur, f)
			if err != nil {
				return view, err
			}
			af.Current = &v
		}
		if err := db.Model(&store.Version{}).Where("identity_id = ? AND field_id = ? AND status = ?", a.ID, f.ID, store.VersionPendingReview).Count(&af.Pending).Error; err != nil {
			return view, err
		}
		view.Fields = append(view.Fields, af)
	}
	return view, nil
}

// handleListVersions 列出身份某个字段（或全部字段）的版本，按时间倒序分页。
func (s *Server) handleListVersions(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "ID 不合法")
		return
	}
	db := s.store().DB.WithContext(r.Context())
	var a store.Identity
	if err := db.First(&a, id).Error; err != nil {
		s.writeFindError(w, r, err, "身份不存在")
		return
	}
	var fields []store.Field
	if err := db.Where("source_id = ?", a.SourceID).Find(&fields).Error; err != nil {
		s.internalError(w, r, err)
		return
	}
	byID := map[uint]store.Field{}
	q := db.Where("identity_id = ?", a.ID).Order("id DESC").Limit(100)
	field, matched := r.URL.Query().Get("field"), false
	for _, f := range fields {
		byID[f.ID] = f
		if f.Key == field {
			q, matched = q.Where("field_id = ?", f.ID), true
		}
	}
	// 字段写错时报错，不退回成列出全部字段。
	if field != "" && !matched {
		writeError(w, http.StatusBadRequest, "来源没有这个字段："+field)
		return
	}
	if v := r.URL.Query().Get("before"); v != "" {
		before, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "before 不合法")
			return
		}
		q = q.Where("id < ?", before)
	}
	var vs []store.Version
	if err := q.Find(&vs).Error; err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]versionView, 0, len(vs))
	for _, v := range vs {
		view, err := s.viewVersion(v, byID[v.FieldID])
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		out = append(out, view)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleRevealVersion 返回版本的原值，只限管理员会话，每次都写审计。
func (s *Server) handleRevealVersion(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "ID 不合法")
		return
	}
	db := s.store().DB.WithContext(r.Context())
	var v store.Version
	if err := db.First(&v, id).Error; err != nil {
		s.writeFindError(w, r, err, "版本不存在")
		return
	}
	svc, err := s.ingest()
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	val, err := svc.Reveal(v)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(r, identityFrom(r.Context()).actor(), "version.reveal", "version:"+strconv.FormatUint(uint64(v.ID), 10), "")
	writeJSON(w, http.StatusOK, map[string]string{"value": val})
}

// versionAction 执行通过、驳回、回滚这类改版本状态的操作，并写审计。
func (s *Server) versionAction(action string, op func(*ingest.Service, *http.Request, uint) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := idParam(r)
		if !ok {
			writeError(w, http.StatusBadRequest, "ID 不合法")
			return
		}
		svc, err := s.ingest()
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		err = op(svc, r, id)
		var st *ingest.StateError
		switch {
		case errors.Is(err, ingest.ErrNotFound):
			writeError(w, http.StatusNotFound, err.Error())
			return
		case errors.As(err, &st):
			writeError(w, http.StatusConflict, st.Error())
			return
		case err != nil:
			s.internalError(w, r, err)
			return
		}
		s.audit(r, identityFrom(r.Context()).actor(), action, "version:"+strconv.FormatUint(uint64(id), 10), "")
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}
