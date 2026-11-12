// Package config 读写 ferry.yaml，并允许用 FERRY_* 环境变量覆盖其中的值。
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

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
	Listen   string   `yaml:"listen"`
	Database Database `yaml:"database"`
	// MasterKey 是 base64 编码的 32 字节密钥，用来加密敏感字段；丢失后这些字段无法解密。
	MasterKey string `yaml:"master_key"`
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
}{
	{"FERRY_LISTEN", func(c *Config, v string) { c.Listen = v }},
	{"FERRY_DATABASE_DRIVER", func(c *Config, v string) { c.Database.Driver = v }},
	{"FERRY_DATABASE_DSN", func(c *Config, v string) { c.Database.DSN = v }},
	{"FERRY_MASTER_KEY", func(c *Config, v string) { c.MasterKey = v }},
}

// Load 读取 path 处的配置文件，再叠加环境变量。
// 文件和环境变量都没有给出数据库和主密钥时，返回 ErrNotInitialized，
// 同时返回已读到的部分配置（如 listen），供初始化向导使用。
func Load(path string) (*Config, error) {
	c := &Config{}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := yaml.Unmarshal(data, c); err != nil {
			return nil, fmt.Errorf("解析 %s：%w", path, err)
		}
	case errors.Is(err, fs.ErrNotExist):
	default:
		return nil, err
	}

	for _, o := range envOverrides {
		if v, ok := os.LookupEnv(o.name); ok {
			o.set(c, v)
		}
	}
	c.applyDefaults()
	if c.Database.Driver == "" && c.Database.DSN == "" && c.MasterKey == "" {
		return c, ErrNotInitialized
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) applyDefaults() {
	if c.Listen == "" {
		c.Listen = ":8080"
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
func (c *Config) Save(path string) error {
	if err := c.Validate(); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
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
