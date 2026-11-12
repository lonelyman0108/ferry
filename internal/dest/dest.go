// Package dest 是推送目标的适配器：青龙（OpenAPI 环境变量）和通用 Webhook。
package dest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// 目的地类型。
const (
	KindQinglong = "qinglong"
	KindWebhook  = "webhook"
)

// Var 是要写到目的地的一个变量。Remarks 用来在目的地里认出这是 Ferry 管理的哪一条。
type Var struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	Remarks string `json:"remarks"`
}

// Push 是一次推送的内容。
type Push struct {
	SourceKey  string `json:"source"`
	SourceName string `json:"source_name"`
	// IdentityKey 在“合成一条”模式下为空。
	IdentityKey string `json:"identity,omitempty"`
	Vars        []Var  `json:"vars"`
}

// Adapter 是一个目的地。
type Adapter interface {
	// Upsert 把变量写进目的地：按 Remarks 和 Name 找到已有的就更新，没有就新建，并确保启用。
	Upsert(ctx context.Context, p Push) error
	// Test 检查配置能否连通，不改任何数据。
	Test(ctx context.Context) error
}

// Config 是目的地的配置，按 Kind 使用其中一部分。带 secret 标记的字段在接口里只写不读。
type Config struct {
	// 青龙
	BaseURL      string `json:"base_url,omitempty"`
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
	// Webhook
	URL     string            `json:"url,omitempty"`
	Method  string            `json:"method,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
	Secret  string            `json:"secret,omitempty"`
}

// Redacted 返回去掉密钥的配置，以及哪些密钥已经设置。
func (c Config) Redacted() (Config, map[string]bool) {
	set := map[string]bool{"client_secret": c.ClientSecret != "", "secret": c.Secret != ""}
	c.ClientSecret, c.Secret = "", ""
	return c, set
}

// MergeSecrets 用旧配置补上新配置里留空的密钥：接口里密钥只写不读，更新时留空表示不改。
func (c Config) MergeSecrets(old Config) Config {
	if c.ClientSecret == "" {
		c.ClientSecret = old.ClientSecret
	}
	if c.Secret == "" {
		c.Secret = old.Secret
	}
	return c
}

// Validate 按类型检查配置。
func Validate(kind string, c Config) error {
	switch kind {
	case KindQinglong:
		return validateQinglong(c)
	case KindWebhook:
		return validateWebhook(c)
	}
	return fmt.Errorf("不支持的目的地类型 %q", kind)
}

// New 按类型创建适配器。client 为 nil 时使用带 15 秒超时的默认客户端。
func New(kind string, c Config, client *http.Client) (Adapter, error) {
	if err := Validate(kind, c); err != nil {
		return nil, err
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	switch kind {
	case KindQinglong:
		return &qinglong{cfg: c, http: client}, nil
	case KindWebhook:
		return &webhook{cfg: c, http: client}, nil
	}
	return nil, errors.New("不支持的目的地类型")
}

// Marshal 和 Unmarshal 在配置和 JSON 之间转换，存库前由调用方加密。
func Marshal(c Config) ([]byte, error) { return json.Marshal(c) }

// Unmarshal 见 Marshal。
func Unmarshal(b []byte) (Config, error) {
	var c Config
	err := json.Unmarshal(b, &c)
	return c, err
}
