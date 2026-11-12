// Package store 封装数据库连接和表结构，支持 SQLite、PostgreSQL、MySQL。
package store

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/lonelyman0108/ferry/internal/config"
)

// Store 持有数据库连接。
type Store struct {
	DB *gorm.DB
}

// models 是需要自动迁移的全部表，新增模型时加在这里。
var models = []any{
	&User{},
	&Session{},
	&APIToken{},
	&AuditLog{},
	&Setting{},
	&Source{},
	&Matcher{},
	&Field{},
	&Output{},
	&Endpoint{},
	&Subscription{},
	&TransportNonce{},
	&Identity{},
	&Batch{},
	&Version{},
	&Destination{},
	&Binding{},
	&PushJob{},
	&Channel{},
	&NotifyRule{},
}

// Open 按配置连接数据库，不做迁移。
func Open(db config.Database) (*Store, error) {
	var dialector gorm.Dialector
	switch db.Driver {
	case config.DriverSQLite:
		dialector = sqlite.Open(sqliteDSN(db.DSN))
	case config.DriverPostgres:
		dialector = postgres.Open(db.DSN)
	case config.DriverMySQL:
		dialector = mysql.Open(db.DSN)
	default:
		return nil, fmt.Errorf("不支持的数据库驱动：%q", db.Driver)
	}
	g, err := gorm.Open(dialector, &gorm.Config{
		// 查不到记录在业务里是正常分支，不打印成错误。
		Logger: logger.New(log.New(os.Stderr, "", log.LstdFlags), logger.Config{
			SlowThreshold: 200 * time.Millisecond, LogLevel: logger.Warn, IgnoreRecordNotFoundError: true,
		}),
		TranslateError: true,
	})
	if err != nil {
		return nil, fmt.Errorf("连接数据库：%w", err)
	}
	if db.Driver == config.DriverSQLite {
		// SQLite 同一时刻只允许一个写入者，单连接可以避免 database is locked。
		sqlDB, err := g.DB()
		if err != nil {
			return nil, err
		}
		sqlDB.SetMaxOpenConns(1)
	}
	return &Store{DB: g}, nil
}

// sqliteDSN 给文件路径补上外键约束、WAL 和忙等待；已经带参数的 DSN 原样使用。
func sqliteDSN(path string) string {
	if strings.Contains(path, "?") {
		return path
	}
	return path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
}

// Ping 检查连接是否可用，初始化向导测连通时用。
func (s *Store) Ping() error {
	sqlDB, err := s.DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Ping()
}

// Migrate 建表或补齐缺少的列和索引。
func (s *Store) Migrate() error {
	return s.DB.AutoMigrate(models...)
}

// Close 关闭连接。
func (s *Store) Close() error {
	sqlDB, err := s.DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
