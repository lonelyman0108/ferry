package server

import (
	"context"
	"net/http"
	"time"

	"github.com/lonelyman0108/ferry/internal/source"
	"github.com/lonelyman0108/ferry/internal/store"
)

// overviewWindow 是总览里“最近”的统计范围；当前值在 expiringWithin 内到期算即将过期。
const (
	overviewWindow = 7 * 24 * time.Hour
	expiringWithin = 3 * 24 * time.Hour
)

// overviewAttention 是需要处理的事。停用的来源不算；推送只看启用的推送规则和目的地，
// 且每个“推送规则 + 身份”只看最近一次任务：失败了算失败，排队中带着错误算等待重试。
type overviewAttention struct {
	PendingReview  int `json:"pending_review"`
	PushFailed     int `json:"push_failed"`
	PushRetrying   int `json:"push_retrying"`
	Expiring       int `json:"expiring"`
	Expired        int `json:"expired"`
	Collecting     int `json:"collecting"`
	NeverConnected int `json:"never_connected"`
	// Outdated 是手机上那份订阅的 MITM 域名已经过时、需要在客户端里更新的订阅数。
	Outdated int `json:"outdated"`
}

type overviewCounts struct {
	Sources                int `json:"sources"`
	SourcesEnabled         int `json:"sources_enabled"`
	Subscriptions          int `json:"subscriptions"`
	SubscriptionsConnected int `json:"subscriptions_connected"`
	Identities             int `json:"identities"`
	Destinations           int `json:"destinations"`
	DestinationsEnabled    int `json:"destinations_enabled"`
	Bindings               int `json:"bindings"`
	Channels               int `json:"channels"`
	ChannelsEnabled        int `json:"channels_enabled"`
	Rules                  int `json:"rules"`
	RulesEnabled           int `json:"rules_enabled"`
}

// overviewSource 是一个来源的状态，口径同 overviewAttention；Rejected 只算 overviewWindow 内的。
type overviewSource struct {
	ID            uint       `json:"id"`
	Key           string     `json:"key"`
	Name          string     `json:"name"`
	Enabled       bool       `json:"enabled"`
	Identities    int        `json:"identities"`
	LastSeenAt    *time.Time `json:"last_seen_at"`
	PendingReview int        `json:"pending_review"`
	Rejected      int        `json:"rejected"`
	PushFailed    int        `json:"push_failed"`
	PushRetrying  int        `json:"push_retrying"`
	Expiring      int        `json:"expiring"`
	Expired       int        `json:"expired"`
	Bindings      int        `json:"bindings"`
	// ExpiresAt 是各身份当前值里最早的过期时间。
	ExpiresAt *time.Time `json:"expires_at"`
}

type overviewView struct {
	WindowDays int               `json:"window_days"`
	Attention  overviewAttention `json:"attention"`
	Counts     overviewCounts    `json:"counts"`
	Sources    []overviewSource  `json:"sources"`
	// Items 是 Attention 里各项计数对应的条目（收集中除外），带着就地处理需要的 id。
	Items []attentionItem `json:"items"`
}

// handleOverview 给总览和侧栏角标用：需要处理的事、各类对象的数量、每个来源的状态。
func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	out, err := s.overview(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if out.Items, err = s.attentionItems(r.Context()); err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// overview 只做计数和少量明细查询，时间的比较和取最大值放在 Go 里做，三种数据库的行为一致。
func (s *Server) overview(ctx context.Context) (overviewView, error) {
	db := s.store().DB.WithContext(ctx)
	now := s.now()
	out := overviewView{WindowDays: int(overviewWindow / (24 * time.Hour)), Sources: []overviewSource{}}

	sources, err := source.LoadAll(ctx, s.store().DB)
	if err != nil {
		return out, err
	}
	for _, x := range sources {
		out.Sources = append(out.Sources, overviewSource{ID: x.ID, Key: x.Key, Name: x.Name, Enabled: x.Enabled})
		if x.Enabled {
			out.Counts.SourcesEnabled++
		}
	}
	out.Counts.Sources = len(sources)
	byID := map[uint]*overviewSource{}
	for i := range out.Sources {
		byID[out.Sources[i].ID] = &out.Sources[i]
	}

	var idents []store.Identity
	if err := db.Find(&idents).Error; err != nil {
		return out, err
	}
	out.Counts.Identities = len(idents)
	for _, a := range idents {
		src := byID[a.SourceID]
		if src == nil {
			continue
		}
		src.Identities++
		if src.LastSeenAt == nil || a.LastSeenAt.After(*src.LastSeenAt) {
			t := a.LastSeenAt
			src.LastSeenAt = &t
		}
	}

	type sourceCount struct {
		SourceID uint
		N        int
	}
	var pending, rejected []sourceCount
	if err := db.Model(&store.Version{}).Select("source_id, COUNT(*) AS n").Where("status = ?", store.VersionPendingReview).
		Group("source_id").Scan(&pending).Error; err != nil {
		return out, err
	}
	if err := db.Model(&store.Version{}).Select("source_id, COUNT(*) AS n").Where("status = ? AND created_at >= ?", store.VersionRejected, now.Add(-overviewWindow)).
		Group("source_id").Scan(&rejected).Error; err != nil {
		return out, err
	}
	for _, c := range pending {
		if src := byID[c.SourceID]; src != nil {
			src.PendingReview = c.N
		}
	}
	for _, c := range rejected {
		if src := byID[c.SourceID]; src != nil {
			src.Rejected = c.N
		}
	}

	var current []store.Version
	if err := db.Select("id", "source_id", "expires_at").Where("current = ? AND expires_at IS NOT NULL", true).Find(&current).Error; err != nil {
		return out, err
	}
	for _, v := range current {
		src := byID[v.SourceID]
		if src == nil {
			continue
		}
		if src.ExpiresAt == nil || v.ExpiresAt.Before(*src.ExpiresAt) {
			src.ExpiresAt = v.ExpiresAt
		}
		switch {
		case !v.ExpiresAt.After(now):
			src.Expired++
		case v.ExpiresAt.Before(now.Add(expiringWithin)):
			src.Expiring++
		}
	}

	var batches []store.Batch
	if err := db.Where("status = ?", store.BatchCollecting).Find(&batches).Error; err != nil {
		return out, err
	}
	for _, b := range batches {
		if src := byID[b.SourceID]; src != nil && src.Enabled {
			out.Attention.Collecting++
		}
	}

	var subs []store.Subscription
	if err := db.Find(&subs).Error; err != nil {
		return out, err
	}
	out.Counts.Subscriptions = len(subs)
	for _, m := range subs {
		if m.LastFetchedAt != nil || m.TransportLastAt != nil {
			out.Counts.SubscriptionsConnected++
		} else {
			out.Attention.NeverConnected++
		}
		if m.FileMitmHash != "" && m.FileMitmHash != mitmHash(subscriptionDocsFrom(sources, m)) {
			out.Attention.Outdated++
		}
	}

	var dests []store.Destination
	if err := db.Find(&dests).Error; err != nil {
		return out, err
	}
	destEnabled := map[uint]bool{}
	for _, d := range dests {
		destEnabled[d.ID] = d.Enabled
		if d.Enabled {
			out.Counts.DestinationsEnabled++
		}
	}
	out.Counts.Destinations = len(dests)
	var bindings []store.Binding
	if err := db.Find(&bindings).Error; err != nil {
		return out, err
	}
	out.Counts.Bindings = len(bindings)
	// 启用、目的地也启用的推送规则 → 所属来源。
	active := map[uint]uint{}
	for _, b := range bindings {
		if src := byID[b.SourceID]; src != nil {
			src.Bindings++
		}
		if b.Enabled && destEnabled[b.DestinationID] {
			active[b.ID] = b.SourceID
		}
	}
	latest, err := latestPushes(db, nil)
	if err != nil {
		return out, err
	}
	for _, j := range latest {
		src := byID[active[j.BindingID]]
		if src == nil {
			continue
		}
		switch pushState(j) {
		case store.PushFailed:
			src.PushFailed++
		case pushRetrying:
			src.PushRetrying++
		}
	}

	for _, src := range out.Sources {
		if !src.Enabled {
			continue
		}
		out.Attention.PendingReview += src.PendingReview
		out.Attention.PushFailed += src.PushFailed
		out.Attention.PushRetrying += src.PushRetrying
		out.Attention.Expiring += src.Expiring
		out.Attention.Expired += src.Expired
	}

	var chans []store.Channel
	if err := db.Find(&chans).Error; err != nil {
		return out, err
	}
	for _, c := range chans {
		if c.Enabled {
			out.Counts.ChannelsEnabled++
		}
	}
	out.Counts.Channels = len(chans)
	var rules []store.NotifyRule
	if err := db.Find(&rules).Error; err != nil {
		return out, err
	}
	for _, x := range rules {
		if x.Enabled {
			out.Counts.RulesEnabled++
		}
	}
	out.Counts.Rules = len(rules)
	return out, nil
}
