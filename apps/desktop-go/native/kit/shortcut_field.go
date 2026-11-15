package kit

import (
	"sync/atomic"

	"github.com/egoist/mygo/ui"

	"guizhi-mygo-shell/native/kit/icons"
	"guizhi-mygo-shell/native/locales"
)

// keyCapture 统计正在录制的快捷键输入框数。应用层的窗口快捷键（命令面板、切模块……）
// 在它不为 0 时必须让路：窗口快捷键的优先级高于获得焦点的元素，不让路的话，
// 用户想把 Ctrl+K 录成某个动作的快捷键，按下去却打开了命令面板。
var keyCapture atomic.Int32

// KeyCaptureActive 报告是否有快捷键输入框正在录制。应用层处理自己的窗口快捷键之前先问它。
func KeyCaptureActive() bool { return keyCapture.Load() > 0 }

// ShortcutFieldOptions 描述一个快捷键输入框。
type ShortcutFieldOptions struct {
	// Label 是动作名（如「全局搜索」），进入输入框与清除钮的读屏名。
	Label string
	// Value 是当前的加速键字符串，空串是未设置。
	Value string
	// OnChange 在录到完整组合时调用（UI 线程），参数是 Electron 风格的加速键，如 "CommandOrControl+Shift+K"。
	OnChange func(accelerator string)
	// OnClear 在点清除钮时调用；nil 则不画清除钮。已有值时才显示清除钮。
	OnClear func()
	// Invalid 为真时描边变红：与别的动作冲突、被系统占用……说明文字由调用方放在输入框旁边。
	Invalid bool
}

// ShortcutField 是设置里的快捷键录制框，对应渲染层 ShortcutItem 的输入框与清除钮。
// 每个动作持有一个，每帧调用 View。
//
// 与渲染层的差别（有意的，都是它的缺陷）：
//   - 点击或在焦点上按 Enter/Space 才开始录制，Tab 经过时不录——渲染层是「获得焦点就录」，
//     Tab 键本身会被当成要录的键，键盘用户 Tab 进来就出不去；
//   - 录制中 Esc 取消，Tab/Shift+Tab 取消并把焦点交给下一个控件；
//   - 方向键录成 Up/Down/Left/Right（渲染层录出的 ARROWUP 不是合法的加速键，注册会失败）；
//   - Win 键录成 Meta、显示为 Win（渲染层把 Win 键当成 Ctrl 记下）；
//   - 录制中按住修饰键会实时显示，松开会回退；窗口失去焦点时取消录制。
//
// 组合必须带修饰键（单个字母或 F 键当快捷键会吃掉正常输入），与渲染层一致。
type ShortcutField struct {
	recording bool
	held      ui.Modifiers // 录制中当前按住的修饰键
	heldKey   string       // 录制中按下了主键但没有修饰键，显示出来提示继续
}

// NewShortcutField 创建一个快捷键录制框。
func NewShortcutField() *ShortcutField { return &ShortcutField{} }

// Recording 报告是否正在录制。
func (f *ShortcutField) Recording() bool { return f.recording }

func (f *ShortcutField) start() {
	if f.recording {
		return
	}
	f.recording, f.held, f.heldKey = true, 0, ""
	keyCapture.Add(1)
}

// stop 结束录制（取消或完成）；重复调用无害。
func (f *ShortcutField) stop() {
	if !f.recording {
		return
	}
	f.recording, f.held, f.heldKey = false, 0, ""
	keyCapture.Add(-1)
}

// View 画输入框（和清除钮）。
func (f *ShortcutField) View(c *ui.Context, l *Look, o ShortcutFieldOptions) {
	ui.Row(c).AlignItems(ui.Center).Gap(16).Children(func() {
		f.field(c, l, o)
		ui.Row(c).Width(32).Height(32).Center().Shrink(0).Children(func() {
			if o.OnClear != nil && o.Value != "" {
				clearButton(c, l, locales.SettingsClearShortcutFor(o.Label), o.OnClear)
			}
		})
	})
}

func (f *ShortcutField) field(c *ui.Context, l *Look, o ShortcutFieldOptions) {
	text := FormatAccelerator(o.Value)
	if f.recording {
		text = FormatHeld(f.held, f.heldKey)
	}
	field := ui.ButtonBase(c).Label(locales.SettingsShortcutInputLabel(o.Label)).Description(text).Cursor(ui.CursorPointer)
	field.OnUnmount(f.stop) // 录制中整页被换走：别让「正在录制」的标记一直留着，把应用的窗口快捷键永远关掉
	field.HandleInput(func(ev ui.InputEvent) bool { return f.input(ev, o) })
	if field.Clicked() {
		f.start()
	}
	// 失去焦点（点到别处、Tab 走了）或窗口失去键盘（切到别的窗口）：取消录制。
	// FocusLeft 把「窗口失去键盘」也算作焦点离开；窗口失焦时 FocusWithin 仍指向这个元素，单看它抓不到
	if f.recording && (field.FocusLeft() || !field.FocusWithin()) {
		f.stop()
		text = FormatAccelerator(o.Value)
	}

	base := l.T.SettingsInputBg
	bg, fg, border := base, l.T.Foreground, l.T.SettingsInputBorder
	switch {
	case f.recording:
		bg = over(l.T.Primary.Alpha(0.05), l.solid(l.T.Background))
		fg, border = l.T.Primary, l.T.Primary
	case field.Hovered():
		bg, fg = over(l.T.Accent.Alpha(0.7), l.solid(base)), l.T.AccentForeground
	}
	if o.Invalid && !f.recording {
		border = tw("red-500")
	}
	field.Width(160).Height(36).Padding(0, 12).Center().Radius(6).Border(1, border).Background(bg).
		Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.05)).Children(func() {
		ui.Text(c, text).FontSize(12).FontWeight(500).TextColor(fg).TextAlign(ui.Center).MaxLines(1).Ellipsis("…")
	})
	if f.recording { // ring-2 ring-primary/20
		field.Shadow(0, 0, 0, 2, l.T.Primary.Alpha(0.2))
	}
}

// input 处理输入框获得焦点时的按键。不在录制时一律不接管，让按钮自己响应 Enter/Space 与 Tab。
func (f *ShortcutField) input(ev ui.InputEvent, o ShortcutFieldOptions) bool {
	if !f.recording || (ev.Kind != ui.InputKeyDown && ev.Kind != ui.InputKeyUp) {
		return false
	}
	// 修饰键的状态：按下时 Mods 含它，松开时不含——显示随之增减
	f.held = ev.Mods
	if ev.Kind == ui.InputKeyUp || ev.Repeat {
		return true // 松开与自动重复都吞掉，免得启动录制的那一下 Enter/Space 的松开被当成别的
	}
	if IsModifierCode(ev.Code) {
		return true // 修饰键本身：只更新显示。接管它也让 Win 键和 Alt 键不去开开始菜单与窗口菜单
	}
	switch {
	case ev.Key == ui.KeyEscape && ev.Mods == 0:
		f.stop()
		return true
	case ev.Key == ui.KeyTab && ev.Mods&(ui.Ctrl|ui.Alt|ui.Super) == 0:
		f.stop()
		return false // 不接管：Tab/Shift+Tab 照常把焦点交给下一个控件
	}
	acc, ok := Accelerator(ev.Mods, ev.Key, ev.PhysicalKey, ev.Code)
	if !ok {
		// 没有修饰键（或这个键不能作主键）：显示已按下的，等用户补上修饰键
		f.heldKey, _ = KeyToken(ev.Key, ev.PhysicalKey, ev.Code)
		return true
	}
	f.stop()
	if o.OnChange != nil {
		o.OnChange(acc)
	}
	return true
}

// clearButton 画清除钮：32×32 圆形，悬停垫 muted 色、图标转 destructive 色。
func clearButton(c *ui.Context, l *Look, label string, onClear func()) {
	b := ui.ButtonBase(c).Label(label).Tooltip(label)
	fg, bg := l.T.MutedForeground, ui.Transparent
	if b.Hovered() {
		fg, bg = l.T.Destructive, l.T.Muted
	}
	b.Size(32, 32).Center().Radius(16).Background(bg).Children(func() {
		Icon(c, icons.X, 16).TextColor(fg)
	})
	if b.Clicked() {
		onClear()
	}
}
