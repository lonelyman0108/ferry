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
	if got.Database != valid().Database || got.MasterKey != testKey {
		t.Errorf("Load = %+v，期望数据库与主密钥和保存时一致", got)
	}
	if got.Listen != ":9000" || got.Sources.DSN.From != SourceFile || got.Sources.MasterKey.From != SourceFile || got.Sources.Listen.From != SourceFile {
		t.Errorf("来源 = %+v", got.Sources)
	}
}

// 配置文件里有的项以文件为准，文件里没有的才用环境变量。
func TestFileOverridesEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ferry.yaml")
	c := valid()
	c.Listen = ""
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FERRY_LISTEN", ":7000")
	t.Setenv("FERRY_DATABASE_DRIVER", DriverPostgres)
	t.Setenv("FERRY_DATABASE_DSN", "postgres://u@h/db")
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Database != valid().Database {
		t.Errorf("文件里的数据库应优先：%+v", got.Database)
	}
	if got.Listen != ":7000" || got.Sources.Listen != (Source{From: SourceEnv, Env: "FERRY_LISTEN"}) {
		t.Errorf("文件里没有监听地址时应用环境变量：%q %+v", got.Listen, got.Sources.Listen)
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
	if got.Listen != ":8080" || got.Sources.MasterKey.Env != "FERRY_MASTER_KEY" {
		t.Errorf("Load = %+v", got)
	}
}

// 只给了数据库、没给主密钥：进向导，并带着已有的数据库信息供回填。
func TestPartialEnvGoesToWizard(t *testing.T) {
	t.Setenv("FERRY_DATABASE_URL", "postgres://u:p@db:5432/ferry")
	got, err := Load(filepath.Join(t.TempDir(), "ferry.yaml"))
	if !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("err = %v，期望 ErrNotInitialized", err)
	}
	if got.Database.Driver != DriverPostgres || got.Sources.DSN.Env != "FERRY_DATABASE_URL" {
		t.Errorf("部分配置 = %+v", got)
	}
}

// 主密钥和监听地址来自环境变量时都不写进文件。
func TestSaveOmitsEnvKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ferry.yaml")
	c := valid()
	c.Sources.MasterKey = Source{From: SourceEnv, Env: "FERRY_MASTER_KEY"}
	c.Sources.Listen = Source{From: SourceEnv, Env: "FERRY_LISTEN"}
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "master_key") || strings.Contains(string(data), "listen") {
		t.Errorf("文件里不该有主密钥和监听地址：\n%s", data)
	}
	if !strings.Contains(string(data), "ferry.db") {
		t.Errorf("文件里应有数据库：\n%s", data)
	}
}

func TestParseURL(t *testing.T) {
	cases := map[string]Database{
		"postgres://u:p@h:5432/db?sslmode=disable": {DriverPostgres, "postgres://u:p@h:5432/db?sslmode=disable"},
		"postgresql://h/db":                        {DriverPostgres, "postgresql://h/db"},
		"sqlite:///data/ferry.db":                  {DriverSQLite, "/data/ferry.db"},
		"sqlite://ferry.db":                        {DriverSQLite, "ferry.db"},
		"mysql://u:p%40ss@h:3306/db":               {DriverMySQL, "u:p@ss@tcp(h:3306)/db?charset=utf8mb4&loc=UTC&parseTime=True"},
		"mysql://u@h/db?loc=Local":                 {DriverMySQL, "u@tcp(h)/db?charset=utf8mb4&loc=Local&parseTime=True"},
	}
	for in, want := range cases {
		got, err := ParseURL(in)
		if err != nil || got != want {
			t.Errorf("ParseURL(%q) = %+v, %v；期望 %+v", in, got, err, want)
		}
	}
	for _, bad := range []string{"ferry.db", "oracle://h/db", "mysql://h", "sqlite://"} {
		if _, err := ParseURL(bad); err == nil {
			t.Errorf("ParseURL(%q) 应报错", bad)
		}
	}
}

// 两种写法都给时，以 FERRY_DATABASE_DRIVER / FERRY_DATABASE_DSN 为准。
func TestDriverDSNOverURL(t *testing.T) {
	t.Setenv("FERRY_DATABASE_URL", "postgres://h/db")
	t.Setenv("FERRY_DATABASE_DRIVER", DriverSQLite)
	t.Setenv("FERRY_DATABASE_DSN", "x.db")
	got, _ := Load(filepath.Join(t.TempDir(), "ferry.yaml"))
	if got.Database != (Database{DriverSQLite, "x.db"}) || got.Sources.DSN.Env != "FERRY_DATABASE_DSN" {
		t.Errorf("Load = %+v", got)
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
