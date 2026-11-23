package overlays

// 窗口内（局部）快捷键的匹配与分发（App.tsx 里「Handle local shortcuts」那段，台账 B-026）。
//
// 快捷键有两种模式：「全局」由系统注册，窗口没有焦点也响应，到达时走 Trigger；「局部」只在应用窗口有焦点时响应，
// 由窗口收到的按键与已保存的组合键比较，匹配了就在这里分发。模式没设置过按「局部」。
//
// Viva 把按键分两路投递：带 Ctrl / Alt 的可打印字符走 Config.OnShortcut，具名键走 Config.OnKey；
// 接线时两条路都交给 Handle（见 settings.EventAccelerator，录制与匹配共用同一份格式化）。

import (
	"strings"
	"sync"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/screens/settings"
)

// FocusKind 是键盘焦点所在的控件类别，由调用方（窗口的按键钩子）判断。
type FocusKind uint8

const (
	// FocusNone 是焦点在普通控件或没有焦点。
	FocusNone FocusKind = iota
	// FocusTextInput 是焦点在文字输入框：不带 Ctrl / Alt 的组合是在打字，必须放行。
	FocusTextInput
	// FocusRecorder 是焦点在快捷键录制框：所有按键都归录制框，局部快捷键不能抢。
	FocusRecorder
)

// ShortcutHooks 是局部快捷键触发的界面动作；为 nil 的钩子什么也不做（但按键仍算被消化）。
type ShortcutHooks struct {
	// ToggleWindow 显示或隐藏主窗口（showApp，仅局部模式在这里触发）。
	ToggleWindow func()
	// NewItem 打开快速采集（newItem）。
	NewItem func()
	// Search 打开全局命令面板（search）。
	Search func()
	// Settings 切到设置页（settings）。
	Settings func()
}

// LocalShortcuts 持有当前的快捷键配置并匹配按键。方法可从任意 goroutine 调用。
type LocalShortcuts struct {
	Env
	// Service 读取已保存的配置；nil 时只能用 Update 注入。
	Service services.Shortcuts
	Guard   kit.Guard
	Hooks   ShortcutHooks

	mu     sync.Mutex
	accels map[string]string // 动作 → 规范化后的组合键
	modes  map[string]string
}

// NewLocalShortcuts 创建局部快捷键分发器，配置为默认值，Reload 之后换成已保存的。
func NewLocalShortcuts(guard kit.Guard) *LocalShortcuts {
	l := &LocalShortcuts{Guard: guard}
	l.Update(services.ShortcutConfig{Accelerators: services.DefaultShortcuts(), Modes: services.DefaultShortcutModes()})
	return l
}

// Update 换上新的配置（设置页保存后、启动读到已保存的配置后调用）。
func (l *LocalShortcuts) Update(cfg services.ShortcutConfig) {
	accels := make(map[string]string, len(cfg.Accelerators))
	for action, a := range cfg.Accelerators {
		if c := CanonicalAccelerator(a); c != "" {
			accels[action] = c
		}
	}
	modes := make(map[string]string, len(cfg.Modes))
	for action, m := range cfg.Modes {
		modes[action] = m
	}
	l.mu.Lock()
	l.accels, l.modes = accels, modes
	l.mu.Unlock()
}

// Reload 在后台读已保存的配置并换上；读不出来时保留当前配置（默认或上一次读到的），留日志。
func (l *LocalShortcuts) Reload() {
	if l.Service == nil {
		return
	}
	l.spawn(func() {
		cfg, err := l.Service.Get(l.context())
		if err != nil {
			l.Guard.Background("shortcuts.reload", err)
			return
		}
		l.Update(cfg)
	})
}

// Accelerator 是动作当前生效的组合键（规范化写法）；没有则为空串。
func (l *LocalShortcuts) Accelerator(action string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.accels[action]
}

// Mode 是动作的模式：没设置过按局部。
func (l *LocalShortcuts) Mode(action string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.modeLocked(action)
}

func (l *LocalShortcuts) modeLocked(action string) string {
	if m := l.modes[action]; m != "" {
		return m
	}
	return services.ShortcutModeLocal
}

// Handle 匹配一次窗口按键：它是已保存组合键、且该动作是局部模式时分发动作并返回 true（调用方应当消化这个按键）；
// 否则返回 false，按键照常交给后面。松开事件不处理；按住不放的重复事件只消化、不重复触发。
//
// 放行规则：焦点在录制框时一律不匹配（录制框要收下所有组合，包括正好等于已有快捷键的）；
// 焦点在文字输入框时，不带 Ctrl、Meta、Alt 的组合是在打字，放行。
func (l *LocalShortcuts) Handle(e gfx.KeyEvent, focus FocusKind) bool {
	if e.Release || focus == FocusRecorder {
		return false
	}
	if focus == FocusTextInput && e.Modifiers&(gfx.ModifierControl|gfx.ModifierMeta|gfx.ModifierAlt) == 0 {
		return false
	}
	raw, ok := settings.EventAccelerator(e)
	if !ok {
		return false
	}
	pressed := CanonicalAccelerator(raw)

	l.mu.Lock()
	var matched []string
	for _, action := range services.ShortcutActions { // 固定顺序：两个动作被设成同一个组合键时结果稳定
		if l.accels[action] == pressed && l.modeLocked(action) == services.ShortcutModeLocal {
			matched = append(matched, action)
		}
	}
	l.mu.Unlock()
	if len(matched) == 0 {
		return false
	}
	if !e.Repeat {
		for _, action := range matched {
			l.run(action, true)
		}
	}
	return true
}

// Trigger 分发一次系统级（全局）快捷键的触发（shortcut:triggered）。showApp 由外壳自己处理，这里不再重复切换窗口。
func (l *LocalShortcuts) Trigger(action string) { l.run(action, false) }

func (l *LocalShortcuts) run(action string, includeToggle bool) {
	h := l.Hooks
	switch action {
	case services.ShortcutShowApp:
		if includeToggle {
			call(h.ToggleWindow)
		}
	case services.ShortcutNewItem:
		call(h.NewItem)
	case services.ShortcutSearch:
		call(h.Search)
	case services.ShortcutSettings:
		call(h.Settings)
	}
}

// 修饰键别名 → 规范名。
var modifierAliases = map[string]string{
	"commandorcontrol": "CommandOrControl", "cmdorctrl": "CommandOrControl",
	"command": "CommandOrControl", "cmd": "CommandOrControl", "control": "CommandOrControl", "ctrl": "CommandOrControl",
	"alt": "Alt", "option": "Alt",
	"shift": "Shift",
}

// 命名键别名 → 规范名（大写，比较不分大小写）。
var keyAliases = map[string]string{
	"ARROWUP": "UP", "ARROWDOWN": "DOWN", "ARROWLEFT": "LEFT", "ARROWRIGHT": "RIGHT",
	"RETURN": "ENTER", "ESC": "ESCAPE", "DEL": "DELETE", "INS": "INSERT", "PGUP": "PAGEUP", "PGDN": "PAGEDOWN",
}

// CanonicalAccelerator 把组合键写法统一成可比较的形式：修饰键按 CommandOrControl、Alt、Shift 排序并去重，别名归一
// （Ctrl / Control / Cmd → CommandOrControl，Option → Alt，ArrowUp → Up，Return → Enter），键名大写。
// 不是「修饰键 + 一个键」的写法（空串、只有修饰键、多个键）返回空串，永远不会匹配。
func CanonicalAccelerator(s string) string {
	var ctrl, alt, shift bool
	key := ""
	for _, part := range strings.Split(strings.TrimSpace(s), "+") {
		part = strings.TrimSpace(part)
		if part == "" {
			return "" // 空段（"++"、首尾的加号）：录制框不产生这类写法，不是有效组合
		}
		if m, ok := modifierAliases[strings.ToLower(part)]; ok {
			switch m {
			case "CommandOrControl":
				ctrl = true
			case "Alt":
				alt = true
			default:
				shift = true
			}
			continue
		}
		if key != "" {
			return "" // 两个非修饰键
		}
		key = strings.ToUpper(part)
		if a, ok := keyAliases[key]; ok {
			key = a
		}
	}
	if key == "" || !(ctrl || alt || shift) {
		return ""
	}
	var out []string
	if ctrl {
		out = append(out, "CommandOrControl")
	}
	if alt {
		out = append(out, "Alt")
	}
	if shift {
		out = append(out, "Shift")
	}
	return strings.Join(append(out, key), "+")
}
