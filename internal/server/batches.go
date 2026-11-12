package server

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/lonelyman0108/ferry/internal/ingest"
	"github.com/lonelyman0108/ferry/internal/store"
)

// batchView 是一个收集中的批次：手机已经回传、还在等合并窗口结束的值。
type batchView struct {
	ID               uint         `json:"id"`
	SourceID         uint         `json:"source_id"`
	SourceKey        string       `json:"source_key"`
	SourceName       string       `json:"source_name"`
	SubscriptionName string       `json:"subscription_name"`
	CreatedAt        time.Time    `json:"created_at"`
	ClosesAt         time.Time    `json:"closes_at"`
	Values           []batchValue `json:"values"`
	// Missing 是来源声明了、但这一批还没收到的字段。
	Missing []string `json:"missing"`
}

type batchValue struct {
	versionView
	Name string `json:"name"`
}

func (s *Server) handleListBatches(w http.ResponseWriter, r *http.Request) {
	db := s.store().DB.WithContext(r.Context())
	var batches []store.Batch
	if err := db.Where("status = ?", store.BatchCollecting).Order("id DESC").Find(&batches).Error; err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]batchView, 0, len(batches))
	for _, b := range batches {
		var src store.Source
		if err := db.Preload("Fields").First(&src, b.SourceID).Error; err != nil {
			s.internalError(w, r, err)
			return
		}
		var sub store.Subscription
		if err := db.First(&sub, b.SubscriptionID).Error; err != nil {
			s.internalError(w, r, err)
			return
		}
		var vs []store.Version
		if err := db.Where("batch_id = ? AND status <> ?", b.ID, store.VersionRejected).Order("id").Find(&vs).Error; err != nil {
			s.internalError(w, r, err)
			return
		}
		v := batchView{
			ID: b.ID, SourceID: src.ID, SourceKey: src.Key, SourceName: src.Name, SubscriptionName: sub.Name,
			CreatedAt: b.CreatedAt, ClosesAt: b.ClosesAt, Values: []batchValue{}, Missing: []string{},
		}
		got := map[uint]bool{}
		for _, ver := range vs {
			for _, f := range src.Fields {
				if f.ID != ver.FieldID {
					continue
				}
				vv, err := s.viewVersion(ver, f)
				if err != nil {
					s.internalError(w, r, err)
					return
				}
				v.Values = append(v.Values, batchValue{versionView: vv, Name: f.Name})
				got[f.ID] = true
			}
		}
		for _, f := range src.Fields {
			if !got[f.ID] {
				v.Missing = append(v.Missing, f.Key)
			}
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleProcessBatch(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "ID 不合法")
		return
	}
	svc, err := s.ingest()
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	done, err := svc.ProcessNow(r.Context(), id)
	switch {
	case errors.Is(err, ingest.ErrBatchNotFound):
		writeError(w, http.StatusNotFound, err.Error())
		return
	case err != nil:
		s.internalError(w, r, err)
		return
	case !done:
		writeError(w, http.StatusConflict, "批次已处理")
		return
	}
	s.audit(r, identityFrom(r.Context()).actor(), "batch.process", "batch:"+strconv.FormatUint(uint64(id), 10), "")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
