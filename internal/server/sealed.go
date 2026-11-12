package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/lonelyman0108/ferry/internal/ingest"
	"github.com/lonelyman0108/ferry/internal/store"
	"github.com/lonelyman0108/ferry/internal/transport"
)

const (
	// kidHeader 携带传输密钥编号，手机端走加密通道时使用；编号不是秘密。
	kidHeader = "X-Ferry-Kid"
	// sealedWindow 是加密请求里时间戳允许的偏差。超出的请求按重放处理，nonce 记录也只需保留这么久。
	sealedWindow = 5 * time.Minute
	// sealedPad 把回复明文补齐到它的整数倍，减少按长度猜内容的余地。
	sealedPad = 256
)

// sealedRequest 是加密请求解开后的内容。
type sealedRequest struct {
	V int `json:"v"`
	// Op 是 ping、rules 或 sync。
	Op string `json:"op"`
	// TS 是手机上的毫秒时间戳。
	TS   int64           `json:"ts"`
	ETag string          `json:"etag,omitempty"`
	Body json.RawMessage `json:"body,omitempty"`
}

// sealedReply 是加密回复的内容。HTTP 状态码一律是 200，真实结果在 Status 里，外面看不出请求成没成功。
type sealedReply struct {
	V int `json:"v"`
	// Re 是对应请求的 nonce，手机端核对它，旧回复就没法拿来冒充。
	Re     string `json:"re"`
	Status int    `json:"status"`
	ETag   string `json:"etag,omitempty"`
	// Time 是平台的毫秒时间，只在时间超窗时给出，方便手机提示校时。
	Time int64 `json:"time,omitempty"`
	Body any   `json:"body,omitempty"`
}

func transportKeyAAD(id uint) []byte {
	return []byte("subscriptions/" + strconv.FormatUint(uint64(id), 10) + "/transport_key")
}

var kidEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// newKID 生成 16 个字符的密钥编号，只含小写字母和数字，能直接放进脚本参数。
func newKID() (string, error) {
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return strings.ToLower(kidEncoding.EncodeToString(b)), nil
}

// issueTransportKey 给 m 生成新的传输密钥，旧密钥立即失效；第一次生成时同时分配编号。
// m.ID 必须已经确定，因为密文绑定了行 ID。
func (s *Server) issueTransportKey(m *store.Subscription) error {
	key, err := transport.GenerateKey()
	if err != nil {
		return err
	}
	b, err := s.box()
	if err != nil {
		return err
	}
	sealed, err := b.Seal([]byte(key), transportKeyAAD(m.ID))
	if err != nil {
		return err
	}
	if m.TransportKID == nil {
		kid, err := newKID()
		if err != nil {
			return err
		}
		m.TransportKID = &kid
	}
	m.TransportKeySealed, m.TransportVerifiedAt = sealed, nil
	return nil
}

// transportKey 解出订阅的传输密钥文本。
func (s *Server) transportKey(m store.Subscription) (string, error) {
	b, err := s.box()
	if err != nil {
		return "", err
	}
	key, err := b.Open(m.TransportKeySealed, transportKeyAAD(m.ID))
	return string(key), err
}

// sealedSubscription 按密钥编号找到订阅和它的子密钥；编号不存在或从没生成过密钥时返回 gorm.ErrRecordNotFound。
func (s *Server) sealedSubscription(ctx context.Context, kid string) (store.Subscription, *transport.Keys, error) {
	var m store.Subscription
	if kid == "" {
		return m, nil, gorm.ErrRecordNotFound
	}
	if err := s.store().DB.WithContext(ctx).Where("transport_kid = ?", kid).First(&m).Error; err != nil {
		return m, nil, err
	}
	if m.TransportKeySealed == "" {
		return m, nil, gorm.ErrRecordNotFound
	}
	key, err := s.transportKey(m)
	if err != nil {
		return m, nil, err
	}
	keys, err := transport.ParseKey(key)
	return m, keys, err
}

// handleSealed 是加密通道的唯一入口：解开请求，检查时间窗和重放，按 Op 分发到和明文接口相同的逻辑，再加密回复。
func (s *Server) handleSealed(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "请求正文过大")
		return
	}
	kid := r.Header.Get(kidHeader)
	m, keys, err := s.sealedSubscription(r.Context(), kid)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		s.sealedUnauthorized(w, r, nil, kid)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	plain, nonce, err := keys.Open(transport.Up, string(raw))
	if err != nil {
		s.sealedUnauthorized(w, r, &m, kid)
		return
	}
	reply := func(rep sealedReply) { s.writeSealed(w, r, keys, nonce, rep) }
	fail := func(status int, msg string) { reply(sealedReply{Status: status, Body: errorBody{Error: msg}}) }

	var req sealedRequest
	if err := strictJSON(plain, &req); err != nil || req.V != 1 {
		fail(http.StatusBadRequest, "报文内容不合法")
		return
	}
	now := s.now()
	sent := time.UnixMilli(req.TS)
	if d := now.Sub(sent); d > sealedWindow || d < -sealedWindow {
		reply(sealedReply{Status: http.StatusConflict, Time: now.UnixMilli(), Body: errorBody{Error: "设备时间与服务端时间相差超过 5 分钟，请校准设备时间"}})
		return
	}
	db := s.store().DB.WithContext(r.Context())
	seen := store.TransportNonce{SubscriptionID: m.ID, Nonce: base64.RawURLEncoding.EncodeToString(nonce[:]), ExpiresAt: sent.Add(sealedWindow)}
	if err := db.Create(&seen).Error; errors.Is(err, gorm.ErrDuplicatedKey) {
		fail(http.StatusConflict, "重复请求")
		return
	} else if err != nil {
		s.sealedInternal(r, err, fail)
		return
	}
	s.markSealedSeen(r.Context(), m, now)
	if m.TransportMode != store.TransportSealed {
		// 订阅已切回明文：告诉手机清掉“只走加密”的记录，重新安装模块后改走明文。
		fail(http.StatusGone, "该订阅已切换为明文传输，请重新安装订阅")
		return
	}

	switch req.Op {
	case "ping":
		reply(sealedReply{Status: http.StatusOK, Body: map[string]bool{"ok": true}})
	case "rules":
		rules, etag, changed, err := s.rulesReply(r, m, req.ETag)
		if err != nil {
			s.sealedInternal(r, err, fail)
			return
		}
		if !changed {
			reply(sealedReply{Status: http.StatusNotModified, ETag: etag})
			return
		}
		reply(sealedReply{Status: http.StatusOK, ETag: etag, Body: rules})
	case "sync":
		var in ingest.SyncInput
		if err := strictJSON(req.Body, &in); err != nil {
			fail(http.StatusBadRequest, "回传内容不是合法的 JSON")
			return
		}
		status, body, err := s.syncReply(r, m, in)
		if err != nil {
			s.sealedInternal(r, err, fail)
			return
		}
		reply(sealedReply{Status: status, Body: body})
	default:
		fail(http.StatusBadRequest, "未知操作")
	}
}

// strictJSON 解码一个 JSON 对象，拒绝未知字段和多余内容（末尾的空白除外，回复补齐用的就是空格）。
func strictJSON(b []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return errors.New("仅允许包含一个 JSON 对象")
	}
	return nil
}

func (s *Server) sealedInternal(r *http.Request, err error, fail func(int, string)) {
	if errors.Is(err, context.Canceled) {
		return
	}
	slog.Error("处理加密请求出错", "path", r.URL.Path, "err", err)
	fail(http.StatusInternalServerError, "服务器内部错误")
}

// writeSealed 用回复方向的子密钥加密回复。明文先补齐到 sealedPad 的整数倍。
func (s *Server) writeSealed(w http.ResponseWriter, r *http.Request, keys *transport.Keys, re [transport.NonceSize]byte, rep sealedReply) {
	rep.V, rep.Re = 1, base64.RawURLEncoding.EncodeToString(re[:])
	b, err := json.Marshal(rep)
	if err == nil {
		var nonce [transport.NonceSize]byte
		if nonce, err = transport.NewNonce(); err == nil {
			b = append(b, strings.Repeat(" ", sealedPad-len(b)%sealedPad)...)
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = w.Write([]byte(keys.Seal(transport.Down, nonce, b)))
			return
		}
	}
	s.internalError(w, r, err)
}

// sealedUnauthorized 拒绝解不开的请求，不区分是编号不存在、被篡改还是密钥不对。
// 同一个编号（编号不存在时按来源 IP）每 10 分钟最多记一条审计，免得被刷屏。
func (s *Server) sealedUnauthorized(w http.ResponseWriter, r *http.Request, m *store.Subscription, kid string) {
	actor, target, key := "system", "kid:"+truncate(kid, 32), "ip:"+clientIP(r)
	if m != nil {
		actor, target, key = "subscription:"+strconv.FormatUint(uint64(m.ID), 10), "subscription:"+m.Name, "kid:"+kid
	}
	if s.sealedFailLimiter.allow(key, s.now()) {
		s.audit(r, actor, "transport.decrypt_failed", target, "")
	}
	writeError(w, http.StatusUnauthorized, "报文解密失败")
}

// markSealedSeen 记下手机上的密钥已填对，最近通信时间按分钟更新。
func (s *Server) markSealedSeen(ctx context.Context, m store.Subscription, now time.Time) {
	updates := map[string]any{}
	if m.TransportVerifiedAt == nil {
		updates["transport_verified_at"] = now
	}
	if m.TransportLastAt == nil || now.Sub(*m.TransportLastAt) > time.Minute {
		updates["transport_last_at"] = now
	}
	if len(updates) > 0 {
		_ = s.store().DB.WithContext(ctx).Model(&store.Subscription{}).Where("id = ?", m.ID).Updates(updates).Error
	}
}

// cleanupNonces 删除过期的 nonce 记录，返回删除的条数。
func (s *Server) cleanupNonces(ctx context.Context) (int64, error) {
	res := s.store().DB.WithContext(ctx).Where("expires_at < ?", s.now()).Delete(&store.TransportNonce{})
	return res.RowsAffected, res.Error
}

// ---- 管理接口：查看和更换传输密钥 ----

func (s *Server) sealedSubscriptionByID(w http.ResponseWriter, r *http.Request) (store.Subscription, bool) {
	var m store.Subscription
	id, ok := idParam(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "ID 不合法")
		return m, false
	}
	if err := s.store().DB.WithContext(r.Context()).First(&m, id).Error; err != nil {
		s.writeFindError(w, r, err, "订阅不存在")
		return m, false
	}
	if m.TransportMode != store.TransportSealed {
		writeError(w, http.StatusConflict, "该订阅未开启加密传输")
		return m, false
	}
	return m, true
}

// handleRevealTransportKey 返回传输密钥原文，给用户填进客户端的模块参数；每次查看都记审计。
func (s *Server) handleRevealTransportKey(w http.ResponseWriter, r *http.Request) {
	m, ok := s.sealedSubscriptionByID(w, r)
	if !ok {
		return
	}
	key, err := s.transportKey(m)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(r, identityFrom(r.Context()).actor(), "subscription.transport_key_reveal", "subscription:"+m.Name, "")
	writeJSON(w, http.StatusOK, map[string]string{"key": key})
}

// handleRotateTransportKey 换一把新的传输密钥，旧密钥立即失效，手机上要填入新密钥；编号不变。
func (s *Server) handleRotateTransportKey(w http.ResponseWriter, r *http.Request) {
	m, ok := s.sealedSubscriptionByID(w, r)
	if !ok {
		return
	}
	if err := s.issueTransportKey(&m); err != nil {
		s.internalError(w, r, err)
		return
	}
	if err := s.store().DB.WithContext(r.Context()).Save(&m).Error; err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(r, identityFrom(r.Context()).actor(), "subscription.transport_key_rotate", "subscription:"+m.Name, "")
	v, err := s.viewSubscription(r, m, nil)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
