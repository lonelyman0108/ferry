package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/lonelyman0108/ferry/internal/events"
	"github.com/lonelyman0108/ferry/internal/store"
)

// Format 把事件写成通知文案，只用事件里的名字和字段名。
func Format(e events.Event) Message {
	src := e.SourceName
	if src == "" {
		src = e.SourceKey
	}
	who := src
	if e.IdentityKey != "" {
		who += " · " + e.IdentityKey
	}
	fields := strings.Join(e.Fields, "、")
	m := Message{Event: e.Type, At: e.At, Link: e.Link}
	switch e.Type {
	case events.IdentityNew:
		m.Title, m.Body = "Ferry：发现新身份", who
	case events.ValueUpdated:
		m.Title, m.Body = "Ferry：登录态已更新", who+"："+fields
		if e.Detail != "" {
			m.Body += "（" + e.Detail + "）"
		}
	case events.ValueRejected:
		m.Title, m.Body = "Ferry：新值被驳回", who+"："+fields
		if e.Detail != "" {
			m.Body += "\n原因：" + e.Detail
		}
	case events.ValuePendingReview:
		m.Title, m.Body = "Ferry：新值待审核", who+"："+fields
		if e.Detail != "" {
			m.Body += "\n" + e.Detail
		}
	case events.PushFailed:
		m.Title, m.Body = "Ferry：推送失败", who+"："+fields+"\n"+e.Detail
	case events.ValueExpiring:
		m.Title, m.Body = "Ferry：登录态即将过期", who+"："+fields
		if e.ExpiresAt != nil {
			m.Body += "\n将于 " + e.ExpiresAt.Local().Format("01-02 15:04") + " 过期，请在 App 中刷新登录态"
		}
	case events.MitmChanged:
		m.Title = "Ferry：订阅待更新"
		m.Body = "订阅「" + e.Subject + "」的 MITM 域名已变更，点击此通知可在客户端中更新订阅"
	default:
		m.Title, m.Body = "Ferry："+e.Type, who
	}
	return m
}

// Service 按规则分发通知。Decrypt 解密渠道配置，由调用方注入。
type Service struct {
	DB      *gorm.DB
	HTTP    *http.Client
	Decrypt func(store.Channel) (Config, error)
	// Audit 记录每次发送的结果（只有渠道名和成败，不含内容里的敏感信息）。
	Audit func(action, target, detail string)
}

func decodeList[T any](raw string) []T {
	var out []T
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

// channelsFor 返回事件要发往的渠道（去重）。
func (s *Service) channelsFor(ctx context.Context, e events.Event) ([]store.Channel, error) {
	var rules []store.NotifyRule
	if err := s.DB.WithContext(ctx).Where("enabled = ?", true).Find(&rules).Error; err != nil {
		return nil, err
	}
	want := map[uint]bool{}
	for _, r := range rules {
		match := false
		for _, t := range decodeList[string](r.Events) {
			match = match || t == e.Type
		}
		if !match {
			continue
		}
		if keys := decodeList[string](r.SourceKeys); len(keys) > 0 && e.SourceKey != "" {
			hit := false
			for _, k := range keys {
				hit = hit || k == e.SourceKey
			}
			if !hit {
				continue
			}
		}
		for _, id := range decodeList[uint](r.ChannelIDs) {
			want[id] = true
		}
	}
	if len(want) == 0 {
		return nil, nil
	}
	ids := make([]uint, 0, len(want))
	for id := range want {
		ids = append(ids, id)
	}
	var chans []store.Channel
	err := s.DB.WithContext(ctx).Where("id IN ? AND enabled = ?", ids, true).Order("id").Find(&chans).Error
	return chans, err
}

// Dispatch 同步地把事件发到所有匹配的渠道，返回失败的个数。
func (s *Service) Dispatch(ctx context.Context, e events.Event) int {
	chans, err := s.channelsFor(ctx, e)
	if err != nil {
		slog.Error("查询通知规则失败", "err", err)
		return 0
	}
	m := Format(e)
	failed := 0
	for _, ch := range chans {
		err := s.SendTo(ctx, ch, m)
		if err != nil {
			failed++
			slog.Warn("发送通知失败", "channel", ch.Name, "event", e.Type, "err", err)
		}
		if s.Audit != nil {
			result := "ok"
			if err != nil {
				result = err.Error()
			}
			s.Audit("notify."+e.Type, "channel:"+ch.Name, result)
		}
	}
	return failed
}

// SendTo 用一个渠道发一条通知。
func (s *Service) SendTo(ctx context.Context, ch store.Channel, m Message) error {
	c, err := s.Decrypt(ch)
	if err != nil {
		return fmt.Errorf("解密通知渠道配置失败：%w", err)
	}
	return Send(ctx, s.HTTP, ch.Kind, c, m)
}

// OnEvent 订阅事件总线。通知在后台发送，不拖慢回传和推送。
func (s *Service) OnEvent(e events.Event) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		s.Dispatch(ctx, e)
	}()
}
