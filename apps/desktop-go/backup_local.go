package main

// 本地备份服务，对应 apps/desktop/src/main/services/backup.ts：单文件 .db 备份（manual / auto / pre-update / pre-restore）。
//
// 在线一致性备份用 VACUUM INTO：MyGo 内置 SQLite 拒绝该语句（authorization denied），改用纯 Go 的 modernc 只读连接执行（见迁移文档「备份验证」）。
// 与 TS 的差异：legacy 自动备份调度器（startBackupScheduler / maybeRunAutoBackup）在 TS 里从未被启动（自动备份已改为后台任务生成的完整快照），
// 所以不移植；`backup:autoStatus` 推送因此也无触发源。

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"time"
)

// supportedSchemaVersion 是当前应用支持的数据库结构版本，对应 TS 的 SCHEMA_VERSION（MIGRATIONS 条数）。
// 取自 Go 迁移清单的长度（db_migrate.go），并由测试保证与 TS 清单同名同序。
var supportedSchemaVersion = schemaVersion()

var (
	backupFilePattern = regexp.MustCompile(`^knowledge-(manual|auto|pre-update|pre-restore)-(\d{8})-(\d{6})(?:-(\d+))?\.db$`)
	requiredTables    = []string{"settings", "knowledge_items", "collections", "tags"}
)

// backupFileInfo 对应 BackupFileInfo（legacy-db 形态）。
type backupFileInfo struct {
	FileName   string `json:"fileName"`
	Path       string `json:"path"`
	Kind       string `json:"kind"`
	SizeBytes  int64  `json:"sizeBytes"`
	CreatedAt  int64  `json:"createdAt"`
	Format     string `json:"format"`
	Encrypted  bool   `json:"encrypted"`
	Validation string `json:"validation"`
}

type localBackups struct {
	dbPath     string // 在线数据库文件
	backupsDir string
	now        func() time.Time
}

func (b *localBackups) clock() time.Time {
	if b.now != nil {
		return b.now()
	}
	return time.Now()
}

func formatBackupTimestamp(t time.Time) string {
	return fmt.Sprintf("%04d%02d%02d-%02d%02d%02d", t.Year(), int(t.Month()), t.Day(), t.Hour(), t.Minute(), t.Second())
}

func parseBackupKind(name string) string {
	if m := backupFilePattern.FindStringSubmatch(name); m != nil {
		return m[1]
	}
	return ""
}

// resolveBackupFileName 生成不冲突的文件名（同一秒内多次备份追加序号）。
func (b *localBackups) resolveBackupFileName(kind string) string {
	base := "knowledge-" + kind + "-" + formatBackupTimestamp(b.clock())
	name, counter := base+".db", 1
	for {
		if _, err := os.Stat(filepath.Join(b.backupsDir, name)); err != nil {
			return name
		}
		name = base + "-" + strconv.Itoa(counter) + ".db"
		counter++
	}
}

// parseBackupTimestamp 从文件名解析创建时间（毫秒）；不用 mtime——整体复制/同步备份目录会让 mtime 全部刷新。
func parseBackupTimestamp(name string) (int64, bool) {
	m := backupFilePattern.FindStringSubmatch(name)
	if m == nil {
		return 0, false
	}
	n := func(s string) int { v, _ := strconv.Atoi(s); return v }
	d, t := m[2], m[3]
	ts := time.Date(n(d[0:4]), time.Month(n(d[4:6])), n(d[6:8]), n(t[0:2]), n(t[2:4]), n(t[4:6]), 0, time.Local).UnixMilli()
	if m[4] != "" {
		ts += int64(n(m[4])) // 同一秒内的多份靠序号区分先后
	}
	return ts, true
}

func (b *localBackups) info(name, kind string) (backupFileInfo, error) {
	full := filepath.Join(b.backupsDir, name)
	st, err := os.Stat(full)
	if err != nil {
		return backupFileInfo{}, err
	}
	created, ok := parseBackupTimestamp(name)
	if !ok {
		created = st.ModTime().UnixMilli()
	}
	return backupFileInfo{FileName: name, Path: full, Kind: kind, SizeBytes: st.Size(), CreatedAt: created, Format: "legacy-db", Encrypted: false, Validation: "unchecked"}, nil
}

// create 对在线库执行 VACUUM INTO 生成备份；失败清理半成品。
func (b *localBackups) create(kind string) (backupFileInfo, error) {
	if err := os.MkdirAll(b.backupsDir, 0o755); err != nil {
		return backupFileInfo{}, err
	}
	name := b.resolveBackupFileName(kind)
	full := filepath.Join(b.backupsDir, name)
	if err := vacuumInto(b.dbPath, full); err != nil {
		os.Remove(full)
		return backupFileInfo{}, err
	}
	return b.info(name, kind)
}

// createSafe 对应 createBackupSafe：不抛错，pre-update/pre-restore 创建后按保留份数清理。
func (b *localBackups) createSafe(kind string) (backupFileInfo, error) {
	info, err := b.create(kind)
	if err != nil {
		return backupFileInfo{}, err
	}
	if kind == "pre-update" || kind == "pre-restore" {
		b.pruneOfKind(kind, snapshotKeepCount)
	}
	return info, nil
}

func (b *localBackups) list() []backupFileInfo {
	out := []backupFileInfo{}
	entries, err := os.ReadDir(b.backupsDir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		kind := parseBackupKind(e.Name())
		if kind == "" {
			continue
		}
		if info, err := b.info(e.Name(), kind); err == nil { // 文件在扫描间隙被移除则跳过
			out = append(out, info)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out
}

func (b *localBackups) findByFileName(name any) *backupFileInfo {
	s, ok := name.(string)
	if !ok || filepath.Base(s) != s {
		return nil
	}
	for _, info := range b.list() {
		if info.FileName == s {
			c := info
			return &c
		}
	}
	return nil
}

type backupDeleteResult struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// delete 仅允许删除备份目录内、命名符合规范的文件（防路径穿越）。
func (b *localBackups) delete(name string) backupDeleteResult {
	if filepath.Base(name) != name || parseBackupKind(name) == "" {
		return backupDeleteResult{Error: "备份文件名不合法"}
	}
	full := filepath.Join(b.backupsDir, name)
	if _, err := os.Stat(full); err != nil {
		return backupDeleteResult{Error: "备份文件已不存在"}
	}
	if err := os.Remove(full); err != nil {
		return backupDeleteResult{Error: err.Error()}
	}
	return backupDeleteResult{Success: true}
}

// pruneOfKind 按类别保留最近 keepCount 份，其余删除；返回删除数量。
func (b *localBackups) pruneOfKind(kind string, keepCount int) int {
	var ofKind []backupFileInfo
	for _, info := range b.list() {
		if info.Kind == kind {
			ofKind = append(ofKind, info)
		}
	}
	keep := max(1, keepCount)
	if len(ofKind) <= keep {
		return 0
	}
	excess := ofKind[keep:]
	for _, info := range excess {
		os.Remove(info.Path)
	}
	return len(excess)
}

// validateBackupFile 恢复前校验：能只读打开、quick_check 通过、含核心表、快照结构合法、结构版本不高于当前。
func validateBackupFile(filePath string) (ok bool, errMsg string) {
	st, err := os.Stat(filePath)
	if err != nil || st.Size() == 0 {
		return false, "备份文件不存在或为空"
	}
	probe, err := openReadOnly(filePath)
	if err != nil {
		return false, "备份文件无法读取: " + err.Error()
	}
	defer probe.Close()
	fail := func(err error) (bool, string) { return false, "备份文件无法读取: " + err.Error() }
	healthy, qerr := quickCheckOK(probe)
	if qerr != nil {
		return fail(qerr)
	}
	if !healthy {
		return false, "备份文件完整性校验未通过"
	}
	for _, table := range requiredTables {
		if !tableExists(probe, table) {
			return false, "备份文件缺少数据表: " + table
		}
	}
	if tableExists(probe, "web_source_versions") {
		payloads, err := queryStrings(probe, "SELECT payload FROM web_source_versions")
		if err != nil {
			return fail(err)
		}
		if err := validateSourceVersionPayloads(payloads); err != nil {
			return fail(err)
		}
	}
	// 来自更新版本的备份可能带着本版本读不懂的结构；旧备份没问题（恢复后迁移执行器会补齐结构）
	ver, err := userVersion(probe)
	if err != nil {
		return fail(err)
	}
	if ver > supportedSchemaVersion {
		return false, fmt.Sprintf("备份来自更新版本的归知（数据结构 v%d，当前支持 v%d），请先升级应用", ver, supportedSchemaVersion)
	}
	if _, err := inspectThemedReadingBackup(probe); err != nil {
		return fail(err)
	}
	return true, ""
}

// performRestoreSwap 恢复的文件交换（调用方必须先关闭数据库）：当前库存为 pre-restore 快照 → 备份先拷成 .incoming 再原子改名 → 清理旁车文件。
// 先拷到同目录的 .incoming 再改名：直接覆盖时拷贝中途失败会把主库截成半个文件，应用重启就起不来。
func (b *localBackups) performRestoreSwap(backupFilePath string) (preRestoreFileName string, err error) {
	databasePath := b.dbPath
	var preRestorePath string
	if _, serr := os.Stat(databasePath); serr == nil {
		if err = os.MkdirAll(b.backupsDir, 0o755); err != nil {
			return "", err
		}
		preRestoreFileName = b.resolveBackupFileName("pre-restore")
		preRestorePath = filepath.Join(b.backupsDir, preRestoreFileName)
		// WAL 模式：先把 -wal 合进主文件，否则只拷主文件会丢最近的写入（db_wal.go）
		if err = checkpointLiveWAL(context.Background(), databasePath); err != nil {
			return "", fmt.Errorf("保存换库前的快照失败：%w", err)
		}
		if err = copyFile(databasePath, preRestorePath); err != nil {
			return "", err
		}
		b.pruneOfKind("pre-restore", snapshotKeepCount)
	}
	incoming := databasePath + ".incoming"
	swapErr := func() error {
		os.Remove(incoming)
		if err := copyFile(backupFilePath, incoming); err != nil {
			return err
		}
		return os.Rename(incoming, databasePath)
	}()
	if swapErr != nil {
		os.Remove(incoming)
		// 无论失败在改名前还是改名中，都用刚存的快照兜一次底，保证留在原地的始终是一个能打开的库
		if preRestorePath != "" {
			if _, serr := os.Stat(preRestorePath); serr == nil {
				if rerr := copyFile(preRestorePath, databasePath); rerr != nil {
					logAppErrorf("backup", "恢复失败后回滚主库", "%v", rerr)
				}
			}
		}
		return "", swapErr
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		os.Remove(databasePath + suffix)
	}
	// 干净关闭标记是给换下去的那个库开的，留着会让恢复出来的库跳过完整性校验
	os.Remove(databasePath + ".clean")
	return preRestoreFileName, nil
}
