package server

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lonelyman0108/ferry/internal/config"
)

func TestMaskDSN(t *testing.T) {
	cases := map[string]string{
		"postgres://u:secret@h:5432/db?sslmode=disable": "postgres://u:••••@h:5432/db?sslmode=disable",
		"u:secret@tcp(h:3306)/db?parseTime=True":        "u:••••@tcp(h:3306)/db?parseTime=True",
		"host=h user=u password=secret dbname=db":       "host=h user=u password=•••• dbname=db",
		"host=h password='with space' dbname=db":        "host=h password=•••• dbname=db",
		"/data/ferry.db":                                "/data/ferry.db",
		"postgres://h/db":                               "postgres://h/db",
	}
	for in, want := range cases {
		if got := maskDSN(in); got != want {
			t.Errorf("maskDSN(%q) = %q，期望 %q", in, got, want)
		}
	}
}

// 向导提交回填的打码连接串时换回原值；用户改过就用改后的。
func TestResolveMaskedDSN(t *testing.T) {
	cfg := &config.Config{Database: config.Database{Driver: config.DriverPostgres, DSN: "postgres://u:secret@h/db"}}
	h := newHarness(t, Options{Config: cfg})
	if got := h.srv.resolve(databaseInput{Driver: "postgres", DSN: "postgres://u:••••@h/db"}); got.DSN != "postgres://u:secret@h/db" {
		t.Errorf("没改时应换回原值，得到 %q", got.DSN)
	}
	if got := h.srv.resolve(databaseInput{Driver: "postgres", DSN: "postgres://u:new@h/db"}); got.DSN != "postgres://u:new@h/db" {
		t.Errorf("改过时应用新值，得到 %q", got.DSN)
	}
	if got := h.srv.resolve(databaseInput{Driver: "mysql", DSN: "postgres://u:••••@h/db"}); got.DSN != "postgres://u:••••@h/db" {
		t.Errorf("换了驱动就不算原值，得到 %q", got.DSN)
	}
}

// 环境变量给了数据库和主密钥、但没有管理员以外的配置文件：向导回填数据库，沿用主密钥，不把它写进文件。
func TestSetupWithPreset(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "ferry.db")
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	cfg := &config.Config{
		Listen:    ":8080",
		Database:  config.Database{Driver: config.DriverSQLite, DSN: dbPath},
		MasterKey: key,
		Sources: config.Sources{
			Driver:    config.Source{From: config.SourceEnv, Env: "FERRY_DATABASE_URL"},
			DSN:       config.Source{From: config.SourceEnv, Env: "FERRY_DATABASE_URL"},
			MasterKey: config.Source{From: config.SourceEnv, Env: "FERRY_MASTER_KEY"},
		},
	}
	h := newHarness(t, Options{Config: cfg, ConfigPath: filepath.Join(dir, "ferry.yaml")})

	if code := h.do("GET", "/api/v1/setup/preset", nil, nil); code != 403 {
		t.Errorf("没有向导令牌时 = %d，期望 403", code)
	}
	var pre setupPreset
	if code := h.do("GET", "/api/v1/setup/preset", nil, &pre, setupTokenHeader, h.srv.SetupToken()); code != 200 {
		t.Fatalf("回填信息 = %d", code)
	}
	if pre.Driver != "sqlite" || pre.DSN != dbPath || !pre.MasterKey || pre.DSNFrom.Env != "FERRY_DATABASE_URL" || pre.MasterKeyFrom.From != config.SourceEnv {
		t.Errorf("回填信息 = %+v", pre)
	}

	code, res := h.setup(map[string]any{"database": map[string]string{"driver": pre.Driver, "dsn": pre.DSN}, "admin": admin("password123")})
	if code != 200 || res.MasterKey != "" {
		t.Fatalf("初始化 = %d %+v，主密钥来自环境变量时不应返回", code, res)
	}
	data, err := os.ReadFile(filepath.Join(dir, "ferry.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "master_key") || !strings.Contains(string(data), dbPath) {
		t.Errorf("配置文件应有数据库、不应有主密钥：\n%s", data)
	}
	if h.srv.cfg.MasterKey != key {
		t.Error("应沿用环境变量里的主密钥")
	}
}
