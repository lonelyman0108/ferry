// Package notify 按通知规则把事件发到 Bark、Telegram、Webhook。通知里只有来源、身份和字段名，不含任何值。
package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// 渠道类型。
const (
	KindBark     = "bark"
	KindTelegram = "telegram"
	KindWebhook  = "webhook"
)

// Kinds 按界面展示顺序列出渠道类型。
var Kinds = []string{KindBark, KindTelegram, KindWebhook}

// Config 是渠道配置，按 Kind 使用其中一部分。DeviceKey、BotToken、Secret 只写不读。
type Config struct {
	// Bark：Server 默认 https://api.day.app
	Server    string `json:"server,omitempty"`
	DeviceKey string `json:"device_key,omitempty"`
	// Telegram：APIBase 默认 https://api.telegram.org，可以填反代地址
	BotToken string `json:"bot_token,omitempty"`
	ChatID   string `json:"chat_id,omitempty"`
	APIBase  string `json:"api_base,omitempty"`
	// Webhook
	URL    string `json:"url,omitempty"`
	Secret string `json:"secret,omitempty"`
}

// Redacted 返回去掉密钥的配置，以及哪些密钥已经设置。
func (c Config) Redacted() (Config, map[string]bool) {
	set := map[string]bool{"device_key": c.DeviceKey != "", "bot_token": c.BotToken != "", "secret": c.Secret != ""}
	c.DeviceKey, c.BotToken, c.Secret = "", "", ""
	return c, set
}

// MergeSecrets 用旧配置补上留空的密钥。
func (c Config) MergeSecrets(old Config) Config {
	if c.DeviceKey == "" {
		c.DeviceKey = old.DeviceKey
	}
	if c.BotToken == "" {
		c.BotToken = old.BotToken
	}
	if c.Secret == "" {
		c.Secret = old.Secret
	}
	return c
}

func httpURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// Normalize 整理配置：Bark 可以直接填 App 里显示的完整推送地址（如 https://api.day.app/<key>，
// 或挂在子路径下的 https://example.com/bark/<key>），拆成服务器地址和 device key。
func Normalize(kind string, c Config) Config {
	if kind == KindBark && c.URL != "" {
		if u, err := url.Parse(strings.TrimRight(c.URL, "/")); err == nil && u.Host != "" {
			if i := strings.LastIndex(u.Path, "/"); i >= 0 && u.Path[i+1:] != "" {
				c.DeviceKey = u.Path[i+1:]
				c.Server = u.Scheme + "://" + u.Host + u.Path[:i]
			}
		}
		c.URL = ""
	}
	return c
}

// Validate 按类型检查配置。
func Validate(kind string, c Config) error {
	switch kind {
	case KindBark:
		if c.DeviceKey == "" {
			return errors.New("缺少 Bark device key")
		}
		if c.Server != "" && !httpURL(c.Server) {
			return errors.New("Bark 服务器地址需以 http:// 或 https:// 开头")
		}
	case KindTelegram:
		if c.BotToken == "" || c.ChatID == "" {
			return errors.New("缺少 Telegram bot token 或 chat id")
		}
		if c.APIBase != "" && !httpURL(c.APIBase) {
			return errors.New("Telegram API 地址需以 http:// 或 https:// 开头")
		}
	case KindWebhook:
		if !httpURL(c.URL) {
			return errors.New("Webhook 地址需以 http:// 或 https:// 开头")
		}
	default:
		return fmt.Errorf("不支持的渠道类型 %q", kind)
	}
	return nil
}

// Message 是一条通知。
type Message struct {
	Event string    `json:"event"`
	Title string    `json:"title"`
	Body  string    `json:"body"`
	Link  string    `json:"link,omitempty"`
	At    time.Time `json:"at"`
}

// Send 通过渠道发出通知。
func Send(ctx context.Context, client *http.Client, kind string, c Config, m Message) error {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	switch kind {
	case KindBark:
		server := strings.TrimRight(c.Server, "/")
		if server == "" {
			server = "https://api.day.app"
		}
		body := map[string]any{"device_key": c.DeviceKey, "title": m.Title, "body": m.Body, "group": "Ferry"}
		if m.Link != "" {
			body["url"] = m.Link
		}
		return postJSON(ctx, client, server+"/push", body, nil, func(raw []byte) error {
			var r struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			}
			if json.Unmarshal(raw, &r) == nil && r.Code != 0 && r.Code != 200 {
				return fmt.Errorf("Bark 返回 %d：%s", r.Code, r.Message)
			}
			return nil
		})
	case KindTelegram:
		base := strings.TrimRight(c.APIBase, "/")
		if base == "" {
			base = "https://api.telegram.org"
		}
		text := m.Title + "\n" + m.Body
		if m.Link != "" {
			text += "\n" + m.Link
		}
		body := map[string]any{"chat_id": c.ChatID, "text": text, "disable_web_page_preview": true}
		return postJSON(ctx, client, base+"/bot"+c.BotToken+"/sendMessage", body, nil, func(raw []byte) error {
			var r struct {
				OK          bool   `json:"ok"`
				Description string `json:"description"`
			}
			if json.Unmarshal(raw, &r) == nil && !r.OK {
				return fmt.Errorf("Telegram 返回错误：%s", r.Description)
			}
			return nil
		})
	case KindWebhook:
		raw, err := json.Marshal(m)
		if err != nil {
			return err
		}
		headers := map[string]string{}
		if c.Secret != "" {
			mac := hmac.New(sha256.New, []byte(c.Secret))
			mac.Write(raw)
			headers["X-Ferry-Signature"] = "sha256=" + hex.EncodeToString(mac.Sum(nil))
		}
		return post(ctx, client, c.URL, raw, headers, nil)
	}
	return fmt.Errorf("不支持的渠道类型 %q", kind)
}

func postJSON(ctx context.Context, client *http.Client, u string, body any, headers map[string]string, check func([]byte) error) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return post(ctx, client, u, raw, headers, check)
}

// post 发请求。错误信息里不带地址：Telegram 的 bot token 在路径里，Webhook 地址也可能带令牌。
func post(ctx context.Context, client *http.Client, u string, raw []byte, headers map[string]string, check func([]byte) error) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(raw))
	if err != nil {
		return errors.New("通知地址不合法")
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("User-Agent", "Ferry")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("发送失败：%w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if check != nil {
		if err := check(data); err != nil {
			return err
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("对方返回 HTTP %d", resp.StatusCode)
	}
	return nil
}
