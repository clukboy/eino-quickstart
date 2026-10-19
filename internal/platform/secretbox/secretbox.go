// Package secretbox 负责把外部模型供应商的密钥加密后落库。
//
// 只做一件事：AES-256-GCM 的加解密封装。密钥来自部署环境（configs 里只放
// 环境变量名），所以数据库被盗时拿到的是 ciphertext，不是一堆明文 API Key。
//
// 选择 GCM 而不是 CBC：这里只需要认证加密，不需要自己处理 padding 与 IV 校验，
// 用错了会静默降级成不安全的方案。
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// Box 是一个可并发使用的加解密器（cipher.AEAD 本身是并发安全的）。
type Box struct {
	aead cipher.AEAD
}

// New 用一个 base64 编码的 32 字节密钥构造 Box。
//
// 长度必须是 32：AES-256。允许更短的密钥会让部署方以为自己在用 AES-256 而实际
// 上不是，这种「看起来加密了」比直接报错更危险。
func New(keyBase64 string) (*Box, error) {
	if keyBase64 == "" {
		return nil, errors.New("secretbox: key is empty")
	}

	key, err := base64.StdEncoding.DecodeString(keyBase64)
	if err != nil {
		return nil, fmt.Errorf("secretbox: decode key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("secretbox: key must be 32 bytes after base64 decoding, got %d", len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secretbox: create cipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secretbox: create GCM: %w", err)
	}

	return &Box{aead: aead}, nil
}

// Seal 加密并返回 base64(nonce || ciphertext)。
//
// nonce 随密文一起存：它不需要保密，但绝不能复用。每次 Seal 都从 crypto/rand
// 重新取，所以同一个明文两次加密的结果不同 —— 这不是浪费，是 GCM 的硬要求。
func (b *Box) Seal(plaintext string) (string, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("secretbox: generate nonce: %w", err)
	}

	// 把 nonce 当成 dst 的起始内容，Seal 会把密文追加在它后面。
	sealed := b.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Open 解密 Seal 的产物。密文被篡改、密钥不对、格式不对都会返回 error ——
// 调用方不应该把这些情况当成「密钥为空」继续往下走。
func (b *Box) Open(encoded string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("secretbox: decode ciphertext: %w", err)
	}

	size := b.aead.NonceSize()
	if len(raw) < size {
		return "", errors.New("secretbox: ciphertext is shorter than the nonce")
	}

	plaintext, err := b.aead.Open(nil, raw[:size], raw[size:], nil)
	if err != nil {
		return "", fmt.Errorf("secretbox: decrypt: %w", err)
	}

	return string(plaintext), nil
}
