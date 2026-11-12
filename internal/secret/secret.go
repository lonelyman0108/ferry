// Package secret 用主密钥加解密敏感字段（AES-256-GCM）。
//
// 密文格式为 "gcm:" + base64url(nonce || ciphertext)，前缀标明算法，换方案时换前缀。调用方传入的 aad 会参与认证，
// 通常填“表名/行 ID/字段名”，这样把一行的密文挪到另一行时会解密失败。
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

const prefix = "gcm:"

// KeySize 是主密钥的字节数。
const KeySize = 32

// ErrDecrypt 表示密文被篡改、密钥不对或 aad 不匹配；不区分具体原因，避免泄露信息。
var ErrDecrypt = errors.New("解密失败")

// GenerateKey 生成一个新的随机主密钥。
func GenerateKey() ([]byte, error) {
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return key, nil
}

// Box 持有主密钥，可并发使用。
type Box struct {
	aead   cipher.AEAD
	macKey []byte
}

// New 用 32 字节主密钥创建 Box。
func New(key []byte) (*Box, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("主密钥应为 %d 字节，实际 %d 字节", KeySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	// 去重用的 HMAC 密钥从主密钥派生，和加密用的密钥分开。
	m := hmac.New(sha256.New, key)
	m.Write([]byte("ferry value hash"))
	return &Box{aead: aead, macKey: m.Sum(nil)}, nil
}

// MAC 返回 data 的 HMAC-SHA256（十六进制）。用来比较两个值是否相同而不保存原文；
// 用带密钥的 HMAC 而不是普通哈希，是因为设备 ID 这类值熵不高，普通哈希可以被穷举。
func (b *Box) MAC(data []byte) string {
	m := hmac.New(sha256.New, b.macKey)
	m.Write(data)
	return hex.EncodeToString(m.Sum(nil))
}

// Seal 加密 plaintext；每次调用使用新的随机 nonce。
func (b *Box) Seal(plaintext, aad []byte) (string, error) {
	nonce := make([]byte, b.aead.NonceSize(), b.aead.NonceSize()+len(plaintext)+b.aead.Overhead())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := b.aead.Seal(nonce, nonce, plaintext, aad)
	return prefix + base64.RawURLEncoding.EncodeToString(out), nil
}

// Open 解密 Seal 的输出，aad 必须和加密时一致。
func (b *Box) Open(sealed string, aad []byte) ([]byte, error) {
	rest, ok := strings.CutPrefix(sealed, prefix)
	if !ok {
		return nil, ErrDecrypt
	}
	raw, err := base64.RawURLEncoding.DecodeString(rest)
	if err != nil || len(raw) < b.aead.NonceSize()+b.aead.Overhead() {
		return nil, ErrDecrypt
	}
	n := b.aead.NonceSize()
	plain, err := b.aead.Open(nil, raw[:n], raw[n:], aad)
	if err != nil {
		return nil, ErrDecrypt
	}
	return plain, nil
}
