package main

// 便携备份导入，对应 backup-portable-reader.ts + BackupRepository.importPortable（backup-repository.ts）。
// 把 .guizhi-backup 里的快照与加密对象并入本机仓库，不直接改动数据库；真正替换数据仍走「预检并恢复」。
//
// 规则（与 TS 版一致）：
//   - 必须给出包对应的恢复口令；用它解开包内仓库密钥并校验清单可解密、引用的对象齐全。
//   - 本机没有仓库或没有任何快照：换用包里的仓库头（恢复口令随之变化），清掉旧密钥的孤立对象。
//   - 本机已有快照：只有密钥相同（同一仓库的另一份拷贝）才并入，否则拒绝——对象按明文哈希命名，不同密钥无法共存。
//   - 提交顺序：仓库头 → 对象 → 快照目录整体改名；快照出现之前仓库不会列出半成品。
//   - 导入的快照一律记为手动，避免被自动备份的保留数量清掉唯一副本。

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var portableEntryRe = regexp.MustCompile(`^(repository-header\.json|snapshot/header\.json|snapshot/manifest\.enc|objects/[0-9a-f]{64})$`)

type portableImportResult struct {
	Success            bool          `json:"success"`
	Error              string        `json:"error,omitempty"`
	AlreadyExisted     bool          `json:"alreadyExisted,omitempty"`
	ReplacedRepository bool          `json:"replacedRepository,omitempty"`
	Snapshot           *snapshotInfo `json:"snapshot,omitempty"`
}

// extractPortableArchive 把便携包安全地解到 stage：只收白名单条目，拒绝加密/压缩/重名条目，Go 的 zip 读取在读尽时校验 CRC-32。
func extractPortableArchive(archivePath, stage string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("无法读取便携备份（不是有效的 .guizhi-backup）：%w", err)
	}
	defer zr.Close()
	seen := map[string]bool{}
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, "/") {
			continue
		}
		if !portableEntryRe.MatchString(f.Name) {
			return fmt.Errorf("便携备份含有不允许的条目：%s", f.Name)
		}
		if seen[f.Name] {
			return fmt.Errorf("便携备份含有重复条目：%s", f.Name)
		}
		seen[f.Name] = true
		if f.Flags&0x1 != 0 {
			return fmt.Errorf("便携备份条目已加密，无法导入：%s", f.Name)
		}
		if f.Method != zip.Store {
			return fmt.Errorf("便携备份条目使用了压缩，不是归知导出的格式：%s", f.Name)
		}
		target := filepath.Join(stage, filepath.FromSlash(f.Name))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := copyZipEntry(f, target); err != nil {
			return fmt.Errorf("便携备份条目 %s 校验失败，文件可能已损坏：%w", f.Name, err)
		}
	}
	return nil
}

func copyZipEntry(f *zip.File, target string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, rc) // 读到 EOF 时 archive/zip 会比对 CRC-32，不符返回 zip.ErrChecksum
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if uint64(n) != f.UncompressedSize64 {
		return errors.New("长度与目录不符")
	}
	return nil
}

func (r *backupRepository) importPortable(archivePath, password string) (result portableImportResult) {
	fail := func(err error) portableImportResult { return portableImportResult{Error: err.Error()} }
	if err := os.MkdirAll(r.paths.RepositoryDir, 0o755); err != nil {
		return fail(err)
	}
	stage, err := os.MkdirTemp(r.paths.RepositoryDir, ".import-work-")
	if err != nil {
		return fail(err)
	}
	defer os.RemoveAll(stage)

	if err := extractPortableArchive(archivePath, stage); err != nil {
		return fail(err)
	}
	for _, req := range []string{"repository-header.json", "snapshot/header.json", "snapshot/manifest.enc"} {
		if _, err := os.Stat(filepath.Join(stage, filepath.FromSlash(req))); err != nil {
			return fail(fmt.Errorf("便携备份缺少必要文件：%s", req))
		}
	}
	rawHeader, err := os.ReadFile(filepath.Join(stage, "repository-header.json"))
	if err != nil {
		return fail(err)
	}
	var header repositoryHeader
	if json.Unmarshal(rawHeader, &header) != nil || header.Kind != "guizhi-backup-repository" || header.Version != repositoryVersion ||
		header.PasswordWrappedKey.Salt == "" || header.PasswordWrappedKey.Data == "" {
		return fail(errors.New("备份仓库头不合法或版本不受支持"))
	}
	key, err := unwrapKeyWithPassword(header.PasswordWrappedKey, password)
	if err != nil {
		return fail(err)
	}
	rawSnap, err := os.ReadFile(filepath.Join(stage, "snapshot", "header.json"))
	if err != nil {
		return fail(err)
	}
	var snap snapshotHeader
	if json.Unmarshal(rawSnap, &snap) != nil || snap.Kind != "guizhi-repository-snapshot-header" || !snapshotIDRe.MatchString(snap.ID) {
		return fail(errors.New("便携备份的快照头不合法"))
	}
	manifestEnc, err := os.ReadFile(filepath.Join(stage, "snapshot", "manifest.enc"))
	if err != nil {
		return fail(err)
	}
	if sha256Hex(manifestEnc) != snap.ManifestSha256 {
		return fail(errors.New("便携备份的快照清单校验失败"))
	}
	plain, err := decryptBuffer(manifestEnc, key)
	if err == nil {
		plain, err = gunzipBytes(plain)
	}
	var manifest snapshotManifest
	if err != nil || json.Unmarshal(plain, &manifest) != nil {
		return fail(errors.New("便携备份的快照清单无法解密，文件可能已损坏"))
	}
	if manifest.Kind != "guizhi-repository-snapshot" || manifest.ID != snap.ID {
		return fail(errors.New("便携备份的快照清单与快照头不匹配"))
	}
	needed, seen := []string{}, map[string]bool{}
	for _, e := range manifest.Entries {
		if !seen[e.Sha256] {
			seen[e.Sha256] = true
			needed = append(needed, e.Sha256)
		}
	}
	missing := 0
	for _, h := range needed {
		if _, err := os.Stat(filepath.Join(stage, "objects", h)); err != nil {
			missing++
		}
	}
	if missing > 0 {
		return fail(fmt.Errorf("便携备份缺少 %d 个数据对象，文件可能不完整", missing))
	}

	// ── 以下到提交完成都由调用方持有仓库锁 ──
	hasLocal := false
	if _, err := os.Stat(r.headerPath()); err == nil {
		hasLocal = true
	}
	same := false
	if hasLocal {
		local, err := r.readHeader()
		if err != nil {
			return fail(err)
		}
		if k, kerr := unwrapKeyWithPassword(local.PasswordWrappedKey, password); kerr == nil && bytes.Equal(k, key) {
			same = true
		}
		if !same && len(r.listSnapshots()) > 0 {
			return fail(errors.New("本机备份仓库已有完整快照，且与这份便携备份不属于同一个仓库（恢复口令或密钥不同），无法合并。" +
				"请先在本机删除这些快照，或换一台没有完整快照的机器导入。"))
		}
	}
	snapDir := filepath.Join(r.snapshotsDir(), snap.ID)
	if _, err := os.Stat(snapDir); err == nil {
		if !same {
			return fail(errors.New("本机已存在同名快照目录，无法导入"))
		}
		r.cachedKey = key
		info := r.headerToInfo(snap)
		return portableImportResult{Success: true, AlreadyExisted: true, Snapshot: &info}
	}

	if !same {
		if err := os.RemoveAll(r.objectsDir()); err != nil {
			return fail(err)
		}
		next := header
		next.SafeStorageWrappedKey, next.KeyStorageBackend = nil, nil
		if r.protectorUsable() {
			if sealed, err := r.protector.Wrap(key); err == nil {
				b64, backend := base64.StdEncoding.EncodeToString(sealed), r.protector.Backend()
				next.SafeStorageWrappedKey, next.KeyStorageBackend = &b64, &backend
			}
		}
		if err := os.MkdirAll(r.objectsDir(), 0o755); err != nil {
			return fail(err)
		}
		if err := os.MkdirAll(r.snapshotsDir(), 0o755); err != nil {
			return fail(err)
		}
		raw, _ := json.MarshalIndent(next, "", "  ")
		if err := atomicWrite(r.headerPath(), raw); err != nil {
			return fail(err)
		}
	}
	r.cachedKey = key
	for _, d := range []string{r.objectsDir(), r.snapshotsDir()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fail(err)
		}
	}
	for _, h := range needed {
		dst := filepath.Join(r.objectsDir(), h)
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		if err := os.Rename(filepath.Join(stage, "objects", h), dst); err != nil {
			return fail(err)
		}
	}
	// 改写 backupKind 时保留快照头里的其它字段原样
	var generic map[string]any
	if json.Unmarshal(rawSnap, &generic) != nil {
		return fail(errors.New("便携备份的快照头不合法"))
	}
	generic["backupKind"] = "manual"
	imported, _ := json.MarshalIndent(generic, "", "  ")
	if err := os.WriteFile(filepath.Join(stage, "snapshot", "header.json"), imported, 0o644); err != nil {
		return fail(err)
	}
	if err := os.Rename(filepath.Join(stage, "snapshot"), snapDir); err != nil {
		return fail(err)
	}
	snap.BackupKind = "manual"
	info := r.headerToInfo(snap)
	return portableImportResult{Success: true, ReplacedRepository: hasLocal && !same, Snapshot: &info}
}
