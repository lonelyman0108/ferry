// Package config 读写 ferry.yaml，文件里没有的项用 FERRY_* 环境变量补上。
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// 支持的数据库驱动。
const (
	DriverSQLite   = "sqlite"
	DriverPostgres = "postgres"
	DriverMySQL    = "mysql"
)

// ErrNotInitialized 表示既没有配置文件，环境变量也不足以组成完整配置，需要走初始化向导。
var ErrNotInitialized = errors.New("ferry 尚未初始化")

// Config 是 ferry.yaml 的内容。
type Config struct {
	Listen   string   `yaml:"listen,omitempty"`
	Database Database `yaml:"database"`
	// MasterKey 是 base64 编码的 32 字节密钥，用来加密敏感字段；丢失后这些字段无法解密。
	MasterKey string `yaml:"master_key,omitempty"`
	// Sources 记录每一项从哪里来，给初始化向导和系统信息页用，不写进文件。
	Sources Sources `yaml:"-"`
}

// 配置项的来源。
const (
	SourceFile    = "file"
	SourceEnv     = "env"
	SourceDefault = "default"
)

// Sources 是各配置项的来源：SourceFile、SourceEnv、SourceDefault，没给出时为空。
// Env 是给出这一项的环境变量名，来源不是环境变量时为空。
type Sources struct {
	Listen    Source `json:"listen"`
	Driver    Source `json:"driver"`
	DSN       Source `json:"dsn"`
	MasterKey Source `json:"master_key"`
}

// Source 是一个配置项的来源。
type Source struct {
	From string `json:"from"`
	Env  string `json:"env,omitempty"`
}

// Database 描述数据库连接。SQLite 的 DSN 是文件路径。
type Database struct {
	Driver string `yaml:"driver"`
	DSN    string `yaml:"dsn"`
}

// 环境变量名到字段的映射，Docker 部署时可以只用环境变量、不写配置文件。
var envOverrides = []struct {
	name string
	set  func(*Config, string)
	src  func(*Sources) *Source
}{
	{"FERRY_LISTEN", func(c *Config, v string) { c.Listen = v }, func(s *Sources) *Source { return &s.Listen }},
	{"FERRY_DATABASE_DRIVER", func(c *Config, v string) { c.Database.Driver = v }, func(s *Sources) *Source { return &s.Driver }},
	{"FERRY_DATABASE_DSN", func(c *Config, v string) { c.Database.DSN = v }, func(s *Sources) *Source { return &s.DSN }},
	{"FERRY_MASTER_KEY", func(c *Config, v string) { c.MasterKey = v }, func(s *Sources) *Source { return &s.MasterKey }},
}

// Load 读取配置：配置文件里有的项以文件为准，文件里没有的才用环境变量补上，监听地址最后用默认值兜底。
// 数据库驱动、连接串和主密钥三项不全时返回 ErrNotInitialized，同时返回已读到的部分配置，
// 供初始化向导回填。
func Load(path string) (*Config, error) {
	c := &Config{}
	if err := c.fromEnv(); err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		var f Config
		if err := yaml.Unmarshal(data, &f); err != nil {
			return nil, fmt.Errorf("解析 %s：%w", path, err)
		}
		file := SourceFile
		for _, v := range []struct {
			val string
			dst *string
			src *Source
		}{
			{f.Listen, &c.Listen, &c.Sources.Listen},
			{f.Database.Driver, &c.Database.Driver, &c.Sources.Driver},
			{f.Database.DSN, &c.Database.DSN, &c.Sources.DSN},
			{f.MasterKey, &c.MasterKey, &c.Sources.MasterKey},
		} {
			if v.val != "" {
				*v.dst, *v.src = v.val, Source{From: file}
			}
		}
	case errors.Is(err, fs.ErrNotExist):
	default:
		return nil, err
	}

	c.applyDefaults()
	if !c.Complete() {
		return c, ErrNotInitialized
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// fromEnv 读环境变量。FERRY_DATABASE_URL 用一个 URL 同时给出驱动和连接串，
// 和 FERRY_DATABASE_DRIVER、FERRY_DATABASE_DSN 同时给出时，以后两者为准。
func (c *Config) fromEnv() error {
	if v, ok := os.LookupEnv("FERRY_DATABASE_URL"); ok && v != "" {
		db, err := ParseURL(v)
		if err != nil {
			return fmt.Errorf("FERRY_DATABASE_URL：%w", err)
		}
		c.Database = db
		c.Sources.Driver = Source{From: SourceEnv, Env: "FERRY_DATABASE_URL"}
		c.Sources.DSN = Source{From: SourceEnv, Env: "FERRY_DATABASE_URL"}
	}
	for _, o := range envOverrides {
		if v, ok := os.LookupEnv(o.name); ok && v != "" {
			o.set(c, v)
			*o.src(&c.Sources) = Source{From: SourceEnv, Env: o.name}
		}
	}
	return nil
}

// Complete 表示数据库驱动、连接串和主密钥都已给出，可以直接启动，不用走初始化向导。
func (c *Config) Complete() bool {
	return c.Database.Driver != "" && c.Database.DSN != "" && c.MasterKey != ""
}

// ParseURL 把 postgres://、postgresql://、mysql://、sqlite:// 开头的 URL 转成驱动和连接串。
// MySQL 的 URL 会转成 go-sql-driver 的写法，并默认补上 charset=utf8mb4、parseTime=True、loc=UTC。
func ParseURL(raw string) (Database, error) {
	scheme, rest, ok := strings.Cut(raw, "://")
	if !ok {
		return Database{}, errors.New("应以 postgres://、mysql:// 或 sqlite:// 开头")
	}
	switch strings.ToLower(scheme) {
	case "postgres", "postgresql":
		return Database{Driver: DriverPostgres, DSN: raw}, nil
	case "sqlite", "sqlite3":
		if rest == "" {
			return Database{}, errors.New("sqlite:// 后面要跟文件路径")
		}
		return Database{Driver: DriverSQLite, DSN: rest}, nil
	case "mysql":
		u, err := url.Parse(raw)
		if err != nil {
			return Database{}, err
		}
		if u.Host == "" || strings.Trim(u.Path, "/") == "" {
			return Database{}, errors.New("mysql:// 要写明主机和库名")
		}
		q := u.Query()
		for k, v := range map[string]string{"charset": "utf8mb4", "parseTime": "True", "loc": "UTC"} {
			if q.Get(k) == "" {
				q.Set(k, v)
			}
		}
		dsn := ""
		if u.User != nil {
			dsn = u.User.Username()
			if p, ok := u.User.Password(); ok {
				dsn += ":" + p
			}
			dsn += "@"
		}
		dsn += "tcp(" + u.Host + ")/" + strings.Trim(u.Path, "/") + "?" + q.Encode()
		return Database{Driver: DriverMySQL, DSN: dsn}, nil
	default:
		return Database{}, fmt.Errorf("不支持的数据库：%s", scheme)
	}
}

func (c *Config) applyDefaults() {
	if c.Listen == "" {
		c.Listen = ":8080"
		c.Sources.Listen = Source{From: SourceDefault}
	}
}

// Validate 检查配置是否完整、合法。
func (c *Config) Validate() error {
	switch c.Database.Driver {
	case DriverSQLite, DriverPostgres, DriverMySQL:
	case "":
		return errors.New("database.driver 不能为空")
	default:
		return fmt.Errorf("不支持的 database.driver：%q", c.Database.Driver)
	}
	if c.Database.DSN == "" {
		return errors.New("database.dsn 不能为空")
	}
	if _, err := c.Key(); err != nil {
		return err
	}
	return nil
}

// Key 返回解码后的主密钥。
func (c *Config) Key() ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(c.MasterKey)
	if err != nil {
		return nil, fmt.Errorf("master_key 不是合法的 base64：%w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("master_key 解码后应为 32 字节，实际 %d 字节", len(key))
	}
	return key, nil
}

// Save 把配置原子地写到 path，权限 0600，因为文件里有主密钥和数据库密码。
// 数据库总会写进去；主密钥来自环境变量时不写，免得磁盘上多一份；监听地址来自环境变量或默认值时也不写。
func (c *Config) Save(path string) error {
	if err := c.Validate(); err != nil {
		return err
	}
	out := Config{Database: c.Database}
	if c.Sources.MasterKey.From != SourceEnv {
		out.MasterKey = c.MasterKey
	}
	if f := c.Sources.Listen.From; f != SourceEnv && f != SourceDefault {
		out.Listen = c.Listen
	}
	data, err := yaml.Marshal(out)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".ferry-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
