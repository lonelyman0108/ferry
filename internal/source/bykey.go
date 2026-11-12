package source

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/lonelyman0108/ferry/internal/store"
)

// LoadByKey 按 key 读取一个来源。
func LoadByKey(ctx context.Context, db *gorm.DB, key string) (store.Source, error) {
	var s store.Source
	err := preloadOrdered(db.WithContext(ctx)).Where(map[string]any{"key": key}).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return s, ErrNotFound
	}
	return s, err
}
