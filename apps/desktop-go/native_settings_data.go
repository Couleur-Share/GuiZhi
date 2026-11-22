package main

// 设置页「数据与备份」分区的主包适配器（native/services/data_backup.go）。
//
// 与 native_adapters.go 的其余适配器同一做法：经 Bridge.call 调已登记的 IPC 处理器（与渲染进程同一份逻辑、同一套错误），
// 再把 JSON 结果解码成 DTO；备份、恢复、导出与配置迁移的行为只有一个来源，WAL 与 DPAPI 的边界都在处理器那一侧
// （备份走 VACUUM INTO，恢复前 checkpointLiveWAL，机密按口令加密），这里不绕过、不复刻。
// 对应关系由 native_settings_data_test.go 用真实处理器与临时目录守住。
//
//	Data.RuntimePaths       → app:getRuntimePaths
//	Data.OpenPath           → shell:openPath
//	Data.Relaunch           → app:relaunch
//	Backup.List/Create      → backup:list / backup:create
//	Backup.Restore/Delete   → backup:restore / backup:delete
//	Backup.ExportMarkdown   → export:markdown
//	Backup.RepositoryStatus → backupRepository:status
//	Backup.ListSnapshots    → backupRepository:listSnapshots
//	Backup.InitRepository   → backupRepository:init
//	Backup.ChangePassword   → backupRepository:changePassword
//	Backup.CreateSnapshot   → backupRepository:createSnapshot
//	Backup.PreviewRestore   → backupRepository:preview
//	Backup.RestoreSnapshot  → backupRepository:restore
//	Backup.DeleteSnapshot   → backupRepository:deleteSnapshot
//	Backup.ExportPortable   → backupRepository:exportPortable
//	Backup.ImportPortable   → backupRepository:importPortable
//	ConfigTransfer.*        → config:export / config:read / config:apply（设置的取与写经 services.Settings 与 Prefs）
//
// 口令（恢复口令、导出密码、代理口令、API Key）只作为处理器入参经过这里：不进错误文案、不进日志。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/settings"
)

// ipcResult 是备份、导出、配置迁移类处理器的统一回复头：{ success, canceled?, error? }。
type ipcResult struct {
	Success  bool   `json:"success"`
	Canceled bool   `json:"canceled"`
	Error    string `json:"error"`
}

// err 把失败的回复变成带原因的错误：处理器没给原因时补一条兜底，不让界面拿到既失败又没有原因的结果。
// 已取消的不是失败，调用方先判 Canceled。
func (r ipcResult) err(fallback string) error {
	if r.Success {
		return nil
	}
	if strings.TrimSpace(r.Error) == "" {
		return errors.New(fallback)
	}
	return errors.New(r.Error)
}

// ── 数据目录 ──

type dataAdapter struct{ b *Bridge }

var _ services.Data = dataAdapter{}

func (a dataAdapter) RuntimePaths(ctx context.Context) (services.RuntimePaths, error) {
	return callAs[services.RuntimePaths](ctx, a.b, "app:getRuntimePaths")
}

func (a dataAdapter) OpenPath(ctx context.Context, path string) error {
	reply, err := callAs[ipcResult](ctx, a.b, "shell:openPath", path)
	if err != nil {
		return err
	}
	return reply.err("open path failed")
}

func (a dataAdapter) Relaunch(ctx context.Context) error {
	reply, err := callAs[ipcResult](ctx, a.b, "app:relaunch")
	if err != nil {
		return err
	}
	return reply.err("relaunch failed")
}

// ── 备份 ──

type backupAdapter struct{ b *Bridge }

var _ services.Backup = backupAdapter{}

func (a backupAdapter) List(ctx context.Context) ([]services.BackupInfo, error) {
	return callAs[[]services.BackupInfo](ctx, a.b, "backup:list")
}

func (a backupAdapter) Create(ctx context.Context) (services.BackupInfo, error) {
	reply, err := callAs[struct {
		ipcResult
		Backup services.BackupInfo `json:"backup"`
	}](ctx, a.b, "backup:create")
	if err != nil {
		return services.BackupInfo{}, err
	}
	if err := reply.err("backup failed"); err != nil {
		return services.BackupInfo{}, err
	}
	return reply.Backup, nil
}

func (a backupAdapter) Restore(ctx context.Context, fileName string) error {
	reply, err := callAs[ipcResult](ctx, a.b, "backup:restore", fileName)
	if err != nil {
		return err
	}
	return reply.err("restore failed")
}

func (a backupAdapter) Delete(ctx context.Context, fileName string) error {
	reply, err := callAs[ipcResult](ctx, a.b, "backup:delete", fileName)
	if err != nil {
		return err
	}
	return reply.err("delete failed")
}

func (a backupAdapter) ExportMarkdown(ctx context.Context) (services.MarkdownExport, error) {
	reply, err := callAs[struct {
		ipcResult
		Count int    `json:"count"`
		Dir   string `json:"dir"`
	}](ctx, a.b, "export:markdown")
	if err != nil {
		return services.MarkdownExport{}, err
	}
	if reply.Canceled {
		return services.MarkdownExport{Canceled: true}, nil
	}
	if err := reply.err("export failed"); err != nil {
		return services.MarkdownExport{}, err
	}
	return services.MarkdownExport{Count: reply.Count, Dir: reply.Dir}, nil
}

func (a backupAdapter) RepositoryStatus(ctx context.Context) (services.RepositoryStatus, error) {
	return callAs[services.RepositoryStatus](ctx, a.b, "backupRepository:status")
}

func (a backupAdapter) ListSnapshots(ctx context.Context) ([]services.BackupInfo, error) {
	return callAs[[]services.BackupInfo](ctx, a.b, "backupRepository:listSnapshots")
}

func (a backupAdapter) InitRepository(ctx context.Context, password string) error {
	reply, err := callAs[ipcResult](ctx, a.b, "backupRepository:init", password)
	if err != nil {
		return err
	}
	return reply.err("initialize backup repository failed")
}

func (a backupAdapter) ChangePassword(ctx context.Context, current, next string) error {
	reply, err := callAs[ipcResult](ctx, a.b, "backupRepository:changePassword",
		map[string]any{"currentPassword": current, "nextPassword": next})
	if err != nil {
		return err
	}
	return reply.err("change recovery password failed")
}

// repoRequest 是 { snapshotId, recoveryPassword? } 请求：恢复口令为空时不带这个字段（渲染层的 `recoveryPassword || undefined`）。
func repoRequest(snapshotID, recoveryPassword string) map[string]any {
	req := map[string]any{}
	if snapshotID != "" {
		req["snapshotId"] = snapshotID
	}
	if recoveryPassword != "" {
		req["recoveryPassword"] = recoveryPassword
	}
	return req
}

func (a backupAdapter) CreateSnapshot(ctx context.Context, recoveryPassword string) (services.SnapshotCreated, error) {
	// 不带 rendererSettings：原生界面没有渲染层偏好要随快照保存；界面设置在 config/ui-prefs.json，跟配置目录一起进快照。
	reply, err := callAs[struct {
		ipcResult
		services.SnapshotCreated
	}](ctx, a.b, "backupRepository:createSnapshot", repoRequest("", recoveryPassword))
	if err != nil {
		return services.SnapshotCreated{}, err
	}
	if err := reply.err("full backup failed"); err != nil {
		return services.SnapshotCreated{}, err
	}
	return reply.SnapshotCreated, nil
}

func (a backupAdapter) PreviewRestore(ctx context.Context, snapshotID, recoveryPassword string) (services.RestorePreview, error) {
	reply, err := callAs[struct {
		ipcResult
		Snapshot     services.BackupInfo `json:"snapshot"`
		MissingFiles []string            `json:"missingFiles"`
		DamagedFiles []string            `json:"damagedFiles"`
		Warnings     []string            `json:"warnings"`
	}](ctx, a.b, "backupRepository:preview", repoRequest(snapshotID, recoveryPassword))
	if err != nil {
		return services.RestorePreview{}, err
	}
	if !reply.Success {
		msg := strings.TrimSpace(reply.Error)
		if msg == "" {
			msg = "restore preview failed"
		}
		return services.RestorePreview{}, &services.RestorePreviewError{Message: msg, MissingFiles: reply.MissingFiles, DamagedFiles: reply.DamagedFiles}
	}
	return services.RestorePreview{Snapshot: reply.Snapshot, Warnings: reply.Warnings}, nil
}

func (a backupAdapter) RestoreSnapshot(ctx context.Context, snapshotID, recoveryPassword string) error {
	reply, err := callAs[ipcResult](ctx, a.b, "backupRepository:restore", repoRequest(snapshotID, recoveryPassword))
	if err != nil {
		return err
	}
	return reply.err("restore failed")
}

func (a backupAdapter) DeleteSnapshot(ctx context.Context, snapshotID string) error {
	reply, err := callAs[ipcResult](ctx, a.b, "backupRepository:deleteSnapshot", snapshotID)
	if err != nil {
		return err
	}
	return reply.err("delete snapshot failed")
}

func (a backupAdapter) ExportPortable(ctx context.Context, snapshotID, recoveryPassword string) (services.PortableExport, error) {
	reply, err := callAs[struct {
		ipcResult
		FilePath string `json:"filePath"`
	}](ctx, a.b, "backupRepository:exportPortable", repoRequest(snapshotID, recoveryPassword))
	if err != nil {
		return services.PortableExport{}, err
	}
	if reply.Canceled {
		return services.PortableExport{Canceled: true}, nil
	}
	if err := reply.err("export portable backup failed"); err != nil {
		return services.PortableExport{}, err
	}
	return services.PortableExport{FilePath: reply.FilePath}, nil
}

func (a backupAdapter) ImportPortable(ctx context.Context, recoveryPassword string) (services.PortableImport, error) {
	reply, err := callAs[struct {
		ipcResult
		AlreadyExisted     bool                 `json:"alreadyExisted"`
		ReplacedRepository bool                 `json:"replacedRepository"`
		Snapshot           *services.BackupInfo `json:"snapshot"`
	}](ctx, a.b, "backupRepository:importPortable", map[string]any{"recoveryPassword": recoveryPassword})
	if err != nil {
		return services.PortableImport{}, err
	}
	if reply.Canceled {
		return services.PortableImport{Canceled: true}, nil
	}
	if err := reply.err("import portable backup failed"); err != nil {
		return services.PortableImport{}, err
	}
	return services.PortableImport{AlreadyExisted: reply.AlreadyExisted, ReplacedRepository: reply.ReplacedRepository, Snapshot: reply.Snapshot}, nil
}

// ── 配置迁移 ──

// configAdapter 在渲染层 use-config-transfer + config-local-storage 的位置上：设置的采集与写回。
// 渲染层从 localStorage 取 guizhi-settings 与界面偏好；原生界面没有 localStorage，
// 采集自 services.Settings（UI 与 App）、AI 配置文件（mergeSharedAIConfig，与 settings:get 同一份合并）与 Prefs，
// 写回走同样的两个服务——换算规则是 native/settings/transfer.go 的纯函数。
type configAdapter struct {
	b        *Bridge
	lib      *Library
	settings services.Settings
	prefs    services.Prefs
}

var _ services.ConfigTransfer = configAdapter{}

func newConfigAdapter(b *Bridge, lib *Library, set services.Settings, prefs services.Prefs) configAdapter {
	return configAdapter{b: b, lib: lib, settings: set, prefs: prefs}
}

// collectSettings 拼出导出文件的 settings 记录：UI 与 App 的全部键，再并入 AI 服务商、模型与路由。
func (a configAdapter) collectSettings() (map[string]any, error) {
	if a.settings == nil {
		return nil, errors.New("设置服务不可用")
	}
	values, err := a.settings.Snapshot()
	if err != nil {
		return nil, err
	}
	state, err := settings.TransferState(values)
	if err != nil {
		return nil, err
	}
	record := make(map[string]any, len(state)+8)
	for k, raw := range state {
		record[k] = raw
	}
	if a.lib != nil {
		a.lib.mergeSharedAIConfig(record)
	}
	return record, nil
}

func (a configAdapter) layout() map[string]any {
	if a.prefs == nil {
		return map[string]any{}
	}
	return settings.TransferLayout(a.prefs.Get)
}

func (a configAdapter) Export(ctx context.Context, opts services.ConfigExportOptions) (services.ConfigExportResult, error) {
	record, err := a.collectSettings()
	if err != nil {
		return services.ConfigExportResult{}, fmt.Errorf("采集配置失败：%w", err)
	}
	reply, err := callAs[struct {
		ipcResult
		FilePath string `json:"filePath"`
	}](ctx, a.b, "config:export", map[string]any{
		"settings":        record,
		"settingsVersion": settings.PersistedVersion,
		"uiLayout":        a.layout(),
		"includeSecrets":  opts.IncludeSecrets,
		"password":        opts.Password,
		"includeUiLayout": opts.IncludeUILayout,
		// 正文配图已移除，导出包不再带配图风格（与渲染层一致）。
		"includeIllustrationStyles": false,
		"includeShortcuts":          opts.IncludeShortcuts,
		"includeMcpScope":           opts.IncludeMCPScope,
	})
	if err != nil {
		return services.ConfigExportResult{}, err
	}
	if reply.Canceled {
		return services.ConfigExportResult{Canceled: true}, nil
	}
	if err := reply.err("export config failed"); err != nil {
		return services.ConfigExportResult{}, err
	}
	return services.ConfigExportResult{FilePath: reply.FilePath}, nil
}

func (a configAdapter) Read(ctx context.Context) (services.ConfigRead, error) {
	reply, err := callAs[struct {
		ipcResult
		FilePath string                  `json:"filePath"`
		Preview  *services.ConfigPreview `json:"preview"`
	}](ctx, a.b, "config:read")
	if err != nil {
		return services.ConfigRead{}, err
	}
	if reply.Canceled {
		return services.ConfigRead{Canceled: true}, nil
	}
	if err := reply.err("read config failed"); err != nil {
		return services.ConfigRead{}, err
	}
	if reply.Preview == nil || reply.FilePath == "" {
		return services.ConfigRead{}, errors.New("read config failed")
	}
	return services.ConfigRead{FilePath: reply.FilePath, Preview: *reply.Preview}, nil
}

// applyReply 是 config:apply 的回复。settings 与 uiLayout 是要写回本机的最终值（AI 部分处理器已落到 ai-models.json）。
type applyReply struct {
	ipcResult
	WrongPassword   bool                       `json:"wrongPassword"`
	Settings        map[string]json.RawMessage `json:"settings"`
	SettingsVersion *int                       `json:"settingsVersion"`
	UILayout        map[string]json.RawMessage `json:"uiLayout"`
	Warnings        []string                   `json:"warnings"`
	SnapshotDir     string                     `json:"snapshotDir"`
}

func (a configAdapter) Apply(ctx context.Context, filePath, password string, sel services.ConfigApplySelection) (services.ConfigApplyResult, error) {
	reply, err := callAs[applyReply](ctx, a.b, "config:apply", filePath, password, map[string]any{
		"settings": sel.Settings, "uiLayout": sel.UILayout, "illustrationStyles": false,
		"shortcuts": sel.Shortcuts, "mcpScope": sel.MCPScope,
	})
	if err != nil {
		return services.ConfigApplyResult{}, err
	}
	if !reply.Success {
		if reply.WrongPassword {
			return services.ConfigApplyResult{}, &services.ConfigWrongPasswordError{Message: reply.Error}
		}
		return services.ConfigApplyResult{}, reply.err("import config failed")
	}

	result := services.ConfigApplyResult{Warnings: reply.Warnings, SnapshotDir: reply.SnapshotDir}
	if sel.Settings && len(reply.Settings) > 0 {
		if err := a.writeSettings(reply); err != nil {
			return services.ConfigApplyResult{}, err
		}
	}
	if sel.UILayout && len(reply.UILayout) > 0 {
		if err := a.writeLayout(reply.UILayout); err != nil {
			// 设置已经换掉、AI 配置与快捷键也已落盘：布局没写进去只是少了一项偏好，如实说出来，不让整次导入显示成失败。
			result.Warnings = append(result.Warnings, "界面布局未能导入："+err.Error())
		}
	}
	return result, nil
}

// writeSettings 把导入的设置盖到本机设置上（渲染层的 writeImportedLocalStorage + settings:set 的对应物）。
func (a configAdapter) writeSettings(reply applyReply) error {
	if a.settings == nil {
		return errors.New("设置服务不可用")
	}
	version := settings.PersistedVersion
	if reply.SettingsVersion != nil {
		version = *reply.SettingsVersion
	}
	var convertErr error
	_, err := a.settings.Update(func(v *settings.Values) {
		next, err := settings.ImportedValues(*v, reply.Settings, version)
		if err != nil {
			convertErr = err
			return
		}
		*v = next
	})
	if convertErr != nil {
		err = convertErr
	}
	if err != nil {
		if reply.SnapshotDir != "" {
			return fmt.Errorf("导入的设置没能写入：%w。导入前的配置已备份在 %s", err, reply.SnapshotDir)
		}
		return fmt.Errorf("导入的设置没能写入：%w", err)
	}
	return nil
}

func (a configAdapter) writeLayout(layout map[string]json.RawMessage) error {
	if a.prefs == nil {
		return errors.New("偏好服务不可用")
	}
	var errs []error
	for key, raw := range settings.ImportedPrefs(layout) {
		if err := a.prefs.Set(key, raw); err != nil {
			errs = append(errs, fmt.Errorf("%s：%w", key, err))
		}
	}
	return errors.Join(errs...)
}

// ── 备份调度 ──

// watchBackupSchedule 让自动备份任务跟着设置走：备份开关、间隔或后台任务开关变化时重新同步调度。
// 渲染层靠 backupRepository:syncRendererSettings 触发这次同步；原生界面没有那条推送，设置服务的订阅就是它的对应物。
// 保留数量不影响调度（每次任务运行时才读），所以不在比较之内。返回取消函数。
func watchBackupSchedule(set *nativeSettings, lib *Library) (cancel func()) {
	type schedule struct {
		auto       bool
		hours      int
		background bool
	}
	pick := func(v settings.Values) schedule {
		return schedule{auto: v.App.BackupAutoEnabled, hours: v.App.BackupIntervalHours, background: v.App.BackgroundTasksEnabled}
	}
	var last schedule
	if v, err := set.Snapshot(); err == nil {
		last = pick(v)
	}
	var mu sync.Mutex
	return set.Subscribe(func(v settings.Values) {
		next := pick(v)
		mu.Lock()
		changed := next != last
		last = next
		mu.Unlock()
		if changed {
			go lib.syncRepositoryBackupJob(context.Background())
		}
	})
}
