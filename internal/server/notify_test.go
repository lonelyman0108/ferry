package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lonelyman0108/ferry/internal/notify"
)

func TestNotifications(t *testing.T) {
	var mu sync.Mutex
	var got []notify.Message
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m notify.Message
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &m)
		mu.Lock()
		got = append(got, m)
		mu.Unlock()
	}))
	defer hook.Close()
	wait := func(event string) notify.Message {
		t.Helper()
		for i := 0; i < 100; i++ {
			mu.Lock()
			for _, m := range got {
				if m.Event == event {
					mu.Unlock()
					return m
				}
			}
			mu.Unlock()
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("没收到 %s 通知，收到 %+v", event, got)
		return notify.Message{}
	}

	h := ready(t)
	token := phoneSetup(t, h)

	var ch channelView
	if code := h.do("POST", "/api/v1/channels", map[string]any{"name": "hook", "kind": "webhook", "config": map[string]string{"url": hook.URL, "secret": "s"}}, &ch); code != 201 {
		t.Fatalf("建渠道 = %d", code)
	}
	if ch.Config.Secret != "" || !ch.SecretsSet["secret"] {
		t.Errorf("密钥应只写不读：%+v", ch)
	}
	var res map[string]any
	h.do("POST", "/api/v1/channels/"+itoa(ch.ID)+"/test", map[string]any{}, &res)
	if res["ok"] != true || wait("test").Title == "" {
		t.Errorf("测试通知 = %v", res)
	}

	if code := h.do("POST", "/api/v1/notify-rules", map[string]any{"name": "x", "events": []string{"nope"}, "channel_ids": []uint{ch.ID}}, nil); code != 400 {
		t.Errorf("未知事件 = %d", code)
	}
	if code := h.do("POST", "/api/v1/notify-rules", map[string]any{"name": "x", "events": []string{"value.updated"}, "channel_ids": []uint{999}}, nil); code != 400 {
		t.Errorf("不存在的渠道 = %d", code)
	}
	rule := map[string]any{"name": "常用", "events": []string{"value.updated", "subscription.mitm_changed"}, "channel_ids": []uint{ch.ID}}
	if code := h.do("POST", "/api/v1/notify-rules", rule, nil); code != 201 {
		t.Fatalf("建规则 = %d", code)
	}

	phone := &harness{t: t, srv: h.srv, ts: h.ts, client: http.DefaultClient}
	phone.do("POST", "/api/v1/sync", map[string]any{"source": "ninebot", "fields": map[string]string{"device_id": "DEV-SECRET-001"}}, nil, subscriptionHeader, token)
	m := wait("value.updated")
	if !strings.Contains(m.Body, "九号出行") || !strings.Contains(m.Body, "device_id") || strings.Contains(m.Body, "DEV-SECRET-001") {
		t.Errorf("更新通知应有来源和字段名、不含值：%+v", m)
	}

	other := ninebotDoc(t)
	other.Key, other.Name = "other", "另一个"
	other.Matchers[0].Host, other.Matchers[1].Host = "api.other.com", "api.other.com"
	other.Probe = nil
	h.do("POST", "/api/v1/sources", other, nil)
	mc := wait("subscription.mitm_changed")
	if !strings.HasPrefix(mc.Link, "surge:///install-module?url=") || !strings.Contains(mc.Body, "iPhone") {
		t.Errorf("MITM 变化通知要带安装链接：%+v", mc)
	}
}

func TestDeleteChannelUpdatesRules(t *testing.T) {
	h := ready(t)
	var a, b channelView
	h.do("POST", "/api/v1/channels", map[string]any{"name": "a", "kind": "webhook", "config": map[string]string{"url": "http://127.0.0.1:1/a"}}, &a)
	h.do("POST", "/api/v1/channels", map[string]any{"name": "b", "kind": "webhook", "config": map[string]string{"url": "http://127.0.0.1:1/b"}}, &b)
	h.do("POST", "/api/v1/notify-rules", map[string]any{"name": "两个", "events": []string{"push.failed"}, "channel_ids": []uint{a.ID, b.ID}}, nil)
	h.do("POST", "/api/v1/notify-rules", map[string]any{"name": "一个", "events": []string{"push.failed"}, "channel_ids": []uint{b.ID}}, nil)
	if code := h.do("DELETE", "/api/v1/channels/"+itoa(b.ID), nil, nil); code != 200 {
		t.Fatalf("删渠道 = %d", code)
	}

	var rules []ruleView
	h.do("GET", "/api/v1/notify-rules", nil, &rules)
	got := map[string]ruleView{}
	for _, r := range rules {
		got[r.Name] = r
	}
	if r := got["两个"]; len(r.ChannelIDs) != 1 || r.ChannelIDs[0] != a.ID || !r.Enabled {
		t.Errorf("删掉的渠道应从规则里移除，其余不变：%+v", r)
	}
	if r := got["一个"]; len(r.ChannelIDs) != 0 || r.Enabled {
		t.Errorf("没有渠道的规则应停用：%+v", r)
	}
	r := got["两个"]
	body := map[string]any{"name": r.Name, "events": r.Events, "source_keys": r.SourceKeys, "channel_ids": r.ChannelIDs, "enabled": false}
	if code := h.do("PUT", "/api/v1/notify-rules/"+itoa(r.ID), body, nil); code != 200 {
		t.Errorf("移除渠道后的规则应能照常保存，得到 %d", code)
	}
}
