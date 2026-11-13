//go:build windows

package main

import (
	"encoding/base64"
	"errors"
)

// localStateProtector：与 Electron safeStorage 在 Windows 上同格式（Local State 主密钥 + DPAPI + v10 AES-GCM），
// 所以新旧版本可以互相解开对方创建的仓库密钥。Local State 不存在时在首次 Wrap 时按 Chromium 格式新建。
type localStateProtector struct{ userData string }

func newKeyProtector(userData string) keyProtector { return &localStateProtector{userData: userData} }

func (p *localStateProtector) Backend() string { return "dpapi" }
func (p *localStateProtector) Available() bool { return true } // DPAPI 随用户登录始终可用
func (p *localStateProtector) Secure() bool    { return true }

func (p *localStateProtector) Wrap(key []byte) ([]byte, error) {
	if err := safeStorageEnsureMasterKey(p.userData); err != nil {
		return nil, err
	}
	return safeStorageEncrypt(p.userData, []byte(base64.StdEncoding.EncodeToString(key)))
}

func (p *localStateProtector) Unwrap(wrapped []byte) ([]byte, error) {
	plain, err := safeStorageDecrypt(p.userData, wrapped)
	if err != nil {
		return nil, err
	}
	key, err := base64.StdEncoding.DecodeString(string(plain))
	if err != nil || len(key) != keyBytes {
		return nil, errors.New("无法解锁备份仓库")
	}
	return key, nil
}
