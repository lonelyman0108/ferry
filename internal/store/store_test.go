package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/lonelyman0108/ferry/internal/config"
)

// testStores 返回要跑测试的全部数据库：SQLite 必跑；设置了
// FERRY_TEST_POSTGRES_DSN / FERRY_TEST_MYSQL_DSN 时再加上对应数据库。
// 这两个 DSN 必须指向专用的空测试库，测试开始时会删掉全部表。
func testStores(t *testing.T) map[string]*Store {
	t.Helper()
	dbs := map[string]config.Database{
		config.DriverSQLite: {Driver: config.DriverSQLite, DSN: filepath.Join(t.TempDir(), "ferry.db")},
	}
	for driver, env := range map[string]string{
		config.DriverPostgres: "FERRY_TEST_POSTGRES_DSN",
		config.DriverMySQL:    "FERRY_TEST_MYSQL_DSN",
	} {
		if dsn := os.Getenv(env); dsn != "" {
			dbs[driver] = config.Database{Driver: driver, DSN: dsn}
		}
	}
	out := map[string]*Store{}
	for driver, db := range dbs {
		s, err := Open(db)
		if err != nil {
			t.Fatalf("%s：%v", driver, err)
		}
		t.Cleanup(func() { _ = s.Close() })
		if err := s.DB.Migrator().DropTable(models...); err != nil {
			t.Fatalf("%s 清空表：%v", driver, err)
		}
		if err := s.Migrate(); err != nil {
			t.Fatalf("%s 迁移：%v", driver, err)
		}
		out[driver] = s
	}
	return out
}

func TestMigrateIsIdempotent(t *testing.T) {
	for driver, s := range testStores(t) {
		t.Run(driver, func(t *testing.T) {
			if err := s.Migrate(); err != nil {
				t.Fatalf("第二次迁移失败：%v", err)
			}
			if err := s.Ping(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestUniqueUsername(t *testing.T) {
	for driver, s := range testStores(t) {
		t.Run(driver, func(t *testing.T) {
			if err := s.DB.Create(&User{Username: "admin", PasswordHash: "x"}).Error; err != nil {
				t.Fatal(err)
			}
			err := s.DB.Create(&User{Username: "admin", PasswordHash: "y"}).Error
			if !errors.Is(err, gorm.ErrDuplicatedKey) {
				t.Errorf("重复用户名 err = %v，期望 ErrDuplicatedKey", err)
			}
		})
	}
}

func TestSessionCascadeDelete(t *testing.T) {
	for driver, s := range testStores(t) {
		t.Run(driver, func(t *testing.T) {
			u := User{Username: "admin", PasswordHash: "x"}
			if err := s.DB.Create(&u).Error; err != nil {
				t.Fatal(err)
			}
			now := time.Now()
			sess := Session{ID: "abc", UserID: u.ID, ExpiresAt: now.Add(time.Hour), LastSeenAt: now}
			if err := s.DB.Create(&sess).Error; err != nil {
				t.Fatal(err)
			}
			if err := s.DB.Delete(&u).Error; err != nil {
				t.Fatal(err)
			}
			var n int64
			s.DB.Model(&Session{}).Count(&n)
			if n != 0 {
				t.Errorf("删除用户后还剩 %d 个会话，外键级联没有生效", n)
			}
		})
	}
}

func TestSessionRequiresUser(t *testing.T) {
	for driver, s := range testStores(t) {
		t.Run(driver, func(t *testing.T) {
			err := s.DB.Create(&Session{ID: "orphan", UserID: 999, ExpiresAt: time.Now(), LastSeenAt: time.Now()}).Error
			if err == nil {
				t.Error("引用不存在的用户应当失败，外键没有生效")
			}
		})
	}
}

func TestSettingUpsert(t *testing.T) {
	for driver, s := range testStores(t) {
		t.Run(driver, func(t *testing.T) {
			for _, v := range []string{"a", "b"} {
				if err := s.DB.Save(&Setting{Key: "k", Value: v}).Error; err != nil {
					t.Fatal(err)
				}
			}
			var got Setting
			if err := s.DB.Where(&Setting{Key: "k"}).First(&got).Error; err != nil {
				t.Fatal(err)
			}
			if got.Value != "b" {
				t.Errorf("Value = %q，期望 b", got.Value)
			}
		})
	}
}

func TestTransportUniqueness(t *testing.T) {
	for driver, s := range testStores(t) {
		t.Run(driver, func(t *testing.T) {
			sub := func(name string, kid *string) error {
				return s.DB.Create(&Subscription{Name: name, Client: "surge", EndpointID: 1, TokenHash: name, TokenPrefix: "fs_", TokenSealed: "-", SourceKeys: "[]", TransportKID: kid}).Error
			}
			// 没开过加密的订阅编号都是 NULL，不能互相冲突。
			if err := sub("a", nil); err != nil {
				t.Fatal(err)
			}
			if err := sub("b", nil); err != nil {
				t.Fatalf("两个空编号：%v", err)
			}
			kid := "k1"
			if err := sub("c", &kid); err != nil {
				t.Fatal(err)
			}
			if err := sub("d", &kid); !errors.Is(err, gorm.ErrDuplicatedKey) {
				t.Errorf("重复编号 err = %v，期望 ErrDuplicatedKey", err)
			}
			var m Subscription
			if err := s.DB.Where("name = ?", "a").First(&m).Error; err != nil || m.TransportMode != TransportPlain {
				t.Errorf("默认传输方式 = %q, %v", m.TransportMode, err)
			}

			exp := time.Now().Add(time.Minute)
			if err := s.DB.Create(&TransportNonce{SubscriptionID: 1, Nonce: "n", ExpiresAt: exp}).Error; err != nil {
				t.Fatal(err)
			}
			if err := s.DB.Create(&TransportNonce{SubscriptionID: 1, Nonce: "n", ExpiresAt: exp}).Error; !errors.Is(err, gorm.ErrDuplicatedKey) {
				t.Errorf("重复 nonce err = %v，期望 ErrDuplicatedKey", err)
			}
			if err := s.DB.Create(&TransportNonce{SubscriptionID: 2, Nonce: "n", ExpiresAt: exp}).Error; err != nil {
				t.Errorf("不同订阅的相同 nonce：%v", err)
			}
		})
	}
}

// 已有的订阅在升级迁移后应当是明文传输，不受影响。
// TestSubscriptionColumnsOnExistingRows 模拟旧库：删掉后来加的列，升级迁移后旧订阅取默认值。
func TestSubscriptionColumnsOnExistingRows(t *testing.T) {
	for driver, s := range testStores(t) {
		t.Run(driver, func(t *testing.T) {
			for _, col := range []string{"ScriptURL", "TransportMode", "TransportKID", "TransportKeySealed", "TransportVerifiedAt", "TransportLastAt", "FileFetchedAt", "FileMitmHash"} {
				if err := s.DB.Migrator().DropColumn(&Subscription{}, col); err != nil {
					t.Fatalf("删列 %s：%v", col, err)
				}
			}
			now := time.Now()
			row := map[string]any{"name": "old", "client": "surge", "endpoint_id": 1, "token_hash": "h", "token_prefix": "fs_", "token_sealed": "-", "all_sources": true, "source_keys": "[]", "created_at": now, "updated_at": now}
			if err := s.DB.Table("subscriptions").Create(row).Error; err != nil {
				t.Fatal(err)
			}
			if err := s.Migrate(); err != nil {
				t.Fatalf("升级迁移：%v", err)
			}
			var m Subscription
			if err := s.DB.Where("name = ?", "old").First(&m).Error; err != nil {
				t.Fatal(err)
			}
			if m.TransportMode != TransportPlain || m.TransportKID != nil || m.ScriptURL != "" || m.FileFetchedAt != nil || m.FileMitmHash != "" {
				t.Errorf("升级后的旧订阅 = %q %v %v %q", m.TransportMode, m.TransportKID, m.FileFetchedAt, m.FileMitmHash)
			}
		})
	}
}

// TestSourceIconFitsLargestImage 确认来源图标列放得下上限大小的图片：64KB 图片 base64 后约 87KB，超过 MySQL TEXT 的 64KB。
// 同时模拟旧库没有这一列，升级迁移后补上、旧行为空。
func TestSourceIconFitsLargestImage(t *testing.T) {
	for driver, s := range testStores(t) {
		t.Run(driver, func(t *testing.T) {
			if err := s.DB.Migrator().DropColumn(&Source{}, "Icon"); err != nil {
				t.Fatalf("删列：%v", err)
			}
			now := time.Now()
			old := map[string]any{"key": "old", "name": "旧来源", "enabled": true, "merge_window_sec": 60, "notify_mode": "always", "min_interval_sec": 60, "debug": false, "review_mode": "auto", "created_at": now, "updated_at": now}
			if err := s.DB.Table("sources").Create(old).Error; err != nil {
				t.Fatal(err)
			}
			if err := s.Migrate(); err != nil {
				t.Fatalf("升级迁移：%v", err)
			}
			var got Source
			if err := s.DB.Where(&Source{Key: "old"}).First(&got).Error; err != nil {
				t.Fatal(err)
			}
			if got.Icon != "" {
				t.Errorf("旧来源的图标 = %q，期望为空", got.Icon)
			}

			icon := "data:image/png;base64," + strings.Repeat("A", 87384)
			src := Source{Key: "big", Name: "大图标", Icon: icon, Enabled: true, NotifyMode: "always"}
			if err := s.DB.Create(&src).Error; err != nil {
				t.Fatalf("保存：%v", err)
			}
			var back Source
			if err := s.DB.First(&back, src.ID).Error; err != nil {
				t.Fatal(err)
			}
			if back.Icon != icon {
				t.Errorf("读回的图标长度 %d，期望 %d", len(back.Icon), len(icon))
			}
		})
	}
}
