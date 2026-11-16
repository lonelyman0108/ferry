package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"github.com/lonelyman0108/ferry/internal/ghnet"
	"github.com/lonelyman0108/ferry/internal/source"
	"github.com/lonelyman0108/ferry/internal/sourcecheck"
	"github.com/lonelyman0108/ferry/internal/store"
)

// 官方来源仓库默认配置。
const (
	OfficialRepoName     = "官方仓库"
	OfficialRepoURL      = "https://github.com/lonelyman0108/ferry-hub"
	OfficialRepoIndexURL = "https://raw.githubusercontent.com/lonelyman0108/ferry-hub/main/index.json"
	officialSeededKey    = "repos.official_seeded"
)

type repoCachedData struct {
	Items []repoCachedItem `json:"items"`
}

type repoCachedItem struct {
	sourcecheck.HubIndexItem
	IconData   string              `json:"icon_data,omitempty"`
	Source     *source.Doc         `json:"source,omitempty"`
	Violations []sourcecheck.Issue `json:"violations,omitempty"`
	Warnings   []sourcecheck.Issue `json:"warnings,omitempty"`
	Hosts      []string            `json:"hosts,omitempty"`
	ProbeHost  string              `json:"probe_host,omitempty"`
	ReadmeURL  string              `json:"readme_url,omitempty"`
	RawURL     string              `json:"raw_url,omitempty"`
	FetchError string              `json:"fetch_error,omitempty"`
}

type repoItemView struct {
	Key           string              `json:"key"`
	Name          string              `json:"name"`
	Description   string              `json:"description,omitempty"`
	Icon          string              `json:"icon,omitempty"`
	Version       int                 `json:"version"`
	Author        string              `json:"author,omitempty"`
	Homepage      string              `json:"homepage,omitempty"`
	Tags          []string            `json:"tags,omitempty"`
	Path          string              `json:"path"`
	SHA256        string              `json:"sha256"`
	ReadmeURL     string              `json:"readme_url,omitempty"`
	RawURL        string              `json:"raw_url,omitempty"`
	Source        *source.Doc         `json:"source,omitempty"`
	Violations    []sourcecheck.Issue `json:"violations,omitempty"`
	Warnings      []sourcecheck.Issue `json:"warnings,omitempty"`
	Hosts         []string            `json:"hosts,omitempty"`
	ProbeHost     string              `json:"probe_host,omitempty"`
	FetchError    string              `json:"fetch_error,omitempty"`
	Status        string              `json:"status"`
	ExistingID    uint                `json:"existing_id,omitempty"`
	LocalVersion  int                 `json:"local_version,omitempty"`
	LocalModified bool                `json:"local_modified"`
	LocalSource   *source.Doc         `json:"local_source,omitempty"`
}

type repoView struct {
	ID            uint           `json:"id"`
	Name          string         `json:"name"`
	URL           string         `json:"url"`
	IndexURL      string         `json:"index_url"`
	Enabled       bool           `json:"enabled"`
	Official      bool           `json:"official"`
	GitHubURL     bool           `json:"github_url"`
	LastFetchedAt *time.Time     `json:"last_fetched_at,omitempty"`
	LastError     string         `json:"last_error,omitempty"`
	Items         []repoItemView `json:"items"`
}

type repoInput struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Enabled *bool  `json:"enabled,omitempty"`
}

// ResolveRepoIndexURL 校验仓库地址并换算为 index.json 的拉取地址。
// 支持 GitHub 仓库地址（https://github.com/<owner>/<repo>[/tree/<ref>]）或任意以 .json 结尾的 HTTP(S) 链接。
func ResolveRepoIndexURL(rawURL string) (cleanURL string, indexURL string, err error) {
	rawURL = strings.TrimRight(strings.TrimSpace(rawURL), "/")
	if rawURL == "" {
		return "", "", errors.New("请填写仓库地址")
	}
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", "", errors.New("仓库地址仅支持不含查询参数的 http:// 或 https:// 链接")
	}

	if strings.EqualFold(u.Hostname(), "github.com") {
		segs := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(segs) < 2 || segs[0] == "" || segs[1] == "" {
			return "", "", errors.New("GitHub 仓库地址须形如 https://github.com/<owner>/<repo>")
		}
		owner, repo := segs[0], strings.TrimSuffix(segs[1], ".git")
		switch {
		case len(segs) == 2:
			cleanURL = fmt.Sprintf("https://github.com/%s/%s", owner, repo)
			indexURL = fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/main/index.json", owner, repo)
			return cleanURL, indexURL, nil
		case len(segs) >= 4 && segs[2] == "tree":
			ref := segs[3]
			sub := strings.Join(segs[4:], "/")
			cleanURL = fmt.Sprintf("https://github.com/%s/%s/tree/%s", owner, repo, ref)
			if sub != "" {
				cleanURL += "/" + sub
				indexURL = fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s/index.json", owner, repo, ref, sub)
			} else {
				indexURL = fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/index.json", owner, repo, ref)
			}
			return cleanURL, indexURL, nil
		case len(segs) >= 5 && segs[2] == "blob" && strings.HasSuffix(segs[len(segs)-1], ".json"):
			ref := segs[3]
			sub := strings.Join(segs[4:], "/")
			cleanURL = u.String()
			indexURL = fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", owner, repo, ref, sub)
			return cleanURL, indexURL, nil
		default:
			return "", "", errors.New("GitHub 仓库地址须形如 https://github.com/<owner>/<repo> 或指向 index.json")
		}
	}

	if !strings.HasSuffix(strings.ToLower(u.Path), ".json") {
		return "", "", errors.New("非 GitHub 仓库请填写指向 index.json 的完整地址")
	}
	return u.String(), u.String(), nil
}

// buildSourceReadmeURL 根据仓库地址与来源相对路径推算说明文档 README.md 的网页地址。
func buildSourceReadmeURL(repoURL, indexURL, itemPath string) string {
	dir := path.Dir(strings.TrimPrefix(itemPath, "/"))
	if dir == "." || dir == "" {
		return ""
	}
	u, err := url.Parse(repoURL)
	if err == nil && strings.EqualFold(u.Hostname(), "github.com") {
		segs := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(segs) >= 2 {
			owner, repo := segs[0], strings.TrimSuffix(segs[1], ".git")
			ref := "main"
			if len(segs) >= 4 && (segs[2] == "tree" || segs[2] == "blob") {
				ref = segs[3]
			}
			return fmt.Sprintf("https://github.com/%s/%s/blob/%s/%s/README.md", owner, repo, ref, dir)
		}
	}
	if resolved, err := resolveRelativeURL(indexURL, path.Join(dir, "README.md")); err == nil {
		return resolved
	}
	return ""
}

func resolveRelativeURL(baseURL, relPath string) (string, error) {
	relPath = strings.TrimSpace(relPath)
	if relPath == "" {
		return "", errors.New("相对路径为空")
	}
	if strings.Contains(relPath, "://") || strings.HasPrefix(relPath, "//") {
		return "", errors.New("仓库内路径不允许使用外部地址")
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	baseDir := path.Dir(base.Path)
	cleaned := path.Clean("/" + strings.TrimPrefix(relPath, "/"))
	out := *base
	out.Path = path.Join(baseDir, strings.TrimPrefix(cleaned, "/"))
	out.RawQuery = ""
	out.Fragment = ""
	return out.String(), nil
}

func (s *Server) ensureOfficialRepo(ctx context.Context) error {
	db := s.store().DB.WithContext(ctx)
	var count int64
	if err := db.Model(&store.Repo{}).Where("official = ?", true).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	var seeded store.Setting
	if err := db.Where("key = ?", officialSeededKey).First(&seeded).Error; err == nil {
		return nil
	}
	repoURL := OfficialRepoURL
	indexURL := OfficialRepoIndexURL
	if s.officialRepoIndexURL != "" {
		indexURL = s.officialRepoIndexURL
	}
	repo := store.Repo{
		Name:     OfficialRepoName,
		URL:      repoURL,
		IndexURL: indexURL,
		Enabled:  true,
		Official: true,
	}
	if err := db.Create(&repo).Error; err != nil && !errors.Is(err, gorm.ErrDuplicatedKey) {
		return err
	}
	_ = db.Save(&store.Setting{Key: officialSeededKey, Value: "1", UpdatedAt: time.Now()}).Error
	return nil
}

func (s *Server) refreshRepo(ctx context.Context, repo *store.Repo, force bool) error {
	client, err := s.githubClient(ctx, 12*time.Second)
	if err != nil {
		return err
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

	reqCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, repo.IndexURL, nil)
	if err != nil {
		return err
	}
	if !force && repo.ETag != "" && repo.CachedIndex != "" {
		req.Header.Set("If-None-Match", repo.ETag)
	}

	now := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		repo.LastFetchedAt = &now
		repo.LastError = fmt.Sprintf("拉取仓库索引失败：%v", err)
		_ = s.store().DB.WithContext(ctx).Save(repo).Error
		return errors.New(repo.LastError)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified && repo.CachedIndex != "" {
		repo.LastFetchedAt = &now
		repo.LastError = ""
		return s.store().DB.WithContext(ctx).Save(repo).Error
	}
	if resp.StatusCode != http.StatusOK {
		repo.LastFetchedAt = &now
		repo.LastError = fmt.Sprintf("拉取仓库索引失败：HTTP %d", resp.StatusCode)
		_ = s.store().DB.WithContext(ctx).Save(repo).Error
		return errors.New(repo.LastError)
	}

	rawIdx, err := io.ReadAll(io.LimitReader(resp.Body, source.MaxImportBytes+1))
	if err != nil {
		repo.LastFetchedAt = &now
		repo.LastError = fmt.Sprintf("读取仓库索引失败：%v", err)
		_ = s.store().DB.WithContext(ctx).Save(repo).Error
		return errors.New(repo.LastError)
	}
	if len(rawIdx) > source.MaxImportBytes {
		repo.LastFetchedAt = &now
		repo.LastError = fmt.Sprintf("仓库索引超过 %d KB", source.MaxImportBytes>>10)
		_ = s.store().DB.WithContext(ctx).Save(repo).Error
		return errors.New(repo.LastError)
	}

	var idx sourcecheck.HubIndex
	if err := json.Unmarshal(rawIdx, &idx); err != nil || idx.Format != source.EnvelopeFormat {
		repo.LastFetchedAt = &now
		repo.LastError = "仓库 index.json 格式无效，须为 format: 1"
		_ = s.store().DB.WithContext(ctx).Save(repo).Error
		return errors.New(repo.LastError)
	}

	prevByKeySHA := map[string]repoCachedItem{}
	if repo.CachedIndex != "" {
		var prev repoCachedData
		if json.Unmarshal([]byte(repo.CachedIndex), &prev) == nil {
			for _, it := range prev.Items {
				if it.Key != "" && it.SHA256 != "" && it.Source != nil && it.FetchError == "" {
					prevByKeySHA[it.Key+"@"+strings.ToLower(it.SHA256)] = it
				}
			}
		}
	}

	cached := repoCachedData{
		Items: make([]repoCachedItem, 0, len(idx.Sources)),
	}

	for _, entry := range idx.Sources {
		cacheKey := entry.Key + "@" + strings.ToLower(entry.SHA256)
		if !force {
			if hit, ok := prevByKeySHA[cacheKey]; ok {
				cached.Items = append(cached.Items, hit)
				continue
			}
		}

		ci := repoCachedItem{
			HubIndexItem: entry,
			ReadmeURL:    buildSourceReadmeURL(repo.URL, repo.IndexURL, entry.Path),
		}
		rawSourceURL, err := resolveRelativeURL(repo.IndexURL, entry.Path)
		if err != nil {
			ci.FetchError = fmt.Sprintf("来源路径无效：%v", err)
			cached.Items = append(cached.Items, ci)
			continue
		}
		ci.RawURL = rawSourceURL

		srcBytes, err := fetchLimitedBytes(ctx, client, rawSourceURL, source.MaxImportBytes)
		if err != nil {
			ci.FetchError = fmt.Sprintf("拉取来源定义失败：%v", err)
			cached.Items = append(cached.Items, ci)
			continue
		}
		sum := sha256.Sum256(srcBytes)
		gotSHA := hex.EncodeToString(sum[:])
		if entry.SHA256 != "" && !strings.EqualFold(gotSHA, strings.TrimSpace(entry.SHA256)) {
			ci.FetchError = "source.json 的 SHA-256 摘要与 index.json 不一致"
			cached.Items = append(cached.Items, ci)
			continue
		}

		env, err := source.DecodeSingleEnvelope(srcBytes)
		if err != nil {
			ci.FetchError = fmt.Sprintf("解析来源定义失败：%v", err)
			cached.Items = append(cached.Items, ci)
			continue
		}
		if err := env.Source.Validate(); err != nil {
			ci.FetchError = err.Error()
			cached.Items = append(cached.Items, ci)
			continue
		}

		iconData := env.Source.Icon
		if iconData == "" && entry.Icon != "" {
			if strings.HasPrefix(entry.Icon, "data:") {
				iconData = entry.Icon
			} else if iconURL, err := resolveRelativeURL(repo.IndexURL, entry.Icon); err == nil {
				if mime, ok := source.IconMIMEByExt(path.Ext(entry.Icon)); ok {
					if iconBytes, err := fetchLimitedBytes(ctx, client, iconURL, source.MaxIconBytes); err == nil {
						if encoded, err := source.EncodeIconBytes(mime, iconBytes); err == nil {
							iconData = encoded
						}
					}
				}
			}
		}
		if iconData != "" {
			env.Source.Icon = iconData
			ci.IconData = iconData
		}

		rep := sourcecheck.Check(env.Source)
		ci.Source = &env.Source
		ci.Violations = rep.Violations
		ci.Warnings = rep.Warnings
		ci.Hosts = rep.Hosts
		ci.ProbeHost = rep.ProbeHost
		cached.Items = append(cached.Items, ci)
	}

	encodedCache, err := json.Marshal(cached)
	if err != nil {
		return err
	}
	repo.CachedIndex = string(encodedCache)
	repo.ETag = strings.TrimSpace(resp.Header.Get("ETag"))
	repo.LastFetchedAt = &now
	repo.LastError = ""
	return s.store().DB.WithContext(ctx).Save(repo).Error
}

func fetchLimitedBytes(ctx context.Context, client *http.Client, targetURL string, maxBytes int) ([]byte, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxBytes)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxBytes {
		return nil, fmt.Errorf("内容超过 %d KB", maxBytes>>10)
	}
	return data, nil
}

func (s *Server) buildRepoView(repo store.Repo, localByKey map[string]store.Source) repoView {
	u, _ := url.Parse(repo.IndexURL)
	gh := u != nil && ghnet.IsGitHubHost(u.Hostname())
	v := repoView{
		ID:            repo.ID,
		Name:          repo.Name,
		URL:           repo.URL,
		IndexURL:      repo.IndexURL,
		Enabled:       repo.Enabled,
		Official:      repo.Official,
		GitHubURL:     gh,
		LastFetchedAt: repo.LastFetchedAt,
		LastError:     repo.LastError,
		Items:         []repoItemView{},
	}
	if repo.CachedIndex == "" {
		return v
	}
	var cached repoCachedData
	if json.Unmarshal([]byte(repo.CachedIndex), &cached) != nil {
		return v
	}
	for _, ci := range cached.Items {
		iv := repoItemView{
			Key:         ci.Key,
			Name:        ci.Name,
			Description: ci.Description,
			Icon:        ci.IconData,
			Version:     ci.Version,
			Author:      ci.Author,
			Homepage:    ci.Homepage,
			Tags:        ci.Tags,
			Path:        ci.Path,
			SHA256:      ci.SHA256,
			ReadmeURL:   ci.ReadmeURL,
			RawURL:      ci.RawURL,
			Source:      ci.Source,
			Violations:  ci.Violations,
			Warnings:    ci.Warnings,
			Hosts:       ci.Hosts,
			ProbeHost:   ci.ProbeHost,
			FetchError:  ci.FetchError,
			Status:      "not_imported",
		}
		if local, ok := localByKey[ci.Key]; ok {
			localDoc := source.FromModel(local)
			localHash := source.HashDoc(localDoc)
			iv.ExistingID = local.ID
			iv.LocalVersion = local.OriginVersion
			iv.LocalModified = local.OriginHash != "" && localHash != local.OriginHash

			remoteDiffers := ci.Source != nil && source.HashDoc(*ci.Source) != localHash
			hasNewerVersion := ci.Version > local.OriginVersion && (remoteDiffers || local.OriginVersion == 0 && remoteDiffers)
			switch {
			case hasNewerVersion:
				iv.Status = "update_available"
				iv.LocalSource = &localDoc
			case iv.LocalModified:
				iv.Status = "local_modified"
				iv.LocalSource = &localDoc
			default:
				iv.Status = "imported"
			}
		}
		v.Items = append(v.Items, iv)
	}
	return v
}

func (s *Server) handleListRepos(w http.ResponseWriter, r *http.Request) {
	if err := s.ensureOfficialRepo(r.Context()); err != nil {
		s.internalError(w, r, err)
		return
	}
	db := s.store().DB.WithContext(r.Context())
	var repos []store.Repo
	if err := db.Order("official DESC, id ASC").Find(&repos).Error; err != nil {
		s.internalError(w, r, err)
		return
	}

	forceRefresh := r.URL.Query().Get("refresh") == "1"
	for i := range repos {
		if repos[i].Enabled && (forceRefresh || repos[i].LastFetchedAt == nil) {
			_ = s.refreshRepo(r.Context(), &repos[i], forceRefresh)
		}
	}

	localSources, err := source.LoadAll(r.Context(), s.store().DB)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	localByKey := make(map[string]store.Source, len(localSources))
	for _, src := range localSources {
		localByKey[src.Key] = src
	}

	out := make([]repoView, 0, len(repos))
	for _, repo := range repos {
		out = append(out, s.buildRepoView(repo, localByKey))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateRepo(w http.ResponseWriter, r *http.Request) {
	var in repoInput
	if !decodeJSON(w, r, &in) {
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > 64 {
		writeError(w, http.StatusBadRequest, "名称不能为空，且不超过 64 个字符")
		return
	}
	cleanURL, indexURL, err := ResolveRepoIndexURL(in.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}

	repo := store.Repo{
		Name:     name,
		URL:      cleanURL,
		IndexURL: indexURL,
		Enabled:  enabled,
		Official: false,
	}
	if err := s.store().DB.WithContext(r.Context()).Create(&repo).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			writeError(w, http.StatusConflict, "同名仓库已存在")
			return
		}
		s.internalError(w, r, err)
		return
	}

	if repo.Enabled {
		_ = s.refreshRepo(r.Context(), &repo, true)
	}
	s.audit(r, identityFrom(r.Context()).actor(), "repo.create", "repo:"+repo.Name, repo.URL)

	localSources, _ := source.LoadAll(r.Context(), s.store().DB)
	localByKey := map[string]store.Source{}
	for _, src := range localSources {
		localByKey[src.Key] = src
	}
	writeJSON(w, http.StatusCreated, s.buildRepoView(repo, localByKey))
}

func (s *Server) handleUpdateRepo(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r, "id")
	if !ok {
		return
	}
	var in repoInput
	if !decodeJSON(w, r, &in) {
		return
	}
	db := s.store().DB.WithContext(r.Context())
	var repo store.Repo
	if err := db.First(&repo, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(w, http.StatusNotFound, "仓库不存在")
			return
		}
		s.internalError(w, r, err)
		return
	}

	urlChanged := false
	if !repo.Official {
		if name := strings.TrimSpace(in.Name); name != "" {
			if len(name) > 64 {
				writeError(w, http.StatusBadRequest, "名称不超过 64 个字符")
				return
			}
			repo.Name = name
		}
		if strings.TrimSpace(in.URL) != "" {
			cleanURL, indexURL, err := ResolveRepoIndexURL(in.URL)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			if repo.IndexURL != indexURL {
				urlChanged = true
				repo.URL = cleanURL
				repo.IndexURL = indexURL
				repo.ETag = ""
				repo.CachedIndex = ""
			}
		}
	}
	if in.Enabled != nil {
		repo.Enabled = *in.Enabled
	}

	if err := db.Save(&repo).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			writeError(w, http.StatusConflict, "同名仓库已存在")
			return
		}
		s.internalError(w, r, err)
		return
	}
	if repo.Enabled && (urlChanged || repo.LastFetchedAt == nil) {
		_ = s.refreshRepo(r.Context(), &repo, true)
	}
	s.audit(r, identityFrom(r.Context()).actor(), "repo.update", "repo:"+repo.Name, fmt.Sprintf("enabled=%v", repo.Enabled))

	localSources, _ := source.LoadAll(r.Context(), s.store().DB)
	localByKey := map[string]store.Source{}
	for _, src := range localSources {
		localByKey[src.Key] = src
	}
	writeJSON(w, http.StatusOK, s.buildRepoView(repo, localByKey))
}

func (s *Server) handleDeleteRepo(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r, "id")
	if !ok {
		return
	}
	db := s.store().DB.WithContext(r.Context())
	var repo store.Repo
	if err := db.First(&repo, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(w, http.StatusNotFound, "仓库不存在")
			return
		}
		s.internalError(w, r, err)
		return
	}
	if repo.Official {
		writeError(w, http.StatusBadRequest, "官方仓库不可删除，如不需要可停用")
		return
	}
	if err := db.Model(&store.Source{}).Where("origin_repo_id = ?", repo.ID).Update("origin_repo_id", nil).Error; err != nil {
		s.internalError(w, r, err)
		return
	}
	if err := db.Delete(&repo).Error; err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(r, identityFrom(r.Context()).actor(), "repo.delete", "repo:"+repo.Name, repo.URL)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRefreshRepo(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r, "id")
	if !ok {
		return
	}
	db := s.store().DB.WithContext(r.Context())
	var repo store.Repo
	if err := db.First(&repo, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(w, http.StatusNotFound, "仓库不存在")
			return
		}
		s.internalError(w, r, err)
		return
	}
	_ = s.refreshRepo(r.Context(), &repo, true)

	localSources, _ := source.LoadAll(r.Context(), s.store().DB)
	localByKey := map[string]store.Source{}
	for _, src := range localSources {
		localByKey[src.Key] = src
	}
	writeJSON(w, http.StatusOK, s.buildRepoView(repo, localByKey))
}

func parseID(w http.ResponseWriter, r *http.Request, param string) (uint, bool) {
	raw := chi.URLParam(r, param)
	var id uint
	if _, err := fmt.Sscanf(raw, "%d", &id); err != nil || id == 0 {
		writeError(w, http.StatusBadRequest, "无效的 ID")
		return 0, false
	}
	return id, true
}
