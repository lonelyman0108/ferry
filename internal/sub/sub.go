// Package sub 负责订阅：各客户端的文件格式、订阅和脚本地址、一键安装链接。
// 各家语法差异见 docs/clients.md。
package sub

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// 支持的客户端。Shadowrocket 的订阅文件和 Surge 相同，只是一键安装链接不同。
const (
	Surge        = "surge"
	Shadowrocket = "shadowrocket"
	Loon         = "loon"
	QX           = "qx"
	Stash        = "stash"
	Egern        = "egern"
)

// Clients 按界面展示顺序列出全部客户端。
var Clients = []string{Surge, Shadowrocket, Loon, QX, Stash, Egern}

var fileNames = map[string]string{
	Surge:        "ferry.sgmodule",
	Shadowrocket: "ferry.sgmodule",
	Loon:         "ferry.plugin",
	QX:           "ferry.snippet",
	Stash:        "ferry.stoverride",
	Egern:        "ferry.yaml",
}

// SupportsSealed 判断客户端能否用加密传输：传输密钥要由用户填进模块参数，不能写进订阅文件。
// QX 没有模块参数，只能把参数写进订阅里的脚本地址，所以不支持。
func SupportsSealed(client string) bool {
	return ValidClient(client) && client != QX
}

// ValidClient 判断客户端名是否支持。
func ValidClient(c string) bool {
	_, ok := fileNames[c]
	return ok
}

// FileName 是订阅文件名；客户端会按扩展名识别格式，所以订阅地址以它结尾。
func FileName(client string) string { return fileNames[client] }

// NormalizeBaseURL 校验访问地址并去掉末尾的斜杠。允许带路径（放在反向代理的子路径下时）。
func NormalizeBaseURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errors.New("访问地址需以 http:// 或 https:// 开头并包含主机名")
	}
	if u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return "", errors.New("访问地址不能带 query、# 或用户名密码")
	}
	return strings.TrimRight(u.String(), "/"), nil
}

// SubscriptionURL 是订阅文件的地址。
func SubscriptionURL(base, token, client string) string {
	return base + "/sub/" + token + "/" + FileName(client)
}

// ScriptURL 是通用脚本的地址，脚本本身不含任何令牌。custom 不为空时用它。
func ScriptURL(base, custom string) string {
	if custom != "" {
		return custom
	}
	return base + "/script/ferry.js"
}

// NormalizeScriptURL 校验自定义的脚本地址：只接受 https，不能带 #（QX 用 # 后面传参）。空串表示不自定义。
func NormalizeScriptURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Fragment != "" || strings.Contains(raw, "#") || u.User != nil {
		return "", errors.New("脚本地址需以 https:// 开头，且不能带 # 或用户名密码")
	}
	if strings.ContainsAny(raw, "\",\n ") {
		return "", errors.New("脚本地址不能含逗号、引号或空白")
	}
	return raw, nil
}

// InstallURL 是客户端的一键安装链接，在手机上点开后由客户端确认安装或更新。
func InstallURL(client, name, subURL string) (string, error) {
	q := url.QueryEscape
	switch client {
	case Surge:
		return "surge:///install-module?url=" + q(subURL), nil
	case Shadowrocket:
		return "shadowrocket://install?module=" + q(subURL), nil
	case Loon:
		return "loon://import?plugin=" + q(subURL), nil
	case QX:
		res, err := json.Marshal(map[string][]string{
			"rewrite_remote": {subURL + ", tag=" + name + ", update-interval=86400, opt-parser=false, enabled=true"},
		})
		if err != nil {
			return "", err
		}
		return "quantumult-x:///add-resource?remote-resource=" + q(string(res)), nil
	case Stash:
		return "stash://install-override?url=" + q(subURL), nil
	case Egern:
		return "egern:/modules/new?name=" + q(name) + "&url=" + q(subURL), nil
	}
	return "", fmt.Errorf("不支持的客户端 %q", client)
}
