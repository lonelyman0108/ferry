package rules

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"text/template"
)

// 模板和渲染结果的长度上限，防止模板写错时生成超大的变量。
const (
	maxTemplateLen = 4 << 10
	maxOutputLen   = 64 << 10
)

// templateFuncs 是模板里能用的函数，都是纯字符串处理，没有任何 IO。
// text/template 自带的 and、or、not、eq、len、index、print、printf、urlquery 等也可以用。
var templateFuncs = template.FuncMap{
	"trim":      strings.TrimSpace,
	"lower":     strings.ToLower,
	"upper":     strings.ToUpper,
	"b64":       func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) },
	"urlencode": url.QueryEscape,
	// default 在值为空时返回 fallback，写法是 {{default "x" .field}}。
	"default": func(fallback, v string) string {
		if v == "" {
			return fallback
		}
		return v
	},
}

// Template 是解析好的输出模板。
type Template struct {
	t *template.Template
}

// ParseTemplate 解析输出模板，例如 pt_key={{.pt_key}};pt_pin={{.pt_pin}};。
func ParseTemplate(text string) (*Template, error) {
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("模板不能为空")
	}
	if len(text) > maxTemplateLen {
		return nil, fmt.Errorf("模板不能超过 %d 字节", maxTemplateLen)
	}
	t, err := template.New("output").Funcs(templateFuncs).Option("missingkey=error").Parse(text)
	if err != nil {
		return nil, fmt.Errorf("模板语法错误：%w", err)
	}
	return &Template{t: t}, nil
}

// Render 用字段值渲染模板。引用了不存在的字段会报错。
func (t *Template) Render(values map[string]string) (string, error) {
	var buf limitedBuffer
	if err := t.t.Execute(&buf, values); err != nil {
		if errors.Is(err, errOutputTooLong) {
			return "", fmt.Errorf("渲染结果超过 %d 字节", maxOutputLen)
		}
		return "", fmt.Errorf("渲染模板：%s", redact(err.Error(), values))
	}
	return buf.String(), nil
}

// redact 把错误信息里出现的字段值替换成 ***。text/template 的执行错误会带出数据本身，
// 而字段值可能是敏感的登录态，不能进日志或接口响应。少于 4 个字符的值不替换，免得误伤错误信息里的其他文字。
func redact(msg string, values map[string]string) string {
	for _, v := range values {
		if len(v) >= 4 {
			msg = strings.ReplaceAll(msg, v, "***")
		}
	}
	return msg
}

// CheckFields 用占位值试渲染一次，确认模板只引用了 known 里的字段。
func (t *Template) CheckFields(known []string) error {
	values := make(map[string]string, len(known))
	for _, k := range known {
		values[k] = "x"
	}
	_, err := t.Render(values)
	return err
}

var errOutputTooLong = errors.New("输出过长")

type limitedBuffer struct {
	bytes.Buffer
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > maxOutputLen {
		return 0, errOutputTooLong
	}
	return b.Buffer.Write(p)
}
