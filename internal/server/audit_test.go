package server

import (
	"net/http"
	"testing"
	"time"

	"github.com/lonelyman0108/ferry/internal/store"
)

func TestAuditView(t *testing.T) {
	h := ready(t)
	token := phoneSetup(t, h)
	phone := &harness{t: t, srv: h.srv, ts: h.ts, client: http.DefaultClient}
	phone.do("POST", "/api/v1/sync", map[string]any{"source": "ninebot", "fields": map[string]string{"device_id": "DEV-001"}}, nil, subscriptionHeader, token)
	var idents []identityView
	h.do("GET", "/api/v1/identities", nil, &idents)
	var vs []versionView
	h.do("GET", "/api/v1/identities/"+itoa(idents[0].ID)+"/versions", nil, &vs)
	h.do("POST", "/api/v1/versions/"+itoa(vs[0].ID)+"/reveal", map[string]any{}, nil)
	// 发送通知的结果由后台写入，这里直接造；再造一条订阅和来源都已删除的部分驳回回传。
	db := h.srv.store().DB
	now := time.Now()
	for _, l := range []store.AuditLog{
		{Actor: "system", Action: "notify.value.rejected", Target: "channel:Bark", Detail: "ok", At: now},
		{Actor: "system", Action: "notify.push.failed", Target: "channel:Bark", Detail: "连接超时", At: now},
		{Actor: "subscription:999", Action: "sync.accepted", Target: "source:gone", Detail: "a,b；未通过校验：b", At: now},
	} {
		if err := db.Create(&l).Error; err != nil {
			t.Fatal(err)
		}
	}

	var all []auditView
	h.do("GET", "/api/v1/audit?limit=200", nil, &all)
	// 同一个动作取最早的一条，造的那条部分驳回的回传按操作者另取。
	by := map[string]auditView{}
	var partial auditView
	for _, a := range all {
		if a.Actor == "subscription:999" {
			partial = a
			continue
		}
		by[a.Action] = a
	}
	for _, c := range []struct{ action, label, category, outcome, actorKind, actorName, target string }{
		{"auth.login", "登录", auditLogin, outcomeSuccess, "user", "admin", ""},
		{"source.import_example", "导入示例来源", auditConfig, "", "user", "admin", "九号出行"},
		{"sync.accepted", "回传新值", auditSync, outcomeSuccess, "subscription", "iPhone", "九号出行"},
		{"version.reveal", "查看原值", auditReveal, "", "user", "admin", "九号出行 · 设备 ID"},
		// 发送成功就是成功，不看事件名里的 rejected。
		{"notify.value.rejected", "发送通知：新值被驳回", auditNotify, outcomeSuccess, "system", "系统", "Bark"},
		{"notify.push.failed", "发送通知：推送失败", auditNotify, outcomeFailure, "system", "系统", "Bark"},
	} {
		a, ok := by[c.action]
		if !ok {
			t.Errorf("没有 %s", c.action)
			continue
		}
		if a.ActionLabel != c.label || a.Category != c.category || a.Outcome != c.outcome ||
			a.ActorKind != c.actorKind || a.ActorName != c.actorName || a.TargetName != c.target {
			t.Errorf("%s = %+v", c.action, a)
		}
	}
	// 订阅和来源都已删除：显示编号和来源标识；部分字段没通过算有问题。
	if partial.ActorName != "#999" || partial.TargetName != "gone" || partial.Outcome != outcomeWarning {
		t.Errorf("部分驳回的回传 = %+v", partial)
	}

	for cat, want := range map[string]int{auditReveal: 1, auditNotify: 2} {
		var got []auditView
		h.do("GET", "/api/v1/audit?category="+cat, nil, &got)
		if len(got) != want {
			t.Errorf("类别 %s 有 %d 条，期望 %d：%+v", cat, len(got), want, got)
		}
		for _, a := range got {
			if a.Category != cat {
				t.Errorf("类别 %s 里混进了 %s", cat, a.Action)
			}
		}
	}
	if code := h.do("GET", "/api/v1/audit?category=nope", nil, nil); code != 400 {
		t.Errorf("不存在的类别 = %d，期望 400", code)
	}
}
