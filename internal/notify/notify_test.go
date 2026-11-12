package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lonelyman0108/ferry/internal/config"
	"github.com/lonelyman0108/ferry/internal/events"
	"github.com/lonelyman0108/ferry/internal/store"
)

type recorder struct {
	mu   sync.Mutex
	reqs []recorded
}

type recorded struct {
	path string
	body map[string]any
	sig  string
}

func (r *recorder) server(t *testing.T) *httptest.Server {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		var body map[string]any
		_ = json.Unmarshal(b, &body)
		r.mu.Lock()
		r.reqs = append(r.reqs, recorded{path: req.URL.Path, body: body, sig: req.Header.Get("X-Ferry-Signature")})
		r.mu.Unlock()
		if strings.Contains(req.URL.Path, "/botbad/") {
			_, _ = w.Write([]byte(`{"ok":false,"description":"Unauthorized"}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":200,"ok":true}`))
	}))
	t.Cleanup(ts.Close)
	return ts
}

func TestSend(t *testing.T) {
	rec := &recorder{}
	ts := rec.server(t)
	ctx := context.Background()
	m := Message{Event: "x", Title: "标题", Body: "正文", Link: "surge:///install-module?url=x", At: time.Now()}

	if err := Send(ctx, nil, KindBark, Config{Server: ts.URL + "/", DeviceKey: "dk"}, m); err != nil {
		t.Fatal(err)
	}
	b := rec.reqs[0]
	if b.path != "/push" || b.body["device_key"] != "dk" || b.body["url"] != m.Link || b.body["group"] != "Ferry" {
		t.Errorf("Bark 请求 = %+v", b)
	}

	if err := Send(ctx, nil, KindTelegram, Config{APIBase: ts.URL, BotToken: "123:abc", ChatID: "42"}, m); err != nil {
		t.Fatal(err)
	}
	tg := rec.reqs[1]
	if tg.path != "/bot123:abc/sendMessage" || tg.body["chat_id"] != "42" || !strings.Contains(tg.body["text"].(string), m.Link) {
		t.Errorf("Telegram 请求 = %+v", tg)
	}
	if err := Send(ctx, nil, KindTelegram, Config{APIBase: ts.URL, BotToken: "bad", ChatID: "42"}, m); err == nil || !strings.Contains(err.Error(), "Unauthorized") {
		t.Errorf("Telegram 返回 ok=false 时应报错：%v", err)
	}

	if err := Send(ctx, nil, KindWebhook, Config{URL: ts.URL + "/hook", Secret: "s"}, m); err != nil {
		t.Fatal(err)
	}
	if wh := rec.reqs[3]; wh.body["title"] != "标题" || !strings.HasPrefix(wh.sig, "sha256=") {
		t.Errorf("Webhook 请求 = %+v", wh)
	}

	err := Send(ctx, nil, KindTelegram, Config{APIBase: "http://127.0.0.1:1", BotToken: "999:secret-bot-token", ChatID: "1"}, m)
	if err == nil || strings.Contains(err.Error(), "secret-bot-token") {
		t.Errorf("连不上时不应带出 bot token：%v", err)
	}
}

func TestValidate(t *testing.T) {
	bad := map[string]Config{
		KindBark:     {Server: "x", DeviceKey: "k"},
		KindTelegram: {BotToken: "t"},
		KindWebhook:  {URL: "ftp://x"},
		"sms":        {},
	}
	for kind, c := range bad {
		if Validate(kind, c) == nil {
			t.Errorf("%s %+v 应当不合法", kind, c)
		}
	}
	if Validate(KindBark, Config{DeviceKey: "k"}) != nil {
		t.Error("Bark 服务器可以不填")
	}
}

func TestFormatHasNoValues(t *testing.T) {
	exp := time.Now().Add(20 * time.Hour)
	cases := []events.Event{
		{Type: events.ValueUpdated, SourceName: "九号出行", IdentityKey: "default", Fields: []string{"authorization", "device_id"}},
		{Type: events.ValueRejected, SourceName: "九号出行", Fields: []string{"authorization"}, Detail: "JWT 已过期"},
		{Type: events.ValueExpiring, SourceName: "九号出行", IdentityKey: "default", Fields: []string{"authorization"}, ExpiresAt: &exp},
		{Type: events.MitmChanged, Subject: "iPhone", Link: "surge:///install-module?url=x"},
		{Type: events.PushFailed, SourceKey: "ninebot", Detail: "青龙：连接失败"},
	}
	for _, e := range cases {
		m := Format(e)
		if m.Title == "" || m.Body == "" {
			t.Errorf("%s 文案为空：%+v", e.Type, m)
		}
	}
	if m := Format(cases[2]); !strings.Contains(m.Body, "过期") {
		t.Errorf("过期提醒 = %+v", m)
	}
	if m := Format(cases[3]); m.Link == "" || !strings.Contains(m.Body, "iPhone") {
		t.Errorf("MITM 变化提醒要带安装链接：%+v", m)
	}
}

func TestDispatchRules(t *testing.T) {
	rec := &recorder{}
	ts := rec.server(t)
	st, err := store.Open(config.Database{Driver: config.DriverSQLite, DSN: filepath.Join(t.TempDir(), "f.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	_ = st.Migrate()
	cfgs := map[uint]Config{}
	mk := func(name string, enabled bool) store.Channel {
		c := store.Channel{Name: name, Kind: KindWebhook, Config: "x", Enabled: enabled}
		st.DB.Create(&c)
		cfgs[c.ID] = Config{URL: ts.URL + "/" + name}
		return c
	}
	a, b, off := mk("a", true), mk("b", true), mk("off", true)
	st.DB.Model(&off).Update("enabled", false)
	rule := func(evs, srcs string, chans string) {
		st.DB.Create(&store.NotifyRule{Name: "r", Events: evs, SourceKeys: srcs, ChannelIDs: chans, Enabled: true})
	}
	rule(`["push.failed","value.rejected"]`, `[]`, `[`+itoa(a.ID)+`,`+itoa(off.ID)+`]`)
	rule(`["value.rejected"]`, `["ninebot"]`, `[`+itoa(a.ID)+`,`+itoa(b.ID)+`]`)
	st.DB.Create(&store.NotifyRule{Name: "停用", Events: `["value.updated"]`, SourceKeys: `[]`, ChannelIDs: `[` + itoa(a.ID) + `]`, Enabled: false})

	var audits []string
	svc := &Service{DB: st.DB, Decrypt: func(c store.Channel) (Config, error) { return cfgs[c.ID], nil }, Audit: func(action, target, detail string) {
		audits = append(audits, action+" "+target+" "+detail)
	}}
	ctx := context.Background()
	svc.Dispatch(ctx, events.Event{Type: events.ValueRejected, SourceKey: "ninebot"})
	paths := map[string]int{}
	for _, r := range rec.reqs {
		paths[r.path]++
	}
	if paths["/a"] != 1 || paths["/b"] != 1 || paths["/off"] != 0 {
		t.Errorf("两条规则都命中时每个渠道只发一次、停用渠道不发：%v", paths)
	}
	rec.reqs = nil
	svc.Dispatch(ctx, events.Event{Type: events.ValueRejected, SourceKey: "jd"})
	if len(rec.reqs) != 1 || rec.reqs[0].path != "/a" {
		t.Errorf("来源过滤：%+v", rec.reqs)
	}
	rec.reqs = nil
	svc.Dispatch(ctx, events.Event{Type: events.ValueUpdated, SourceKey: "ninebot"})
	if len(rec.reqs) != 0 {
		t.Errorf("停用的规则不应发：%+v", rec.reqs)
	}
	if len(audits) != 3 || !strings.HasSuffix(audits[0], " ok") {
		t.Errorf("审计 = %v", audits)
	}
}

func itoa(n uint) string { b, _ := json.Marshal(n); return string(b) }

func TestNormalizeBarkURL(t *testing.T) {
	cases := map[string][2]string{
		"https://api.day.app/abcKEY":              {"https://api.day.app", "abcKEY"},
		"https://notify.example.com/bark/abcKEY/": {"https://notify.example.com/bark", "abcKEY"},
	}
	for in, want := range cases {
		c := Normalize(KindBark, Config{URL: in})
		if c.Server != want[0] || c.DeviceKey != want[1] || c.URL != "" {
			t.Errorf("%s => %+v", in, c)
		}
	}
}
