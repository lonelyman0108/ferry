package ingest

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lonelyman0108/ferry/internal/config"
	"github.com/lonelyman0108/ferry/internal/events"
	"github.com/lonelyman0108/ferry/internal/probe"
	"github.com/lonelyman0108/ferry/internal/secret"
	"github.com/lonelyman0108/ferry/internal/source"
	"github.com/lonelyman0108/ferry/internal/store"
)

type env struct {
	t   *testing.T
	svc *Service
	sub store.Subscription
	now time.Time
	evs []events.Event
}

func newEnv(t *testing.T, docs ...source.Doc) *env {
	t.Helper()
	st, err := store.Open(config.Database{Driver: config.DriverSQLite, DSN: filepath.Join(t.TempDir(), "f.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	key, _ := secret.GenerateKey()
	box, _ := secret.New(key)
	e := &env{t: t, now: time.Unix(1_800_000_000, 0)}
	bus := &events.Bus{}
	bus.Subscribe(func(ev events.Event) { e.evs = append(e.evs, ev) })
	e.svc = &Service{DB: st.DB, Box: box, Bus: bus, Now: func() time.Time { return e.now }}
	for _, d := range docs {
		d.Normalize()
		if err := d.Validate(); err != nil {
			t.Fatal(err)
		}
		if _, err := source.Create(context.Background(), st.DB, d); err != nil {
			t.Fatal(err)
		}
	}
	e.sub = store.Subscription{Name: "iPhone", Client: "surge", EndpointID: 1, TokenHash: "h", TokenPrefix: "p", TokenSealed: "s", AllSources: true, SourceKeys: "[]"}
	if err := st.DB.Create(&e.sub).Error; err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *env) sync(src string, fields map[string]string) SyncResult {
	e.t.Helper()
	r, err := e.svc.Sync(context.Background(), e.sub, SyncInput{Source: src, Fields: fields})
	if err != nil {
		e.t.Fatalf("Sync：%v", err)
	}
	return r
}

func (e *env) process() int {
	e.t.Helper()
	n, err := e.svc.ProcessDue(context.Background())
	if err != nil {
		e.t.Fatal(err)
	}
	return n
}

// current 返回某来源某身份下各字段的当前值（明文）。
func (e *env) current(identityKey string) map[string]string {
	var ident store.Identity
	if err := e.svc.DB.Where(map[string]any{"key": identityKey}).First(&ident).Error; err != nil {
		return nil
	}
	var vs []store.Version
	e.svc.DB.Where("identity_id = ? AND current = ?", ident.ID, true).Find(&vs)
	out := map[string]string{}
	for _, v := range vs {
		var f store.Field
		e.svc.DB.First(&f, v.FieldID)
		val, err := e.svc.Reveal(v)
		if err != nil {
			e.t.Fatal(err)
		}
		out[f.Key] = val
	}
	return out
}

func jwtToken(exp time.Time) string {
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"HS256"}`)) + "." + enc([]byte(`{"exp":`+strconv.FormatInt(exp.Unix(), 10)+`}`)) + ".c2lnbmF0dXJl"
}

// ninebot 返回去掉了主动探测的九号示例：单元测试不访问外网，探测另有专门的用例。
func ninebot(t *testing.T) source.Doc {
	for _, ex := range source.Examples() {
		if ex.Key == "ninebot" {
			d := ex.Doc
			d.Probe = nil
			return d
		}
	}
	t.Fatal("没有九号示例")
	return source.Doc{}
}

func TestMergeWindowAndDedupe(t *testing.T) {
	e := newEnv(t, ninebot(t))
	tok := jwtToken(e.now.Add(48 * time.Hour))

	if r := e.sync("ninebot", map[string]string{"authorization": tok}); r.Result != ResultAccepted || !strings.Contains(r.Message, "1 分钟") {
		t.Fatalf("第一次回传 = %+v", r)
	}
	if e.process() != 0 {
		t.Error("窗口未到期、字段未到齐时不应处理")
	}
	e.now = e.now.Add(30 * time.Second)
	// 第二次回传后两个字段都到齐了，不等窗口结束就处理。
	if r := e.sync("ninebot", map[string]string{"device_id": "DEV-001", "authorization": tok}); r.Result != ResultAccepted || strings.Contains(r.Message, "后生效") {
		t.Fatalf("第二次回传 = %+v", r)
	}
	var batches int64
	e.svc.DB.Model(&store.Batch{}).Count(&batches)
	if batches != 1 {
		t.Errorf("窗口内应只有 1 个批次，实际 %d", batches)
	}
	if e.process() != 0 {
		t.Error("批次已提前处理，不应再处理")
	}
	if cur := e.current(DefaultIdentity); cur["authorization"] != tok || cur["device_id"] != "DEV-001" {
		t.Errorf("当前值 = %v", cur)
	}
	if len(e.evs) != 2 || e.evs[0].Type != events.IdentityNew || e.evs[1].Type != events.ValueUpdated || len(e.evs[1].Fields) != 2 {
		t.Errorf("事件 = %+v", e.evs)
	}

	if r := e.sync("ninebot", map[string]string{"authorization": tok}); r.Result != ResultUnchanged {
		t.Errorf("值没变 = %+v", r)
	}
	var v store.Version
	e.svc.DB.Where("current = ?", true).Order("id").First(&v)
	if v.SeenCount != 3 {
		t.Errorf("同一个 token 共回传 3 次，SeenCount = %d", v.SeenCount)
	}

	tok2 := jwtToken(e.now.Add(72 * time.Hour))
	e.sync("ninebot", map[string]string{"authorization": tok2})
	e.now = e.now.Add(6 * time.Minute)
	e.process()
	if cur := e.current(DefaultIdentity); cur["authorization"] != tok2 || cur["device_id"] != "DEV-001" {
		t.Errorf("新值应替换当前值，其他字段不变：%v", cur)
	}
	var n int64
	e.svc.DB.Model(&store.Version{}).Where("current = ?", true).Count(&n)
	if n != 2 {
		t.Errorf("当前值应只有 2 个，实际 %d", n)
	}
}

func TestSensitiveValueIsSealed(t *testing.T) {
	e := newEnv(t, ninebot(t))
	tok := jwtToken(e.now.Add(time.Hour))
	e.sync("ninebot", map[string]string{"authorization": tok, "device_id": "DEV-001"})
	var vs []store.Version
	e.svc.DB.Order("id").Find(&vs)
	for _, v := range vs {
		if strings.Contains(v.Value, tok) {
			t.Error("敏感值不应以明文存储")
		}
	}
	sealed := 0
	for _, v := range vs {
		if v.Sealed {
			sealed++
		}
	}
	if sealed != 1 {
		t.Errorf("只有 authorization 是敏感字段，sealed = %d", sealed)
	}
}

func TestRejected(t *testing.T) {
	e := newEnv(t, ninebot(t))
	expired := jwtToken(e.now.Add(-time.Hour))
	r := e.sync("ninebot", map[string]string{"authorization": expired, "device_id": "DEV-001"})
	// 其余字段的新值照常收下，结果文字写清哪个字段没通过，不含值。
	if r.Result != ResultAccepted || r.Message != "收到 1 个新值，约 1 分钟后生效，authorization 未通过校验" ||
		!strings.Contains(r.Rejected["authorization"], "过期") || strings.Contains(r.Message, expired) {
		t.Fatalf("过期 JWT = %+v", r)
	}
	if len(e.evs) != 1 || e.evs[0].Type != events.ValueRejected {
		t.Errorf("事件 = %+v", e.evs)
	}
	e.sync("ninebot", map[string]string{"authorization": expired})
	var n int64
	e.svc.DB.Model(&store.Version{}).Where("status = ?", store.VersionRejected).Count(&n)
	if n != 1 {
		t.Errorf("同一个坏值一小时内只记一次，实际 %d", n)
	}
	if r := e.sync("ninebot", map[string]string{"device_id": "x"}); r.Result != ResultRejected || r.Message != "device_id 未通过校验" {
		t.Errorf("太短的 device_id = %+v", r)
	}
}

// 有字段被驳回时，其余字段照常判断是否到齐；这次回传没有新值、批次里也没有在等的值时，批次当场关掉。
func TestPartialRejectionAndIdleBatch(t *testing.T) {
	e := newEnv(t, ninebot(t))
	tok := jwtToken(e.now.Add(48 * time.Hour))
	expired := jwtToken(e.now.Add(-time.Hour))
	collecting := func() int64 {
		var n int64
		e.svc.DB.Model(&store.Batch{}).Where("status = ?", store.BatchCollecting).Count(&n)
		return n
	}

	r := e.sync("ninebot", map[string]string{"authorization": "not-a-jwt-xxxxxxxxxxxx", "device_id": "x"})
	if r.Result != ResultRejected || r.Message != "authorization、device_id 未通过校验" {
		t.Errorf("整条被驳回 = %+v", r)
	}
	if n := collecting(); n != 0 {
		t.Errorf("整条被驳回后收集中的批次 = %d，期望 0", n)
	}

	e.sync("ninebot", map[string]string{"authorization": tok})
	e.now = e.now.Add(10 * time.Second)
	// 带来的 authorization 过期了，但批次里已经有一个有效的，device_id 一到就算齐。
	r = e.sync("ninebot", map[string]string{"device_id": "DEV-001", "authorization": expired})
	if r.Result != ResultAccepted || r.Message != "收到 1 个新值，authorization 未通过校验" {
		t.Errorf("部分被驳回 = %+v", r)
	}
	if cur := e.current(DefaultIdentity); cur["device_id"] != "DEV-001" || cur["authorization"] != tok {
		t.Errorf("字段到齐后应当场生效：%v", cur)
	}

	e.now = e.now.Add(time.Minute)
	if r := e.sync("ninebot", map[string]string{"device_id": "DEV-001"}); r.Result != ResultUnchanged || collecting() != 0 {
		t.Errorf("值没变 = %+v，收集中 %d", r, collecting())
	}
	var ident store.Identity
	e.svc.DB.First(&ident)
	if !ident.LastSeenAt.Equal(e.now) {
		t.Errorf("值没变的回传也要当场更新最近回传时间：%v", ident.LastSeenAt)
	}
	r = e.sync("ninebot", map[string]string{"device_id": "DEV-001", "authorization": expired})
	if r.Result != ResultRejected || r.Message != "authorization 未通过校验，其余值未变化" || collecting() != 0 {
		t.Errorf("没变的值加被驳回的值 = %+v，收集中 %d", r, collecting())
	}

	// 批次里还有在等的值时，没有新值的回传不关它。
	e.sync("ninebot", map[string]string{"device_id": "DEV-002"})
	e.sync("ninebot", map[string]string{"device_id": "x"})
	if n := collecting(); n != 1 {
		t.Errorf("还有在等的值时收集中的批次 = %d，期望 1", n)
	}
}

func TestRejectedBatchIdentity(t *testing.T) {
	e := newEnv(t, ninebot(t))
	e.sync("ninebot", map[string]string{"authorization": "not-a-jwt-xxxxxxxxxxxx", "device_id": "x"})
	e.now = e.now.Add(2 * time.Minute)
	e.process()
	var identities int64
	e.svc.DB.Model(&store.Identity{}).Count(&identities)
	if identities != 0 {
		t.Errorf("全部被驳回的批次不应新建身份，实际 %d 个", identities)
	}

	// 身份已经存在时，被驳回的批次照常挂到它下面。
	e.sync("ninebot", map[string]string{"device_id": "DEV-001"})
	e.now = e.now.Add(2 * time.Minute)
	e.process()
	e.sync("ninebot", map[string]string{"device_id": "y"})
	e.now = e.now.Add(2 * time.Minute)
	e.process()
	var b store.Batch
	e.svc.DB.Order("id DESC").First(&b)
	if b.Status != store.BatchDone || b.IdentityID == nil {
		t.Errorf("已有身份时被驳回的批次 = %+v", b)
	}
	e.svc.DB.Model(&store.Identity{}).Count(&identities)
	if identities != 1 {
		t.Errorf("身份数 = %d，期望 1", identities)
	}
}

func TestBadInput(t *testing.T) {
	off := ninebot(t)
	off.Key, off.Name = "off", "停用"
	f := false
	off.Enabled = &f
	e := newEnv(t, ninebot(t), off)
	var bad *BadInputError
	if _, err := e.svc.Sync(context.Background(), e.sub, SyncInput{Source: "ninebot", Fields: map[string]string{"nope": "1"}}); !errors.As(err, &bad) {
		t.Errorf("未声明字段 err = %v", err)
	}
	if _, err := e.svc.Sync(context.Background(), e.sub, SyncInput{Source: "ninebot", Matcher: "nope", Fields: map[string]string{"device_id": "DEV-001"}}); !errors.As(err, &bad) {
		t.Errorf("不存在的匹配规则 err = %v", err)
	}
	for _, src := range []string{"missing", "off"} {
		if _, err := e.svc.Sync(context.Background(), e.sub, SyncInput{Source: src, Fields: map[string]string{"device_id": "DEV-001"}}); !errors.Is(err, ErrUnknownSource) {
			t.Errorf("%s err = %v", src, err)
		}
	}
	e.sub.AllSources, e.sub.SourceKeys = false, `["other"]`
	if _, err := e.svc.Sync(context.Background(), e.sub, SyncInput{Source: "ninebot", Fields: map[string]string{"device_id": "DEV-001"}}); !errors.Is(err, ErrUnknownSource) {
		t.Errorf("订阅看不到的来源 err = %v", err)
	}
}

func TestImmediateAndReview(t *testing.T) {
	d := ninebot(t)
	zero := 0
	d.MergeWindow = &zero
	e := newEnv(t, d)
	if r := e.sync("ninebot", map[string]string{"device_id": "DEV-001"}); r.Result != ResultAccepted {
		t.Fatal(r)
	}
	if cur := e.current(DefaultIdentity); cur["device_id"] != "DEV-001" {
		t.Errorf("合并窗口为 0 时应立即生效：%v", cur)
	}

	m := ninebot(t)
	m.Key, m.Review, m.MergeWindow = "manual", source.ReviewManual, &zero
	m.Matchers[0].Host, m.Matchers[1].Host = "a.example.com", "a.example.com"
	e = newEnv(t, m)
	r := e.sync("manual", map[string]string{"device_id": "DEV-002"})
	if !strings.Contains(r.Message, "审核") {
		t.Errorf("人工审核的提示 = %q", r.Message)
	}
	var v store.Version
	e.svc.DB.First(&v)
	if v.Status != store.VersionPendingReview || v.Current {
		t.Errorf("人工审核时不应直接生效：%+v", v)
	}
	if r := e.sync("manual", map[string]string{"device_id": "DEV-002"}); r.Result != ResultUnchanged {
		t.Errorf("待审核的同一个值再次回传 = %+v", r)
	}
}

func TestIdentityField(t *testing.T) {
	jd := source.Doc{
		Key: "jd", Name: "京东", IdentityField: "pt_pin",
		Matchers: []source.MatcherDoc{{Name: "api", Host: "*.jd.com"}},
		Fields: []source.FieldDoc{
			{Key: "pt_key", In: "cookie", Path: "pt_key", Sensitive: true},
			{Key: "pt_pin", In: "cookie", Path: "pt_pin"},
		},
	}
	e := newEnv(t, jd)
	e.sync("jd", map[string]string{"pt_key": "AAJ-1", "pt_pin": "jd_alice"})
	e.now = e.now.Add(6 * time.Minute)
	e.process()
	e.sync("jd", map[string]string{"pt_key": "AAJ-2"})
	e.now = e.now.Add(6 * time.Minute)
	e.process()
	if cur := e.current("jd_alice"); cur["pt_key"] != "AAJ-2" || cur["pt_pin"] != "jd_alice" {
		t.Errorf("缺身份字段的批次应沿用上次的身份：%v", cur)
	}
	var identities int64
	e.svc.DB.Model(&store.Identity{}).Count(&identities)
	if identities != 1 {
		t.Errorf("身份数 = %d，期望 1", identities)
	}
}

func TestProbe(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("Authorization") {
		case "good-token-1234567890":
			_, _ = w.Write([]byte(`{"code":0,"data":{"userId":"u-42"}}`))
		case "slow-token-1234567890":
			time.Sleep(1500 * time.Millisecond)
		default:
			_, _ = w.Write([]byte(`{"code":401}`))
		}
	}))
	defer ts.Close()

	d := source.Doc{
		Key: "svc", Name: "测试服务",
		Matchers: []source.MatcherDoc{{Name: "api", Host: "api.example.com"}},
		Fields:   []source.FieldDoc{{Key: "token", In: "header", Path: "Authorization", Sensitive: true}},
		Probe: &probe.Spec{URL: ts.URL + "/me", Headers: map[string]string{"Authorization": "{{.token}}"},
			Expect: "status == 200 && json.code == 0", Identity: "json.data.userId", Timeout: 1},
	}
	zero := 0
	d.MergeWindow = &zero

	e := newEnv(t, d)
	e.sync("svc", map[string]string{"token": "good-token-1234567890"})
	if cur := e.current("u-42"); cur["token"] != "good-token-1234567890" {
		t.Errorf("探测通过时应生效，并用探测结果里的身份：%v", cur)
	}

	e.sync("svc", map[string]string{"token": "bad-token-1234567890"})
	var bad store.Version
	e.svc.DB.Where("status = ?", store.VersionRejected).First(&bad)
	if bad.ID == 0 || !strings.Contains(bad.Reason, "检测未通过") {
		t.Errorf("探测不通过应驳回：%+v", bad)
	}
	if cur := e.current("u-42"); cur["token"] != "good-token-1234567890" {
		t.Errorf("驳回不应影响当前值：%v", cur)
	}
	if last := e.evs[len(e.evs)-1]; last.Type != events.ValueRejected {
		t.Errorf("应发出驳回事件：%+v", last)
	}

	e.sync("svc", map[string]string{"token": "slow-token-1234567890"})
	var slow store.Version
	e.svc.DB.Where("status = ?", store.VersionPendingReview).First(&slow)
	if slow.ID == 0 || !strings.Contains(slow.Reason, "检测出错") {
		t.Errorf("探测超时应转人工审核：%+v", slow)
	}
}

func TestProcessNow(t *testing.T) {
	e := newEnv(t, ninebot(t))
	tok := jwtToken(e.now.Add(48 * time.Hour))
	e.sync("ninebot", map[string]string{"authorization": tok})
	var b store.Batch
	e.svc.DB.First(&b)
	if done, err := e.svc.ProcessNow(context.Background(), b.ID); err != nil || !done {
		t.Fatalf("立即处理 = %v, %v", done, err)
	}
	if cur := e.current(DefaultIdentity); cur["authorization"] != tok {
		t.Errorf("立即处理后的当前值 = %v", cur)
	}
	if done, _ := e.svc.ProcessNow(context.Background(), b.ID); done {
		t.Error("已处理的批次不应再处理")
	}
	if _, err := e.svc.ProcessNow(context.Background(), 999); !errors.Is(err, ErrBatchNotFound) {
		t.Errorf("不存在的批次 = %v", err)
	}
}
