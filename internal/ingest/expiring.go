package ingest

import (
	"context"
	"time"

	"github.com/lonelyman0108/ferry/internal/events"
	"github.com/lonelyman0108/ferry/internal/store"
)

// ExpiryWarning 是“即将过期”提醒的提前量。
const ExpiryWarning = 24 * time.Hour

// CheckExpiring 给 ExpiryWarning 内过期（或已经过期）的当前值发一次 value.expiring，返回发出的个数。
func (s *Service) CheckExpiring(ctx context.Context) (int, error) {
	db := s.DB.WithContext(ctx)
	now := s.Now()
	var vs []store.Version
	if err := db.Where("current = ? AND status = ? AND expires_at IS NOT NULL AND expires_at <= ? AND expiry_notified_at IS NULL",
		true, store.VersionValid, now.Add(ExpiryWarning)).Find(&vs).Error; err != nil {
		return 0, err
	}
	n := 0
	for _, v := range vs {
		res := db.Model(&store.Version{}).Where("id = ? AND expiry_notified_at IS NULL", v.ID).Update("expiry_notified_at", now)
		if res.Error != nil {
			return n, res.Error
		}
		if res.RowsAffected == 0 || v.IdentityID == nil {
			continue
		}
		var ident store.Identity
		var src store.Source
		var f store.Field
		if db.First(&ident, *v.IdentityID).Error != nil || db.First(&src, v.SourceID).Error != nil || db.First(&f, v.FieldID).Error != nil {
			continue
		}
		s.Bus.Publish(events.Event{Type: events.ValueExpiring, At: now, SourceID: src.ID, SourceKey: src.Key, SourceName: src.Name,
			IdentityID: ident.ID, IdentityKey: ident.Key, Fields: []string{f.Key}, VersionIDs: []uint{v.ID}, ExpiresAt: v.ExpiresAt})
		n++
	}
	return n, nil
}
