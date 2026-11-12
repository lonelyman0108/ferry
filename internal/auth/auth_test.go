package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestPassword(t *testing.T) {
	h, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(h, "correct horse") {
		t.Error("正确密码校验失败")
	}
	if CheckPassword(h, "correct horsE") {
		t.Error("错误密码校验通过")
	}
}

func TestPasswordRules(t *testing.T) {
	if _, err := HashPassword("短密码1234"[:6]); !errors.Is(err, ErrWeakPassword) {
		t.Errorf("短密码 err = %v", err)
	}
	if _, err := HashPassword("八个汉字的密码啊"); err != nil {
		t.Errorf("8 个汉字应当可以：%v", err)
	}
	if _, err := HashPassword(strings.Repeat("a", 73)); err == nil {
		t.Error("超过 72 字节的密码应当被拒绝")
	}
}

func TestNewToken(t *testing.T) {
	raw, hash, err := NewToken("fy_")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, "fy_") || len(raw) != 3+43 {
		t.Errorf("令牌格式不对：%q", raw)
	}
	if HashToken(raw) != hash || len(hash) != 64 {
		t.Errorf("哈希不一致：%q", hash)
	}
	raw2, _, _ := NewToken("fy_")
	if raw == raw2 {
		t.Error("两次生成的令牌相同")
	}
}
