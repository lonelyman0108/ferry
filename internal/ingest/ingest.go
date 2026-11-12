// Package ingest 处理手机端回传：校验、去重、按合并窗口归批，窗口关闭后定身份、生成新版本并发出事件。
package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/lonelyman0108/ferry/internal/events"
	"github.com/lonelyman0108/ferry/internal/probe"
	"github.com/lonelyman0108/ferry/internal/secret"
	"github.com/lonelyman0108/ferry/internal/source"
	"github.com/lonelyman0108/ferry/internal/store"
	"github.com/lonelyman0108/ferry/internal/validate"
)

// DefaultIdentity 是来源没有身份字段、或这批回传里没有身份字段时用的身份键。
const DefaultIdentity = "default"

// 回传结果，手机端据此决定通知内容。
const (
	ResultAccepted  = "accepted"
	ResultUnchanged = "unchanged"
	ResultRejected  = "rejected"
)

// ErrUnknownSource 表示来源不存在、已停用，或这个订阅看不到它。
var ErrUnknownSource = errors.New("来源不存在或不在该订阅范围内")

// ErrBatchNotFound 表示批次不存在。
var ErrBatchNotFound = errors.New("批次不存在")

// BadInputError 表示回传内容不合法（未声明的字段等）。
type BadInputError struct{ Msg string }

func (e *BadInputError) Error() string { return e.Msg }

// Service 处理回传。Now 可在测试里替换。
type Service struct {
	DB  *gorm.DB
	Box *secret.Box
	Bus *events.Bus
	Now func() time.Time
	// HTTP 用于主动探测；为 nil 时用默认客户端。
	HTTP *http.Client
}

// SyncInput 是一次回传的内容，格式见计划「回传协议」。
type SyncInput struct {
	Source  string            `json:"source"`
	Matcher string            `json:"matcher"`
	Fields  map[string]string `json:"fields"`
	SeenAt  int64             `json:"seen_at"`
}

// SyncResult 返回给手机端。Message 会出现在手机通知里，不含任何值。
type SyncResult struct {
	Result   string            `json:"result"`
	Message  string            `json:"message"`
	Rejected map[string]string `json:"rejected,omitempty"`
}

func valueAAD(fieldID uint) []byte {
	return []byte("fields/" + strconv.FormatUint(uint64(fieldID), 10) + "/value")
}

func (s *Service) mac(fieldID uint, value string) string {
	return s.Box.MAC([]byte(strconv.FormatUint(uint64(fieldID), 10) + "\x00" + value))
}

// Reveal 返回版本的明文值。
func (s *Service) Reveal(v store.Version) (string, error) { return RevealValue(s.Box, v) }

// RevealValue 用主密钥解出版本的明文值，推送时也要用。
func RevealValue(box *secret.Box, v store.Version) (string, error) {
	if !v.Sealed {
		return v.Value, nil
	}
	b, err := box.Open(v.Value, valueAAD(v.FieldID))
	return string(b), err
}

func visible(sub store.Subscription, key string) bool {
	if sub.AllSources {
		return true
	}
	var keys []string
	_ = json.Unmarshal([]byte(sub.SourceKeys), &keys)
	for _, k := range keys {
		if k == key {
			return true
		}
	}
	return false
}

// Sync 处理一次回传。
func (s *Service) Sync(ctx context.Context, sub store.Subscription, in SyncInput) (SyncResult, error) {
	src, err := source.LoadByKey(ctx, s.DB, in.Source)
	if errors.Is(err, source.ErrNotFound) || (err == nil && (!src.Enabled || !visible(sub, src.Key))) {
		return SyncResult{}, ErrUnknownSource
	}
	if err != nil {
		return SyncResult{}, err
	}
	if len(in.Fields) == 0 {
		return SyncResult{}, &BadInputError{"缺少字段"}
	}
	fields := map[string]store.Field{}
	for _, f := range src.Fields {
		fields[f.Key] = f
	}
	var unknown []string
	for k := range in.Fields {
		if _, ok := fields[k]; !ok {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		return SyncResult{}, &BadInputError{"字段未在来源中声明：" + strings.Join(unknown, "、")}
	}
	if in.Matcher != "" {
		found := false
		for _, m := range src.Matchers {
			found = found || m.Name == in.Matcher
		}
		if !found {
			return SyncResult{}, &BadInputError{"匹配规则不存在：" + in.Matcher}
		}
	}

	doc := source.FromModel(src)
	docFields := map[string]source.FieldDoc{}
	for _, f := range doc.Fields {
		docFields[f.Key] = f
	}
	now := s.Now()
	res := SyncResult{Rejected: map[string]string{}}
	var created int
	var batchID uint
	var idle bool
	var rejectedEvents []events.Event

	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var batch store.Batch
		err := tx.Where("source_id = ? AND subscription_id = ? AND status = ? AND closes_at > ?", src.ID, sub.ID, store.BatchCollecting, now).
			Order("id DESC").First(&batch).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			batch = store.Batch{SourceID: src.ID, SubscriptionID: sub.ID, Status: store.BatchCollecting, ClosesAt: now.Add(time.Duration(src.MergeWindowSec) * time.Second), CreatedAt: now}
			if err := tx.Create(&batch).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		batchID = batch.ID

		for key, value := range in.Fields {
			f := fields[key]
			hash := s.mac(f.ID, value)
			check := validate.Check(docFields[key], value, now)
			if check.Err != nil {
				res.Rejected[key] = check.Err.Error()
				// 同一个不合法的值一小时内只记一次，免得重复回传刷出一堆驳回记录。
				var n int64
				if err := tx.Model(&store.Version{}).Where("field_id = ? AND value_hash = ? AND status = ? AND created_at > ?", f.ID, hash, store.VersionRejected, now.Add(-time.Hour)).Count(&n).Error; err != nil {
					return err
				}
				if n > 0 {
					continue
				}
				v, err := s.newVersion(f, batch.ID, src.ID, value, hash, store.VersionRejected, check, now)
				if err != nil {
					return err
				}
				v.Reason = truncate(check.Err.Error(), 255)
				if err := tx.Create(&v).Error; err != nil {
					return err
				}
				rejectedEvents = append(rejectedEvents, events.Event{
					Type: events.ValueRejected, At: now, SourceID: src.ID, SourceKey: src.Key, SourceName: src.Name,
					Fields: []string{key}, VersionIDs: []uint{v.ID}, Detail: v.Reason,
				})
				continue
			}

			// 和当前值或本批次里已有的值相同，只记一次“又见到了”。
			var same store.Version
			err := tx.Where("field_id = ? AND value_hash = ? AND status <> ?", f.ID, hash, store.VersionRejected).Order("id DESC").First(&same).Error
			if err == nil && (same.Current || same.BatchID == batch.ID || same.Status == store.VersionPendingReview) {
				if err := tx.Model(&same).Updates(map[string]any{"seen_count": same.SeenCount + 1, "last_seen_at": now}).Error; err != nil {
					return err
				}
				continue
			}
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			v, err := s.newVersion(f, batch.ID, src.ID, value, hash, store.VersionPending, check, now)
			if err != nil {
				return err
			}
			if err := tx.Create(&v).Error; err != nil {
				return err
			}
			created++
		}
		if created > 0 {
			return nil
		}
		// 这次没有新值（全被驳回或都没变），批次里也没有在等的值：当场关掉，不留空批次等合并窗口结束。
		// 先把截止时间改成现在，之后的回传就不会再并进这个批次。
		var waiting int64
		if err := tx.Model(&store.Version{}).Where("batch_id = ? AND status = ?", batch.ID, store.VersionPending).Count(&waiting).Error; err != nil {
			return err
		}
		if waiting == 0 {
			idle = true
			return tx.Model(&batch).Update("closes_at", now).Error
		}
		return nil
	})
	if err != nil {
		return SyncResult{}, err
	}
	for _, e := range rejectedEvents {
		s.Bus.Publish(e)
	}

	// 被驳回的字段不算到齐，其余字段照常判断。
	arrived := map[string]bool{}
	var rejected []string
	for k := range in.Fields {
		if _, bad := res.Rejected[k]; bad {
			rejected = append(rejected, k)
		} else {
			arrived[k] = true
		}
	}
	sort.Strings(rejected)
	failed := ""
	if len(rejected) > 0 {
		failed = strings.Join(rejected, "、") + " 未通过校验"
	}

	// 有新值就算 accepted，手机端“变化时通知”才会提示；没通过的字段写在结果文字和 Rejected 里。
	switch {
	case created > 0:
		res.Result = ResultAccepted
		res.Message = fmt.Sprintf("收到 %d 个新值", created)
		complete, err := s.batchComplete(ctx, src, batchID, arrived)
		if err != nil {
			return SyncResult{}, err
		}
		if src.MergeWindowSec == 0 || complete {
			if _, err := s.processBatch(ctx, batchID); err != nil {
				return SyncResult{}, err
			}
		} else {
			res.Message += "，" + humanDuration(src.MergeWindowSec) + "后生效"
		}
		if src.ReviewMode == source.ReviewManual {
			res.Message += "（待审核）"
		}
		if failed != "" {
			res.Message += "，" + failed
		}
	case failed != "":
		res.Result = ResultRejected
		res.Message = failed
		if len(arrived) > 0 {
			res.Message += "，其余值未变化"
		}
	default:
		res.Result = ResultUnchanged
		res.Message = "值未变化"
	}
	if idle {
		if _, err := s.processBatch(ctx, batchID); err != nil {
			return SyncResult{}, err
		}
	}
	if len(res.Rejected) == 0 {
		res.Rejected = nil
	}
	return res, nil
}

// batchComplete 判断来源声明的字段是否都已到齐：本批次里有新值，或这次回传带了它（值没变的也算）。
// 到齐了就不必等合并窗口结束。
func (s *Service) batchComplete(ctx context.Context, src store.Source, batchID uint, seen map[string]bool) (bool, error) {
	var ids []uint
	if err := s.DB.WithContext(ctx).Model(&store.Version{}).Where("batch_id = ? AND status <> ?", batchID, store.VersionRejected).
		Distinct().Pluck("field_id", &ids).Error; err != nil {
		return false, err
	}
	have := map[uint]bool{}
	for _, id := range ids {
		have[id] = true
	}
	for _, f := range src.Fields {
		if _, ok := seen[f.Key]; !ok && !have[f.ID] {
			return false, nil
		}
	}
	return true, nil
}

// ProcessNow 不等合并窗口结束，立即处理一个收集中的批次。批次已处理过时返回 false。
func (s *Service) ProcessNow(ctx context.Context, batchID uint) (bool, error) {
	err := s.DB.WithContext(ctx).First(&store.Batch{}, batchID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, ErrBatchNotFound
	}
	if err != nil {
		return false, err
	}
	return s.processBatch(ctx, batchID)
}

func (s *Service) newVersion(f store.Field, batchID, sourceID uint, value, hash, status string, check validate.Result, now time.Time) (store.Version, error) {
	v := store.Version{
		BatchID: batchID, SourceID: sourceID, FieldID: f.ID, Value: value, ValueHash: hash,
		Status: status, ExpiresAt: check.ExpiresAt, SeenCount: 1, LastSeenAt: now,
		// 显式用服务的时钟，和去重查询里的时间来自同一个来源。
		CreatedAt: now,
	}
	if f.Sensitive {
		sealed, err := s.Box.Seal([]byte(value), valueAAD(f.ID))
		if err != nil {
			return v, err
		}
		v.Value, v.Sealed = sealed, true
	}
	return v, nil
}

func humanDuration(sec int) string {
	if sec < 60 {
		return fmt.Sprintf("%d 秒", sec)
	}
	return fmt.Sprintf("约 %d 分钟", (sec+59)/60)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// ProcessDue 处理所有到期的批次，返回处理的个数。
func (s *Service) ProcessDue(ctx context.Context) (int, error) {
	var ids []uint
	if err := s.DB.WithContext(ctx).Model(&store.Batch{}).Where("status = ? AND closes_at <= ?", store.BatchCollecting, s.Now()).Order("id").Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	n := 0
	for _, id := range ids {
		done, err := s.processBatch(ctx, id)
		if err != nil {
			return n, fmt.Errorf("处理批次 %d 失败：%w", id, err)
		}
		if done {
			n++
		}
	}
	return n, nil
}

// Run 每隔 interval 处理一次到期批次，直到 ctx 结束。
func (s *Service) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := s.ProcessDue(ctx); err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("处理回传批次失败", "err", err)
			}
		}
	}
}

// processBatch 关闭一个批次：定身份，按需主动探测，再让待处理的版本生效、进入审核或被驳回。
//
// 分三步：先只读地准备（读批次、定身份、合并出完整的值）；再在事务外探测（网络请求不放进事务）；
// 最后在事务里用带条件的 UPDATE 认领批次并写结果。后台定时处理和合并窗口为 0 时的即时处理
// 可能同时准备同一批次，只有认领成功的一方写入；探测接口应当是只读的，多探测一次没有副作用。
func (s *Service) processBatch(ctx context.Context, batchID uint) (bool, error) {
	db := s.DB.WithContext(ctx)
	var batch store.Batch
	if err := db.First(&batch, batchID).Error; err != nil {
		return false, err
	}
	if batch.Status != store.BatchCollecting {
		return false, nil
	}
	var src store.Source
	if err := db.Preload("Fields").First(&src, batch.SourceID).Error; err != nil {
		return false, err
	}
	doc := source.FromModel(src)
	fieldKey := map[uint]string{}
	var identityFieldID uint
	for _, f := range src.Fields {
		fieldKey[f.ID] = f.Key
		if f.Key == src.IdentityField {
			identityFieldID = f.ID
		}
	}
	var versions []store.Version
	if err := db.Where("batch_id = ?", batch.ID).Order("id").Find(&versions).Error; err != nil {
		return false, err
	}
	identityKey, err := s.identityKey(db, src, batch, versions, identityFieldID)
	if err != nil {
		return false, err
	}

	// 探测用的值：身份已有的当前值，加上这批的新值。
	verdict := probeVerdict{}
	hasPending := false
	for _, v := range versions {
		hasPending = hasPending || v.Status == store.VersionPending
	}
	if doc.Probe != nil && hasPending {
		values, err := s.mergedValues(db, src.ID, identityKey, versions, fieldKey)
		if err != nil {
			return false, err
		}
		r := probe.Run(ctx, s.HTTP, *doc.Probe, values)
		switch {
		case r.Err != nil:
			// 网络抖动不该丢掉可能有效的值，转人工审核。
			verdict.review, verdict.reason = true, truncate("有效性检测失败："+r.Err.Error(), 255)
		case !r.OK:
			verdict.reject, verdict.reason = true, truncate(r.Detail, 255)
		case r.Identity != "":
			identityKey = truncate(r.Identity, 255)
		}
	}

	now := s.Now()
	var evs []events.Event
	claimed := false
	err = db.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&store.Batch{}).Where("id = ? AND status = ?", batch.ID, store.BatchCollecting).
			Updates(map[string]any{"status": store.BatchDone, "processed_at": now})
		if res.Error != nil || res.RowsAffected == 0 {
			return res.Error
		}
		claimed = true
		var ident store.Identity
		err := tx.Where(map[string]any{"source_id": src.ID, "key": identityKey}).First(&ident).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 这批的值在回传时就全被驳回了：不为它新建身份，否则会多出一个没有任何有效值的空身份。
			// 驳回记录仍挂在批次上，回传列表里看得到。
			if !hasPending {
				return nil
			}
			ident = store.Identity{SourceID: src.ID, Key: identityKey, LastSeenAt: now, CreatedAt: now}
			if err := tx.Create(&ident).Error; err != nil {
				return err
			}
			evs = append(evs, events.Event{Type: events.IdentityNew, At: now, SourceID: src.ID, SourceKey: src.Key, SourceName: src.Name, IdentityID: ident.ID, IdentityKey: ident.Key})
		} else if err != nil {
			return err
		} else if err := tx.Model(&ident).Update("last_seen_at", now).Error; err != nil {
			return err
		}
		if err := tx.Model(&store.Batch{}).Where("id = ?", batch.ID).Update("identity_id", ident.ID).Error; err != nil {
			return err
		}
		if err := tx.Model(&store.Version{}).Where("batch_id = ?", batch.ID).Update("identity_id", ident.ID).Error; err != nil {
			return err
		}

		var updated, review, rejected []string
		var updatedIDs, reviewIDs, rejectedIDs []uint
		for _, v := range versions {
			if v.Status != store.VersionPending {
				continue
			}
			key := fieldKey[v.FieldID]
			switch {
			case verdict.reject:
				if err := tx.Model(&store.Version{}).Where("id = ?", v.ID).Updates(map[string]any{"status": store.VersionRejected, "reason": verdict.reason}).Error; err != nil {
					return err
				}
				rejected, rejectedIDs = append(rejected, key), append(rejectedIDs, v.ID)
			case verdict.review || src.ReviewMode == source.ReviewManual:
				if err := tx.Model(&store.Version{}).Where("id = ?", v.ID).Updates(map[string]any{"status": store.VersionPendingReview, "reason": verdict.reason}).Error; err != nil {
					return err
				}
				review, reviewIDs = append(review, key), append(reviewIDs, v.ID)
			default:
				if err := activate(tx, v.ID, ident.ID, v.FieldID, now); err != nil {
					return err
				}
				updated, updatedIDs = append(updated, key), append(updatedIDs, v.ID)
			}
		}
		base := events.Event{At: now, SourceID: src.ID, SourceKey: src.Key, SourceName: src.Name, IdentityID: ident.ID, IdentityKey: ident.Key}
		for _, g := range []struct {
			typ    string
			fields []string
			ids    []uint
		}{{events.ValueUpdated, updated, updatedIDs}, {events.ValuePendingReview, review, reviewIDs}, {events.ValueRejected, rejected, rejectedIDs}} {
			if len(g.fields) > 0 {
				e := base
				e.Type, e.Fields, e.VersionIDs, e.Detail = g.typ, g.fields, g.ids, verdict.reason
				evs = append(evs, e)
			}
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	for _, e := range evs {
		s.Bus.Publish(e)
	}
	return claimed, nil
}

type probeVerdict struct {
	reject, review bool
	reason         string
}

// mergedValues 返回探测用的完整取值：身份已有的当前值，被这批待处理的新值覆盖。
func (s *Service) mergedValues(db *gorm.DB, sourceID uint, identityKey string, versions []store.Version, fieldKey map[uint]string) (map[string]string, error) {
	values := map[string]string{}
	var ident store.Identity
	err := db.Where(map[string]any{"source_id": sourceID, "key": identityKey}).First(&ident).Error
	if err == nil {
		var cur []store.Version
		if err := db.Where("identity_id = ? AND current = ?", ident.ID, true).Find(&cur).Error; err != nil {
			return nil, err
		}
		for _, v := range cur {
			val, err := s.Reveal(v)
			if err != nil {
				return nil, err
			}
			values[fieldKey[v.FieldID]] = val
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	for _, v := range versions {
		if v.Status != store.VersionPending {
			continue
		}
		val, err := s.Reveal(v)
		if err != nil {
			return nil, err
		}
		values[fieldKey[v.FieldID]] = val
	}
	return values, nil
}

// activate 把版本设为合法的当前值，同一身份同一字段的其他版本取消当前。
func activate(tx *gorm.DB, versionID, identityID, fieldID uint, now time.Time) error {
	if err := tx.Model(&store.Version{}).Where("identity_id = ? AND field_id = ? AND current = ?", identityID, fieldID, true).Update("current", false).Error; err != nil {
		return err
	}
	return tx.Model(&store.Version{}).Where("id = ?", versionID).
		Updates(map[string]any{"status": store.VersionValid, "current": true, "activated_at": now}).Error
}

// identityKey 决定这批回传属于哪个身份：
// 来源有身份字段且这批里有它的值时用这个值；没有时沿用同一订阅上一次的身份；都没有时用 default。
func (s *Service) identityKey(tx *gorm.DB, src store.Source, batch store.Batch, versions []store.Version, identityFieldID uint) (string, error) {
	if src.IdentityField == "" {
		return DefaultIdentity, nil
	}
	for _, v := range versions {
		if v.FieldID == identityFieldID && v.Status != store.VersionRejected {
			val, err := s.Reveal(v)
			if err != nil {
				return "", err
			}
			return truncate(val, 255), nil
		}
	}
	var prev store.Batch
	err := tx.Where("source_id = ? AND subscription_id = ? AND status = ? AND identity_id IS NOT NULL AND id <> ?", batch.SourceID, batch.SubscriptionID, store.BatchDone, batch.ID).
		Order("id DESC").First(&prev).Error
	if err == nil {
		var ident store.Identity
		if err := tx.First(&ident, *prev.IdentityID).Error; err == nil {
			return ident.Key, nil
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", err
	}
	return DefaultIdentity, nil
}
