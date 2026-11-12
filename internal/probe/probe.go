// Package probe 用新取到的值主动请求一次目标服务，按判定表达式确认登录态是否有效。
//
// 请求的 URL、请求头和正文是输出模板（{{.field}}），判定和取身份用 expr 表达式，
// 可用的变量有 status、headers（键是小写）、body 和 json（正文能解析成 JSON 时）。
package probe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"

	"github.com/lonelyman0108/ferry/internal/rules"
)

const (
	defaultTimeout = 5 * time.Second
	maxTimeout     = 30 * time.Second
	maxBody        = 1 << 20
)

// Spec 是来源上的探测配置。
type Spec struct {
	Method  string            `json:"method,omitempty" yaml:"method,omitempty"`
	URL     string            `json:"url" yaml:"url"`
	Headers map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
	Body    string            `json:"body,omitempty" yaml:"body,omitempty"`
	// Expect 是判定表达式，结果必须是布尔值，例如 status == 200 && json.code == 0。
	Expect string `json:"expect" yaml:"expect"`
	// Identity 是可选的取身份表达式，例如 json.data.userId；结果会转成字符串作为身份键。
	Identity string `json:"identity,omitempty" yaml:"identity,omitempty"`
	// Timeout 是超时秒数，默认 5，最多 30。
	Timeout int `json:"timeout,omitempty" yaml:"timeout,omitempty"`
}

// Result 是一次探测的结论。Err 不为 nil 表示没能完成探测（网络错误等），这时 OK 没有意义。
type Result struct {
	OK       bool
	Identity string
	Detail   string
	Err      error
}

// sampleEnv 是编译表达式时用的环境。json 用空 map，表达式里才能写 json.code 这样的访问；
// 运行时正文不是 JSON 对象时，这类访问会执行出错，探测按“出错”处理。
func sampleEnv() map[string]any {
	return map[string]any{"status": 0, "headers": map[string]string{}, "body": "", "json": map[string]any{}}
}

// Validate 检查配置，fields 是来源已声明的字段 key。返回的错误是 *rules.PartError，指向出错的那一项
// （method、url、headers、body、expect、identity 或 timeout）。
func (s Spec) Validate(fields []string) error {
	switch strings.ToUpper(s.Method) {
	case "", "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD":
	default:
		return rules.Invalid("method", "有效性检测不支持 method %q", s.Method)
	}
	if !strings.HasPrefix(s.URL, "http://") && !strings.HasPrefix(s.URL, "https://") {
		return rules.Invalid("url", "有效性检测的 url 须以 http:// 或 https:// 开头")
	}
	type tmpl struct{ part, name, text string }
	templates := []tmpl{{"url", "url", s.URL}, {"body", "body", s.Body}}
	keys := make([]string, 0, len(s.Headers))
	for k := range s.Headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		templates = append(templates, tmpl{"headers", "headers." + k, s.Headers[k]})
	}
	for _, x := range templates {
		if x.text == "" {
			continue
		}
		t, err := rules.ParseTemplate(x.text)
		if err == nil {
			err = t.CheckFields(fields)
		}
		if err != nil {
			return rules.Invalid(x.part, "有效性检测的 %s：%w", x.name, err)
		}
	}
	if strings.TrimSpace(s.Expect) == "" {
		return rules.Invalid("expect", "有效性检测缺少成功条件 expect")
	}
	if _, err := expr.Compile(s.Expect, expr.Env(sampleEnv()), expr.AsBool()); err != nil {
		return rules.Invalid("expect", "成功条件表达式错误：%w", err)
	}
	if s.Identity != "" {
		if _, err := expr.Compile(s.Identity, expr.Env(sampleEnv())); err != nil {
			return rules.Invalid("identity", "身份表达式错误：%w", err)
		}
	}
	if s.Timeout < 0 || time.Duration(s.Timeout)*time.Second > maxTimeout {
		return rules.Invalid("timeout", "有效性检测的 timeout 应在 0 到 %d 秒之间", int(maxTimeout/time.Second))
	}
	return nil
}

func render(text string, values map[string]string) (string, error) {
	if text == "" {
		return "", nil
	}
	t, err := rules.ParseTemplate(text)
	if err != nil {
		return "", err
	}
	return t.Render(values)
}

// Run 用 values 渲染请求并发出，按 Expect 判定。client 为 nil 时用默认客户端。
func Run(ctx context.Context, client *http.Client, s Spec, values map[string]string) Result {
	if client == nil {
		client = http.DefaultClient
	}
	timeout := defaultTimeout
	if s.Timeout > 0 {
		timeout = time.Duration(s.Timeout) * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	rawURL, err := render(s.URL, values)
	if err != nil {
		return Result{Err: err}
	}
	if u, err := url.Parse(rawURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return Result{Err: errors.New("渲染后的 url 不合法")}
	}
	body, err := render(s.Body, values)
	if err != nil {
		return Result{Err: err}
	}
	method := strings.ToUpper(s.Method)
	if method == "" {
		method = http.MethodGet
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, strings.NewReader(body))
	if err != nil {
		return Result{Err: err}
	}
	for k, v := range s.Headers {
		hv, err := render(v, values)
		if err != nil {
			return Result{Err: err}
		}
		if strings.EqualFold(k, "Host") {
			req.Host = hv
		} else {
			req.Header.Set(k, hv)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		// 不带出完整地址：探测 URL 是渲染出来的，里面可能有登录态。
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return Result{Err: fmt.Errorf("请求失败：%w", err)}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return Result{Err: fmt.Errorf("读取响应失败：%w", err)}
	}

	env := sampleEnv()
	env["status"] = resp.StatusCode
	hs := map[string]string{}
	for k := range resp.Header {
		hs[strings.ToLower(k)] = resp.Header.Get(k)
	}
	env["headers"], env["body"] = hs, string(data)
	var parsed any
	if json.Unmarshal(data, &parsed) == nil {
		env["json"] = parsed
	}

	out, err := expr.Eval(s.Expect, env)
	if err != nil {
		return Result{Err: fmt.Errorf("成功条件执行失败：%w", err)}
	}
	ok, isBool := out.(bool)
	if !isBool {
		return Result{Err: errors.New("成功条件的结果不是布尔值")}
	}
	res := Result{OK: ok, Detail: fmt.Sprintf("HTTP %d", resp.StatusCode)}
	if !ok {
		res.Detail = "检测未通过（HTTP " + fmt.Sprint(resp.StatusCode) + "）"
	}
	if ok && s.Identity != "" {
		prog, err := expr.Compile(s.Identity, expr.Env(env))
		if err == nil {
			var v any
			if v, err = vm.Run(prog, env); err == nil && v != nil {
				res.Identity = strings.TrimSpace(fmt.Sprint(v))
			}
		}
		if err != nil {
			res.Detail += "；身份表达式执行失败，沿用原身份"
		}
	}
	return res
}
