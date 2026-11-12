package server

import (
	"net/http"
	"testing"
	"time"

	"github.com/lonelyman0108/ferry/internal/source"
	"github.com/lonelyman0108/ferry/internal/store"
)

func TestOverview(t *testing.T) {
	h := ready(t)
	token := phoneSetup(t, h)
	phone := &harness{t: t, srv: h.srv, ts: h.ts, client: http.DefaultClient}
	db := h.srv.store().DB
	get := func() overviewView {
		t.Helper()
		var v overviewView
		if code := h.do("GET", "/api/v1/overview", nil, &v); code != 200 {
			t.Fatalf("总览 = %d", code)
		}
		return v
	}
	sync := func(src string, fields map[string]string) {
		t.Helper()
		if code := phone.do("POST", "/api/v1/sync", map[string]any{"source": src, "fields": fields}, nil, subscriptionHeader, token); code != 200 {
			t.Fatalf("回传 %s = %d", src, code)
		}
	}

	if v := get(); v.Attention.NeverConnected != 1 || v.Counts.Subscriptions != 1 || v.Counts.SubscriptionsConnected != 0 {
		t.Errorf("刚建的订阅应算从未连通：%+v %+v", v.Attention, v.Counts)
	}
	phone.do("GET", "/api/v1/rules", nil, nil, subscriptionHeader, token)

	// 九号：令牌两天后过期，算即将过期。
	sync("ninebot", map[string]string{"authorization": testJWT(time.Now().Add(48 * time.Hour)), "device_id": "DEV-001"})

	// 另一个来源人工审核、合并窗口用默认的 60 秒：回传后先在收集中，处理后待审核。
	other := ninebotDoc(t)
	other.Key, other.Name, other.Review, other.Probe = "other", "另一个", source.ReviewManual, nil
	other.Matchers[0].Host, other.Matchers[1].Host = "api.other.com", "api.other.com"
	var otherSrc sourceView
	if code := h.do("POST", "/api/v1/sources", other, &otherSrc); code != 201 {
		t.Fatalf("建来源 = %d", code)
	}
	sync("other", map[string]string{"device_id": "DEV-OTHER"})
	if v := get(); v.Attention.Collecting != 1 || v.Attention.PendingReview != 0 {
		t.Errorf("处理前应只有收集中：%+v", v.Attention)
	}
	// 太短的 device_id 被驳回；整条回传都被驳回时批次当场关掉，不算进收集中。
	sync("ninebot", map[string]string{"device_id": "x"})
	var batches []batchView
	h.do("GET", "/api/v1/batches", nil, &batches)
	if len(batches) != 1 || batches[0].SourceKey != "other" {
		t.Fatalf("收集中 = %+v，期望只剩另一个来源的批次", batches)
	}
	h.do("POST", "/api/v1/batches/"+itoa(batches[0].ID)+"/process", map[string]any{}, nil)

	v := get()
	if a := v.Attention; a.Collecting != 0 || a.PendingReview != 1 || a.Expiring != 1 || a.Expired != 0 || a.NeverConnected != 0 {
		t.Errorf("需要处理 = %+v", a)
	}
	if c := v.Counts; c.Sources != 2 || c.SourcesEnabled != 2 || c.Identities != 2 || c.SubscriptionsConnected != 1 {
		t.Errorf("计数 = %+v", c)
	}
	nb := v.Sources[0]
	if nb.Key != "ninebot" || nb.Identities != 1 || nb.LastSeenAt == nil || nb.Rejected != 1 || nb.Expiring != 1 || nb.PendingReview != 0 {
		t.Errorf("九号 = %+v", nb)
	}
	if v.Sources[1].PendingReview != 1 {
		t.Errorf("另一个来源 = %+v", v.Sources[1])
	}

	var idents []identityView
	h.do("GET", "/api/v1/identities", nil, &idents)
	for _, a := range idents {
		switch a.SourceKey {
		case "ninebot":
			if a.ExpiresAt == nil || a.Pending != 0 {
				t.Errorf("九号的身份应带最早到期时间：%+v", a)
			}
		case "other":
			if a.Pending != 1 || a.ExpiresAt != nil {
				t.Errorf("另一个来源的身份应有 1 个待审核：%+v", a)
			}
		}
	}

	// 推送：只看每个“推送规则 + 身份”最近一次任务。
	var d destinationView
	h.do("POST", "/api/v1/destinations", map[string]any{"name": "坏的", "kind": "webhook", "config": map[string]any{"url": "http://127.0.0.1:1/push"}}, &d)
	var b bindingView
	h.do("POST", "/api/v1/bindings", map[string]any{"source_id": nb.ID, "destination_id": d.ID, "mode": store.ModeJoined}, &b)
	h.do("POST", "/api/v1/pushes", map[string]any{"binding_id": b.ID}, nil)
	if a := get().Attention; a.PushRetrying != 1 || a.PushFailed != 0 {
		t.Errorf("推送失败一次后应在等待重试：%+v", a)
	}
	db.Model(&store.PushJob{}).Where("binding_id = ?", b.ID).Update("status", store.PushFailed)
	if a := get().Attention; a.PushRetrying != 0 || a.PushFailed != 1 {
		t.Errorf("重试用完应算失败：%+v", a)
	}
	db.Create(&store.PushJob{BindingID: b.ID, Status: store.PushSuccess, NextAttemptAt: time.Now(), Trigger: "manual", CreatedAt: time.Now()})
	if a := get().Attention; a.PushFailed != 0 {
		t.Errorf("之后推送成功了，就不算异常：%+v", a)
	}
	db.Create(&store.PushJob{BindingID: b.ID, Status: store.PushFailed, NextAttemptAt: time.Now(), Trigger: "manual", CreatedAt: time.Now()})
	h.do("PUT", "/api/v1/bindings/"+itoa(b.ID), map[string]any{"source_id": b.SourceID, "destination_id": b.DestinationID, "mode": b.Mode, "rename": b.Rename, "enabled": false}, nil)
	if a := get().Attention; a.PushFailed != 0 {
		t.Errorf("停用的推送规则不算异常：%+v", a)
	}

	// 停用的来源不算进需要处理，来源自己的计数照常给出。
	other.Enabled = new(bool)
	if code := h.do("PUT", "/api/v1/sources/"+itoa(otherSrc.ID), other, nil); code != 200 {
		t.Fatalf("停用来源 = %d", code)
	}
	v = get()
	if v.Attention.PendingReview != 0 || v.Sources[1].PendingReview != 1 || v.Counts.SourcesEnabled != 1 {
		t.Errorf("停用来源后 = %+v %+v", v.Attention, v.Sources[1])
	}

	var created createdToken
	h.do("POST", "/api/v1/tokens", map[string]string{"name": "monitor"}, &created)
	bearer := &harness{t: t, srv: h.srv, ts: h.ts, client: http.DefaultClient}
	if code := bearer.do("GET", "/api/v1/overview", nil, nil, "Authorization", "Bearer "+created.Token); code != 200 {
		t.Errorf("API 令牌读总览 = %d", code)
	}
}
