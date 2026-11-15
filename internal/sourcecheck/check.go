// Package sourcecheck 检查来源定义是否符合分享与导入的安全规则，并校验仓库测试样例。
package sourcecheck

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/lonelyman0108/ferry/internal/rules"
	"github.com/lonelyman0108/ferry/internal/source"
	"github.com/lonelyman0108/ferry/internal/validate"
)

// 规则问题编码。
const (
	CodeBroadHost         = "broad_host"
	CodeProbeHostMismatch = "probe_host_mismatch"
	CodeDirKeyMismatch    = "dir_key_mismatch"
	CodeMissingReadme     = "missing_readme"
	CodeMissingCases      = "missing_cases"
	CodeCaseFailed        = "case_failed"
	CodeNotifyNotAlways   = "notify_not_always"
	CodeDebugEnabled      = "debug_enabled"
	CodeHighEntropySample = "high_entropy_sample"
)

// Issue 是规则检查发现的一项违规或警告。
type Issue struct {
	Code    string `json:"code"`
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

func (i Issue) String() string {
	if i.Path == "" {
		return i.Message
	}
	return i.Path + "：" + i.Message
}

// Report 是单个来源定义的规则检查结论。
type Report struct {
	Violations []Issue  `json:"violations,omitempty"`
	Warnings   []Issue  `json:"warnings,omitempty"`
	Hosts      []string `json:"hosts,omitempty"`
	ProbeHost  string   `json:"probe_host,omitempty"`
}

// HasViolations 报告是否存在违反硬规则的条目。
func (r Report) HasViolations() bool {
	return len(r.Violations) > 0
}

// 常见的二级公共后缀前缀（搭配两字母国家或地区顶级域名，如 com.cn、co.uk）。
var secondLevelGenericLabels = map[string]bool{
	"ac": true, "co": true, "com": true, "edu": true, "go": true,
	"gov": true, "idv": true, "me": true, "mil": true, "ne": true,
	"net": true, "nom": true, "or": true, "org": true, "per": true,
}

// 显式禁止作为泛域名后缀的多级公共后缀与多租户托管平台域名。
var bannedWildcardSuffixes = map[string]bool{
	"eu.org":                true,
	"github.io":             true,
	"githubusercontent.com": true,
	"pages.dev":             true,
	"workers.dev":           true,
	"vercel.app":            true,
	"netlify.app":           true,
	"herokuapp.com":         true,
	"appspot.com":           true,
	"cloudfunctions.net":    true,
	"run.app":               true,
	"web.app":               true,
	"firebaseapp.com":       true,
	"azurewebsites.net":     true,
	"cloudfront.net":        true,
	"amazonaws.com":         true,
	"aliyuncs.com":          true,
	"myqcloud.com":          true,
	"baidubce.com":          true,
	"huaweicloud.com":       true,
	"volces.com":            true,
}

// Check 检查来源定义的硬规则与软警告。
func Check(d source.Doc) Report {
	d.Normalize()
	var r Report
	seenHosts := map[string]bool{}
	for i, m := range d.Matchers {
		h := strings.ToLower(strings.TrimSpace(m.Host))
		if h != "" && !seenHosts[h] {
			seenHosts[h] = true
			r.Hosts = append(r.Hosts, h)
		}
		if broad, reason := IsBroadHostPattern(h); broad {
			r.Violations = append(r.Violations, Issue{
				Code:    CodeBroadHost,
				Path:    fmt.Sprintf("matchers[%d].host", i),
				Message: reason,
			})
		}
	}

	if d.Probe != nil {
		probeHost, issues := checkProbeHosts(d)
		r.ProbeHost = probeHost
		r.Violations = append(r.Violations, issues...)
	}

	if d.Notify != "" && d.Notify != source.NotifyAlways {
		r.Warnings = append(r.Warnings, Issue{
			Code:    CodeNotifyNotAlways,
			Path:    "notify",
			Message: "回传通知未设为「每次回传」，部分回传不会在手机端弹出通知",
		})
	}
	if d.Debug {
		r.Warnings = append(r.Warnings, Issue{
			Code:    CodeDebugEnabled,
			Path:    "debug",
			Message: "已开启调试模式，未命中的请求也会上报调试信息",
		})
	}
	return r
}

// IsBroadHostPattern 判断匹配域名是否过于宽泛（如 *、*.com、*.com.cn、*.github.io）。
func IsBroadHostPattern(host string) (bool, string) {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false, ""
	}
	if host == "*" || host == "*." {
		return true, "不允许使用通配符匹配任意域名"
	}
	if strings.HasPrefix(host, "*.") {
		rest := strings.TrimPrefix(host, "*.")
		if !strings.Contains(rest, ".") {
			return true, fmt.Sprintf("泛域名 %q 过于宽泛，不允许直接匹配顶级域名", host)
		}
		if bannedWildcardSuffixes[rest] {
			return true, fmt.Sprintf("泛域名 %q 属于公共托管或共享后缀，范围过于宽泛", host)
		}
		parts := strings.Split(rest, ".")
		if len(parts) == 2 && len(parts[1]) == 2 && secondLevelGenericLabels[parts[0]] {
			return true, fmt.Sprintf("泛域名 %q 过于宽泛，不允许直接匹配公共后缀", host)
		}
	}
	return false, ""
}

var templateTagRe = regexp.MustCompile(`\{\{[^}]*\}\}`)

func checkProbeHosts(d source.Doc) (string, []Issue) {
	var issues []Issue
	rawURL := strings.TrimSpace(d.Probe.URL)
	if rawURL == "" {
		return "", nil
	}

	benignVals := map[string]string{}
	advVals := map[string]string{}
	for _, f := range d.Fields {
		benignVals[f.Key] = "ferrycheck"
		advVals[f.Key] = "untrusted.invalid/x?"
	}

	authorityPart, hasScheme := extractAuthorityPart(rawURL)
	preURL := replacePathTemplates(rawURL)
	preHost := parseHostname(preURL)

	var benignHost, advHost string
	if t, err := rules.ParseTemplate(rawURL); err == nil {
		if rendered, err := t.Render(benignVals); err == nil {
			benignHost = parseHostname(rendered)
		}
		if rendered, err := t.Render(advVals); err == nil {
			advHost = parseHostname(rendered)
		}
	} else {
		benignHost = preHost
		advHost = preHost
	}

	probeHost := benignHost
	if probeHost == "" {
		probeHost = preHost
	}

	switch {
	case !hasScheme || strings.Contains(authorityPart, "{{") || (benignHost != "" && advHost != benignHost) || (preHost != "" && benignHost != preHost):
		issues = append(issues, Issue{
			Code:    CodeProbeHostMismatch,
			Path:    "probe.url",
			Message: "探测地址的域名部分不允许使用模板变量，以免展开后超出匹配域名范围",
		})
	case probeHost == "":
		issues = append(issues, Issue{
			Code:    CodeProbeHostMismatch,
			Path:    "probe.url",
			Message: "探测地址缺少有效域名",
		})
	case !matchesAnyMatcher(d.Matchers, probeHost):
		issues = append(issues, Issue{
			Code:    CodeProbeHostMismatch,
			Path:    "probe.url",
			Message: fmt.Sprintf("探测域名 %q 不在匹配规则声明的域名范围内", probeHost),
		})
	}

	for k, v := range d.Probe.Headers {
		if !strings.EqualFold(strings.TrimSpace(k), "Host") && !strings.EqualFold(strings.TrimSpace(k), ":authority") {
			continue
		}
		val := strings.TrimSpace(v)
		if strings.Contains(val, "{{") {
			issues = append(issues, Issue{
				Code:    CodeProbeHostMismatch,
				Path:    "probe.headers",
				Message: fmt.Sprintf("探测请求头 %s 不允许使用模板变量", k),
			})
			continue
		}
		hdrHost := stripPort(val)
		if hdrHost == "" || !matchesAnyMatcher(d.Matchers, hdrHost) {
			issues = append(issues, Issue{
				Code:    CodeProbeHostMismatch,
				Path:    "probe.headers",
				Message: fmt.Sprintf("探测请求头 %s 的域名 %q 不在匹配规则声明的域名范围内", k, hdrHost),
			})
		}
	}

	return probeHost, issues
}

// extractAuthorityPart 取出 URL 中 scheme:// 之后、路径/查询/片段之前的部分。
func extractAuthorityPart(raw string) (string, bool) {
	lower := strings.ToLower(raw)
	var rest string
	switch {
	case strings.HasPrefix(lower, "https://"):
		rest = raw[len("https://"):]
	case strings.HasPrefix(lower, "http://"):
		rest = raw[len("http://"):]
	default:
		return raw, false
	}
	if idx := strings.IndexAny(rest, "/?#"); idx >= 0 {
		return rest[:idx], true
	}
	return rest, true
}

// replacePathTemplates 把 URL 路径、查询与片段中的模板占位符替换成固定字符，保留域名部分原样。
func replacePathTemplates(raw string) string {
	lower := strings.ToLower(raw)
	prefixLen := 0
	switch {
	case strings.HasPrefix(lower, "https://"):
		prefixLen = len("https://")
	case strings.HasPrefix(lower, "http://"):
		prefixLen = len("http://")
	default:
		return raw
	}
	rest := raw[prefixLen:]
	idx := strings.IndexAny(rest, "/?#")
	if idx < 0 {
		return raw
	}
	return raw[:prefixLen+idx] + templateTagRe.ReplaceAllString(rest[idx:], "x")
}

func parseHostname(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func stripPort(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return strings.ToLower(h)
	}
	return strings.ToLower(hostport)
}

func matchesAnyMatcher(matchers []source.MatcherDoc, host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	for _, m := range matchers {
		pattern := strings.ToLower(strings.TrimSpace(m.Host))
		if pattern == "" {
			continue
		}
		if broad, _ := IsBroadHostPattern(pattern); broad {
			continue
		}
		if rules.HostMatches(pattern, host) {
			return true
		}
	}
	return false
}

// CaseFile 是 hub 仓库中 cases.json 的顶层结构。
type CaseFile struct {
	Cases []TestCase `json:"cases"`
}

// TestCase 是一条脱敏请求样例与期望提取出的字段值。
type TestCase struct {
	Name     string            `json:"name"`
	Matcher  string            `json:"matcher,omitempty"`
	Request  CaseRequest       `json:"request"`
	Expected map[string]string `json:"expected"`
}

// CaseRequest 是样例中的模拟 HTTP 请求。
type CaseRequest struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
}

// CheckCases 用来源定义试跑 cases.json，校验匹配规则、字段提取、字段静态校验与覆盖率，并检查疑似未脱敏的高熵字符串。
func CheckCases(d source.Doc, data []byte) ([]Issue, []Issue) {
	d.Normalize()
	var violations, warnings []Issue
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return []Issue{{Code: CodeMissingCases, Path: "cases.json", Message: "cases.json 内容为空"}}, nil
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	var cf CaseFile
	if err := dec.Decode(&cf); err != nil {
		return []Issue{{Code: CodeCaseFailed, Path: "cases.json", Message: fmt.Sprintf("cases.json 解析失败：%v", err)}}, nil
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return []Issue{{Code: CodeCaseFailed, Path: "cases.json", Message: "cases.json 末尾存在多余内容"}}, nil
	}
	if len(cf.Cases) == 0 {
		return []Issue{{Code: CodeMissingCases, Path: "cases.json", Message: "cases.json 至少包含 1 条测试样例"}}, nil
	}

	declaredFields := map[string]source.FieldDoc{}
	for _, f := range d.Fields {
		declaredFields[f.Key] = f
	}
	coveredFields := map[string]bool{}

	for i, tc := range cf.Cases {
		at := fmt.Sprintf("cases[%d]", i)
		if strings.TrimSpace(tc.Name) != "" {
			at = fmt.Sprintf("cases[%d](%s)", i, strings.TrimSpace(tc.Name))
		}
		if len(tc.Expected) == 0 {
			violations = append(violations, Issue{
				Code:    CodeCaseFailed,
				Path:    at + ".expected",
				Message: "expected 不能为空",
			})
			continue
		}

		hdr := http.Header{}
		for k, v := range tc.Request.Headers {
			hdr.Add(k, v)
			if looksLikeRealSecret(v) {
				warnings = append(warnings, Issue{
					Code:    CodeHighEntropySample,
					Path:    fmt.Sprintf("%s.request.headers[%s]", at, k),
					Message: "样例请求头包含高熵字符串，请确认已替换为脱敏虚构值",
				})
			}
		}
		if looksLikeRealSecret(tc.Request.Body) {
			warnings = append(warnings, Issue{
				Code:    CodeHighEntropySample,
				Path:    at + ".request.body",
				Message: "样例请求正文包含高熵字符串，请确认已替换为脱敏虚构值",
			})
		}
		req := rules.Request{
			Method:  strings.ToUpper(strings.TrimSpace(tc.Request.Method)),
			URL:     strings.TrimSpace(tc.Request.URL),
			Headers: hdr,
			Body:    []byte(tc.Request.Body),
		}

		matchedNames := map[string]bool{}
		for _, m := range d.Matchers {
			rm := rules.Matcher{Host: m.Host, PathRegex: m.Path, Method: m.Method}
			if rm.Match(req) {
				matchedNames[m.Name] = true
			}
		}
		if len(matchedNames) == 0 {
			violations = append(violations, Issue{
				Code:    CodeCaseFailed,
				Path:    at + ".request",
				Message: "样例请求未命中任何匹配规则",
			})
			continue
		}
		if tc.Matcher != "" && !matchedNames[tc.Matcher] {
			violations = append(violations, Issue{
				Code:    CodeCaseFailed,
				Path:    at + ".matcher",
				Message: fmt.Sprintf("样例请求未命中指定的匹配规则 %q", tc.Matcher),
			})
			continue
		}

		for k, want := range tc.Expected {
			f, ok := declaredFields[k]
			if !ok {
				violations = append(violations, Issue{
					Code:    CodeCaseFailed,
					Path:    fmt.Sprintf("%s.expected.%s", at, k),
					Message: fmt.Sprintf("字段 %q 未在来源中声明", k),
				})
				continue
			}
			if f.Matcher != "" && !matchedNames[f.Matcher] {
				violations = append(violations, Issue{
					Code:    CodeCaseFailed,
					Path:    fmt.Sprintf("%s.expected.%s", at, k),
					Message: fmt.Sprintf("字段 %q 绑定的匹配规则 %q 未命中该请求", k, f.Matcher),
				})
				continue
			}
			rf := rules.Field{In: f.In, Path: f.Path, RegexOn: f.RegexOn}
			got, extracted, err := rf.Extract(req)
			if err != nil {
				violations = append(violations, Issue{
					Code:    CodeCaseFailed,
					Path:    fmt.Sprintf("%s.expected.%s", at, k),
					Message: fmt.Sprintf("提取字段 %q 出错：%v", k, err),
				})
				continue
			}
			if !extracted {
				violations = append(violations, Issue{
					Code:    CodeCaseFailed,
					Path:    fmt.Sprintf("%s.expected.%s", at, k),
					Message: fmt.Sprintf("未能从样例请求中提取出字段 %q", k),
				})
				continue
			}
			if got != want {
				violations = append(violations, Issue{
					Code:    CodeCaseFailed,
					Path:    fmt.Sprintf("%s.expected.%s", at, k),
					Message: fmt.Sprintf("字段 %q 提取结果为 %q，与期望值 %q 不符", k, got, want),
				})
				continue
			}
			// 对非 JWT 字段（或无需当前时间的校验）执行静态校验。
			if f.Type != source.TypeJWT {
				if res := validate.Check(f, got, time.Now()); res.Err != nil {
					violations = append(violations, Issue{
						Code:    CodeCaseFailed,
						Path:    fmt.Sprintf("%s.expected.%s", at, k),
						Message: fmt.Sprintf("字段 %q 的期望值未通过校验：%v", k, res.Err),
					})
					continue
				}
			}
			if looksLikeRealSecret(want) {
				warnings = append(warnings, Issue{
					Code:    CodeHighEntropySample,
					Path:    fmt.Sprintf("%s.expected.%s", at, k),
					Message: fmt.Sprintf("字段 %q 的样例值熵值较高，请确认已替换为脱敏虚构值", k),
				})
			}
			coveredFields[k] = true
		}
	}

	for _, f := range d.Fields {
		if !coveredFields[f.Key] {
			violations = append(violations, Issue{
				Code:    CodeCaseFailed,
				Path:    "cases.json",
				Message: fmt.Sprintf("字段 %q 未被任何测试样例覆盖", f.Key),
			})
		}
	}
	return violations, warnings
}

var safeSampleMarkers = []string{
	"example", "sample", "redacted", "dummy", "mock", "test", "ferry",
	"demo", "placeholder", "fake", "0000", "1234", "abcd", "xxxx",
}

func looksLikeRealSecret(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 28 {
		return false
	}
	lower := strings.ToLower(s)
	for _, m := range safeSampleMarkers {
		if strings.Contains(lower, m) {
			return false
		}
	}
	// 挑选最长的连续 token 片段计算香农熵。
	for _, tok := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == ';' || r == '&' || r == '"' || r == '\'' || r == ',' || r == ':' || r == '='
	}) {
		if len(tok) < 28 {
			continue
		}
		if shannonEntropy(tok) >= 4.2 && hasMixedCharClasses(tok) {
			return true
		}
	}
	return false
}

func shannonEntropy(s string) float64 {
	if len(s) == 0 {
		return 0
	}
	counts := map[byte]int{}
	for i := 0; i < len(s); i++ {
		counts[s[i]]++
	}
	n := float64(len(s))
	var ent float64
	for _, c := range counts {
		p := float64(c) / n
		ent -= p * math.Log2(p)
	}
	return ent
}

func hasMixedCharClasses(s string) bool {
	var hasLower, hasUpper, hasDigit bool
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case 'a' <= c && c <= 'z':
			hasLower = true
		case 'A' <= c && c <= 'Z':
			hasUpper = true
		case '0' <= c && c <= '9':
			hasDigit = true
		}
	}
	return hasLower && hasUpper && hasDigit
}

// TargetResult 是 CLI 或 CI 检查单个目标文件/目录的完整结果。
type TargetResult struct {
	Target   string           `json:"target"`
	Key      string           `json:"key,omitempty"`
	Name     string           `json:"name,omitempty"`
	Version  int              `json:"version,omitempty"`
	Problems []source.Problem `json:"problems,omitempty"`
	Report   Report           `json:"report"`
	Err      error            `json:"-"`
}

// OK 报告该目标是否无任何解析错误、定义错误与硬规则违规。
func (tr TargetResult) OK() bool {
	return tr.Err == nil && len(tr.Problems) == 0 && !tr.Report.HasViolations()
}

// CheckTarget 检查单个文件或目录。hubMode 为 true 时同时校验目录名、README.md 与 cases.json。
func CheckTarget(target string, hubMode bool) ([]TargetResult, error) {
	info, err := os.Stat(target)
	if err != nil {
		return nil, fmt.Errorf("无法访问 %s：%v", target, err)
	}
	sourceFile := target
	dir := filepath.Dir(target)
	if info.IsDir() {
		dir = target
		sourceFile = filepath.Join(target, "source.json")
	}

	data, err := os.ReadFile(sourceFile)
	if err != nil {
		return nil, fmt.Errorf("读取 %s 失败：%v", sourceFile, err)
	}

	if hubMode {
		env, err := source.DecodeSingleEnvelope(data)
		if err != nil {
			return []TargetResult{{Target: sourceFile, Err: err}}, nil
		}
		tr := evaluateEnvelope(sourceFile, env)
		absDir, err := filepath.Abs(dir)
		if err == nil {
			dirBase := filepath.Base(absDir)
			if dirBase != "." && dirBase != "/" && env.Source.Key != "" && dirBase != env.Source.Key {
				tr.Report.Violations = append(tr.Report.Violations, Issue{
					Code:    CodeDirKeyMismatch,
					Path:    "key",
					Message: fmt.Sprintf("目录名 %q 与来源标识 %q 不一致", dirBase, env.Source.Key),
				})
			}
		}
		readmePath := filepath.Join(dir, "README.md")
		if readmeBytes, err := os.ReadFile(readmePath); err != nil || len(bytes.TrimSpace(readmeBytes)) == 0 {
			tr.Report.Violations = append(tr.Report.Violations, Issue{
				Code:    CodeMissingReadme,
				Path:    "README.md",
				Message: "缺少说明文档 README.md 或内容为空",
			})
		}
		casesPath := filepath.Join(dir, "cases.json")
		if casesBytes, err := os.ReadFile(casesPath); err != nil {
			tr.Report.Violations = append(tr.Report.Violations, Issue{
				Code:    CodeMissingCases,
				Path:    "cases.json",
				Message: "缺少测试样例文件 cases.json",
			})
		} else {
			v, w := CheckCases(env.Source, casesBytes)
			tr.Report.Violations = append(tr.Report.Violations, v...)
			tr.Report.Warnings = append(tr.Report.Warnings, w...)
		}
		return []TargetResult{tr}, nil
	}

	envs, err := source.DecodeEnvelopes(data)
	if err != nil {
		return []TargetResult{{Target: sourceFile, Err: err}}, nil
	}
	results := make([]TargetResult, 0, len(envs))
	for i, env := range envs {
		label := sourceFile
		if len(envs) > 1 {
			label = fmt.Sprintf("%s[%d]", sourceFile, i)
		}
		results = append(results, evaluateEnvelope(label, env))
	}
	return results, nil
}

func evaluateEnvelope(label string, env source.Envelope) TargetResult {
	tr := TargetResult{
		Target:  label,
		Key:     env.Source.Key,
		Name:    env.Source.Name,
		Version: env.Meta.Version,
	}
	if err := env.Source.Validate(); err != nil {
		var ve *source.ValidationError
		if errors.As(err, &ve) {
			tr.Problems = ve.Problems
		} else {
			tr.Err = err
		}
	}
	tr.Report = Check(env.Source)
	return tr
}
