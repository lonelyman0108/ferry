package dest

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
	"text/template"
)

// SignatureHeader 是 Webhook 签名所在的请求头，值为 sha256=<正文的 HMAC-SHA256 十六进制>。
const SignatureHeader = "X-Ferry-Signature"

// webhook 把一次推送发到任意 HTTP 地址。正文默认是 Push 的 JSON，也可以写成模板。
type webhook struct {
	cfg  Config
	http *http.Client
}

func validateWebhook(c Config) error {
	u, err := url.Parse(c.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("Webhook 地址需以 http:// 或 https:// 开头")
	}
	switch strings.ToUpper(c.Method) {
	case "", "POST", "PUT", "PATCH":
	default:
		return fmt.Errorf("Webhook 的 method 只能是 POST、PUT 或 PATCH")
	}
	if c.Body != "" {
		if _, err := parseBody(c.Body); err != nil {
			return err
		}
	}
	return nil
}

// parseBody 解析正文模板，数据是 Push，另外 vars 按变量名索引，例如 {{index .VarMap "JD_COOKIE"}}。
func parseBody(text string) (*template.Template, error) {
	t, err := template.New("body").Option("missingkey=error").Parse(text)
	if err != nil {
		return nil, fmt.Errorf("Webhook 正文模板有误：%w", err)
	}
	return t, nil
}

type bodyData struct {
	Push
	VarMap map[string]string
}

func (w *webhook) send(ctx context.Context, p Push) error {
	var body []byte
	if w.cfg.Body == "" {
		b, err := json.Marshal(p)
		if err != nil {
			return err
		}
		body = b
	} else {
		t, err := parseBody(w.cfg.Body)
		if err != nil {
			return err
		}
		data := bodyData{Push: p, VarMap: map[string]string{}}
		for _, v := range p.Vars {
			data.VarMap[v.Name] = v.Value
		}
		var buf bytes.Buffer
		if err := t.Execute(&buf, data); err != nil {
			// 不带原始错误：text/template 的执行错误可能带出变量值。
			return errors.New("渲染 Webhook 正文失败，检查模板里引用的字段")
		}
		body = buf.Bytes()
	}
	method := strings.ToUpper(w.cfg.Method)
	if method == "" {
		method = http.MethodPost
	}
	req, err := http.NewRequestWithContext(ctx, method, w.cfg.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Ferry")
	for k, v := range w.cfg.Headers {
		req.Header.Set(k, v)
	}
	if w.cfg.Secret != "" {
		m := hmac.New(sha256.New, []byte(w.cfg.Secret))
		m.Write(body)
		req.Header.Set(SignatureHeader, "sha256="+hex.EncodeToString(m.Sum(nil)))
	}
	resp, err := w.http.Do(req)
	if err != nil {
		// Webhook 地址里可能带着令牌，同样不带出完整地址。
		return fmt.Errorf("请求 Webhook 失败：%w", stripURL(err))
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Webhook 返回 HTTP %d", resp.StatusCode)
	}
	return nil
}

func (w *webhook) Upsert(ctx context.Context, p Push) error { return w.send(ctx, p) }

// Test 发一个不含任何值的测试推送。
func (w *webhook) Test(ctx context.Context) error {
	return w.send(ctx, Push{SourceKey: "ferry-test", SourceName: "Ferry 测试", Vars: []Var{{Name: "FERRY_TEST", Value: "test", Remarks: "ferry:test"}}})
}
