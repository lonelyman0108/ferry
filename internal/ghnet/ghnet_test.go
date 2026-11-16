package ghnet

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lonelyman0108/ferry/internal/config"
	"github.com/lonelyman0108/ferry/internal/secret"
	"github.com/lonelyman0108/ferry/internal/store"
)

func TestValidateAndMask(t *testing.T) {
	okConfigs := []Config{
		{Mode: ModeDirect},
		{Mode: ModeMirror, MirrorPrefix: "https://ghfast.top/"},
		{Mode: ModeProxy, ProxyURL: "http://127.0.0.1:7890"},
		{Mode: ModeProxy, ProxyURL: "https://user:pass@proxy.example.com:8443"},
		{Mode: ModeProxy, ProxyURL: "socks5://127.0.0.1:1080"},
		{Mode: ModeProxy, ProxyURL: "socks5h://u:p@127.0.0.1:1080"},
	}
	for _, c := range okConfigs {
		if err := c.Validate(); err != nil {
			t.Errorf("配置 %+v 应当合法：%v", c, err)
		}
	}

	badConfigs := []Config{
		{Mode: "unknown"},
		{Mode: ModeMirror},
		{Mode: ModeMirror, MirrorPrefix: "http://ghfast.top"},
		{Mode: ModeMirror, MirrorPrefix: "https://ghfast.top?a=1"},
		{Mode: ModeProxy},
		{Mode: ModeProxy, ProxyURL: "ftp://127.0.0.1:21"},
		{Mode: ModeProxy, ProxyURL: "socks4://127.0.0.1:1080"},
	}
	for _, c := range badConfigs {
		if err := c.Validate(); err == nil {
			t.Errorf("配置 %+v 应当报错", c)
		}
	}

	raw := "socks5h://alice:s3cretP@ss@127.0.0.1:1080"
	masked := MaskProxyURL(raw)
	if masked != "socks5h://alice:••••@127.0.0.1:1080" {
		t.Errorf("MaskProxyURL = %q", masked)
	}
	if got := ResolveProxyURL(masked, raw); got != raw {
		t.Errorf("ResolveProxyURL 未还原原值：%q", got)
	}
	if got := ResolveProxyURL("socks5h://alice:newpass@127.0.0.1:1080", raw); got != "socks5h://alice:newpass@127.0.0.1:1080" {
		t.Errorf("ResolveProxyURL 改密码后应取新值：%q", got)
	}
}

func TestSaveLoadConfigSealsProxy(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(config.Database{Driver: config.DriverSQLite, DSN: filepath.Join(t.TempDir(), "f.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	key, err := secret.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	box, err := secret.New(key)
	if err != nil {
		t.Fatal(err)
	}

	want := Config{
		Mode:         ModeProxy,
		MirrorPrefix: "https://ghfast.top/",
		ProxyURL:     "http://admin:topsecret@127.0.0.1:7890",
	}
	if err := SaveConfig(ctx, st.DB, box, want); err != nil {
		t.Fatal(err)
	}

	var row store.Setting
	if err := st.DB.Where("`key` = ?", SettingProxyURL).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(row.Value, "gcm:") || strings.Contains(row.Value, "topsecret") {
		t.Errorf("settings 表中代理地址未用主密钥加密：%q", row.Value)
	}

	got, err := LoadConfig(ctx, st.DB, box)
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != ModeProxy || got.MirrorPrefix != "https://ghfast.top" || got.ProxyURL != want.ProxyURL {
		t.Errorf("LoadConfig = %+v，期望 %+v", got, want)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMirrorRewriteOnlyGitHub(t *testing.T) {
	var seen []string
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		seen = append(seen, req.URL.String())
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("ok")),
			Header:     make(http.Header),
		}, nil
	})

	client, err := NewHTTPClient(Config{
		Mode:         ModeMirror,
		MirrorPrefix: "https://ghfast.top",
	}, 5*time.Second, base)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := client.Get("https://raw.githubusercontent.com/lonelyman0108/ferry/main/README.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Get("https://example.com/source.json"); err != nil {
		t.Fatal(err)
	}

	if len(seen) != 2 {
		t.Fatalf("seen = %v", seen)
	}
	if seen[0] != "https://ghfast.top/https://raw.githubusercontent.com/lonelyman0108/ferry/main/README.md" {
		t.Errorf("GitHub 地址未改写为镜像前缀：%s", seen[0])
	}
	if seen[1] != "https://example.com/source.json" {
		t.Errorf("非 GitHub 地址不应改写：%s", seen[1])
	}
}

func TestExtractReleaseTag(t *testing.T) {
	cases := []struct {
		loc     string
		wantTag string
		wantURL string
		wantOK  bool
	}{
		{
			loc:     "https://github.com/lonelyman0108/ferry/releases/tag/v0.2.0",
			wantTag: "v0.2.0",
			wantURL: "https://github.com/lonelyman0108/ferry/releases/tag/v0.2.0",
			wantOK:  true,
		},
		{
			loc:     "/https://github.com/lonelyman0108/ferry/releases/tag/v0.3.1",
			wantTag: "v0.3.1",
			wantURL: "https://github.com/lonelyman0108/ferry/releases/tag/v0.3.1",
			wantOK:  true,
		},
		{
			loc:     "https://ghfast.top/https://github.com/lonelyman0108/ferry/releases/tag/v1.0.0?foo=1",
			wantTag: "v1.0.0",
			wantURL: "https://github.com/lonelyman0108/ferry/releases/tag/v1.0.0",
			wantOK:  true,
		},
		{
			loc:    "https://github.com/lonelyman0108/ferry/releases",
			wantOK: false,
		},
	}
	for _, c := range cases {
		tag, u, ok := ExtractReleaseTag(c.loc, "lonelyman0108/ferry")
		if ok != c.wantOK || tag != c.wantTag || u != c.wantURL {
			t.Errorf("ExtractReleaseTag(%q) = (%q, %q, %v), want (%q, %q, %v)", c.loc, tag, u, ok, c.wantTag, c.wantURL, c.wantOK)
		}
	}
}
