//go:build embed_ui

// Package web 提供管理界面的静态文件。带 embed_ui 构建标签时把 dist 打进二进制。
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS 返回前端构建产物；第二个返回值表示这个二进制是否带界面。
func FS() (fs.FS, bool) {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return nil, false
	}
	return sub, true
}
