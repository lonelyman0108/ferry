package server

import (
	"net/http"
	"time"

	"github.com/lonelyman0108/ferry/internal/store"
)

// laneIdentity 是来源下的一个身份：各字段的当前值（敏感字段打码）、待审核数，以及每条推送规则推给它的最近一次任务。
type laneIdentity struct {
	identityView
	Pushes []identityPushView `json:"pushes"`
}

// laneBinding 是来源的一条推送规则，带目的地名称和推送状态（口径同流向图的连线）。
type laneBinding struct {
	flowBinding
	DestinationName string `json:"destination_name"`
	DestinationKind string `json:"destination_kind"`
}

// laneView 是来源详情一次取回的全部内容：来源状态、身份与当前值、推送规则、覆盖它的设备、和它有关的待处理、最近记录。
type laneView struct {
	Source        overviewSource  `json:"source"`
	Review        string          `json:"review"`
	Probe         bool            `json:"probe"`
	IdentityField string          `json:"identity_field"`
	Identities    []laneIdentity  `json:"identities"`
	Bindings      []laneBinding   `json:"bindings"`
	Devices       []flowDevice    `json:"devices"`
	Items         []attentionItem `json:"items"`
	Recent        []activityItem  `json:"recent"`
}

const laneRecent = 10

// handleSourceLane 给来源详情用：从手机到目的地，这个来源整条链路的状态。
func (s *Server) handleSourceLane(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "ID 不合法")
		return
	}
	ctx := r.Context()
	db := s.store().DB.WithContext(ctx)
	var src store.Source
	if err := db.First(&src, id).Error; err != nil {
		s.writeFindError(w, r, err, "来源不存在")
		return
	}
	ov, err := s.overview(ctx)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := laneView{Review: src.ReviewMode, Probe: src.Probe != "", IdentityField: src.IdentityField,
		Identities: []laneIdentity{}, Bindings: []laneBinding{}, Devices: []flowDevice{}, Items: []attentionItem{}}
	for _, x := range ov.Sources {
		if x.ID == src.ID {
			out.Source = x
		}
	}

	var idents []store.Identity
	if err := db.Where("source_id = ?", src.ID).Order("last_seen_at DESC").Find(&idents).Error; err != nil {
		s.internalError(w, r, err)
		return
	}
	for _, a := range idents {
		v, err := s.identityDetail(db, a)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		pushes, err := identityPushes(db, a)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		out.Identities = append(out.Identities, laneIdentity{identityView: v, Pushes: pushes})
	}

	dests, bindings, err := flowPushes(db)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	destByID := map[uint]flowDestination{}
	for _, d := range dests {
		destByID[d.ID] = d
	}
	for _, b := range bindings {
		if b.SourceID == src.ID {
			d := destByID[b.DestinationID]
			out.Bindings = append(out.Bindings, laneBinding{flowBinding: b, DestinationName: d.Name, DestinationKind: d.Kind})
		}
	}

	devices, err := flowDevices(ctx, db)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	for _, d := range devices {
		for _, sid := range d.SourceIDs {
			if sid == src.ID {
				out.Devices = append(out.Devices, d)
				break
			}
		}
	}

	items, err := s.attentionItems(ctx)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	for _, it := range items {
		if it.SourceID == src.ID {
			out.Items = append(out.Items, it)
		}
	}

	if out.Recent, err = s.activity(ctx, activityFilter{SourceID: src.ID, Before: s.now().Add(time.Minute), Limit: laneRecent}); err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
