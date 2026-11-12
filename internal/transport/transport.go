// Package transport 给手机脚本和平台之间的数据通道做应用层加密，访问地址是 http 时也读不到、改不了。
//
// 每个订阅一把 32 字节预共享密钥，算法是 NaCl secretbox（XSalsa20-Poly1305）。请求和回复用不同的子密钥，
// 子密钥 = SHA-512(方向标签 ‖ 密钥) 的前 32 字节，这样回复不能被当成请求反射回来。
// 报文格式为 "f1." + base64url(nonce ‖ secretbox 输出)，不带填充；前缀标明格式，换算法时换前缀。
// 手机端 script/ferry.js 实现同一格式，两边共用 testdata/sealed_cases.json 测试。
package transport

import (
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/nacl/secretbox"
)

// KeySize 是预共享密钥的字节数，NonceSize 是每条报文的 nonce 字节数。
const (
	KeySize   = 32
	NonceSize = 24
)

const prefix = "f1."

// Direction 区分请求（手机到平台）和回复（平台到手机），两个方向用不同的子密钥。
type Direction int

// 两个方向。
const (
	Up Direction = iota
	Down
)

var labels = [...]string{Up: "ferry/v1/up", Down: "ferry/v1/down"}

// ErrOpen 表示报文格式不对、被篡改、密钥或方向不对；不区分具体原因，避免泄露信息。
var ErrOpen = errors.New("无法解开报文")

// 严格模式拒绝末尾多余位不为零的写法，让同一段字节只有一种编码，和 ferry.js 的解码一致。
var b64 = base64.RawURLEncoding.Strict()

// Keys 持有一把预共享密钥派生出的两个子密钥，可并发使用。
type Keys struct {
	sub [2][KeySize]byte
}

// GenerateKey 生成一把新的预共享密钥，返回给用户填进模块参数的文本形式（43 个字符）。
func GenerateKey() (string, error) {
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		return "", err
	}
	return b64.EncodeToString(key), nil
}

// ParseKey 解析文本形式的预共享密钥并派生子密钥。
func ParseKey(s string) (*Keys, error) {
	key, err := b64.DecodeString(s)
	if err != nil || len(key) != KeySize {
		return nil, fmt.Errorf("传输密钥应为 %d 字节的 base64url 文本", KeySize)
	}
	var k Keys
	for d, label := range labels {
		sum := sha512.Sum512(append([]byte(label), key...))
		copy(k.sub[d][:], sum[:KeySize])
	}
	return &k, nil
}

// NewNonce 生成随机 nonce。
func NewNonce() ([NonceSize]byte, error) {
	var n [NonceSize]byte
	_, err := rand.Read(n[:])
	return n, err
}

// Seal 用 d 方向的子密钥加密 plaintext。nonce 对同一把密钥不能重复使用。
func (k *Keys) Seal(d Direction, nonce [NonceSize]byte, plaintext []byte) string {
	out := make([]byte, NonceSize, NonceSize+secretbox.Overhead+len(plaintext))
	copy(out, nonce[:])
	out = secretbox.Seal(out, plaintext, &nonce, &k.sub[d])
	return prefix + b64.EncodeToString(out)
}

// Open 解开 d 方向的报文，返回明文和报文里的 nonce（用来去重和把回复绑定到请求）。
func (k *Keys) Open(d Direction, msg string) ([]byte, [NonceSize]byte, error) {
	var nonce [NonceSize]byte
	body, ok := strings.CutPrefix(msg, prefix)
	if !ok {
		return nil, nonce, ErrOpen
	}
	raw, err := b64.DecodeString(body)
	if err != nil || len(raw) < NonceSize+secretbox.Overhead {
		return nil, nonce, ErrOpen
	}
	copy(nonce[:], raw)
	plain, ok := secretbox.Open(nil, raw[NonceSize:], &nonce, &k.sub[d])
	if !ok {
		return nil, nonce, ErrOpen
	}
	return plain, nonce, nil
}
