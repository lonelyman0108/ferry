package server

import (
	"context"
	"sort"
	"time"

	"gorm.io/gorm"

	"github.com/lonelyman0108/ferry/internal/source"
	"github.com/lonelyman0108/ferry/internal/store"
)

// 待处理条目的类型。action 是要动手处理的（待审核、推送失败），其余是提醒。
const (
	itemPendingReview  = "pending_review"
	itemPushFailed     = "push_failed"
	itemPushRetrying   = "push_retrying"
	itemExpired        = "expired"
	itemExpiring       = "expiring"
	itemOutdated       = "outdated"
	itemNeverConnected = "never_connected"

	severityAction = "action"
	severityNotice = "notice"
)

// itemOrder 是条目的排列顺序：要动手的在前，同类按时间倒序。
var itemOrder = map[string]int{
	itemPushFailed: 0, itemPendingReview: 1, itemPushRetrying: 2, itemExpired: 3, itemExpiring: 4, itemOutdated: 5, itemNeverConnected: 6,
}

// attentionItem 是总览收件箱里的一条，带着就地处理需要的 id。口径和 overviewAttention 的计数一致。
type attentionItem struct {
	Kind             string     `json:"kind"`
	Severity         string     `json:"severity"`
	SourceID         uint       `json:"source_id,omitempty"`
	SourceName       string     `json:"source_name,omitempty"`
	IdentityID       uint       `json:"identity_id,omitempty"`
	IdentityKey      string     `json:"identity_key,omitempty"`
	Field            string     `json:"field,omitempty"`
	FieldName        string     `json:"field_name,omitempty"`
	VersionID        uint       `json:"version_id,omitempty"`
	Value            string     `json:"value,omitempty"`
	Masked           bool       `json:"masked,omitempty"`
	PushID           uint       `json:"push_id,omitempty"`
	BindingID        uint       `json:"binding_id,omitempty"`
	DestinationID    uint       `json:"destination_id,omitempty"`
	DestinationName  string     `json:"destination_name,omitempty"`
	SubscriptionID   uint       `json:"subscription_id,omitempty"`
	SubscriptionName string     `json:"subscription_name,omitempty"`
	Client           string     `json:"client,omitempty"`
	Error            string     `json:"error,omitempty"`
	Attempts         int        `json:"attempts,omitempty"`
	At               *time.Time `json:"at"`
}

// attentionItems 列出待处理条目。停用的来源不算；推送只看启用的推送规则和目的地，每个“推送规则 + 身份”只看最近一次任务。
func (s *Server) attentionItems(ctx context.Context) ([]attentionItem, error) {
	db := s.store().DB.WithContext(ctx)
	now := s.now()
	out := []attentionItem{}

	srcs, err := source.LoadAll(ctx, s.store().DB)
	if err != nil {
		return nil, err
	}
	srcByID := map[uint]store.Source{}
	fieldByID := map[uint]store.Field{}
	for _, x := range srcs {
		srcByID[x.ID] = x
		for _, f := range x.Fields {
			fieldByID[f.ID] = f
		}
	}
	enabled := func(id uint) bool { return srcByID[id].Enabled }
	var idents []store.Identity
	if err := db.Find(&idents).Error; err != nil {
		return nil, err
	}
	identKey := map[uint]string{}
	for _, a := range idents {
		identKey[a.ID] = a.Key
	}
	base := func(kind string, sourceID uint, identityID *uint) attentionItem {
		it := attentionItem{Kind: kind, Severity: severityNotice, SourceID: sourceID, SourceName: srcByID[sourceID].Name}
		if kind == itemPendingReview || kind == itemPushFailed {
			it.Severity = severityAction
		}
		if identityID != nil {
			it.IdentityID, it.IdentityKey = *identityID, identKey[*identityID]
		}
		return it
	}

	// 待审核的新值，带着打码后的值，审核时不用再点开。
	var pending []store.Version
	if err := db.Where("status = ?", store.VersionPendingReview).Order("id DESC").Find(&pending).Error; err != nil {
		return nil, err
	}
	for _, v := range pending {
		if !enabled(v.SourceID) {
			continue
		}
		f := fieldByID[v.FieldID]
		it := base(itemPendingReview, v.SourceID, v.IdentityID)
		it.Field, it.FieldName, it.VersionID = f.Key, fieldLabel(f), v.ID
		at := v.CreatedAt
		it.At = &at
		vv, err := s.viewVersion(v, f)
		if err != nil {
			return nil, err
		}
		it.Value, it.Masked = vv.Value, vv.Masked
		out = append(out, it)
	}

	// 当前值过期或快过期。
	var current []store.Version
	if err := db.Where("current = ? AND expires_at IS NOT NULL", true).Find(&current).Error; err != nil {
		return nil, err
	}
	for _, v := range current {
		if !enabled(v.SourceID) {
			continue
		}
		kind := ""
		switch {
		case !v.ExpiresAt.After(now):
			kind = itemExpired
		case v.ExpiresAt.Before(now.Add(expiringWithin)):
			kind = itemExpiring
		default:
			continue
		}
		f := fieldByID[v.FieldID]
		it := base(kind, v.SourceID, v.IdentityID)
		it.Field, it.FieldName, it.VersionID, it.At = f.Key, fieldLabel(f), v.ID, v.ExpiresAt
		out = append(out, it)
	}

	// 推送失败或等待重试。
	pushItems, err := failingPushItems(db, srcByID, base)
	if err != nil {
		return nil, err
	}
	out = append(out, pushItems...)

	// 订阅：手机上的规则过时、从没连上过。
	var subs []store.Subscription
	if err := db.Order("id").Find(&subs).Error; err != nil {
		return nil, err
	}
	for _, m := range subs {
		subItem := func(kind string, at *time.Time) attentionItem {
			return attentionItem{Kind: kind, Severity: severityNotice, SubscriptionID: m.ID, SubscriptionName: m.Name, Client: m.Client, At: at}
		}
		if m.FileMitmHash != "" && m.FileMitmHash != mitmHash(subscriptionDocsFrom(srcs, m)) {
			out = append(out, subItem(itemOutdated, m.FileFetchedAt))
		}
		if m.LastFetchedAt == nil && m.TransportLastAt == nil {
			created := m.CreatedAt
			out = append(out, subItem(itemNeverConnected, &created))
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if itemOrder[a.Kind] != itemOrder[b.Kind] {
			return itemOrder[a.Kind] < itemOrder[b.Kind]
		}
		if a.At == nil || b.At == nil {
			return b.At == nil && a.At != nil
		}
		// 快过期的按到期先后，其余按时间倒序。
		if a.Kind == itemExpiring || a.Kind == itemExpired {
			return a.At.Before(*b.At)
		}
		return a.At.After(*b.At)
	})
	return out, nil
}

// failingPushItems 找出启用的推送规则（目的地也启用）下，最近一次任务失败或在等待重试的“推送规则 + 身份”。
func failingPushItems(db *gorm.DB, srcByID map[uint]store.Source, base func(string, uint, *uint) attentionItem) ([]attentionItem, error) {
	var dests []store.Destination
	if err := db.Find(&dests).Error; err != nil {
		return nil, err
	}
	destByID := map[uint]store.Destination{}
	for _, d := range dests {
		destByID[d.ID] = d
	}
	var bindings []store.Binding
	if err := db.Find(&bindings).Error; err != nil {
		return nil, err
	}
	active := map[uint]store.Binding{}
	for _, b := range bindings {
		if b.Enabled && destByID[b.DestinationID].Enabled && srcByID[b.SourceID].Enabled {
			active[b.ID] = b
		}
	}
	latest, err := latestPushes(db, nil)
	if err != nil {
		return nil, err
	}
	var out []attentionItem
	for _, j := range latest {
		b, ok := active[j.BindingID]
		if !ok {
			continue
		}
		kind := ""
		switch pushState(j) {
		case store.PushFailed:
			kind = itemPushFailed
		case pushRetrying:
			kind = itemPushRetrying
		default:
			continue
		}
		it := base(kind, b.SourceID, j.IdentityID)
		d := destByID[b.DestinationID]
		it.PushID, it.BindingID, it.DestinationID, it.DestinationName = j.ID, b.ID, d.ID, d.Name
		it.Error, it.Attempts = j.LastError, j.Attempts
		at := j.CreatedAt
		if j.FinishedAt != nil {
			at = *j.FinishedAt
		}
		it.At = &at
		out = append(out, it)
	}
	return out, nil
}
