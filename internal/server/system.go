package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/lonelyman0108/ferry/internal/buildinfo"
	"github.com/lonelyman0108/ferry/internal/config"
	"github.com/lonelyman0108/ferry/internal/store"
)

// systemView 是系统信息页的内容。不含任何密钥和原值：连接串里的密码打码，主密钥只说从哪里来。
type systemView struct {
	Build struct {
		Version string `json:"version"`
		Commit  string `json:"commit"`
		Date    string `json:"date"`
		Go      string `json:"go"`
		OS      string `json:"os"`
		Arch    string `json:"arch"`
	} `json:"build"`
	Runtime struct {
		Container bool      `json:"container"`
		StartedAt time.Time `json:"started_at"`
		Timezone  string    `json:"timezone"`
		Listen    string    `json:"listen"`
	} `json:"runtime"`
	Database struct {
		Driver  string `json:"driver"`
		Version string `json:"version"`
		Target  string `json:"target"`
		// Size 是 SQLite 数据库文件（含 WAL）的字节数，其他数据库为 0。
		Size int64 `json:"size"`
	} `json:"database"`
	Config struct {
		Path    string         `json:"path"`
		Exists  bool           `json:"exists"`
		Sources config.Sources `json:"sources"`
	} `json:"config"`
	Counts struct {
		Sources       int64 `json:"sources"`
		Identities    int64 `json:"identities"`
		Versions      int64 `json:"versions"`
		Subscriptions int64 `json:"subscriptions"`
		Destinations  int64 `json:"destinations"`
		Pushes        int64 `json:"pushes"`
	} `json:"counts"`
}

// handleSystem 返回系统信息，只给登录的管理员。
func (s *Server) handleSystem(w http.ResponseWriter, r *http.Request) {
	out, err := s.system(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) system(ctx context.Context) (systemView, error) {
	var v systemView
	v.Build.Version, v.Build.Commit, v.Build.Date = buildinfo.Version, buildinfo.Commit, buildinfo.Date
	v.Build.Go, v.Build.OS, v.Build.Arch = runtime.Version(), runtime.GOOS, runtime.GOARCH

	v.Runtime.Container = inContainer()
	v.Runtime.StartedAt = s.startedAt
	name, offset := time.Now().Zone()
	v.Runtime.Timezone = time.Local.String() + "（" + name + " " + utcOffset(offset) + "）"
	v.Runtime.Listen = s.listen

	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()
	v.Config.Path = s.configPath
	_, err := os.Stat(s.configPath)
	v.Config.Exists = err == nil
	v.Config.Sources = cfg.Sources

	db := s.store().DB.WithContext(ctx)
	v.Database.Driver = cfg.Database.Driver
	v.Database.Target = maskDSN(cfg.Database.DSN)
	query := map[string]string{
		config.DriverSQLite:   "SELECT sqlite_version()",
		config.DriverPostgres: "SHOW server_version",
		config.DriverMySQL:    "SELECT VERSION()",
	}[cfg.Database.Driver]
	if query != "" {
		_ = db.Raw(query).Scan(&v.Database.Version).Error
	}
	if cfg.Database.Driver == config.DriverSQLite {
		path, _, _ := strings.Cut(cfg.Database.DSN, "?")
		for _, p := range []string{path, path + "-wal"} {
			if fi, err := os.Stat(p); err == nil {
				v.Database.Size += fi.Size()
			}
		}
	}

	for _, c := range []struct {
		model any
		n     *int64
	}{
		{&store.Source{}, &v.Counts.Sources},
		{&store.Identity{}, &v.Counts.Identities},
		{&store.Version{}, &v.Counts.Versions},
		{&store.Subscription{}, &v.Counts.Subscriptions},
		{&store.Destination{}, &v.Counts.Destinations},
		{&store.PushJob{}, &v.Counts.Pushes},
	} {
		if err := db.Model(c.model).Count(c.n).Error; err != nil {
			return v, err
		}
	}
	return v, nil
}

// inContainer 判断是否跑在容器里：官方镜像设了 FERRY_CONTAINER，Docker 还会在根目录放 /.dockerenv。
func inContainer() bool {
	if os.Getenv("FERRY_CONTAINER") != "" {
		return true
	}
	_, err := os.Stat("/.dockerenv")
	return err == nil
}

func utcOffset(sec int) string {
	sign := "+"
	if sec < 0 {
		sign, sec = "-", -sec
	}
	return fmt.Sprintf("UTC%s%02d:%02d", sign, sec/3600, sec%3600/60)
}

// updateView 是检查更新的结果。Newer 只在当前是正式版本号、且最新版本更高时为 true。
type updateView struct {
	Current   string    `json:"current"`
	Latest    string    `json:"latest"`
	URL       string    `json:"url"`
	Newer     bool      `json:"newer"`
	CheckedAt time.Time `json:"checked_at"`
	Error     string    `json:"error,omitempty"`
}

// updateTTL 内重复检查直接用上次的结果，免得每次打开页面都请求 GitHub。
const updateTTL = time.Hour

// handleSystemUpdate 查 GitHub 上的最新版本。失败时把原因放在 error 里返回 200，页面照常显示。
func (s *Server) handleSystemUpdate(w http.ResponseWriter, r *http.Request) {
	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	if s.update != nil && s.now().Sub(s.update.CheckedAt) < updateTTL && r.URL.Query().Get("force") == "" {
		writeJSON(w, http.StatusOK, s.update)
		return
	}
	v := &updateView{Current: buildinfo.Version, CheckedAt: s.now()}
	latest, url, err := s.latestRelease(r.Context())
	if err != nil {
		v.Error = err.Error()
	} else {
		v.Latest, v.URL = latest, url
		v.Newer = newerVersion(latest, buildinfo.Version)
	}
	s.update = v
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) latestRelease(ctx context.Context) (tag, url string, err error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.releaseURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "ferry/"+buildinfo.Version)
	client := s.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", errors.New("连不上 GitHub")
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return "", "", errors.New("还没有发布过版本")
	default:
		return "", "", errors.New("GitHub 返回 " + resp.Status)
	}
	var body struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", "", errors.New("GitHub 的回复看不懂")
	}
	return body.TagName, body.HTMLURL, nil
}

// newerVersion 比较 vX.Y.Z：latest 比 current 高时为 true。current 不是正式版本号（如 dev）时不比较。
func newerVersion(latest, current string) bool {
	l, ok1 := parseSemver(latest)
	c, ok2 := parseSemver(current)
	if !ok1 || !ok2 {
		return false
	}
	for i := range l {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

func parseSemver(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(v, "v")
	v, _, _ = strings.Cut(v, "-")
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
