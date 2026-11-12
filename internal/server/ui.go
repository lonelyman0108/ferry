package server

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/lonelyman0108/ferry/web"
)

const noUIPage = `<!doctype html><meta charset="utf-8"><title>Ferry</title>
<body style="font-family:system-ui;max-width:40em;margin:4em auto;line-height:1.6">
<h1>Ferry</h1><p>这个二进制没有内嵌管理界面。正式构建请加 <code>-tags embed_ui</code>；
开发时在 <code>web/</code> 下运行 <code>npm run dev</code>。</p><p>接口在 <code>/api/v1</code>。</p></body>`

// uiHandler 提供管理界面：文件存在就返回文件，否则返回 index.html，交给前端路由。
func uiHandler() http.Handler {
	files, ok := web.FS()
	if !ok {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(noUIPage))
		})
	}
	fileServer := http.FileServerFS(files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name != "" {
			if st, err := fs.Stat(files, name); err == nil && !st.IsDir() {
				// 带哈希的构建产物可以长期缓存，index.html 每次都要重新取。
				if strings.HasPrefix(name, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-cache")
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, r2)
	})
}
