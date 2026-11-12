package server

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/lonelyman0108/ferry/internal/source"
)

func TestSourceLifecycle(t *testing.T) {
	h := ready(t)

	var examples []exampleView
	h.do("GET", "/api/v1/examples", nil, &examples)
	if len(examples) == 0 || examples[0].Key != "ninebot" {
		t.Fatalf("示例列表 = %+v", examples)
	}
	var created sourceView
	if code := h.do("POST", "/api/v1/examples/ninebot/import", map[string]any{}, &created); code != 201 {
		t.Fatalf("导入示例 = %d", code)
	}
	if code := h.do("POST", "/api/v1/examples/ninebot/import", map[string]any{}, nil); code != 409 {
		t.Errorf("重复导入 = %d，期望 409", code)
	}
	id := itoa(created.ID)

	var tr tryResult
	sign := map[string]any{
		"source": created.Doc,
		"method": "POST", "url": "https://cn-cbu-gateway.ninebot.com/portal/api/user-sign/v2/sign",
		"headers": map[string]string{"Authorization": "eyJhbGciOiJIUzI1NiJ9.e30.sig-value-here"},
		"body":    `{"deviceId":"DEV-001"}`,
	}
	if code := h.do("POST", "/api/v1/sources/try", sign, &tr); code != 200 {
		t.Fatalf("试跑 = %d", code)
	}
	if len(tr.Matched) != 2 || !tr.Fields["device_id"].OK || tr.Outputs["NINEBOT_DEVICE_ID"].Value != "DEV-001" ||
		tr.Outputs["NINEBOT_AUTHORIZATION"].Value != "eyJhbGciOiJIUzI1NiJ9.e30.sig-value-here" {
		t.Errorf("签到请求试跑结果不对：%+v", tr)
	}
	status := map[string]any{"source": created.Doc, "url": "https://cn-cbu-gateway.ninebot.com/portal/api/user-sign/v2/status", "headers": map[string]string{"authorization": "eyJxx.yy.zz-long-enough"}}
	h.do("POST", "/api/v1/sources/try", status, &tr)
	if len(tr.Matched) != 1 || !tr.Fields["authorization"].OK || tr.Fields["device_id"].OK || tr.Outputs["NINEBOT_DEVICE_ID"].OK {
		t.Errorf("状态请求只应取到 authorization：%+v", tr)
	}
	for _, m := range tr.Matchers {
		if !m.Matched && m.Reason != "路径不符" {
			t.Errorf("规则 %s 未命中的原因 = %q，期望“路径不符”", m.Name, m.Reason)
		}
	}

	// 未保存的草稿也能试跑：改了域名的规则给出未命中原因。
	draft := created.Doc
	draft.Matchers = append([]source.MatcherDoc{}, draft.Matchers...)
	draft.Matchers[0].Host = "other.example.com"
	sign["source"] = draft
	h.do("POST", "/api/v1/sources/try", sign, &tr)
	if tr.Matchers[0].Matched || tr.Matchers[0].Reason != "域名不符" {
		t.Errorf("草稿试跑 = %+v，期望第一条规则域名不符", tr.Matchers)
	}

	var got sourceView
	h.do("GET", "/api/v1/sources/"+id, nil, &got)
	got.Doc.Fields[1].Matcher = "missing"
	got.Doc.Outputs = append(got.Doc.Outputs, got.Doc.Outputs[0])
	var bad errorBody
	if code := h.do("PUT", "/api/v1/sources/"+id, got.Doc, &bad); code != 400 || len(bad.Details) < 2 {
		t.Errorf("非法更新 = %d %+v，期望 400 且列出全部问题", code, bad)
	}
	got.Doc.Key = "other"
	got.Doc.Fields[1].Matcher = "sign"
	got.Doc.Outputs = got.Doc.Outputs[:2]
	if code := h.do("PUT", "/api/v1/sources/"+id, got.Doc, nil); code != 409 {
		t.Errorf("改 key = %d，期望 409", code)
	}

	resp, err := h.client.Get(h.ts.URL + "/api/v1/sources/" + id + "/export")
	if err != nil {
		t.Fatal(err)
	}
	yamlText, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(yamlText), "key: ninebot") || !strings.Contains(resp.Header.Get("Content-Disposition"), "ferry-ninebot.yaml") {
		t.Fatalf("导出 = %d %q", resp.StatusCode, yamlText)
	}
	if code := h.do("DELETE", "/api/v1/sources/"+id, nil, nil); code != 200 {
		t.Fatalf("删除 = %d", code)
	}
	if code := h.do("POST", "/api/v1/sources/import", map[string]string{"yaml": string(yamlText)}, &created); code != 201 {
		t.Errorf("导出的 YAML 再导入 = %d", code)
	}
	var importErr errorBody
	if code := h.do("POST", "/api/v1/sources/import", map[string]string{"yaml": "key: [oops"}, &importErr); code != 400 {
		t.Errorf("坏 YAML = %d", code)
	}

	var list []sourceSummary
	h.do("GET", "/api/v1/sources", nil, &list)
	if len(list) != 1 || list[0].Fields != 2 || list[0].Matchers != 2 {
		t.Errorf("列表 = %+v", list)
	}

	var tok createdToken
	h.do("POST", "/api/v1/tokens", map[string]string{"name": "ci"}, &tok)
	bearer := &harness{t: t, srv: h.srv, ts: h.ts, client: http.DefaultClient}
	if code := bearer.do("GET", "/api/v1/sources", nil, nil, "Authorization", "Bearer "+tok.Token); code != 200 {
		t.Errorf("令牌读来源 = %d", code)
	}
	if code := bearer.do("POST", "/api/v1/examples/ninebot/import", map[string]any{}, nil, "Authorization", "Bearer "+tok.Token); code != 403 {
		t.Errorf("令牌写来源 = %d，期望 403", code)
	}
}
