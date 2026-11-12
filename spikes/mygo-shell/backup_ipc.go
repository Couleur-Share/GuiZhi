package main

// 备份 / 恢复 IPC，对应 apps/desktop/src/main/ipc/backup.ipc.ts。
//
// 恢复是破坏性操作：只接受备份目录内规范命名的文件；有采集/导入/后台写任务在跑时拒绝；
// 换库成功后自动重启应用。重启通过 l.relaunch（新进程等本进程退出后再取单实例锁）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const (
	restoreRelaunchDelay = 800 * time.Millisecond

	// chBackupAutoStatus 是主进程→渲染进程的推送频道（自动备份开始/结束/失败）。
	// TS 里只有 legacy 自动备份调度器会触发它，而该调度器从未被启动，所以这里只保留频道名，没有触发源。
	chBackupAutoStatus = "backup:autoStatus"
)

type backupState struct {
	mu       sync.Mutex // 串行化仓库操作（TS 是单线程；这里创建快照可能耗时较长，不阻塞其它频道）
	repo     *backupRepository
	renderer *rendererBackupSettings
}

func (l *Library) backupsDir() string { return l.paths.BackupsDir() }

func (l *Library) localBackups() *localBackups {
	return &localBackups{dbPath: l.dbPath, backupsDir: l.backupsDir()}
}

func (l *Library) repositoryConfigDir() string {
	if l.configDir != "" {
		return l.configDir
	}
	return l.paths.ConfigDir()
}

// backupRepository 对应 getBackupRepository()：按需创建单例。
func (l *Library) backupRepository() *backupRepository {
	if l.backup.repo == nil {
		l.backup.repo = &backupRepository{
			paths: repoPaths{
				RepositoryDir: filepath.Join(l.backupsDir(), "repository"),
				DatabasePath:  l.dbPath,
				ConfigDir:     l.repositoryConfigDir(),
				ImagesDir:     l.paths.ImagesDir(),
				VideosDir:     l.paths.VideosDir(),
			},
			schemaVersion: supportedSchemaVersion,
			protector:     newKeyProtector(l.paths.UserData),
		}
	}
	return l.backup.repo
}

// readSettingRaw 读 settings 表里的原始字符串值。
func (l *Library) readSettingRaw(ctx context.Context, key string) (string, bool) {
	rows, err := l.db.Query(ctx, "SELECT value FROM settings WHERE key = ?", key)
	if err != nil || len(rows.Values) == 0 {
		return "", false
	}
	s, _ := rows.Values[0][0].(string)
	return s, true
}

// backupBoolSetting 对应 backup.ipc.ts 的 readBooleanSetting：JSON.parse(value) === true；解析失败时看原文是不是 "true"。
func (l *Library) backupBoolSetting(ctx context.Context, key string, fallback bool) bool {
	raw, ok := l.readSettingRaw(ctx, key)
	if !ok {
		return fallback
	}
	var v any
	if json.Unmarshal([]byte(raw), &v) != nil {
		return raw == "true"
	}
	b, _ := v.(bool)
	return b
}

// backupNumberSetting 对应 readNumberSetting：Number(JSON.parse(value))，非有限数回落。
func (l *Library) backupNumberSetting(ctx context.Context, key string, fallback float64) float64 {
	raw, ok := l.readSettingRaw(ctx, key)
	if !ok {
		return fallback
	}
	var v any
	if json.Unmarshal([]byte(raw), &v) != nil {
		return fallback
	}
	switch n := v.(type) {
	case float64:
		return n
	case bool:
		if n {
			return 1
		}
		return 0
	case nil:
		return 0
	case string:
		if f, err := strconv.ParseFloat(jsTrim(n), 64); err == nil && !math.IsInf(f, 0) {
			return f
		}
		if jsTrim(n) == "" {
			return 0
		}
	}
	return fallback
}

func roundHalfUp(f float64) float64 { return math.Floor(f + 0.5) }

// syncRepositoryBackupJob 对应同名函数：仓库已初始化、可无人值守解锁、开启后台任务与自动备份时才启用仓库自动备份任务。
func (l *Library) syncRepositoryBackupJob(ctx context.Context) {
	status := l.backupRepository().status()
	hours := math.Min(math.Max(roundHalfUp(l.backupNumberSetting(ctx, "backupIntervalHours", 24)), 1), 168)
	enabled := status.Initialized && status.AutomaticAccessAvailable &&
		l.backupBoolSetting(ctx, "backgroundTasksEnabled", false) &&
		l.backupBoolSetting(ctx, "backupAutoEnabled", true)
	var preserved *int64
	if jobs, err := l.jobs.List(ctx); err == nil {
		for _, j := range jobs {
			if j["kind"] == "backup" && j["scopeId"] == "repository" && (j["state"] == "scheduled" || j["state"] == "retry_wait") {
				if j["nextRunAt"] != nil {
					v := asInt(j["nextRunAt"])
					preserved = &v
				}
				break
			}
		}
	}
	next := time.Now().UnixMilli() + int64(hours)*60*60_000
	if preserved != nil {
		next = *preserved
	}
	interval := hours * 60
	if _, err := l.jobs.Schedule(ctx, "repository-auto-backup", jobScheduleInput{
		Kind: "backup", ScopeID: "repository", IntervalMinutes: &interval, NextRunAt: &next, Disabled: !enabled,
	}); err != nil {
		logAppErrorf("backup", "同步仓库备份任务", "%v", err)
	}
}

func str(v any) string { s, _ := v.(string); return s }

// snapshotCreateOutput 对应 RepositorySnapshotResult：失败时只带 error（TS 不输出计数字段）。
func snapshotCreateOutput(r snapshotResult) map[string]any {
	if !r.Success {
		return map[string]any{"success": false, "error": r.Error}
	}
	return map[string]any{"success": true, "snapshot": r.Snapshot, "reusedObjects": r.ReusedObjects, "createdObjects": r.CreatedObjects}
}

// rendererSettingsFromRequest：请求里的 rendererSettings（对象）优先，否则用 syncRendererSettings 记下的最近一份。
func (l *Library) rendererSettingsFromRequest(req map[string]json.RawMessage) *omap {
	if raw, ok := req["rendererSettings"]; ok && len(raw) > 0 && raw[0] == '{' {
		if v, err := parseOrdered(raw); err == nil {
			if o, ok := v.(*omap); ok {
				return o
			}
		}
	}
	return l.backup.renderer.ordered()
}

func (l *Library) registerBackup(b *Bridge, renderer *rendererBackupSettings) {
	l.backup.renderer = renderer
	renderer.mu.Lock()
	renderer.onChange = func() { l.syncRepositoryBackupJob(context.Background()) }
	renderer.mu.Unlock()
	_ = l.jobs.RegisterHandler("backup", func(ctx context.Context, _ map[string]any) (*jobHandlerResult, error) {
		l.backup.mu.Lock()
		defer l.backup.mu.Unlock()
		repo := l.backupRepository()
		res := repo.createSnapshot(snapshotRequest{RendererSettings: renderer.ordered(), Kind: "auto", AppVersion: appVersion})
		if !res.Success {
			if res.Error == "" {
				res.Error = "完整备份失败"
			}
			return nil, errors.New(res.Error)
		}
		_, err := repo.pruneAutoSnapshots(int(math.Round(l.backupNumberSetting(ctx, "backupKeepCount", 10))))
		return nil, err
	})
	l.syncRepositoryBackupJob(context.Background())

	obj := func(a []json.RawMessage, i int) map[string]json.RawMessage {
		var m map[string]json.RawMessage
		if len(a) > i && len(a[i]) > 0 && a[i][0] == '{' {
			_ = json.Unmarshal(a[i], &m)
		}
		return m
	}
	strField := func(m map[string]json.RawMessage, k string) string {
		var s string
		if raw, ok := m[k]; ok {
			_ = json.Unmarshal(raw, &s)
		}
		return s
	}
	errText := func(err error) string { return err.Error() }
	lock := func() func() { l.backup.mu.Lock(); return l.backup.mu.Unlock }

	b.handlers["backup:create"] = func(context.Context, []json.RawMessage) (any, error) {
		info, err := l.localBackups().create("manual")
		if err != nil {
			return map[string]any{"success": false, "error": errText(err)}, nil
		}
		return map[string]any{"success": true, "backup": info}, nil
	}
	b.handlers["backup:list"] = func(context.Context, []json.RawMessage) (any, error) {
		return l.localBackups().list(), nil
	}
	b.handlers["backup:delete"] = func(_ context.Context, a []json.RawMessage) (any, error) {
		var name string
		if len(a) > 0 {
			_ = json.Unmarshal(a[0], &name)
		}
		return l.localBackups().delete(name), nil
	}
	b.handlers["backup:restore"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		var name any
		if len(a) > 0 {
			_ = json.Unmarshal(a[0], &name)
		}
		return l.restoreLocalBackup(ctx, name), nil
	}

	b.handlers["backupRepository:status"] = func(context.Context, []json.RawMessage) (any, error) {
		defer lock()()
		return l.backupRepository().status(), nil
	}
	b.handlers["backupRepository:init"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		defer lock()()
		var pw string
		if len(a) > 0 {
			_ = json.Unmarshal(a[0], &pw)
		}
		repo := l.backupRepository()
		if err := repo.initialize(pw); err != nil {
			st := repo.status()
			return statusFields(false, st, errText(err)), nil
		}
		st := repo.status()
		l.syncRepositoryBackupJob(ctx)
		return statusFields(true, st, ""), nil
	}
	b.handlers["backupRepository:changePassword"] = func(_ context.Context, a []json.RawMessage) (any, error) {
		defer lock()()
		req := obj(a, 0)
		if err := l.backupRepository().changePassword(strField(req, "currentPassword"), strField(req, "nextPassword")); err != nil {
			return map[string]any{"success": false, "error": errText(err)}, nil
		}
		return map[string]any{"success": true}, nil
	}
	b.handlers["backupRepository:createSnapshot"] = func(_ context.Context, a []json.RawMessage) (any, error) {
		defer lock()()
		req := obj(a, 0)
		res := l.backupRepository().createSnapshot(snapshotRequest{
			RecoveryPassword: strField(req, "recoveryPassword"),
			RendererSettings: l.rendererSettingsFromRequest(req),
			Kind:             "manual", AppVersion: appVersion,
		})
		return snapshotCreateOutput(res), nil
	}
	b.handlers["backupRepository:listSnapshots"] = func(context.Context, []json.RawMessage) (any, error) {
		defer lock()()
		return l.backupRepository().listSnapshots(), nil
	}
	b.handlers["backupRepository:preview"] = func(_ context.Context, a []json.RawMessage) (any, error) {
		defer lock()()
		req := obj(a, 0)
		return previewRepositoryRestore(l.backupRepository(), strField(req, "snapshotId"), strField(req, "recoveryPassword")), nil
	}
	b.handlers["backupRepository:deleteSnapshot"] = func(_ context.Context, a []json.RawMessage) (any, error) {
		defer lock()()
		var id string
		if len(a) > 0 {
			_ = json.Unmarshal(a[0], &id)
		}
		n, err := l.backupRepository().deleteSnapshot(id, "")
		if err != nil {
			return map[string]any{"success": false, "error": errText(err)}, nil
		}
		return map[string]any{"success": true, "removedObjects": n}, nil
	}
	b.handlers["backupRepository:exportPortable"] = func(_ context.Context, a []json.RawMessage) (any, error) {
		req := obj(a, 0)
		name := fmt.Sprintf("GuiZhi-%s.guizhi-backup", formatBackupTimestamp(time.Now()))
		dest, err := l.saveFileDialog("导出归知便携备份", defaultExportPath(name), []dialogFilter{{Name: "GuiZhi Backup", Exts: []string{"guizhi-backup"}}})
		if err != nil {
			return map[string]any{"success": false, "error": errText(err)}, nil
		}
		if dest == "" {
			return map[string]any{"success": false, "canceled": true}, nil
		}
		defer lock()()
		if err := l.backupRepository().exportPortable(strField(req, "snapshotId"), dest, strField(req, "recoveryPassword")); err != nil {
			return map[string]any{"success": false, "error": errText(err)}, nil
		}
		return map[string]any{"success": true, "filePath": dest}, nil
	}
	b.handlers["backupRepository:importPortable"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		req := obj(a, 0)
		password := strField(req, "recoveryPassword")
		if password == "" {
			return map[string]any{"success": false, "error": "请先输入这份便携备份对应的恢复口令"}, nil
		}
		if l.pickFiles == nil {
			return map[string]any{"success": false, "error": "当前环境不支持选择文件"}, nil
		}
		paths, err := l.pickFiles(false, []dialogFilter{{Name: "GuiZhi Backup", Exts: []string{"guizhi-backup"}}, {Name: "All Files", Exts: []string{"*"}}})
		if err != nil {
			return map[string]any{"success": false, "error": errText(err)}, nil
		}
		if len(paths) == 0 {
			return map[string]any{"success": false, "canceled": true}, nil
		}
		defer lock()()
		res := l.backupRepository().importPortable(paths[0], password)
		if res.Success {
			l.syncRepositoryBackupJob(ctx) // 新机器导入后仓库变为已启用，立即按新状态调度自动完整备份
		}
		return res, nil
	}
	b.handlers["backupRepository:restore"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		req := obj(a, 0)
		return l.restoreRepositorySnapshot(ctx, strField(req, "snapshotId"), strField(req, "recoveryPassword")), nil
	}
	b.handlers["backupRepository:consumeRendererSettings"] = func(context.Context, []json.RawMessage) (any, error) {
		pending := filepath.Join(l.repositoryConfigDir(), "pending-renderer-settings.json")
		raw, err := os.ReadFile(pending)
		if err != nil {
			return nil, nil
		}
		var probe any
		if err := json.Unmarshal(raw, &probe); err != nil {
			log.Printf("[backup] 读取恢复后的 Renderer 设置失败：%v", err) // 解析失败不删文件，留给人工排查
			return nil, nil
		}
		_ = os.Remove(pending)
		if _, ok := probe.(map[string]any); !ok {
			return nil, nil
		}
		return json.RawMessage(raw), nil
	}
}

// statusFields 对应 `{ success, ...status }` 里的展开：warning 为空时整个字段省略（TS 是 undefined）。
func statusFields(success bool, st repoStatus, errMsg string) map[string]any {
	m := map[string]any{"success": success, "initialized": st.Initialized, "automaticAccessAvailable": st.AutomaticAccessAvailable, "keyStorageBackend": st.KeyStorageBackend}
	if st.Warning != "" {
		m["warning"] = st.Warning
	}
	if errMsg != "" {
		m["error"] = errMsg
	}
	return m
}

// ── 恢复编排 ──

func (l *Library) scheduleRelaunch(delay time.Duration) {
	time.AfterFunc(delay, func() {
		if l.relaunch != nil {
			l.relaunch()
		}
	})
}

// closeDatabaseForRestore 对应 closeDatabase()：换库前必须释放文件句柄。
func (l *Library) closeDatabaseForRestore() {
	if err := l.db.Close(); err != nil {
		log.Printf("[backup] 关闭数据库：%v", err)
	}
}

func (l *Library) stopServicesForRestore() {
	l.jobs.Stop()
	if l.retireIdleThemedReading != nil {
		l.retireIdleThemedReading()
	}
	// stopMobileCapture / stopReadingSync 属冻结功能，Go 壳层没有对应服务
}

func (l *Library) restoreGuards(ctx context.Context) string {
	if l.hasActiveThemedReading != nil && l.hasActiveThemedReading() {
		return "主题排版正在执行，请先停止任务再恢复备份"
	}
	n, err := l.countActiveImportTasks(ctx)
	if err != nil {
		return err.Error()
	}
	if n > 0 {
		return "有采集任务正在进行，请等待完成或取消后再恢复"
	}
	return ""
}

func (l *Library) restoreLocalBackup(ctx context.Context, name any) map[string]any {
	lb := l.localBackups()
	backup := lb.findByFileName(name)
	if backup == nil {
		return map[string]any{"success": false, "error": "找不到指定的备份文件"}
	}
	if msg := l.restoreGuards(ctx); msg != "" {
		return map[string]any{"success": false, "error": msg}
	}
	if ok, msg := validateBackupFile(backup.Path); !ok {
		return map[string]any{"success": false, "error": msg}
	}
	l.stopServicesForRestore()
	l.closeDatabaseForRestore()
	if _, err := lb.performRestoreSwap(backup.Path); err != nil {
		// 数据库已关闭，无论交换是否完成都必须重启恢复运行状态；performRestoreSwap 失败时会用 pre-restore 快照回滚主库
		logAppErrorf("backup", "恢复过程中出错，应用将重启", "%v", err)
		l.scheduleRelaunch(restoreRelaunchDelay * 3)
		return map[string]any{"success": false, "relaunching": true,
			"error": fmt.Sprintf("恢复失败：%s。已尝试用换库前的 pre-restore 快照回滚，快照保存在 %s", err.Error(), l.backupsDir())}
	}
	log.Printf("[backup] 已从备份恢复：%s，即将重启应用", backup.FileName)
	l.scheduleRelaunch(restoreRelaunchDelay)
	return map[string]any{"success": true, "relaunching": true}
}

func (l *Library) restoreRepositorySnapshot(ctx context.Context, snapshotID, password string) map[string]any {
	l.backup.mu.Lock()
	defer l.backup.mu.Unlock()
	if msg := l.restoreGuards(ctx); msg != "" {
		return map[string]any{"success": false, "error": msg}
	}
	rows, err := l.db.Query(ctx, "SELECT COUNT(*) AS count FROM background_jobs WHERE state = 'running'")
	if err != nil {
		return map[string]any{"success": false, "error": err.Error()}
	}
	if len(rows.Values) > 0 && asInt(rows.Values[0][0]) > 0 {
		return map[string]any{"success": false, "error": "有后台写任务正在执行，请等待本轮完成后再恢复"}
	}
	repo := l.backupRepository()
	preview := previewRepositoryRestore(repo, snapshotID, password)
	if !preview.Success {
		msg := preview.Error
		if msg == "" {
			msg = "恢复预检失败"
		}
		return map[string]any{"success": false, "error": msg}
	}
	targets := restoreTargets{
		DatabasePath: l.dbPath, ImagesDir: l.paths.ImagesDir(), VideosDir: l.paths.VideosDir(), ConfigDir: l.repositoryConfigDir(),
	}
	live := func(key string) (string, bool, error) {
		rows, err := l.db.Query(ctx, "SELECT value FROM settings WHERE key = ?", key)
		if err != nil {
			return "", false, err
		}
		if len(rows.Values) == 0 {
			return "", false, nil
		}
		s, _ := rows.Values[0][0].(string)
		return s, true, nil
	}
	prepared, err := prepareRepositoryRestore(repo, snapshotID, password, live, targets, l.backup.renderer.ordered())
	if err == nil {
		lb := l.localBackups()
		if _, err = lb.create("pre-restore"); err == nil {
			lb.pruneOfKind("pre-restore", snapshotKeepCount)
		} else {
			os.RemoveAll(prepared.StageDir)
		}
	}
	if err != nil {
		return map[string]any{"success": false, "error": "恢复准备失败：" + err.Error()}
	}
	l.stopServicesForRestore()
	l.closeDatabaseForRestore()
	if err := applyPreparedRepositoryRestore(prepared, targets); err != nil {
		l.scheduleRelaunch(restoreRelaunchDelay * 3)
		return map[string]any{"success": false, "relaunching": true, "error": "恢复交换失败并已回滚：" + err.Error()}
	}
	l.scheduleRelaunch(restoreRelaunchDelay)
	return map[string]any{"success": true, "relaunching": true}
}
