package server

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/lonelyman0108/ferry/internal/source"
)

func TestSetupCreatesEndpoint(t *testing.T) {
	h := newHarness(t, Options{})
	body := map[string]any{"database": h.sqliteDB(), "admin": admin("password123"), "endpoint": map[string]string{"name": "局域网", "base_url": "http://192.168.1.10:8080/"}}
	if code, _ := h.setup(body); code != 200 {
		t.Fatalf("初始化 = %d", code)
	}
	h.do("POST", "/api/v1/auth/login", admin("password123"), nil)
	var es []endpointView
	h.do("GET", "/api/v1/endpoints", nil, &es)
	if len(es) != 1 || es[0].BaseURL != "http://192.168.1.10:8080" {
		t.Errorf("访问地址 = %+v", es)
	}
}

func TestSubscriptionFlow(t *testing.T) {
	h := ready(t)
	var lan, public endpointView
	if code := h.do("POST", "/api/v1/endpoints", map[string]string{"name": "lan", "base_url": "ftp://x"}, nil); code != 400 {
		t.Errorf("非法地址 = %d", code)
	}
	h.do("POST", "/api/v1/endpoints", map[string]string{"name": "lan", "base_url": "http://192.168.1.10:8080"}, &lan)
	h.do("POST", "/api/v1/endpoints", map[string]string{"name": "public", "base_url": "https://ferry.example.com"}, &public)
	if code := h.do("POST", "/api/v1/endpoints", map[string]string{"name": "lan", "base_url": "http://x"}, nil); code != 409 {
		t.Errorf("重名地址 = %d", code)
	}

	h.do("POST", "/api/v1/examples/ninebot/import", map[string]any{}, nil)
	other := ninebotDoc(t)
	other.Key, other.Name = "other", "另一个"
	other.Matchers[0].Host, other.Matchers[1].Host = "api.other.com", "api.other.com"
	h.do("POST", "/api/v1/sources", other, nil)

	bad := []map[string]any{
		{"name": "x", "client": "clash", "endpoint_id": lan.ID, "all_sources": true},
		{"name": "x", "client": "surge", "endpoint_id": 999, "all_sources": true},
		{"name": "x", "client": "surge", "endpoint_id": lan.ID, "fallback_id": lan.ID, "all_sources": true},
		{"name": "x", "client": "surge", "endpoint_id": lan.ID},
		{"name": "x", "client": "surge", "endpoint_id": lan.ID, "source_keys": []string{"nope"}},
	}
	for i, b := range bad {
		if code := h.do("POST", "/api/v1/subscriptions", b, nil); code != 400 {
			t.Errorf("非法订阅 %d = %d，期望 400", i, code)
		}
	}

	var sv subscriptionView
	in := map[string]any{"name": "iPhone", "client": "surge", "endpoint_id": lan.ID, "fallback_id": public.ID, "source_keys": []string{"ninebot"}}
	if code := h.do("POST", "/api/v1/subscriptions", in, &sv); code != 201 {
		t.Fatalf("建订阅 = %d", code)
	}
	if !strings.HasPrefix(sv.Links.SubscriptionURL, "http://192.168.1.10:8080/sub/fs_") || !strings.HasSuffix(sv.Links.SubscriptionURL, "/ferry.sgmodule") ||
		!strings.HasPrefix(sv.Links.InstallURL, "surge:///install-module?url=") || sv.Links.ScriptURL != "http://192.168.1.10:8080/script/ferry.js" {
		t.Errorf("链接不对：%+v", sv.Links)
	}
	token := strings.Split(strings.TrimPrefix(sv.Links.SubscriptionURL, "http://192.168.1.10:8080/sub/"), "/")[0]
	var again subscriptionView
	h.do("GET", "/api/v1/subscriptions/"+itoa(sv.ID), nil, &again)
	if again.Links.SubscriptionURL != sv.Links.SubscriptionURL {
		t.Error("再次读取时链接应当不变（令牌可解密还原）")
	}
	if code := h.do("DELETE", "/api/v1/endpoints/"+itoa(public.ID), nil, nil); code != 409 {
		t.Errorf("删除在用的地址 = %d，期望 409", code)
	}

	phone := &harness{t: t, srv: h.srv, ts: h.ts, client: http.DefaultClient}
	var rules source.ScriptRules
	if code := phone.do("GET", "/api/v1/rules", nil, &rules, subscriptionHeader, token); code != 200 {
		t.Fatalf("拉规则 = %d", code)
	}
	if len(rules.Sources) != 1 || rules.Sources[0].Key != "ninebot" {
		t.Errorf("订阅只应看到 ninebot：%+v", rules)
	}
	req, _ := http.NewRequest("GET", h.ts.URL+"/api/v1/rules", nil)
	req.Header.Set(subscriptionHeader, token)
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	etag := resp.Header.Get("ETag")
	req.Header.Set("If-None-Match", etag)
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if etag == "" || resp.StatusCode != 304 {
		t.Errorf("带 ETag 再拉 = %d（ETag %q），期望 304", resp.StatusCode, etag)
	}
	if code := phone.do("GET", "/api/v1/rules", nil, nil); code != 401 {
		t.Errorf("没带令牌 = %d", code)
	}

	in["all_sources"] = true
	in["source_keys"] = nil
	h.do("PUT", "/api/v1/subscriptions/"+itoa(sv.ID), in, nil)
	phone.do("GET", "/api/v1/rules", nil, &rules, subscriptionHeader, token)
	if len(rules.Sources) != 2 {
		t.Errorf("改成全部来源后应看到 2 个：%d", len(rules.Sources))
	}

	var rotated subscriptionView
	h.do("POST", "/api/v1/subscriptions/"+itoa(sv.ID)+"/rotate", map[string]any{}, &rotated)
	if rotated.Links.SubscriptionURL == sv.Links.SubscriptionURL {
		t.Error("换令牌后链接应当变化")
	}
	if code := phone.do("GET", "/api/v1/rules", nil, nil, subscriptionHeader, token); code != 401 {
		t.Errorf("旧令牌 = %d，期望 401", code)
	}
	if code := h.do("DELETE", "/api/v1/subscriptions/"+itoa(sv.ID), nil, nil); code != 200 {
		t.Errorf("删除订阅 = %d", code)
	}
	if code := h.do("DELETE", "/api/v1/endpoints/"+itoa(public.ID), nil, nil); code != 200 {
		t.Errorf("删除不再使用的地址 = %d", code)
	}
}

func ninebotDoc(t *testing.T) source.Doc {
	t.Helper()
	for _, e := range source.Examples() {
		if e.Key == "ninebot" {
			return e.Doc
		}
	}
	t.Fatal("没有九号示例")
	return source.Doc{}
}

func TestSubscriptionFile(t *testing.T) {
	h := ready(t)
	var lan endpointView
	h.do("POST", "/api/v1/endpoints", map[string]string{"name": "lan", "base_url": h.ts.URL}, &lan)
	h.do("POST", "/api/v1/examples/ninebot/import", map[string]any{}, nil)
	var sv subscriptionView
	h.do("POST", "/api/v1/subscriptions", map[string]any{"name": "iPhone", "client": "stash", "endpoint_id": lan.ID, "all_sources": true}, &sv)

	get := func(u string) (int, string) {
		resp, err := http.Get(u)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	code, body := get(sv.Links.SubscriptionURL)
	if code != 200 || !strings.Contains(body, "cn-cbu-gateway.ninebot.com") || !strings.Contains(body, "require-body: true") {
		t.Fatalf("订阅文件 = %d：\n%s", code, body)
	}
	if code, _ := get(strings.Replace(sv.Links.SubscriptionURL, "ferry.stoverride", "ferry.sgmodule", 1)); code != 404 {
		t.Errorf("文件名和客户端不符 = %d", code)
	}
	if code, _ := get(h.ts.URL + "/sub/fs_wrong/ferry.stoverride"); code != 404 {
		t.Errorf("令牌错误 = %d", code)
	}
}

func TestSubscriptionStatus(t *testing.T) {
	h := ready(t)
	token := phoneSetup(t, h)
	phone := &harness{t: t, srv: h.srv, ts: h.ts, client: http.DefaultClient}
	get := func() subscriptionView {
		t.Helper()
		var list []subscriptionView
		h.do("GET", "/api/v1/subscriptions", nil, &list)
		if len(list) != 1 {
			t.Fatalf("订阅 = %+v", list)
		}
		return list[0]
	}
	outdated := func() int {
		t.Helper()
		var v overviewView
		h.do("GET", "/api/v1/overview", nil, &v)
		return v.Attention.Outdated
	}

	v := get()
	if v.Status != subNotInstalled || v.Outdated || len(v.Hostnames) != 1 || v.Hostnames[0] != "cn-cbu-gateway.ninebot.com" {
		t.Fatalf("刚建的订阅 = %+v", v)
	}
	var pv struct {
		FileName  string   `json:"file_name"`
		Content   string   `json:"content"`
		Hostnames []string `json:"hostnames"`
	}
	if code := h.do("GET", "/api/v1/subscriptions/"+itoa(v.ID)+"/preview", nil, &pv); code != 200 || pv.FileName == "" || !strings.Contains(pv.Content, "cn-cbu-gateway.ninebot.com") {
		t.Fatalf("预览 = %d %+v", code, pv)
	}
	if get().Status != subNotInstalled {
		t.Error("管理界面预览不应改安装状态")
	}

	fileURL := strings.TrimPrefix(v.Links.SubscriptionURL, h.ts.URL)
	if code := phone.do("GET", fileURL, nil, nil); code != 200 {
		t.Fatalf("下载订阅文件 = %d", code)
	}
	if v = get(); v.Status != subInstalled || v.FileFetchedAt == nil || v.LastSeenAt != nil {
		t.Errorf("下载订阅文件后 = %+v", v)
	}
	phone.do("GET", "/api/v1/rules", nil, nil, subscriptionHeader, token)
	if v = get(); v.Status != subConnected || v.LastSeenAt == nil {
		t.Errorf("拉过规则后 = %+v", v)
	}

	// 来源加了一个域名：手机上那份订阅过时了；客户端重新下载后恢复。
	var list []sourceSummary
	h.do("GET", "/api/v1/sources", nil, &list)
	var src sourceView
	h.do("GET", "/api/v1/sources/"+itoa(list[0].ID), nil, &src)
	src.Doc.Matchers = append(src.Doc.Matchers, source.MatcherDoc{Name: "h5", Host: "h5.ninebot.com"})
	if code := h.do("PUT", "/api/v1/sources/"+itoa(src.ID), src.Doc, nil); code != 200 {
		t.Fatalf("改来源 = %d", code)
	}
	if v = get(); !v.Outdated || len(v.Hostnames) != 2 || outdated() != 1 {
		t.Errorf("加域名后应待更新：%+v", v)
	}
	phone.do("GET", fileURL, nil, nil)
	if v = get(); v.Outdated || outdated() != 0 {
		t.Errorf("重新下载后不应再待更新：%+v", v)
	}
}
