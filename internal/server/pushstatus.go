package server

import (
	"net/http"
	"strconv"
	"time"

	"gorm.io/gorm"

	"github.com/lonelyman0108/ferry/internal/store"
)

// pushKey 区分推送任务：同一条推送规则下按身份分开；“合并为一条”的任务不带身份，身份记 0。
type pushKey struct{ binding, identity uint }

func keyOf(j store.PushJob) pushKey {
	k := pushKey{binding: j.BindingID}
	if j.IdentityID != nil {
		k.identity = *j.IdentityID
	}
	return k
}

// keyFor 是某个身份在某条推送规则下的任务键。
func keyFor(b store.Binding, identityID uint) pushKey {
	k := pushKey{binding: b.ID}
	if b.Mode == store.ModePerIdentity {
		k.identity = identityID
	}
	return k
}

// 界面上的推送状态：在 store 的任务状态之外，把排队中带着错误的单独算作等待重试，执行中并入排队中；
// none 表示还没推过。
const (
	pushRetrying = "retrying"
	pushNone     = "none"
)

func pushState(j store.PushJob) string {
	switch {
	case j.Status == store.PushQueued && j.LastError != "":
		return pushRetrying
	case j.Status == store.PushRunning:
		return store.PushQueued
	default:
		return j.Status
	}
}

// pushRank 是各状态的严重程度：一个身份有多条推送规则时，显示最严重的那个。
var pushRank = map[string]int{store.PushFailed: 6, pushRetrying: 5, store.PushQueued: 4, pushNone: 3, store.PushSkipped: 2, store.PushSuccess: 1}

// latestPushes 返回每个“推送规则 + 身份”最近一次的任务；bindingIDs 不为 nil 时只看这些推送规则。
func latestPushes(db *gorm.DB, bindingIDs []uint) (map[pushKey]store.PushJob, error) {
	out := map[pushKey]store.PushJob{}
	if bindingIDs != nil && len(bindingIDs) == 0 {
		return out, nil
	}
	q := db.Model(&store.PushJob{}).Select("MAX(id)").Group("binding_id, COALESCE(identity_id, 0)")
	if bindingIDs != nil {
		q = q.Where("binding_id IN ?", bindingIDs)
	}
	var jobs []store.PushJob
	if err := db.Where("id IN (?)", q).Find(&jobs).Error; err != nil {
		return nil, err
	}
	for _, j := range jobs {
		out[keyOf(j)] = j
	}
	return out, nil
}

// identityPushView 是身份所属来源的一条推送规则，以及推给这个身份的最近一次任务。
type identityPushView struct {
	BindingID       uint   `json:"binding_id"`
	DestinationID   uint   `json:"destination_id"`
	DestinationName string `json:"destination_name"`
	DestinationKind string `json:"destination_kind"`
	Mode            string `json:"mode"`
	// Enabled 表示推送规则和目的地都启用。
	Enabled bool      `json:"enabled"`
	Last    *pushView `json:"last"`
}

func (s *Server) handleIdentityPushes(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "ID 不合法")
		return
	}
	db := s.store().DB.WithContext(r.Context())
	var a store.Identity
	if err := db.First(&a, id).Error; err != nil {
		s.writeFindError(w, r, err, "身份不存在")
		return
	}
	out, err := identityPushes(db, a)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// identityPushes 列出身份所属来源的每条推送规则，以及推给这个身份的最近一次任务。
func identityPushes(db *gorm.DB, a store.Identity) ([]identityPushView, error) {
	var bindings []store.Binding
	if err := db.Where("source_id = ?", a.SourceID).Order("id").Find(&bindings).Error; err != nil {
		return nil, err
	}
	out := make([]identityPushView, 0, len(bindings))
	if len(bindings) == 0 {
		return out, nil
	}
	ids := make([]uint, 0, len(bindings))
	destIDs := make([]uint, 0, len(bindings))
	for _, b := range bindings {
		ids = append(ids, b.ID)
		destIDs = append(destIDs, b.DestinationID)
	}
	latest, err := latestPushes(db, ids)
	if err != nil {
		return nil, err
	}
	var dests []store.Destination
	if err := db.Where("id IN ?", destIDs).Find(&dests).Error; err != nil {
		return nil, err
	}
	byID := map[uint]store.Destination{}
	for _, d := range dests {
		byID[d.ID] = d
	}
	for _, b := range bindings {
		d := byID[b.DestinationID]
		v := identityPushView{BindingID: b.ID, DestinationID: d.ID, DestinationName: d.Name, DestinationKind: d.Kind, Mode: b.Mode, Enabled: b.Enabled && d.Enabled}
		if j, ok := latest[keyFor(b, a.ID)]; ok {
			pv := viewPush(j)
			v.Last = &pv
		}
		out = append(out, v)
	}
	return out, nil
}

// handleRetryPush 立即重试一个推送任务：还在排队等重试的马上执行；已经失败或跳过的，
// 按同一推送规则、同一身份重新排一次再执行。返回执行后的任务。
func (s *Server) handleRetryPush(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "ID 不合法")
		return
	}
	db := s.store().DB.WithContext(r.Context())
	var job store.PushJob
	if err := db.First(&job, id).Error; err != nil {
		s.writeFindError(w, r, err, "推送任务不存在")
		return
	}
	p, err := s.pusher()
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	switch job.Status {
	case store.PushQueued:
	case store.PushFailed, store.PushSkipped:
		var b store.Binding
		if err := db.First(&b, job.BindingID).Error; err != nil {
			s.writeFindError(w, r, err, "推送规则不存在")
			return
		}
		if job, err = p.Enqueue(r.Context(), b, job.IdentityID, "manual"); err != nil {
			s.internalError(w, r, err)
			return
		}
	default:
		writeError(w, http.StatusConflict, "推送任务正在执行或已成功，无需重试")
		return
	}
	if _, err := p.Run(r.Context(), job.ID); err != nil {
		s.internalError(w, r, err)
		return
	}
	if err := db.First(&job, job.ID).Error; err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(r, identityFrom(r.Context()).actor(), "push.retry", "binding:"+strconv.FormatUint(uint64(job.BindingID), 10), job.Status)
	writeJSON(w, http.StatusOK, viewPush(job))
}

// identityPushStates 算出每个身份的推送状态：看它所属来源每条启用的推送规则（目的地也启用）最近一次推给它的任务，
// 取最严重的那个；来源没有可用的推送规则时不出现在结果里。pushedAt 是最近一次推送成功的时间。
func identityPushStates(db *gorm.DB, idents []store.Identity) (state map[uint]string, pushedAt map[uint]*time.Time, err error) {
	state, pushedAt = map[uint]string{}, map[uint]*time.Time{}
	var dests []store.Destination
	if err := db.Where("enabled = ?", true).Find(&dests).Error; err != nil {
		return nil, nil, err
	}
	destOn := map[uint]bool{}
	for _, d := range dests {
		destOn[d.ID] = true
	}
	var bindings []store.Binding
	if err := db.Where("enabled = ?", true).Find(&bindings).Error; err != nil {
		return nil, nil, err
	}
	bySource := map[uint][]store.Binding{}
	for _, b := range bindings {
		if destOn[b.DestinationID] {
			bySource[b.SourceID] = append(bySource[b.SourceID], b)
		}
	}
	latest, err := latestPushes(db, nil)
	if err != nil {
		return nil, nil, err
	}
	for _, a := range idents {
		bs := bySource[a.SourceID]
		if len(bs) == 0 {
			continue
		}
		worst := ""
		for _, b := range bs {
			st := pushNone
			if j, ok := latest[keyFor(b, a.ID)]; ok {
				st = pushState(j)
				if j.Status == store.PushSuccess && j.FinishedAt != nil && (pushedAt[a.ID] == nil || j.FinishedAt.After(*pushedAt[a.ID])) {
					pushedAt[a.ID] = j.FinishedAt
				}
			}
			if pushRank[st] > pushRank[worst] {
				worst = st
			}
		}
		state[a.ID] = worst
	}
	return state, pushedAt, nil
}
