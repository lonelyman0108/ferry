package sub

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestNormalizeBaseURL(t *testing.T) {
	ok := map[string]string{
		"http://192.168.1.10:8080/":   "http://192.168.1.10:8080",
		" https://ferry.example.com ": "https://ferry.example.com",
		"https://ferry.example.com":   "https://ferry.example.com",
		"https://example.com/ferry/":  "https://example.com/ferry",
	}
	for in, want := range ok {
		got, err := NormalizeBaseURL(in)
		if err != nil || got != want {
			t.Errorf("%q = %q, %v；期望 %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "192.168.1.10:8080", "ftp://x", "https://", "https://x/?a=1", "https://u:p@x"} {
		if _, err := NormalizeBaseURL(bad); err == nil {
			t.Errorf("%q 应当不合法", bad)
		}
	}
}

func TestInstallURLs(t *testing.T) {
	subURL := SubscriptionURL("http://192.168.1.10:8080", "tok", Surge)
	if subURL != "http://192.168.1.10:8080/sub/tok/ferry.sgmodule" {
		t.Errorf("订阅地址 = %q", subURL)
	}
	for _, c := range Clients {
		u, err := InstallURL(c, "Ferry iPhone", SubscriptionURL("http://h", "tok", c))
		if err != nil || u == "" {
			t.Errorf("%s：%q, %v", c, u, err)
		}
	}
	qx, _ := InstallURL(QX, "Ferry", "http://h/sub/tok/ferry.snippet")
	raw, _ := url.QueryUnescape(strings.TrimPrefix(qx, "quantumult-x:///add-resource?remote-resource="))
	var res map[string][]string
	if err := json.Unmarshal([]byte(raw), &res); err != nil || !strings.HasPrefix(res["rewrite_remote"][0], "http://h/sub/tok/ferry.snippet, tag=Ferry") {
		t.Errorf("QX 资源 JSON 不对：%s %v", raw, err)
	}
	if sr, _ := InstallURL(Shadowrocket, "Ferry", "http://h/sub/tok/ferry.sgmodule"); sr != "shadowrocket://install?module=http%3A%2F%2Fh%2Fsub%2Ftok%2Fferry.sgmodule" {
		t.Errorf("Shadowrocket 安装链接 = %q", sr)
	}
	if _, err := InstallURL("clash", "x", "y"); err == nil {
		t.Error("未知客户端应当报错")
	}
}

func TestNormalizeScriptURL(t *testing.T) {
	for raw, want := range map[string]string{
		"":   "",
		"  ": "",
		" https://cdn.example.com/ferry@1.2.0/ferry.js ": "https://cdn.example.com/ferry@1.2.0/ferry.js",
	} {
		if got, err := NormalizeScriptURL(raw); err != nil || got != want {
			t.Errorf("NormalizeScriptURL(%q) = %q, %v", raw, got, err)
		}
	}
	for _, raw := range []string{"http://x.com/ferry.js", "https:///ferry.js", "https://x.com/a.js#k", "https://u:p@x.com/a.js", "https://x.com/a,b.js", `https://x.com/a"b.js`, "ftp://x.com/a.js"} {
		if _, err := NormalizeScriptURL(raw); err == nil {
			t.Errorf("NormalizeScriptURL(%q) 应当报错", raw)
		}
	}
}
