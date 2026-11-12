package server

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/lonelyman0108/ferry/internal/store"
)

// 记录的类型：回传（一个批次）、审核、推送、通知。
const (
	activitySync   = "sync"
	activityReview = "review"
	activityPush   = "push"
	activityNotify = "notify"

	activityPage = 50
)

// activityItem 是记录页时间线上的一条。Outcome 取 success、warning、failure，或空（中性）。
type activityItem struct {
	Kind            string    `json:"kind"`
	At              time.Time `json:"at"`
	Outcome         string    `json:"outcome"`
	Title           string    `json:"title"`
	Detail          string    `json:"detail,omitempty"`
	SourceID        uint      `json:"source_id,omitempty"`
	SourceName      string    `json:"source_name,omitempty"`
	IdentityID      uint      `json:"identity_id,omitempty"`
	IdentityKey     string    `json:"identity_key,omitempty"`
	DeviceID        uint      `json:"device_id,omitempty"`
	DeviceName      string    `json:"device_name,omitempty"`
	DestinationID   uint      `json:"destination_id,omitempty"`
	DestinationName string    `json:"destination_name,omitempty"`
	BatchID         uint      `json:"batch_id,omitempty"`
	PushID          uint      `json:"push_id,omitempty"`
}

type activityPageView struct {
	Items []activityItem `json:"items"`
	// Next 是下一页的 before 参数；没有更多时为空。
	Next *time.Time `json:"next"`
}

// activityFilter 是记录的筛选条件：来源（给了就不含通知）、结果（failure 只看失败和警告）、时间上限。
type activityFilter struct {
	SourceID uint
	Problems bool
	Before   time.Time
	Limit    int
}

// handleActivity 列出记录时间线：回传、审核、推送、通知按时间倒序合在一起，每页 50 条。
// 参数：source（来源 ID）、result=problem（只看失败和警告）、before（RFC 3339，翻页用）。
func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := activityFilter{Limit: activityPage, Before: s.now().Add(time.Minute)}
	if v := q.Get("source"); v != "" {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil || n == 0 {
			writeError(w, http.StatusBadRequest, "source 不合法")
			return
		}
		f.SourceID = uint(n)
	}
	switch q.Get("result") {
	case "":
	case "problem":
		f.Problems = true
	default:
		writeError(w, http.StatusBadRequest, "result 只能是 problem")
		return
	}
	if v := q.Get("before"); v != "" {
		t, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "before 不合法")
			return
		}
		f.Before = t
	}
	items, err := s.activity(r.Context(), f)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := activityPageView{Items: items}
	if len(items) == f.Limit {
		t := items[len(items)-1].At
		out.Next = &t
	}
	writeJSON(w, http.StatusOK, out)
}

// activityNames 是拼记录标题用的名称表。
type activityNames struct {
	source   map[uint]string
	identity map[uint]string
	device   map[uint]string
	dest     map[uint]string
	field    map[uint]store.Field
	binding  map[uint]store.Binding
}

func loadActivityNames(db *gorm.DB) (activityNames, error) {
	n := activityNames{source: map[uint]string{}, identity: map[uint]string{}, device: map[uint]string{}, dest: map[uint]string{},
		field: map[uint]store.Field{}, binding: map[uint]store.Binding{}}
	var srcs []store.Source
	if err := db.Preload("Fields").Find(&srcs).Error; err != nil {
		return n, err
	}
	for _, x := range srcs {
		n.source[x.ID] = x.Name
		for _, f := range x.Fields {
			n.field[f.ID] = f
		}
	}
	var idents []store.Identity
	if err := db.Find(&idents).Error; err != nil {
		return n, err
	}
	for _, a := range idents {
		n.identity[a.ID] = a.Key
	}
	var subs []store.Subscription
	if err := db.Find(&subs).Error; err != nil {
		return n, err
	}
	for _, m := range subs {
		n.device[m.ID] = m.Name
	}
	var dests []store.Destination
	if err := db.Find(&dests).Error; err != nil {
		return n, err
	}
	for _, d := range dests {
		n.dest[d.ID] = d.Name
	}
	var bindings []store.Binding
	if err := db.Find(&bindings).Error; err != nil {
		return n, err
	}
	for _, b := range bindings {
		n.binding[b.ID] = b
	}
	return n, nil
}

func (n activityNames) withIdentity(it *activityItem, id *uint) {
	if id != nil {
		it.IdentityID, it.IdentityKey = *id, n.identity[*id]
	}
}

// activity 从批次、推送任务和审计记录里各取最多 Limit 条，合并后按时间倒序截取前 Limit 条。
func (s *Server) activity(ctx context.Context, f activityFilter) ([]activityItem, error) {
	db := s.store().DB.WithContext(ctx)
	names, err := loadActivityNames(db)
	if err != nil {
		return nil, err
	}
	// 只看问题时，各部分多取一些再筛，免得一页凑不满。
	pf := f
	if f.Problems {
		pf.Limit = f.Limit * 4
	}
	var out []activityItem
	for _, part := range []func(*gorm.DB, activityNames, activityFilter) ([]activityItem, error){activityBatches, activityPushes, activityAudits} {
		items, err := part(db, names, pf)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
	}
	if f.Problems {
		kept := out[:0]
		for _, it := range out {
			if it.Outcome == outcomeFailure || it.Outcome == outcomeWarning {
				kept = append(kept, it)
			}
		}
		out = kept
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	if len(out) > f.Limit {
		out = out[:f.Limit]
	}
	if out == nil {
		out = []activityItem{}
	}
	return out, nil
}

// activityBatches 把每个批次记成一次回传：收到几个新值、几个待审核、哪些被驳回。没有产生任何版本的批次（值没变）不列出。
func activityBatches(db *gorm.DB, n activityNames, f activityFilter) ([]activityItem, error) {
	q := db.Where("created_at < ?", f.Before).Order("created_at DESC").Limit(f.Limit)
	if f.SourceID != 0 {
		q = q.Where("source_id = ?", f.SourceID)
	}
	var batches []store.Batch
	if err := q.Find(&batches).Error; err != nil {
		return nil, err
	}
	if len(batches) == 0 {
		return nil, nil
	}
	ids := make([]uint, 0, len(batches))
	for _, b := range batches {
		ids = append(ids, b.ID)
	}
	var versions []store.Version
	if err := db.Select("id", "batch_id", "field_id", "status", "reason").Where("batch_id IN ?", ids).Order("id").Find(&versions).Error; err != nil {
		return nil, err
	}
	byBatch := map[uint][]store.Version{}
	for _, v := range versions {
		byBatch[v.BatchID] = append(byBatch[v.BatchID], v)
	}
	var out []activityItem
	for _, b := range batches {
		vs := byBatch[b.ID]
		if len(vs) == 0 && b.Status != store.BatchCollecting {
			continue
		}
		it := activityItem{Kind: activitySync, At: b.CreatedAt, Title: "回传", SourceID: b.SourceID, SourceName: n.source[b.SourceID],
			DeviceID: b.SubscriptionID, DeviceName: n.device[b.SubscriptionID], BatchID: b.ID}
		n.withIdentity(&it, b.IdentityID)
		var fresh, review, rejected []string
		for _, v := range vs {
			label := fieldLabel(n.field[v.FieldID])
			switch v.Status {
			case store.VersionPendingReview:
				review = append(review, label)
			case store.VersionRejected:
				if v.Reason != "" {
					label += "（" + v.Reason + "）"
				}
				rejected = append(rejected, label)
			default:
				fresh = append(fresh, label)
			}
		}
		var parts []string
		if b.Status == store.BatchCollecting {
			parts = append(parts, "收集中，等其余字段到齐")
		}
		if len(fresh) > 0 {
			parts = append(parts, fmt.Sprintf("%d 个新值：%s", len(fresh), strings.Join(fresh, "、")))
		}
		if len(review) > 0 {
			parts = append(parts, fmt.Sprintf("%d 个待审核：%s", len(review), strings.Join(review, "、")))
		}
		if len(rejected) > 0 {
			parts = append(parts, "被驳回："+strings.Join(rejected, "、"))
		}
		it.Detail = strings.Join(parts, "；")
		switch {
		case len(rejected) > 0 && len(fresh)+len(review) == 0:
			it.Outcome = outcomeFailure
		case len(rejected) > 0 || len(review) > 0:
			it.Outcome = outcomeWarning
		case b.Status != store.BatchCollecting:
			it.Outcome = outcomeSuccess
		}
		out = append(out, it)
	}
	return out, nil
}

// activityPushes 列出推送任务，时间取创建时间，翻页时和其它记录用同一把尺子。
func activityPushes(db *gorm.DB, n activityNames, f activityFilter) ([]activityItem, error) {
	q := db.Where("created_at < ?", f.Before).Order("created_at DESC").Limit(f.Limit)
	if f.SourceID != 0 {
		var ids []uint
		for id, b := range n.binding {
			if b.SourceID == f.SourceID {
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 {
			return nil, nil
		}
		q = q.Where("binding_id IN ?", ids)
	}
	var jobs []store.PushJob
	if err := q.Find(&jobs).Error; err != nil {
		return nil, err
	}
	out := make([]activityItem, 0, len(jobs))
	for _, j := range jobs {
		b := n.binding[j.BindingID]
		it := activityItem{Kind: activityPush, At: j.CreatedAt, Title: "推送到 " + n.dest[b.DestinationID], SourceID: b.SourceID, SourceName: n.source[b.SourceID],
			DestinationID: b.DestinationID, DestinationName: n.dest[b.DestinationID], PushID: j.ID}
		n.withIdentity(&it, j.IdentityID)
		switch pushState(j) {
		case store.PushSuccess:
			it.Outcome, it.Detail = outcomeSuccess, j.Detail
		case store.PushFailed:
			it.Outcome, it.Detail = outcomeFailure, fmt.Sprintf("%s · 已试 %d 次", j.LastError, j.Attempts)
		case pushRetrying:
			it.Outcome, it.Detail = outcomeWarning, fmt.Sprintf("%s · 等待重试", j.LastError)
		case store.PushSkipped:
			it.Detail = "跳过：" + j.LastError
		default:
			it.Detail = "排队中"
		}
		out = append(out, it)
	}
	return out, nil
}

// activityAudits 取审核（通过、驳回、恢复旧值）和通知发送两类审计记录。按来源筛选时只取审核。
func activityAudits(db *gorm.DB, n activityNames, f activityFilter) ([]activityItem, error) {
	actions := []string{"version.approve", "version.reject", "version.activate"}
	q := db.Where("at < ?", f.Before).Order("at DESC").Limit(f.Limit)
	if f.SourceID != 0 {
		q = q.Where("action IN ?", actions)
	} else {
		q = q.Where("action IN ? OR action LIKE ?", actions, notifyPrefix+"%")
	}
	var logs []store.AuditLog
	if err := q.Find(&logs).Error; err != nil {
		return nil, err
	}
	var vids []uint
	for _, a := range logs {
		if id, ok := idOf(a.Target, "version:"); ok {
			vids = append(vids, id)
		}
	}
	versions := map[uint]store.Version{}
	if len(vids) > 0 {
		var vs []store.Version
		if err := db.Select("id", "source_id", "identity_id", "field_id").Where("id IN ?", vids).Find(&vs).Error; err != nil {
			return nil, err
		}
		for _, v := range vs {
			versions[v.ID] = v
		}
	}
	out := make([]activityItem, 0, len(logs))
	for _, a := range logs {
		if strings.HasPrefix(a.Action, notifyPrefix) {
			it := activityItem{Kind: activityNotify, At: a.At, Outcome: outcomeOf(a), Title: kindOf(a.Action).label}
			it.Detail, _ = strings.CutPrefix(a.Target, "channel:")
			if a.Detail != "ok" && a.Detail != "" {
				it.Detail += " · " + a.Detail
			}
			out = append(out, it)
			continue
		}
		id, _ := idOf(a.Target, "version:")
		v, ok := versions[id]
		if f.SourceID != 0 && (!ok || v.SourceID != f.SourceID) {
			continue
		}
		it := activityItem{Kind: activityReview, At: a.At, Title: kindOf(a.Action).label}
		if ok {
			it.SourceID, it.SourceName, it.Detail = v.SourceID, n.source[v.SourceID], fieldLabel(n.field[v.FieldID])
			n.withIdentity(&it, v.IdentityID)
		} else {
			it.Detail = "这个值已被清理"
		}
		switch a.Action {
		case "version.approve":
			it.Outcome = outcomeSuccess
		case "version.reject":
			it.Outcome = outcomeWarning
		}
		out = append(out, it)
	}
	return out, nil
}
