// Package events 是进程内的事件总线：回传处理发出事件，目的地推送和通知订阅它们。
package events

import (
	"log/slog"
	"sync"
	"time"
)

// 事件类型。
const (
	IdentityNew        = "identity.new"
	ValueUpdated       = "value.updated"
	ValueRejected      = "value.rejected"
	ValuePendingReview = "value.pending_review"
	PushFailed         = "push.failed"
	ValueExpiring      = "value.expiring"
	MitmChanged        = "subscription.mitm_changed"
)

// value.updated 的起因，推送任务按它记触发原因。
const (
	CauseApprove  = "approve"
	CauseActivate = "activate"
)

// Types 列出全部事件类型，通知规则只能选这些。
var Types = []string{IdentityNew, ValueUpdated, ValueRejected, ValuePendingReview, PushFailed, ValueExpiring, MitmChanged}

// Event 描述一次变化。只带 ID、key 和字段名，不带任何值。
type Event struct {
	Type        string
	At          time.Time
	SourceID    uint
	SourceKey   string
	SourceName  string
	IdentityID  uint
	IdentityKey string
	Fields      []string
	VersionIDs  []uint
	// Detail 是给人看的补充说明，例如驳回原因。
	Detail string
	// Cause 是 value.updated 的起因，取值见 CauseApprove、CauseActivate；为空表示回传处理后生效。
	Cause string
	// Link 是可以点开的链接，例如 MITM 变化时订阅的一键安装链接。
	Link string
	// Subject 是事件涉及的其他对象的名字，例如订阅名。
	Subject string
	// ExpiresAt 用于 value.expiring。
	ExpiresAt *time.Time
}

// Bus 按注册顺序同步调用订阅者；订阅者要尽快返回，耗时工作自己放到后台。
type Bus struct {
	mu   sync.RWMutex
	subs []func(Event)
}

// Subscribe 注册一个订阅者。
func (b *Bus) Subscribe(fn func(Event)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs = append(b.subs, fn)
}

// Publish 发出事件。某个订阅者 panic 不影响其他订阅者和发布方。
func (b *Bus) Publish(e Event) {
	if b == nil {
		return
	}
	b.mu.RLock()
	subs := append([]func(Event){}, b.subs...)
	b.mu.RUnlock()
	for _, fn := range subs {
		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("事件订阅者出错", "type", e.Type, "panic", r)
				}
			}()
			fn(e)
		}()
	}
}
