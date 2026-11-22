package main

// 设置页「快捷键」「网络与 FlareSolverr」「关于」三个分区的系统能力适配器（native/services/settings_system.go）。
//
// 与 native_adapters.go 的其余适配器同一做法：经 Bridge.call 调已登记的 IPC 处理器（与渲染进程同一份逻辑、同一套错误），
// 再把 JSON 结果解码成 DTO；行为只有一个来源，对应关系由 native_settings_system_test.go 用真实处理器守住。
//
//	Shortcuts.Get      → shortcuts:get + shortcuts:getMode
//	Shortcuts.Save     → shortcuts:set（返回 false 即系统拒绝注册，对应 ErrShortcutNotSaved）
//	Shortcuts.SetModes → shortcuts:setMode（单向频道，处理器自己记写失败的日志）
//	FlareSolverr.Check → web:solverCheck
//	Updater.*          → updater:check / updater:download / updater:install / updater:openReleases，状态来自 updater:status 推送
//
// 代理口令只存在于 settings.Values（经 Settings 服务读写），这里不接触，也不进任何日志。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/settings"
)

// ── 快捷键 ──

type shortcutsAdapter struct{ b *Bridge }

var _ services.Shortcuts = shortcutsAdapter{}

// stringTable 只留下取值是字符串的项：配置文件被手改成别的类型时，那一项当作没有，由界面用默认值兜底。
func stringTable(in map[string]any) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}

func (a shortcutsAdapter) Get(ctx context.Context) (services.ShortcutConfig, error) {
	acc, err := callAs[map[string]any](ctx, a.b, chShortcutsGet)
	if err != nil {
		return services.ShortcutConfig{}, err
	}
	modes, err := callAs[map[string]any](ctx, a.b, chShortcutsGetMode)
	if err != nil {
		return services.ShortcutConfig{}, err
	}
	return services.ShortcutConfig{Accelerators: stringTable(acc), Modes: stringTable(modes)}, nil
}

// orderedTable 把动作表按「已知动作在前、其余按名字排序」的顺序编码：处理器按键的出现顺序注册系统级快捷键并写配置文件，
// 渲染层发的是 { showApp, newItem, search, settings } 的顺序，这里保持一致（Go 的 map 编码会按字母序打乱）。
func orderedTable(table map[string]string) *omap {
	out := newOmap()
	seen := map[string]bool{}
	for _, k := range services.ShortcutActions {
		if v, ok := table[k]; ok {
			out.set(k, v)
			seen[k] = true
		}
	}
	var rest []string
	for k := range table {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	for _, k := range rest {
		out.set(k, table[k])
	}
	return out
}

func (a shortcutsAdapter) Save(ctx context.Context, accelerators map[string]string) error {
	saved, err := callAs[bool](ctx, a.b, chShortcutsSet, orderedTable(accelerators))
	if err != nil {
		return err
	}
	if !saved {
		return services.ErrShortcutNotSaved
	}
	return nil
}

func (a shortcutsAdapter) SetModes(_ context.Context, modes map[string]string) error {
	encoded, err := json.Marshal([]any{orderedTable(modes)})
	if err != nil {
		return fmt.Errorf("编码快捷键模式失败：%w", err)
	}
	a.b.Send(chShortcutsSetMode, string(encoded))
	return nil
}

// ── FlareSolverr 连通性测试 ──

type solverAdapter struct{ b *Bridge }

var _ services.FlareSolverr = solverAdapter{}

// webReply 是 web:* 处理器的统一回复形状（webResponse）：成功 { ok:true, data }，失败 { ok:false, error }。
type webReply struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
	Data  struct {
		Connected bool `json:"connected"`
	} `json:"data"`
}

func (a solverAdapter) Check(ctx context.Context, cfg settings.FlareSolverr) error {
	reply, err := callAs[webReply](ctx, a.b, "web:solverCheck", cfg)
	if err != nil {
		return err
	}
	if !reply.OK {
		if reply.Error == "" {
			return errors.New("FlareSolverr connection failed")
		}
		return errors.New(reply.Error)
	}
	if !reply.Data.Connected {
		return errors.New("FlareSolverr connection failed")
	}
	return nil
}

// ── 更新 ──

type updaterAdapter struct {
	b   *Bridge
	hub *pushHub
}

var _ services.Updater = updaterAdapter{}

// updaterReply 是 updater:check / download / install 的回复：失败时 success 为 false 并带 error 原文。
type updaterReply struct {
	Success         bool   `json:"success"`
	DevDisabled     bool   `json:"devDisabled"`
	Error           string `json:"error"`
	UpdateAvailable bool   `json:"updateAvailable"`
	Result          struct {
		UpdateInfo struct {
			Version string `json:"version"`
		} `json:"updateInfo"`
	} `json:"result"`
}

// err 把失败的回复变成带原因的错误：处理器没给原因时补一条兜底，不让界面拿到既失败又没有原因的结果。
func (r updaterReply) err(fallback string) error {
	if r.Success {
		return nil
	}
	if strings.TrimSpace(r.Error) == "" {
		return errors.New(fallback)
	}
	return errors.New(r.Error)
}

func updaterOptions(o services.UpdateCheckOptions) map[string]any {
	channel := string(o.Channel)
	if channel != string(settings.ChannelPreview) {
		channel = string(settings.ChannelStable)
	}
	return map[string]any{"useMirror": o.UseMirror, "channel": channel, "trigger": "manual"}
}

func (a updaterAdapter) Check(ctx context.Context, opts services.UpdateCheckOptions) (services.UpdateCheckResult, error) {
	reply, err := callAs[updaterReply](ctx, a.b, "updater:check", updaterOptions(opts))
	if err != nil {
		return services.UpdateCheckResult{}, err
	}
	if err := reply.err("update check failed"); err != nil {
		return services.UpdateCheckResult{}, err
	}
	return services.UpdateCheckResult{Available: reply.UpdateAvailable, Version: reply.Result.UpdateInfo.Version}, nil
}

func (a updaterAdapter) Download(ctx context.Context, opts services.UpdateCheckOptions) error {
	reply, err := callAs[updaterReply](ctx, a.b, "updater:download", updaterOptions(opts))
	if err != nil {
		return err
	}
	return reply.err("download update failed")
}

func (a updaterAdapter) Install(ctx context.Context) error {
	reply, err := callAs[updaterReply](ctx, a.b, "updater:install")
	if err != nil {
		return err
	}
	return reply.err("install update failed")
}

func (a updaterAdapter) OpenReleases() error {
	_, err := a.b.call(context.Background(), "updater:openReleases")
	return err
}

// updaterStatusWire 是 updater:status 推送的载荷：{ status, info?:{version}, progress?:{percent}, error? }。
type updaterStatusWire struct {
	Status string `json:"status"`
	Info   *struct {
		Version string `json:"version"`
	} `json:"info"`
	Progress *struct {
		Percent float64 `json:"percent"`
	} `json:"progress"`
	Error string `json:"error"`
}

func (w updaterStatusWire) status() services.UpdateStatus {
	st := services.UpdateStatus{State: w.Status, Error: w.Error}
	if w.Info != nil {
		st.Version = w.Info.Version
	}
	if w.Progress != nil {
		st.Percent = w.Progress.Percent
	}
	return st
}

func (a updaterAdapter) OnStatus(fn func(services.UpdateStatus)) func() {
	if a.hub == nil {
		return func() {}
	}
	return a.hub.Subscribe("updater:status", func(payload any) {
		raw, err := json.Marshal(payload)
		if err != nil {
			return
		}
		var wire updaterStatusWire
		if json.Unmarshal(raw, &wire) != nil || wire.Status == "" {
			return // 载荷不是状态对象：忽略，不能让一条坏推送把界面带进错误态
		}
		fn(wire.status())
	})
}
