package main

// 快捷键配置：对应 apps/desktop/src/main/shortcuts.ts 的数据层与 IPC。
//
// 文件落在 config 目录（shortcuts.json / shortcut-mode.json），读取时回退到旧版 userData 根目录下的同名文件。
// 注册系统级快捷键的部分通过 shortcutNative 接口交给外壳（main.go 里接 mygo.GlobalShortcut），
// 测试里替换成记录器——配置读写的行为与 Electron 版逐项对拍，系统级注册本身只能在真机上验证。

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

const (
	chShortcutsGet     = "shortcuts:get"
	chShortcutsSet     = "shortcuts:set"
	chShortcutsSetMode = "shortcuts:setMode"
	chShortcutsGetMode = "shortcuts:getMode"
	chShortcutsUpdated = "shortcuts:updated"
)

var shortcutActions = []string{"showApp", "newItem", "search", "settings"}

var defaultShortcutAccelerators = map[string]string{
	"showApp": "Alt+Shift+P", "newItem": "Alt+Shift+N", "search": "Alt+Shift+F", "settings": "Alt+Shift+S",
}

var defaultShortcutModes = map[string]string{
	"showApp": "global", "newItem": "local", "search": "local", "settings": "local",
}

// shortcutNative 是系统级快捷键注册；register 返回是否成功（失败只记日志，与 Electron 版一致）。
type shortcutNative interface {
	unregisterAll()
	register(action, accelerator string) bool
}

type shortcutState struct {
	mu           sync.Mutex
	accelerators any // 通常是 *omap；shortcuts:set 的入参是渲染进程给的，原样保存
	modes        any
	native       shortcutNative
}

func defaultAcceleratorMap() *omap {
	m := newOmap()
	for _, a := range shortcutActions {
		m.set(a, defaultShortcutAccelerators[a])
	}
	return m
}

func defaultModeMap() *omap {
	m := newOmap()
	for _, a := range shortcutActions {
		m.set(a, defaultShortcutModes[a])
	}
	return m
}

func (s *shortcutState) ensure() {
	if s.accelerators == nil {
		s.accelerators = defaultAcceleratorMap()
	}
	if s.modes == nil {
		s.modes = defaultModeMap()
	}
}

func omapDelete(m *omap, key string) {
	if _, ok := m.vals[key]; !ok {
		return
	}
	delete(m.vals, key)
	for i, k := range m.keys {
		if k == key {
			m.keys = append(m.keys[:i], m.keys[i+1:]...)
			break
		}
	}
}

// migrateLegacyActionKey 旧版「新建」动作叫 newPrompt（PromptHub 时代命名），读取时迁移为 newItem。
func migrateLegacyActionKey(m *omap) *omap {
	if v, ok := m.vals["newPrompt"]; ok {
		if _, has := m.vals["newItem"]; !has {
			m.set("newItem", v)
		}
		omapDelete(m, "newPrompt")
	}
	return m
}

func cloneOmap(m *omap) *omap {
	out := newOmap()
	for _, k := range m.keys {
		out.set(k, m.vals[k])
	}
	return out
}

func (l *Library) shortcutsConfigPath() string { return filepath.Join(l.configDir, "shortcuts.json") }
func (l *Library) shortcutModeConfigPath() string {
	return filepath.Join(l.configDir, "shortcut-mode.json")
}

// 优先 config 目录，不存在时回退旧版 userData 根目录。
func (l *Library) shortcutsReadPath() string {
	if _, err := os.Stat(l.shortcutsConfigPath()); err == nil {
		return l.shortcutsConfigPath()
	}
	return filepath.Join(l.paths.UserData, "shortcuts.json")
}

func (l *Library) shortcutModeReadPath() string {
	if _, err := os.Stat(l.shortcutModeConfigPath()); err == nil {
		return l.shortcutModeConfigPath()
	}
	return filepath.Join(l.paths.UserData, "shortcut-mode.json")
}

// loadShortcuts 对应 loadShortcuts：`{...DEFAULT, ...saved}`，读不出来或形状不对就用默认。
func (l *Library) loadShortcuts() *omap {
	data, err := os.ReadFile(l.shortcutsReadPath())
	if err != nil {
		return defaultAcceleratorMap()
	}
	parsed, err := parseOrdered(data)
	if err != nil {
		return defaultAcceleratorMap()
	}
	merged := defaultAcceleratorMap()
	switch saved := parsed.(type) {
	case *omap:
		migrateLegacyActionKey(saved)
		for _, k := range saved.keys {
			merged.set(k, saved.vals[k])
		}
	case []any: // 数组也能展开：键是下标
		for i, v := range saved {
			merged.set(strconv.Itoa(i), v)
		}
	default: // null / 字符串 / 数字：`'newPrompt' in x` 抛 TypeError，被 catch 后用默认
		return defaultAcceleratorMap()
	}
	return merged
}

// normalizeShortcutModes：只认已有动作名，且只认 global / local。
func normalizeShortcutModes(v any) *omap {
	out := defaultModeMap()
	src, ok := v.(*omap)
	if !ok || src == nil {
		return out
	}
	for _, action := range shortcutActions {
		if mode, ok := src.vals[action].(string); ok && (mode == "global" || mode == "local") {
			out.set(action, mode)
		}
	}
	return out
}

func (l *Library) loadShortcutModes() *omap {
	data, err := os.ReadFile(l.shortcutModeReadPath())
	if err != nil {
		return defaultModeMap()
	}
	parsed, err := parseOrdered(data)
	if err != nil {
		return defaultModeMap()
	}
	if m, ok := parsed.(*omap); ok {
		migrateLegacyActionKey(m)
	}
	return normalizeShortcutModes(parsed)
}

func (l *Library) writeConfigJSON(path string, v any) error {
	text, err := stringifyIndent2(v)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(text), 0o644)
}

// persistImportedShortcuts 对应 persistImportedShortcuts：配置导入时落盘，只认已有动作名；
// 空串是「用户清掉了这个快捷键」，要保留。写失败直接返回错误，让导入能说出原因。
func (l *Library) persistImportedShortcuts(accelerators, modes *omap) error {
	incoming := newOmap()
	if accelerators != nil {
		incoming = migrateLegacyActionKey(cloneOmap(accelerators))
	}
	next := defaultAcceleratorMap()
	for _, action := range shortcutActions {
		if s, ok := incoming.vals[action].(string); ok {
			next.set(action, s)
		}
	}
	modeSrc := newOmap()
	if modes != nil {
		modeSrc = migrateLegacyActionKey(cloneOmap(modes))
	}
	nextModes := normalizeShortcutModes(modeSrc)

	if err := l.writeConfigJSON(l.shortcutsConfigPath(), next); err != nil {
		return err
	}
	if err := l.writeConfigJSON(l.shortcutModeConfigPath(), nextModes); err != nil {
		return err
	}
	l.sc.mu.Lock()
	l.sc.accelerators, l.sc.modes = next, nextModes
	l.sc.mu.Unlock()
	return nil
}

// registerGlobalLocked 按当前配置重新注册系统级快捷键：只注册模式为 global 的。
func (l *Library) registerGlobalLocked() {
	if l.sc.native == nil {
		return
	}
	l.sc.native.unregisterAll()
	accs, ok := l.sc.accelerators.(*omap)
	if !ok {
		return
	}
	modes, _ := l.sc.modes.(*omap)
	for _, action := range accs.keys {
		if !jsTruthy(accs.vals[action]) {
			continue
		}
		mode := "local"
		if modes != nil {
			if s, ok := modes.vals[action].(string); ok && s != "" {
				mode = s
			}
		}
		if mode == "global" {
			if s, ok := accs.vals[action].(string); ok {
				l.sc.native.register(action, s)
			}
		}
	}
}

// startShortcuts 对应启动时的 registerShortcuts：加载配置；skipGlobal 时不抢系统级快捷键
// （自动化实例与用户正在运行的归知并存时，不该夺走用户的 Alt+Shift+P）。
func (l *Library) startShortcuts(skipGlobal bool) {
	acc, modes := l.loadShortcuts(), l.loadShortcutModes()
	l.sc.mu.Lock()
	defer l.sc.mu.Unlock()
	l.sc.accelerators, l.sc.modes = acc, modes
	if l.sc.native != nil {
		l.sc.native.unregisterAll()
	}
	if skipGlobal {
		return
	}
	l.registerGlobalLocked()
}

func (l *Library) currentShortcuts() any {
	l.sc.mu.Lock()
	defer l.sc.mu.Unlock()
	l.sc.ensure()
	return l.sc.accelerators
}

func (l *Library) currentShortcutModes() any {
	l.sc.mu.Lock()
	defer l.sc.mu.Unlock()
	l.sc.ensure()
	return l.sc.modes
}

func (l *Library) registerShortcutsIPC(b *Bridge) {
	b.handlers[chShortcutsGet] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		return l.currentShortcuts(), nil
	}
	b.handlers[chShortcutsGetMode] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		return l.currentShortcutModes(), nil
	}
	b.handlers[chShortcutsSet] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		var arg any // undefined（没传参）与 null 在 TS 里不同，但后续都会在遍历时抛错，这里合并处理
		if len(a) > 0 {
			v, err := parseOrdered(a[0])
			if err != nil {
				return nil, err
			}
			arg = v
		}
		l.sc.mu.Lock()
		l.sc.ensure()
		l.sc.accelerators = arg
		saved := false
		if len(a) > 0 {
			saved = l.writeConfigJSON(l.shortcutsConfigPath(), arg) == nil
		}
		if _, ok := arg.(*omap); !ok && (arg == nil) {
			if l.sc.native != nil {
				l.sc.native.unregisterAll()
			}
			l.sc.mu.Unlock()
			return nil, &jsTypeError{"Cannot convert undefined or null to object"}
		}
		l.registerGlobalLocked()
		l.sc.mu.Unlock()
		l.pushMain(chShortcutsUpdated, arg) // 广播给所有窗口（局部快捷键由渲染进程处理）
		return saved, nil
	}
	b.sends[chShortcutsSetMode] = func(a []json.RawMessage) {
		var arg any
		if len(a) > 0 {
			arg, _ = parseOrdered(a[0])
		}
		l.sc.mu.Lock()
		defer l.sc.mu.Unlock()
		l.sc.ensure()
		modes := normalizeShortcutModes(arg)
		l.sc.modes = modes
		if err := l.writeConfigJSON(l.shortcutModeConfigPath(), modes); err != nil {
			logAppErrorf("shortcuts", "保存快捷键模式", "%s", err.Error())
		}
		l.registerGlobalLocked()
	}
}

type jsTypeError struct{ msg string }

func (e *jsTypeError) Error() string { return e.msg }
