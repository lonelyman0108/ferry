// Package ghnet 封装面向 GitHub 域名的出站 HTTP 客户端，支持直连、镜像前缀与网络代理三种模式，
// 非 GitHub 域名的请求保持直连。
package ghnet

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/lonelyman0108/ferry/internal/secret"
	"github.com/lonelyman0108/ferry/internal/store"
)

// 网络模式。
const (
	ModeDirect = "direct"
	ModeMirror = "mirror"
	ModeProxy  = "proxy"
)

// settings 表中的键名与加密 AAD。
const (
	SettingMode         = "github.network_mode"
	SettingMirrorPrefix = "github.mirror_prefix"
	SettingProxyURL     = "github.proxy_url"
	proxySealAAD        = "settings/github.proxy_url"
)

// MirrorPreset 是预置的 GitHub 镜像前缀选项。
type MirrorPreset struct {
	Prefix string `json:"prefix"`
	Label  string `json:"label"`
	Note   string `json:"note"`
}

// Presets 是界面可选的预置镜像列表。
var Presets = []MirrorPreset{
	{
		Prefix: "https://ghfast.top",
		Label:  "ghfast.top",
		Note:   "支持 raw 文件、releases/latest 跳转与发布附件",
	},
	{
		Prefix: "https://ghproxy.net",
		Label:  "ghproxy.net",
		Note:   "支持 raw 文件、releases/latest 跳转与发布附件",
	},
	{
		Prefix: "https://gh-proxy.com",
		Label:  "gh-proxy.com",
		Note:   "支持 raw 文件与发布附件，不支持 releases/latest 跳转检查更新",
	},
}

// Config 是 GitHub 出站网络配置。
type Config struct {
	Mode         string `json:"mode"`
	MirrorPrefix string `json:"mirror_prefix,omitempty"`
	ProxyURL     string `json:"proxy_url,omitempty"`
}

// Normalize 去除首尾空白并填充默认模式。
func (c *Config) Normalize() {
	c.Mode = strings.ToLower(strings.TrimSpace(c.Mode))
	if c.Mode == "" {
		c.Mode = ModeDirect
	}
	c.MirrorPrefix = strings.TrimRight(strings.TrimSpace(c.MirrorPrefix), "/")
	c.ProxyURL = strings.TrimSpace(c.ProxyURL)
}

// Validate 校验网络配置。
func (c *Config) Validate() error {
	c.Normalize()
	switch c.Mode {
	case ModeDirect, ModeMirror, ModeProxy:
	default:
		return errors.New("mode 仅允许 direct、mirror 或 proxy")
	}

	if c.Mode == ModeMirror && c.MirrorPrefix == "" {
		return errors.New("请选择或填写镜像前缀地址")
	}
	if c.MirrorPrefix != "" {
		if err := ValidateMirrorPrefix(c.MirrorPrefix); err != nil {
			return err
		}
	}

	if c.Mode == ModeProxy && c.ProxyURL == "" {
		return errors.New("请填写网络代理地址")
	}
	if c.ProxyURL != "" {
		if err := ValidateProxyURL(c.ProxyURL); err != nil {
			return err
		}
	}
	return nil
}

// ValidateMirrorPrefix 校验镜像前缀：须为 https 地址，不含查询串或片段。
func ValidateMirrorPrefix(prefix string) error {
	prefix = strings.TrimRight(strings.TrimSpace(prefix), "/")
	u, err := url.Parse(prefix)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("镜像前缀须为 https:// 地址")
	}
	return nil
}

// ValidateProxyURL 校验网络代理地址：仅允许 http、https、socks5、socks5h 协议。
func ValidateProxyURL(raw string) error {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" {
		return errors.New("代理地址格式无效，须形如 http://host:port 或 socks5://host:port")
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "socks5", "socks5h":
		return nil
	default:
		return errors.New("代理地址仅支持 http://、https://、socks5:// 或 socks5h://")
	}
}

var proxyPasswordRe = regexp.MustCompile(`(^[a-zA-Z0-9]+://[^:/@]*):[^/]*@`)

// MaskProxyURL 将代理地址中的密码脱敏为 ••••，供接口返回给界面展示。
func MaskProxyURL(raw string) string {
	return proxyPasswordRe.ReplaceAllString(strings.TrimSpace(raw), "$1:••••@")
}

// ResolveProxyURL 若界面回传的代理地址与已保存地址脱敏后一致，则还原出未脱敏的原值。
func ResolveProxyURL(input, storedPlain string) string {
	input = strings.TrimSpace(input)
	if storedPlain != "" && input == MaskProxyURL(storedPlain) {
		return storedPlain
	}
	return input
}

// Masked 返回将代理密码脱敏、前缀规范化后的配置副本。
func (c Config) Masked() Config {
	c.MirrorPrefix = strings.TrimRight(strings.TrimSpace(c.MirrorPrefix), "/")
	c.ProxyURL = MaskProxyURL(c.ProxyURL)
	return c
}

// IsGitHubHost 判断域名是否属于 GitHub 相关域名。
func IsGitHubHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	if hostOnly, _, err := net.SplitHostPort(h); err == nil {
		h = hostOnly
	}
	switch h {
	case "github.com",
		"raw.githubusercontent.com",
		"objects.githubusercontent.com",
		"release-assets.githubusercontent.com",
		"codeload.github.com",
		"gist.githubusercontent.com",
		"api.github.com":
		return true
	}
	return false
}

// RewriteMirrorURL 在目标地址为 GitHub 域名时加上镜像前缀，非 GitHub 地址原样返回。
func RewriteMirrorURL(targetURL, mirrorPrefix string) string {
	mirrorPrefix = strings.TrimRight(strings.TrimSpace(mirrorPrefix), "/")
	if mirrorPrefix == "" {
		return targetURL
	}
	u, err := url.Parse(strings.TrimSpace(targetURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || !IsGitHubHost(u.Hostname()) {
		return targetURL
	}
	return mirrorPrefix + "/" + u.String()
}

// LoadConfig 从数据库的 settings 表读取 GitHub 网络配置，并解密代理地址。
func LoadConfig(ctx context.Context, db *gorm.DB, box *secret.Box) (Config, error) {
	var rows []store.Setting
	keys := []string{SettingMode, SettingMirrorPrefix, SettingProxyURL}
	if err := db.WithContext(ctx).Where("`key` IN ?", keys).Find(&rows).Error; err != nil {
		return Config{Mode: ModeDirect}, err
	}
	kv := make(map[string]string, len(rows))
	for _, r := range rows {
		kv[r.Key] = r.Value
	}
	cfg := Config{
		Mode:         kv[SettingMode],
		MirrorPrefix: kv[SettingMirrorPrefix],
	}
	rawProxy := kv[SettingProxyURL]
	if rawProxy != "" {
		if strings.HasPrefix(rawProxy, "gcm:") {
			if box != nil {
				if plain, err := box.Open(rawProxy, []byte(proxySealAAD)); err == nil {
					cfg.ProxyURL = string(plain)
				}
			}
		} else {
			cfg.ProxyURL = rawProxy
		}
	}
	cfg.Normalize()
	return cfg, nil
}

// SaveConfig 校验并保存 GitHub 网络配置到 settings 表，代理地址用主密钥加密存储。
func SaveConfig(ctx context.Context, db *gorm.DB, box *secret.Box, cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	sealedProxy := cfg.ProxyURL
	if sealedProxy != "" && box != nil {
		s, err := box.Seal([]byte(sealedProxy), []byte(proxySealAAD))
		if err != nil {
			return err
		}
		sealedProxy = s
	}
	now := time.Now()
	entries := []store.Setting{
		{Key: SettingMode, Value: cfg.Mode, UpdatedAt: now},
		{Key: SettingMirrorPrefix, Value: cfg.MirrorPrefix, UpdatedAt: now},
		{Key: SettingProxyURL, Value: sealedProxy, UpdatedAt: now},
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, e := range entries {
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "key"}},
				DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
			}).Create(&e).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

type githubRoundTripper struct {
	cfg           Config
	directRT      http.RoundTripper
	proxyRT       http.RoundTripper
	mirrorHost    string
	mirrorBaseURL *url.URL
}

func (rt *githubRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL == nil {
		return rt.directRT.RoundTrip(req)
	}
	host := req.URL.Hostname()

	switch rt.cfg.Mode {
	case ModeMirror:
		if IsGitHubHost(host) && rt.mirrorBaseURL != nil {
			rewritten, err := url.Parse(rt.cfg.MirrorPrefix + "/" + req.URL.String())
			if err == nil {
				clone := req.Clone(req.Context())
				clone.URL = rewritten
				clone.Host = rewritten.Host
				return rt.directRT.RoundTrip(clone)
			}
		}
		return rt.directRT.RoundTrip(req)

	case ModeProxy:
		// 仅 GitHub 域名走代理，其余域名直连。
		if IsGitHubHost(host) && rt.proxyRT != nil {
			return rt.proxyRT.RoundTrip(req)
		}
		return rt.directRT.RoundTrip(req)

	default:
		return rt.directRT.RoundTrip(req)
	}
}

// NewHTTPClient 构造按给定网络配置出站的 HTTP 客户端。base 为 nil 时使用标准传输层。
func NewHTTPClient(cfg Config, timeout time.Duration, base http.RoundTripper) (*http.Client, error) {
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	directRT := base
	if directRT == nil {
		directRT = &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          32,
			IdleConnTimeout:       60 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}
	}

	grt := &githubRoundTripper{
		cfg:      cfg,
		directRT: directRT,
	}

	if cfg.Mode == ModeMirror && cfg.MirrorPrefix != "" {
		u, err := url.Parse(cfg.MirrorPrefix)
		if err != nil {
			return nil, err
		}
		grt.mirrorBaseURL = u
		grt.mirrorHost = strings.ToLower(u.Hostname())
	}

	if cfg.Mode == ModeProxy && cfg.ProxyURL != "" {
		pu, err := url.Parse(cfg.ProxyURL)
		if err != nil {
			return nil, err
		}
		grt.proxyRT = &http.Transport{
			Proxy: func(r *http.Request) (*url.URL, error) {
				if IsGitHubHost(r.URL.Hostname()) {
					return pu, nil
				}
				return nil, nil
			},
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          16,
			IdleConnTimeout:       60 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}
	}

	return &http.Client{
		Timeout:   timeout,
		Transport: grt,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("重定向次数过多")
			}
			if (req.URL.Scheme != "http" && req.URL.Scheme != "https") || req.URL.Host == "" {
				return errors.New("不允许跳转到非 http/https 地址")
			}
			return nil
		},
	}, nil
}

// ExtractReleaseTag 从 releases/latest 的跳转地址中提取版本号 tag 与 Release 页面地址。
// 兼容 GitHub 直连（https://github.com/owner/repo/releases/tag/v0.1.0）
// 与镜像跳转（/https://github.com/owner/repo/releases/tag/v0.1.0 或 https://mirror/https://github.com/...）。
func ExtractReleaseTag(rawLocation, repo string) (tag, htmlURL string, ok bool) {
	loc := strings.TrimSpace(rawLocation)
	if loc == "" {
		return "", "", false
	}
	const marker = "/releases/tag/"
	idx := strings.LastIndex(loc, marker)
	if idx < 0 {
		return "", "", false
	}
	rest := loc[idx+len(marker):]
	if cut := strings.IndexAny(rest, "?#/"); cut >= 0 {
		rest = rest[:cut]
	}
	tag, err := url.PathUnescape(strings.TrimSpace(rest))
	if err != nil || tag == "" {
		return "", "", false
	}
	if repo != "" {
		htmlURL = "https://github.com/" + strings.Trim(repo, "/") + "/releases/tag/" + tag
	} else if ghIdx := strings.Index(loc, "https://github.com/"); ghIdx >= 0 {
		htmlURL = loc[ghIdx:]
	} else {
		htmlURL = loc
	}
	return tag, htmlURL, true
}
