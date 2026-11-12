// Package validate 对回传的字段值做静态校验：长度、正则和按类型的格式检查，JWT 还会取出过期时间。
package validate

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/golang-jwt/jwt/v5"

	"github.com/lonelyman0108/ferry/internal/source"
)

// MaxValueLen 是单个字段值的长度上限（字节）。
const MaxValueLen = 16 << 10

// Result 是一次校验的结果。Err 不为 nil 表示值不合法，Err.Error() 可以直接给人看。
type Result struct {
	// ExpiresAt 是 JWT 的过期时间，其他类型或没有 exp 时为 nil。
	ExpiresAt *time.Time
	Err       error
}

// Check 按字段定义校验 value。now 用来判断 JWT 是否已过期。
func Check(f source.FieldDoc, value string, now time.Time) Result {
	if value == "" {
		return Result{Err: errors.New("值为空")}
	}
	if len(value) > MaxValueLen {
		return Result{Err: fmt.Errorf("值超过 %d 字节", MaxValueLen)}
	}
	n := utf8.RuneCountInString(value)
	if f.MinLen > 0 && n < f.MinLen {
		return Result{Err: fmt.Errorf("长度 %d，少于 %d", n, f.MinLen)}
	}
	if f.MaxLen > 0 && n > f.MaxLen {
		return Result{Err: fmt.Errorf("长度 %d，超过 %d", n, f.MaxLen)}
	}
	if f.Pattern != "" {
		re, err := regexp.Compile(f.Pattern)
		if err != nil {
			return Result{Err: fmt.Errorf("校验正则不合法：%w", err)}
		}
		if !re.MatchString(value) {
			return Result{Err: errors.New("不符合校验正则")}
		}
	}
	switch f.Type {
	case source.TypeNumber:
		if _, err := strconv.ParseFloat(value, 64); err != nil {
			return Result{Err: errors.New("不是数字")}
		}
	case source.TypeBool:
		if _, err := strconv.ParseBool(value); err != nil {
			return Result{Err: errors.New("不是布尔值")}
		}
	case source.TypeJSON:
		if !json.Valid([]byte(value)) {
			return Result{Err: errors.New("不是合法的 JSON")}
		}
	case source.TypeCookie:
		if !strings.Contains(value, "=") {
			return Result{Err: errors.New("不是 Cookie 格式（缺少 name=value）")}
		}
	case source.TypeJWT:
		return checkJWT(value, now)
	}
	return Result{}
}

// checkJWT 只解析、不验签（平台没有签发方的密钥），取出 exp 判断是否已过期。
// 允许带 "Bearer " 前缀，因为 Authorization 头里常这样写。
func checkJWT(value string, now time.Time) Result {
	raw := strings.TrimSpace(value)
	if len(raw) > 7 && strings.EqualFold(raw[:7], "bearer ") {
		raw = strings.TrimSpace(raw[7:])
	}
	claims := jwt.MapClaims{}
	if _, _, err := jwt.NewParser().ParseUnverified(raw, claims); err != nil {
		return Result{Err: errors.New("不是合法的 JWT")}
	}
	exp, err := claims.GetExpirationTime()
	if err != nil {
		return Result{Err: errors.New("JWT 的 exp 不合法")}
	}
	if exp == nil {
		return Result{}
	}
	t := exp.Time
	if !t.After(now) {
		return Result{ExpiresAt: &t, Err: fmt.Errorf("JWT 已于 %s 过期", t.Local().Format("2006-01-02 15:04"))}
	}
	return Result{ExpiresAt: &t}
}
