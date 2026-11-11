package main

// 对应 funasr-update-backup.ts：只清理归知明确标记为可丢弃的备份，未知旧目录与恢复副本不按年龄猜测。
// 清单格式（update.json，schema 2）与 TS 完全一致，两个版本可互认对方留下的备份。

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type funasrBackupPhase string

const (
	backupCopying          funasrBackupPhase = "copying"
	backupReady            funasrBackupPhase = "ready"
	backupUpdating         funasrBackupPhase = "updating"
	backupRecoveryRequired funasrBackupPhase = "recovery-required"
	backupDisposable       funasrBackupPhase = "disposable"
)

// funasrBackupManifest 字段顺序与 TS 对象字面量一致（JSON.stringify 按插入顺序输出）。
type funasrBackupManifest struct {
	Schema            int                `json:"schema"`
	Kind              string             `json:"kind"`
	Directory         string             `json:"directory"`
	OwnerPid          int                `json:"ownerPid"`
	CreatedAt         string             `json:"createdAt"`
	Phase             funasrBackupPhase  `json:"phase"`
	Original          funasrInstallState `json:"original"`
	TargetVersion     string             `json:"targetVersion"`
	RootIdentity      directoryIdentity  `json:"rootIdentity"`
	DirectoryIdentity directoryIdentity  `json:"directoryIdentity"`
}

type funasrBackup struct {
	Root      string
	Directory string
	Manifest  funasrBackupManifest
}

const funasrBackupManifestName = "update.json"

var reBackupName = regexp.MustCompile(`^update-backup-[a-zA-Z0-9]{6}$`)

func marshalIndent2(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil // Encode 已带结尾换行，等价于 JSON.stringify(v, null, 2) + "\n"
}

// writeExclusive 独占创建并落盘：绝不截断现有临时文件、符号链接或硬链接目标。
func writeExclusive(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// markFunasrBackup 同目录临时文件 + rename；断电留下坏/旧记录时宁可保留。
func markFunasrBackup(b *funasrBackup, phase funasrBackupPhase) error {
	if err := assertBackupIdentity(b); err != nil {
		return err
	}
	next := b.Manifest
	next.Phase = phase
	data, err := marshalIndent2(next)
	if err != nil {
		return err
	}
	tmp := filepath.Join(b.Directory, ".update-"+newUUID()+".tmp")
	if err := writeExclusive(tmp, data); err != nil {
		return err
	}
	if err := assertBackupIdentity(b); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(b.Directory, funasrBackupManifestName)); err != nil {
		return err
	}
	b.Manifest = next
	return nil
}

const alnum = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// mkdtempAlnum 对应 fs.mkdtemp：6 位字母数字后缀（备份名校验依赖这个格式），0700。
func mkdtempAlnum(prefix string) (string, error) {
	for range 100 {
		raw := make([]byte, 6)
		if _, err := rand.Read(raw); err != nil {
			return "", err
		}
		for i := range raw {
			raw[i] = alnum[int(raw[i])%len(alnum)]
		}
		dir := prefix + string(raw)
		err := os.Mkdir(dir, 0o700)
		if err == nil {
			return dir, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", err
		}
	}
	return "", errors.New("无法创建唯一的备份目录")
}

func createFunasrBackup(root string, original funasrInstallState, targetVersion string) (*funasrBackup, error) {
	rootIdentity, err := readDirectoryIdentity(root)
	if err != nil {
		return nil, err
	}
	directory, err := mkdtempAlnum(filepath.Join(root, "update-backup-"))
	if err != nil {
		return nil, err
	}
	dirIdentity, err := readDirectoryIdentity(directory)
	if err != nil {
		return nil, err
	}
	b := &funasrBackup{Root: root, Directory: directory, Manifest: funasrBackupManifest{
		Schema: 2, Kind: "guizhi-funasr-update", Directory: filepath.Base(directory), OwnerPid: os.Getpid(),
		CreatedAt: time.Now().UTC().Format("2006-01-02T15:04:05.000Z"), Phase: backupCopying,
		Original: original, TargetVersion: targetVersion, RootIdentity: rootIdentity, DirectoryIdentity: dirIdentity,
	}}
	if err := markFunasrBackup(b, backupCopying); err != nil {
		return nil, err
	}
	return b, nil
}

func assertBackupIdentity(b *funasrBackup) error {
	if err := assertBackupPath(b.Root, b.Directory); err != nil {
		return err
	}
	if err := assertDirectoryIdentity(b.Root, b.Manifest.RootIdentity); err != nil {
		return err
	}
	return assertDirectoryIdentity(b.Directory, b.Manifest.DirectoryIdentity)
}

func assertBackupContents(b *funasrBackup) error {
	allowed := map[string]bool{"env": true, "tmp": true, "failed-env": true, funasrBackupManifestName: true}
	entries, err := os.ReadDir(b.Directory)
	if err != nil {
		return err
	}
	for _, e := range entries {
		isManifest := e.Name() == funasrBackupManifestName
		if !allowed[e.Name()] || (isManifest && !e.Type().IsRegular()) || (!isManifest && !e.IsDir()) {
			return fmt.Errorf("备份含有非预期内容，保留整个目录：%s", e.Name())
		}
	}
	return assertPlainFunasrTree(b.Directory)
}

func assertBackupPath(root, directory string) error {
	absRoot, _ := filepath.Abs(root)
	absDir, _ := filepath.Abs(directory)
	if filepath.Dir(absDir) != absRoot || !reBackupName.MatchString(filepath.Base(absDir)) {
		return errors.New("备份目录越界，拒绝清理")
	}
	st, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	if !st.IsDir() || isLinkMode(st.Mode()) {
		return errors.New("备份目录是链接，拒绝清理")
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	realDir, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return err
	}
	want := filepath.Join(realRoot, filepath.Base(directory))
	if realDir != want && !(isWindows() && strings.EqualFold(realDir, want)) {
		return errors.New("备份真实路径越界，拒绝清理")
	}
	return nil
}

func compactJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func removeBackup(b *funasrBackup) error {
	if err := assertBackupIdentity(b); err != nil {
		return err
	}
	if err := assertBackupContents(b); err != nil {
		return err
	}
	if err := assertBackupIdentity(b); err != nil {
		return err
	}
	// 扫描后再次读状态；其他操作若已改成恢复中，不使用旧快照继续删除。
	marker := filepath.Join(b.Directory, funasrBackupManifestName)
	st, err := os.Lstat(marker)
	if err != nil {
		return err
	}
	id, err := statIdentity(marker)
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() || isLinkMode(st.Mode()) || id.Nlink != 1 {
		return errors.New("备份状态记录异常，保留文件")
	}
	raw, err := os.ReadFile(marker)
	if err != nil {
		return err
	}
	var current funasrBackupManifest
	if err := json.Unmarshal(raw, &current); err != nil {
		return err
	}
	if current.Phase != backupDisposable || compactJSON(current) != compactJSON(b.Manifest) {
		return errors.New("备份状态发生变化，保留文件")
	}
	if cause := removeAllRetry(b.Directory, 3, 500*time.Millisecond); cause != nil {
		// RemoveAll 可能先删掉记录、再遇到被占用的依赖文件；重写终态，供下次安全重试。
		if assertBackupIdentity(b) == nil {
			// 只补回已被删掉的记录；其他操作写入的新记录不能被覆盖。
			if data, err := marshalIndent2(b.Manifest); err == nil {
				_ = writeExclusive(marker, data)
			}
		}
		return cause
	}
	return nil
}

func removeAllRetry(dir string, retries int, delay time.Duration) error {
	var err error
	for i := 0; i <= retries; i++ {
		if err = os.RemoveAll(dir); err == nil {
			return nil
		}
		time.Sleep(delay)
	}
	return err
}

// discardFunasrBackup 失败返回可展示的警告；清理故障不得把验收成功的升级变成回滚。
func discardFunasrBackup(b *funasrBackup) string {
	err := assertBackupIdentity(b)
	if err == nil {
		err = markFunasrBackup(b, backupDisposable)
	}
	if err == nil {
		err = removeBackup(b)
	}
	if err != nil {
		return fmt.Sprintf("备份清理未完成，下次更新会重试：%s（%s）", b.Directory, jsErrorString(err))
	}
	return ""
}

// jsErrorString 对应 String(error)：Error 对象得到「Error: 消息」。
func jsErrorString(err error) string { return "Error: " + err.Error() }

// cleanupFunasrBackups 仅在更新维护锁内、创建本次备份之前调用。
func cleanupFunasrBackups(root string) ([]string, error) {
	var warnings []string
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "update-backup-") && !strings.HasPrefix(name, "pip-repair-backup-") {
			continue
		}
		directory := filepath.Join(root, name)
		manifest, err := readBackupManifest(root, directory, name)
		if err != nil {
			warnings = append(warnings, "保留状态不明的旧备份，请核对后处理："+directory)
			continue
		}
		// 进程退出不能证明备份已不需要；历史中断记录一律留给用户核对。
		if manifest.Phase != backupDisposable {
			warnings = append(warnings, "保留使用中或可能需要恢复的备份："+directory)
			continue
		}
		if cause := removeBackup(&funasrBackup{Root: root, Directory: directory, Manifest: manifest}); cause != nil {
			warnings = append(warnings, fmt.Sprintf("旧备份清理失败，下次更新会重试：%s（%s）", directory, jsErrorString(cause)))
		}
	}
	return warnings, nil
}

func readBackupManifest(root, directory, name string) (funasrBackupManifest, error) {
	var m funasrBackupManifest
	if err := assertBackupPath(root, directory); err != nil {
		return m, err
	}
	marker := filepath.Join(directory, funasrBackupManifestName)
	st, err := os.Lstat(marker)
	if err != nil {
		return m, err
	}
	if !st.Mode().IsRegular() || isLinkMode(st.Mode()) {
		return m, errors.New("非普通记录")
	}
	raw, err := os.ReadFile(marker)
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return m, err
	}
	if m.Schema != 2 || m.Kind != "guizhi-funasr-update" || m.Directory != name || m.OwnerPid <= 0 {
		return m, errors.New("无法识别记录")
	}
	return m, nil
}

// preserveFunasrRollbackEnvironment 回滚先保存失败现场，恢复复制失败时也不会先删掉现有环境。
func preserveFunasrRollbackEnvironment(b *funasrBackup, environment string, identity directoryIdentity) error {
	if err := assertBackupIdentity(b); err != nil {
		return err
	}
	absEnv, _ := filepath.Abs(environment)
	absWant, _ := filepath.Abs(filepath.Join(b.Root, "env"))
	if absEnv != absWant {
		return errors.New("回滚环境路径越界，保留文件")
	}
	if err := assertDirectoryIdentity(environment, identity); err != nil {
		return err
	}
	source := filepath.Join(b.Directory, "env")
	if err := assertPlainFunasrTree(source); err != nil {
		return err
	}
	for _, name := range []string{"Scripts/python.exe", "Lib/site-packages/funasr/__init__.py"} {
		st, err := os.Lstat(filepath.Join(source, filepath.FromSlash(name)))
		if err != nil {
			return err
		}
		if !st.Mode().IsRegular() {
			return errors.New("恢复备份缺少引擎文件，保留当前环境")
		}
	}
	failed := filepath.Join(b.Directory, "failed-env")
	if _, err := os.Lstat(failed); err == nil {
		return errors.New("失败环境目录已存在，拒绝覆盖")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := assertBackupIdentity(b); err != nil {
		return err
	}
	if err := assertDirectoryIdentity(environment, identity); err != nil {
		return err
	}
	return os.Rename(environment, failed)
}
