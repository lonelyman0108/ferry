package server

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/lonelyman0108/ferry/internal/probe"
	"github.com/lonelyman0108/ferry/internal/rules"
	"github.com/lonelyman0108/ferry/internal/source"
	"github.com/lonelyman0108/ferry/internal/store"
	"github.com/lonelyman0108/ferry/internal/validate"
)

type sourceSummary struct {
	ID        uint      `json:"id"`
	Key       string    `json:"key"`
	Name      string    `json:"name"`
	Enabled   bool      `json:"enabled"`
	Matchers  int       `json:"matchers"`
	Fields    int       `json:"fields"`
	Outputs   int       `json:"outputs"`
	UpdatedAt time.Time `json:"updated_at"`
}

type sourceView struct {
	ID uint `json:"id"`
	source.Doc
	UpdatedAt time.Time `json:"updated_at"`
}

func viewSource(s store.Source) sourceView {
	return sourceView{ID: s.ID, Doc: source.FromModel(s), UpdatedAt: s.UpdatedAt}
}

// writeSourceError 把 source 包的错误映射成 HTTP 状态；其他错误按内部错误处理。
func (s *Server) writeSourceError(w http.ResponseWriter, r *http.Request, err error) {
	var ve *source.ValidationError
	switch {
	case errors.As(err, &ve):
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "来源定义有误", Details: ve.Problems})
	case errors.Is(err, source.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, source.ErrKeyExists), errors.Is(err, source.ErrKeyChanged):
		writeError(w, http.StatusConflict, err.Error())
	default:
		s.internalError(w, r, err)
	}
}

func sourceID(r *http.Request) (uint, bool) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	return uint(id), err == nil
}

func (s *Server) handleListSources(w http.ResponseWriter, r *http.Request) {
	all, err := source.LoadAll(r.Context(), s.store().DB)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]sourceSummary, 0, len(all))
	for _, x := range all {
		out = append(out, sourceSummary{
			ID: x.ID, Key: x.Key, Name: x.Name, Enabled: x.Enabled,
			Matchers: len(x.Matchers), Fields: len(x.Fields), Outputs: len(x.Outputs), UpdatedAt: x.UpdatedAt,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetSource(w http.ResponseWriter, r *http.Request) {
	id, ok := sourceID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "来源 ID 不合法")
		return
	}
	x, err := source.Load(r.Context(), s.store().DB, id)
	if err != nil {
		s.writeSourceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, viewSource(x))
}

// createSource 校验并保存新来源，成功后写审计并返回 201。
func (s *Server) createSource(w http.ResponseWriter, r *http.Request, d source.Doc, action string) {
	d.Normalize()
	if err := d.Validate(); err != nil {
		s.writeSourceError(w, r, err)
		return
	}
	x, err := source.Create(r.Context(), s.store().DB, d)
	if err != nil {
		s.writeSourceError(w, r, err)
		return
	}
	s.audit(r, identityFrom(r.Context()).actor(), action, "source:"+x.Key, "")
	s.checkMitm(r.Context())
	writeJSON(w, http.StatusCreated, viewSource(x))
}

func (s *Server) handleCreateSource(w http.ResponseWriter, r *http.Request) {
	var d source.Doc
	if !decodeJSON(w, r, &d) {
		return
	}
	s.createSource(w, r, d, "source.create")
}

type importInput struct {
	YAML string `json:"yaml"`
}

// handleImportSource 导入 YAML 文本。为了让写请求统一走 JSON（跨站防护依赖这一点），YAML 放在 JSON 字段里。
func (s *Server) handleImportSource(w http.ResponseWriter, r *http.Request) {
	var in importInput
	if !decodeJSON(w, r, &in) {
		return
	}
	d, err := source.UnmarshalYAML([]byte(in.YAML))
	var ve *source.ValidationError
	if err != nil && !errors.As(err, &ve) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.createSource(w, r, d, "source.import")
}

func (s *Server) handleUpdateSource(w http.ResponseWriter, r *http.Request) {
	id, ok := sourceID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "来源 ID 不合法")
		return
	}
	var d source.Doc
	if !decodeJSON(w, r, &d) {
		return
	}
	d.Normalize()
	if err := d.Validate(); err != nil {
		s.writeSourceError(w, r, err)
		return
	}
	x, err := source.Update(r.Context(), s.store().DB, id, d)
	if err != nil {
		s.writeSourceError(w, r, err)
		return
	}
	s.audit(r, identityFrom(r.Context()).actor(), "source.update", "source:"+x.Key, "")
	s.checkMitm(r.Context())
	writeJSON(w, http.StatusOK, viewSource(x))
}

func (s *Server) handleDeleteSource(w http.ResponseWriter, r *http.Request) {
	id, ok := sourceID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "来源 ID 不合法")
		return
	}
	x, err := source.Load(r.Context(), s.store().DB, id)
	if err == nil {
		err = source.Delete(r.Context(), s.store().DB, id)
	}
	if err != nil {
		s.writeSourceError(w, r, err)
		return
	}
	s.audit(r, identityFrom(r.Context()).actor(), "source.delete", "source:"+x.Key, "")
	s.checkMitm(r.Context())
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleExportSource(w http.ResponseWriter, r *http.Request) {
	id, ok := sourceID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "来源 ID 不合法")
		return
	}
	x, err := source.Load(r.Context(), s.store().DB, id)
	if err != nil {
		s.writeSourceError(w, r, err)
		return
	}
	data, err := source.MarshalYAML(source.FromModel(x))
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="ferry-%s.yaml"`, x.Key))
	_, _ = w.Write(data)
}

type exampleView struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (s *Server) handleListExamples(w http.ResponseWriter, _ *http.Request) {
	out := []exampleView{}
	for _, e := range source.Examples() {
		out = append(out, exampleView{Key: e.Key, Name: e.Name, Description: e.Description})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleImportExample(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	for _, e := range source.Examples() {
		if e.Key == key {
			s.createSource(w, r, e.Doc, "source.import_example")
			return
		}
	}
	writeError(w, http.StatusNotFound, "示例不存在")
}

// tryInput 是一次试跑：来源定义（可以是还没保存的草稿）加一条样例请求。
// Probe 为 true 时用取到的值真实请求一次探测接口。
type tryInput struct {
	Source  source.Doc        `json:"source"`
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
	Probe   bool              `json:"probe"`
}

type tryValue struct {
	OK    bool   `json:"ok"`
	Value string `json:"value,omitempty"`
	Error string `json:"error,omitempty"`
	// Invalid 是取到了值、但没通过字段校验的原因。
	Invalid   string     `json:"invalid,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type tryMatcher struct {
	Name    string `json:"name"`
	Matched bool   `json:"matched"`
	Reason  string `json:"reason,omitempty"`
}

type tryProbe struct {
	OK       bool   `json:"ok"`
	Detail   string `json:"detail,omitempty"`
	Identity string `json:"identity,omitempty"`
	Error    string `json:"error,omitempty"`
}

type tryResult struct {
	Matched  []string            `json:"matched"`
	Matchers []tryMatcher        `json:"matchers"`
	Fields   map[string]tryValue `json:"fields"`
	Outputs  map[string]tryValue `json:"outputs"`
	Probe    *tryProbe           `json:"probe,omitempty"`
}

// handleTrySource 用一条样例请求试跑来源定义，什么都不保存。定义不要求完整，
// 每条规则、每个字段各自给出结果，编辑途中就能看到效果。样例是管理员自己贴进来的，结果原样返回。
func (s *Server) handleTrySource(w http.ResponseWriter, r *http.Request) {
	var in tryInput
	if !decodeJSON(w, r, &in) {
		return
	}
	d := in.Source
	d.Normalize()
	req := rules.Request{Method: in.Method, URL: in.URL, Headers: http.Header{}, Body: []byte(in.Body)}
	if req.Method == "" {
		req.Method = http.MethodGet
	}
	for k, v := range in.Headers {
		req.Headers.Add(k, v)
	}

	res := tryResult{Matched: []string{}, Matchers: []tryMatcher{}, Fields: map[string]tryValue{}, Outputs: map[string]tryValue{}}
	matched := map[string]bool{}
	for _, m := range d.Matchers {
		tm := tryMatcher{Name: m.Name, Reason: matchReason(m, req)}
		if tm.Reason == "" {
			tm.Matched = true
			matched[m.Name] = true
			res.Matched = append(res.Matched, m.Name)
		}
		res.Matchers = append(res.Matchers, tm)
	}
	values := map[string]string{}
	now := time.Now()
	for _, f := range d.Fields {
		if f.Key == "" {
			continue
		}
		applies := len(matched) > 0 && (f.Matcher == "" || matched[f.Matcher])
		if !applies {
			res.Fields[f.Key] = tryValue{Error: "请求未命中该字段对应的匹配规则"}
			continue
		}
		rf := rules.Field{In: f.In, Path: f.Path, RegexOn: f.RegexOn}
		if err := rf.Validate(); err != nil {
			res.Fields[f.Key] = tryValue{Error: err.Error()}
			continue
		}
		v, ok, err := rf.Extract(req)
		switch {
		case err != nil:
			res.Fields[f.Key] = tryValue{Error: err.Error()}
		case !ok:
			res.Fields[f.Key] = tryValue{Error: "未读取到值"}
		default:
			values[f.Key] = v
			tv := tryValue{OK: true, Value: v}
			if c := validate.Check(f, v, now); c.Err != nil {
				tv.Invalid = c.Err.Error()
			} else {
				tv.ExpiresAt = c.ExpiresAt
			}
			res.Fields[f.Key] = tv
		}
	}
	for _, o := range d.Outputs {
		if o.Name == "" {
			continue
		}
		t, err := rules.ParseTemplate(o.Template)
		if err == nil {
			var out string
			if out, err = t.Render(values); err == nil {
				res.Outputs[o.Name] = tryValue{OK: true, Value: out}
				continue
			}
		}
		res.Outputs[o.Name] = tryValue{Error: err.Error()}
	}
	if in.Probe && d.Probe != nil {
		res.Probe = s.tryProbe(r, d, values)
	}
	writeJSON(w, http.StatusOK, res)
}

// matchReason 返回请求没命中规则的原因，命中时返回空串。
func matchReason(m source.MatcherDoc, req rules.Request) string {
	rm := rules.Matcher{Host: m.Host, PathRegex: m.Path, Method: m.Method}
	if err := rm.Validate(); err != nil {
		return err.Error()
	}
	u, err := url.Parse(req.URL)
	if err != nil || u.Host == "" {
		return "样例 URL 不合法"
	}
	rm.Method = ""
	switch {
	case !rules.HostMatches(m.Host, u.Hostname()):
		return "域名不匹配"
	case !rm.Match(req):
		return "路径不匹配"
	case m.Method != "" && !strings.EqualFold(m.Method, req.Method):
		return "请求方法不匹配"
	}
	return ""
}

// tryProbe 用样例里取到的值真实请求一次探测接口。
func (s *Server) tryProbe(r *http.Request, d source.Doc, values map[string]string) *tryProbe {
	keys := make([]string, 0, len(d.Fields))
	for _, f := range d.Fields {
		keys = append(keys, f.Key)
	}
	if err := d.Probe.Validate(keys); err != nil {
		return &tryProbe{Error: err.Error()}
	}
	pr := probe.Run(r.Context(), s.HTTP, *d.Probe, values)
	if pr.Err != nil {
		return &tryProbe{Error: pr.Err.Error()}
	}
	return &tryProbe{OK: pr.OK, Detail: pr.Detail, Identity: pr.Identity}
}
