//go:build windows

package main

// Electron safeStorage（Windows）的 Go 实现，用来解开/生成备份仓库头里的 safeStorageWrappedKey。
//
// 格式（Chromium os_crypt，Electron 45 实测）：
//   <userData>/Local State 的 os_crypt.encrypted_key = base64( "DPAPI" + CryptProtectData(32 字节主密钥) )
//   密文 = "v10" + 12 字节 nonce + AES-256-GCM(明文) + 16 字节 tag，密钥就是上面的主密钥，无附加数据。
// 主密钥受当前 Windows 用户的 DPAPI 保护，所以只有同一用户、能读到该 userData 的进程才能解开——和 Electron 版一致。

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

func dpapiUnprotect(data []byte) ([]byte, error) {
	in := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, nil, 0, nil, 0, &out); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...), nil
}

// safeStorageMasterKey 读取 Local State 里受 DPAPI 保护的主密钥。
func safeStorageMasterKey(userData string) ([]byte, error) {
	raw, err := os.ReadFile(filepath.Join(userData, "Local State"))
	if err != nil {
		return nil, err
	}
	var st struct {
		OSCrypt struct {
			EncryptedKey string `json:"encrypted_key"`
		} `json:"os_crypt"`
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, err
	}
	blob, err := base64.StdEncoding.DecodeString(st.OSCrypt.EncryptedKey)
	if err != nil {
		return nil, err
	}
	if len(blob) < 6 || string(blob[:5]) != "DPAPI" {
		return nil, errors.New("Local State 里的 encrypted_key 不是 DPAPI 格式")
	}
	return dpapiUnprotect(blob[5:])
}

// dpapiProtect 用当前 Windows 用户的 DPAPI 保护数据。
func dpapiProtect(data []byte) ([]byte, error) {
	in := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	var out windows.DataBlob
	if err := windows.CryptProtectData(&in, nil, nil, 0, nil, 0, &out); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...), nil
}

// safeStorageEnsureMasterKey 保证 Local State 里有受 DPAPI 保护的主密钥：已有则不动，没有就按 Chromium 的格式新建（保留文件里其它键）。
// 这样 Go 版新建的仓库头与 Electron 版共用同一份 safeStorage 密钥环，新旧版可互读。
func safeStorageEnsureMasterKey(userData string) error {
	if _, err := safeStorageMasterKey(userData); err == nil {
		return nil
	}
	path := filepath.Join(userData, "Local State")
	state := map[string]any{}
	if raw, err := os.ReadFile(path); err == nil {
		if json.Unmarshal(raw, &state) != nil || state == nil {
			return errors.New("Local State 已存在但无法解析，拒绝覆盖")
		}
	}
	master := make([]byte, 32)
	if _, err := rand.Read(master); err != nil {
		return err
	}
	protected, err := dpapiProtect(master)
	if err != nil {
		return err
	}
	osCrypt, _ := state["os_crypt"].(map[string]any)
	if osCrypt == nil {
		osCrypt = map[string]any{}
	}
	osCrypt["encrypted_key"] = base64.StdEncoding.EncodeToString(append([]byte("DPAPI"), protected...))
	state["os_crypt"] = osCrypt
	out, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(userData, 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func safeStorageDecrypt(userData string, wrapped []byte) ([]byte, error) {
	if len(wrapped) < 3+12+16 || string(wrapped[:3]) != "v10" {
		return nil, errors.New("不是 v10 格式的 safeStorage 密文")
	}
	key, err := safeStorageMasterKey(userData)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, wrapped[3:15], wrapped[15:], nil)
}

func safeStorageEncrypt(userData string, plain []byte) ([]byte, error) {
	key, err := safeStorageMasterKey(userData)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return append(append([]byte("v10"), nonce...), gcm.Seal(nil, nonce, plain, nil)...), nil
}
