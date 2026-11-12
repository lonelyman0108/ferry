package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/lonelyman0108/ferry/internal/dest"
	"github.com/lonelyman0108/ferry/internal/store"
)

func destinationAAD(id uint) []byte {
	return []byte("destinations/" + strconv.FormatUint(uint64(id), 10) + "/config")
}

type destinationInput struct {
	Name    string      `json:"name"`
	Kind    string      `json:"kind"`
	Enabled *bool       `json:"enabled"`
	Config  dest.Config `json:"config"`
}

type destinationView struct {
	ID         uint            `json:"id"`
	Name       string          `json:"name"`
	Kind       string          `json:"kind"`
	Enabled    bool            `json:"enabled"`
	Config     dest.Config     `json:"config"`
	SecretsSet map[string]bool `json:"secrets_set"`
	UpdatedAt  time.Time       `json:"updated_at"`
	// LastPush 是推到这个目的地的最近一次任务；Problems 是最近一次推送失败或在等待重试的“推送规则 + 身份”数，
	// 只算启用的推送规则。两者只在列表里给出。
	LastPush *pushView `json:"last_push,omitempty"`
	Problems int       `json:"problems"`
}

// destinationConfig 解密目的地配置。
func (s *Server) destinationConfig(d store.Destination) (dest.Config, error) {
	b, err := s.box()
	if err != nil {
		return dest.Config{}, err
	}
	plain, err := b.Open(d.Config, destinationAAD(d.ID))
	if err != nil {
		return dest.Config{}, err
	}
	return dest.Unmarshal(plain)
}

func (s *Server) sealDestination(d *store.Destination, c dest.Config) error {
	b, err := s.box()
	if err != nil {
		return err
	}
	raw, err := dest.Marshal(c)
	if err != nil {
		return err
	}
	d.Config, err = b.Seal(raw, destinationAAD(d.ID))
	return err
}

func (s *Server) viewDestination(d store.Destination) (destinationView, error) {
	c, err := s.destinationConfig(d)
	if err != nil {
		return destinationView{}, err
	}
	red, set := c.Redacted()
	return destinationView{ID: d.ID, Name: d.Name, Kind: d.Kind, Enabled: d.Enabled, Config: red, SecretsSet: set, UpdatedAt: d.UpdatedAt}, nil
}

func (in destinationInput) normalize() (destinationInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 64 {
		return in, errors.New("名称不能为空，且不超过 64 个字节")
	}
	return in, nil
}

func (s *Server) handleListDestinations(w http.ResponseWriter, r *http.Request) {
	var ds []store.Destination
	if err := s.store().DB.WithContext(r.Context()).Order("id").Find(&ds).Error; err != nil {
		s.internalError(w, r, err)
		return
	}
	db := s.store().DB.WithContext(r.Context())
	var bindings []store.Binding
	if err := db.Find(&bindings).Error; err != nil {
		s.internalError(w, r, err)
		return
	}
	bindingOf := map[uint]store.Binding{}
	for _, b := range bindings {
		bindingOf[b.ID] = b
	}
	latest, err := latestPushes(db, nil)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]destinationView, 0, len(ds))
	for _, d := range ds {
		v, err := s.viewDestination(d)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		for _, j := range latest {
			b, ok := bindingOf[j.BindingID]
			if !ok || b.DestinationID != d.ID {
				continue
			}
			if v.LastPush == nil || j.ID > v.LastPush.ID {
				pv := viewPush(j)
				v.LastPush = &pv
			}
			if st := pushState(j); b.Enabled && d.Enabled && (st == store.PushFailed || st == pushRetrying) {
				v.Problems++
			}
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateDestination(w http.ResponseWriter, r *http.Request) {
	var in destinationInput
	if !decodeJSON(w, r, &in) {
		return
	}
	in, err := in.normalize()
	if err == nil {
		err = dest.Validate(in.Kind, in.Config)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	d := store.Destination{Name: in.Name, Kind: in.Kind, Enabled: in.Enabled == nil || *in.Enabled, Config: "-"}
	err = s.store().DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		// 先建行拿到 ID，再生成绑定这个 ID 的配置密文。
		if err := tx.Create(&d).Error; err != nil {
			return err
		}
		if err := s.sealDestination(&d, in.Config); err != nil {
			return err
		}
		return tx.Save(&d).Error
	})
	if err != nil {
		s.writeSaveError(w, r, err, "目的地名称重复")
		return
	}
	s.audit(r, identityFrom(r.Context()).actor(), "destination.create", "destination:"+d.Name, d.Kind)
	v, err := s.viewDestination(d)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

func (s *Server) handleUpdateDestination(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "ID 不合法")
		return
	}
	var in destinationInput
	if !decodeJSON(w, r, &in) {
		return
	}
	in, err := in.normalize()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	db := s.store().DB.WithContext(r.Context())
	var d store.Destination
	if err := db.First(&d, id).Error; err != nil {
		s.writeFindError(w, r, err, "目的地不存在")
		return
	}
	if in.Kind != d.Kind {
		writeError(w, http.StatusBadRequest, "目的地类型不可修改")
		return
	}
	old, err := s.destinationConfig(d)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	cfg := in.Config.MergeSecrets(old)
	if err := dest.Validate(d.Kind, cfg); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	d.Name = in.Name
	if in.Enabled != nil {
		d.Enabled = *in.Enabled
	}
	if err := s.sealDestination(&d, cfg); err != nil {
		s.internalError(w, r, err)
		return
	}
	if err := db.Save(&d).Error; err != nil {
		s.writeSaveError(w, r, err, "目的地名称重复")
		return
	}
	s.audit(r, identityFrom(r.Context()).actor(), "destination.update", "destination:"+d.Name, "")
	v, err := s.viewDestination(d)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleDeleteDestination(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "ID 不合法")
		return
	}
	db := s.store().DB.WithContext(r.Context())
	var d store.Destination
	if err := db.First(&d, id).Error; err != nil {
		s.writeFindError(w, r, err, "目的地不存在")
		return
	}
	var n int64
	if err := db.Model(&store.Binding{}).Where("destination_id = ?", d.ID).Count(&n).Error; err != nil {
		s.internalError(w, r, err)
		return
	}
	if n > 0 {
		writeError(w, http.StatusConflict, fmt.Sprintf("目的地仍被 %d 条推送规则引用，无法删除", n))
		return
	}
	if err := db.Delete(&d).Error; err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(r, identityFrom(r.Context()).actor(), "destination.delete", "destination:"+d.Name, "")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// adapter 为目的地创建适配器。
func (s *Server) adapter(d store.Destination) (dest.Adapter, error) {
	c, err := s.destinationConfig(d)
	if err != nil {
		return nil, err
	}
	return dest.New(d.Kind, c, s.HTTP)
}

// handleTestDestination 检查目的地能否连通。结果写审计，但不改目的地里的任何数据（Webhook 会收到一条测试推送）。
func (s *Server) handleTestDestination(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "ID 不合法")
		return
	}
	var d store.Destination
	if err := s.store().DB.WithContext(r.Context()).First(&d, id).Error; err != nil {
		s.writeFindError(w, r, err, "目的地不存在")
		return
	}
	a, err := s.adapter(d)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	result := map[string]any{"ok": true}
	if err := a.Test(ctx); err != nil {
		result = map[string]any{"ok": false, "error": err.Error()}
	}
	s.audit(r, identityFrom(r.Context()).actor(), "destination.test", "destination:"+d.Name, strconv.FormatBool(result["ok"].(bool)))
	writeJSON(w, http.StatusOK, result)
}
