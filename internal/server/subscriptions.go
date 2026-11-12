package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"github.com/lonelyman0108/ferry/internal/auth"
	"github.com/lonelyman0108/ferry/internal/secret"
	"github.com/lonelyman0108/ferry/internal/source"
	"github.com/lonelyman0108/ferry/internal/store"
	"github.com/lonelyman0108/ferry/internal/sub"
)

const (
	subscriptionTokenPrefix = "fs_"
	// subscriptionHeader 携带订阅令牌，手机端脚本拉规则、回传时使用。
	subscriptionHeader = "X-Ferry-Subscription"
)

func (s *Server) box() (*secret.Box, error) {
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()
	key, err := cfg.Key()
	if err != nil {
		return nil, err
	}
	return secret.New(key)
}

func idParam(r *http.Request) (uint, bool) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	return uint(id), err == nil
}

// ---- 访问地址 ----

type endpointInput struct {
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	Note    string `json:"note"`
}

func (in endpointInput) model() (store.Endpoint, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > 64 {
		return store.Endpoint{}, errors.New("名称不能为空，且不超过 64 个字节")
	}
	base, err := sub.NormalizeBaseURL(in.BaseURL)
	if err != nil {
		return store.Endpoint{}, err
	}
	return store.Endpoint{Name: name, BaseURL: base, Note: truncate(strings.TrimSpace(in.Note), 255)}, nil
}

type endpointView struct {
	ID      uint   `json:"id"`
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	Note    string `json:"note"`
}

func viewEndpoint(e store.Endpoint) endpointView {
	return endpointView{ID: e.ID, Name: e.Name, BaseURL: e.BaseURL, Note: e.Note}
}

func (s *Server) handleListEndpoints(w http.ResponseWriter, r *http.Request) {
	var es []store.Endpoint
	if err := s.store().DB.WithContext(r.Context()).Order("id").Find(&es).Error; err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]endpointView, 0, len(es))
	for _, e := range es {
		out = append(out, viewEndpoint(e))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateEndpoint(w http.ResponseWriter, r *http.Request) {
	var in endpointInput
	if !decodeJSON(w, r, &in) {
		return
	}
	e, err := in.model()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store().DB.WithContext(r.Context()).Create(&e).Error; err != nil {
		s.writeSaveError(w, r, err, "访问地址名称重复")
		return
	}
	s.audit(r, identityFrom(r.Context()).actor(), "endpoint.create", "endpoint:"+e.Name, e.BaseURL)
	writeJSON(w, http.StatusCreated, viewEndpoint(e))
}

func (s *Server) handleUpdateEndpoint(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "ID 不合法")
		return
	}
	var in endpointInput
	if !decodeJSON(w, r, &in) {
		return
	}
	e, err := in.model()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	db := s.store().DB.WithContext(r.Context())
	var old store.Endpoint
	if err := db.First(&old, id).Error; err != nil {
		s.writeFindError(w, r, err, "访问地址不存在")
		return
	}
	e.ID, e.CreatedAt = old.ID, old.CreatedAt
	if err := db.Save(&e).Error; err != nil {
		s.writeSaveError(w, r, err, "访问地址名称重复")
		return
	}
	s.audit(r, identityFrom(r.Context()).actor(), "endpoint.update", "endpoint:"+e.Name, e.BaseURL)
	writeJSON(w, http.StatusOK, viewEndpoint(e))
}

func (s *Server) handleDeleteEndpoint(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "ID 不合法")
		return
	}
	db := s.store().DB.WithContext(r.Context())
	var n int64
	if err := db.Model(&store.Subscription{}).Where("endpoint_id = ? OR fallback_id = ?", id, id).Count(&n).Error; err != nil {
		s.internalError(w, r, err)
		return
	}
	if n > 0 {
		writeError(w, http.StatusConflict, fmt.Sprintf("访问地址仍被 %d 个订阅引用，无法删除", n))
		return
	}
	var e store.Endpoint
	if err := db.First(&e, id).Error; err != nil {
		s.writeFindError(w, r, err, "访问地址不存在")
		return
	}
	if err := db.Delete(&e).Error; err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(r, identityFrom(r.Context()).actor(), "endpoint.delete", "endpoint:"+e.Name, "")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) writeFindError(w http.ResponseWriter, r *http.Request, err error, notFound string) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, notFound)
		return
	}
	s.internalError(w, r, err)
}

func (s *Server) writeSaveError(w http.ResponseWriter, r *http.Request, err error, duplicate string) {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		writeError(w, http.StatusConflict, duplicate)
		return
	}
	s.internalError(w, r, err)
}

// ---- 订阅 ----

type subscriptionInput struct {
	Name       string   `json:"name"`
	Client     string   `json:"client"`
	EndpointID uint     `json:"endpoint_id"`
	FallbackID *uint    `json:"fallback_id"`
	AllSources bool     `json:"all_sources"`
	SourceKeys []string `json:"source_keys"`
	// TransportMode 是 plain 或 sealed；不填时新建为 plain，修改时保持原样。
	TransportMode string `json:"transport_mode"`
	// ScriptURL 是自定义的 https 脚本地址，空串表示用平台地址。
	ScriptURL string `json:"script_url"`
}

type subscriptionLinks struct {
	SubscriptionURL string `json:"subscription_url"`
	InstallURL      string `json:"install_url"`
	ScriptURL       string `json:"script_url"`
}

type subscriptionView struct {
	ID            uint       `json:"id"`
	Name          string     `json:"name"`
	Client        string     `json:"client"`
	EndpointID    uint       `json:"endpoint_id"`
	FallbackID    *uint      `json:"fallback_id"`
	AllSources    bool       `json:"all_sources"`
	SourceKeys    []string   `json:"source_keys"`
	ScriptURL     string     `json:"script_url"`
	TokenPrefix   string     `json:"token_prefix"`
	LastFetchedAt *time.Time `json:"last_fetched_at"`
	// Status 是安装状态，取值见 subNotInstalled 等；Outdated 表示手机上那份订阅的 MITM 域名已经过时，要在客户端里更新。
	Status        string     `json:"status"`
	Outdated      bool       `json:"outdated"`
	FileFetchedAt *time.Time `json:"file_fetched_at"`
	// LastSeenAt 是手机最近一次连上平台的时间：拉规则，或者走加密通道。
	LastSeenAt *time.Time `json:"last_seen_at"`
	// Hostnames 是这个订阅现在的 MITM 域名。
	Hostnames []string          `json:"hostnames"`
	Links     subscriptionLinks `json:"links"`
	Transport transportView     `json:"transport"`
}

// 订阅的安装状态：没下载过订阅文件、也没连上过；下载过订阅文件；手机上的脚本连上过平台。
const (
	subNotInstalled = "not_installed"
	subInstalled    = "installed"
	subConnected    = "connected"
)

// mitmHash 是 MITM 域名集合的哈希：检查域名变化、记录手机上那份订阅时都用它。
func mitmHash(docs []source.Doc) string {
	sum := sha256.Sum256([]byte(strings.Join(source.Hostnames(docs), ",")))
	return hex.EncodeToString(sum[:])
}

// laterOf 返回两个时间里较晚的那个，都为空时返回 nil。
func laterOf(a, b *time.Time) *time.Time {
	if a == nil || (b != nil && b.After(*a)) {
		return b
	}
	return a
}

// transportView 是订阅的传输加密状态，不含密钥本身；密钥要单独调用 reveal 接口查看。
type transportView struct {
	Mode       string     `json:"mode"`
	KID        string     `json:"kid"`
	VerifiedAt *time.Time `json:"verified_at"`
	LastAt     *time.Time `json:"last_at"`
}

// apply 检查输入并把它写进 m（不含令牌）。
func (in subscriptionInput) apply(db *gorm.DB, m *store.Subscription) error {
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > 64 {
		return errors.New("名称不能为空，且不超过 64 个字节")
	}
	if !sub.ValidClient(in.Client) {
		return fmt.Errorf("客户端仅允许为 %s", strings.Join(sub.Clients, "、"))
	}
	var n int64
	if err := db.Model(&store.Endpoint{}).Where("id = ?", in.EndpointID).Count(&n).Error; err != nil || n == 0 {
		return errors.New("主访问地址不存在")
	}
	if in.FallbackID != nil {
		if *in.FallbackID == in.EndpointID {
			return errors.New("备用访问地址不能与主访问地址相同")
		}
		if err := db.Model(&store.Endpoint{}).Where("id = ?", *in.FallbackID).Count(&n).Error; err != nil || n == 0 {
			return errors.New("备用访问地址不存在")
		}
	}
	keys := []string{}
	if !in.AllSources {
		if len(in.SourceKeys) == 0 {
			return errors.New("未包含全部来源时需至少选择一个来源")
		}
		seen := map[string]bool{}
		for _, k := range in.SourceKeys {
			if !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
		var found int64
		// key 在 MySQL 里是保留字，用 map 条件让 GORM 按方言加引号。
		if err := db.Model(&store.Source{}).Where(map[string]any{"key": keys}).Count(&found).Error; err != nil {
			return err
		}
		if int(found) != len(keys) {
			return errors.New("部分所选来源不存在")
		}
	}
	mode := in.TransportMode
	if mode == "" {
		mode = m.TransportMode
	}
	if mode == "" {
		mode = store.TransportPlain
	}
	if mode != store.TransportPlain && mode != store.TransportSealed {
		return fmt.Errorf("传输方式仅允许为 %s 或 %s", store.TransportPlain, store.TransportSealed)
	}
	if mode == store.TransportSealed && !sub.SupportsSealed(in.Client) {
		return errors.New("该客户端不支持加密传输：无法通过模块参数配置传输密钥")
	}
	script, err := sub.NormalizeScriptURL(in.ScriptURL)
	if err != nil {
		return err
	}
	keysJSON, _ := json.Marshal(keys)
	m.ScriptURL = script
	m.Name, m.Client, m.EndpointID, m.FallbackID = name, in.Client, in.EndpointID, in.FallbackID
	m.AllSources, m.SourceKeys, m.TransportMode = in.AllSources, string(keysJSON), mode
	return nil
}

func subscriptionAAD(id uint) []byte {
	return []byte("subscriptions/" + strconv.FormatUint(uint64(id), 10) + "/token")
}

// issueToken 生成新令牌写进 m；m.ID 必须已经确定，因为密文绑定了行 ID。
func (s *Server) issueToken(m *store.Subscription) error {
	raw, hash, err := auth.NewToken(subscriptionTokenPrefix)
	if err != nil {
		return err
	}
	b, err := s.box()
	if err != nil {
		return err
	}
	sealed, err := b.Seal([]byte(raw), subscriptionAAD(m.ID))
	if err != nil {
		return err
	}
	m.TokenHash, m.TokenPrefix, m.TokenSealed = hash, raw[:len(subscriptionTokenPrefix)+6], sealed
	return nil
}

// viewSubscription 生成订阅的视图。all 是全部来源（预加载了子记录），为 nil 时现查；列表页传进来，免得每个订阅查一遍。
func (s *Server) viewSubscription(r *http.Request, m store.Subscription, all []store.Source) (subscriptionView, error) {
	var keys []string
	_ = json.Unmarshal([]byte(m.SourceKeys), &keys)
	if keys == nil {
		keys = []string{}
	}
	v := subscriptionView{
		ID: m.ID, Name: m.Name, Client: m.Client, EndpointID: m.EndpointID, FallbackID: m.FallbackID,
		AllSources: m.AllSources, SourceKeys: keys, ScriptURL: m.ScriptURL,
		TokenPrefix: m.TokenPrefix, LastFetchedAt: m.LastFetchedAt,
		Transport: transportView{Mode: m.TransportMode, VerifiedAt: m.TransportVerifiedAt, LastAt: m.TransportLastAt},
	}
	if m.TransportKID != nil {
		v.Transport.KID = *m.TransportKID
	}
	if all == nil {
		var err error
		if all, err = source.LoadAll(r.Context(), s.store().DB); err != nil {
			return v, err
		}
	}
	docs := subscriptionDocsFrom(all, m)
	v.Hostnames = source.Hostnames(docs)
	v.Outdated = m.FileMitmHash != "" && m.FileMitmHash != mitmHash(docs)
	v.FileFetchedAt = m.FileFetchedAt
	v.LastSeenAt = laterOf(m.LastFetchedAt, m.TransportLastAt)
	switch {
	case v.LastSeenAt != nil:
		v.Status = subConnected
	case m.FileFetchedAt != nil:
		v.Status = subInstalled
	default:
		v.Status = subNotInstalled
	}
	var e store.Endpoint
	if err := s.store().DB.WithContext(r.Context()).First(&e, m.EndpointID).Error; err != nil {
		return v, err
	}
	b, err := s.box()
	if err != nil {
		return v, err
	}
	token, err := b.Open(m.TokenSealed, subscriptionAAD(m.ID))
	if err != nil {
		return v, fmt.Errorf("解密订阅令牌：%w", err)
	}
	v.Links.SubscriptionURL = sub.SubscriptionURL(e.BaseURL, string(token), m.Client)
	v.Links.ScriptURL = sub.ScriptURL(e.BaseURL, m.ScriptURL)
	v.Links.InstallURL, err = sub.InstallURL(m.Client, "Ferry "+m.Name, v.Links.SubscriptionURL)
	return v, err
}

func (s *Server) handleListSubscriptions(w http.ResponseWriter, r *http.Request) {
	var ms []store.Subscription
	if err := s.store().DB.WithContext(r.Context()).Order("id").Find(&ms).Error; err != nil {
		s.internalError(w, r, err)
		return
	}
	all, err := source.LoadAll(r.Context(), s.store().DB)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]subscriptionView, 0, len(ms))
	for _, m := range ms {
		v, err := s.viewSubscription(r, m, all)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetSubscription(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "ID 不合法")
		return
	}
	var m store.Subscription
	if err := s.store().DB.WithContext(r.Context()).First(&m, id).Error; err != nil {
		s.writeFindError(w, r, err, "订阅不存在")
		return
	}
	v, err := s.viewSubscription(r, m, nil)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleCreateSubscription(w http.ResponseWriter, r *http.Request) {
	var in subscriptionInput
	if !decodeJSON(w, r, &in) {
		return
	}
	db := s.store().DB.WithContext(r.Context())
	var m store.Subscription
	if err := in.apply(db, &m); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		// 先用占位哈希建行拿到 ID，再生成绑定这个 ID 的令牌密文。
		placeholder, _, err := auth.NewToken("")
		if err != nil {
			return err
		}
		m.TokenHash, m.TokenPrefix, m.TokenSealed = auth.HashToken(placeholder), "-", "-"
		if err := tx.Create(&m).Error; err != nil {
			return err
		}
		if err := s.issueToken(&m); err != nil {
			return err
		}
		if m.TransportMode == store.TransportSealed {
			if err := s.issueTransportKey(&m); err != nil {
				return err
			}
		}
		return tx.Save(&m).Error
	})
	if err != nil {
		s.writeSaveError(w, r, err, "订阅名称重复")
		return
	}
	s.audit(r, identityFrom(r.Context()).actor(), "subscription.create", "subscription:"+m.Name, m.Client)
	s.checkMitm(r.Context())
	v, err := s.viewSubscription(r, m, nil)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

func (s *Server) handleUpdateSubscription(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "ID 不合法")
		return
	}
	var in subscriptionInput
	if !decodeJSON(w, r, &in) {
		return
	}
	db := s.store().DB.WithContext(r.Context())
	var m store.Subscription
	if err := db.First(&m, id).Error; err != nil {
		s.writeFindError(w, r, err, "订阅不存在")
		return
	}
	oldMode := m.TransportMode
	if err := in.apply(db, &m); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// 第一次开加密时生成密钥；切回明文再切回加密时沿用原来的密钥，手机上不用重填。
	if m.TransportMode == store.TransportSealed && m.TransportKeySealed == "" {
		if err := s.issueTransportKey(&m); err != nil {
			s.internalError(w, r, err)
			return
		}
	}
	if err := db.Save(&m).Error; err != nil {
		s.writeSaveError(w, r, err, "订阅名称重复")
		return
	}
	s.audit(r, identityFrom(r.Context()).actor(), "subscription.update", "subscription:"+m.Name, m.Client)
	if m.TransportMode != oldMode {
		s.audit(r, identityFrom(r.Context()).actor(), "subscription.transport_mode", "subscription:"+m.Name, m.TransportMode)
	}
	s.checkMitm(r.Context())
	v, err := s.viewSubscription(r, m, nil)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// handleRotateSubscription 换一个新令牌，旧的订阅链接立即失效，手机上要重新安装。
func (s *Server) handleRotateSubscription(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "ID 不合法")
		return
	}
	db := s.store().DB.WithContext(r.Context())
	var m store.Subscription
	if err := db.First(&m, id).Error; err != nil {
		s.writeFindError(w, r, err, "订阅不存在")
		return
	}
	if err := s.issueToken(&m); err != nil {
		s.internalError(w, r, err)
		return
	}
	if err := db.Save(&m).Error; err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(r, identityFrom(r.Context()).actor(), "subscription.rotate", "subscription:"+m.Name, "")
	v, err := s.viewSubscription(r, m, nil)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleDeleteSubscription(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "ID 不合法")
		return
	}
	db := s.store().DB.WithContext(r.Context())
	var m store.Subscription
	if err := db.First(&m, id).Error; err != nil {
		s.writeFindError(w, r, err, "订阅不存在")
		return
	}
	if err := db.Delete(&m).Error; err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(r, identityFrom(r.Context()).actor(), "subscription.delete", "subscription:"+m.Name, "")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- 手机端脚本用的接口 ----

type subscriptionKey struct{}

// authenticateSubscription 校验订阅令牌（请求头 X-Ferry-Subscription）。
func (s *Server) authenticateSubscription(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := r.Header.Get(subscriptionHeader)
		if raw == "" {
			writeError(w, http.StatusUnauthorized, "缺少订阅令牌")
			return
		}
		var m store.Subscription
		err := s.store().DB.WithContext(r.Context()).Where("token_hash = ?", auth.HashToken(raw)).First(&m).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(w, http.StatusUnauthorized, "订阅令牌无效，可能已被更换")
			return
		}
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		// 开了加密的订阅只认加密通道，免得令牌外泄后有人拿它走明文接口。
		if m.TransportMode == store.TransportSealed {
			writeError(w, http.StatusUnauthorized, "该订阅已开启加密传输，仅接受加密请求")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), subscriptionKey{}, m)))
	})
}

// subscriptionDocs 返回这个订阅能看到的来源（只含启用的会在生成规则时再过滤）。
func (s *Server) subscriptionDocs(r *http.Request, m store.Subscription) ([]source.Doc, error) {
	all, err := source.LoadAll(r.Context(), s.store().DB)
	if err != nil {
		return nil, err
	}
	return subscriptionDocsFrom(all, m), nil
}

// subscriptionDocsFrom 从已加载的全部来源里挑出这个订阅能看到的。
func subscriptionDocsFrom(all []store.Source, m store.Subscription) []source.Doc {
	allowed := map[string]bool{}
	if !m.AllSources {
		var keys []string
		_ = json.Unmarshal([]byte(m.SourceKeys), &keys)
		for _, k := range keys {
			allowed[k] = true
		}
	}
	var docs []source.Doc
	for _, x := range all {
		if m.AllSources || allowed[x.Key] {
			docs = append(docs, source.FromModel(x))
		}
	}
	return docs
}

// rulesReply 返回订阅能看到的规则和 ETag；etag 和 ifNoneMatch 相同时 changed 为 false，表示规则没变。
// 明文和加密两个入口共用。
func (s *Server) rulesReply(r *http.Request, m store.Subscription, ifNoneMatch string) (rules source.ScriptRules, etag string, changed bool, err error) {
	docs, err := s.subscriptionDocs(r, m)
	if err != nil {
		return rules, "", false, err
	}
	rules, etag = source.BuildScriptRules(docs)
	now := s.now()
	if m.LastFetchedAt == nil || now.Sub(*m.LastFetchedAt) > time.Minute {
		_ = s.store().DB.WithContext(r.Context()).Model(&m).Update("last_fetched_at", now).Error
	}
	return rules, etag, ifNoneMatch != etag, nil
}

// handleRules 给手机端脚本下发取值规则，支持 If-None-Match。
func (s *Server) handleRules(w http.ResponseWriter, r *http.Request) {
	m := r.Context().Value(subscriptionKey{}).(store.Subscription)
	rules, etag, changed, err := s.rulesReply(r, m, r.Header.Get("If-None-Match"))
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	w.Header().Set("ETag", etag)
	if !changed {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeJSON(w, http.StatusOK, rules)
}
