package source

import (
	"context"
	"encoding/base64"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lonelyman0108/ferry/internal/config"
	"github.com/lonelyman0108/ferry/internal/store"
)

func ninebot(t *testing.T) Doc {
	t.Helper()
	for _, e := range Examples() {
		if e.Key == "ninebot" {
			return e.Doc
		}
	}
	t.Fatal("没有九号示例")
	return Doc{}
}

func TestExamplesAreValid(t *testing.T) {
	ex := Examples()
	if len(ex) == 0 {
		t.Fatal("没有内置示例")
	}
	d := ninebot(t)
	if *d.MergeWindow != DefaultMergeWindow || d.Notify != NotifyAlways || !*d.Enabled {
		t.Errorf("默认值没有填上：%+v", d)
	}
}

func TestValidateCollectsAllProblems(t *testing.T) {
	d := Doc{
		Key: "Bad Key", Name: "",
		Matchers: []MatcherDoc{{Name: "a", Host: "*.x"}, {Name: "a", Host: "https://x.com"}},
		Fields: []FieldDoc{
			{Key: "device-id", In: "header", Path: "X"},
			{Key: "tok", Matcher: "missing", In: "nowhere", Path: "x", Type: "date", MinLen: 5, MaxLen: 2, Pattern: "("},
		},
		IdentityField: "nobody",
		Outputs:       []OutputDoc{{Name: "1BAD", Template: "{{.nope}}"}},
	}
	d.Normalize()
	err := d.Validate()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v，期望 ValidationError", err)
	}
	got := map[string]string{}
	for _, p := range ve.Problems {
		got[p.Path] = p.Message
	}
	for _, path := range []string{
		"key", "name", "matchers[1].name", "matchers[1].host",
		"fields[0].key", "fields[1].matcher", "fields[1].in", "fields[1].type", "fields[1].min_len", "fields[1].pattern",
		"identity_field", "outputs[0].name", "outputs[0].template",
	} {
		if got[path] == "" {
			t.Errorf("缺少位置 %s 的问题：%+v", path, ve.Problems)
		}
	}
	if strings.HasPrefix(got["matchers[1].host"], "matchers") || !strings.Contains(got["matchers[1].host"], "域名") {
		t.Errorf("message 应只说明问题、不含位置：%q", got["matchers[1].host"])
	}
	// 只显示文字的场合用 Details，每条带上位置。
	details := ve.Details()
	if len(details) != len(ve.Problems) {
		t.Fatalf("Details 与 Problems 条数不一致：%d / %d", len(details), len(ve.Problems))
	}
	for _, want := range []string{"key 仅允许", "name 不能为空", "matchers[1].name \"a\" 与其他匹配规则重名", "fields[1].matcher 匹配规则 \"missing\" 不存在", "outputs[0].template 引用了未声明的字段"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("缺少问题 %q：%v", want, err)
		}
	}
}

// 出错位置细化到 rules、probe 校验的具体一项。
func TestValidateProblemPaths(t *testing.T) {
	cases := []struct {
		name string
		edit func(d *Doc)
		path string
	}{
		{"路径正则", func(d *Doc) { d.Matchers[0].Path = "(" }, "matchers[0].path"},
		{"缺少读取路径", func(d *Doc) { d.Fields[0].Path = " " }, "fields[0].path"},
		{"正则读取对象", func(d *Doc) { d.Fields[0].In, d.Fields[0].Path, d.Fields[0].RegexOn = "regex", "a(b)", "" }, "fields[0].regex_on"},
		{"最大长度为负", func(d *Doc) { d.Fields[0].MaxLen = -1 }, "fields[0].max_len"},
		{"字段数量", func(d *Doc) { d.Fields = nil }, "fields"},
		{"检测地址", func(d *Doc) { d.Probe.URL = "ftp://x" }, "probe.url"},
		{"检测请求头", func(d *Doc) { d.Probe.Headers = map[string]string{"X": "{{.nope}}"} }, "probe.headers"},
		{"成功条件", func(d *Doc) { d.Probe.Expect = "status +" }, "probe.expect"},
		{"检测超时", func(d *Doc) { d.Probe.Timeout = 99 }, "probe.timeout"},
	}
	for _, c := range cases {
		d := ninebot(t)
		d.Matchers = append([]MatcherDoc{}, d.Matchers...)
		d.Fields = append([]FieldDoc{}, d.Fields...)
		pr := *d.Probe
		d.Probe = &pr
		c.edit(&d)
		d.Normalize()
		var ve *ValidationError
		if !errors.As(d.Validate(), &ve) {
			t.Errorf("%s：期望 ValidationError", c.name)
			continue
		}
		var paths []string
		for _, p := range ve.Problems {
			paths = append(paths, p.Path)
		}
		if !slices.Contains(paths, c.path) {
			t.Errorf("%s：位置 = %v，期望包含 %s", c.name, paths, c.path)
		}
	}
}

func TestMatcherNeedsBody(t *testing.T) {
	d := ninebot(t)
	need := d.MatcherNeedsBody()
	if need["gateway"] || !need["sign"] {
		t.Errorf("need = %v，期望只有 sign 需要正文", need)
	}
	d.Fields = append(d.Fields, FieldDoc{Key: "x", In: "body_form", Path: "x"})
	if need := d.MatcherNeedsBody(); !need["gateway"] {
		t.Error("没绑定规则的正文字段应让所有规则都需要正文")
	}
}

func TestYAMLRoundTrip(t *testing.T) {
	d := ninebot(t)
	data, err := MarshalYAML(d)
	if err != nil {
		t.Fatal(err)
	}
	got, err := UnmarshalYAML(data)
	if err != nil {
		t.Fatalf("导出的 YAML 无法再导入：%v\n%s", err, data)
	}
	if got.Key != d.Key || len(got.Fields) != len(d.Fields) || got.Outputs[1].Template != d.Outputs[1].Template {
		t.Errorf("往返后不一致：%+v", got)
	}
	if _, err := UnmarshalYAML([]byte("key: a\nunknown: 1\n")); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Errorf("未知字段应当报错：%v", err)
	}
}

func TestScriptRules(t *testing.T) {
	d := ninebot(t)
	r, etag := BuildScriptRules([]Doc{d})
	if len(r.Sources) != 1 || !r.Sources[0].Matchers[1].Body || r.Sources[0].Matchers[0].Body {
		t.Errorf("规则不对：%+v", r)
	}
	_, etag2 := BuildScriptRules([]Doc{d})
	if etag != etag2 || !strings.HasPrefix(etag, `"`) {
		t.Errorf("同样内容的 ETag 应当相同且带引号：%s %s", etag, etag2)
	}
	d.Fields[0].Path = "X-Auth"
	if _, etag3 := BuildScriptRules([]Doc{d}); etag3 == etag {
		t.Error("规则变了 ETag 应当变")
	}
	off := false
	d.Enabled = &off
	if r, _ := BuildScriptRules([]Doc{d}); len(r.Sources) != 0 {
		t.Error("停用的来源不应下发")
	}
	if hs := Hostnames([]Doc{ninebot(t)}); len(hs) != 1 || hs[0] != "cn-cbu-gateway.ninebot.com" {
		t.Errorf("Hostnames = %v", hs)
	}
}

func openDB(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(config.Database{Driver: config.DriverSQLite, DSN: filepath.Join(t.TempDir(), "f.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestCreateUpdateKeepsIdentity(t *testing.T) {
	ctx := context.Background()
	db := openDB(t).DB
	s, err := Create(ctx, db, ninebot(t))
	if err != nil {
		t.Fatal(err)
	}
	authID := s.Fields[0].ID
	if _, err := Create(ctx, db, ninebot(t)); !errors.Is(err, ErrKeyExists) {
		t.Errorf("重复 key err = %v", err)
	}

	d := FromModel(s)
	d.Fields[0].Path = "X-Authorization"
	d.Fields = d.Fields[:1]
	d.Outputs = d.Outputs[:1]
	d.Matchers = append(d.Matchers, MatcherDoc{Name: "h5", Host: "*.ninebot.com"})
	d.Normalize()
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	s2, err := Update(ctx, db, s.ID, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(s2.Fields) != 1 || s2.Fields[0].ID != authID || s2.Fields[0].Path != "X-Authorization" {
		t.Errorf("字段应按 key 保留原 ID 并更新：%+v", s2.Fields)
	}
	if len(s2.Outputs) != 1 || len(s2.Matchers) != 3 || s2.Matchers[2].Name != "h5" {
		t.Errorf("子记录没有同步：%d outputs, %+v", len(s2.Outputs), s2.Matchers)
	}

	d.Key = "renamed"
	if _, err := Update(ctx, db, s.ID, d); !errors.Is(err, ErrKeyChanged) {
		t.Errorf("改 key err = %v", err)
	}
	if err := Delete(ctx, db, s.ID); err != nil {
		t.Fatal(err)
	}
	var n int64
	db.Model(&store.Field{}).Count(&n)
	if n != 0 {
		t.Errorf("删除来源后还剩 %d 个字段", n)
	}
	if err := Delete(ctx, db, s.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("重复删除 err = %v", err)
	}
}

func TestDeleteRemovesDependents(t *testing.T) {
	ctx := context.Background()
	db := openDB(t).DB
	a, err := Create(ctx, db, ninebot(t))
	if err != nil {
		t.Fatal(err)
	}
	other := ninebot(t)
	other.Key = "other"
	b, err := Create(ctx, db, other)
	if err != nil {
		t.Fatal(err)
	}
	// 两个来源各有一套身份、批次、版本、映射和推送任务，删掉 a 之后 b 的要原样留着。
	now := time.Now()
	for _, s := range []store.Source{a, b} {
		ident := store.Identity{SourceID: s.ID, Key: "default", LastSeenAt: now}
		batch := store.Batch{SourceID: s.ID, SubscriptionID: 1, Status: store.BatchDone, ClosesAt: now}
		binding := store.Binding{SourceID: s.ID, DestinationID: 1, Mode: store.ModeJoined, Rename: "{}", Enabled: true}
		for _, x := range []any{&ident, &batch, &binding} {
			if err := db.Create(x).Error; err != nil {
				t.Fatal(err)
			}
		}
		v := store.Version{BatchID: batch.ID, SourceID: s.ID, FieldID: s.Fields[0].ID, IdentityID: &ident.ID, Value: "x", ValueHash: "h", Status: store.VersionValid, LastSeenAt: now}
		job := store.PushJob{BindingID: binding.ID, Status: store.PushSuccess, NextAttemptAt: now, Trigger: "manual"}
		for _, x := range []any{&v, &job} {
			if err := db.Create(x).Error; err != nil {
				t.Fatal(err)
			}
		}
	}

	if err := Delete(ctx, db, a.ID); err != nil {
		t.Fatal(err)
	}
	for _, m := range []any{&store.Identity{}, &store.Batch{}, &store.Version{}, &store.Binding{}, &store.PushJob{}} {
		var n int64
		db.Model(m).Count(&n)
		if n != 1 {
			t.Errorf("%T 剩 %d 条，期望只剩另一个来源的 1 条", m, n)
		}
	}
	var left store.Identity
	db.First(&left)
	if left.SourceID != b.ID {
		t.Errorf("留下的身份属于来源 %d，期望 %d", left.SourceID, b.ID)
	}
}

func TestIconValidation(t *testing.T) {
	b64 := base64.StdEncoding.EncodeToString
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)
	jpeg := append([]byte("\xff\xd8\xff\xe0"), make([]byte, 32)...)
	webp := append([]byte("RIFF\x24\x00\x00\x00WEBPVP8 "), make([]byte, 32)...)
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1 1"/>`)
	big := append(append([]byte{}, png...), make([]byte, MaxIconBytes)...)

	for _, tc := range []struct {
		name, icon, want string
	}{
		{"空", "", ""},
		{"PNG", "data:image/png;base64," + b64(png), ""},
		{"JPEG", "data:image/jpeg;base64," + b64(jpeg), ""},
		{"WebP", "data:image/webp;base64," + b64(webp), ""},
		{"SVG", "data:image/svg+xml;base64," + b64(svg), ""},
		{"刚好 64KB", "data:image/png;base64," + b64(big[:MaxIconBytes]), ""},
		{"网络地址", "https://example.com/a.png", "不支持网络地址"},
		{"不支持的类型", "data:image/gif;base64,R0lGODlh", "仅支持 PNG"},
		{"未 base64", "data:image/svg+xml;utf8,<svg/>", "仅支持 PNG"},
		{"base64 无效", "data:image/png;base64,***", "base64 数据无效"},
		{"内容为空", "data:image/png;base64,", "图片内容为空"},
		{"超过 64KB", "data:image/png;base64," + b64(big[:MaxIconBytes+1]), "超过 64 KB"},
		{"类型不符", "data:image/png;base64," + b64(jpeg), "不符"},
		{"不是 SVG", "data:image/svg+xml;base64," + b64([]byte("<html></html>")), "不是 SVG"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := ninebot(t)
			d.Icon = tc.icon
			d.Normalize()
			err := d.Validate()
			if tc.want == "" {
				if err != nil {
					t.Errorf("应当通过：%v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "icon") || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v，期望包含 %q", err, tc.want)
			}
		})
	}
}

// SVG 图标里的 DOCTYPE、脚本、事件属性和外部引用一律拒收，正常的图形、文档内引用与内嵌位图不受影响。
func TestIconSVGUnsafe(t *testing.T) {
	const head = `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" viewBox="0 0 24 24">`
	for _, tc := range []struct {
		name, body string
		bad        bool
	}{
		{"普通 path", `<path d="M0 0h24v24H0z" fill="#333"/>`, false},
		{"use 指向片段", `<defs><path id="a" d="M0 0h1v1z"/></defs><use href="#a"/>`, false},
		{"xlink:href 指向片段", `<use xlink:href = "#a"/>`, false},
		{"渐变 url(#g)", `<defs><linearGradient id="g"/></defs><rect width="24" height="24" fill="url(#g)"/>`, false},
		{"style 里的 url('#g')", `<rect style="fill: url( '#g' )" width="1" height="1"/>`, false},
		{"内嵌位图", `<image href="data:image/png;base64,iVBORw0KGgo=" width="24" height="24"/>`, false},
		{"内嵌 SVG", `<image href="data:image/svg+xml;base64,PHN2Zy8+" width="24" height="24"/>`, true},
		{"script", `<script>alert(1)</script>`, true},
		{"SCRIPT 大写", `<SCRIPT>alert(1)</SCRIPT>`, true},
		{"ScRiPt 混写", `<ScRiPt type="text/javascript">alert(1)</ScRiPt>`, true},
		{"带前缀的 script", `<svg:script xmlns:svg="http://www.w3.org/2000/svg">alert(1)</svg:script>`, true},
		{"onload 带空白", `<rect width="1" height="1" onload = "alert(1)"/>`, true},
		{"OnClick 大写", `<rect width="1" height="1" OnClick='alert(1)'/>`, true},
		{"xlink:href 外链", `<image xlink:href="https://example.com/a.png" width="24" height="24"/>`, true},
		{"href 外链带空白", `<use href ="//example.com/s.svg#a"/>`, true},
		{"style 里 url(http…)", `<rect style="fill:url(http://example.com/x.svg#g)" width="1" height="1"/>`, true},
		{"style 元素里 url(http…)", `<style>rect{fill:URL("https://example.com/p")}</style>`, true},
		{"@import", `<style>@import "https://example.com/a.css";</style>`, true},
		{"javascript: 字符引用", `<a href="jav&#x61;script:alert(1)"><path d="M0 0h1v1z"/></a>`, true},
		{"javascript: 夹空白", `<animate to="java&#9;script:alert(1)"/>`, true},
		{"动画改写 href", `<animate attributeName="href" to="https://example.com/"/>`, true},
		{"foreignObject", `<foreignObject width="24" height="24"><div/></foreignObject>`, true},
		{"CSS 转义的 url", `<rect style="fill:u\72l(https://example.com/p)" width="1" height="1"/>`, true},
		{"CSS 转义的 @import", `<style>@\69mport "https://example.com/a.css";</style>`, true},
		{"image-set 字符串地址", `<rect style="fill:image-set(&quot;https://example.com/a.png&quot; 1x)" width="1" height="1"/>`, true},
		{"-webkit-image-set", `<style>rect{background:-webkit-image-set("https://example.com/a.png" 1x)}</style>`, true},
		{"cross-fade", `<style>rect{background:cross-fade("https://example.com/a.png" 50%, url(#g))}</style>`, true},
		{"image 元素不误伤", `<image href="#a" width="24" height="24"/>`, false},
		{"属性里的 Windows 路径", `<path d="M0 0h1v1z" inkscape:export-filename="C:\Users\me\icon.png"/>`, false},
		{"转义加空白的 url", `<style>rect{fill:\75 rl(https://example.com/p)}</style>`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := ninebot(t)
			d.Icon = "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(head+tc.body+"</svg>"))
			d.Normalize()
			err := d.Validate()
			if !tc.bad {
				if err != nil {
					t.Errorf("应当通过：%v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "icon SVG 图片不得包含脚本、事件属性或外部引用") {
				t.Errorf("err = %v，期望拒收", err)
			}
		})
	}
	// DTD 与实体声明在 <svg 之前，单独拼。
	for _, pre := range []string{
		`<!DOCTYPE svg [<!ENTITY x "y">]>`,
		`<!doctype svg>`,
		`<!ENTITY x SYSTEM "file:///etc/passwd">`,
	} {
		d := ninebot(t)
		d.Icon = "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(pre+head+`<path d="M0 0"/></svg>`))
		d.Normalize()
		if err := d.Validate(); err == nil || !strings.Contains(err.Error(), "icon SVG 图片不得包含 DOCTYPE 或 ENTITY 声明") {
			t.Errorf("%s：err = %v，期望拒收", pre, err)
		}
	}
}

func TestIconPersists(t *testing.T) {
	ctx := context.Background()
	db := openDB(t).DB
	d := ninebot(t)
	d.Icon = "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`))
	s, err := Create(ctx, db, d)
	if err != nil {
		t.Fatal(err)
	}
	if got := FromModel(s).Icon; got != d.Icon {
		t.Errorf("新建后图标 = %q", got)
	}
	d.Icon = ""
	s, err = Update(ctx, db, s.ID, d)
	if err != nil {
		t.Fatal(err)
	}
	if got := FromModel(s).Icon; got != "" {
		t.Errorf("清除后图标 = %q，期望为空", got)
	}
}
