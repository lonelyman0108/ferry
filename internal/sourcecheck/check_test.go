package sourcecheck

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lonelyman0108/ferry/internal/probe"
	"github.com/lonelyman0108/ferry/internal/source"
)

func validDoc() source.Doc {
	return source.Doc{
		Key:    "demo",
		Name:   "示例来源",
		Notify: source.NotifyAlways,
		Matchers: []source.MatcherDoc{
			{Name: "api", Host: "*.example.com", Path: "^/v1/"},
		},
		Fields: []source.FieldDoc{
			{Key: "token", Name: "令牌", Matcher: "api", In: "header", Path: "Authorization", MinLen: 8},
		},
		Probe: &probe.Spec{
			Method:  "GET",
			URL:     "https://api.example.com/v1/profile?t={{.token}}",
			Headers: map[string]string{"Authorization": "{{.token}}"},
			Expect:  "status == 200",
		},
	}
}

func TestCheckValid(t *testing.T) {
	r := Check(validDoc())
	if r.HasViolations() {
		t.Fatalf("合法定义不应违规：%v", r.Violations)
	}
	if len(r.Warnings) > 0 {
		t.Fatalf("合法定义不应有警告：%v", r.Warnings)
	}
	if r.ProbeHost != "api.example.com" {
		t.Errorf("ProbeHost = %q", r.ProbeHost)
	}
}

func TestCheckBroadHost(t *testing.T) {
	badHosts := []string{"*", "*.com", "*.cn", "*.com.cn", "*.co.uk", "*.github.io", "*.pages.dev", "*.vercel.app"}
	for _, h := range badHosts {
		d := validDoc()
		d.Probe = nil
		d.Matchers[0].Host = h
		r := Check(d)
		if !hasCode(r.Violations, CodeBroadHost) {
			t.Errorf("域名 %q 应被判定为过于宽泛，实际结果：%v", h, r.Violations)
		}
	}

	okHosts := []string{"api.example.com", "*.example.com", "*.service.example.com.cn"}
	for _, h := range okHosts {
		d := validDoc()
		d.Probe = nil
		d.Matchers[0].Host = h
		r := Check(d)
		if hasCode(r.Violations, CodeBroadHost) {
			t.Errorf("域名 %q 不应被判定为过于宽泛：%v", h, r.Violations)
		}
	}
}

func TestCheckProbeHostMismatch(t *testing.T) {
	cases := []struct {
		name    string
		url     string
		headers map[string]string
		wantBad bool
	}{
		{
			name:    "探测域名在匹配泛域名内",
			url:     "https://user.example.com/v1/me?t={{.token}}",
			wantBad: false,
		},
		{
			name:    "探测域名不在匹配域名内",
			url:     "https://collector.other.org/v1/me?t={{.token}}",
			wantBad: true,
		},
		{
			name:    "探测域名使用模板变量",
			url:     "https://{{.token}}/v1/me",
			wantBad: true,
		},
		{
			name:    "探测子域名使用模板变量",
			url:     "https://{{.token}}.example.com/v1/me",
			wantBad: true,
		},
		{
			name:    "探测地址带用户信息绕过",
			url:     "https://api.example.com@{{.token}}/v1/me",
			wantBad: true,
		},
		{
			name:    "Host 请求头指向外部域名",
			url:     "https://api.example.com/v1/me",
			headers: map[string]string{"Host": "evil.other.org"},
			wantBad: true,
		},
		{
			name:    "Host 请求头使用模板变量",
			url:     "https://api.example.com/v1/me",
			headers: map[string]string{"Host": "{{.token}}"},
			wantBad: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := validDoc()
			d.Probe.URL = c.url
			if c.headers != nil {
				d.Probe.Headers = c.headers
			}
			r := Check(d)
			gotBad := hasCode(r.Violations, CodeProbeHostMismatch)
			if gotBad != c.wantBad {
				t.Errorf("gotBad = %v, want %v (violations=%v)", gotBad, c.wantBad, r.Violations)
			}
		})
	}
}

func TestCheckWarnings(t *testing.T) {
	d := validDoc()
	d.Notify = source.NotifyChanged
	d.Debug = true
	r := Check(d)
	if !hasCode(r.Warnings, CodeNotifyNotAlways) {
		t.Errorf("应警告 notify != always：%v", r.Warnings)
	}
	if !hasCode(r.Warnings, CodeDebugEnabled) {
		t.Errorf("应警告 debug == true：%v", r.Warnings)
	}
}

func TestCheckTargetHubMode(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	d := validDoc()
	envBytes, err := source.MarshalEnvelope(d, source.Meta{Version: 1, Author: "tester", Tags: []string{"测试"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "source.json"), envBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	// 缺少 README.md 和 cases.json 时应当报错。
	res, err := CheckTarget(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].OK() {
		t.Fatalf("缺少文件时不应通过：%+v", res)
	}
	if !hasCode(res[0].Report.Violations, CodeMissingReadme) || !hasCode(res[0].Report.Violations, CodeMissingCases) {
		t.Errorf("应报告缺少 README.md 与 cases.json：%v", res[0].Report.Violations)
	}

	// 补齐 README.md 与合法脱敏 cases.json 后应当通过。
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# 示例来源\n\n说明文本。\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	casesJSON := `{
  "cases": [
    {
      "name": "个人资料请求",
      "matcher": "api",
      "request": {
        "method": "GET",
        "url": "https://api.example.com/v1/profile",
        "headers": {
          "Authorization": "Bearer sample_redacted_token"
        }
      },
      "expected": {
        "token": "Bearer sample_redacted_token"
      }
    }
  ]
}
`
	if err := os.WriteFile(filepath.Join(dir, "cases.json"), []byte(casesJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err = CheckTarget(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if !res[0].OK() {
		t.Fatalf("补齐文件后应当通过：problems=%v violations=%v err=%v", res[0].Problems, res[0].Report.Violations, res[0].Err)
	}

	// 高熵未脱敏字符串应触发警告。
	highEntropyCases := `{
  "cases": [
    {
      "name": "高熵令牌",
      "request": {
        "method": "GET",
        "url": "https://api.example.com/v1/profile",
        "headers": {
          "Authorization": "Bearer k9Qm2Xv8Lp4Zr7Wt1Ny6Bc3Df5Gh8Jk0Mn2Pq9Rs4Tu7Vw"
        }
      },
      "expected": {
        "token": "Bearer k9Qm2Xv8Lp4Zr7Wt1Ny6Bc3Df5Gh8Jk0Mn2Pq9Rs4Tu7Vw"
      }
    }
  ]
}
`
	if err := os.WriteFile(filepath.Join(dir, "cases.json"), []byte(highEntropyCases), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err = CheckTarget(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if !hasCode(res[0].Report.Warnings, CodeHighEntropySample) {
		t.Errorf("高熵样例应给出警告：%v", res[0].Report.Warnings)
	}
}

func hasCode(issues []Issue, code string) bool {
	for _, item := range issues {
		if item.Code == code {
			return true
		}
	}
	return false
}
