// Package source 定义来源的文档格式（接口和 YAML 导入导出共用），负责校验、存取和生成给脚本的规则。
package source

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strings"
	"unicode"

	"github.com/lonelyman0108/ferry/internal/probe"
	"github.com/lonelyman0108/ferry/internal/rules"
)

// 值类型。
const (
	TypeText   = "text"
	TypeJWT    = "jwt"
	TypeCookie = "cookie"
	TypeJSON   = "json"
	TypeNumber = "number"
	TypeBool   = "bool"
)

// 手机端通知时机。默认每次回传都通知，见 AGENTS.md「使用范围」。
const (
	NotifyAlways  = "always"
	NotifyFirst   = "first"
	NotifyChanged = "changed"
)

// 新值生效方式。
const (
	ReviewAuto   = "auto"
	ReviewManual = "manual"
)

// 默认值与上限。
const (
	DefaultMergeWindow = 60
	DefaultMinInterval = 60
	maxMergeWindow     = 3600
	maxMinInterval     = 86400
	maxMatchers        = 20
	maxFields          = 50
	maxOutputs         = 20
	// MaxIconBytes 是自定义图标解码后的上限。
	MaxIconBytes = 64 << 10
)

// Doc 是一个来源的完整定义。
type Doc struct {
	Key         string `json:"key" yaml:"key"`
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	// Icon 是可选的自定义图标，只接受 base64 编码的图片 data URI（见 validateIcon），为空时界面显示名称首字。
	Icon string `json:"icon,omitempty" yaml:"icon,omitempty"`
	// Enabled 为 nil 时按启用处理。
	Enabled       *bool  `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	IdentityField string `json:"identity_field,omitempty" yaml:"identity_field,omitempty"`
	MergeWindow   *int   `json:"merge_window,omitempty" yaml:"merge_window,omitempty"`
	Notify        string `json:"notify,omitempty" yaml:"notify,omitempty"`
	MinInterval   *int   `json:"min_interval,omitempty" yaml:"min_interval,omitempty"`
	Debug         bool   `json:"debug,omitempty" yaml:"debug,omitempty"`
	// Review 为 manual 时新值要人工审核才生效。
	Review string `json:"review,omitempty" yaml:"review,omitempty"`
	// Probe 是可选的主动探测，批次关闭时用新值请求一次目标服务确认有效。
	Probe    *probe.Spec  `json:"probe,omitempty" yaml:"probe,omitempty"`
	Matchers []MatcherDoc `json:"matchers" yaml:"matchers"`
	Fields   []FieldDoc   `json:"fields" yaml:"fields"`
	Outputs  []OutputDoc  `json:"outputs,omitempty" yaml:"outputs,omitempty"`
}

// MatcherDoc 是一条请求匹配规则。
type MatcherDoc struct {
	Name   string `json:"name" yaml:"name"`
	Host   string `json:"host" yaml:"host"`
	Path   string `json:"path,omitempty" yaml:"path,omitempty"`
	Method string `json:"method,omitempty" yaml:"method,omitempty"`
}

// FieldDoc 是一个字段的取值方式和校验规则。
type FieldDoc struct {
	Key       string `json:"key" yaml:"key"`
	Name      string `json:"name,omitempty" yaml:"name,omitempty"`
	Matcher   string `json:"matcher,omitempty" yaml:"matcher,omitempty"`
	In        string `json:"in" yaml:"in"`
	Path      string `json:"path" yaml:"path"`
	RegexOn   string `json:"regex_on,omitempty" yaml:"regex_on,omitempty"`
	Type      string `json:"type,omitempty" yaml:"type,omitempty"`
	Sensitive bool   `json:"sensitive,omitempty" yaml:"sensitive,omitempty"`
	MinLen    int    `json:"min_len,omitempty" yaml:"min_len,omitempty"`
	MaxLen    int    `json:"max_len,omitempty" yaml:"max_len,omitempty"`
	Pattern   string `json:"pattern,omitempty" yaml:"pattern,omitempty"`
}

// OutputDoc 是一个输出变量。
type OutputDoc struct {
	Name     string `json:"name" yaml:"name"`
	Template string `json:"template" yaml:"template"`
}

var (
	sourceKeyRe = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
	// 字段 key 要能直接写进模板（{{.device_id}}），所以不允许连字符。
	fieldKeyRe   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	matcherRe    = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	outputNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
)

// Problem 是文档里的一个问题。Path 是出错的位置，写法同 JSON 路径（如 key、matchers[0].host、probe.url），
// 涉及整份文档时为空；Message 只说明问题本身、不含位置，界面把它显示在对应的输入框下方。
type Problem struct {
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

// String 返回带位置的完整说明，如「key 仅允许小写字母…」，供只显示文字的场合（YAML 导入、日志）。
func (p Problem) String() string {
	if p.Path == "" {
		return p.Message
	}
	return p.Path + " " + p.Message
}

// ValidationError 汇总一份文档里的全部问题，方便界面一次提示完。
type ValidationError struct {
	Problems []Problem
}

// Details 返回每个问题带位置的完整说明。
func (e *ValidationError) Details() []string {
	out := make([]string, len(e.Problems))
	for i, p := range e.Problems {
		out[i] = p.String()
	}
	return out
}

func (e *ValidationError) Error() string {
	return "来源定义有误：" + strings.Join(e.Details(), "；")
}

// partPath 在 base 后面接上 err 指向的那一项（err 是 *rules.PartError 时），得到具体的出错位置。
func partPath(base string, err error) string {
	var pe *rules.PartError
	if errors.As(err, &pe) {
		return base + "." + pe.Part
	}
	return base
}

// Normalize 去掉首尾空白并填上默认值，校验前调用。
func (d *Doc) Normalize() {
	d.Key = strings.TrimSpace(d.Key)
	d.Name = strings.TrimSpace(d.Name)
	d.Icon = strings.TrimSpace(d.Icon)
	d.IdentityField = strings.TrimSpace(d.IdentityField)
	if d.Enabled == nil {
		t := true
		d.Enabled = &t
	}
	if d.MergeWindow == nil {
		v := DefaultMergeWindow
		d.MergeWindow = &v
	}
	if d.MinInterval == nil {
		v := DefaultMinInterval
		d.MinInterval = &v
	}
	if d.Notify == "" {
		d.Notify = NotifyAlways
	}
	if d.Review == "" {
		d.Review = ReviewAuto
	}
	for i := range d.Matchers {
		m := &d.Matchers[i]
		m.Name, m.Host, m.Method = strings.TrimSpace(m.Name), strings.ToLower(strings.TrimSpace(m.Host)), strings.ToUpper(strings.TrimSpace(m.Method))
	}
	for i := range d.Fields {
		f := &d.Fields[i]
		f.Key, f.Matcher = strings.TrimSpace(f.Key), strings.TrimSpace(f.Matcher)
		if f.Type == "" {
			f.Type = TypeText
		}
	}
	for i := range d.Outputs {
		d.Outputs[i].Name = strings.TrimSpace(d.Outputs[i].Name)
	}
}

// Validate 检查文档，返回 *ValidationError 或 nil。调用前先 Normalize。
func (d *Doc) Validate() error {
	var p []Problem
	add := func(path, format string, args ...any) {
		p = append(p, Problem{Path: path, Message: fmt.Sprintf(format, args...)})
	}
	// nested 记下 rules、probe 返回的错误，位置细化到出错的那一项。
	nested := func(base string, err error) {
		p = append(p, Problem{Path: partPath(base, err), Message: err.Error()})
	}

	if !sourceKeyRe.MatchString(d.Key) {
		add("key", "仅允许小写字母、数字、- 和 _，须以字母开头，最长 64 个字符")
	}
	if d.Name == "" || len(d.Name) > 128 {
		add("name", "不能为空，且不超过 128 个字节")
	}
	if d.Icon != "" {
		if err := validateIcon(d.Icon); err != nil {
			add("icon", "%v", err)
		}
	}
	switch d.Notify {
	case NotifyAlways, NotifyFirst, NotifyChanged:
	default:
		add("notify", "仅允许 always、first 或 changed")
	}
	if d.Review != ReviewAuto && d.Review != ReviewManual {
		add("review", "仅允许 auto 或 manual")
	}
	if *d.MergeWindow < 0 || *d.MergeWindow > maxMergeWindow {
		add("merge_window", "应在 0 到 %d 秒之间", maxMergeWindow)
	}
	if *d.MinInterval < 0 || *d.MinInterval > maxMinInterval {
		add("min_interval", "应在 0 到 %d 秒之间", maxMinInterval)
	}

	if len(d.Matchers) == 0 || len(d.Matchers) > maxMatchers {
		add("matchers", "需 1 到 %d 条", maxMatchers)
	}
	matchers := map[string]bool{}
	for i, m := range d.Matchers {
		at := fmt.Sprintf("matchers[%d]", i)
		if !matcherRe.MatchString(m.Name) {
			add(at+".name", "仅允许小写字母、数字、- 和 _")
		} else if matchers[m.Name] {
			add(at+".name", "%q 与其他匹配规则重名", m.Name)
		}
		matchers[m.Name] = true
		if err := (rules.Matcher{Host: m.Host, PathRegex: m.Path, Method: m.Method}).Validate(); err != nil {
			nested(at, err)
		}
		switch m.Method {
		case "", "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
		default:
			add(at+".method", "%q 不是受支持的 HTTP 方法", m.Method)
		}
	}

	if len(d.Fields) == 0 || len(d.Fields) > maxFields {
		add("fields", "需 1 到 %d 个", maxFields)
	}
	fields := map[string]bool{}
	var fieldKeys []string
	for i, f := range d.Fields {
		at := fmt.Sprintf("fields[%d]", i)
		if !fieldKeyRe.MatchString(f.Key) {
			add(at+".key", "仅允许小写字母、数字和 _，须以字母开头")
		} else if fields[f.Key] {
			add(at+".key", "%q 与其他字段重复", f.Key)
		}
		fields[f.Key] = true
		fieldKeys = append(fieldKeys, f.Key)
		if f.Matcher != "" && !matchers[f.Matcher] {
			add(at+".matcher", "匹配规则 %q 不存在", f.Matcher)
		}
		if err := (rules.Field{In: f.In, Path: f.Path, RegexOn: f.RegexOn}).Validate(); err != nil {
			nested(at, err)
		}
		switch f.Type {
		case TypeText, TypeJWT, TypeCookie, TypeJSON, TypeNumber, TypeBool:
		default:
			add(at+".type", "不支持类型 %q", f.Type)
		}
		switch {
		case f.MinLen < 0:
			add(at+".min_len", "不能为负数")
		case f.MaxLen > 0 && f.MinLen > f.MaxLen:
			add(at+".min_len", "不能大于最大长度")
		}
		if f.MaxLen < 0 {
			add(at+".max_len", "不能为负数")
		}
		if f.Pattern != "" {
			if _, err := regexp.Compile(f.Pattern); err != nil {
				add(at+".pattern", "不是合法的正则：%v", err)
			}
		}
	}
	if d.Probe != nil {
		if err := d.Probe.Validate(fieldKeys); err != nil {
			nested("probe", err)
		}
	}
	if d.IdentityField != "" && !fields[d.IdentityField] {
		add("identity_field", "%q 不是已声明的字段", d.IdentityField)
	}

	if len(d.Outputs) > maxOutputs {
		add("outputs", "最多 %d 个", maxOutputs)
	}
	outputs := map[string]bool{}
	for i, o := range d.Outputs {
		at := fmt.Sprintf("outputs[%d]", i)
		if !outputNameRe.MatchString(o.Name) {
			add(at+".name", "仅允许字母、数字和 _，且不能以数字开头")
		} else if outputs[o.Name] {
			add(at+".name", "%q 与其他输出变量重名", o.Name)
		}
		outputs[o.Name] = true
		t, err := rules.ParseTemplate(o.Template)
		if err != nil {
			add(at+".template", "%v", err)
		} else if err := t.CheckFields(fieldKeys); err != nil {
			add(at+".template", "引用了未声明的字段：%v", err)
		}
	}

	if len(p) > 0 {
		return &ValidationError{Problems: p}
	}
	return nil
}

// 图标允许的图片类型。只收内嵌的 data URI，不收 http(s) 地址，管理界面不去加载第三方资源。
var iconTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/webp": true, "image/svg+xml": true}

// validateIcon 检查图标是 data:image/<类型>;base64,<数据>，类型在 iconTypes 里，解码后不超过 MaxIconBytes，
// 且内容和声明的类型一致（位图看文件头，SVG 要有 <svg 标签）。SVG 另见 checkSVG。
func validateIcon(icon string) error {
	rest, ok := strings.CutPrefix(icon, "data:")
	if !ok {
		return fmt.Errorf("仅支持 data:image/…;base64, 形式的图片，不支持网络地址")
	}
	mime, data, ok := strings.Cut(rest, ";base64,")
	if !ok || !iconTypes[mime] {
		return fmt.Errorf("仅支持 PNG、JPEG、WebP、SVG 图片，且须为 base64 编码")
	}
	if base64.StdEncoding.DecodedLen(len(data)) > MaxIconBytes+2 {
		return fmt.Errorf("图片超过 %d KB", MaxIconBytes>>10)
	}
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return fmt.Errorf("base64 数据无效")
	}
	if len(raw) == 0 {
		return fmt.Errorf("图片内容为空")
	}
	if len(raw) > MaxIconBytes {
		return fmt.Errorf("图片超过 %d KB", MaxIconBytes>>10)
	}
	if mime == "image/svg+xml" {
		if !bytes.Contains(raw, []byte("<svg")) {
			return fmt.Errorf("内容不是 SVG 图片")
		}
		if err := checkSVG(string(raw)); err != nil {
			return err
		}
	} else if http.DetectContentType(raw) != mime {
		return fmt.Errorf("内容与声明的类型 %s 不符", mime)
	}
	return nil
}

// SVG 图标的拒绝规则，都不区分大小写，属性名与等号之间允许空白。
var (
	// DOCTYPE 与实体声明（可引入外部实体）。
	svgDTDRe = regexp.MustCompile(`(?i)<!\s*(?:doctype|entity)`)
	// 脚本与可嵌入 HTML 的元素（含带命名空间前缀的写法，如 <svg:script>）、CSS 的 @import，
	// 以及可以直接写字符串地址、不经过 url() 的 CSS 图像函数（image-set("…")、image("…")、cross-fade("…")）。
	svgBannedRe = regexp.MustCompile(`(?i)<\s*(?:[\w.-]+:)?(?:script|foreignobject)|@import|\bimage(?:-set)?\s*\(|\bcross-fade\s*\(`)
	// on 开头的事件属性，如 onload=、onclick =。
	svgEventRe = regexp.MustCompile(`(?i)[\s"'/]on[a-z]+\s*=`)
	// href 与任意前缀的 href（xlink:href），取出属性值。
	svgHrefRe = regexp.MustCompile(`(?i)[\s"'/](?:[\w.-]+:)?href\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]*))`)
	// CSS 与属性里的 url(...)，取出括号里的值。
	svgURLRe = regexp.MustCompile(`(?i)url\s*\(\s*(?:"([^"]*)"|'([^']*)'|([^)]*))`)
	// 用动画改写 href（<animate attributeName="href" to="…">）也算外部引用。
	svgAnimHrefRe = regexp.MustCompile(`(?i)attributename\s*=\s*["']?\s*(?:[\w.-]+:)?href`)
	// 允许的引用：文档内的片段（#id），以及内嵌的位图（导出工具常把位图以 data URI 嵌进 SVG）。
	svgRefOKRe = regexp.MustCompile(`(?i)^(?:#|data:image/(?:png|jpeg|gif|webp);base64,)`)
)

// checkSVG 检查 SVG 是否含 DOCTYPE、脚本、事件属性或外部引用，有则返回给界面的说明。
// 界面用 <img> 显示图标时脚本本来不会执行，这里是纵深防御：来源定义可以导入导出，图标随定义流转到别处。
// 先解开字符引用（&#x6A; 之类），免得用编码绕过；href、xlink:href 与 url(...) 只允许 svgRefOKRe 里的两种。
func checkSVG(svg string) error {
	text := html.UnescapeString(svg)
	if svgDTDRe.MatchString(text) {
		return fmt.Errorf("SVG 图片不得包含 DOCTYPE 或 ENTITY 声明")
	}
	// 浏览器也会解开 CSS 转义（u\72l(、@\69mport），所以解开后再查一遍。不直接拒收反斜杠：
	// 有的编辑器会把 Windows 路径写进属性（如 inkscape:export-filename）。
	for _, t := range []string{text, cssUnescape(text)} {
		if svgUnsafe(t) {
			return fmt.Errorf("SVG 图片不得包含脚本、事件属性或外部引用")
		}
	}
	return nil
}

// svgUnsafe 报告 SVG 文本里是否有脚本、事件属性或外部引用。
func svgUnsafe(text string) bool {
	if svgBannedRe.MatchString(text) || svgEventRe.MatchString(text) || svgAnimHrefRe.MatchString(text) {
		return true
	}
	// 浏览器解析地址时会去掉其中的空白和控制字符，java&#9;script: 一样生效，所以去掉后再找。
	compact := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, text)
	if strings.Contains(compact, "javascript:") {
		return true
	}
	for _, re := range []*regexp.Regexp{svgHrefRe, svgURLRe} {
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			if !svgRefOKRe.MatchString(strings.TrimSpace(m[1] + m[2] + m[3])) {
				return true
			}
		}
	}
	return false
}

// cssUnescape 按 CSS 的规则解开反斜杠转义：\72 → r（十六进制最多 6 位，后面可跟一个空白），
// 反斜杠加其他字符得到该字符本身，反斜杠加换行是续行。
func cssUnescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			b.WriteByte(s[i])
			continue
		}
		var r rune
		n := 0
		for n < 6 && i+1+n < len(s) {
			v := hexValue(s[i+1+n])
			if v < 0 {
				break
			}
			r = r<<4 | rune(v)
			n++
		}
		if n == 0 {
			if i+1 < len(s) && s[i+1] != '\n' {
				b.WriteByte(s[i+1])
			}
			i++
			continue
		}
		if r == 0 || r > unicode.MaxRune || (r >= 0xD800 && r <= 0xDFFF) {
			r = unicode.ReplacementChar
		}
		b.WriteRune(r)
		i += n
		if i+1 < len(s) && strings.IndexByte(" \t\n\r\f", s[i+1]) >= 0 {
			i++
		}
	}
	return b.String()
}

func hexValue(c byte) int {
	switch {
	case '0' <= c && c <= '9':
		return int(c - '0')
	case 'a' <= c && c <= 'f':
		return int(c-'a') + 10
	case 'A' <= c && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

// MatcherNeedsBody 返回每条匹配规则是否需要请求正文：绑定在它上面、或没绑定规则的字段要从正文取值时就需要。
func (d *Doc) MatcherNeedsBody() map[string]bool {
	need := map[string]bool{}
	for _, f := range d.Fields {
		if !(rules.Field{In: f.In, Path: f.Path, RegexOn: f.RegexOn}).NeedsBody() {
			continue
		}
		if f.Matcher != "" {
			need[f.Matcher] = true
			continue
		}
		for _, m := range d.Matchers {
			need[m.Name] = true
		}
	}
	return need
}
