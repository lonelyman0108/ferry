// Package script 把手机端通用脚本 ferry.js 打进二进制，由 /script/ferry.js 提供。
package script

import _ "embed"

// FerryJS 是 ferry.js 的内容。脚本里不含任何令牌，令牌由订阅以参数传入。
//
//go:embed ferry.js
var FerryJS []byte
