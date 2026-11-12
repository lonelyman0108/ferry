package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/lonelyman0108/ferry/internal/source"
	"github.com/lonelyman0108/ferry/internal/store"
)

// TestFlowAndLane 覆盖总览的待处理条目、流向图、来源链路和记录时间线：条目数要和计数一致，连线状态取最近一次任务。
func TestFlowAndLane(t *testing.T) {
	hook := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer hook.Close()

	h := ready(t)
	token := phoneSetup(t, h)
	phone := &harness{t: t, srv: h.srv, ts: h.ts, client: http.DefaultClient}
	db := h.srv.store().DB
	sync := func(src string, fields map[string]string) {
		t.Helper()
		if code := phone.do("POST", "/api/v1/sync", map[string]any{"source": src, "fields": fields}, nil, subscriptionHeader, token); code != 200 {
			t.Fatalf("回传 %s = %d", src, code)
		}
	}
	phone.do("GET", "/api/v1/rules", nil, nil, subscriptionHeader, token)

	// 九号：令牌两天后过期；另一个来源人工审核，回传后待审核。
	sync("ninebot", map[string]string{"authorization": testJWT(time.Now().Add(48 * time.Hour)), "device_id": "DEV-001"})
	other := ninebotDoc(t)
	zero := 0
	other.Key, other.Name, other.Review, other.Probe, other.MergeWindow = "other", "另一个", source.ReviewManual, nil, &zero
	other.Matchers[0].Host, other.Matchers[1].Host = "api.other.com", "api.other.com"
	var otherSrc sourceView
	if code := h.do("POST", "/api/v1/sources", other, &otherSrc); code != 201 {
		t.Fatalf("建来源 = %d", code)
	}
	sync("other", map[string]string{"authorization": testJWT(time.Now().Add(30 * 24 * time.Hour)), "device_id": "DEV-OTHER"})

	var srcs []sourceSummary
	h.do("GET", "/api/v1/sources", nil, &srcs)
	nbID := srcs[0].ID

	// 两个目的地：一个能推，一个必失败；失败的那条推送规则把任务标成最终失败。
	var good, bad destinationView
	h.do("POST", "/api/v1/destinations", map[string]any{"name": "好的", "kind": "webhook", "config": map[string]any{"url": hook.URL}}, &good)
	h.do("POST", "/api/v1/destinations", map[string]any{"name": "坏的", "kind": "webhook", "config": map[string]any{"url": "http://127.0.0.1:1/push"}}, &bad)
	var bGood, bBad bindingView
	h.do("POST", "/api/v1/bindings", map[string]any{"source_id": nbID, "destination_id": good.ID, "mode": store.ModeJoined}, &bGood)
	h.do("POST", "/api/v1/bindings", map[string]any{"source_id": nbID, "destination_id": bad.ID, "mode": store.ModeJoined}, &bBad)
	h.do("POST", "/api/v1/pushes", map[string]any{"binding_id": bGood.ID}, nil)
	h.do("POST", "/api/v1/pushes", map[string]any{"binding_id": bBad.ID}, nil)
	db.Model(&store.PushJob{}).Where("binding_id = ?", bBad.ID).Update("status", store.PushFailed)

	// 待处理条目：数量和计数一致，带着处理用的 id。
	var ov overviewView
	if code := h.do("GET", "/api/v1/overview", nil, &ov); code != 200 {
		t.Fatalf("总览 = %d", code)
	}
	kinds := map[string][]attentionItem{}
	for _, it := range ov.Items {
		kinds[it.Kind] = append(kinds[it.Kind], it)
	}
	a := ov.Attention
	for kind, want := range map[string]int{itemPendingReview: a.PendingReview, itemPushFailed: a.PushFailed, itemPushRetrying: a.PushRetrying,
		itemExpiring: a.Expiring, itemExpired: a.Expired, itemOutdated: a.Outdated, itemNeverConnected: a.NeverConnected} {
		if len(kinds[kind]) != want {
			t.Errorf("%s 条目 %d 条，计数是 %d", kind, len(kinds[kind]), want)
		}
	}
	if a.PendingReview != 2 || a.PushFailed != 1 || a.Expiring != 1 {
		t.Fatalf("计数 = %+v", a)
	}
	if ov.Items[0].Kind != itemPushFailed || ov.Items[0].Severity != severityAction {
		t.Errorf("推送失败应排在最前：%+v", ov.Items[0])
	}
	if it := kinds[itemPushFailed][0]; it.PushID == 0 || it.DestinationName != "坏的" || it.SourceID != nbID || it.Error == "" {
		t.Errorf("推送失败条目 = %+v", it)
	}
	for _, it := range kinds[itemPendingReview] {
		if it.VersionID == 0 || it.SourceID != otherSrc.ID || it.IdentityKey != "default" || it.Value == "" {
			t.Errorf("待审核条目 = %+v", it)
		}
		if it.Field == "authorization" && (!it.Masked || strings.Count(it.Value, ".") > 0) {
			t.Errorf("敏感字段的新值应打码：%+v", it)
		}
	}
	if it := kinds[itemExpiring][0]; it.Severity != severityNotice || it.At == nil || it.Field != "authorization" {
		t.Errorf("即将过期条目 = %+v", it)
	}
	if ov.Sources[0].ExpiresAt == nil {
		t.Errorf("来源应带最早过期时间：%+v", ov.Sources[0])
	}

	// 流向图：订阅覆盖全部来源，推送规则的状态取最近一次任务。
	var fv flowView
	if code := h.do("GET", "/api/v1/flow", nil, &fv); code != 200 {
		t.Fatalf("流向图 = %d", code)
	}
	if len(fv.Devices) != 1 || fv.Devices[0].Status != subConnected || len(fv.Devices[0].SourceIDs) != 2 {
		t.Errorf("设备 = %+v", fv.Devices)
	}
	states := map[uint]flowBinding{}
	for _, b := range fv.Bindings {
		states[b.ID] = b
	}
	if b := states[bGood.ID]; b.State != store.PushSuccess || b.LastAt == nil {
		t.Errorf("能推的推送规则 = %+v", b)
	}
	if b := states[bBad.ID]; b.State != store.PushFailed || b.PushID == 0 || b.LastError == "" {
		t.Errorf("失败的推送规则 = %+v", b)
	}
	for _, d := range fv.Destinations {
		if want := map[uint]string{good.ID: store.PushSuccess, bad.ID: store.PushFailed}[d.ID]; d.State != want || d.Bindings != 1 {
			t.Errorf("目的地 %s = %+v，期望状态 %s", d.Name, d, want)
		}
	}

	// 停用目的地后，连线照样列出，但不算状态。
	h.do("PUT", "/api/v1/destinations/"+itoa(bad.ID), map[string]any{"name": "坏的", "kind": "webhook", "enabled": false, "config": map[string]any{"url": "http://127.0.0.1:1/push"}}, nil)
	h.do("GET", "/api/v1/flow", nil, &fv)
	for _, b := range fv.Bindings {
		if b.ID == bBad.ID && (b.Enabled || b.State != "") {
			t.Errorf("停用目的地后的连线 = %+v", b)
		}
	}

	// 来源链路：身份、当前值打码、推送规则、覆盖它的设备、和它有关的待处理、最近记录。
	var lane laneView
	if code := h.do("GET", "/api/v1/sources/"+itoa(nbID)+"/lane", nil, &lane); code != 200 {
		t.Fatalf("来源链路 = %d", code)
	}
	if lane.Source.ID != nbID || len(lane.Identities) != 1 || len(lane.Bindings) != 2 || len(lane.Devices) != 1 {
		t.Fatalf("来源链路 = %+v", lane)
	}
	ident := lane.Identities[0]
	if ident.Key != "default" || len(ident.Fields) != 2 || len(ident.Pushes) != 2 {
		t.Errorf("身份 = %+v", ident)
	}
	for _, f := range ident.Fields {
		if f.Key == "authorization" && (f.Current == nil || !f.Current.Masked) {
			t.Errorf("敏感字段的当前值应打码：%+v", f)
		}
	}
	if len(lane.Items) != 1 || lane.Items[0].Kind != itemExpiring {
		t.Errorf("和九号有关的待处理（坏的目的地已停用，只剩快过期）= %+v", lane.Items)
	}
	if len(lane.Recent) == 0 || lane.Recent[len(lane.Recent)-1].Kind != activitySync {
		t.Errorf("最近记录应以那次回传开头：%+v", lane.Recent)
	}
	for _, it := range lane.Recent {
		if it.SourceID != nbID {
			t.Errorf("最近记录混进了别的来源：%+v", it)
		}
	}
	if code := h.do("GET", "/api/v1/sources/9999/lane", nil, nil); code != 404 {
		t.Errorf("不存在的来源 = %d", code)
	}

	// 审核一个值，记录里出现审核；按来源筛选；只看问题；翻页。
	h.do("POST", "/api/v1/versions/"+itoa(kinds[itemPendingReview][0].VersionID)+"/approve", map[string]any{}, nil)
	var page activityPageView
	if code := h.do("GET", "/api/v1/activity", nil, &page); code != 200 || len(page.Items) == 0 || page.Next != nil {
		t.Fatalf("记录 = %d %+v", code, page)
	}
	seen := map[string]bool{}
	for i, it := range page.Items {
		seen[it.Kind] = true
		if i > 0 && it.At.After(page.Items[i-1].At) {
			t.Errorf("记录没按时间倒序：%+v 在 %+v 之后", it, page.Items[i-1])
		}
	}
	if !seen[activitySync] || !seen[activityPush] || !seen[activityReview] {
		t.Errorf("记录应包含回传、推送、审核：%v", seen)
	}
	h.do("GET", "/api/v1/activity?source="+itoa(otherSrc.ID), nil, &page)
	for _, it := range page.Items {
		if it.SourceID != otherSrc.ID {
			t.Errorf("按来源筛选混进了 %+v", it)
		}
	}
	h.do("GET", "/api/v1/activity?result=problem", nil, &page)
	if len(page.Items) == 0 {
		t.Error("只看问题时应有那次失败的推送")
	}
	for _, it := range page.Items {
		if it.Outcome != outcomeFailure && it.Outcome != outcomeWarning {
			t.Errorf("只看问题时混进了 %+v", it)
		}
	}
	h.do("GET", "/api/v1/activity", nil, &page)
	all := len(page.Items)
	before := page.Items[1].At.Format(time.RFC3339Nano)
	h.do("GET", "/api/v1/activity?before="+url.QueryEscape(before), nil, &page)
	if len(page.Items) != all-2 {
		t.Errorf("翻页后 %d 条，期望 %d 条", len(page.Items), all-2)
	}
	for q, want := range map[string]int{"source=x": 400, "result=bogus": 400, "before=yesterday": 400} {
		if code := h.do("GET", "/api/v1/activity?"+q, nil, nil); code != want {
			t.Errorf("记录 %s = %d，期望 %d", q, code, want)
		}
	}
}
