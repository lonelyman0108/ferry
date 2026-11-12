package sub

import (
	"net/url"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/lonelyman0108/ferry/internal/source"
)

func TestURLPattern(t *testing.T) {
	cases := []struct {
		host, path string
		match      []string
		miss       []string
	}{
		{"cn-cbu-gateway.ninebot.com", "^/portal/api/",
			[]string{"https://cn-cbu-gateway.ninebot.com/portal/api/x", "http://cn-cbu-gateway.ninebot.com:8443/portal/api/"},
			[]string{"https://cn-cbu-gateway.ninebot.com/other", "https://cn-cbu-gatewayXninebot.com/portal/api/", "https://evil.com/?u=https://cn-cbu-gateway.ninebot.com/portal/api/"}},
		{"cn-cbu-gateway.ninebot.com", "^/portal/api/user-sign/v2/sign$",
			[]string{"https://cn-cbu-gateway.ninebot.com/portal/api/user-sign/v2/sign", "https://cn-cbu-gateway.ninebot.com/portal/api/user-sign/v2/sign?t=1"},
			[]string{"https://cn-cbu-gateway.ninebot.com/portal/api/user-sign/v2/signx"}},
		{"*.jd.com", "",
			[]string{"https://api.m.jd.com/client.action", "https://a.b.jd.com/"},
			[]string{"https://jd.com/", "https://evil-jd.com/", "https://jd.com.evil.com/"}},
		{"a.example.com", "/unanchored/",
			[]string{"https://a.example.com/anything"}, nil},
	}
	for _, c := range cases {
		p := URLPattern(c.host, c.path)
		re := regexp.MustCompile(p)
		for _, u := range c.match {
			if !re.MatchString(u) {
				t.Errorf("%s 应当匹配 %s", p, u)
			}
		}
		for _, u := range c.miss {
			if re.MatchString(u) {
				t.Errorf("%s 不应匹配 %s", p, u)
			}
		}
	}
}

func ninebot(t *testing.T) source.Doc {
	t.Helper()
	for _, e := range source.Examples() {
		if e.Key == "ninebot" {
			return e.Doc
		}
	}
	t.Fatal("没有九号示例")
	return source.Doc{}
}

func params(t *testing.T, client string) Params {
	return Params{Name: "iPhone", Client: client, BaseURL: "http://192.168.1.10:8080", FallbackURL: "https://ferry.example.com",
		Token: "fs_abc", Sources: []source.Doc{ninebot(t)}}
}

func TestGenerateSurge(t *testing.T) {
	out, err := Generate(params(t, Surge))
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	lines := strings.Split(s, "\n")
	var scripts []string
	for _, l := range lines {
		if strings.HasPrefix(l, "ferry-") {
			scripts = append(scripts, l)
		}
	}
	if len(scripts) != 2 || !strings.HasPrefix(scripts[0], "ferry-ninebot-sign = type=http-request,") || !strings.Contains(scripts[0], "requires-body=true") || !strings.Contains(scripts[1], "requires-body=false") {
		t.Errorf("要正文的脚本应排在前面：\n%s", s)
	}
	for _, want := range []string{"#!name=Ferry · iPhone", "max-size=-1,", "[MITM]\nhostname = %APPEND% cn-cbu-gateway.ninebot.com",
		"script-path=http://192.168.1.10:8080/script/ferry.js", `argument="base=http%3A%2F%2F192.168.1.10%3A8080&fallback=https%3A%2F%2Fferry.example.com&token=fs_abc"`} {
		if !strings.Contains(s, want) {
			t.Errorf("Surge 订阅缺少 %q：\n%s", want, s)
		}
	}
}

func TestGenerateOthers(t *testing.T) {
	loon, _ := Generate(params(t, Loon))
	if !strings.Contains(string(loon), "http-request ^https?://cn-cbu-gateway") || !strings.Contains(string(loon), "[Mitm]\nhostname = cn-cbu-gateway.ninebot.com") {
		t.Errorf("Loon 插件不对：\n%s", loon)
	}
	qx, _ := Generate(params(t, QX))
	if !strings.Contains(string(qx), "url script-request-body http://192.168.1.10:8080/script/ferry.js#base=") || !strings.Contains(string(qx), "url script-request-header ") {
		t.Errorf("QX 片段不对：\n%s", qx)
	}

	stashOut, err := Generate(params(t, Stash))
	if err != nil {
		t.Fatal(err)
	}
	var st struct {
		HTTP struct {
			MITM   []string         `yaml:"mitm"`
			Script []map[string]any `yaml:"script"`
		} `yaml:"http"`
		Providers map[string]map[string]any `yaml:"script-providers"`
	}
	if err := yaml.Unmarshal(stashOut, &st); err != nil {
		t.Fatalf("Stash 覆写不是合法 YAML：%v\n%s", err, stashOut)
	}
	if len(st.HTTP.Script) != 2 || st.HTTP.Script[0]["require-body"] != true || st.Providers["ferry"]["url"] != "http://192.168.1.10:8080/script/ferry.js" || len(st.HTTP.MITM) != 1 {
		t.Errorf("Stash 覆写内容不对：\n%s", stashOut)
	}

	egernOut, err := Generate(params(t, Egern))
	if err != nil {
		t.Fatal(err)
	}
	var eg struct {
		Scriptings []struct {
			HTTPRequest map[string]any `yaml:"http_request"`
		} `yaml:"scriptings"`
		MITM struct {
			Hostnames struct {
				Includes []string `yaml:"includes"`
			} `yaml:"hostnames"`
		} `yaml:"mitm"`
	}
	if err := yaml.Unmarshal(egernOut, &eg); err != nil {
		t.Fatalf("Egern 模块不是合法 YAML：%v\n%s", err, egernOut)
	}
	if len(eg.Scriptings) != 2 || eg.Scriptings[0].HTTPRequest["body_required"] != true || len(eg.MITM.Hostnames.Includes) != 1 {
		t.Errorf("Egern 模块内容不对：\n%s", egernOut)
	}
}

func TestGenerateSkipsDisabledSources(t *testing.T) {
	p := params(t, Surge)
	off := false
	p.Sources[0].Enabled = &off
	out, _ := Generate(p)
	if strings.Contains(string(out), "ferry-ninebot") || strings.Contains(string(out), "[MITM]") {
		t.Errorf("停用的来源不应出现在订阅里：\n%s", out)
	}
}

// 加密订阅：文件里不能有令牌和密钥，只有密钥编号和各客户端的参数占位符；占位符替换成用户填的值后，脚本参数完整。
func TestGenerateSealed(t *testing.T) {
	const key = "AQgPFh0kKzI5QEdOVVxjanF4f4aNlJuiqbC3vsXM09o" // gitleaks:allow 测试用的固定假密钥
	argRe := regexp.MustCompile(`argument="([^"]+)"|argument: '?([^'\n]+)'?|_compat\.\$argument: '?([^'\n]+)'?`)
	for client, placeholder := range map[string]string{Surge: "{{{key}}}", Shadowrocket: "{{{key}}}", Stash: "${args.key}", Egern: "{{{key}}}"} {
		t.Run(client, func(t *testing.T) {
			p := params(t, client)
			p.KID = "kid1234567890abc"
			out, err := Generate(p)
			if err != nil {
				t.Fatal(err)
			}
			text := string(out)
			if strings.Contains(text, "fs_abc") || strings.Contains(text, "token=") {
				t.Fatalf("加密订阅不能带令牌：\n%s", text)
			}
			m := argRe.FindStringSubmatch(text)
			if m == nil {
				t.Fatalf("找不到脚本参数：\n%s", text)
			}
			arg := m[1] + m[2] + m[3]
			if !strings.Contains(arg, "key="+placeholder) {
				t.Fatalf("脚本参数里应引用 %s：%s", placeholder, arg)
			}
			q, err := url.ParseQuery(strings.Replace(arg, placeholder, key, 1))
			if err != nil || q.Get("base") != p.BaseURL || q.Get("fallback") != p.FallbackURL || q.Get("kid") != p.KID || q.Get("mode") != "sealed" || q.Get("key") != key {
				t.Errorf("替换占位符后的参数 = %v, %v", q, err)
			}
			if client == Stash || client == Egern {
				var doc map[string]any
				if err := yaml.Unmarshal(out, &doc); err != nil {
					t.Fatalf("YAML 不合法：%v", err)
				}
			}
			switch client {
			case Surge, Shadowrocket:
				if !strings.Contains(text, "#!arguments=key:\n") {
					t.Errorf("缺少参数声明：\n%s", text)
				}
			case Stash:
				if !strings.Contains(text, "arguments-version: 1") || !strings.Contains(text, "required: true") {
					t.Errorf("缺少覆写参数声明：\n%s", text)
				}
			case Egern:
				if !strings.Contains(text, "compat_arguments:\n  key: \"\"") {
					t.Errorf("缺少 compat_arguments：\n%s", text)
				}
			}
		})
	}

	p := params(t, Loon)
	p.KID = "kid1234567890abc"
	out, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	for _, want := range []string{"[Argument]\nkey = input,\"\",tag=传输密钥", `kid = input,"kid1234567890abc"`, `base = input,"http://192.168.1.10:8080"`, `mode = input,"sealed"`, "argument=[{base},{fallback},{kid},{mode},{key}]"} {
		if !strings.Contains(text, want) {
			t.Errorf("Loon 插件缺少 %q：\n%s", want, text)
		}
	}
	if strings.Contains(text, "fs_abc") {
		t.Error("Loon 加密插件不能带令牌")
	}

	p = params(t, QX)
	p.KID = "kid1234567890abc"
	if _, err := Generate(p); err == nil {
		t.Error("QX 不支持加密，应当报错")
	}
}
