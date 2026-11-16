package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/lonelyman0108/ferry/internal/ghnet"
	"github.com/lonelyman0108/ferry/internal/probe"
	"github.com/lonelyman0108/ferry/internal/rules"
	"github.com/lonelyman0108/ferry/internal/source"
	"github.com/lonelyman0108/ferry/internal/sourcecheck"
	"github.com/lonelyman0108/ferry/internal/store"
	"github.com/lonelyman0108/ferry/internal/validate"
)

type sourceSummary struct {
	ID        uint      `json:"id"`
	Key       string    `json:"key"`
	Name      string    `json:"name"`
	Icon      string    `json:"icon,omitempty"`
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
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "来源定义有误", Details: ve.Details(), Problems: ve.Problems})
	case errors.Is(err, source.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, source.ErrKeyExists), errors.Is(err, source.ErrKeyChanged):
		// 两者都出在 key 上，附上位置，界面把错误显示在来源标识的输入框下方。
		writeJSON(w, http.StatusConflict, errorBody{Error: err.Error(), Problems: []source.Problem{{Path: "key", Message: err.Error()}}})
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
			ID: x.ID, Key: x.Key, Name: x.Name, Icon: x.Icon, Enabled: x.Enabled,
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

const (
	importFetchTimeout = 10 * time.Second
	conflictOverwrite  = "overwrite"
	conflictRename     = "rename"
)

type importPreviewInput struct {
	Text  string   `json:"text,omitempty"`
	Texts []string `json:"texts,omitempty"`
	URL   string   `json:"url,omitempty"`
}

type importPreviewItem struct {
	Meta          source.Meta         `json:"meta"`
	Source        source.Doc          `json:"source"`
	Valid         bool                `json:"valid"`
	Problems      []source.Problem    `json:"problems,omitempty"`
	Details       []string            `json:"details,omitempty"`
	Violations    []sourcecheck.Issue `json:"violations,omitempty"`
	Warnings      []sourcecheck.Issue `json:"warnings,omitempty"`
	Hosts         []string            `json:"hosts,omitempty"`
	ProbeHost     string              `json:"probe_host,omitempty"`
	Conflict      bool                `json:"conflict"`
	ExistingID    uint                `json:"existing_id,omitempty"`
	ExistingName  string              `json:"existing_name,omitempty"`
	LocalModified bool                `json:"local_modified,omitempty"`
}

type importPreviewOutput struct {
	URL       string              `json:"url,omitempty"`
	GitHubURL bool                `json:"github_url,omitempty"`
	Items     []importPreviewItem `json:"items"`
}

func (s *Server) fetchImportURL(ctx context.Context, rawURL string) ([]byte, bool, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, false, errors.New("链接须为 http 或 https 地址")
	}
	ghURL := ghnet.IsGitHubHost(u.Hostname())

	ctx, cancel := context.WithTimeout(ctx, importFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, ghURL, fmt.Errorf("构造请求失败：%v", err)
	}
	req.Header.Set("Accept", "application/json, text/plain;q=0.9, */*;q=0.8")
	client, err := s.githubClient(ctx, importFetchTimeout)
	if err != nil {
		return nil, ghURL, fmt.Errorf("初始化网络客户端失败：%v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, ghURL, fmt.Errorf("拉取导入链接失败：%v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, ghURL, fmt.Errorf("拉取导入链接失败：HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, source.MaxImportBytes+1))
	if err != nil {
		return nil, ghURL, fmt.Errorf("读取导入内容失败：%v", err)
	}
	if len(data) > source.MaxImportBytes {
		return nil, ghURL, fmt.Errorf("导入内容超过 %d KB", source.MaxImportBytes>>10)
	}
	return data, ghURL, nil
}

// tryFetchSiblingIcon 在导入链接形如 .../source.json 且未内嵌图标时，尝试拉取同目录的 icon.* 文件并编码为 data URI。
func (s *Server) tryFetchSiblingIcon(ctx context.Context, sourceURL string) string {
	u, err := url.Parse(strings.TrimSpace(sourceURL))
	if err != nil || !strings.HasSuffix(u.Path, "/source.json") {
		return ""
	}
	basePath := strings.TrimSuffix(u.Path, "source.json")
	client, err := s.githubClient(ctx, 4*time.Second)
	if err != nil {
		return ""
	}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("重定向次数过多")
		}
		if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
			return errors.New("不允许跳转到非 HTTP(S) 协议")
		}
		return nil
	}
	for _, ext := range []string{".svg", ".png", ".webp", ".jpg", ".jpeg"} {
		mime, ok := source.IconMIMEByExt(ext)
		if !ok {
			continue
		}
		iconURL := *u
		iconURL.Path = basePath + "icon" + ext
		reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, iconURL.String(), nil)
		if err != nil {
			cancel()
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			cancel()
			continue
		}
		raw, rerr := io.ReadAll(io.LimitReader(resp.Body, source.MaxIconBytes+1))
		resp.Body.Close()
		cancel()
		if rerr != nil || resp.StatusCode != http.StatusOK {
			continue
		}
		if dataURI, err := source.EncodeIconBytes(mime, raw); err == nil {
			return dataURI
		}
	}
	return ""
}

func (s *Server) handleImportPreview(w http.ResponseWriter, r *http.Request) {
	var in importPreviewInput
	if !decodeJSON(w, r, &in) {
		return
	}

	rawURL := strings.TrimSpace(in.URL)
	text := strings.TrimSpace(in.Text)
	var envs []source.Envelope
	var ghURL bool

	switch {
	case rawURL != "":
		if text != "" || len(in.Texts) > 0 {
			writeError(w, http.StatusBadRequest, "链接导入与文本导入不能同时提供")
			return
		}
		data, isGH, err := s.fetchImportURL(r.Context(), rawURL)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		ghURL = isGH
		parsed, err := source.DecodeEnvelopes(data)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if len(parsed) == 1 && parsed[0].Source.Icon == "" {
			if iconData := s.tryFetchSiblingIcon(r.Context(), rawURL); iconData != "" {
				parsed[0].Source.Icon = iconData
			}
		}
		envs = parsed
	case len(in.Texts) > 0:
		if text != "" {
			writeError(w, http.StatusBadRequest, "请勿同时传入 text 与 texts")
			return
		}
		totalBytes := 0
		for i, t := range in.Texts {
			totalBytes += len(t)
			if totalBytes > source.MaxImportBytes {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("导入文件总大小超过 %d KB", source.MaxImportBytes>>10))
				return
			}
			parsed, err := source.DecodeEnvelopes([]byte(t))
			if err != nil {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("文件 %d：%v", i+1, err))
				return
			}
			envs = append(envs, parsed...)
			if len(envs) > source.MaxImportItems {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("单次最多导入 %d 个来源", source.MaxImportItems))
				return
			}
		}
		if len(envs) == 0 {
			writeError(w, http.StatusBadRequest, "未提供有效的导入内容")
			return
		}
	case text != "":
		parsed, err := source.DecodeEnvelopes([]byte(text))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		envs = parsed
	default:
		writeError(w, http.StatusBadRequest, "请提供 JSON 内容或导入链接")
		return
	}

	existingList, err := source.LoadAll(r.Context(), s.store().DB)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	byKey := make(map[string]store.Source, len(existingList))
	for _, x := range existingList {
		byKey[x.Key] = x
	}

	items := make([]importPreviewItem, 0, len(envs))
	for _, env := range envs {
		item := importPreviewItem{
			Meta:   env.Meta,
			Source: env.Source,
			Valid:  true,
		}
		if err := env.Source.Validate(); err != nil {
			item.Valid = false
			var ve *source.ValidationError
			if errors.As(err, &ve) {
				item.Problems = ve.Problems
				item.Details = ve.Details()
			} else {
				item.Details = []string{err.Error()}
			}
		}
		rep := sourcecheck.Check(env.Source)
		item.Violations = rep.Violations
		item.Warnings = rep.Warnings
		item.Hosts = rep.Hosts
		item.ProbeHost = rep.ProbeHost

		if ex, ok := byKey[env.Source.Key]; ok {
			item.Conflict = true
			item.ExistingID = ex.ID
			item.ExistingName = ex.Name
			item.LocalModified = source.HashDoc(source.FromModel(ex)) != source.HashDoc(env.Source)
		}
		items = append(items, item)
	}

	writeJSON(w, http.StatusOK, importPreviewOutput{
		URL:       rawURL,
		GitHubURL: ghURL,
		Items:     items,
	})
}

type importApplyItem struct {
	Meta        source.Meta `json:"meta"`
	Source      source.Doc  `json:"source"`
	Conflict    string      `json:"conflict,omitempty"`
	NewKey      string      `json:"new_key,omitempty"`
	Acknowledge bool        `json:"acknowledge,omitempty"`
}

type importApplyInput struct {
	Items []importApplyItem `json:"items"`
}

type importApplyResult struct {
	Index    int              `json:"index"`
	Key      string           `json:"key"`
	Name     string           `json:"name"`
	Action   string           `json:"action,omitempty"`
	OK       bool             `json:"ok"`
	Source   *sourceView      `json:"source,omitempty"`
	Error    string           `json:"error,omitempty"`
	Details  []string         `json:"details,omitempty"`
	Problems []source.Problem `json:"problems,omitempty"`
}

type importApplyOutput struct {
	Results   []importApplyResult `json:"results"`
	Succeeded int                 `json:"succeeded"`
	Failed    int                 `json:"failed"`
}

// handleImportSource 逐条应用预览确认后的来源导入。
func (s *Server) handleImportSource(w http.ResponseWriter, r *http.Request) {
	var in importApplyInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if len(in.Items) == 0 {
		writeError(w, http.StatusBadRequest, "导入列表为空")
		return
	}
	if len(in.Items) > source.MaxImportItems {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("单次最多导入 %d 个来源", source.MaxImportItems))
		return
	}

	actor := identityFrom(r.Context()).actor()
	out := importApplyOutput{Results: make([]importApplyResult, 0, len(in.Items))}

	for i, item := range in.Items {
		d := item.Source
		if item.Conflict == conflictRename {
			nk := strings.TrimSpace(item.NewKey)
			if nk == "" {
				out.Failed++
				out.Results = append(out.Results, importApplyResult{
					Index:    i,
					Key:      d.Key,
					Name:     d.Name,
					OK:       false,
					Error:    "请填写新的来源标识",
					Problems: []source.Problem{{Path: "key", Message: "不能为空"}},
				})
				continue
			}
			d.Key = nk
		}

		d.Normalize()
		res := importApplyResult{Index: i, Key: d.Key, Name: d.Name}

		meta := item.Meta
		meta.Normalize()
		if meta.Version < 1 {
			meta.Version = 1
		}
		if err := meta.Validate(); err != nil {
			res.Error = err.Error()
			out.Failed++
			out.Results = append(out.Results, res)
			continue
		}

		if err := d.Validate(); err != nil {
			var ve *source.ValidationError
			if errors.As(err, &ve) {
				res.Error = "来源定义有误"
				res.Details = ve.Details()
				res.Problems = ve.Problems
			} else {
				res.Error = err.Error()
			}
			out.Failed++
			out.Results = append(out.Results, res)
			continue
		}

		if rep := sourcecheck.Check(d); rep.HasViolations() && !item.Acknowledge {
			res.Error = "该来源违反安全规则，请勾选「我已了解风险」后再导入"
			out.Failed++
			out.Results = append(out.Results, res)
			continue
		}

		switch item.Conflict {
		case "", conflictRename:
			created, err := source.Create(r.Context(), s.store().DB, d)
			if err != nil {
				if errors.Is(err, source.ErrKeyExists) {
					res.Error = err.Error()
					res.Problems = []source.Problem{{Path: "key", Message: err.Error()}}
				} else {
					res.Error = err.Error()
				}
				out.Failed++
				out.Results = append(out.Results, res)
				continue
			}
			s.audit(r, actor, "source.import", "source:"+created.Key, "")
			sv := viewSource(created)
			res.OK = true
			res.Action = "created"
			res.Source = &sv
			out.Succeeded++
			out.Results = append(out.Results, res)

		case conflictOverwrite:
			var existing store.Source
			err := s.store().DB.WithContext(r.Context()).Where("`key` = ?", d.Key).First(&existing).Error
			if err != nil {
				created, cerr := source.Create(r.Context(), s.store().DB, d)
				if cerr != nil {
					res.Error = cerr.Error()
					out.Failed++
					out.Results = append(out.Results, res)
					continue
				}
				s.audit(r, actor, "source.import", "source:"+created.Key, "")
				sv := viewSource(created)
				res.OK = true
				res.Action = "created"
				res.Source = &sv
				out.Succeeded++
				out.Results = append(out.Results, res)
				continue
			}
			updated, uerr := source.Update(r.Context(), s.store().DB, existing.ID, d)
			if uerr != nil {
				res.Error = uerr.Error()
				out.Failed++
				out.Results = append(out.Results, res)
				continue
			}
			s.audit(r, actor, "source.import", "source:"+updated.Key, "覆盖更新")
			sv := viewSource(updated)
			res.OK = true
			res.Action = "updated"
			res.Source = &sv
			out.Succeeded++
			out.Results = append(out.Results, res)

		default:
			res.Error = fmt.Sprintf("不支持的冲突处理方式 %q", item.Conflict)
			out.Failed++
			out.Results = append(out.Results, res)
		}
	}

	if out.Succeeded > 0 {
		s.checkMitm(r.Context())
	}
	writeJSON(w, http.StatusOK, out)
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
	data, err := source.MarshalEnvelope(source.FromModel(x), source.Meta{Version: 1})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="ferry-%s.json"`, x.Key))
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
