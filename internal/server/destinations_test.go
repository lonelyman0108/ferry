package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/lonelyman0108/ferry/internal/dest"
)

func TestDestinationAndPush(t *testing.T) {
	var mu sync.Mutex
	var received []dest.Push
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p dest.Push
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &p)
		mu.Lock()
		received = append(received, p)
		mu.Unlock()
	}))
	defer hook.Close()

	h := ready(t)
	token := phoneSetup(t, h)

	var dv destinationView
	in := map[string]any{"name": "hook", "kind": "webhook", "config": map[string]any{"url": hook.URL, "secret": "s3cret"}}
	if code := h.do("POST", "/api/v1/destinations", in, &dv); code != 201 {
		t.Fatalf("建目的地 = %d", code)
	}
	if dv.Config.Secret != "" || !dv.SecretsSet["secret"] {
		t.Errorf("密钥应只写不读：%+v", dv)
	}
	in["config"] = map[string]any{"url": hook.URL}
	h.do("PUT", "/api/v1/destinations/"+itoa(dv.ID), in, &dv)
	if !dv.SecretsSet["secret"] {
		t.Error("更新时留空的密钥应沿用旧值")
	}
	var test map[string]any
	h.do("POST", "/api/v1/destinations/"+itoa(dv.ID)+"/test", map[string]any{}, &test)
	if test["ok"] != true || len(received) != 1 || received[0].SourceKey != "ferry-test" {
		t.Errorf("测试连接 = %v，收到 %+v", test, received)
	}

	var src []sourceSummary
	h.do("GET", "/api/v1/sources", nil, &src)
	bad := map[string]any{"source_id": src[0].ID, "destination_id": dv.ID, "mode": "per_identity", "rename": map[string]string{"NOPE": "X"}}
	if code := h.do("POST", "/api/v1/bindings", bad, nil); code != 400 {
		t.Errorf("改名引用不存在的输出 = %d", code)
	}
	var bv bindingView
	b := map[string]any{"source_id": src[0].ID, "destination_id": dv.ID, "mode": "per_identity",
		"rename": map[string]string{"NINEBOT_AUTHORIZATION": "TEMP_NINEBOT_AUTHORIZATION", "NINEBOT_DEVICE_ID": "TEMP_NINEBOT_DEVICE_ID"}}
	if code := h.do("POST", "/api/v1/bindings", b, &bv); code != 201 {
		t.Fatalf("建映射 = %d", code)
	}

	phone := &harness{t: t, srv: h.srv, ts: h.ts, client: http.DefaultClient}
	phone.do("POST", "/api/v1/sync", map[string]any{"source": "ninebot", "fields": map[string]string{"device_id": "DEV-001"}}, nil, subscriptionHeader, token)
	var jobs []pushView
	h.do("GET", "/api/v1/pushes", nil, &jobs)
	if len(jobs) != 1 || jobs[0].Status != "queued" {
		t.Fatalf("回传后应排一个推送任务：%+v", jobs)
	}

	var idents []identityView
	h.do("GET", "/api/v1/identities", nil, &idents)
	if code := h.do("POST", "/api/v1/pushes", map[string]any{"binding_id": bv.ID}, nil); code != 400 {
		t.Errorf("一个身份一条的映射不指定身份 = %d", code)
	}
	var job pushView
	if code := h.do("POST", "/api/v1/pushes", map[string]any{"binding_id": bv.ID, "identity_id": idents[0].ID}, &job); code != 200 || job.Status != "success" {
		t.Fatalf("手动推送 = %d %+v", code, job)
	}
	mu.Lock()
	last := received[len(received)-1]
	mu.Unlock()
	if len(last.Vars) != 1 || last.Vars[0].Name != "TEMP_NINEBOT_DEVICE_ID" || last.Vars[0].Value != "DEV-001" || last.Vars[0].Remarks != "ferry:ninebot:default" {
		t.Errorf("Webhook 收到 %+v（authorization 还没有值，只推 device_id）", last)
	}

	if code := h.do("DELETE", "/api/v1/destinations/"+itoa(dv.ID), nil, nil); code != 409 {
		t.Errorf("有映射时删除目的地 = %d，期望 409", code)
	}
	h.do("DELETE", "/api/v1/bindings/"+itoa(bv.ID), nil, nil)
	if code := h.do("DELETE", "/api/v1/destinations/"+itoa(dv.ID), nil, nil); code != 200 {
		t.Errorf("删掉映射后删除目的地 = %d", code)
	}
}
