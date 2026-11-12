package source

import (
	"context"
	"errors"
	"path/filepath"
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
	for _, want := range []string{"key 只能", "name 不能为空", "重复", "matchers[1]", "fields[0].key", "matcher \"missing\" 不存在", "不支持的取值位置", "type \"date\"", "min_len", "pattern", "identity_field", "outputs[0].name", "未声明的字段"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("缺少问题 %q：%v", want, err)
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
