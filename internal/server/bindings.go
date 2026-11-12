package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"gorm.io/gorm"

	"github.com/lonelyman0108/ferry/internal/push"
	"github.com/lonelyman0108/ferry/internal/store"
)

var varNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

// pusher 用当前的数据库和主密钥构造推送服务。
func (s *Server) pusher() (*push.Service, error) {
	if s.store() == nil {
		return nil, errors.New("尚未初始化")
	}
	b, err := s.box()
	if err != nil {
		return nil, err
	}
	return &push.Service{DB: s.store().DB, Box: b, Bus: s.Bus, Now: s.now, Adapter: s.adapter}, nil
}

type bindingInput struct {
	SourceID      uint              `json:"source_id"`
	DestinationID uint              `json:"destination_id"`
	Mode          string            `json:"mode"`
	Rename        map[string]string `json:"rename"`
	Enabled       *bool             `json:"enabled"`
}

type bindingView struct {
	ID            uint              `json:"id"`
	SourceID      uint              `json:"source_id"`
	DestinationID uint              `json:"destination_id"`
	Mode          string            `json:"mode"`
	Rename        map[string]string `json:"rename"`
	Enabled       bool              `json:"enabled"`
}

func viewBinding(b store.Binding) bindingView {
	rename := map[string]string{}
	_ = json.Unmarshal([]byte(b.Rename), &rename)
	return bindingView{ID: b.ID, SourceID: b.SourceID, DestinationID: b.DestinationID, Mode: b.Mode, Rename: rename, Enabled: b.Enabled}
}

func (in bindingInput) apply(db *gorm.DB, b *store.Binding) error {
	if in.Mode != store.ModePerIdentity && in.Mode != store.ModeJoined {
		return errors.New("mode 只能是 per_identity（每个身份一条）或 joined（合并为一条）")
	}
	var src store.Source
	if err := db.Preload("Outputs").First(&src, in.SourceID).Error; err != nil {
		return errors.New("来源不存在")
	}
	var n int64
	if err := db.Model(&store.Destination{}).Where("id = ?", in.DestinationID).Count(&n).Error; err != nil || n == 0 {
		return errors.New("目的地不存在")
	}
	outputs := map[string]bool{}
	for _, o := range src.Outputs {
		outputs[o.Name] = true
	}
	rename := map[string]string{}
	for from, to := range in.Rename {
		if !outputs[from] {
			return fmt.Errorf("来源中不存在输出变量 %s", from)
		}
		if to == "" || to == from {
			continue
		}
		if !varNameRe.MatchString(to) {
			return fmt.Errorf("变量名 %q 只能用字母、数字和 _，不能以数字开头", to)
		}
		rename[from] = to
	}
	raw, _ := json.Marshal(rename)
	b.SourceID, b.DestinationID, b.Mode, b.Rename = in.SourceID, in.DestinationID, in.Mode, string(raw)
	if in.Enabled != nil {
		b.Enabled = *in.Enabled
	}
	return nil
}

func (s *Server) handleListBindings(w http.ResponseWriter, r *http.Request) {
	var bs []store.Binding
	if err := s.store().DB.WithContext(r.Context()).Order("id").Find(&bs).Error; err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]bindingView, 0, len(bs))
	for _, b := range bs {
		out = append(out, viewBinding(b))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateBinding(w http.ResponseWriter, r *http.Request) {
	var in bindingInput
	if !decodeJSON(w, r, &in) {
		return
	}
	db := s.store().DB.WithContext(r.Context())
	b := store.Binding{Enabled: true}
	if err := in.apply(db, &b); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := db.Create(&b).Error; err != nil {
		s.writeSaveError(w, r, err, "这个来源到该目的地的推送规则已存在")
		return
	}
	s.audit(r, identityFrom(r.Context()).actor(), "binding.create", "binding:"+strconv.FormatUint(uint64(b.ID), 10), b.Mode)
	writeJSON(w, http.StatusCreated, viewBinding(b))
}

func (s *Server) handleUpdateBinding(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "ID 不合法")
		return
	}
	var in bindingInput
	if !decodeJSON(w, r, &in) {
		return
	}
	db := s.store().DB.WithContext(r.Context())
	var b store.Binding
	if err := db.First(&b, id).Error; err != nil {
		s.writeFindError(w, r, err, "推送规则不存在")
		return
	}
	if err := in.apply(db, &b); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := db.Save(&b).Error; err != nil {
		s.writeSaveError(w, r, err, "这个来源到该目的地的推送规则已存在")
		return
	}
	s.audit(r, identityFrom(r.Context()).actor(), "binding.update", "binding:"+strconv.FormatUint(uint64(b.ID), 10), b.Mode)
	writeJSON(w, http.StatusOK, viewBinding(b))
}

func (s *Server) handleDeleteBinding(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "ID 不合法")
		return
	}
	db := s.store().DB.WithContext(r.Context())
	res := db.Delete(&store.Binding{}, id)
	if res.Error != nil {
		s.internalError(w, r, res.Error)
		return
	}
	if res.RowsAffected == 0 {
		writeError(w, http.StatusNotFound, "推送规则不存在")
		return
	}
	if err := db.Where("binding_id = ? AND status = ?", id, store.PushQueued).Delete(&store.PushJob{}).Error; err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(r, identityFrom(r.Context()).actor(), "binding.delete", "binding:"+strconv.FormatUint(uint64(id), 10), "")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type pushView struct {
	ID            uint       `json:"id"`
	BindingID     uint       `json:"binding_id"`
	IdentityID    *uint      `json:"identity_id"`
	Status        string     `json:"status"`
	Attempts      int        `json:"attempts"`
	Trigger       string     `json:"trigger"`
	LastError     string     `json:"last_error,omitempty"`
	Vars          string     `json:"vars,omitempty"`
	NextAttemptAt time.Time  `json:"next_attempt_at"`
	CreatedAt     time.Time  `json:"created_at"`
	FinishedAt    *time.Time `json:"finished_at"`
}

func viewPush(j store.PushJob) pushView {
	return pushView{ID: j.ID, BindingID: j.BindingID, IdentityID: j.IdentityID, Status: j.Status, Attempts: j.Attempts, Trigger: j.Trigger,
		LastError: j.LastError, Vars: j.Detail, NextAttemptAt: j.NextAttemptAt, CreatedAt: j.CreatedAt, FinishedAt: j.FinishedAt}
}

// handleListPushes 列出最近 100 个推送任务，可按推送规则、状态（retrying 表示等待重试）、目的地、来源标识筛选。
func (s *Server) handleListPushes(w http.ResponseWriter, r *http.Request) {
	var js []store.PushJob
	db := s.store().DB.WithContext(r.Context())
	q := db.Order("id DESC").Limit(100)
	query := r.URL.Query()
	if v := query.Get("binding"); v != "" {
		q = q.Where("binding_id = ?", v)
	}
	switch st := query.Get("status"); st {
	case "":
	case pushRetrying:
		q = q.Where("status = ? AND last_error <> ?", store.PushQueued, "")
	case store.PushQueued, store.PushRunning, store.PushSuccess, store.PushFailed, store.PushSkipped:
		q = q.Where("status = ?", st)
	default:
		writeError(w, http.StatusBadRequest, "status 不合法")
		return
	}
	if v := query.Get("destination"); v != "" {
		q = q.Where("binding_id IN (?)", db.Model(&store.Binding{}).Select("id").Where("destination_id = ?", v))
	}
	if key := query.Get("source"); key != "" {
		var src store.Source
		// key 在 MySQL 里是保留字，用 map 条件让 GORM 按方言加引号。
		if err := db.Where(map[string]any{"key": key}).First(&src).Error; err != nil {
			s.writeFindError(w, r, err, "来源不存在")
			return
		}
		q = q.Where("binding_id IN (?)", db.Model(&store.Binding{}).Select("id").Where("source_id = ?", src.ID))
	}
	if err := q.Find(&js).Error; err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]pushView, 0, len(js))
	for _, j := range js {
		out = append(out, viewPush(j))
	}
	writeJSON(w, http.StatusOK, out)
}

type manualPushInput struct {
	BindingID  uint  `json:"binding_id"`
	IdentityID *uint `json:"identity_id"`
}

// handleManualPush 手动重推：排一个任务并马上执行，返回执行后的状态。
func (s *Server) handleManualPush(w http.ResponseWriter, r *http.Request) {
	var in manualPushInput
	if !decodeJSON(w, r, &in) {
		return
	}
	db := s.store().DB.WithContext(r.Context())
	var b store.Binding
	if err := db.First(&b, in.BindingID).Error; err != nil {
		s.writeFindError(w, r, err, "推送规则不存在")
		return
	}
	if b.Mode == store.ModePerIdentity {
		if in.IdentityID == nil {
			writeError(w, http.StatusBadRequest, "“每个身份一条”的推送规则需指定身份")
			return
		}
		var n int64
		if err := db.Model(&store.Identity{}).Where("id = ? AND source_id = ?", *in.IdentityID, b.SourceID).Count(&n).Error; err != nil || n == 0 {
			writeError(w, http.StatusBadRequest, "身份不属于这条推送规则的来源")
			return
		}
	}
	p, err := s.pusher()
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	job, err := p.Enqueue(r.Context(), b, in.IdentityID, "manual")
	if err == nil {
		_, err = p.Run(r.Context(), job.ID)
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if err := db.First(&job, job.ID).Error; err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(r, identityFrom(r.Context()).actor(), "push.manual", "binding:"+strconv.FormatUint(uint64(b.ID), 10), job.Status)
	writeJSON(w, http.StatusOK, viewPush(job))
}
