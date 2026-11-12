package server

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lonelyman0108/ferry/internal/ingest"
	"github.com/lonelyman0108/ferry/internal/store"
	"github.com/lonelyman0108/ferry/internal/transport"
)

// sealedPhone 模拟走加密通道的手机。
type sealedPhone struct {
	t    *testing.T
	h    *harness
	kid  string
	keys *transport.Keys
}

// sealedSetup 建好九号来源和一个开了加密的 Surge 订阅，返回订阅视图、明文令牌和手机。
func sealedSetup(t *testing.T, h *harness) (subscriptionView, string, *sealedPhone) {
	t.Helper()
	token := phoneSetup(t, h)
	var subs []subscriptionView
	h.do("GET", "/api/v1/subscriptions", nil, &subs)
	sv := subs[0]
	in := map[string]any{"name": sv.Name, "client": sv.Client, "endpoint_id": sv.EndpointID, "all_sources": true, "transport_mode": "sealed"}
	if code := h.do("PUT", "/api/v1/subscriptions/"+itoa(sv.ID), in, &sv); code != 200 {
		t.Fatalf("开启加密 = %d", code)
	}
	if sv.Transport.Mode != store.TransportSealed || len(sv.Transport.KID) != 16 || sv.Transport.VerifiedAt != nil {
		t.Fatalf("加密状态 = %+v", sv.Transport)
	}
	return sv, token, &sealedPhone{t: t, h: h, kid: sv.Transport.KID, keys: revealKeys(t, h, sv.ID)}
}

func revealKeys(t *testing.T, h *harness, id uint) *transport.Keys {
	t.Helper()
	var out map[string]string
	if code := h.do("POST", "/api/v1/subscriptions/"+itoa(id)+"/transport-key/reveal", map[string]any{}, &out); code != 200 {
		t.Fatalf("查看传输密钥 = %d", code)
	}
	keys, err := transport.ParseKey(out["key"])
	if err != nil {
		t.Fatal(err)
	}
	return keys
}

// seal 构造一条加密请求，返回报文和它的 nonce。
func (p *sealedPhone) seal(op string, ts time.Time, body any, etag string) (string, [transport.NonceSize]byte) {
	p.t.Helper()
	req := map[string]any{"v": 1, "op": op, "ts": ts.UnixMilli()}
	if body != nil {
		req["body"] = body
	}
	if etag != "" {
		req["etag"] = etag
	}
	plain, _ := json.Marshal(req)
	nonce, err := transport.NewNonce()
	if err != nil {
		p.t.Fatal(err)
	}
	return p.keys.Seal(transport.Up, nonce, plain), nonce
}

// post 发送原始报文，返回 HTTP 状态码和正文。
func (p *sealedPhone) post(msg string) (int, string) {
	p.t.Helper()
	req, _ := http.NewRequest("POST", p.h.ts.URL+"/api/v1/sealed", strings.NewReader(msg))
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set(kidHeader, p.kid)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		p.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

type openedReply struct {
	V      int             `json:"v"`
	Re     string          `json:"re"`
	Status int             `json:"status"`
	ETag   string          `json:"etag"`
	Time   int64           `json:"time"`
	Body   json.RawMessage `json:"body"`
	raw    string
}

// open 解开回复并核对它对应的是哪条请求。
func (p *sealedPhone) open(code int, body string, nonce [transport.NonceSize]byte) openedReply {
	p.t.Helper()
	if code != 200 {
		p.t.Fatalf("加密入口 HTTP 状态 = %d %s", code, body)
	}
	plain, _, err := p.keys.Open(transport.Down, body)
	if err != nil {
		p.t.Fatalf("解不开回复：%v", err)
	}
	if len(plain)%sealedPad != 0 {
		p.t.Errorf("回复明文长度 %d 没有补齐到 %d 的整数倍", len(plain), sealedPad)
	}
	var rep openedReply
	if err := json.Unmarshal(plain, &rep); err != nil {
		p.t.Fatal(err)
	}
	if rep.V != 1 || rep.Re != base64.RawURLEncoding.EncodeToString(nonce[:]) {
		p.t.Fatalf("回复没有绑定到请求：%+v", rep)
	}
	rep.raw = body
	return rep
}

// call 发一条请求并解开回复。
func (p *sealedPhone) call(op string, body any, etag string) openedReply {
	p.t.Helper()
	msg, nonce := p.seal(op, time.Now(), body, etag)
	code, out := p.post(msg)
	return p.open(code, out, nonce)
}

func syncBody(jwt, dev string) map[string]any {
	return map[string]any{"source": "ninebot", "matcher": "sign", "fields": map[string]string{"authorization": jwt, "device_id": dev}}
}

func TestSealedEndToEnd(t *testing.T) {
	h := ready(t)
	sv, _, phone := sealedSetup(t, h)

	if rep := phone.call("ping", nil, ""); rep.Status != 200 {
		t.Fatalf("ping = %+v", rep)
	}
	var got subscriptionView
	h.do("GET", "/api/v1/subscriptions/"+itoa(sv.ID), nil, &got)
	if got.Transport.VerifiedAt == nil || got.Transport.LastAt == nil {
		t.Errorf("成功解开后应记下密钥已填对：%+v", got.Transport)
	}

	rules := phone.call("rules", nil, "")
	if rules.Status != 200 || rules.ETag == "" || !strings.Contains(string(rules.Body), "ninebot") {
		t.Fatalf("拉规则 = %+v", rules)
	}
	if rep := phone.call("rules", nil, rules.ETag); rep.Status != 304 || rep.ETag != rules.ETag || len(rep.Body) != 0 {
		t.Errorf("规则没变 = %+v", rep)
	}

	jwt := testJWT(time.Now().Add(48 * time.Hour))
	var res ingest.SyncResult
	rep := phone.call("sync", syncBody(jwt, "DEV-SEALED"), "")
	if err := json.Unmarshal(rep.Body, &res); err != nil || rep.Status != 200 || res.Result != ingest.ResultAccepted {
		t.Fatalf("加密回传 = %+v %+v", rep, res)
	}
	if strings.Contains(rep.raw, "DEV-SEALED") || strings.Contains(rep.raw, "收到") {
		t.Error("回复里不应出现明文")
	}
	rep = phone.call("sync", syncBody(jwt, "DEV-SEALED"), "")
	if _ = json.Unmarshal(rep.Body, &res); res.Result != ingest.ResultUnchanged {
		t.Errorf("同样的值再传 = %+v", res)
	}
	var idents []identityView
	h.do("GET", "/api/v1/identities?source=ninebot", nil, &idents)
	if len(idents) != 1 {
		t.Fatalf("身份 = %+v", idents)
	}

	if rep := phone.call("sync", map[string]any{"source": "ninebot", "fields": map[string]string{"nope": "x"}}, ""); rep.Status != 400 {
		t.Errorf("未声明字段 = %+v", rep)
	}
	if rep := phone.call("sync", map[string]any{"source": "nope", "fields": map[string]string{"a": "b"}}, ""); rep.Status != 404 {
		t.Errorf("未知来源 = %+v", rep)
	}
	if rep := phone.call("drop", nil, ""); rep.Status != 400 {
		t.Errorf("未知操作 = %+v", rep)
	}
}

func TestSealedRejectsTamperingAndReplay(t *testing.T) {
	h := ready(t)
	_, _, phone := sealedSetup(t, h)

	msg, nonce := phone.seal("ping", time.Now(), nil, "")
	raw, _ := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(msg, "f1."))
	for _, i := range []int{0, transport.NonceSize, len(raw) - 1} {
		b := append([]byte(nil), raw...)
		b[i] ^= 1
		if code, _ := phone.post("f1." + base64.RawURLEncoding.EncodeToString(b)); code != 401 {
			t.Errorf("改动第 %d 个字节 = %d，期望 401", i, code)
		}
	}
	for name, m := range map[string]string{"空正文": "", "缺前缀": strings.TrimPrefix(msg, "f1."), "不是 base64": "f1.***"} {
		if code, _ := phone.post(m); code != 401 {
			t.Errorf("%s = %d，期望 401", name, code)
		}
	}

	code, body := phone.post(msg)
	rep := phone.open(code, body, nonce)
	if rep.Status != 200 {
		t.Fatalf("原报文 = %+v", rep)
	}
	code, body = phone.post(msg)
	if rep := phone.open(code, body, nonce); rep.Status != 409 {
		t.Errorf("原样重放 = %+v，期望 409", rep)
	}

	// 把平台的回复当请求发回去：方向不同的子密钥解不开。
	if code, _ := phone.post(body); code != 401 {
		t.Errorf("反射回复 = %d，期望 401", code)
	}

	for _, skew := range []time.Duration{-10 * time.Minute, 10 * time.Minute} {
		msg, nonce := phone.seal("ping", time.Now().Add(skew), nil, "")
		code, body := phone.post(msg)
		if rep := phone.open(code, body, nonce); rep.Status != 409 || rep.Time == 0 {
			t.Errorf("时间偏差 %v = %+v，期望 409 并带平台时间", skew, rep)
		}
	}

	wrong := *phone
	other, _ := transport.GenerateKey()
	wrong.keys, _ = transport.ParseKey(other)
	msg, _ = wrong.seal("ping", time.Now(), nil, "")
	if code, _ := wrong.post(msg); code != 401 {
		t.Errorf("错误的密钥 = %d，期望 401", code)
	}
	unknown := *phone
	unknown.kid = "aaaaaaaaaaaaaaaa"
	msg, _ = phone.seal("ping", time.Now(), nil, "")
	if code, _ := unknown.post(msg); code != 401 {
		t.Errorf("不存在的编号 = %d，期望 401", code)
	}

	var logs []store.AuditLog
	h.do("GET", "/api/v1/audit?limit=200", nil, &logs)
	n := 0
	for _, l := range logs {
		if l.Action == "transport.decrypt_failed" {
			n++
		}
	}
	// 同一编号限频只记一条；不存在的编号按 IP 另记一条。
	if n != 2 {
		t.Errorf("解不开的审计 = %d 条，期望 2", n)
	}
}

func TestSealedBlocksPlaintextDowngrade(t *testing.T) {
	h := ready(t)
	sv, token, phone := sealedSetup(t, h)
	plain := &harness{t: t, srv: h.srv, ts: h.ts, client: http.DefaultClient}
	if code := plain.do("GET", "/api/v1/rules", nil, nil, subscriptionHeader, token); code != 401 {
		t.Errorf("加密订阅走明文拉规则 = %d，期望 401", code)
	}
	if code := plain.do("POST", "/api/v1/sync", syncBody(testJWT(time.Now().Add(time.Hour)), "D"), nil, subscriptionHeader, token); code != 401 {
		t.Errorf("加密订阅走明文回传 = %d，期望 401", code)
	}

	resp, err := http.Get(sv.Links.SubscriptionURL)
	if err != nil {
		t.Fatal(err)
	}
	file, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(file), "kid="+phone.kid) || !strings.Contains(string(file), "mode=sealed") || strings.Contains(string(file), token) {
		t.Errorf("加密订阅的脚本参数应只带编号、不带令牌：\n%s", file)
	}

	// 切回明文：明文接口恢复；已加密的手机收到加密的 410，知道要改回明文。
	in := map[string]any{"name": sv.Name, "client": sv.Client, "endpoint_id": sv.EndpointID, "all_sources": true, "transport_mode": "plain"}
	h.do("PUT", "/api/v1/subscriptions/"+itoa(sv.ID), in, nil)
	if code := plain.do("GET", "/api/v1/rules", nil, nil, subscriptionHeader, token); code != 200 {
		t.Errorf("切回明文后拉规则 = %d", code)
	}
	if rep := phone.call("ping", nil, ""); rep.Status != 410 {
		t.Errorf("切回明文后的加密请求 = %+v，期望 410", rep)
	}
	var out map[string]string
	if code := h.do("POST", "/api/v1/subscriptions/"+itoa(sv.ID)+"/transport-key/reveal", map[string]any{}, &out); code != 409 {
		t.Errorf("明文订阅查看密钥 = %d，期望 409", code)
	}
	// 再切回加密时沿用原来的密钥，手机上不用重填。
	in["transport_mode"] = "sealed"
	h.do("PUT", "/api/v1/subscriptions/"+itoa(sv.ID), in, nil)
	if rep := phone.call("ping", nil, ""); rep.Status != 200 {
		t.Errorf("再切回加密 = %+v", rep)
	}
}

func TestTransportKeyRotation(t *testing.T) {
	h := ready(t)
	sv, _, phone := sealedSetup(t, h)
	phone.call("ping", nil, "")

	var rotated subscriptionView
	if code := h.do("POST", "/api/v1/subscriptions/"+itoa(sv.ID)+"/transport-key/rotate", map[string]any{}, &rotated); code != 200 {
		t.Fatalf("更换密钥 = %d", code)
	}
	if rotated.Transport.KID != phone.kid || rotated.Transport.VerifiedAt != nil {
		t.Errorf("换密钥后编号不变、待重新验证：%+v", rotated.Transport)
	}
	msg, _ := phone.seal("ping", time.Now(), nil, "")
	if code, _ := phone.post(msg); code != 401 {
		t.Errorf("旧密钥 = %d，期望 401", code)
	}
	phone.keys = revealKeys(t, h, sv.ID)
	if rep := phone.call("ping", nil, ""); rep.Status != 200 {
		t.Errorf("新密钥 = %+v", rep)
	}

	var logs []store.AuditLog
	h.do("GET", "/api/v1/audit?limit=50", nil, &logs)
	seen := map[string]bool{}
	for _, l := range logs {
		seen[l.Action] = true
	}
	for _, a := range []string{"subscription.transport_mode", "subscription.transport_key_reveal", "subscription.transport_key_rotate"} {
		if !seen[a] {
			t.Errorf("缺少审计 %s", a)
		}
	}
}

func TestSealedSubscriptionValidation(t *testing.T) {
	h := ready(t)
	var ep endpointView
	h.do("POST", "/api/v1/endpoints", map[string]string{"name": "lan", "base_url": h.ts.URL}, &ep)
	h.do("POST", "/api/v1/examples/ninebot/import", map[string]any{}, nil)
	if code := h.do("POST", "/api/v1/subscriptions", map[string]any{"name": "qx", "client": "qx", "endpoint_id": ep.ID, "all_sources": true, "transport_mode": "sealed"}, nil); code != 400 {
		t.Errorf("QX 开加密 = %d，期望 400", code)
	}
	if code := h.do("POST", "/api/v1/subscriptions", map[string]any{"name": "x", "client": "surge", "endpoint_id": ep.ID, "all_sources": true, "transport_mode": "tls"}, nil); code != 400 {
		t.Errorf("未知传输方式 = %d，期望 400", code)
	}
	var sv subscriptionView
	if code := h.do("POST", "/api/v1/subscriptions", map[string]any{"name": "loon", "client": "loon", "endpoint_id": ep.ID, "all_sources": true, "transport_mode": "sealed"}, &sv); code != 201 {
		t.Fatalf("新建即开加密 = %d", code)
	}
	if sv.Transport.Mode != store.TransportSealed || sv.Transport.KID == "" {
		t.Errorf("新建的加密订阅 = %+v", sv.Transport)
	}
	revealKeys(t, h, sv.ID)
	var plainSub subscriptionView
	h.do("POST", "/api/v1/subscriptions", map[string]any{"name": "plain", "client": "surge", "endpoint_id": ep.ID, "all_sources": true}, &plainSub)
	if plainSub.Transport.Mode != store.TransportPlain || plainSub.Transport.KID != "" {
		t.Errorf("不填传输方式时应为明文：%+v", plainSub.Transport)
	}
}

func TestCleanupNonces(t *testing.T) {
	h := ready(t)
	db := h.srv.store().DB
	now := time.Now()
	db.Create(&store.TransportNonce{SubscriptionID: 1, Nonce: "old", ExpiresAt: now.Add(-time.Minute)})
	db.Create(&store.TransportNonce{SubscriptionID: 1, Nonce: "new", ExpiresAt: now.Add(time.Minute)})
	if n, err := h.srv.cleanupNonces(t.Context()); err != nil || n != 1 {
		t.Fatalf("清理 = %d, %v", n, err)
	}
	var left []store.TransportNonce
	db.Find(&left)
	if len(left) != 1 || left[0].Nonce != "new" {
		t.Errorf("剩下 = %+v", left)
	}
}

func TestCustomScriptURL(t *testing.T) {
	h := ready(t)
	var ep endpointView
	h.do("POST", "/api/v1/endpoints", map[string]string{"name": "lan", "base_url": h.ts.URL}, &ep)
	h.do("POST", "/api/v1/examples/ninebot/import", map[string]any{}, nil)
	in := map[string]any{"name": "s", "client": "surge", "endpoint_id": ep.ID, "all_sources": true, "script_url": "http://cdn.example.com/ferry.js"}
	if code := h.do("POST", "/api/v1/subscriptions", in, nil); code != 400 {
		t.Errorf("http 脚本地址 = %d，期望 400", code)
	}
	in["script_url"] = "https://cdn.example.com/ferry@1/ferry.js"
	var sv subscriptionView
	if code := h.do("POST", "/api/v1/subscriptions", in, &sv); code != 201 {
		t.Fatalf("建订阅 = %d", code)
	}
	if sv.Links.ScriptURL != "https://cdn.example.com/ferry@1/ferry.js" || sv.ScriptURL != sv.Links.ScriptURL {
		t.Errorf("脚本链接 = %+v", sv)
	}
	resp, err := http.Get(sv.Links.SubscriptionURL)
	if err != nil {
		t.Fatal(err)
	}
	file, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(file), "script-path=https://cdn.example.com/ferry@1/ferry.js") {
		t.Errorf("订阅应引用自定义脚本地址：\n%s", file)
	}
	in["script_url"] = ""
	h.do("PUT", "/api/v1/subscriptions/"+itoa(sv.ID), in, &sv)
	if sv.Links.ScriptURL != h.ts.URL+"/script/ferry.js" {
		t.Errorf("清空后应回到平台地址：%s", sv.Links.ScriptURL)
	}
}
