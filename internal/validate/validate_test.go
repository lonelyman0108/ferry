package validate

import (
	"encoding/base64"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lonelyman0108/ferry/internal/source"
)

func jwtWithExp(exp int64) string {
	enc := base64.RawURLEncoding.EncodeToString
	payload := `{"sub":"u1"}`
	if exp != 0 {
		payload = `{"sub":"u1","exp":` + strconv.FormatInt(exp, 10) + `}`
	}
	return enc([]byte(`{"alg":"HS256","typ":"JWT"}`)) + "." + enc([]byte(payload)) + ".c2ln"
}

func TestCheck(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	future, past := now.Add(24*time.Hour).Unix(), now.Add(-time.Hour).Unix()
	cases := []struct {
		name  string
		f     source.FieldDoc
		value string
		ok    bool
	}{
		{"空值", source.FieldDoc{Type: "text"}, "", false},
		{"太短", source.FieldDoc{Type: "text", MinLen: 4}, "abc", false},
		{"按字符计长度", source.FieldDoc{Type: "text", MinLen: 3, MaxLen: 3}, "设备号", true},
		{"太长", source.FieldDoc{Type: "text", MaxLen: 2}, "abc", false},
		{"超过上限", source.FieldDoc{Type: "text"}, strings.Repeat("x", MaxValueLen+1), false},
		{"正则通过", source.FieldDoc{Type: "text", Pattern: `^[A-Z0-9-]+$`}, "DEV-001", true},
		{"正则不通过", source.FieldDoc{Type: "text", Pattern: `^[A-Z0-9-]+$`}, "dev 001", false},
		{"数字", source.FieldDoc{Type: "number"}, "12.5", true},
		{"不是数字", source.FieldDoc{Type: "number"}, "12a", false},
		{"布尔", source.FieldDoc{Type: "bool"}, "true", true},
		{"JSON", source.FieldDoc{Type: "json"}, `{"a":1}`, true},
		{"坏 JSON", source.FieldDoc{Type: "json"}, `{a:1}`, false},
		{"Cookie", source.FieldDoc{Type: "cookie"}, "pt_key=1", true},
		{"不像 Cookie", source.FieldDoc{Type: "cookie"}, "abc", false},
		{"未过期的 JWT", source.FieldDoc{Type: "jwt"}, jwtWithExp(future), true},
		{"带 Bearer 前缀", source.FieldDoc{Type: "jwt"}, "Bearer " + jwtWithExp(future), true},
		{"没有 exp 的 JWT", source.FieldDoc{Type: "jwt"}, jwtWithExp(0), true},
		{"过期的 JWT", source.FieldDoc{Type: "jwt"}, jwtWithExp(past), false},
		{"不是 JWT", source.FieldDoc{Type: "jwt"}, "not-a-jwt", false},
	}
	for _, c := range cases {
		r := Check(c.f, c.value, now)
		if (r.Err == nil) != c.ok {
			t.Errorf("%s：err = %v", c.name, r.Err)
		}
	}
	r := Check(source.FieldDoc{Type: "jwt"}, jwtWithExp(future), now)
	if r.ExpiresAt == nil || r.ExpiresAt.Unix() != future {
		t.Errorf("应取出 exp：%v", r.ExpiresAt)
	}
}
