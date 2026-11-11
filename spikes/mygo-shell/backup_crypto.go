package main

// 备份仓库的密码学部分，对应 apps/desktop/src/main/services/backup-repository-crypto.ts。
// 信封：`GZENC1` + 12 字节 nonce + 16 字节 GCM tag + 密文（AES-256-GCM）。
// 口令派生：scrypt(N=32768, r=8, p=1)，口令先做 NFKC 规范化。

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"unicode/utf8"

	"golang.org/x/crypto/scrypt"
	"golang.org/x/text/unicode/norm"
)

var encryptedMagic = []byte("GZENC1")

const (
	nonceBytes = 12
	tagBytes   = 16
	keyBytes   = 32
)

type passwordWrappedKey struct {
	Salt string `json:"salt"`
	Data string `json:"data"`
}

func derivePasswordKey(password string, salt []byte) ([]byte, error) {
	return scrypt.Key([]byte(norm.NFKC.String(password)), salt, 32768, 8, 1, keyBytes)
}

// validateRecoveryPassword：JS 的 trim() 的空白集合见 jsTrim；Array.from 按码点计数。
func validateRecoveryPassword(password string) error {
	if utf8.RuneCountInString(jsTrim(password)) < 12 {
		return errors.New("恢复口令至少需要 12 个字符")
	}
	return nil
}

func encryptBuffer(plain, key []byte) ([]byte, error) {
	if len(key) != keyBytes {
		return nil, errors.New("仓库密钥长度不合法")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, nonceBytes)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	sealed := gcm.Seal(nil, nonce, plain, nil) // 密文 || tag
	ct, tag := sealed[:len(sealed)-tagBytes], sealed[len(sealed)-tagBytes:]
	out := make([]byte, 0, len(encryptedMagic)+nonceBytes+tagBytes+len(ct))
	out = append(out, encryptedMagic...)
	out = append(out, nonce...)
	out = append(out, tag...)
	out = append(out, ct...)
	return out, nil
}

func decryptBuffer(envelope, key []byte) ([]byte, error) {
	header := len(encryptedMagic) + nonceBytes + tagBytes
	if len(envelope) < header || !bytes.HasPrefix(envelope, encryptedMagic) {
		return nil, errors.New("加密对象格式不合法")
	}
	nonce := envelope[len(encryptedMagic) : len(encryptedMagic)+nonceBytes]
	tag := envelope[len(encryptedMagic)+nonceBytes : header]
	ct := envelope[header:]
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plain, err := gcm.Open(nil, nonce, append(append([]byte{}, ct...), tag...), nil)
	if err != nil {
		// 与 Node 的 decipher.final() 报错文案一致，用户看到的详情不因内核而变
		return nil, errors.New("Unsupported state or unable to authenticate data")
	}
	return plain, nil
}

func wrapKeyWithPassword(repoKey []byte, password string) (passwordWrappedKey, error) {
	if err := validateRecoveryPassword(password); err != nil {
		return passwordWrappedKey{}, err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return passwordWrappedKey{}, err
	}
	pk, err := derivePasswordKey(password, salt)
	if err != nil {
		return passwordWrappedKey{}, err
	}
	data, err := encryptBuffer(repoKey, pk)
	if err != nil {
		return passwordWrappedKey{}, err
	}
	return passwordWrappedKey{Salt: base64.StdEncoding.EncodeToString(salt), Data: base64.StdEncoding.EncodeToString(data)}, nil
}

func unwrapKeyWithPassword(w passwordWrappedKey, password string) ([]byte, error) {
	if err := validateRecoveryPassword(password); err != nil {
		return nil, err
	}
	bad := errors.New("恢复口令不正确或仓库头已损坏")
	salt, err := base64.StdEncoding.DecodeString(w.Salt)
	if err != nil {
		return nil, bad
	}
	data, err := base64.StdEncoding.DecodeString(w.Data)
	if err != nil {
		return nil, bad
	}
	pk, err := derivePasswordKey(password, salt)
	if err != nil {
		return nil, bad
	}
	key, err := decryptBuffer(data, pk)
	if err != nil {
		return nil, bad
	}
	return key, nil
}

func createRepositoryKey() ([]byte, error) {
	k := make([]byte, keyBytes)
	_, err := rand.Read(k)
	return k, err
}
