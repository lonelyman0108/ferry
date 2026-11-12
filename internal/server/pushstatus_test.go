package server

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lonelyman0108/ferry/internal/store"
)

func TestPushVisibility(t *testing.T) {
	var ok atomic.Bool
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ok.Load() {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer hook.Close()

	h := ready(t)
	token := phoneSetup(t, h)
	phone := &harness{t: t, srv: h.srv, ts: h.ts, client: http.DefaultClient}
	db := h.srv.store().DB

	var d destinationView
	h.do("POST", "/api/v1/destinations", map[string]any{"name": "hook", "kind": "webhook", "config": map[string]any{"url": hook.URL}}, &d)
	var srcs []sourceSummary
	h.do("GET", "/api/v1/sources", nil, &srcs)
	var b bindingView
	h.do("POST", "/api/v1/bindings", map[string]any{"source_id": srcs[0].ID, "destination_id": d.ID, "mode": store.ModePerIdentity}, &b)
	phone.do("POST", "/api/v1/sync", map[string]any{"source": "ninebot", "fields": map[string]string{"authorization": testJWT(time.Now().Add(48 * time.Hour)), "device_id": "DEV-001"}}, nil, subscriptionHeader, token)

	identity := func() identityView {
		t.Helper()
		var list []identityView
		h.do("GET", "/api/v1/identities", nil, &list)
		if len(list) != 1 {
			t.Fatalf("身份 = %+v", list)
		}
		return list[0]
	}
	pushesOf := func(id uint) []identityPushView {
		t.Helper()
		var out []identityPushView
		h.do("GET", "/api/v1/identities/"+itoa(id)+"/pushes", nil, &out)
		return out
	}
	destOf := func() destinationView {
		t.Helper()
		var list []destinationView
		h.do("GET", "/api/v1/destinations", nil, &list)
		return list[0]
	}

	// 值生效后排了一个任务，后台任务还没执行：排队中。
	a := identity()
	if a.PushStatus != store.PushQueued {
		t.Fatalf("排队后的推送状态 = %q", a.PushStatus)
	}
	ps := pushesOf(a.ID)
	if len(ps) != 1 || ps[0].DestinationName != "hook" || !ps[0].Enabled || ps[0].Last == nil || ps[0].Last.Status != store.PushQueued {
		t.Fatalf("身份的推送 = %+v", ps)
	}
	jobID := ps[0].Last.ID

	// 立即重试：目的地返回 500，变成等待重试。
	var job pushView
	if code := h.do("POST", "/api/v1/pushes/"+itoa(jobID)+"/retry", map[string]any{}, &job); code != 200 || job.Status != store.PushQueued || job.LastError == "" || job.Attempts != 1 {
		t.Fatalf("立即重试 = %d %+v", code, job)
	}
	if a = identity(); a.PushStatus != pushRetrying {
		t.Errorf("失败一次后的推送状态 = %q", a.PushStatus)
	}
	if v := destOf(); v.Problems != 1 || v.LastPush == nil || v.LastPush.ID != jobID {
		t.Errorf("目的地 = %+v", v)
	}
	for q, want := range map[string]int{"status=retrying": 1, "status=success": 0, "destination=" + itoa(d.ID): 1, "source=ninebot": 1} {
		var list []pushView
		if code := h.do("GET", "/api/v1/pushes?"+q, nil, &list); code != 200 || len(list) != want {
			t.Errorf("推送记录 %s = %d，%d 条，期望 %d 条", q, code, len(list), want)
		}
	}
	if code := h.do("GET", "/api/v1/pushes?status=bogus", nil, nil); code != 400 {
		t.Errorf("不认识的状态 = %d", code)
	}
	if code := h.do("GET", "/api/v1/pushes?source=nope", nil, nil); code != 404 {
		t.Errorf("不存在的来源 = %d", code)
	}

	// 重试用完算失败；目的地恢复后再重试，会重新排一个任务并推送成功。
	db.Model(&store.PushJob{}).Where("id = ?", jobID).Update("status", store.PushFailed)
	if a = identity(); a.PushStatus != store.PushFailed {
		t.Errorf("最终失败后的推送状态 = %q", a.PushStatus)
	}
	ok.Store(true)
	if code := h.do("POST", "/api/v1/pushes/"+itoa(jobID)+"/retry", map[string]any{}, &job); code != 200 || job.Status != store.PushSuccess || job.ID == jobID {
		t.Fatalf("失败后重试 = %d %+v", code, job)
	}
	if a = identity(); a.PushStatus != store.PushSuccess || a.PushedAt == nil {
		t.Errorf("推送成功后 = %q %v", a.PushStatus, a.PushedAt)
	}
	if v := destOf(); v.Problems != 0 || v.LastPush == nil || v.LastPush.Status != store.PushSuccess {
		t.Errorf("推送成功后的目的地 = %+v", v)
	}
	if code := h.do("POST", "/api/v1/pushes/"+itoa(job.ID)+"/retry", map[string]any{}, nil); code != 409 {
		t.Errorf("重试已成功的任务 = %d，期望 409", code)
	}
}
