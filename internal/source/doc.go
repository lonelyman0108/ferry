// Package source 定义来源的文档格式（接口和 YAML 导入导出共用），负责校验、存取和生成给脚本的规则。
package source

import (
	"fmt"
	"regexp"
	"strings"

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
)

// Doc 是一个来源的完整定义。
type Doc struct {
	Key         string `json:"key" yaml:"key"`
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
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

// ValidationError 汇总一份文档里的全部问题，方便界面一次提示完。
type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	return "来源定义有误：" + strings.Join(e.Problems, "；")
}

// Normalize 去掉首尾空白并填上默认值，校验前调用。
func (d *Doc) Normalize() {
	d.Key = strings.TrimSpace(d.Key)
	d.Name = strings.TrimSpace(d.Name)
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
	var p []string
	add := func(format string, args ...any) { p = append(p, fmt.Sprintf(format, args...)) }

	if !sourceKeyRe.MatchString(d.Key) {
		add("key 只能用小写字母、数字、- 和 _，以字母开头，最长 64 个字符")
	}
	if d.Name == "" || len(d.Name) > 128 {
		add("name 不能为空，且不超过 128 个字节")
	}
	switch d.Notify {
	case NotifyAlways, NotifyFirst, NotifyChanged:
	default:
		add("notify 只能是 always、first 或 changed")
	}
	if d.Review != ReviewAuto && d.Review != ReviewManual {
		add("review 只能是 auto 或 manual")
	}
	if *d.MergeWindow < 0 || *d.MergeWindow > maxMergeWindow {
		add("merge_window 应在 0 到 %d 秒之间", maxMergeWindow)
	}
	if *d.MinInterval < 0 || *d.MinInterval > maxMinInterval {
		add("min_interval 应在 0 到 %d 秒之间", maxMinInterval)
	}

	if len(d.Matchers) == 0 || len(d.Matchers) > maxMatchers {
		add("matchers 需 1 到 %d 条", maxMatchers)
	}
	matchers := map[string]bool{}
	for i, m := range d.Matchers {
		if !matcherRe.MatchString(m.Name) {
			add("matchers[%d].name 只能用小写字母、数字、- 和 _", i)
		} else if matchers[m.Name] {
			add("matchers[%d].name %q 重复", i, m.Name)
		}
		matchers[m.Name] = true
		if err := (rules.Matcher{Host: m.Host, PathRegex: m.Path, Method: m.Method}).Validate(); err != nil {
			add("matchers[%d]：%v", i, err)
		}
		switch m.Method {
		case "", "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
		default:
			add("matchers[%d].method %q 不是常见的 HTTP 方法", i, m.Method)
		}
	}

	if len(d.Fields) == 0 || len(d.Fields) > maxFields {
		add("fields 需 1 到 %d 个", maxFields)
	}
	fields := map[string]bool{}
	var fieldKeys []string
	for i, f := range d.Fields {
		if !fieldKeyRe.MatchString(f.Key) {
			add("fields[%d].key 只能用小写字母、数字和 _，以字母开头", i)
		} else if fields[f.Key] {
			add("fields[%d].key %q 重复", i, f.Key)
		}
		fields[f.Key] = true
		fieldKeys = append(fieldKeys, f.Key)
		if f.Matcher != "" && !matchers[f.Matcher] {
			add("fields[%d].matcher %q 不存在", i, f.Matcher)
		}
		if err := (rules.Field{In: f.In, Path: f.Path, RegexOn: f.RegexOn}).Validate(); err != nil {
			add("fields[%d]：%v", i, err)
		}
		switch f.Type {
		case TypeText, TypeJWT, TypeCookie, TypeJSON, TypeNumber, TypeBool:
		default:
			add("fields[%d].type %q 不支持", i, f.Type)
		}
		if f.MinLen < 0 || f.MaxLen < 0 || (f.MaxLen > 0 && f.MinLen > f.MaxLen) {
			add("fields[%d] 的 min_len、max_len 不合理", i)
		}
		if f.Pattern != "" {
			if _, err := regexp.Compile(f.Pattern); err != nil {
				add("fields[%d].pattern 不是合法的正则：%v", i, err)
			}
		}
	}
	if d.Probe != nil {
		if err := d.Probe.Validate(fieldKeys); err != nil {
			add("%v", err)
		}
	}
	if d.IdentityField != "" && !fields[d.IdentityField] {
		add("identity_field %q 不是已声明的字段", d.IdentityField)
	}

	if len(d.Outputs) > maxOutputs {
		add("outputs 最多 %d 个", maxOutputs)
	}
	outputs := map[string]bool{}
	for i, o := range d.Outputs {
		if !outputNameRe.MatchString(o.Name) {
			add("outputs[%d].name 只能用字母、数字和 _，不能以数字开头", i)
		} else if outputs[o.Name] {
			add("outputs[%d].name %q 重复", i, o.Name)
		}
		outputs[o.Name] = true
		t, err := rules.ParseTemplate(o.Template)
		if err != nil {
			add("outputs[%d]：%v", i, err)
		} else if err := t.CheckFields(fieldKeys); err != nil {
			add("outputs[%d] 引用了未声明的字段：%v", i, err)
		}
	}

	if len(p) > 0 {
		return &ValidationError{Problems: p}
	}
	return nil
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
