package main

// 设置页「采集与转写」「MCP 设置」分区的主包适配器（native/services/capture_tools.go、mcp.go）。
//
// 与 native_settings_system.go、native_settings_data.go 同一做法：经 Bridge.call 调已登记的 IPC 处理器（与渲染进程同一份逻辑、
// 同一套错误），再把 JSON 结果解码成 DTO；引擎安装、校验和、选择器登记（rememberPickedBinaryPath）与 SSRF 防护都在处理器一侧，
// 这里不绕过、不复刻。对应关系由 native_settings_capture_mcp_test.go 用真实处理器与临时目录守住。
//
//	CaptureTools.ToolStatus / CheckToolUpdate      → ytdlp:status / ytdlp:checkUpdate、ffmpeg:status / ffmpeg:checkUpdate
//	CaptureTools.InstallTool / RemoveTool          → ytdlp:install / ytdlp:remove、ffmpeg:install / ffmpeg:remove
//	CaptureTools.PickToolBinary                    → ytdlp:pickBinary / ffmpeg:pickBinary（主进程登记选中的路径，之后才能存进设置）
//	CaptureTools.OnToolProgress                    → 推送 ytdlp:downloadProgress / ffmpeg:downloadProgress
//	CaptureTools.FunasrStatus / CheckFunasrUpdate  → funasr:status / funasr:checkUpdate
//	CaptureTools.InstallFunasr / UpdateFunasr      → funasr:install / funasr:update
//	CaptureTools.UninstallFunasr                   → funasr:uninstall
//	CaptureTools.OnFunasrProgress                  → 推送 funasr:installProgress
//	CaptureTools.PlatformStatuses                  → platformCapture:status
//	CaptureTools.PlatformLogin / CancelLogin       → platformCapture:login / platformCapture:cancelLogin
//	CaptureTools.PlatformLogout / PlatformClearAll → platformCapture:logout / platformCapture:clearAll
//	MCP.Config / Scope / SetScope / Install        → mcp:config / mcp:getScope / mcp:setScope / mcp:install
//
// 自定义可执行文件路径（ytDlpPath、ffmpegPath）不在这里：它们是 settings.App 的字段，经 services.Settings 写入，
// 非空值只接受 PickToolBinary 返回过的路径。MCP 的环境变量、平台登录凭证只作为处理器的结果经过这里：不进错误文案、不进日志。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"guizhi-mygo-shell/native/services"
)

type captureToolsAdapter struct {
	b   *Bridge
	hub *pushHub
}

var _ services.CaptureTools = captureToolsAdapter{}

// toolChannel 是二进制引擎在 IPC 里的命名空间：ytdlp 与 ffmpeg 的频道名只差这一段。
func toolChannel(tool services.ManagedTool, op string) (string, error) {
	switch tool {
	case services.ToolYtDlp, services.ToolFfmpeg:
		return string(tool) + ":" + op, nil
	}
	return "", errors.New("unknown managed tool: " + string(tool))
}

// installReply 是 ytdlp:install / ffmpeg:install / funasr:install / funasr:update / funasr:uninstall 的回复：
// 失败时 success 为 false 并带 error 原文（处理器内部已兜住业务异常，不抛）。
type installReply struct {
	Success bool   `json:"success"`
	Version string `json:"version"`
	Warning string `json:"warning"`
	Error   string `json:"error"`
}

// err 把失败的回复变成带原因的错误：处理器没给原因时补一条兜底，不让界面拿到既失败又没有原因的结果。
func (r installReply) err(fallback string) error {
	if r.Success {
		return nil
	}
	if strings.TrimSpace(r.Error) == "" {
		return errors.New(fallback)
	}
	return errors.New(r.Error)
}

func (a captureToolsAdapter) ToolStatus(ctx context.Context, tool services.ManagedTool, force bool) (services.ToolStatus, error) {
	ch, err := toolChannel(tool, "status")
	if err != nil {
		return services.ToolStatus{}, err
	}
	return callAs[services.ToolStatus](ctx, a.b, ch, force)
}

func (a captureToolsAdapter) CheckToolUpdate(ctx context.Context, tool services.ManagedTool) (services.ToolUpdateCheck, error) {
	ch, err := toolChannel(tool, "checkUpdate")
	if err != nil {
		return services.ToolUpdateCheck{}, err
	}
	return callAs[services.ToolUpdateCheck](ctx, a.b, ch)
}

func (a captureToolsAdapter) InstallTool(ctx context.Context, tool services.ManagedTool) (string, error) {
	ch, err := toolChannel(tool, "install")
	if err != nil {
		return "", err
	}
	reply, err := callAs[installReply](ctx, a.b, ch)
	if err != nil {
		return "", err
	}
	if err := reply.err("install failed"); err != nil {
		return "", err
	}
	return reply.Version, nil
}

func (a captureToolsAdapter) RemoveTool(ctx context.Context, tool services.ManagedTool) error {
	ch, err := toolChannel(tool, "remove")
	if err != nil {
		return err
	}
	_, err = a.b.call(ctx, ch)
	return err
}

func (a captureToolsAdapter) PickToolBinary(ctx context.Context, tool services.ManagedTool) (string, error) {
	ch, err := toolChannel(tool, "pickBinary")
	if err != nil {
		return "", err
	}
	return callAs[string](ctx, a.b, ch) // 取消或没有选择器时处理器回 null：解成空串
}

// decodePayload 把推送载荷（处理器侧的结构体或 map）按 JSON 往返解成 DTO；解不出的推送视为无效。
func decodePayload[T any](payload any) (T, bool) {
	var out T
	raw, err := json.Marshal(payload)
	if err != nil || json.Unmarshal(raw, &out) != nil {
		var zero T
		return zero, false
	}
	return out, true
}

func (a captureToolsAdapter) OnToolProgress(tool services.ManagedTool, fn func(services.ToolProgress)) func() {
	ch := chYtDlpProgress
	if tool == services.ToolFfmpeg {
		ch = chFfmpegProgress
	}
	if a.hub == nil {
		return func() {}
	}
	return a.hub.Subscribe(ch, func(payload any) {
		if p, ok := decodePayload[services.ToolProgress](payload); ok {
			fn(p)
		}
	})
}

func (a captureToolsAdapter) FunasrStatus(ctx context.Context, force bool) (services.FunasrStatus, error) {
	return callAs[services.FunasrStatus](ctx, a.b, "funasr:status", force)
}

func (a captureToolsAdapter) CheckFunasrUpdate(ctx context.Context) (services.ToolUpdateCheck, error) {
	return callAs[services.ToolUpdateCheck](ctx, a.b, "funasr:checkUpdate")
}

func (a captureToolsAdapter) InstallFunasr(ctx context.Context) (string, error) {
	reply, err := callAs[installReply](ctx, a.b, "funasr:install")
	if err != nil {
		return "", err
	}
	if err := reply.err("install failed"); err != nil {
		return "", err
	}
	return reply.Version, nil
}

func (a captureToolsAdapter) UpdateFunasr(ctx context.Context, version string) (services.FunasrUpdateResult, error) {
	reply, err := callAs[installReply](ctx, a.b, "funasr:update", version)
	if err != nil {
		return services.FunasrUpdateResult{}, err
	}
	if err := reply.err("update failed"); err != nil {
		return services.FunasrUpdateResult{}, err
	}
	return services.FunasrUpdateResult{Version: reply.Version, Warning: reply.Warning}, nil
}

func (a captureToolsAdapter) UninstallFunasr(ctx context.Context) error {
	reply, err := callAs[installReply](ctx, a.b, "funasr:uninstall")
	if err != nil {
		return err
	}
	return reply.err("uninstall failed")
}

func (a captureToolsAdapter) OnFunasrProgress(fn func(services.FunasrProgress)) func() {
	if a.hub == nil {
		return func() {}
	}
	return a.hub.Subscribe(chFunasrProgress, func(payload any) {
		if p, ok := decodePayload[services.FunasrProgress](payload); ok && p.Phase != "" {
			fn(p)
		}
	})
}

func (a captureToolsAdapter) PlatformStatuses(ctx context.Context) ([]services.PlatformStatus, error) {
	return callAs[[]services.PlatformStatus](ctx, a.b, "platformCapture:status")
}

func (a captureToolsAdapter) PlatformLogin(ctx context.Context, platform string, forceRelogin bool) (services.PlatformStatus, error) {
	return callAs[services.PlatformStatus](ctx, a.b, "platformCapture:login", map[string]any{"platform": platform, "forceRelogin": forceRelogin})
}

func (a captureToolsAdapter) PlatformCancelLogin(ctx context.Context, platform string) (bool, error) {
	return callAs[bool](ctx, a.b, "platformCapture:cancelLogin", platform)
}

func (a captureToolsAdapter) PlatformLogout(ctx context.Context, platform string) error {
	_, err := a.b.call(ctx, "platformCapture:logout", platform)
	return err
}

func (a captureToolsAdapter) PlatformClearAll(ctx context.Context) error {
	_, err := a.b.call(ctx, "platformCapture:clearAll")
	return err
}

// ── MCP ──

type mcpAdapter struct{ b *Bridge }

var _ services.MCP = mcpAdapter{}

func (a mcpAdapter) Config(ctx context.Context) (services.MCPConfig, error) {
	cfg, err := callAs[services.MCPConfig](ctx, a.b, "mcp:config")
	if err != nil {
		return services.MCPConfig{}, err
	}
	if cfg.Args == nil {
		cfg.Args = []string{}
	}
	if cfg.Env == nil {
		cfg.Env = map[string]string{}
	}
	return cfg, nil
}

// normalizeMCPScope 给解码后的范围补上空列表：界面与保存都不该碰 nil 切片（序列化成 null 会被处理器当作「不是数组」）。
func normalizeMCPScope(s services.MCPScope) services.MCPScope {
	if s.AllowedCollectionIDs == nil {
		s.AllowedCollectionIDs = []string{}
	}
	return s
}

func (a mcpAdapter) Scope(ctx context.Context) (services.MCPScope, error) {
	scope, err := callAs[services.MCPScope](ctx, a.b, "mcp:getScope")
	return normalizeMCPScope(scope), err
}

func (a mcpAdapter) SetScope(ctx context.Context, scope services.MCPScope) (services.MCPScope, error) {
	saved, err := callAs[services.MCPScope](ctx, a.b, "mcp:setScope", normalizeMCPScope(scope))
	return normalizeMCPScope(saved), err
}

// mcpInstallReply 是 mcp:install 的回复：失败时 success 为 false 并带 error 原文（含被拒绝改动的文件路径）。
type mcpInstallReply struct {
	Success  bool   `json:"success"`
	FilePath string `json:"filePath"`
	Replaced bool   `json:"replaced"`
	Error    string `json:"error"`
}

func (a mcpAdapter) Install(ctx context.Context, client services.MCPClient) (services.MCPInstallResult, error) {
	reply, err := callAs[mcpInstallReply](ctx, a.b, "mcp:install", string(client))
	if err != nil {
		return services.MCPInstallResult{}, err
	}
	if !reply.Success {
		if strings.TrimSpace(reply.Error) == "" {
			return services.MCPInstallResult{}, errors.New("install failed")
		}
		return services.MCPInstallResult{}, errors.New(reply.Error)
	}
	return services.MCPInstallResult{FilePath: reply.FilePath, Replaced: reply.Replaced}, nil
}
