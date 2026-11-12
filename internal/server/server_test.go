package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/lonelyman0108/ferry/internal/config"
	"github.com/lonelyman0108/ferry/internal/store"
)

type harness struct {
	t      *testing.T
	srv    *Server
	ts     *httptest.Server
	client *http.Client
	dir    string
}

func newHarness(t *testing.T, o Options) *harness {
	t.Helper()
	dir := t.TempDir()
	if o.ConfigPath == "" {
		o.ConfigPath = filepath.Join(dir, "ferry.yaml")
	}
	srv, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { ts.Close(); _ = srv.Close() })
	jar, _ := cookiejar.New(nil)
	return &harness{t: t, srv: srv, ts: ts, client: &http.Client{Jar: jar}, dir: dir}
}

// do 发请求；body 不为 nil 时编码成 JSON。返回状态码和解码到 out 的正文。
func (h *harness) do(method, path string, body any, out any, headers ...string) int {
	h.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, h.ts.URL+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := h.client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		_ = json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

func (h *harness) setup(body any) (int, setupResult) {
	var res setupResult
	code := h.do("POST", "/api/v1/setup", body, &res, setupTokenHeader, h.srv.SetupToken())
	return code, res
}

func admin(password string) map[string]any {
	return map[string]any{"username": "admin", "password": password}
}

func (h *harness) sqliteDB() map[string]string {
	return map[string]string{"driver": "sqlite", "dsn": filepath.Join(h.dir, "ferry.db")}
}

func TestHealthz(t *testing.T) {
	h := newHarness(t, Options{})
	resp, err := http.Get(h.ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || string(b) != "ok" {
		t.Fatalf("GET /healthz = %d %q", resp.StatusCode, b)
	}
}

func TestFullSetupFlow(t *testing.T) {
	h := newHarness(t, Options{Config: &config.Config{Listen: ":9000"}})

	var st setupState
	if h.do("GET", "/api/v1/setup/status", nil, &st); !st.NeedsConfig || !st.NeedsAdmin {
		t.Fatalf("初始状态 = %+v", st)
	}
	if code := h.do("POST", "/api/v1/auth/login", map[string]string{"username": "a", "password": "b"}, nil); code != 503 {
		t.Errorf("未初始化时登录 = %d，期望 503", code)
	}
	body := map[string]any{"database": h.sqliteDB(), "admin": admin("password123")}
	if code := h.do("POST", "/api/v1/setup", body, nil, setupTokenHeader, "wrong"); code != 403 {
		t.Errorf("令牌错误 = %d，期望 403", code)
	}
	if code := h.do("POST", "/api/v1/setup/database", map[string]string{"driver": "postgres", "dsn": "postgres://x@127.0.0.1:1/x?connect_timeout=1"}, nil, setupTokenHeader, h.srv.SetupToken()); code != 400 {
		t.Errorf("连不上的数据库 = %d，期望 400", code)
	}
	if code := h.do("POST", "/api/v1/setup/database", h.sqliteDB(), nil, setupTokenHeader, h.srv.SetupToken()); code != 200 {
		t.Errorf("测 SQLite 连通 = %d，期望 200", code)
	}

	code, res := h.setup(body)
	if code != 200 || res.MasterKey == "" {
		t.Fatalf("初始化 = %d %+v", code, res)
	}
	if h.srv.SetupToken() != "" {
		t.Error("初始化后向导令牌应当清空")
	}
	cfg, err := config.Load(res.ConfigPath)
	if err != nil {
		t.Fatalf("读取写出的配置：%v", err)
	}
	if cfg.MasterKey != res.MasterKey || cfg.Listen != ":9000" {
		t.Errorf("配置内容不对：%+v", cfg)
	}
	if info, _ := os.Stat(res.ConfigPath); info.Mode().Perm() != 0o600 {
		t.Errorf("配置文件权限 = %o", info.Mode().Perm())
	}

	if code, _ := h.setup(body); code != 409 {
		t.Errorf("重复初始化 = %d，期望 409", code)
	}
	var me userView
	if code := h.do("POST", "/api/v1/auth/login", admin("password123"), &me); code != 200 || me.Username != "admin" {
		t.Fatalf("登录 = %d %+v", code, me)
	}
	if code := h.do("GET", "/api/v1/auth/me", nil, &me); code != 200 {
		t.Errorf("me = %d", code)
	}
}

func TestSetupAdminOnlyWhenConfigFromEnv(t *testing.T) {
	st, err := store.Open(config.Database{Driver: config.DriverSQLite, DSN: filepath.Join(t.TempDir(), "f.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, Options{Store: st, Config: &config.Config{}})

	var state setupState
	if h.do("GET", "/api/v1/setup/status", nil, &state); state.NeedsConfig || !state.NeedsAdmin {
		t.Fatalf("状态 = %+v，期望只缺管理员", state)
	}
	if code, _ := h.setup(map[string]any{"database": h.sqliteDB(), "admin": admin("password123")}); code != 400 {
		t.Errorf("已有配置时再传数据库 = %d，期望 400", code)
	}
	code, res := h.setup(map[string]any{"admin": admin("password123")})
	if code != 200 || res.MasterKey != "" {
		t.Fatalf("只建管理员 = %d %+v", code, res)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "ferry.yaml")); !os.IsNotExist(err) {
		t.Error("配置来自环境变量时不应写配置文件")
	}
}

// ready 返回已完成初始化并登录的 harness。
func ready(t *testing.T) *harness {
	h := newHarness(t, Options{})
	if code, _ := h.setup(map[string]any{"database": h.sqliteDB(), "admin": admin("password123")}); code != 200 {
		t.Fatalf("初始化 = %d", code)
	}
	if code := h.do("POST", "/api/v1/auth/login", admin("password123"), nil); code != 200 {
		t.Fatalf("登录 = %d", code)
	}
	return h
}

func TestLoginRateLimit(t *testing.T) {
	h := ready(t)
	for i := 0; i < 5; i++ {
		if code := h.do("POST", "/api/v1/auth/login", admin("wrong-password"), nil); code != 401 {
			t.Fatalf("第 %d 次错误密码 = %d，期望 401", i+1, code)
		}
	}
	if code := h.do("POST", "/api/v1/auth/login", admin("password123"), nil); code != 429 {
		t.Errorf("超过次数后 = %d，期望 429（正确密码也要拒绝）", code)
	}
}

func TestLogout(t *testing.T) {
	h := ready(t)
	if code := h.do("POST", "/api/v1/auth/logout", map[string]any{}, nil); code != 200 {
		t.Fatalf("logout = %d", code)
	}
	if code := h.do("GET", "/api/v1/auth/me", nil, nil); code != 401 {
		t.Errorf("登出后 me = %d，期望 401", code)
	}
}

func TestCookieWriteRequiresJSON(t *testing.T) {
	h := ready(t)
	req, _ := http.NewRequest("POST", h.ts.URL+"/api/v1/auth/logout", bytes.NewBufferString("a=b"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 415 {
		t.Errorf("表单形式的写请求 = %d，期望 415", resp.StatusCode)
	}
}

func TestChangePasswordRevokesOtherSessions(t *testing.T) {
	h := ready(t)
	other := &harness{t: t, srv: h.srv, ts: h.ts, dir: h.dir}
	jar, _ := cookiejar.New(nil)
	other.client = &http.Client{Jar: jar}
	if code := other.do("POST", "/api/v1/auth/login", admin("password123"), nil); code != 200 {
		t.Fatal("第二个会话登录失败")
	}

	if code := h.do("POST", "/api/v1/auth/password", map[string]string{"old_password": "bad", "new_password": "newpassword1"}, nil); code != 400 {
		t.Errorf("原密码错误 = %d，期望 400", code)
	}
	if code := h.do("POST", "/api/v1/auth/password", map[string]string{"old_password": "password123", "new_password": "newpassword1"}, nil); code != 200 {
		t.Fatalf("改密码 = %d", code)
	}
	if code := h.do("GET", "/api/v1/auth/me", nil, nil); code != 200 {
		t.Errorf("当前会话应保留，me = %d", code)
	}
	if code := other.do("GET", "/api/v1/auth/me", nil, nil); code != 401 {
		t.Errorf("其他会话应被注销，me = %d", code)
	}
	if code := other.do("POST", "/api/v1/auth/login", admin("newpassword1"), nil); code != 200 {
		t.Errorf("新密码登录 = %d", code)
	}
}

func TestAPITokenLifecycle(t *testing.T) {
	h := ready(t)
	var created createdToken
	if code := h.do("POST", "/api/v1/tokens", map[string]string{"name": "qinglong"}, &created); code != 201 || created.Token == "" {
		t.Fatalf("建令牌 = %d %+v", code, created)
	}
	bearer := &harness{t: t, srv: h.srv, ts: h.ts, client: http.DefaultClient}
	var logs []store.AuditLog
	if code := bearer.do("GET", "/api/v1/audit", nil, &logs, "Authorization", "Bearer "+created.Token); code != 200 {
		t.Fatalf("用令牌查审计 = %d", code)
	}
	actions := map[string]bool{}
	for _, l := range logs {
		actions[l.Action] = true
	}
	for _, a := range []string{"setup.complete", "auth.login", "token.create"} {
		if !actions[a] {
			t.Errorf("审计日志缺少 %s：%v", a, actions)
		}
	}
	if code := bearer.do("GET", "/api/v1/tokens", nil, nil, "Authorization", "Bearer "+created.Token); code != 403 {
		t.Errorf("API 令牌管理令牌 = %d，期望 403", code)
	}

	var list []tokenView
	h.do("GET", "/api/v1/tokens", nil, &list)
	if len(list) != 1 || list[0].Prefix != created.Token[:9] {
		t.Errorf("令牌列表 = %+v", list)
	}
	if code := h.do("DELETE", "/api/v1/tokens/"+itoa(created.ID), nil, nil); code != 200 {
		t.Fatalf("吊销 = %d", code)
	}
	if code := bearer.do("GET", "/api/v1/audit", nil, nil, "Authorization", "Bearer "+created.Token); code != 401 {
		t.Errorf("吊销后 = %d，期望 401", code)
	}
	if code := h.do("DELETE", "/api/v1/tokens/"+itoa(created.ID), nil, nil); code != 404 {
		t.Errorf("重复吊销 = %d，期望 404", code)
	}
}

func itoa(n uint) string { return strconv.FormatUint(uint64(n), 10) }

func TestFailedLoginWithLongUsernameIsAudited(t *testing.T) {
	h := ready(t)
	long := map[string]any{"username": strings.Repeat("x", 300), "password": "whatever1"}
	if code := h.do("POST", "/api/v1/auth/login", long, nil); code != 401 {
		t.Fatalf("超长用户名登录 = %d，期望 401", code)
	}
	var logs []store.AuditLog
	h.do("GET", "/api/v1/audit?limit=1", nil, &logs)
	if len(logs) != 1 || logs[0].Action != "auth.login_failed" || len(logs[0].Target) != 128 {
		t.Errorf("最新一条审计 = %+v，期望截断到 128 字节的 auth.login_failed", logs)
	}
}

func TestScriptServed(t *testing.T) {
	h := newHarness(t, Options{})
	resp, err := http.Get(h.ts.URL + "/script/ferry.js")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Type"), "javascript") || !strings.Contains(string(b), "$done({})") {
		t.Errorf("GET /script/ferry.js = %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
}

func TestUIFallback(t *testing.T) {
	h := newHarness(t, Options{})
	for path, want := range map[string]string{"/": "没有内嵌管理界面", "/sources/3": "没有内嵌管理界面", "/api/v1/nope": "接口不存在"} {
		resp, err := http.Get(h.ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if !strings.Contains(string(b), want) {
			t.Errorf("GET %s = %d %q，期望包含 %q", path, resp.StatusCode, b, want)
		}
	}
}
