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
	status := http.StatusFound
	location := "https://github.com/lonelyman0108/ferry/releases/tag/v0.2.0"
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if location != "" && status == http.StatusFound {
			w.Header().Set("Location", location)
		}
		w.WriteHeader(status)
	}))
	defer gh.Close()
	old := buildinfo.Version
	buildinfo.Version = "0.1.0"
	defer func() { buildinfo.Version = old }()

	h := ready(t)
	h.srv.releaseURL = gh.URL + "/releases/latest"
	var u updateView
	if code := h.do("GET", "/api/v1/system/update", nil, &u); code != 200 || !u.Newer || u.Channel != "release" || u.Latest != "v0.2.0" || u.URL != "https://github.com/lonelyman0108/ferry/releases/tag/v0.2.0" {
		t.Fatalf("检查更新 = %d %+v", code, u)
	}
	h.do("GET", "/api/v1/system/update", nil, &u)
	if hits.Load() != 1 {
		t.Errorf("一小时内应直接用缓存，实际请求了 %d 次", hits.Load())
	}
	// 镜像形式跳转（/https://github.com/.../releases/tag/v0.2.1）同样可解析。
	location = "/https://github.com/lonelyman0108/ferry/releases/tag/v0.2.1"
	h.do("GET", "/api/v1/system/update?force=1", nil, &u)
	if !u.Newer || u.Latest != "v0.2.1" {
		t.Errorf("镜像跳转解析失败 = %+v", u)
	}
	// edge 预览构建不参与正式版本号大小比较，渠道标记为 edge。
	buildinfo.Version = "edge"
	h.do("GET", "/api/v1/system/update?force=1", nil, &u)
	if u.Channel != "edge" || u.Current != "edge" || u.Newer || u.Latest != "v0.2.1" {
		t.Errorf("edge 渠道检查更新 = %+v", u)
	}
	status = http.StatusNotFound
	h.do("GET", "/api/v1/system/update?force=1", nil, &u)
	if hits.Load() != 4 || u.Error == "" || u.Newer {
		t.Errorf("强制重查、没有发布过版本 = %+v", u)
	}
}

func TestNetworkSettings(t *testing.T) {
	h := ready(t)

	var view networkSettingsView
	if code := h.do("GET", "/api/v1/settings/network", nil, &view); code != 200 || view.Mode != "direct" || len(view.Presets) < 3 {
		t.Fatalf("初始网络设置 = %d %+v", code, view)
	}

	// 非法镜像前缀（非 https）和非法代理协议应返回 400。
	if code := h.do("PUT", "/api/v1/settings/network", map[string]string{"mode": "mirror", "mirror_prefix": "http://bad.example.com"}, nil); code != 400 {
		t.Errorf("非 https 镜像前缀应返回 400，实际 = %d", code)
	}
	if code := h.do("PUT", "/api/v1/settings/network", map[string]string{"mode": "proxy", "proxy_url": "ftp://127.0.0.1:21"}, nil); code != 400 {
		t.Errorf("非法代理协议应返回 400，实际 = %d", code)
	}

	// 保存带密码的代理地址，接口返回脱敏形式；原样回传脱敏值不覆盖原密码。
	if code := h.do("PUT", "/api/v1/settings/network", map[string]string{
		"mode":      "proxy",
		"proxy_url": "socks5://user:mysecret@127.0.0.1:1080",
	}, &view); code != 200 || view.ProxyURL != "socks5://user:••••@127.0.0.1:1080" {
		t.Fatalf("保存代理设置 = %d %+v", code, view)
	}
	if code := h.do("PUT", "/api/v1/settings/network", map[string]string{
		"mode":          "mirror",
		"mirror_prefix": "https://ghfast.top/",
		"proxy_url":     view.ProxyURL,
	}, &view); code != 200 || view.Mode != "mirror" || view.MirrorPrefix != "https://ghfast.top" || view.ProxyURL != "socks5://user:••••@127.0.0.1:1080" {
		t.Fatalf("切换为镜像并保留原代理密码 = %d %+v", code, view)
	}

	// 测试连接接口。
	targetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer targetSrv.Close()
	h.srv.networkTestURL = targetSrv.URL + "/README.md"

	var tr networkTestResult
	if code := h.do("POST", "/api/v1/settings/network/test", map[string]string{"mode": "direct"}, &tr); code != 200 || !tr.OK {
		t.Fatalf("测试连接 = %d %+v", code, tr)
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
		{"v0.2.0", "edge", false},
		{"edge", "0.2.0", false},
		{"nightly", "0.1.0", false},
	}
	for _, c := range cases {
		if got := newerVersion(c.latest, c.current); got != c.want {
			t.Errorf("newerVersion(%q, %q) = %v，期望 %v", c.latest, c.current, got, c.want)
		}
	}
}

func TestBuildChannel(t *testing.T) {
	cases := []struct {
		version string
		want    string
	}{
		{"0.2.0", "release"},
		{"v0.2.0", "release"},
		{"edge", "edge"},
		{"dev", "dev"},
		{"0.2.0-rc.1", "dev"},
	}
	for _, c := range cases {
		if got := buildChannel(c.version); got != c.want {
			t.Errorf("buildChannel(%q) = %q，期望 %q", c.version, got, c.want)
		}
	}
}
