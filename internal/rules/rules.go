// Package rules 定义来源的匹配规则和取值方式，并在服务端按同样的规则复核手机回传的请求。
//
// 手机端脚本 ferry.js 用同一套语义取值，两边共用 testdata/extract_cases.json 里的样例做测试。
package rules

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// 取值位置。
const (
	InHeader   = "header"
	InCookie   = "cookie"
	InQuery    = "query"
	InBodyJSON = "body_json"
	InBodyForm = "body_form"
	InRegex    = "regex"
)

// regex 取值的作用对象。
const (
	RegexOnURL  = "url"
	RegexOnBody = "body"
)

// Request 是一次待匹配、待取值的 HTTP 请求。
type Request struct {
	Method  string
	URL     string
	Headers http.Header
	Body    []byte
}

// Matcher 是一条请求匹配规则。
type Matcher struct {
	// Host 是精确域名，或 "*.example.com" 形式的通配（匹配任意一级及多级子域，不含 example.com 本身）。
	Host string
	// PathRegex 匹配 URL 的 path 部分，不含 query；为空表示任意路径。
	PathRegex string
	// Method 为空表示任意方法。
	Method string
}

// Field 描述从请求里取一个值的方式。
type Field struct {
	In string
	// Path 的含义随 In 而定：请求头名、Cookie 名、query 参数名、JSON 点路径、表单键名，或正则表达式。
	Path string
	// RegexOn 只在 In 为 regex 时使用，取 url 或 body。
	RegexOn string
}

// Validate 检查匹配规则本身是否合法。
func (m Matcher) Validate() error {
	h := strings.TrimPrefix(m.Host, "*.")
	if h == "" || strings.ContainsAny(h, "*/: ") || strings.HasPrefix(h, ".") || strings.HasSuffix(h, ".") {
		return fmt.Errorf("域名 %q 不合法，只能是精确域名或 *.example.com", m.Host)
	}
	if m.PathRegex != "" {
		if _, err := regexp.Compile(m.PathRegex); err != nil {
			return fmt.Errorf("路径正则不合法：%w", err)
		}
		// 订阅里 Surge 用逗号、Loon 用空格分隔参数，正则里出现它们会让订阅解析出错。
		if strings.ContainsAny(m.PathRegex, ", \t") {
			return fmt.Errorf(`路径正则不能含逗号或空白（订阅格式的分隔符）；逗号写作 \x2C，空格写作 \x20`)
		}
	}
	return nil
}

// Validate 检查取值规则本身是否合法。
func (f Field) Validate() error {
	switch f.In {
	case InHeader, InCookie, InQuery, InBodyJSON, InBodyForm:
		if strings.TrimSpace(f.Path) == "" {
			return fmt.Errorf("取值位置 %s 缺少 path", f.In)
		}
	case InRegex:
		re, err := regexp.Compile(f.Path)
		if err != nil {
			return fmt.Errorf("取值正则不合法：%w", err)
		}
		if re.NumSubexp() < 1 {
			return fmt.Errorf("取值正则至少需一个捕获组")
		}
		if f.RegexOn != RegexOnURL && f.RegexOn != RegexOnBody {
			return fmt.Errorf("regex 取值需指定 regex_on 为 url 或 body")
		}
	default:
		return fmt.Errorf("不支持的取值位置 %q", f.In)
	}
	return nil
}

// NeedsBody 表示取这个值需要请求正文。
func (f Field) NeedsBody() bool {
	return f.In == InBodyJSON || f.In == InBodyForm || (f.In == InRegex && f.RegexOn == RegexOnBody)
}

// Match 判断请求是否命中规则。规则不合法时视为不命中。
func (m Matcher) Match(r Request) bool {
	u, err := url.Parse(r.URL)
	if err != nil {
		return false
	}
	if m.Method != "" && !strings.EqualFold(m.Method, r.Method) {
		return false
	}
	if !HostMatches(m.Host, u.Hostname()) {
		return false
	}
	if m.PathRegex == "" {
		return true
	}
	re, err := regexp.Compile(m.PathRegex)
	if err != nil {
		return false
	}
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	return re.MatchString(path)
}

// HostMatches 判断 host 是否符合 pattern（精确或 *. 通配），不区分大小写。
func HostMatches(pattern, host string) bool {
	pattern, host = strings.ToLower(pattern), strings.ToLower(strings.TrimSuffix(host, "."))
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if suffix, ok := strings.CutPrefix(pattern, "*."); ok {
		return strings.HasSuffix(host, "."+suffix)
	}
	return host == pattern
}

// Extract 从请求里取值。没取到时 ok 为 false；规则本身不合法时返回 error。
func (f Field) Extract(r Request) (value string, ok bool, err error) {
	switch f.In {
	case InHeader:
		vs := r.Headers.Values(f.Path)
		if len(vs) == 0 {
			return "", false, nil
		}
		return vs[0], true, nil
	case InCookie:
		for _, line := range r.Headers.Values("Cookie") {
			cookies, err := http.ParseCookie(line)
			if err != nil {
				continue
			}
			for _, c := range cookies {
				if c.Name == f.Path {
					return c.Value, true, nil
				}
			}
		}
		return "", false, nil
	case InQuery:
		u, err := url.Parse(r.URL)
		if err != nil {
			return "", false, nil
		}
		q := u.Query()
		if !q.Has(f.Path) {
			return "", false, nil
		}
		return q.Get(f.Path), true, nil
	case InBodyJSON:
		return jsonPath(r.Body, f.Path)
	case InBodyForm:
		q, err := url.ParseQuery(string(r.Body))
		if err != nil || !q.Has(f.Path) {
			return "", false, nil
		}
		return q.Get(f.Path), true, nil
	case InRegex:
		re, err := regexp.Compile(f.Path)
		if err != nil {
			return "", false, err
		}
		subject := r.URL
		if f.RegexOn == RegexOnBody {
			subject = string(r.Body)
		}
		m := re.FindStringSubmatch(subject)
		if len(m) < 2 {
			return "", false, nil
		}
		return m[1], true, nil
	}
	return "", false, fmt.Errorf("不支持的取值位置 %q", f.In)
}

// jsonPath 按点路径取 JSON 里的值，数组用数字下标（如 data.list.0.id）。
// 字符串原样返回；数字、布尔原样输出 JSON 文本；对象和数组输出紧凑 JSON；null 视为没取到。
func jsonPath(body []byte, path string) (string, bool, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var cur any
	if err := dec.Decode(&cur); err != nil {
		return "", false, nil
	}
	for _, seg := range strings.Split(path, ".") {
		switch v := cur.(type) {
		case map[string]any:
			next, ok := v[seg]
			if !ok {
				return "", false, nil
			}
			cur = next
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(v) {
				return "", false, nil
			}
			cur = v[i]
		default:
			return "", false, nil
		}
	}
	switch v := cur.(type) {
	case nil:
		return "", false, nil
	case string:
		return v, true, nil
	case json.Number:
		return v.String(), true, nil
	case bool:
		return strconv.FormatBool(v), true, nil
	default:
		// 不做 HTML 转义：ferry.js 的 JSON.stringify 不转义 <、>、&，两边输出要一致。
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(v); err != nil {
			return "", false, err
		}
		return strings.TrimSuffix(buf.String(), "\n"), true, nil
	}
}
