package secret

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func newBox(t *testing.T) *Box {
	t.Helper()
	key, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	b, err := New(key)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRoundTrip(t *testing.T) {
	b := newBox(t)
	for _, plain := range [][]byte{nil, []byte("ey.jwt.token"), bytes.Repeat([]byte("x"), 4096)} {
		sealed, err := b.Seal(plain, []byte("versions/1/value"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(sealed, "gcm:") {
			t.Errorf("密文缺少算法前缀：%q", sealed)
		}
		got, err := b.Open(sealed, []byte("versions/1/value"))
		if err != nil || !bytes.Equal(got, plain) {
			t.Errorf("Open = %q, %v；期望 %q", got, err, plain)
		}
	}
}

func TestNonceIsRandom(t *testing.T) {
	b := newBox(t)
	s1, _ := b.Seal([]byte("same"), nil)
	s2, _ := b.Seal([]byte("same"), nil)
	if s1 == s2 {
		t.Error("同一明文两次加密结果相同，nonce 没有随机化")
	}
}

func TestOpenRejects(t *testing.T) {
	b := newBox(t)
	sealed, _ := b.Seal([]byte("secret"), []byte("versions/1/value"))
	other := newBox(t)

	tampered := []byte(sealed)
	tampered[len(tampered)-2] ^= 1

	cases := map[string]struct {
		box    *Box
		sealed string
		aad    string
	}{
		"aad 不同":    {b, sealed, "versions/2/value"},
		"密钥不同":      {other, sealed, "versions/1/value"},
		"密文被改":      {b, string(tampered), "versions/1/value"},
		"缺少前缀":      {b, strings.TrimPrefix(sealed, "gcm:"), "versions/1/value"},
		"不是 base64": {b, "gcm:%%%", "versions/1/value"},
		"太短":        {b, "gcm:AAAA", "versions/1/value"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := tc.box.Open(tc.sealed, []byte(tc.aad)); !errors.Is(err, ErrDecrypt) {
				t.Errorf("err = %v，期望 ErrDecrypt", err)
			}
		})
	}
}

func TestNewRejectsBadKey(t *testing.T) {
	if _, err := New(make([]byte, 16)); err == nil {
		t.Error("16 字节密钥应当被拒绝")
	}
}

func TestMAC(t *testing.T) {
	b, other := newBox(t), newBox(t)
	if b.MAC([]byte("a")) != b.MAC([]byte("a")) || b.MAC([]byte("a")) == b.MAC([]byte("b")) {
		t.Error("同一密钥下 MAC 应当稳定且区分不同输入")
	}
	if b.MAC([]byte("a")) == other.MAC([]byte("a")) {
		t.Error("不同主密钥的 MAC 应当不同")
	}
}
