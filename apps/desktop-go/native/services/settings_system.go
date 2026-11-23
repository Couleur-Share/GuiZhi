package services

import (
	"context"
	"errors"

	"guizhi-mygo-shell/native/settings"
)

// 设置页「网络与 FlareSolverr」「快捷键」「关于」三个分区依赖的系统能力。
//
// 代理与 FlareSolverr 的配置本身就是 settings.Values 的字段，经 Settings.Update 读写；
// 这里只是配置之外的那几个动作：快捷键的读写（它们有自己的配置文件）、FlareSolverr 的连通性测试、检查与下载更新。

// 快捷键动作与模式的取值（与 config/shortcuts.json、shortcut-mode.json 一致）。
const (
	ShortcutShowApp  = "showApp"
	ShortcutNewItem  = "newItem"
	ShortcutSearch   = "search"
	ShortcutSettings = "settings"

	ShortcutModeGlobal = "global"
	ShortcutModeLocal  = "local"
)

// ShortcutActions 是设置页展示的快捷键动作，顺序与渲染层 ShortcutsSettings 一致。
var ShortcutActions = []string{ShortcutShowApp, ShortcutNewItem, ShortcutSearch, ShortcutSettings}

// DefaultShortcuts 是各动作的默认组合键（主进程 DEFAULT_SHORTCUTS，渲染层读到真实配置前先显示它，避免闪错值）。
func DefaultShortcuts() map[string]string {
	return map[string]string{
		ShortcutShowApp: "Alt+Shift+P", ShortcutNewItem: "Alt+Shift+N",
		ShortcutSearch: "Alt+Shift+F", ShortcutSettings: "Alt+Shift+S",
	}
}

// DefaultShortcutModes 是各动作的默认模式（showApp 是系统级，其余只在应用内响应）。
func DefaultShortcutModes() map[string]string {
	return map[string]string{
		ShortcutShowApp: ShortcutModeGlobal, ShortcutNewItem: ShortcutModeLocal,
		ShortcutSearch: ShortcutModeLocal, ShortcutSettings: ShortcutModeLocal,
	}
}

// ShortcutConfig 是快捷键配置：动作 → 组合键（空串表示用户清掉了它），动作 → 模式。
type ShortcutConfig struct {
	Accelerators map[string]string
	Modes        map[string]string
}

// ErrShortcutNotSaved 表示主进程没有把快捷键写进配置文件（shortcuts:set 返回 false）。
// 渲染层此时回滚并提示「系统拒绝注册该快捷键」，界面据此选文案。
var ErrShortcutNotSaved = errors.New("快捷键没有保存成功")

// Shortcuts 是快捷键配置的读写（shortcuts:get / shortcuts:getMode / shortcuts:set / shortcuts:setMode）。
type Shortcuts interface {
	// Get 返回当前生效的组合键与模式；读不到（处理器出错）返回错误，界面保留默认值并提示。
	Get(ctx context.Context) (ShortcutConfig, error)
	// Save 保存全部动作的组合键并让主进程重新注册系统级快捷键；没写进去返回 ErrShortcutNotSaved。
	Save(ctx context.Context, accelerators map[string]string) error
	// SetModes 保存全部动作的模式（渲染层 setShortcutMode 每次发整张表）。
	SetModes(ctx context.Context, modes map[string]string) error
}

// FlareSolverr 是 FlareSolverr 服务的连通性测试（web:solverCheck）。
type FlareSolverr interface {
	// Check 用 cfg 连接并列出会话：连通返回 nil；配置无效、服务没启动、SSH 失败等返回带原因的错误。
	// 不改动已保存的设置（保存走 Settings.Update）。
	Check(ctx context.Context, cfg settings.FlareSolverr) error
}

// UpdateCheckOptions 是检查与下载更新的参数（updater:check / updater:download 的入参）。
type UpdateCheckOptions struct {
	// UseMirror 为真时走镜像源（设置里的 useUpdateMirror）。
	UseMirror bool
	// Channel 是更新通道；空按稳定版。
	Channel settings.UpdateChannel
	// Trigger 是检查的触发来源，写进更新日志：UpdateTriggerStartup / Interval / Visibility 是自动检查，空串是手动。
	Trigger string
}

// 自动检查更新的触发来源（UpdateCheckOptions.Trigger；手动检查留空）。
const (
	UpdateTriggerStartup    = "startup"
	UpdateTriggerInterval   = "interval"
	UpdateTriggerVisibility = "visibility"
)

// UpdateDisabledError 表示这次检查被主进程禁用（开发模式），不是检查失败：自动检查据此既不记失败也不提示。
type UpdateDisabledError struct{ Reason string }

func (e *UpdateDisabledError) Error() string { return e.Reason }

// UpdateCheckResult 是一次检查更新的结果。
type UpdateCheckResult struct {
	// Available 为真表示有比当前版本更新的版本。
	Available bool
	// Version 是服务端最新版本号（没有可用版本信息时为空）。
	Version string
}

// 更新状态的取值（updater:status 推送的 status 字段）。
const (
	UpdateStateChecking     = "checking"
	UpdateStateAvailable    = "available"
	UpdateStateNotAvailable = "not-available"
	UpdateStateDownloading  = "downloading"
	UpdateStateDownloaded   = "downloaded"
	UpdateStateError        = "error"
)

// UpdateStatus 是主进程推送的一次更新状态（updater:status）。
type UpdateStatus struct {
	State string
	// Version 在 available / not-available / downloaded 时是对应版本号。
	Version string
	// Percent 在 downloading 时是 0–100 的下载进度。
	Percent float64
	// Error 在 error 时是原因。
	Error string
	// ReleaseNotes 与 ReleaseDate 在 available / downloaded 时是更新说明（Markdown）与发布日期（原样字符串），可能为空。
	ReleaseNotes string
	ReleaseDate  string
}

// Updater 是应用更新（updater:*）。检查与下载是同步调用：返回时已有结果；进度与状态另经 OnStatus 推送。
type Updater interface {
	// Check 检查更新。失败（含镜像源全挂）返回带原因的错误；开发模式被禁用时返回 *UpdateDisabledError（errors.As 可辨认）。
	Check(ctx context.Context, opts UpdateCheckOptions) (UpdateCheckResult, error)
	// Download 下载已检查到的更新；失败返回带原因的错误。
	Download(ctx context.Context, opts UpdateCheckOptions) error
	// Install 运行已下载的安装包并退出应用；失败返回带原因的错误。
	Install(ctx context.Context) error
	// OpenReleases 用系统浏览器打开发布页。
	OpenReleases() error
	// OpenDownloaded 在资源管理器里显示已下载的安装包（没有下载过或文件丢失时打开下载目录）；
	// 没有可用文件或打开失败返回带原因的错误（updater:openDownloadedUpdate）。
	OpenDownloaded() error
	// LogAutoSkip 记下一次自动检查被跳过的原因（updater:logAutoSkip），没有它「没检查」与「检查了没有新版本」无法区分；
	// 只写日志，失败也不影响界面，所以没有返回值。trigger 取 UpdateTrigger*，reason 取 disabled / hidden / offline / in-flight / cooldown。
	LogAutoSkip(trigger, reason string)
	// OnStatus 订阅更新状态推送（回调在推送方的 goroutine 上，必须很快返回）；返回取消函数，可重复调用。
	OnStatus(fn func(UpdateStatus)) (cancel func())
}
