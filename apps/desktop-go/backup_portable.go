package main

// 便携包导出，对应 backup-portable.ts：zip 里只做 STORE（对象本身已压缩并加密）。
// 内容：repository-header.json（去掉 safeStorage 包装，它绑定当前系统账户）、snapshot/header.json、
// snapshot/manifest.enc、objects/<sha256>（按清单去重）。

import (
	"archive/zip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
)

func (r *backupRepository) exportPortable(snapshotID, dest, password string) (err error) {
	manifest, err := r.readManifest(snapshotID, password)
	if err != nil {
		return err
	}
	dir, err := r.resolveSnapshotDir(snapshotID)
	if err != nil {
		return err
	}
	header, err := r.readHeader()
	if err != nil {
		return err
	}
	header.SafeStorageWrappedKey, header.KeyStorageBackend = nil, nil
	headerJSON, _ := json.MarshalIndent(header, "", "  ")

	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644) // 与 flags:"wx" 一致：目标已存在即失败
	if err != nil {
		return err
	}
	defer func() {
		out.Close()
		if err != nil {
			os.Remove(dest)
		}
	}()
	zw := zip.NewWriter(out)
	put := func(name string, data []byte) error {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	}
	putFile := func(name, path string) error {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		if err != nil {
			return err
		}
		_, err = io.Copy(w, f)
		return err
	}
	if err := put("repository-header.json", headerJSON); err != nil {
		return err
	}
	if err := putFile("snapshot/header.json", filepath.Join(dir, "header.json")); err != nil {
		return err
	}
	if err := putFile("snapshot/manifest.enc", filepath.Join(dir, "manifest.enc")); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, e := range manifest.Entries {
		if seen[e.Sha256] {
			continue
		}
		seen[e.Sha256] = true
		if err := putFile("objects/"+e.Sha256, filepath.Join(r.objectsDir(), e.Sha256)); err != nil {
			return err
		}
	}
	return zw.Close()
}
