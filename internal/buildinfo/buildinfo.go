// Package buildinfo 保存构建时由 -ldflags 注入的版本信息。
package buildinfo

import (
	"fmt"
	"runtime"
)

// 构建时用 -ldflags "-X github.com/lonelyman0108/ferry/internal/buildinfo.Version=..." 覆盖。
var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

// String 返回一行可读的构建信息。
func String() string {
	return fmt.Sprintf("ferry %s (commit %s, built %s, %s %s/%s)",
		Version, Commit, Date, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}
