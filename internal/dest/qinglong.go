package dest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// qinglong 通过青龙 OpenAPI 写环境变量。应用只需要“环境变量”权限。
type qinglong struct {
	cfg  Config
	http *http.Client
}

func validateQinglong(c Config) error {
	u, err := url.Parse(c.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("青龙地址须以 http:// 或 https:// 开头")
	}
	if c.ClientID == "" || c.ClientSecret == "" {
		return errors.New("缺少青龙应用的 client_id 或 client_secret")
	}
	return nil
}

// 青龙的 token 有效期约 30 天，按“地址 + client_id”缓存，提前一天换新。
var (
	tokenMu    sync.Mutex
	tokenCache = map[string]cachedToken{}
)

type cachedToken struct {
	token   string
	expires time.Time
}

func (q *qinglong) base() string { return strings.TrimRight(q.cfg.BaseURL, "/") }

func (q *qinglong) cacheKey() string { return q.base() + "|" + q.cfg.ClientID }

type qlResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (q *qinglong) token(ctx context.Context) (string, error) {
	tokenMu.Lock()
	c, ok := tokenCache[q.cacheKey()]
	tokenMu.Unlock()
	if ok && time.Until(c.expires) > 24*time.Hour {
		return c.token, nil
	}
	v := url.Values{"client_id": {q.cfg.ClientID}, "client_secret": {q.cfg.ClientSecret}}
	var data struct {
		Token      string `json:"token"`
		Expiration int64  `json:"expiration"`
	}
	if err := q.call(ctx, http.MethodGet, "/open/auth/token?"+v.Encode(), "", nil, &data); err != nil {
		return "", fmt.Errorf("获取青龙 token 失败：%w", err)
	}
	if data.Token == "" {
		return "", errors.New("青龙未返回 token")
	}
	tokenMu.Lock()
	tokenCache[q.cacheKey()] = cachedToken{token: data.Token, expires: time.Unix(data.Expiration, 0)}
	tokenMu.Unlock()
	return data.Token, nil
}

func (q *qinglong) forgetToken() {
	tokenMu.Lock()
	delete(tokenCache, q.cacheKey())
	tokenMu.Unlock()
}

// call 调青龙接口，code 不是 200 时返回错误。错误信息里不带请求内容，因为里面可能有变量值。
func (q *qinglong) call(ctx context.Context, method, path, token string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, q.base()+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := q.http.Do(req)
	if err != nil {
		return fmt.Errorf("连接青龙失败：%w", stripURL(err))
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode == http.StatusUnauthorized {
		q.forgetToken()
		return errors.New("青龙返回 401：应用凭据无效或缺少环境变量权限")
	}
	var r qlResponse
	if err := json.Unmarshal(raw, &r); err != nil {
		return fmt.Errorf("青龙返回 HTTP %d，响应不是 JSON", resp.StatusCode)
	}
	if r.Code != 200 {
		return fmt.Errorf("青龙返回 code %d：%s", r.Code, r.Message)
	}
	if out != nil && len(r.Data) > 0 {
		return json.Unmarshal(r.Data, out)
	}
	return nil
}

type qlEnv struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Value   string `json:"value"`
	Remarks string `json:"remarks"`
	Status  int    `json:"status"`
}

func (q *qinglong) Test(ctx context.Context) error {
	tok, err := q.token(ctx)
	if err != nil {
		return err
	}
	var envs []qlEnv
	return q.call(ctx, http.MethodGet, "/open/envs?searchValue=ferry%3A", tok, nil, &envs)
}

// Upsert 按“备注 + 变量名”找到 Ferry 管理的那条变量来更新，找不到就新建；写完确保启用。
// 不碰备注不是 Ferry 写的变量，手动维护的变量要由使用者在界面上明确接管。
func (q *qinglong) Upsert(ctx context.Context, p Push) error {
	tok, err := q.token(ctx)
	if err != nil {
		return err
	}
	for _, v := range p.Vars {
		var found []qlEnv
		if err := q.call(ctx, http.MethodGet, "/open/envs?searchValue="+url.QueryEscape(v.Remarks), tok, nil, &found); err != nil {
			return fmt.Errorf("查询变量 %s 失败：%w", v.Name, err)
		}
		var existing *qlEnv
		for i := range found {
			if found[i].Name == v.Name && found[i].Remarks == v.Remarks {
				existing = &found[i]
				break
			}
		}
		id := 0
		if existing == nil {
			var created []qlEnv
			if err := q.call(ctx, http.MethodPost, "/open/envs", tok, []map[string]string{{"name": v.Name, "value": v.Value, "remarks": v.Remarks}}, &created); err != nil {
				return fmt.Errorf("新建变量 %s 失败：%w", v.Name, err)
			}
			if len(created) == 0 {
				return fmt.Errorf("新建变量 %s 失败：青龙未返回记录", v.Name)
			}
			id = created[0].ID
		} else {
			id = existing.ID
			if existing.Value != v.Value {
				if err := q.call(ctx, http.MethodPut, "/open/envs", tok, map[string]any{"id": id, "name": v.Name, "value": v.Value, "remarks": v.Remarks}, nil); err != nil {
					return fmt.Errorf("更新变量 %s 失败：%w", v.Name, err)
				}
			}
			if existing.Status == 0 && existing.Value == v.Value {
				continue
			}
		}
		if err := q.call(ctx, http.MethodPut, "/open/envs/enable", tok, []int{id}, nil); err != nil {
			return fmt.Errorf("启用变量 %s 失败：%w", v.Name, err)
		}
	}
	return nil
}

// stripURL 去掉 *url.Error 里的完整地址：换 token 时 client_secret 在 query 里，
// 原样带出会让密钥进到错误信息、推送记录和界面上。
func stripURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}
