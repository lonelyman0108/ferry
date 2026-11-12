// Package auth 提供密码哈希和随机令牌。令牌原文只交给用户一次，数据库里只存 SHA-256。
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

// MinPasswordLen 是管理员密码的最短长度（按字符计）。
const MinPasswordLen = 8

// ErrWeakPassword 表示密码太短。
var ErrWeakPassword = errors.New("密码至少 8 个字符")

// HashPassword 用 bcrypt 计算密码哈希。
func HashPassword(password string) (string, error) {
	if utf8.RuneCountInString(password) < MinPasswordLen {
		return "", ErrWeakPassword
	}
	// bcrypt 只取前 72 字节，超长密码直接拒绝，避免用户以为后半段也生效。
	if len(password) > 72 {
		return "", errors.New("密码不能超过 72 字节")
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(h), err
}

// CheckPassword 比较密码和哈希。
func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// dummyHash 用于用户不存在时也跑一次 bcrypt，让响应时间不暴露用户名是否存在。
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("ferry-dummy-password"), bcrypt.DefaultCost)

// BurnPasswordCheck 做一次无意义的 bcrypt 比较。
func BurnPasswordCheck(password string) {
	_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
}

// NewToken 生成 32 字节随机令牌，返回带前缀的原文和它的哈希。
func NewToken(prefix string) (raw, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	raw = prefix + base64.RawURLEncoding.EncodeToString(b)
	return raw, HashToken(raw), nil
}

// HashToken 返回令牌原文的 SHA-256（十六进制）。令牌本身熵足够，不需要慢哈希。
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// Equal 以常量时间比较两个字符串。
func Equal(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
