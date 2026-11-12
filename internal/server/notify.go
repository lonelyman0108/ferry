package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/lonelyman0108/ferry/internal/events"
	"github.com/lonelyman0108/ferry/internal/notify"
	"github.com/lonelyman0108/ferry/internal/source"
	"github.com/lonelyman0108/ferry/internal/store"
)

func channelAAD(id uint) []byte {
	return []byte("channels/" + strconv.FormatUint(uint64(id), 10) + "/config")
}

func (s *Server) channelConfig(c store.Channel) (notify.Config, error) {
	b, err := s.box()
	if err != nil {
		return notify.Config{}, err
	}
	plain, err := b.Open(c.Config, channelAAD(c.ID))
	if err != nil {
		return notify.Config{}, err
	}
	var cfg notify.Config
	err = json.Unmarshal(plain, &cfg)
	return cfg, err
}

func (s *Server) sealChannel(c *store.Channel, cfg notify.Config) error {
	b, err := s.box()
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(cfg)
	c.Config, err = b.Seal(raw, channelAAD(c.ID))
	return err
}

// notifier 构造通知服务；发送结果以 system 身份写审计。
func (s *Server) notifier() (*notify.Service, error) {
	if s.store() == nil {
		return nil, errors.New("尚未初始化")
	}
	db := s.store().DB
	return &notify.Service{DB: db, HTTP: s.HTTP, Decrypt: s.channelConfig, Audit: func(action, target, detail string) {
		_ = db.Create(&store.AuditLog{Actor: "system", Action: truncate(action, 64), Target: truncate(target, 128), Detail: detail, At: s.now()}).Error
	}}, nil
}

type channelInput struct {
	Name    string        `json:"name"`
	Kind    string        `json:"kind"`
	Enabled *bool         `json:"enabled"`
	Config  notify.Config `json:"config"`
}

type channelView struct {
	ID         uint            `json:"id"`
	Name       string          `json:"name"`
	Kind       string          `json:"kind"`
	Enabled    bool            `json:"enabled"`
	Config     notify.Config   `json:"config"`
	SecretsSet map[string]bool `json:"secrets_set"`
}

func (s *Server) viewChannel(c store.Channel) (channelView, error) {
	cfg, err := s.channelConfig(c)
	if err != nil {
		return channelView{}, err
	}
	red, set := cfg.Redacted()
	return channelView{ID: c.ID, Name: c.Name, Kind: c.Kind, Enabled: c.Enabled, Config: red, SecretsSet: set}, nil
}

func (s *Server) handleListChannels(w http.ResponseWriter, r *http.Request) {
	var cs []store.Channel
	if err := s.store().DB.WithContext(r.Context()).Order("id").Find(&cs).Error; err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]channelView, 0, len(cs))
	for _, c := range cs {
		v, err := s.viewChannel(c)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateChannel(w http.ResponseWriter, r *http.Request) {
	var in channelInput
	if !decodeJSON(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 64 {
		writeError(w, http.StatusBadRequest, "名称不能为空，且不超过 64 个字节")
		return
	}
	in.Config = notify.Normalize(in.Kind, in.Config)
	if err := notify.Validate(in.Kind, in.Config); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	c := store.Channel{Name: in.Name, Kind: in.Kind, Enabled: in.Enabled == nil || *in.Enabled, Config: "-"}
	err := s.store().DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&c).Error; err != nil {
			return err
		}
		if err := s.sealChannel(&c, in.Config); err != nil {
			return err
		}
		return tx.Save(&c).Error
	})
	if err != nil {
		s.writeSaveError(w, r, err, "通知渠道名称重复")
		return
	}
	s.audit(r, identityFrom(r.Context()).actor(), "channel.create", "channel:"+c.Name, c.Kind)
	v, err := s.viewChannel(c)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

func (s *Server) handleUpdateChannel(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "ID 不合法")
		return
	}
	var in channelInput
	if !decodeJSON(w, r, &in) {
		return
	}
	db := s.store().DB.WithContext(r.Context())
	var c store.Channel
	if err := db.First(&c, id).Error; err != nil {
		s.writeFindError(w, r, err, "通知渠道不存在")
		return
	}
	if in.Kind != c.Kind {
		writeError(w, http.StatusBadRequest, "通知渠道类型不可修改")
		return
	}
	old, err := s.channelConfig(c)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	cfg := notify.Normalize(c.Kind, in.Config).MergeSecrets(old)
	if err := notify.Validate(c.Kind, cfg); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if name := strings.TrimSpace(in.Name); name != "" {
		c.Name = name
	}
	if in.Enabled != nil {
		c.Enabled = *in.Enabled
	}
	if err := s.sealChannel(&c, cfg); err != nil {
		s.internalError(w, r, err)
		return
	}
	if err := db.Save(&c).Error; err != nil {
		s.writeSaveError(w, r, err, "通知渠道名称重复")
		return
	}
	s.audit(r, identityFrom(r.Context()).actor(), "channel.update", "channel:"+c.Name, "")
	v, err := s.viewChannel(c)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleDeleteChannel(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "ID 不合法")
		return
	}
	db := s.store().DB.WithContext(r.Context())
	var c store.Channel
	if err := db.First(&c, id).Error; err != nil {
		s.writeFindError(w, r, err, "通知渠道不存在")
		return
	}
	changed := 0
	err := db.Transaction(func(tx *gorm.DB) error {
		var rules []store.NotifyRule
		if err := tx.Find(&rules).Error; err != nil {
			return err
		}
		// 从引用它的规则里移除；移除后一个渠道都不剩的规则停用，免得规则留着失效的渠道、之后也保存不了。
		for _, x := range rules {
			var ids []uint
			_ = json.Unmarshal([]byte(x.ChannelIDs), &ids)
			kept := make([]uint, 0, len(ids))
			for _, v := range ids {
				if v != c.ID {
					kept = append(kept, v)
				}
			}
			if len(kept) == len(ids) {
				continue
			}
			b, _ := json.Marshal(kept)
			x.ChannelIDs = string(b)
			if len(kept) == 0 {
				x.Enabled = false
			}
			if err := tx.Save(&x).Error; err != nil {
				return err
			}
			changed++
		}
		return tx.Delete(&c).Error
	})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	detail := ""
	if changed > 0 {
		detail = fmt.Sprintf("从 %d 条通知规则中移除", changed)
	}
	s.audit(r, identityFrom(r.Context()).actor(), "channel.delete", "channel:"+c.Name, detail)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleTestChannel 通过渠道发一条测试通知。
func (s *Server) handleTestChannel(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "ID 不合法")
		return
	}
	var c store.Channel
	if err := s.store().DB.WithContext(r.Context()).First(&c, id).Error; err != nil {
		s.writeFindError(w, r, err, "通知渠道不存在")
		return
	}
	n, err := s.notifier()
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	m := notify.Message{Event: "test", Title: "Ferry：测试通知", Body: "通知渠道「" + c.Name + "」配置正确，可正常接收通知。", At: s.now()}
	result := map[string]any{"ok": true}
	if err := n.SendTo(ctx, c, m); err != nil {
		result = map[string]any{"ok": false, "error": err.Error()}
	}
	s.audit(r, identityFrom(r.Context()).actor(), "channel.test", "channel:"+c.Name, fmt.Sprint(result["ok"]))
	writeJSON(w, http.StatusOK, result)
}

type ruleInput struct {
	Name       string   `json:"name"`
	Events     []string `json:"events"`
	SourceKeys []string `json:"source_keys"`
	ChannelIDs []uint   `json:"channel_ids"`
	Enabled    *bool    `json:"enabled"`
}

type ruleView struct {
	ID         uint     `json:"id"`
	Name       string   `json:"name"`
	Events     []string `json:"events"`
	SourceKeys []string `json:"source_keys"`
	ChannelIDs []uint   `json:"channel_ids"`
	Enabled    bool     `json:"enabled"`
}

func viewRule(r store.NotifyRule) ruleView {
	v := ruleView{ID: r.ID, Name: r.Name, Enabled: r.Enabled, Events: []string{}, SourceKeys: []string{}, ChannelIDs: []uint{}}
	_ = json.Unmarshal([]byte(r.Events), &v.Events)
	_ = json.Unmarshal([]byte(r.SourceKeys), &v.SourceKeys)
	_ = json.Unmarshal([]byte(r.ChannelIDs), &v.ChannelIDs)
	return v
}

func (in ruleInput) apply(db *gorm.DB, r *store.NotifyRule) error {
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > 64 {
		return errors.New("名称不能为空，且不超过 64 个字节")
	}
	if len(in.Events) == 0 {
		return errors.New("至少选择一种事件类型")
	}
	known := map[string]bool{}
	for _, t := range events.Types {
		known[t] = true
	}
	for _, e := range in.Events {
		if !known[e] {
			return fmt.Errorf("未知事件类型 %q，可选：%s", e, strings.Join(events.Types, "、"))
		}
	}
	if len(in.ChannelIDs) == 0 {
		return errors.New("至少选择一个通知渠道")
	}
	var n int64
	if err := db.Model(&store.Channel{}).Where("id IN ?", in.ChannelIDs).Count(&n).Error; err != nil || int(n) != len(in.ChannelIDs) {
		return errors.New("部分所选通知渠道不存在")
	}
	if in.SourceKeys == nil {
		in.SourceKeys = []string{}
	}
	evs, _ := json.Marshal(in.Events)
	srcs, _ := json.Marshal(in.SourceKeys)
	chans, _ := json.Marshal(in.ChannelIDs)
	r.Name, r.Events, r.SourceKeys, r.ChannelIDs = name, string(evs), string(srcs), string(chans)
	if in.Enabled != nil {
		r.Enabled = *in.Enabled
	}
	return nil
}

func (s *Server) handleListRules(w http.ResponseWriter, r *http.Request) {
	var rs []store.NotifyRule
	if err := s.store().DB.WithContext(r.Context()).Order("id").Find(&rs).Error; err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]ruleView, 0, len(rs))
	for _, x := range rs {
		out = append(out, viewRule(x))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateRule(w http.ResponseWriter, r *http.Request) {
	var in ruleInput
	if !decodeJSON(w, r, &in) {
		return
	}
	db := s.store().DB.WithContext(r.Context())
	x := store.NotifyRule{Enabled: true}
	if err := in.apply(db, &x); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := db.Create(&x).Error; err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(r, identityFrom(r.Context()).actor(), "notify_rule.create", "rule:"+x.Name, x.Events)
	writeJSON(w, http.StatusCreated, viewRule(x))
}

func (s *Server) handleUpdateRule(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "ID 不合法")
		return
	}
	var in ruleInput
	if !decodeJSON(w, r, &in) {
		return
	}
	db := s.store().DB.WithContext(r.Context())
	var x store.NotifyRule
	if err := db.First(&x, id).Error; err != nil {
		s.writeFindError(w, r, err, "通知规则不存在")
		return
	}
	if err := in.apply(db, &x); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := db.Save(&x).Error; err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(r, identityFrom(r.Context()).actor(), "notify_rule.update", "rule:"+x.Name, x.Events)
	writeJSON(w, http.StatusOK, viewRule(x))
}

func (s *Server) handleDeleteRule(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "ID 不合法")
		return
	}
	db := s.store().DB.WithContext(r.Context())
	var x store.NotifyRule
	if err := db.First(&x, id).Error; err != nil {
		s.writeFindError(w, r, err, "通知规则不存在")
		return
	}
	if err := db.Delete(&x).Error; err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(r, identityFrom(r.Context()).actor(), "notify_rule.delete", "rule:"+x.Name, "")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// checkMitm 逐个订阅比对 MITM 域名集合。第一次只记下来；之后变了就发 subscription.mitm_changed，
// 附上一键安装链接，由使用者在手机上确认更新。来源或订阅变化后调用。
func (s *Server) checkMitm(ctx context.Context) {
	db := s.store().DB.WithContext(ctx)
	var subs []store.Subscription
	if err := db.Find(&subs).Error; err != nil {
		slog.Error("检查 MITM 域名失败", "err", err)
		return
	}
	all, err := source.LoadAll(ctx, s.store().DB)
	if err != nil {
		slog.Error("检查 MITM 域名失败", "err", err)
		return
	}
	r := (&http.Request{}).WithContext(ctx)
	for _, m := range subs {
		hash := mitmHash(subscriptionDocsFrom(all, m))
		if hash == m.MitmHash {
			continue
		}
		if err := db.Model(&store.Subscription{}).Where("id = ?", m.ID).Update("mitm_hash", hash).Error; err != nil {
			slog.Error("记录 MITM 域名失败", "err", err)
			continue
		}
		if m.MitmHash == "" {
			continue
		}
		v, err := s.viewSubscription(r, m, all)
		if err != nil {
			slog.Error("生成订阅链接失败", "err", err)
			continue
		}
		s.Bus.Publish(events.Event{Type: events.MitmChanged, At: s.now(), Subject: m.Name, Link: v.Links.InstallURL})
	}
}
