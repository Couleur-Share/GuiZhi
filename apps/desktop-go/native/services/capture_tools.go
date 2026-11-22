package services

import (
	"context"
	"fmt"
	"strings"
)

// 设置页「采集与转写」分区依赖的系统能力：yt-dlp / ffmpeg 两个二进制引擎、本地转写引擎（FunASR）与平台登录账号。
//
// 对应主进程的 ytdlp:* / ffmpeg:* / funasr:* / platformCapture:* 频道；界面（viva/screens/settings）只依赖本接口，
// 适配器（package main 的 native_settings_capture_mcp.go）把它接到现有处理器，行为与渲染层同一个来源。
// 自定义可执行文件路径本身是 settings.App 的 YtDlpPath / FfmpegPath，经 Settings.Update 读写：
// 非空值只接受 PickToolBinary 返回过的路径（主进程登记，Settings 实现校验），界面不能手输。

// ManagedTool 是应用内可安装的二进制引擎。
type ManagedTool string

const (
	ToolYtDlp  ManagedTool = "ytdlp"
	ToolFfmpeg ManagedTool = "ffmpeg"
)

// 二进制引擎当前生效版本的来源（ToolStatus.Source）。
const (
	ToolSourceManaged = "managed" // 应用内置版
	ToolSourcePath    = "path"    // 系统 PATH
	ToolSourceCustom  = "custom"  // 用户指定的路径
)

// 平台账号的取值（platformCapture:* 的 platform 参数）。
const (
	PlatformXiaohongshu = "xiaohongshu"
	PlatformDouyin      = "douyin"
	PlatformLinuxdo     = "linuxdo"
)

// ToolStatus 是一个二进制引擎的探测结果（toolStatus）。
type ToolStatus struct {
	Installed bool `json:"installed"`
	// Source 是生效版本的来源，见 ToolSource*；没有可用版本时为空。
	Source string `json:"source"`
	// Version 是探测到的版本串（yt-dlp 的发布日、ffmpeg 的构建标识）。
	Version string `json:"version"`
	// Path 是生效的可执行文件路径；来源是系统 PATH 时只是命令名。
	Path string `json:"path"`
	// ManagedPath 是内置版的安装位置（即使还没装）。
	ManagedPath string `json:"managedPath"`
	// InstallSupported 为假表示当前平台不提供应用内安装。
	InstallSupported bool `json:"installSupported"`
	// InstallHintCommand 在不支持应用内安装时给出推荐的终端命令（可为空）。
	InstallHintCommand string `json:"installHintCommand"`
}

// ToolUpdateCheck 是一次远端版本比对的结果（toolUpdateCheck）。
type ToolUpdateCheck struct {
	Current string `json:"current"`
	// Latest 为空表示没能取到远端最新版本（网络或代理问题），调用方按「检查失败」处理。
	Latest          string `json:"latest"`
	UpdateAvailable bool   `json:"updateAvailable"`
}

// ToolProgress 是二进制引擎下载进度的一次推送（toolDownloadProgress）。
type ToolProgress struct {
	Transferred int64 `json:"transferred"`
	// Total 是总字节数；0 表示服务端没给总量，只能报已下载量。
	Total int64 `json:"total"`
}

// FunasrStatus 是本地转写引擎的状态（funasrStatus）。
type FunasrStatus struct {
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
	Port      int    `json:"port"`
	Dir       string `json:"dir"`
	Version   string `json:"version"`
	// InstallSupported、UpdateSupported 为假表示当前平台不提供应用内安装 / 更新。
	InstallSupported bool `json:"installSupported"`
	UpdateSupported  bool `json:"updateSupported"`
	// InstallFlavor 是安装形态：空或 python 为 SenseVoice（Windows），gguf 为 FunASR GGUF（不支持说话人分离）。
	InstallFlavor string `json:"installFlavor"`
}

// FunasrPhase 的取值（FunasrProgress.Phase）。
const (
	FunasrPhasePrepare  = "prepare"
	FunasrPhaseBackup   = "backup"
	FunasrPhaseRuntime  = "runtime"
	FunasrPhaseDeps     = "deps"
	FunasrPhaseModels   = "models"
	FunasrPhaseVerify   = "verify"
	FunasrPhaseRollback = "rollback"
)

// FunasrFlavorGguf 是 FunasrStatus.InstallFlavor 里的 GGUF 形态。
const FunasrFlavorGguf = "gguf"

// FunasrProgress 是本地转写引擎安装 / 更新进度的一次推送（funasrInstallProgress）。
type FunasrProgress struct {
	Phase string `json:"phase"`
	// Percent 是 0–100；nil 表示这个阶段没有总量，不能推算百分比。
	Percent *int `json:"percent"`
	// Detail 是运行详情（脚本输出的末尾），可能含本机路径，不进日志。
	Detail string `json:"detail"`
}

// FunasrUpdateResult 是更新成功的结果；Warning 非空表示更新完成但有需要用户知道的事（如旧备份没清理）。
type FunasrUpdateResult struct {
	Version string
	Warning string
}

// PlatformStatus 是一个平台账号的登录状态（PlatformSessionStatus）。
type PlatformStatus struct {
	Platform       string `json:"platform"`
	Browser        string `json:"browser"`
	BrowserVersion string `json:"browserVersion"`
	// Available 为假表示内置登录窗口暂不可用。
	Available bool `json:"available"`
	LoggedIn  bool `json:"loggedIn"`
	// Busy 为真表示该平台正在登录或采集。
	Busy      bool   `json:"busy"`
	ErrorCode string `json:"errorCode"`
}

// CaptureTools 是「采集与转写」分区的全部系统动作。失败一律返回带原因的错误（安装失败的 { success:false, error } 也转成错误）。
type CaptureTools interface {
	// ToolStatus 探测引擎；force 为真绕过主进程的探测缓存重新探测。
	ToolStatus(ctx context.Context, tool ManagedTool, force bool) (ToolStatus, error)
	// CheckToolUpdate 比对远端最新版本；只有当前跑在内置版上才有 Current。
	CheckToolUpdate(ctx context.Context, tool ManagedTool) (ToolUpdateCheck, error)
	// InstallTool 下载并安装（或更新）内置版，返回安装的版本；进度另经 OnToolProgress 推送。
	InstallTool(ctx context.Context, tool ManagedTool) (version string, err error)
	// RemoveTool 移除内置版。
	RemoveTool(ctx context.Context, tool ManagedTool) error
	// PickToolBinary 弹出文件选择器，返回选中的路径并让主进程登记（之后才能存进设置）；用户取消返回空串。
	PickToolBinary(ctx context.Context, tool ManagedTool) (path string, err error)
	// OnToolProgress 订阅内置版下载进度（回调在推送方的 goroutine 上，必须很快返回）；返回取消函数，可重复调用。
	OnToolProgress(tool ManagedTool, fn func(ToolProgress)) (cancel func())

	// FunasrStatus 探测本地转写引擎；force 为真绕过缓存。
	FunasrStatus(ctx context.Context, force bool) (FunasrStatus, error)
	// CheckFunasrUpdate 比对远端最新版本。
	CheckFunasrUpdate(ctx context.Context) (ToolUpdateCheck, error)
	// InstallFunasr 安装本地转写引擎（几十分钟的长任务，不随 ctx 取消），成功后主进程已把内置模型接进「语音转写」路由。
	InstallFunasr(ctx context.Context) (version string, err error)
	// UpdateFunasr 更新到 version（分阶段，失败时主进程已回滚到旧版本）。
	UpdateFunasr(ctx context.Context, version string) (FunasrUpdateResult, error)
	// UninstallFunasr 卸载引擎并移除对应路由。
	UninstallFunasr(ctx context.Context) error
	// OnFunasrProgress 订阅安装 / 更新进度；约定同 OnToolProgress。
	OnFunasrProgress(fn func(FunasrProgress)) (cancel func())

	// PlatformStatuses 返回各平台账号的登录状态。
	PlatformStatuses(ctx context.Context) ([]PlatformStatus, error)
	// PlatformLogin 打开内置登录窗口并等待结果（可能几分钟），返回登录后的状态；forceRelogin 先清掉旧会话。
	PlatformLogin(ctx context.Context, platform string, forceRelogin bool) (PlatformStatus, error)
	// PlatformCancelLogin 取消正在进行的登录；没有可取消的登录返回 false。
	PlatformCancelLogin(ctx context.Context, platform string) (bool, error)
	// PlatformLogout 退出一个平台账号。
	PlatformLogout(ctx context.Context, platform string) error
	// PlatformClearAll 清除全部平台登录数据（只删内置登录会话，不碰知识库与日常浏览器）。
	PlatformClearAll(ctx context.Context) error
}

// FormatEngineVersion 把引擎版本标识统一成日期展示（渲染层 formatEngineVersion）：
// yt-dlp 的 2026.07.04 与 ffmpeg 每日构建尾部的 20260724 都归成 2026-07-04；拿不到日期的（发行版 8.1-essentials…、nightly）退回主版本号或原串。
func FormatEngineVersion(version string) string {
	v := strings.TrimSpace(version)
	if len(v) == 10 && v[4] == '.' && v[7] == '.' && allDigits(v[0:4]) && allDigits(v[5:7]) && allDigits(v[8:10]) {
		return v[0:4] + "-" + v[5:7] + "-" + v[8:10]
	}
	// ffmpeg 每日构建：整串就是 8 位日期，或以 -8 位日期结尾。
	compact := v
	if i := strings.LastIndex(v, "-"); i >= 0 {
		compact = v[i+1:]
	}
	if len(compact) == 8 && allDigits(compact) {
		return fmt.Sprintf("%s-%s-%s", compact[0:4], compact[4:6], compact[6:8])
	}
	head := v
	if i := strings.Index(v, "-"); i >= 0 {
		head = v[:i]
	}
	if head != "" && head[0] >= '0' && head[0] <= '9' {
		return head
	}
	return v
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
