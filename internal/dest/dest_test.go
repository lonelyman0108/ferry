package dest

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeQinglong 按 2026-10-23 实测的青龙 2.23 OpenAPI 行为模拟：
// 搜索会匹配名字、值和备注；新建收数组返回数组；修改收单个对象；启用收 ID 数组；status 0 为启用。
type fakeQinglong struct {
	mu       sync.Mutex
	envs     []qlEnv
	nextID   int
	tokens   int
	calls    []string
	rejectTk string
}

func (f *fakeQinglong) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.calls = append(f.calls, r.Method+" "+r.URL.Path)
		reply := func(data any) { _ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": data}) }
		if r.URL.Path == "/open/auth/token" {
			if r.URL.Query().Get("client_secret") != "secret" {
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 400, "message": "client_secret 错误"})
				return
			}
			f.tokens++
			reply(map[string]any{"token": "tk" + string(rune('0'+f.tokens)), "expiration": time.Now().Add(30 * 24 * time.Hour).Unix()})
			return
		}
		if auth := r.Header.Get("Authorization"); !strings.HasPrefix(auth, "Bearer tk") || auth == "Bearer "+f.rejectTk {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		switch r.Method + " " + r.URL.Path {
		case "GET /open/envs":
			q := r.URL.Query().Get("searchValue")
			out := []qlEnv{}
			for _, e := range f.envs {
				if strings.Contains(e.Name, q) || strings.Contains(e.Value, q) || strings.Contains(e.Remarks, q) {
					out = append(out, e)
				}
			}
			reply(out)
		case "POST /open/envs":
			var in []qlEnv
			_ = json.Unmarshal(body, &in)
			for i := range in {
				f.nextID++
				in[i].ID = f.nextID
				f.envs = append(f.envs, in[i])
			}
			reply(in)
		case "PUT /open/envs":
			var in qlEnv
			_ = json.Unmarshal(body, &in)
			for i := range f.envs {
				if f.envs[i].ID == in.ID {
					f.envs[i].Name, f.envs[i].Value, f.envs[i].Remarks = in.Name, in.Value, in.Remarks
				}
			}
			reply(in)
		case "PUT /open/envs/enable":
			var ids []int
			_ = json.Unmarshal(body, &ids)
			for i := range f.envs {
				for _, id := range ids {
					if f.envs[i].ID == id {
						f.envs[i].Status = 0
					}
				}
			}
			reply(nil)
		default:
			t.Errorf("意外的请求 %s %s", r.Method, r.URL.Path)
		}
	})
}

func newQinglong(t *testing.T) (*fakeQinglong, Adapter) {
	f := &fakeQinglong{}
	ts := httptest.NewServer(f.handler(t))
	t.Cleanup(ts.Close)
	a, err := New(KindQinglong, Config{BaseURL: ts.URL + "/", ClientID: t.Name(), ClientSecret: "secret"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return f, a
}

func push(vars ...Var) Push { return Push{SourceKey: "ninebot", Vars: vars} }

func TestQinglongUpsert(t *testing.T) {
	f, a := newQinglong(t)
	ctx := context.Background()
	// 使用者手动维护的同名变量，备注不是 Ferry 的，不能碰。
	f.envs = []qlEnv{{ID: 100, Name: "TEMP_NINEBOT_DEVICE_ID", Value: "manual", Remarks: "手动填的"}}
	f.nextID = 100

	v := Var{Name: "TEMP_NINEBOT_DEVICE_ID", Value: "DEV-1", Remarks: "ferry:ninebot:default"}
	if err := a.Upsert(ctx, push(v)); err != nil {
		t.Fatal(err)
	}
	if len(f.envs) != 2 || f.envs[0].Value != "manual" || f.envs[1].Value != "DEV-1" || f.envs[1].Remarks != v.Remarks {
		t.Fatalf("应新建一条 Ferry 的变量、不动手动那条：%+v", f.envs)
	}

	v.Value = "DEV-2"
	f.envs[1].Status = 1
	if err := a.Upsert(ctx, push(v)); err != nil {
		t.Fatal(err)
	}
	if len(f.envs) != 2 || f.envs[1].Value != "DEV-2" || f.envs[1].Status != 0 {
		t.Errorf("应更新原来那条并重新启用：%+v", f.envs)
	}

	before := len(f.calls)
	if err := a.Upsert(ctx, push(v)); err != nil {
		t.Fatal(err)
	}
	if got := f.calls[before:]; len(got) != 1 || got[0] != "GET /open/envs" {
		t.Errorf("值没变且已启用时只应查询一次：%v", got)
	}
	if f.tokens != 1 {
		t.Errorf("token 应缓存复用，换了 %d 次", f.tokens)
	}
}

func TestQinglongTokenRefreshAndErrors(t *testing.T) {
	f, a := newQinglong(t)
	ctx := context.Background()
	if err := a.Test(ctx); err != nil {
		t.Fatal(err)
	}
	f.rejectTk = "tk1"
	if err := a.Test(ctx); err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("token 失效时应报 401：%v", err)
	}
	if err := a.Test(ctx); err != nil {
		t.Errorf("401 之后应重新换 token：%v", err)
	}
	if f.tokens != 2 {
		t.Errorf("换 token 次数 = %d，期望 2", f.tokens)
	}

	bad, _ := New(KindQinglong, Config{BaseURL: "http://127.0.0.1:1", ClientID: "x", ClientSecret: "very-secret-value"}, nil)
	if err := bad.Test(ctx); err == nil || !strings.Contains(err.Error(), "连接青龙失败") || strings.Contains(err.Error(), "very-secret-value") {
		t.Errorf("连不上时应报错且不带出 client_secret：%v", err)
	}
	hook, _ := New(KindWebhook, Config{URL: "http://127.0.0.1:1/hook?token=hook-secret"}, nil)
	if err := hook.Test(ctx); err == nil || strings.Contains(err.Error(), "hook-secret") {
		t.Errorf("Webhook 连不上时不应带出地址里的令牌：%v", err)
	}
	for _, c := range []Config{{BaseURL: "x", ClientID: "a", ClientSecret: "b"}, {BaseURL: "http://h"}} {
		if Validate(KindQinglong, c) == nil {
			t.Errorf("配置 %+v 应当不合法", c)
		}
	}
}

func TestWebhook(t *testing.T) {
	var got struct {
		body []byte
		sig  string
		hdr  string
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.body, _ = io.ReadAll(r.Body)
		got.sig, got.hdr = r.Header.Get(SignatureHeader), r.Header.Get("X-Extra")
		if strings.Contains(string(got.body), "fail") {
			w.WriteHeader(500)
		}
	}))
	defer ts.Close()

	a, err := New(KindWebhook, Config{URL: ts.URL, Secret: "s3", Headers: map[string]string{"X-Extra": "1"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Upsert(context.Background(), push(Var{Name: "A", Value: "1", Remarks: "ferry:x"})); err != nil {
		t.Fatal(err)
	}
	m := hmac.New(sha256.New, []byte("s3"))
	m.Write(got.body)
	if got.sig != "sha256="+hex.EncodeToString(m.Sum(nil)) || got.hdr != "1" || !strings.Contains(string(got.body), `"vars":[{"name":"A"`) {
		t.Errorf("签名或正文不对：%s %s", got.sig, got.body)
	}

	tmpl, _ := New(KindWebhook, Config{URL: ts.URL, Body: `{"cookie":"{{index .VarMap "A"}}","from":"{{.SourceKey}}"}`}, nil)
	_ = tmpl.Upsert(context.Background(), push(Var{Name: "A", Value: "v"}))
	if string(got.body) != `{"cookie":"v","from":"ninebot"}` {
		t.Errorf("模板正文 = %s", got.body)
	}
	if err := a.Upsert(context.Background(), push(Var{Name: "fail"})); err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("对方返回 500 时应报错：%v", err)
	}
	if Validate(KindWebhook, Config{URL: ts.URL, Method: "GET"}) == nil || Validate(KindWebhook, Config{URL: ts.URL, Body: "{{"}) == nil {
		t.Error("GET 方法和坏模板应当不合法")
	}
}

func TestRedactAndMerge(t *testing.T) {
	c := Config{BaseURL: "http://h", ClientID: "id", ClientSecret: "sec"}
	r, set := c.Redacted()
	if r.ClientSecret != "" || !set["client_secret"] || set["secret"] {
		t.Errorf("Redacted = %+v %v", r, set)
	}
	if m := (Config{BaseURL: "http://h2", ClientID: "id"}).MergeSecrets(c); m.ClientSecret != "sec" || m.BaseURL != "http://h2" {
		t.Errorf("留空的密钥应沿用旧值：%+v", m)
	}
}
