package ingest

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/lonelyman0108/ferry/internal/events"
	"github.com/lonelyman0108/ferry/internal/source"
	"github.com/lonelyman0108/ferry/internal/store"
)

func zeroWindow(d source.Doc) source.Doc {
	z := 0
	d.MergeWindow = &z
	return d
}

func versionOf(t *testing.T, e *env, value string) store.Version {
	t.Helper()
	var vs []store.Version
	e.svc.DB.Order("id").Find(&vs)
	for _, v := range vs {
		if got, _ := e.svc.Reveal(v); got == value {
			return v
		}
	}
	t.Fatalf("找不到值 %q 的版本", value)
	return store.Version{}
}

func TestReview(t *testing.T) {
	d := zeroWindow(ninebot(t))
	d.Review = source.ReviewManual
	e := newEnv(t, d)
	ctx := context.Background()
	e.sync("ninebot", map[string]string{"device_id": "DEV-001"})
	e.sync("ninebot", map[string]string{"device_id": "DEV-002"})
	first, second := versionOf(t, e, "DEV-001"), versionOf(t, e, "DEV-002")

	if err := e.svc.Approve(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if cur := e.current(DefaultIdentity); cur["device_id"] != "DEV-001" {
		t.Errorf("通过后应成为当前值：%v", cur)
	}
	if last := e.evs[len(e.evs)-1]; last.Type != events.ValueUpdated || last.Cause != events.CauseApprove {
		t.Errorf("事件 = %+v", last)
	}
	if err := e.svc.Reject(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	var st *StateError
	if err := e.svc.Approve(ctx, second.ID); !errors.As(err, &st) {
		t.Errorf("已驳回的版本不能再通过：%v", err)
	}
	if err := e.svc.Approve(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("不存在的版本：%v", err)
	}
}

func TestActivateRollback(t *testing.T) {
	e := newEnv(t, zeroWindow(ninebot(t)))
	ctx := context.Background()
	e.sync("ninebot", map[string]string{"device_id": "DEV-001"})
	e.sync("ninebot", map[string]string{"device_id": "DEV-002"})
	old := versionOf(t, e, "DEV-001")
	var st *StateError
	if err := e.svc.Activate(ctx, versionOf(t, e, "DEV-002").ID); !errors.As(err, &st) {
		t.Errorf("已经是当前值时应报错：%v", err)
	}
	if err := e.svc.Activate(ctx, old.ID); err != nil {
		t.Fatal(err)
	}
	if cur := e.current(DefaultIdentity); cur["device_id"] != "DEV-001" {
		t.Errorf("回滚后当前值 = %v", cur)
	}
	if last := e.evs[len(e.evs)-1]; last.Type != events.ValueUpdated || last.Cause != events.CauseActivate {
		t.Errorf("回滚应发出更新事件（目的地据此重新推送）：%+v", last)
	}
	e.sync("ninebot", map[string]string{"device_id": "x"})
	if err := e.svc.Activate(ctx, versionOf(t, e, "x").ID); !errors.As(err, &st) {
		t.Errorf("驳回的版本不能设为当前值：%v", err)
	}
}

func TestCleanup(t *testing.T) {
	e := newEnv(t, zeroWindow(ninebot(t)))
	start := e.now
	// 70 个版本，每天一个：前 70 天的都比 180 天的截止线早。
	for i := 0; i < 70; i++ {
		e.now = start.Add(time.Duration(i) * 24 * time.Hour)
		e.sync("ninebot", map[string]string{"device_id": fmt.Sprintf("DEV-%03d", i)})
	}
	e.now = start.Add(400 * 24 * time.Hour)
	n, err := e.svc.Cleanup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var left int64
	e.svc.DB.Model(&store.Version{}).Count(&left)
	if n != 20 || left != KeepVersions {
		t.Errorf("删除 %d 个、剩 %d 个；期望删 20、剩 %d", n, left, KeepVersions)
	}
	if cur := e.current(DefaultIdentity); cur["device_id"] != "DEV-069" {
		t.Errorf("当前值不能被清理：%v", cur)
	}
	var oldest store.Version
	e.svc.DB.Order("id").First(&oldest)
	if v, _ := e.svc.Reveal(oldest); v != "DEV-020" {
		t.Errorf("应删掉最旧的那些，最旧的剩下 %q", v)
	}

	e2 := newEnv(t, zeroWindow(ninebot(t)))
	for i := 0; i < 60; i++ {
		e2.now = e2.now.Add(time.Hour)
		e2.sync("ninebot", map[string]string{"device_id": fmt.Sprintf("DEV-%03d", i)})
	}
	if n, _ := e2.svc.Cleanup(context.Background()); n != 0 {
		t.Errorf("180 天内的版本都要保留，却删了 %d 个", n)
	}
}

func TestCleanupOrphans(t *testing.T) {
	jd := zeroWindow(source.Doc{
		Key: "jd", Name: "京东", IdentityField: "pt_pin",
		Matchers: []source.MatcherDoc{{Name: "api", Host: "*.jd.com"}},
		Fields: []source.FieldDoc{
			{Key: "pt_key", In: "cookie", Path: "pt_key"},
			{Key: "pt_pin", In: "cookie", Path: "pt_pin"},
		},
	})
	e := newEnv(t, zeroWindow(ninebot(t)), jd)
	e.sync("ninebot", map[string]string{"authorization": jwtToken(e.now.Add(time.Hour)), "device_id": "DEV-001"})
	e.sync("jd", map[string]string{"pt_key": "AAJ-1", "pt_pin": "jd_alice"})
	ctx := context.Background()
	db := e.svc.DB

	var jdSrc, nbSrc store.Source
	db.Where(map[string]any{"key": "jd"}).First(&jdSrc)
	db.Where(map[string]any{"key": "ninebot"}).First(&nbSrc)
	b := store.Binding{SourceID: jdSrc.ID, DestinationID: 1, Mode: store.ModeJoined, Rename: "{}", Enabled: true}
	if err := db.Create(&b).Error; err != nil {
		t.Fatal(err)
	}
	for _, st := range []string{store.PushQueued, store.PushSuccess} {
		if err := db.Create(&store.PushJob{BindingID: b.ID, Status: st, NextAttemptAt: e.now, Trigger: "manual"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	// 直接删来源这一行，不走 source.Delete：身份、版本这些表没有外键，会留下来。
	if err := db.Delete(&store.Source{}, jdSrc.ID).Error; err != nil {
		t.Fatal(err)
	}
	// 九号去掉 device_id 字段，这个字段的版本也没有归属了。
	nb, err := source.Load(ctx, db, nbSrc.ID)
	if err != nil {
		t.Fatal(err)
	}
	d := source.FromModel(nb)
	d.Fields, d.Outputs = d.Fields[:1], d.Outputs[:1]
	if _, err := source.Update(ctx, db, nb.ID, d); err != nil {
		t.Fatal(err)
	}

	n, err := e.svc.Cleanup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("删除 %d 个版本，期望 3 个（京东两个、九号 device_id 一个）", n)
	}
	count := func(m any, where ...any) int64 {
		var c int64
		q := db.Model(m)
		if len(where) > 0 {
			q = q.Where(where[0], where[1:]...)
		}
		q.Count(&c)
		return c
	}
	if v, i := count(&store.Version{}), count(&store.Identity{}); v != 1 || i != 1 {
		t.Errorf("剩 %d 个版本、%d 个身份，期望九号的 authorization 和默认身份各 1 个", v, i)
	}
	if c := count(&store.Batch{}, "source_id = ?", jdSrc.ID); c != 0 {
		t.Errorf("已删来源的批次还剩 %d 个", c)
	}
	if c := count(&store.Binding{}); c != 0 {
		t.Errorf("已删来源的映射还剩 %d 个", c)
	}
	if q, s := count(&store.PushJob{}, "status = ?", store.PushQueued), count(&store.PushJob{}, "status = ?", store.PushSuccess); q != 0 || s != 1 {
		t.Errorf("排队中的任务剩 %d 个（期望 0），推送记录剩 %d 个（期望 1）", q, s)
	}
}

func TestCheckExpiring(t *testing.T) {
	d := zeroWindow(ninebot(t))
	d.Fields[0].MinLen = 0
	e := newEnv(t, d)
	e.sync("ninebot", map[string]string{"authorization": jwtToken(e.now.Add(30 * time.Hour))})
	if n, _ := e.svc.CheckExpiring(context.Background()); n != 0 {
		t.Errorf("30 小时后过期，不该提醒：%d", n)
	}
	e.now = e.now.Add(7 * time.Hour)
	if n, _ := e.svc.CheckExpiring(context.Background()); n != 1 {
		t.Fatalf("进入 24 小时内应提醒 1 次：%d", n)
	}
	last := e.evs[len(e.evs)-1]
	if last.Type != events.ValueExpiring || last.ExpiresAt == nil || last.Fields[0] != "authorization" {
		t.Errorf("事件 = %+v", last)
	}
	if n, _ := e.svc.CheckExpiring(context.Background()); n != 0 {
		t.Errorf("同一个版本只提醒一次：%d", n)
	}
}
