//go:build !embed_ui

// Package web 提供管理界面的静态文件。默认构建不带界面：go vet / go test 时 dist 可能还没构建，
// 正式构建用 -tags embed_ui，开发时用 npm run dev 由 Vite 把 /api 转发给后端。
package web

import "io/fs"

// FS 返回前端构建产物；第二个返回值表示这个二进制是否带界面。
func FS() (fs.FS, bool) { return nil, false }
