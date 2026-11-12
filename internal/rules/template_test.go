package rules

import (
	"strings"
	"testing"
)

func TestRender(t *testing.T) {
	cases := []struct {
		tmpl   string
		values map[string]string
		want   string
	}{
		{"pt_key={{.pt_key}};pt_pin={{.pt_pin}};", map[string]string{"pt_key": "AAJ", "pt_pin": "jd_x"}, "pt_key=AAJ;pt_pin=jd_x;"},
		{"{{.authorization}}", map[string]string{"authorization": "Bearer ey"}, "Bearer ey"},
		{"{{trim .a | upper}}", map[string]string{"a": "  ab "}, "AB"},
		{"{{default \"none\" .a}}", map[string]string{"a": ""}, "none"},
		{"{{b64 .a}}", map[string]string{"a": "hi"}, "aGk="},
		{"{{urlencode .a}}", map[string]string{"a": "a b&c"}, "a+b%26c"},
		{"{{if eq .a \"1\"}}yes{{else}}no{{end}}", map[string]string{"a": "1"}, "yes"},
	}
	for _, c := range cases {
		tp, err := ParseTemplate(c.tmpl)
		if err != nil {
			t.Fatalf("%q：%v", c.tmpl, err)
		}
		got, err := tp.Render(c.values)
		if err != nil || got != c.want {
			t.Errorf("%q = %q, %v；期望 %q", c.tmpl, got, err, c.want)
		}
	}
}

func TestTemplateErrors(t *testing.T) {
	for _, bad := range []string{"", "   ", "{{.a", "{{exec .a}}", strings.Repeat("x", maxTemplateLen+1)} {
		if _, err := ParseTemplate(bad); err == nil {
			t.Errorf("模板 %q 应当解析失败", bad[:min(len(bad), 20)])
		}
	}
	tp, _ := ParseTemplate("{{.a}}{{.b}}")
	if err := tp.CheckFields([]string{"a"}); err == nil {
		t.Error("引用未声明的字段 b 应当报错")
	}
	if err := tp.CheckFields([]string{"a", "b"}); err != nil {
		t.Errorf("字段都声明了：%v", err)
	}
}

func TestRenderOutputLimit(t *testing.T) {
	tp, _ := ParseTemplate(`{{range 100}}{{$.big}}{{end}}`)
	big := strings.Repeat("x", 1024)
	if _, err := tp.Render(map[string]string{"big": big}); err == nil || !strings.Contains(err.Error(), "超过") {
		t.Errorf("超长输出 err = %v，期望报超过上限", err)
	}
}

func TestRenderErrorHidesValues(t *testing.T) {
	tp, _ := ParseTemplate(`{{range .token}}{{end}}`)
	_, err := tp.Render(map[string]string{"token": "secret-token-value"})
	if err == nil {
		t.Fatal("对字符串 range 应当报错")
	}
	if strings.Contains(err.Error(), "secret-token-value") {
		t.Errorf("错误信息泄露了字段值：%v", err)
	}
}
