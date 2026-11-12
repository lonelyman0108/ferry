package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"gorm.io/gorm"

	"github.com/lonelyman0108/ferry/internal/source"
	"github.com/lonelyman0108/ferry/internal/store"
)

// flowDevice 是流向图左列的一台设备，也就是一个订阅。SourceIDs 是它覆盖的来源（含停用的）。
type flowDevice struct {
	ID            uint       `json:"id"`
	Name          string     `json:"name"`
	Client        string     `json:"client"`
	Status        string     `json:"status"`
	Outdated      bool       `json:"outdated"`
	LastSeenAt    *time.Time `json:"last_seen_at"`
	TransportMode string     `json:"transport_mode"`
	SourceIDs     []uint     `json:"source_ids"`
}

// flowDestination 是流向图右列的一个目的地。State 是推到它的各条启用的推送规则里最严重的推送状态，
// 没有可用的推送规则时为空。LastAt 是最近一次推送的时间。
type flowDestination struct {
	ID        uint       `json:"id"`
	Name      string     `json:"name"`
	Kind      string     `json:"kind"`
	Enabled   bool       `json:"enabled"`
	Bindings  int        `json:"bindings"`
	State     string     `json:"state"`
	LastError string     `json:"last_error,omitempty"`
	LastAt    *time.Time `json:"last_at"`
}

// flowBinding 是来源到目的地的一条连线，即一条推送规则。State 是这条规则下各身份最近一次任务里最严重的状态；
// PushID 是那次任务，失败时用它重试。
type flowBinding struct {
	ID            uint       `json:"id"`
	SourceID      uint       `json:"source_id"`
	DestinationID uint       `json:"destination_id"`
	Mode          string     `json:"mode"`
	Enabled       bool       `json:"enabled"`
	State         string     `json:"state"`
	PushID        uint       `json:"push_id,omitempty"`
	LastError     string     `json:"last_error,omitempty"`
	LastAt        *time.Time `json:"last_at"`
}

type flowView struct {
	Sources      []overviewSource  `json:"sources"`
	Devices      []flowDevice      `json:"devices"`
	Destinations []flowDestination `json:"destinations"`
	Bindings     []flowBinding     `json:"bindings"`
}

// handleFlow 给总览的流向图用：设备 → 来源 → 目的地，以及各条连线的状态。
func (s *Server) handleFlow(w http.ResponseWriter, r *http.Request) {
	out, err := s.flow(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) flow(ctx context.Context) (flowView, error) {
	db := s.store().DB.WithContext(ctx)
	ov, err := s.overview(ctx)
	if err != nil {
		return flowView{}, err
	}
	out := flowView{Sources: ov.Sources}
	if out.Devices, err = flowDevices(ctx, db); err != nil {
		return out, err
	}
	out.Destinations, out.Bindings, err = flowPushes(db)
	return out, err
}

// flowDevices 列出全部订阅和它们覆盖的来源。状态口径同订阅列表。
func flowDevices(ctx context.Context, db *gorm.DB) ([]flowDevice, error) {
	srcs, err := source.LoadAll(ctx, db)
	if err != nil {
		return nil, err
	}
	var subs []store.Subscription
	if err := db.Order("id").Find(&subs).Error; err != nil {
		return nil, err
	}
	out := make([]flowDevice, 0, len(subs))
	for _, m := range subs {
		d := flowDevice{ID: m.ID, Name: m.Name, Client: m.Client, TransportMode: m.TransportMode, SourceIDs: []uint{}}
		d.LastSeenAt = laterOf(m.LastFetchedAt, m.TransportLastAt)
		switch {
		case d.LastSeenAt != nil:
			d.Status = subConnected
		case m.FileFetchedAt != nil:
			d.Status = subInstalled
		default:
			d.Status = subNotInstalled
		}
		covered := subscriptionSources(srcs, m)
		for _, x := range covered {
			d.SourceIDs = append(d.SourceIDs, x.ID)
		}
		d.Outdated = m.FileMitmHash != "" && m.FileMitmHash != mitmHash(subscriptionDocsFrom(srcs, m))
		out = append(out, d)
	}
	return out, nil
}

// subscriptionSources 是订阅覆盖的来源（不管启用没有），顺序同 srcs。
func subscriptionSources(srcs []store.Source, m store.Subscription) []store.Source {
	if m.AllSources {
		return srcs
	}
	var list []string
	_ = json.Unmarshal([]byte(m.SourceKeys), &list)
	keys := map[string]bool{}
	for _, k := range list {
		keys[k] = true
	}
	var out []store.Source
	for _, x := range srcs {
		if keys[x.Key] {
			out = append(out, x)
		}
	}
	return out
}

// flowPushes 算出每个目的地和每条推送规则的推送状态。停用的推送规则、停用的目的地照样列出，状态为空。
func flowPushes(db *gorm.DB) ([]flowDestination, []flowBinding, error) {
	var dests []store.Destination
	if err := db.Order("id").Find(&dests).Error; err != nil {
		return nil, nil, err
	}
	var bindings []store.Binding
	if err := db.Order("id").Find(&bindings).Error; err != nil {
		return nil, nil, err
	}
	latest, err := latestPushes(db, nil)
	if err != nil {
		return nil, nil, err
	}
	byBinding := map[uint][]store.PushJob{}
	for _, j := range latest {
		byBinding[j.BindingID] = append(byBinding[j.BindingID], j)
	}
	destOn := map[uint]bool{}
	for _, d := range dests {
		destOn[d.ID] = d.Enabled
	}

	outB := make([]flowBinding, 0, len(bindings))
	worstOf := map[uint]flowBinding{}
	count := map[uint]int{}
	lastAt := map[uint]*time.Time{}
	for _, b := range bindings {
		fb := flowBinding{ID: b.ID, SourceID: b.SourceID, DestinationID: b.DestinationID, Mode: b.Mode, Enabled: b.Enabled && destOn[b.DestinationID]}
		count[b.DestinationID]++
		if fb.Enabled {
			for _, j := range byBinding[b.ID] {
				at := jobTime(j)
				if fb.LastAt == nil || at.After(*fb.LastAt) {
					fb.LastAt = &at
				}
				if st := pushState(j); pushRank[st] > pushRank[fb.State] {
					fb.State, fb.PushID, fb.LastError = st, j.ID, j.LastError
				}
			}
			if fb.State == "" {
				fb.State = pushNone
			}
			if w, ok := worstOf[b.DestinationID]; !ok || pushRank[fb.State] > pushRank[w.State] {
				worstOf[b.DestinationID] = fb
			}
			if fb.LastAt != nil && (lastAt[b.DestinationID] == nil || fb.LastAt.After(*lastAt[b.DestinationID])) {
				lastAt[b.DestinationID] = fb.LastAt
			}
		}
		outB = append(outB, fb)
	}

	outD := make([]flowDestination, 0, len(dests))
	for _, d := range dests {
		fd := flowDestination{ID: d.ID, Name: d.Name, Kind: d.Kind, Enabled: d.Enabled, Bindings: count[d.ID], LastAt: lastAt[d.ID]}
		if w, ok := worstOf[d.ID]; ok {
			fd.State, fd.LastError = w.State, w.LastError
		}
		outD = append(outD, fd)
	}
	return outD, outB, nil
}

// jobTime 是任务最近一次有动静的时间：结束了取结束时间，否则取创建时间。
func jobTime(j store.PushJob) time.Time {
	if j.FinishedAt != nil {
		return *j.FinishedAt
	}
	return j.CreatedAt
}
