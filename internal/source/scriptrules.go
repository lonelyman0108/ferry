package source

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
)

// ScriptRules 是下发给手机端脚本的规则，只有取值需要的信息，不含输出模板和校验规则。
type ScriptRules struct {
	Sources []ScriptSource `json:"sources"`
}

// ScriptSource 是一个来源的脚本侧规则。
type ScriptSource struct {
	Key           string          `json:"key"`
	Name          string          `json:"name"`
	IdentityField string          `json:"identity_field,omitempty"`
	Notify        string          `json:"notify"`
	MinInterval   int             `json:"min_interval"`
	Debug         bool            `json:"debug,omitempty"`
	Matchers      []ScriptMatcher `json:"matchers"`
	Fields        []ScriptField   `json:"fields"`
}

// ScriptMatcher 是一条匹配规则，Body 表示这条规则需要请求正文。
type ScriptMatcher struct {
	Name   string `json:"name"`
	Host   string `json:"host"`
	Path   string `json:"path,omitempty"`
	Method string `json:"method,omitempty"`
	Body   bool   `json:"body,omitempty"`
}

// ScriptField 是一个字段的取值方式。
type ScriptField struct {
	Key     string `json:"key"`
	Matcher string `json:"matcher,omitempty"`
	In      string `json:"in"`
	Path    string `json:"path"`
	RegexOn string `json:"regex_on,omitempty"`
}

// BuildScriptRules 把启用的来源转成脚本规则，并算出 ETag（内容不变时 ETag 不变）。
func BuildScriptRules(docs []Doc) (ScriptRules, string) {
	r := ScriptRules{Sources: []ScriptSource{}}
	for _, d := range docs {
		if d.Enabled != nil && !*d.Enabled {
			continue
		}
		need := d.MatcherNeedsBody()
		s := ScriptSource{
			Key: d.Key, Name: d.Name, IdentityField: d.IdentityField, Notify: d.Notify, Debug: d.Debug,
			Matchers: []ScriptMatcher{}, Fields: []ScriptField{},
		}
		if d.MinInterval != nil {
			s.MinInterval = *d.MinInterval
		}
		for _, m := range d.Matchers {
			s.Matchers = append(s.Matchers, ScriptMatcher{Name: m.Name, Host: m.Host, Path: m.Path, Method: m.Method, Body: need[m.Name]})
		}
		for _, f := range d.Fields {
			s.Fields = append(s.Fields, ScriptField{Key: f.Key, Matcher: f.Matcher, In: f.In, Path: f.Path, RegexOn: f.RegexOn})
		}
		r.Sources = append(r.Sources, s)
	}
	sort.Slice(r.Sources, func(i, j int) bool { return r.Sources[i].Key < r.Sources[j].Key })
	b, _ := json.Marshal(r)
	sum := sha256.Sum256(b)
	return r, `"` + hex.EncodeToString(sum[:8]) + `"`
}

// Hostnames 返回启用的来源需要 MITM 的域名，去重排序；订阅生成和“MITM 已变化”提醒都用它。
func Hostnames(docs []Doc) []string {
	set := map[string]bool{}
	for _, d := range docs {
		if d.Enabled != nil && !*d.Enabled {
			continue
		}
		for _, m := range d.Matchers {
			set[strings.ToLower(m.Host)] = true
		}
	}
	out := make([]string, 0, len(set))
	for h := range set {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}
