package server

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/lonelyman0108/ferry/internal/buildinfo"
)

func TestSystemInfo(t *testing.T) {
	h := ready(t)
	var v systemView
	if code := h.do("GET", "/api/v1/system", nil, &v); code != 200 {
		t.Fatalf("系统信息 = %d", code)
	}
	if v.Build.Version != buildinfo.Version || v.Build.Go == "" || v.Database.Driver != "sqlite" || v.Database.Version == "" {
		t.Errorf("构建与数据库 = %+v %+v", v.Build, v.Database)
	}
	if !v.Config.Exists || v.Config.Sources.DSN.From != "file" || v.Database.Size == 0 {
		t.Errorf("配置与大小 = %+v size=%d", v.Config, v.Database.Size)
	}
	if v.Runtime.StartedAt.IsZero() || v.Runtime.Timezone == "" {
		t.Errorf("运行环境 = %+v", v.Runtime)
	}

	// 只读 API 令牌读不到：里面有部署细节。
	var created createdToken
	h.do("POST", "/api/v1/tokens", map[string]string{"name": "monitor"}, &created)
	bearer := &harness{t: t, srv: h.srv, ts: h.ts, client: http.DefaultClient}
	if code := bearer.do("GET", "/api/v1/system", nil, nil, "Authorization", "Bearer "+created.Token); code == 200 {
		t.Error("API 令牌不应能读系统信息")
	}
}

func TestSystemUpdate(t *testing.T) {
	var hits atomic.Int32
	status := http.StatusOK
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"tag_name":"v0.2.0","html_url":"https://example.com/r/v0.2.0"}`))
	}))
	defer gh.Close()
	old := buildinfo.Version
	buildinfo.Version = "0.1.0"
	defer func() { buildinfo.Version = old }()

	h := ready(t)
	h.srv.releaseURL = gh.URL
	var u updateView
	if code := h.do("GET", "/api/v1/system/update", nil, &u); code != 200 || !u.Newer || u.Latest != "v0.2.0" || u.URL == "" {
		t.Fatalf("检查更新 = %d %+v", code, u)
	}
	h.do("GET", "/api/v1/system/update", nil, &u)
	if hits.Load() != 1 {
		t.Errorf("一小时内应直接用缓存，实际请求了 %d 次", hits.Load())
	}
	status = http.StatusNotFound
	h.do("GET", "/api/v1/system/update?force=1", nil, &u)
	if hits.Load() != 2 || u.Error == "" || u.Newer {
		t.Errorf("强制重查、没有发布过版本 = %+v", u)
	}
}

func TestNewerVersion(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"v0.2.0", "0.1.0", true},
		{"v0.1.1", "v0.1.0", true},
		{"v1.0.0", "0.9.9", true},
		{"v0.1.0", "0.1.0", false},
		{"v0.1.0", "0.2.0", false},
		{"v0.2.0", "dev", false},
		{"nightly", "0.1.0", false},
	}
	for _, c := range cases {
		if got := newerVersion(c.latest, c.current); got != c.want {
			t.Errorf("newerVersion(%q, %q) = %v，期望 %v", c.latest, c.current, got, c.want)
		}
	}
}
