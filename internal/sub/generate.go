package sub

import (
	"bytes"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/lonelyman0108/ferry/internal/source"
)

// 脚本在客户端里的执行超时（秒）和脚本文件的刷新间隔（秒）。回传本身在脚本里限时 3 秒。
const (
	scriptTimeout  = 10
	scriptInterval = 86400
	// maxBodySize 用于超限时只是不触发脚本的客户端（Stash、Egern）。
	maxBodySize = 1 << 20
)

// Params 是生成一份订阅所需的全部信息。
type Params struct {
	Name        string
	Client      string
	BaseURL     string
	FallbackURL string
	// ScriptURL 是自定义的脚本地址，为空时用 BaseURL 下的 /script/ferry.js。
	ScriptURL string
	Token     string
	// KID 不为空时是加密订阅：脚本参数只带密钥编号，不带令牌；密钥由用户填进模块参数。
	KID     string
	Sources []source.Doc
}

// trigger 是订阅里的一条脚本声明。脚本被触发后会用完整规则重新匹配，
// 所以这里只是“哪些请求要调起脚本、要不要带正文”。
type trigger struct {
	name    string
	pattern string
	body    bool
}

// Generate 生成订阅文件内容。
func Generate(p Params) ([]byte, error) {
	triggers := buildTriggers(p.Sources)
	hosts := source.Hostnames(p.Sources)
	arg := scriptArgument(p)
	script := ScriptURL(p.BaseURL, p.ScriptURL)
	title := "Ferry · " + p.Name
	desc := "本人账号登录态同步至自建 Ferry；回传时显示通知。"
	sealed := p.KID != ""
	if sealed && !SupportsSealed(p.Client) {
		return nil, fmt.Errorf("客户端 %q 不支持加密传输", p.Client)
	}
	// 加密订阅的传输密钥不写进文件：只声明一个空参数，由用户在客户端里填，脚本参数里引用它的占位符。
	switch p.Client {
	case Surge, Shadowrocket:
		if sealed {
			arg += "&key={{{key}}}"
		}
		return surge(title, desc, script, arg, triggers, hosts, sealed), nil
	case Loon:
		if sealed {
			return loonSealed(title, desc, script, p, triggers, hosts), nil
		}
		return loon(title, desc, script, arg, triggers, hosts), nil
	case QX:
		return qx(title, script, arg, triggers, hosts), nil
	case Stash:
		if sealed {
			arg += "&key=${args.key}"
		}
		return stash(title, desc, script, arg, triggers, hosts, sealed)
	case Egern:
		if sealed {
			arg += "&key={{{key}}}"
		}
		return egern(title, desc, script, arg, triggers, hosts, sealed)
	}
	return nil, fmt.Errorf("不支持的客户端 %q", p.Client)
}

// scriptArgument 是传给脚本的参数，query 格式，值都做了编码，不含逗号、引号和空格。
// 加密订阅不带令牌：拿到订阅文件的人也冒充不了手机，旧版脚本缺令牌时直接放行，不会误发明文。
func scriptArgument(p Params) string {
	v := url.Values{}
	v.Set("base", p.BaseURL)
	if p.KID != "" {
		v.Set("kid", p.KID)
		v.Set("mode", "sealed")
	} else {
		v.Set("token", p.Token)
	}
	if p.FallbackURL != "" {
		v.Set("fallback", p.FallbackURL)
	}
	return v.Encode()
}

func buildTriggers(docs []source.Doc) []trigger {
	var out []trigger
	seen := map[string]bool{}
	for _, d := range docs {
		if d.Enabled != nil && !*d.Enabled {
			continue
		}
		need := d.MatcherNeedsBody()
		for _, m := range d.Matchers {
			t := trigger{name: "ferry-" + d.Key + "-" + m.Name, pattern: URLPattern(m.Host, m.Path), body: need[m.Name]}
			k := fmt.Sprintf("%s|%v", t.pattern, t.body)
			if !seen[k] {
				seen[k] = true
				out = append(out, t)
			}
		}
	}
	// Surge、Loon 对一个请求只运行第一条命中的脚本：要正文的排前面，
	// 这样同时命中两条时脚本能拿到正文，一次取全所有字段。
	sort.SliceStable(out, func(i, j int) bool { return out[i].body && !out[j].body })
	return out
}

var trailingDollar = regexp.MustCompile(`([^\\]|^)\$$`)

// URLPattern 把域名和路径正则拼成客户端用的 URL 正则。
// 路径正则以 ^ 开头时接在域名后面；否则只按域名匹配，由脚本再按路径细分。
// 结尾的 $ 放宽为允许后面跟 query 或 #，因为客户端匹配的是整个 URL。
func URLPattern(host, path string) string {
	var h string
	if suffix, ok := strings.CutPrefix(host, "*."); ok {
		h = `[^/:?#]+\.` + regexp.QuoteMeta(suffix)
	} else {
		h = regexp.QuoteMeta(host)
	}
	p := `^https?://` + h + `(?::\d+)?`
	rest, anchored := strings.CutPrefix(path, "^")
	if !anchored || rest == "" {
		return p + `/`
	}
	if trailingDollar.MatchString(rest) {
		rest = rest[:len(rest)-1] + `(?:[?#].*)?$`
	}
	return p + rest
}

// keyHelp 是模块参数里传输密钥的说明。
const keyHelp = "传输密钥：在 Ferry 后台的订阅详情里复制，粘贴到这里。填好之前不回传。"

// surge 生成模块。max-size 设为 -1：Surge 的请求正文超过上限时会直接拒绝连接，会弄坏 App 本身的请求。
func surge(title, desc, script, arg string, ts []trigger, hosts []string, sealed bool) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "#!name=%s\n#!desc=%s\n", title, desc)
	if sealed {
		fmt.Fprintf(&b, "#!arguments=key:\n#!arguments-desc=%s\n", keyHelp)
	}
	b.WriteString("\n[Script]\n")
	for _, t := range ts {
		fmt.Fprintf(&b, "%s = type=http-request,pattern=%s,requires-body=%v,max-size=-1,timeout=%d,script-path=%s,script-update-interval=%d,argument=\"%s\"\n",
			t.name, t.pattern, t.body, scriptTimeout, script, scriptInterval, arg)
	}
	if len(hosts) > 0 {
		fmt.Fprintf(&b, "\n[MITM]\nhostname = %%APPEND%% %s\n", strings.Join(hosts, ", "))
	}
	return b.Bytes()
}

func loon(title, desc, script, arg string, ts []trigger, hosts []string) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "#!name = %s\n#!desc = %s\n#!author = Ferry\n\n[Script]\n", title, desc)
	for _, t := range ts {
		fmt.Fprintf(&b, "http-request %s script-path=%s, requires-body=%v, binary-body-mode=false, timeout=%d, tag=%s, argument=\"%s\"\n",
			t.pattern, script, t.body, scriptTimeout, t.name, arg)
	}
	if len(hosts) > 0 {
		fmt.Fprintf(&b, "\n[Mitm]\nhostname = %s\n", strings.Join(hosts, ", "))
	}
	return b.Bytes()
}

// loonSealed 生成加密订阅的 Loon 插件。Loon 的新参数语法只能把 [Argument] 里声明的参数整组传给脚本
// （$argument 是对象），所以平台地址、密钥编号这些固定值也声明成带默认值的参数。
func loonSealed(title, desc, script string, p Params, ts []trigger, hosts []string) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "#!name = %s\n#!desc = %s\n#!author = Ferry\n#!loon_version = 3.2.1(733)\n\n[Argument]\n", title, desc)
	fmt.Fprintf(&b, "key = input,\"\",tag=传输密钥,desc=%s\n", keyHelp)
	for _, a := range [][3]string{
		{"base", p.BaseURL, "平台地址"},
		{"fallback", p.FallbackURL, "备用地址"},
		{"kid", p.KID, "密钥编号"},
		{"mode", "sealed", "传输方式"},
	} {
		fmt.Fprintf(&b, "%s = input,\"%s\",tag=%s,desc=由 Ferry 生成，不要修改\n", a[0], a[1], a[2])
	}
	b.WriteString("\n[Script]\n")
	for _, t := range ts {
		fmt.Fprintf(&b, "http-request %s script-path=%s, requires-body=%v, binary-body-mode=false, timeout=%d, tag=%s, argument=[{base},{fallback},{kid},{mode},{key}]\n",
			t.pattern, script, t.body, scriptTimeout, t.name)
	}
	if len(hosts) > 0 {
		fmt.Fprintf(&b, "\n[Mitm]\nhostname = %s\n", strings.Join(hosts, ", "))
	}
	return b.Bytes()
}

// qx 生成重写片段。QX 没有 $argument，参数放在脚本地址的 # 后面，由脚本从 $environment.sourcePath 读取。
func qx(title, script, arg string, ts []trigger, hosts []string) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "# %s\n", title)
	if len(hosts) > 0 {
		fmt.Fprintf(&b, "hostname = %s\n", strings.Join(hosts, ", "))
	}
	for _, t := range ts {
		kind := "script-request-header"
		if t.body {
			kind = "script-request-body"
		}
		fmt.Fprintf(&b, "%s url %s %s#%s\n", t.pattern, kind, script, arg)
	}
	return b.Bytes()
}

type stashScript struct {
	Match       string `yaml:"match"`
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	RequireBody bool   `yaml:"require-body"`
	MaxSize     int    `yaml:"max-size"`
	Timeout     int    `yaml:"timeout"`
	Argument    string `yaml:"argument"`
}

type stashProvider struct {
	URL      string `yaml:"url"`
	Interval int    `yaml:"interval"`
}

// stashArgument 是 Stash 覆写参数的声明（iOS 3.7+），远程更新覆写时会保留用户填的值。
type stashArgument struct {
	Type        string `yaml:"type"`
	Title       string `yaml:"title"`
	Description string `yaml:"description"`
	Required    bool   `yaml:"required"`
}

func stash(title, desc, script, arg string, ts []trigger, hosts []string, sealed bool) ([]byte, error) {
	scripts := make([]stashScript, 0, len(ts))
	for _, t := range ts {
		scripts = append(scripts, stashScript{Match: t.pattern, Name: "ferry", Type: "request", RequireBody: t.body, MaxSize: maxBodySize, Timeout: scriptTimeout, Argument: arg})
	}
	doc := struct {
		Name             string                   `yaml:"name"`
		Desc             string                   `yaml:"desc"`
		ArgumentsVersion int                      `yaml:"arguments-version,omitempty"`
		Arguments        map[string]stashArgument `yaml:"arguments,omitempty"`
		HTTP             struct {
			MITM   []string      `yaml:"mitm"`
			Script []stashScript `yaml:"script"`
		} `yaml:"http"`
		Providers map[string]stashProvider `yaml:"script-providers"`
	}{Name: title, Desc: desc, Providers: map[string]stashProvider{"ferry": {URL: script, Interval: scriptInterval}}}
	doc.HTTP.MITM, doc.HTTP.Script = append([]string{}, hosts...), scripts
	if sealed {
		doc.ArgumentsVersion = 1
		doc.Arguments = map[string]stashArgument{"key": {Type: "input", Title: "传输密钥", Description: keyHelp, Required: true}}
	}
	return marshalYAML(doc)
}

type egernRequest struct {
	Name           string            `yaml:"name"`
	Match          string            `yaml:"match"`
	ScriptURL      string            `yaml:"script_url"`
	BodyRequired   bool              `yaml:"body_required"`
	MaxSize        int               `yaml:"max_size"`
	Timeout        int               `yaml:"timeout"`
	UpdateInterval int               `yaml:"update_interval"`
	Arguments      map[string]string `yaml:"arguments"`
}

// egern 生成原生模块。脚本按 Surge 写法（$request、$httpClient）编写，经 _compat.$argument 传参，
// 这种兼容写法来自社区模块，待真机确认，见 docs/clients.md 的待验证清单。
// 加密订阅用 compat_arguments 声明空的 key 参数，脚本参数里引用 {{{key}}}，由用户在模块引用处填值。
func egern(title, desc, script, arg string, ts []trigger, hosts []string, sealed bool) ([]byte, error) {
	type item struct {
		HTTPRequest egernRequest `yaml:"http_request"`
	}
	items := make([]item, 0, len(ts))
	for _, t := range ts {
		items = append(items, item{egernRequest{
			Name: t.name, Match: t.pattern, ScriptURL: script, BodyRequired: t.body, MaxSize: maxBodySize,
			Timeout: scriptTimeout, UpdateInterval: scriptInterval, Arguments: map[string]string{"_compat.$argument": arg},
		}})
	}
	doc := struct {
		Name                string            `yaml:"name"`
		Description         string            `yaml:"description"`
		CompatArguments     map[string]string `yaml:"compat_arguments,omitempty"`
		CompatArgumentsDesc string            `yaml:"compat_arguments_desc,omitempty"`
		Scriptings          []item            `yaml:"scriptings"`
		MITM                struct {
			Hostnames struct {
				Includes []string `yaml:"includes"`
			} `yaml:"hostnames"`
		} `yaml:"mitm"`
	}{Name: title, Description: desc, Scriptings: items}
	doc.MITM.Hostnames.Includes = append([]string{}, hosts...)
	if sealed {
		doc.CompatArguments, doc.CompatArgumentsDesc = map[string]string{"key": ""}, keyHelp
	}
	return marshalYAML(doc)
}

func marshalYAML(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
