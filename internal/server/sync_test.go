package server

import (
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lonelyman0108/ferry/internal/ingest"
	"github.com/lonelyman0108/ferry/internal/store"
)

func testJWT(exp time.Time) string {
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"HS256"}`)) + "." + enc([]byte(`{"exp":`+strconv.FormatInt(exp.Unix(), 10)+`}`)) + ".c2lnbmF0dXJl"
}

// phoneSetup 建好九号来源（合并窗口 0）、访问地址和订阅，返回订阅令牌。
func phoneSetup(t *testing.T, h *harness) string {
	t.Helper()
	var src sourceView
	h.do("POST", "/api/v1/examples/ninebot/import", map[string]any{}, &src)
	zero := 0
	src.Doc.MergeWindow = &zero
	// 测试不访问外网，去掉示例里的主动探测。
	src.Doc.Probe = nil
	if code := h.do("PUT", "/api/v1/sources/"+itoa(src.ID), src.Doc, nil); code != 200 {
		t.Fatalf("改合并窗口 = %d", code)
	}
	var ep endpointView
	h.do("POST", "/api/v1/endpoints", map[string]string{"name": "lan", "base_url": h.ts.URL}, &ep)
	var sv subscriptionView
	h.do("POST", "/api/v1/subscriptions", map[string]any{"name": "iPhone", "client": "surge", "endpoint_id": ep.ID, "all_sources": true}, &sv)
	return strings.Split(strings.TrimPrefix(sv.Links.SubscriptionURL, h.ts.URL+"/sub/"), "/")[0]
}

func TestSyncEndToEnd(t *testing.T) {
	h := ready(t)
	token := phoneSetup(t, h)
	phone := &harness{t: t, srv: h.srv, ts: h.ts, client: http.DefaultClient}
	jwt := testJWT(time.Now().Add(48 * time.Hour))
	body := map[string]any{"source": "ninebot", "matcher": "sign", "fields": map[string]string{"authorization": jwt, "device_id": "DEV-001"}}

	var res ingest.SyncResult
	if code := phone.do("POST", "/api/v1/sync", body, &res, subscriptionHeader, token); code != 200 || res.Result != ingest.ResultAccepted {
		t.Fatalf("回传 = %d %+v", code, res)
	}
	if code := phone.do("POST", "/api/v1/sync", body, &res, subscriptionHeader, token); code != 200 || res.Result != ingest.ResultUnchanged {
		t.Errorf("同样的值再传 = %d %+v", code, res)
	}

	var idents []identityView
	h.do("GET", "/api/v1/identities?source=ninebot", nil, &idents)
	if len(idents) != 1 || idents[0].Key != ingest.DefaultIdentity {
		t.Fatalf("身份 = %+v", idents)
	}
	var ident identityView
	h.do("GET", "/api/v1/identities/"+itoa(idents[0].ID), nil, &ident)
	byKey := map[string]identityField{}
	for _, f := range ident.Fields {
		byKey[f.Key] = f
	}
	auth, dev := byKey["authorization"].Current, byKey["device_id"].Current
	if auth == nil || !auth.Masked || strings.Contains(auth.Value, jwt[10:30]) || auth.ExpiresAt == nil {
		t.Errorf("敏感值应打码并带过期时间：%+v", auth)
	}
	if dev == nil || dev.Value != "DEV-001" || dev.Masked {
		t.Errorf("非敏感值原样显示：%+v", dev)
	}
	var revealed map[string]string
	if code := h.do("POST", "/api/v1/versions/"+itoa(auth.ID)+"/reveal", map[string]any{}, &revealed); code != 200 || revealed["value"] != jwt {
		t.Errorf("显示原值 = %d", code)
	}
	var logs []store.AuditLog
	h.do("GET", "/api/v1/audit?limit=5", nil, &logs)
	if len(logs) == 0 || logs[0].Action != "version.reveal" {
		t.Errorf("显示原值要写审计：%+v", logs)
	}
	for _, l := range logs {
		if strings.Contains(l.Detail, jwt) || strings.Contains(l.Detail, "DEV-001") {
			t.Errorf("审计里不能有值：%+v", l)
		}
	}
	var vs []versionView
	h.do("GET", "/api/v1/identities/"+itoa(idents[0].ID)+"/versions?field=device_id", nil, &vs)
	if len(vs) != 1 || vs[0].Field != "device_id" {
		t.Errorf("按字段列版本 = %+v", vs)
	}
	if code := h.do("GET", "/api/v1/identities/"+itoa(idents[0].ID)+"/versions?field=nope", nil, nil); code != 400 {
		t.Errorf("不存在的字段 = %d，期望 400", code)
	}

	if code := phone.do("POST", "/api/v1/sync", map[string]any{"source": "nope", "fields": map[string]string{"a": "1"}}, nil, subscriptionHeader, token); code != 404 {
		t.Errorf("未知来源 = %d", code)
	}
	if code := phone.do("POST", "/api/v1/sync", map[string]any{"source": "ninebot", "fields": map[string]string{"x": "1"}}, nil, subscriptionHeader, token); code != 400 {
		t.Errorf("未声明字段 = %d", code)
	}
	if code := phone.do("POST", "/api/v1/sync", body, nil, subscriptionHeader, "fs_wrong"); code != 401 {
		t.Errorf("错误令牌 = %d", code)
	}

	h.srv.phoneLimiter = newRateLimiter(2, time.Minute)
	codes := []int{}
	for i := 0; i < 3; i++ {
		codes = append(codes, phone.do("POST", "/api/v1/sync", body, nil, subscriptionHeader, token))
	}
	if codes[2] != 429 {
		t.Errorf("超过频率 = %v，期望第 3 次 429", codes)
	}
}

func TestVersionActions(t *testing.T) {
	h := ready(t)
	token := phoneSetup(t, h)
	phone := &harness{t: t, srv: h.srv, ts: h.ts, client: http.DefaultClient}
	for _, dev := range []string{"DEV-001", "DEV-002"} {
		phone.do("POST", "/api/v1/sync", map[string]any{"source": "ninebot", "fields": map[string]string{"device_id": dev}}, nil, subscriptionHeader, token)
	}
	var idents []identityView
	h.do("GET", "/api/v1/identities", nil, &idents)
	var vs []versionView
	h.do("GET", "/api/v1/identities/"+itoa(idents[0].ID)+"/versions?field=device_id", nil, &vs)
	if len(vs) != 2 || !vs[0].Current || vs[1].Current {
		t.Fatalf("版本 = %+v", vs)
	}
	if code := h.do("POST", "/api/v1/versions/"+itoa(vs[0].ID)+"/activate", map[string]any{}, nil); code != 409 {
		t.Errorf("已是当前值 = %d，期望 409", code)
	}
	if code := h.do("POST", "/api/v1/versions/"+itoa(vs[1].ID)+"/approve", map[string]any{}, nil); code != 409 {
		t.Errorf("不是待审核 = %d，期望 409", code)
	}
	if code := h.do("POST", "/api/v1/versions/"+itoa(vs[1].ID)+"/activate", map[string]any{}, nil); code != 200 {
		t.Fatalf("回滚 = %d", code)
	}
	var ident identityView
	h.do("GET", "/api/v1/identities/"+itoa(idents[0].ID), nil, &ident)
	for _, f := range ident.Fields {
		if f.Key == "device_id" && (f.Current == nil || f.Current.Value != "DEV-001") {
			t.Errorf("回滚后当前值 = %+v", f.Current)
		}
	}
	if code := h.do("POST", "/api/v1/versions/99999/activate", map[string]any{}, nil); code != 404 {
		t.Errorf("不存在的版本 = %d", code)
	}
}

func TestCollectingBatches(t *testing.T) {
	h := ready(t)
	token := phoneSetup(t, h)
	var src sourceView
	h.do("GET", "/api/v1/sources/1", nil, &src)
	window := 300
	src.Doc.MergeWindow = &window
	h.do("PUT", "/api/v1/sources/1", src.Doc, nil)

	phone := &harness{t: t, srv: h.srv, ts: h.ts, client: http.DefaultClient}
	jwt := testJWT(time.Now().Add(48 * time.Hour))
	body := map[string]any{"source": "ninebot", "fields": map[string]string{"authorization": jwt}}
	phone.do("POST", "/api/v1/sync", body, nil, subscriptionHeader, token)

	var bs []batchView
	h.do("GET", "/api/v1/batches", nil, &bs)
	if len(bs) != 1 || len(bs[0].Values) != 1 || !bs[0].Values[0].Masked || bs[0].Values[0].Value == jwt ||
		len(bs[0].Missing) != 1 || bs[0].Missing[0] != "device_id" || bs[0].SubscriptionName != "iPhone" {
		t.Fatalf("收集中的批次 = %+v", bs)
	}
	id := itoa(bs[0].ID)
	if code := h.do("POST", "/api/v1/batches/"+id+"/process", map[string]any{}, nil); code != 200 {
		t.Fatalf("立即处理 = %d", code)
	}
	if code := h.do("POST", "/api/v1/batches/"+id+"/process", map[string]any{}, nil); code != 409 {
		t.Errorf("重复处理 = %d，期望 409", code)
	}
	h.do("GET", "/api/v1/batches", nil, &bs)
	if len(bs) != 0 {
		t.Errorf("处理后不应再有收集中的批次：%+v", bs)
	}
}
