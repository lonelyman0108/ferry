package source

import (
	"context"
	"encoding/json"
	"errors"

	"gorm.io/gorm"

	"github.com/lonelyman0108/ferry/internal/probe"
	"github.com/lonelyman0108/ferry/internal/store"
)

// ErrNotFound 表示来源不存在。
var ErrNotFound = errors.New("来源不存在")

// ErrKeyExists 表示 key 已被其他来源使用。
var ErrKeyExists = errors.New("key 已被其他来源使用")

// ErrKeyChanged 表示更新时改了 key。key 会写进目的地的备注，改了就找不回原来的变量。
var ErrKeyChanged = errors.New("来源创建后 key 不可修改")

// FromModel 把数据库里的来源转成文档。s 需要预加载 Matchers、Fields、Outputs。
func FromModel(s store.Source) Doc {
	enabled, mw, mi := s.Enabled, s.MergeWindowSec, s.MinIntervalSec
	d := Doc{
		Key: s.Key, Name: s.Name, Description: s.Description, Icon: s.Icon, Enabled: &enabled,
		IdentityField: s.IdentityField, MergeWindow: &mw, Notify: s.NotifyMode, MinInterval: &mi, Debug: s.Debug, Review: s.ReviewMode,
		Matchers: []MatcherDoc{}, Fields: []FieldDoc{}, Outputs: []OutputDoc{},
	}
	if s.Probe != "" {
		var p probe.Spec
		if json.Unmarshal([]byte(s.Probe), &p) == nil {
			d.Probe = &p
		}
	}
	for _, m := range s.Matchers {
		d.Matchers = append(d.Matchers, MatcherDoc{Name: m.Name, Host: m.Host, Path: m.PathRegex, Method: m.Method})
	}
	for _, f := range s.Fields {
		d.Fields = append(d.Fields, FieldDoc{
			Key: f.Key, Name: f.Name, Matcher: f.Matcher, In: f.In, Path: f.Path, RegexOn: f.RegexOn,
			Type: f.Type, Sensitive: f.Sensitive, MinLen: f.MinLen, MaxLen: f.MaxLen, Pattern: f.Pattern,
		})
	}
	for _, o := range s.Outputs {
		d.Outputs = append(d.Outputs, OutputDoc{Name: o.Name, Template: o.Template})
	}
	return d
}

func preloadOrdered(db *gorm.DB) *gorm.DB {
	byPos := func(tx *gorm.DB) *gorm.DB { return tx.Order("position") }
	return db.Preload("Matchers", byPos).Preload("Fields", byPos).Preload("Outputs", byPos)
}

// Load 读取一个来源。
func Load(ctx context.Context, db *gorm.DB, id uint) (store.Source, error) {
	var s store.Source
	err := preloadOrdered(db.WithContext(ctx)).First(&s, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return s, ErrNotFound
	}
	return s, err
}

// LoadAll 按 ID 顺序读取全部来源。
func LoadAll(ctx context.Context, db *gorm.DB) ([]store.Source, error) {
	var out []store.Source
	err := preloadOrdered(db.WithContext(ctx)).Order("id").Find(&out).Error
	return out, err
}

// Create 新建来源。d 必须已经 Normalize 并通过 Validate。
func Create(ctx context.Context, db *gorm.DB, d Doc) (store.Source, error) {
	var s store.Source
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		s = store.Source{Key: d.Key}
		applyScalars(&s, d)
		if err := tx.Create(&s).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return ErrKeyExists
			}
			return err
		}
		return syncChildren(tx, s.ID, d)
	})
	if err != nil {
		return s, err
	}
	return Load(ctx, db, s.ID)
}

// Update 用文档整体替换来源的定义。字段按 key、匹配规则和输出按名字保留原记录，
// 这样以后挂在字段上的版本记录不会因为编辑而断开。
func Update(ctx context.Context, db *gorm.DB, id uint, d Doc) (store.Source, error) {
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var s store.Source
		if err := tx.First(&s, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if s.Key != d.Key {
			return ErrKeyChanged
		}
		applyScalars(&s, d)
		if err := tx.Save(&s).Error; err != nil {
			return err
		}
		return syncChildren(tx, s.ID, d)
	})
	if err != nil {
		return store.Source{}, err
	}
	return Load(ctx, db, id)
}

// Delete 删除来源及其匹配规则、字段和输出，连同它的身份、批次、版本、映射和这些映射的推送任务。
func Delete(ctx context.Context, db *gorm.DB, id uint) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 不依赖数据库的级联：SQLite 连接没开外键时也要删干净，身份、版本、映射这些表本来也没有外键。
		bindings := tx.Model(&store.Binding{}).Select("id").Where("source_id = ?", id)
		if err := tx.Where("binding_id IN (?)", bindings).Delete(&store.PushJob{}).Error; err != nil {
			return err
		}
		for _, m := range []any{&store.Binding{}, &store.Version{}, &store.Batch{}, &store.Identity{}, &store.Matcher{}, &store.Field{}, &store.Output{}} {
			if err := tx.Where("source_id = ?", id).Delete(m).Error; err != nil {
				return err
			}
		}
		res := tx.Delete(&store.Source{}, id)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func applyScalars(s *store.Source, d Doc) {
	s.Name, s.Description, s.Icon, s.Enabled = d.Name, d.Description, d.Icon, *d.Enabled
	s.IdentityField, s.MergeWindowSec, s.NotifyMode = d.IdentityField, *d.MergeWindow, d.Notify
	s.MinIntervalSec, s.Debug, s.ReviewMode = *d.MinInterval, d.Debug, d.Review
	s.Probe = ""
	if d.Probe != nil {
		b, _ := json.Marshal(d.Probe)
		s.Probe = string(b)
	}
}

func syncChildren(tx *gorm.DB, sourceID uint, d Doc) error {
	var oldM []store.Matcher
	if err := tx.Where("source_id = ?", sourceID).Find(&oldM).Error; err != nil {
		return err
	}
	ms := make([]store.Matcher, len(d.Matchers))
	for i, m := range d.Matchers {
		ms[i] = store.Matcher{SourceID: sourceID, Name: m.Name, Host: m.Host, PathRegex: m.Path, Method: m.Method, Position: i}
	}
	if err := sync(tx, oldM, ms, func(m store.Matcher) string { return m.Name }, func(m *store.Matcher, id uint) { m.ID = id }, func(m store.Matcher) uint { return m.ID }); err != nil {
		return err
	}

	var oldF []store.Field
	if err := tx.Where("source_id = ?", sourceID).Find(&oldF).Error; err != nil {
		return err
	}
	fs := make([]store.Field, len(d.Fields))
	for i, f := range d.Fields {
		fs[i] = store.Field{
			SourceID: sourceID, Key: f.Key, Name: f.Name, Matcher: f.Matcher, In: f.In, Path: f.Path, RegexOn: f.RegexOn,
			Type: f.Type, Sensitive: f.Sensitive, MinLen: f.MinLen, MaxLen: f.MaxLen, Pattern: f.Pattern, Position: i,
		}
	}
	if err := sync(tx, oldF, fs, func(f store.Field) string { return f.Key }, func(f *store.Field, id uint) { f.ID = id }, func(f store.Field) uint { return f.ID }); err != nil {
		return err
	}

	var oldO []store.Output
	if err := tx.Where("source_id = ?", sourceID).Find(&oldO).Error; err != nil {
		return err
	}
	os := make([]store.Output, len(d.Outputs))
	for i, o := range d.Outputs {
		os[i] = store.Output{SourceID: sourceID, Name: o.Name, Template: o.Template, Position: i}
	}
	return sync(tx, oldO, os, func(o store.Output) string { return o.Name }, func(o *store.Output, id uint) { o.ID = id }, func(o store.Output) uint { return o.ID })
}

// sync 让表里的子记录和 want 一致：同名的沿用原 ID 更新，新的插入，多余的删除。
// 先删后写，避免改名时撞上唯一索引。
func sync[T any](tx *gorm.DB, old, want []T, name func(T) string, setID func(*T, uint), getID func(T) uint) error {
	keep := map[string]uint{}
	for _, o := range old {
		keep[name(o)] = getID(o)
	}
	wanted := map[string]bool{}
	for _, w := range want {
		wanted[name(w)] = true
	}
	for _, o := range old {
		if !wanted[name(o)] {
			if err := tx.Delete(&o).Error; err != nil {
				return err
			}
		}
	}
	for i := range want {
		if id, ok := keep[name(want[i])]; ok {
			setID(&want[i], id)
		}
		if err := tx.Save(&want[i]).Error; err != nil {
			return err
		}
	}
	return nil
}
