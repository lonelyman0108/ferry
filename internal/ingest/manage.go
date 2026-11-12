package ingest

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/lonelyman0108/ferry/internal/events"
	"github.com/lonelyman0108/ferry/internal/store"
)

// 版本保留：每个“身份 + 字段”至少留最近 KeepVersions 个，并且 KeepDays 天内的都留，取更宽松的；当前值永远保留。
const (
	KeepVersions = 50
	KeepDays     = 180
)

// ErrNotFound 表示版本不存在。
var ErrNotFound = errors.New("版本不存在")

// StateError 表示版本当前的状态不允许这个操作。
type StateError struct{ Msg string }

func (e *StateError) Error() string { return e.Msg }

// Approve 人工通过一个待审核的版本，让它成为当前值。
func (s *Service) Approve(ctx context.Context, versionID uint) error {
	return s.changeVersion(ctx, versionID, func(v store.Version) (string, error) {
		if v.Status != store.VersionPendingReview {
			return "", &StateError{"仅待审核的版本可审核通过"}
		}
		return events.ValueUpdated, nil
	}, events.CauseApprove, "审核通过")
}

// Reject 人工驳回一个待审核的版本。
func (s *Service) Reject(ctx context.Context, versionID uint) error {
	return s.changeVersion(ctx, versionID, func(v store.Version) (string, error) {
		if v.Status != store.VersionPendingReview {
			return "", &StateError{"仅待审核的版本可驳回"}
		}
		return events.ValueRejected, nil
	}, "", "人工驳回")
}

// Activate 把一个以前生效过的版本重新设为当前值（回滚），会触发重新推送。
func (s *Service) Activate(ctx context.Context, versionID uint) error {
	return s.changeVersion(ctx, versionID, func(v store.Version) (string, error) {
		if v.Status != store.VersionValid {
			return "", &StateError{"仅校验通过的版本可设为当前值"}
		}
		if v.Current {
			return "", &StateError{"该版本已是当前值"}
		}
		return events.ValueUpdated, nil
	}, events.CauseActivate, "恢复旧值")
}

// changeVersion 按 check 决定的事件类型修改版本：ValueUpdated 时设为当前值，ValueRejected 时驳回。
func (s *Service) changeVersion(ctx context.Context, versionID uint, check func(store.Version) (string, error), cause, detail string) error {
	now := s.Now()
	var ev events.Event
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var v store.Version
		if err := tx.First(&v, versionID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if v.IdentityID == nil {
			return &StateError{"版本尚未关联身份，请等待批次处理完成"}
		}
		typ, err := check(v)
		if err != nil {
			return err
		}
		switch typ {
		case events.ValueUpdated:
			if err := activate(tx, v.ID, *v.IdentityID, v.FieldID, now); err != nil {
				return err
			}
		case events.ValueRejected:
			if err := tx.Model(&store.Version{}).Where("id = ?", v.ID).Updates(map[string]any{"status": store.VersionRejected, "reason": detail}).Error; err != nil {
				return err
			}
		}
		var ident store.Identity
		var src store.Source
		var f store.Field
		if err := tx.First(&ident, *v.IdentityID).Error; err != nil {
			return err
		}
		if err := tx.First(&src, v.SourceID).Error; err != nil {
			return err
		}
		if err := tx.First(&f, v.FieldID).Error; err != nil {
			return err
		}
		ev = events.Event{Type: typ, At: now, SourceID: src.ID, SourceKey: src.Key, SourceName: src.Name,
			IdentityID: ident.ID, IdentityKey: ident.Key, Fields: []string{f.Key}, VersionIDs: []uint{v.ID}, Cause: cause, Detail: detail}
		return nil
	})
	if err != nil {
		return err
	}
	s.Bus.Publish(ev)
	return nil
}

// Cleanup 按保留策略删除旧版本和已经没有版本的旧批次，并清掉来源或字段删除后留下的数据，返回删除的版本数。
func (s *Service) Cleanup(ctx context.Context) (int, error) {
	db := s.DB.WithContext(ctx)
	cutoff := s.Now().AddDate(0, 0, -KeepDays)
	type group struct {
		FieldID    uint
		IdentityID *uint
	}
	var groups []group
	if err := db.Model(&store.Version{}).Distinct("field_id", "identity_id").Find(&groups).Error; err != nil {
		return 0, err
	}
	deleted := 0
	for _, g := range groups {
		q := db.Model(&store.Version{}).Where("field_id = ?", g.FieldID)
		if g.IdentityID == nil {
			q = q.Where("identity_id IS NULL")
		} else {
			q = q.Where("identity_id = ?", *g.IdentityID)
		}
		var ids []uint
		if err := q.Where("current = ? AND created_at < ?", false, cutoff).Order("id DESC").Pluck("id", &ids).Error; err != nil {
			return deleted, err
		}
		// 再算一遍这组一共有多少个更新的版本，保证至少留 KeepVersions 个。
		var newer int64
		cq := db.Model(&store.Version{}).Where("field_id = ?", g.FieldID)
		if g.IdentityID == nil {
			cq = cq.Where("identity_id IS NULL")
		} else {
			cq = cq.Where("identity_id = ?", *g.IdentityID)
		}
		if err := cq.Where("created_at >= ? OR current = ?", cutoff, true).Count(&newer).Error; err != nil {
			return deleted, err
		}
		keep := KeepVersions - int(newer)
		if keep < 0 {
			keep = 0
		}
		if keep >= len(ids) {
			continue
		}
		res := db.Where("id IN ?", ids[keep:]).Delete(&store.Version{})
		if res.Error != nil {
			return deleted, res.Error
		}
		deleted += int(res.RowsAffected)
	}
	n, err := pruneOrphans(db)
	deleted += n
	if err != nil {
		return deleted, err
	}
	err = db.Where("status = ? AND created_at < ? AND id NOT IN (?)", store.BatchDone, cutoff, db.Model(&store.Version{}).Select("batch_id")).Delete(&store.Batch{}).Error
	return deleted, err
}

// pruneOrphans 删掉来源已不存在的身份、批次、版本和映射，字段已删掉的版本，
// 以及映射已不存在、还在排队的推送任务（推送记录保留，和删除映射时一样）。返回删除的版本数。
func pruneOrphans(db *gorm.DB) (int, error) {
	sourceIDs := func() *gorm.DB { return db.Model(&store.Source{}).Select("id") }
	res := db.Where("source_id NOT IN (?) OR field_id NOT IN (?)", sourceIDs(), db.Model(&store.Field{}).Select("id")).Delete(&store.Version{})
	if res.Error != nil {
		return 0, res.Error
	}
	versions := int(res.RowsAffected)
	for _, m := range []any{&store.Batch{}, &store.Identity{}, &store.Binding{}} {
		if err := db.Where("source_id NOT IN (?)", sourceIDs()).Delete(m).Error; err != nil {
			return versions, err
		}
	}
	err := db.Where("status = ? AND binding_id NOT IN (?)", store.PushQueued, db.Model(&store.Binding{}).Select("id")).Delete(&store.PushJob{}).Error
	return versions, err
}
