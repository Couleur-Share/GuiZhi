package main

// 原生界面的设置服务：services.Settings 的实现。
//
// 一份 settings.Values 由两个存储合成（归属见 native/settings 包说明）：
//   - 界面设置（UI）在 config/ui-prefs.json（ui_prefs.go），备份还原不影响它；
//   - Go 侧设置（App）在 SQLite 的 settings 表，备份、代理、工具、托盘、通知等 Go 代码直接读它。
//
// 读取 App 时直接取表里的原始行，不经 settings:get：后者的默认值是 Electron 时代的（theme、updateChannel 等），
// 且会并入 AI 连接配置；这里要的是渲染层的默认值（native/settings 的 DefaultApp），也不该碰机密。
// 写入 App 经 Library.settingsSet：与渲染进程同一份校验（FlareSolverr、可执行文件路径白名单）、
// 同一个事务、同样地让网络代理即时生效——行为只有一个来源。
//
// The native UI's settings service: UI settings live in config/ui-prefs.json, Go-side settings in the settings table.

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/settings"
)

// nativeSettingsTimeout 是一次设置读写允许花的时间上限：库被长事务占住时给出错误，而不是让界面一直等。
const nativeSettingsTimeout = 15 * time.Second

// appSettingKeys 是 settings.App 在表里的键（均在 persistedSettingsKeys 白名单内，测试核对）。
var appSettingKeys = func() map[string]bool {
	raw, err := settings.Keys(settings.DefaultApp())
	if err != nil {
		panic(err) // 固定结构，不会失败
	}
	keys := make(map[string]bool, len(raw))
	for k := range raw {
		keys[k] = true
	}
	return keys
}()

type nativeSettings struct {
	lib   *Library
	store *uiPrefsStore
	logf  func(format string, args ...any)

	updateMu sync.Mutex // 串行化「读—改—写」，避免两次并发更新互相覆盖

	subMu   sync.Mutex
	nextSub int
	subs    map[int]func(settings.Values)
}

var (
	_ services.Settings = (*nativeSettings)(nil)
	_ services.Prefs    = (*uiPrefsStore)(nil)
)

// newNativeSettings 装配设置服务。logf 为 nil 时不记日志。
func newNativeSettings(lib *Library, store *uiPrefsStore, logf func(format string, args ...any)) *nativeSettings {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &nativeSettings{lib: lib, store: store, logf: logf, subs: map[int]func(settings.Values){}}
}

// toAny 经 JSON 往返得到 map/切片/标量，用来调用以 any 为入参的 Go 侧归一化函数。
func toAny(v any) any {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var out any
	_ = json.Unmarshal(raw, &out)
	return out
}

func proxyFromGo(p networkProxySettings) settings.NetworkProxy {
	return settings.NetworkProxy{Mode: p.Mode, Protocol: p.Protocol, Host: p.Host, Port: p.Port, Username: p.Username, Password: p.Password, Bypass: p.Bypass}
}

func flareFromGo(f flareSolverrSettings) settings.FlareSolverr {
	return settings.FlareSolverr{Enabled: f.Enabled, Connection: f.Connection, Port: f.Port, SSHHost: f.SSHHost}
}

// readApp 读取 Go 侧设置：表里没有的键取渲染层的默认值，类型不对的取默认值；代理与 FlareSolverr 经 Go 侧的归一化。
func (n *nativeSettings) readApp(ctx context.Context) (settings.App, error) {
	rows, err := n.lib.db.Query(ctx, "SELECT key, value FROM settings")
	if err != nil {
		return settings.App{}, err
	}
	raw := map[string]json.RawMessage{}
	for _, r := range rows.Values {
		key, _ := r[0].(string)
		text, isString := r[1].(string)
		if !appSettingKeys[key] || !isString {
			continue
		}
		if json.Valid([]byte(text)) {
			raw[key] = json.RawMessage(text)
		} else if quoted, err := json.Marshal(text); err == nil {
			raw[key] = quoted // 不是 JSON 的文本按字符串处理（与 settings:get 一致）
		}
	}
	app := settings.DecodeApp(raw)
	var proxy, flare any
	if v, ok := raw["networkProxy"]; ok {
		_ = json.Unmarshal(v, &proxy)
	}
	if v, ok := raw["flareSolverr"]; ok {
		_ = json.Unmarshal(v, &flare)
	}
	app.NetworkProxy = proxyFromGo(normalizeNetworkProxy(proxy))
	app.FlareSolverr = flareFromGo(normalizeFlareSolverr(flare))
	app.Normalize()
	return app, nil
}

// Snapshot 实现 services.Settings。
func (n *nativeSettings) Snapshot() (settings.Values, error) {
	ctx, cancel := context.WithTimeout(context.Background(), nativeSettingsTimeout)
	defer cancel()
	return n.snapshot(ctx)
}

func (n *nativeSettings) snapshot(ctx context.Context) (settings.Values, error) {
	app, err := n.readApp(ctx)
	if err != nil {
		return settings.Values{}, fmt.Errorf("读取设置失败：%w", err)
	}
	return settings.Values{UI: n.store.UI(), App: app}, nil
}

// validateAppChanges 在写入任何内容之前检查本次 App 变化：Go 侧的可执行文件路径白名单与 FlareSolverr 校验。
// settingsSet 对前者是静默忽略，对后者是整批拒绝；这里提前报错，界面才能把原因告诉用户，也避免先写了界面设置文件。
func validateAppChanges(changed map[string]json.RawMessage, next settings.App) error {
	for key, raw := range changed {
		if executablePathKeys[key] && !isAcceptableBinaryPath(raw) {
			return fmt.Errorf("%s 只接受通过文件选择器选定的路径", key)
		}
	}
	if _, touched := changed["flareSolverr"]; touched {
		if _, err := validateFlareSolverr(toAny(next.FlareSolverr)); err != nil {
			return err
		}
	}
	return nil
}

// Update 实现 services.Settings：先写界面设置文件，再经 settingsSet 写表；表写失败时把文件恢复原样。
func (n *nativeSettings) Update(fn func(*settings.Values)) (settings.Values, error) {
	ctx, cancel := context.WithTimeout(context.Background(), nativeSettingsTimeout)
	defer cancel()

	n.updateMu.Lock()
	next, changed, err := n.updateLocked(ctx, fn)
	n.updateMu.Unlock()
	if err == nil && changed {
		n.notify(next) // 锁外通知：订阅者里再读写设置不会死锁
	}
	return next, err
}

func (n *nativeSettings) updateLocked(ctx context.Context, fn func(*settings.Values)) (settings.Values, bool, error) {
	prev, err := n.snapshot(ctx)
	if err != nil {
		return prev, false, err
	}
	next := prev.Clone()
	fn(&next)
	next.Normalize()
	// 代理按 Go 侧规则归一（主机里带协议头、端口越界等），保证写进表的就是生效的
	next.App.NetworkProxy = proxyFromGo(normalizeNetworkProxy(toAny(next.App.NetworkProxy)))

	uiChanged, err := settings.ChangedKeys(prev.UI, next.UI)
	if err != nil {
		return prev, false, err
	}
	appChanged, err := settings.ChangedKeys(prev.App, next.App)
	if err != nil {
		return prev, false, err
	}
	if len(uiChanged) == 0 && len(appChanged) == 0 {
		return prev, false, nil
	}
	if err := validateAppChanges(appChanged, next.App); err != nil {
		return prev, false, err
	}

	var undo func() error
	if len(uiChanged) > 0 {
		if undo, err = n.store.ApplyUI(uiChanged); err != nil {
			return prev, false, err
		}
	}
	if len(appChanged) > 0 {
		payload, err := json.Marshal(appChanged)
		if err == nil {
			_, err = n.lib.settingsSet(ctx, payload)
		}
		if err != nil {
			if undo != nil {
				if uerr := undo(); uerr != nil {
					n.logf("[settings] 数据库写入失败后恢复界面设置文件也失败：%v", uerr)
				}
			}
			return prev, false, err
		}
	}
	return next, true, nil
}

// Subscribe 实现 services.Settings。
func (n *nativeSettings) Subscribe(fn func(settings.Values)) (cancel func()) {
	n.subMu.Lock()
	n.nextSub++
	id := n.nextSub
	n.subs[id] = fn
	n.subMu.Unlock()
	return func() {
		n.subMu.Lock()
		delete(n.subs, id)
		n.subMu.Unlock()
	}
}

// notify 通知订阅者：不持有任何锁，订阅者里再读写设置不会死锁。并发写入时通知的先后不保证——
// 每次给的都是完整的设置，订阅者要最新值就调用 Snapshot。
func (n *nativeSettings) notify(v settings.Values) {
	n.subMu.Lock()
	fns := make([]func(settings.Values), 0, len(n.subs))
	for _, fn := range n.subs {
		fns = append(fns, fn)
	}
	n.subMu.Unlock()
	for _, fn := range fns {
		fn(v.Clone())
	}
}
