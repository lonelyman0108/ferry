package push

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lonelyman0108/ferry/internal/config"
	"github.com/lonelyman0108/ferry/internal/dest"
	"github.com/lonelyman0108/ferry/internal/events"
	"github.com/lonelyman0108/ferry/internal/ingest"
	"github.com/lonelyman0108/ferry/internal/secret"
	"github.com/lonelyman0108/ferry/internal/source"
	"github.com/lonelyman0108/ferry/internal/store"
)

type fakeAdapter struct {
	pushes []dest.Push
	fail   error
}

func (f *fakeAdapter) Upsert(_ context.Context, p dest.Push) error {
	if f.fail != nil {
		return f.fail
	}
	f.pushes = append(f.pushes, p)
	return nil
}

func (f *fakeAdapter) Test(context.Context) error { return nil }

type env struct {
	t       *testing.T
	db      *store.Store
	ing     *ingest.Service
	push    *Service
	adapter *fakeAdapter
	sub     store.Subscription
	now     time.Time
	evs     []events.Event
}

func newEnv(t *testing.T, d source.Doc) *env {
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
	e := &env{t: t, db: st, now: time.Unix(1_800_000_000, 0), adapter: &fakeAdapter{}}
	bus := &events.Bus{}
	clock := func() time.Time { return e.now }
	e.ing = &ingest.Service{DB: st.DB, Box: box, Bus: bus, Now: clock}
	e.push = &Service{DB: st.DB, Box: box, Bus: bus, Now: clock, Adapter: func(store.Destination) (dest.Adapter, error) { return e.adapter, nil }}
	bus.Subscribe(e.push.OnEvent)
	bus.Subscribe(func(ev events.Event) { e.evs = append(e.evs, ev) })
	zero := 0
	d.MergeWindow = &zero
	d.Normalize()
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Create(context.Background(), st.DB, d); err != nil {
		t.Fatal(err)
	}
	e.sub = store.Subscription{Name: "p", Client: "surge", EndpointID: 1, TokenHash: "h", TokenPrefix: "p", TokenSealed: "s", AllSources: true, SourceKeys: "[]"}
	st.DB.Create(&e.sub)
	return e
}

func (e *env) bind(mode, rename string) store.Binding {
	d := store.Destination{Name: "ql-" + mode, Kind: dest.KindQinglong, Config: "x", Enabled: true}
	e.db.DB.Create(&d)
	b := store.Binding{SourceID: 1, DestinationID: d.ID, Mode: mode, Rename: rename, Enabled: true}
	if err := e.db.DB.Create(&b).Error; err != nil {
		e.t.Fatal(err)
	}
	return b
}

func (e *env) sync(fields map[string]string) {
	e.t.Helper()
	if _, err := e.ing.Sync(context.Background(), e.sub, ingest.SyncInput{Source: e.srcKey(), Fields: fields}); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) srcKey() string {
	var s store.Source
	e.db.DB.First(&s)
	return s.Key
}

func (e *env) run() int {
	e.t.Helper()
	n, err := e.push.ProcessDue(context.Background())
	if err != nil {
		e.t.Fatal(err)
	}
	return n
}

func ninebot(t *testing.T) source.Doc {
	for _, ex := range source.Examples() {
		if ex.Key == "ninebot" {
			d := ex.Doc
			d.Probe = nil
			d.Fields[0].Type, d.Fields[0].MinLen = "text", 0
			return d
		}
	}
	t.Fatal("没有九号示例")
	return source.Doc{}
}

func TestPerIdentityWithRename(t *testing.T) {
	e := newEnv(t, ninebot(t))
	e.bind(store.ModePerIdentity, `{"NINEBOT_AUTHORIZATION":"TEMP_NINEBOT_AUTHORIZATION","NINEBOT_DEVICE_ID":"TEMP_NINEBOT_DEVICE_ID"}`)

	e.sync(map[string]string{"authorization": "tok-1", "device_id": "DEV-1"})
	var queued int64
	e.db.DB.Model(&store.PushJob{}).Where("status = ?", store.PushQueued).Count(&queued)
	if queued != 1 {
		t.Fatalf("值更新后应排 1 个任务，实际 %d", queued)
	}
	e.sync(map[string]string{"authorization": "tok-2"})
	e.db.DB.Model(&store.PushJob{}).Where("status = ?", store.PushQueued).Count(&queued)
	if queued != 1 {
		t.Errorf("排队中的任务应合并，实际 %d", queued)
	}
	if e.run() != 1 {
		t.Fatal("应执行 1 个任务")
	}
	p := e.adapter.pushes[0]
	want := map[string]string{"TEMP_NINEBOT_AUTHORIZATION": "tok-2", "TEMP_NINEBOT_DEVICE_ID": "DEV-1"}
	if len(p.Vars) != 2 {
		t.Fatalf("变量 = %+v", p.Vars)
	}
	for _, v := range p.Vars {
		if want[v.Name] != v.Value || v.Remarks != "ferry:ninebot:default" {
			t.Errorf("变量 %+v 不对（执行时应读最新值）", v)
		}
	}
	var job store.PushJob
	e.db.DB.First(&job)
	if job.Status != store.PushSuccess || job.Detail != "TEMP_NINEBOT_AUTHORIZATION,TEMP_NINEBOT_DEVICE_ID" {
		t.Errorf("任务 = %+v", job)
	}
}

func TestTriggerFollowsCause(t *testing.T) {
	e := newEnv(t, ninebot(t))
	e.bind(store.ModePerIdentity, "{}")
	e.sync(map[string]string{"authorization": "tok-1", "device_id": "DEV-1"})
	e.run()
	e.sync(map[string]string{"authorization": "tok-2"})
	e.run()
	var vs []store.Version
	e.db.DB.Order("id").Find(&vs)
	var first store.Version
	for _, v := range vs {
		if got, _ := e.ing.Reveal(v); got == "tok-1" {
			first = v
		}
	}
	if err := e.ing.Activate(context.Background(), first.ID); err != nil {
		t.Fatal(err)
	}
	var jobs []store.PushJob
	e.db.DB.Order("id").Find(&jobs)
	got := make([]string, 0, len(jobs))
	for _, j := range jobs {
		got = append(got, j.Trigger)
	}
	if strings.Join(got, ",") != "value.updated,value.updated,activate" {
		t.Errorf("触发原因 = %v，期望回传生效记 value.updated、恢复旧值记 activate", got)
	}
}

func jdDoc() source.Doc {
	return source.Doc{
		Key: "jd", Name: "京东", IdentityField: "pt_pin",
		Matchers: []source.MatcherDoc{{Name: "api", Host: "*.jd.com"}},
		Fields: []source.FieldDoc{
			{Key: "pt_key", In: "cookie", Path: "pt_key", Sensitive: true},
			{Key: "pt_pin", In: "cookie", Path: "pt_pin"},
			{Key: "extra", In: "cookie", Path: "extra"},
		},
		Outputs: []source.OutputDoc{
			{Name: "JD_COOKIE", Template: "pt_key={{.pt_key}};pt_pin={{.pt_pin}};"},
			{Name: "JD_EXTRA", Template: "{{.extra}}"},
		},
	}
}

func TestJoinedSkipsIncompleteIdentities(t *testing.T) {
	e := newEnv(t, jdDoc())
	e.bind(store.ModeJoined, "{}")
	e.sync(map[string]string{"pt_key": "K1", "pt_pin": "alice", "extra": "E1"})
	e.sync(map[string]string{"pt_key": "K2", "pt_pin": "bob"})
	e.sync(map[string]string{"pt_key": "K3", "pt_pin": "carol", "extra": "E3"})
	e.run()
	last := e.adapter.pushes[len(e.adapter.pushes)-1]
	got := map[string]string{}
	for _, v := range last.Vars {
		got[v.Name] = v.Value
		if v.Remarks != "ferry:jd" {
			t.Errorf("合成一条时备注 = %q", v.Remarks)
		}
	}
	if got["JD_COOKIE"] != "pt_key=K1;pt_pin=alice;&pt_key=K3;pt_pin=carol;" || got["JD_EXTRA"] != "E1&E3" {
		t.Errorf("缺 extra 的 bob 应整个跳过，两个变量按身份对齐：%v", got)
	}
}

func TestRetryThenFail(t *testing.T) {
	e := newEnv(t, ninebot(t))
	e.bind(store.ModePerIdentity, "{}")
	e.adapter.fail = errors.New("连接青龙失败")
	e.sync(map[string]string{"device_id": "DEV-1"})
	for i := 1; i <= MaxAttempts; i++ {
		if e.run() != 1 {
			t.Fatalf("第 %d 次应执行", i)
		}
		var job store.PushJob
		e.db.DB.First(&job)
		if i < MaxAttempts {
			wait := RetryBase << (i - 1)
			if job.Status != store.PushQueued || job.Attempts != i || !job.NextAttemptAt.Equal(e.now.Add(wait)) {
				t.Fatalf("第 %d 次失败后 = %+v，期望等 %v", i, job, wait)
			}
			if e.run() != 0 {
				t.Fatal("没到重试时间不应执行")
			}
			e.now = e.now.Add(wait)
		} else if job.Status != store.PushFailed {
			t.Fatalf("试满 %d 次应失败：%+v", MaxAttempts, job)
		}
	}
	last := e.evs[len(e.evs)-1]
	if last.Type != events.PushFailed || last.SourceKey != "ninebot" {
		t.Errorf("应发出推送失败事件：%+v", last)
	}
}

func TestSkipAndStuck(t *testing.T) {
	e := newEnv(t, ninebot(t))
	b := e.bind(store.ModePerIdentity, "{}")
	e.sync(map[string]string{"device_id": "DEV-1"})
	e.db.DB.Model(&store.Destination{}).Where("id = ?", b.DestinationID).Update("enabled", false)
	e.run()
	var job store.PushJob
	e.db.DB.Last(&job)
	if job.Status != store.PushSkipped || len(e.adapter.pushes) != 0 {
		t.Errorf("目的地停用时应跳过：%+v", job)
	}

	ident := uint(1)
	stuck := store.PushJob{BindingID: b.ID, IdentityID: &ident, Status: store.PushRunning, NextAttemptAt: e.now.Add(-time.Hour), Trigger: "manual"}
	e.db.DB.Create(&stuck)
	e.db.DB.Model(&store.Destination{}).Where("id = ?", b.DestinationID).Update("enabled", true)
	if e.run() != 1 {
		t.Error("卡在运行中的任务应重新排队并执行")
	}

	// 来源没有输出变量：跳过，原因写清楚。
	e.db.DB.Where("source_id = ?", b.SourceID).Delete(&store.Output{})
	e.sync(map[string]string{"device_id": "DEV-2"})
	e.run()
	var last store.PushJob
	e.db.DB.Last(&last)
	if last.Status != store.PushSkipped || last.LastError != "来源未配置输出变量" {
		t.Errorf("来源没有输出变量时应跳过并说明：%+v", last)
	}
}
