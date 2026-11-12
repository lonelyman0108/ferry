// Package push 把来源的当前值推送到目的地：按映射渲染输出变量，排队执行，失败按指数退避重试。
package push

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/lonelyman0108/ferry/internal/dest"
	"github.com/lonelyman0108/ferry/internal/events"
	"github.com/lonelyman0108/ferry/internal/ingest"
	"github.com/lonelyman0108/ferry/internal/rules"
	"github.com/lonelyman0108/ferry/internal/secret"
	"github.com/lonelyman0108/ferry/internal/store"
)

// 重试：第 n 次失败后等 RetryBase × 2^(n-1)，一共试 MaxAttempts 次。
const (
	MaxAttempts = 5
	RetryBase   = 30 * time.Second
	// stuckAfter 之后还处于运行中的任务视为进程中途退出，重新排队。
	stuckAfter = 10 * time.Minute
)

// Service 执行推送。Adapter 按目的地创建适配器，由调用方注入（解密配置需要主密钥）。
type Service struct {
	DB      *gorm.DB
	Box     *secret.Box
	Bus     *events.Bus
	Now     func() time.Time
	Adapter func(store.Destination) (dest.Adapter, error)
}

// Enqueue 为映射排一个推送任务；同一映射同一身份已有排队中的任务时不重复排。
func (s *Service) Enqueue(ctx context.Context, b store.Binding, identityID *uint, trigger string) (store.PushJob, error) {
	if b.Mode == store.ModeJoined {
		identityID = nil
	}
	db := s.DB.WithContext(ctx)
	var job store.PushJob
	q := db.Where("binding_id = ? AND status = ?", b.ID, store.PushQueued)
	if identityID == nil {
		q = q.Where("identity_id IS NULL")
	} else {
		q = q.Where("identity_id = ?", *identityID)
	}
	err := q.First(&job).Error
	if err == nil {
		return job, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return job, err
	}
	now := s.Now()
	job = store.PushJob{BindingID: b.ID, IdentityID: identityID, Status: store.PushQueued, NextAttemptAt: now, Trigger: trigger, CreatedAt: now}
	return job, db.Create(&job).Error
}

// OnEvent 订阅事件总线：当前值变化时，给这个来源所有启用的映射排推送。
func (s *Service) OnEvent(e events.Event) {
	if e.Type != events.ValueUpdated {
		return
	}
	ctx := context.Background()
	var bs []store.Binding
	if err := s.DB.WithContext(ctx).Where("source_id = ? AND enabled = ?", e.SourceID, true).Find(&bs).Error; err != nil {
		slog.Error("查询推送规则失败", "err", err)
		return
	}
	trigger := events.ValueUpdated
	if e.Cause != "" {
		trigger = e.Cause
	}
	ident := e.IdentityID
	for _, b := range bs {
		if _, err := s.Enqueue(ctx, b, &ident, trigger); err != nil {
			slog.Error("排推送任务失败", "binding", b.ID, "err", err)
		}
	}
}

// ProcessDue 执行所有到期的任务，返回执行的个数。
func (s *Service) ProcessDue(ctx context.Context) (int, error) {
	db := s.DB.WithContext(ctx)
	now := s.Now()
	if err := db.Model(&store.PushJob{}).Where("status = ? AND next_attempt_at < ?", store.PushRunning, now.Add(-stuckAfter)).
		Update("status", store.PushQueued).Error; err != nil {
		return 0, err
	}
	var ids []uint
	if err := db.Model(&store.PushJob{}).Where("status = ? AND next_attempt_at <= ?", store.PushQueued, now).Order("id").Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	n := 0
	for _, id := range ids {
		ran, err := s.Run(ctx, id)
		if err != nil {
			return n, err
		}
		if ran {
			n++
		}
	}
	return n, nil
}

// Run 执行一个任务。先用带条件的 UPDATE 认领，同一任务不会被执行两次。
func (s *Service) Run(ctx context.Context, jobID uint) (bool, error) {
	db := s.DB.WithContext(ctx)
	now := s.Now()
	res := db.Model(&store.PushJob{}).Where("id = ? AND status = ?", jobID, store.PushQueued).
		Updates(map[string]any{"status": store.PushRunning, "next_attempt_at": now})
	if res.Error != nil || res.RowsAffected == 0 {
		return false, res.Error
	}
	var job store.PushJob
	if err := db.First(&job, jobID).Error; err != nil {
		return true, err
	}
	p, d, b, err := s.build(ctx, job)
	noOutputs := errors.Is(err, errNoOutputs)
	if noOutputs {
		err = nil
	}
	if err == nil && (!d.Enabled || !b.Enabled) {
		return true, s.finish(db, job, store.PushSkipped, "目的地或推送规则已停用", "")
	}
	if noOutputs {
		return true, s.finish(db, job, store.PushSkipped, "来源没有输出变量", "")
	}
	if err == nil && len(p.Vars) == 0 {
		return true, s.finish(db, job, store.PushSkipped, "无可推送的变量（字段尚无值）", "")
	}
	if err == nil {
		var a dest.Adapter
		if a, err = s.Adapter(d); err == nil {
			err = a.Upsert(ctx, p)
		}
	}
	names := make([]string, 0, len(p.Vars))
	for _, v := range p.Vars {
		names = append(names, v.Name)
	}
	if err == nil {
		return true, s.finish(db, job, store.PushSuccess, "", strings.Join(names, ","))
	}

	job.Attempts++
	if job.Attempts >= MaxAttempts {
		if ferr := s.finish(db, job, store.PushFailed, err.Error(), strings.Join(names, ",")); ferr != nil {
			return true, ferr
		}
		s.Bus.Publish(events.Event{Type: events.PushFailed, At: s.Now(), SourceID: b.SourceID, SourceKey: p.SourceKey, SourceName: p.SourceName,
			IdentityKey: p.IdentityKey, Fields: names, Detail: truncate(d.Name+"："+err.Error(), 255)})
		return true, nil
	}
	wait := RetryBase << (job.Attempts - 1)
	return true, db.Model(&store.PushJob{}).Where("id = ?", job.ID).Updates(map[string]any{
		"status": store.PushQueued, "attempts": job.Attempts, "next_attempt_at": s.Now().Add(wait), "last_error": truncate(err.Error(), 512),
	}).Error
}

func (s *Service) finish(db *gorm.DB, job store.PushJob, status, lastErr, detail string) error {
	now := s.Now()
	return db.Model(&store.PushJob{}).Where("id = ?", job.ID).Updates(map[string]any{
		"status": status, "attempts": job.Attempts, "last_error": truncate(lastErr, 512), "detail": truncate(detail, 512), "finished_at": now,
	}).Error
}

// errNoOutputs 表示来源没有定义输出变量，任务按跳过处理，不重试。
var errNoOutputs = errors.New("来源没有输出变量")

// build 读取映射、目的地和当前值，渲染出要推送的变量。
func (s *Service) build(ctx context.Context, job store.PushJob) (dest.Push, store.Destination, store.Binding, error) {
	db := s.DB.WithContext(ctx)
	var b store.Binding
	var d store.Destination
	var src store.Source
	if err := db.First(&b, job.BindingID).Error; err != nil {
		return dest.Push{}, d, b, fmt.Errorf("推送规则不存在：%w", err)
	}
	if err := db.First(&d, b.DestinationID).Error; err != nil {
		return dest.Push{}, d, b, fmt.Errorf("目的地不存在：%w", err)
	}
	byPos := func(tx *gorm.DB) *gorm.DB { return tx.Order("position") }
	if err := db.Preload("Fields").Preload("Outputs", byPos).First(&src, b.SourceID).Error; err != nil {
		return dest.Push{}, d, b, fmt.Errorf("来源不存在：%w", err)
	}
	rename := map[string]string{}
	_ = json.Unmarshal([]byte(b.Rename), &rename)
	p := dest.Push{SourceKey: src.Key, SourceName: src.Name, Vars: []dest.Var{}}
	if len(src.Outputs) == 0 {
		return p, d, b, errNoOutputs
	}

	var identities []store.Identity
	q := db.Where("source_id = ?", src.ID).Order("id")
	if b.Mode == store.ModePerIdentity {
		if job.IdentityID == nil {
			return p, d, b, errors.New("“每个身份一条”的推送任务缺少身份")
		}
		q = q.Where("id = ?", *job.IdentityID)
	}
	if err := q.Find(&identities).Error; err != nil {
		return p, d, b, err
	}
	fieldKey := map[uint]string{}
	for _, f := range src.Fields {
		fieldKey[f.ID] = f.Key
	}
	rendered := map[string][]string{}
	for _, ident := range identities {
		values, err := s.currentValues(db, ident.ID, fieldKey)
		if err != nil {
			return p, d, b, err
		}
		row := map[string]string{}
		for _, o := range src.Outputs {
			t, err := rules.ParseTemplate(o.Template)
			if err != nil {
				continue
			}
			if v, err := t.Render(values); err == nil {
				row[o.Name] = v
			}
		}
		// 合成一条时各变量用 & 拼接，脚本按位置把它们一一对应；某个身份缺了任何一个变量，
		// 就整个跳过，否则后面身份的值会错位，配到别的身份上。一个身份一条时只跳过缺的那个变量。
		if b.Mode == store.ModeJoined && len(row) < len(src.Outputs) {
			continue
		}
		for name, v := range row {
			rendered[name] = append(rendered[name], v)
		}
		if b.Mode == store.ModePerIdentity {
			p.IdentityKey = ident.Key
		}
	}
	remarks := "ferry:" + src.Key
	if b.Mode == store.ModePerIdentity && len(identities) == 1 {
		remarks += ":" + identities[0].Key
	}
	for _, o := range src.Outputs {
		vals, ok := rendered[o.Name]
		if !ok {
			continue
		}
		name := o.Name
		if r := rename[o.Name]; r != "" {
			name = r
		}
		p.Vars = append(p.Vars, dest.Var{Name: name, Value: strings.Join(vals, "&"), Remarks: remarks})
	}
	return p, d, b, nil
}

func (s *Service) currentValues(db *gorm.DB, identityID uint, fieldKey map[uint]string) (map[string]string, error) {
	var vs []store.Version
	if err := db.Where("identity_id = ? AND current = ?", identityID, true).Find(&vs).Error; err != nil {
		return nil, err
	}
	values := map[string]string{}
	for _, v := range vs {
		val, err := ingest.RevealValue(s.Box, v)
		if err != nil {
			return nil, err
		}
		values[fieldKey[v.FieldID]] = val
	}
	return values, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
