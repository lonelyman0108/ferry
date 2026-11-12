package transport

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

type sealedCases struct {
	Keys []struct {
		Key  string `json:"key"`
		Up   string `json:"up"`
		Down string `json:"down"`
	} `json:"keys"`
	Sealed []struct {
		Name      string `json:"name"`
		Key       string `json:"key"`
		Dir       string `json:"dir"`
		Nonce     string `json:"nonce"`
		Plaintext string `json:"plaintext"`
		Sealed    string `json:"sealed"`
	} `json:"sealed"`
	Invalid []struct {
		Name   string `json:"name"`
		Key    string `json:"key"`
		Dir    string `json:"dir"`
		Sealed string `json:"sealed"`
	} `json:"invalid"`
	InvalidKeys []struct {
		Name string `json:"name"`
		Key  string `json:"key"`
	} `json:"invalid_keys"`
}

func loadCases(t *testing.T) sealedCases {
	t.Helper()
	raw, err := os.ReadFile("testdata/sealed_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var c sealedCases
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func dir(t *testing.T, s string) Direction {
	t.Helper()
	switch s {
	case "up":
		return Up
	case "down":
		return Down
	}
	t.Fatalf("未知方向 %q", s)
	return 0
}

func mustKeys(t *testing.T, s string) *Keys {
	t.Helper()
	k, err := ParseKey(s)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestDerivedKeys(t *testing.T) {
	for _, c := range loadCases(t).Keys {
		k := mustKeys(t, c.Key)
		if got := hex.EncodeToString(k.sub[Up][:]); got != c.Up {
			t.Errorf("%s 上行子密钥 = %s，期望 %s", c.Key, got, c.Up)
		}
		if got := hex.EncodeToString(k.sub[Down][:]); got != c.Down {
			t.Errorf("%s 下行子密钥 = %s，期望 %s", c.Key, got, c.Down)
		}
	}
}

func TestSealedCases(t *testing.T) {
	for _, c := range loadCases(t).Sealed {
		t.Run(c.Name, func(t *testing.T) {
			k := mustKeys(t, c.Key)
			var nonce [NonceSize]byte
			if n, err := hex.Decode(nonce[:], []byte(c.Nonce)); err != nil || n != NonceSize {
				t.Fatalf("样例 nonce 不合法：%v", err)
			}
			if got := k.Seal(dir(t, c.Dir), nonce, []byte(c.Plaintext)); got != c.Sealed {
				t.Fatalf("Seal = %s\n期望 %s", got, c.Sealed)
			}
			plain, gotNonce, err := k.Open(dir(t, c.Dir), c.Sealed)
			if err != nil || string(plain) != c.Plaintext || gotNonce != nonce {
				t.Fatalf("Open = %q, %x, %v", plain, gotNonce, err)
			}
		})
	}
}

func TestInvalidCases(t *testing.T) {
	for _, c := range loadCases(t).Invalid {
		t.Run(c.Name, func(t *testing.T) {
			if plain, _, err := mustKeys(t, c.Key).Open(dir(t, c.Dir), c.Sealed); err != ErrOpen {
				t.Fatalf("应当解不开，得到 %q, %v", plain, err)
			}
		})
	}
	for _, c := range loadCases(t).InvalidKeys {
		if _, err := ParseKey(c.Key); err == nil {
			t.Errorf("%s：应当拒绝密钥 %q", c.Name, c.Key)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	text, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	if len(text) != 43 {
		t.Fatalf("密钥文本长度 = %d，期望 43", len(text))
	}
	k := mustKeys(t, text)
	n1, _ := NewNonce()
	n2, _ := NewNonce()
	if n1 == n2 {
		t.Fatal("两次生成的 nonce 相同")
	}
	msg := []byte(`{"v":1,"op":"sync"}`)
	sealed := k.Seal(Up, n1, msg)
	plain, nonce, err := k.Open(Up, sealed)
	if err != nil || !bytes.Equal(plain, msg) || nonce != n1 {
		t.Fatalf("Open = %q, %x, %v", plain, nonce, err)
	}
	if _, _, err := k.Open(Down, sealed); err != ErrOpen {
		t.Fatal("请求报文不应能按回复方向解开")
	}
	if _, _, err := mustKeys(t, mustGenerate(t)).Open(Up, sealed); err != ErrOpen {
		t.Fatal("别的密钥不应能解开")
	}
}

func mustGenerate(t *testing.T) string {
	t.Helper()
	s, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	return s
}
