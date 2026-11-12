package config

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var testKey = base64.StdEncoding.EncodeToString(make([]byte, 32))

func valid() *Config {
	return &Config{Listen: ":9000", Database: Database{Driver: DriverSQLite, DSN: "ferry.db"}, MasterKey: testKey}
}

func TestLoadMissingFile(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "ferry.yaml"))
	if !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("err = %v，期望 ErrNotInitialized", err)
	}
	if c == nil || c.Listen != ":8080" {
		t.Errorf("未初始化时应返回带默认值的部分配置，得到 %+v", c)
	}
}

func TestLoadPartialKeepsListen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ferry.yaml")
	if err := os.WriteFile(path, []byte("listen: 127.0.0.1:9100\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if !errors.Is(err, ErrNotInitialized) || c.Listen != "127.0.0.1:9100" {
		t.Errorf("Load = %+v, %v；期望保留 listen 并返回 ErrNotInitialized", c, err)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ferry.yaml")
	if err := valid().Save(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("权限 = %o，期望 600", perm)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if *got != *valid() {
		t.Errorf("Load = %+v，期望 %+v", got, valid())
	}
}

func TestEnvOverridesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ferry.yaml")
	if err := valid().Save(path); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FERRY_LISTEN", ":7000")
	t.Setenv("FERRY_DATABASE_DRIVER", DriverPostgres)
	t.Setenv("FERRY_DATABASE_DSN", "postgres://u@h/db")
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Listen != ":7000" || got.Database.Driver != DriverPostgres || got.Database.DSN != "postgres://u@h/db" {
		t.Errorf("环境变量没有覆盖文件：%+v", got)
	}
}

func TestEnvOnly(t *testing.T) {
	t.Setenv("FERRY_DATABASE_DRIVER", DriverSQLite)
	t.Setenv("FERRY_DATABASE_DSN", "/data/ferry.db")
	t.Setenv("FERRY_MASTER_KEY", testKey)
	got, err := Load(filepath.Join(t.TempDir(), "ferry.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Listen != ":8080" {
		t.Errorf("Listen 默认值 = %q，期望 :8080", got.Listen)
	}
}

func TestValidate(t *testing.T) {
	cases := map[string]struct {
		mutate func(*Config)
		want   string
	}{
		"缺驱动":   {func(c *Config) { c.Database.Driver = "" }, "driver 不能为空"},
		"未知驱动":  {func(c *Config) { c.Database.Driver = "oracle" }, "不支持"},
		"缺 DSN": {func(c *Config) { c.Database.DSN = "" }, "dsn 不能为空"},
		"密钥非法":  {func(c *Config) { c.MasterKey = "!!" }, "base64"},
		"密钥太短":  {func(c *Config) { c.MasterKey = base64.StdEncoding.EncodeToString(make([]byte, 16)) }, "32 字节"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := valid()
			tc.mutate(c)
			err := c.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Validate() = %v，期望包含 %q", err, tc.want)
			}
		})
	}
}
