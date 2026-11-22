package settings

import (
	"strconv"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// recorder 是快捷键录制框（渲染层 ShortcutItem 里的只读 <input>）：获得焦点即开始录制，
// 修饰键加一个普通键即完成并交给调用方，失焦或 Esc 取消。
//
// 取键分两路，由 Viva 的投递规则决定：带 Ctrl 或 Alt 的和弦只会作为快捷键投递（不进控件的 Key 回调），
// 所以录制中逐个声明候选和弦（recorderChords）；只带 Shift 或不带修饰键的键走 Key 回调。
// 仅按下修饰键（Alt、Ctrl 单独按）Viva 不投递，所以录制中看不到「只按了修饰键」的中间态，
// 提示文字保持「请按下快捷键」直到完整和弦出现（与渲染层不同，登记在台账）。
type recorder struct {
	h       ui.Handlers
	rec     bool     // 正在录制
	temp    []string // 录制中已按下的键（只有不够完成的单键或仅 Shift 时有内容）
	focused bool     // 上一帧是否有焦点：由无到有即开始录制（onFocus）

	pending    string // 在事件里完成、等声明时交给调用方的快捷键
	hasPending bool
}

// recorderChord 是录制时候选的和弦：Shortcut 的修饰键与键名，以及对应的 accelerator 字符串。
type recorderChord struct {
	mods  gfx.Modifiers
	key   string
	accel string
}

// recorderChords 是录制时声明的全部带 Ctrl 或 Alt 的和弦（Ctrl、Alt 各自与可选的 Shift 组合）。
// 键集取全局热键可注册的常用键：字母、数字、F1–F12、常用标点与导航键。
var recorderChords = buildRecorderChords()

func buildRecorderChords() []recorderChord {
	type named struct{ key, accel string }
	keys := []named{}
	for ch := 'a'; ch <= 'z'; ch++ {
		keys = append(keys, named{string(ch), strings.ToUpper(string(ch))})
	}
	for ch := '0'; ch <= '9'; ch++ {
		keys = append(keys, named{string(ch), string(ch)})
	}
	for n := 1; n <= 12; n++ {
		s := "F" + strconv.Itoa(n)
		keys = append(keys, named{strings.ToLower(s), s})
	}
	for _, p := range []string{",", ".", "/", ";", "'", "[", "]", "-", "=", "`", "\\"} {
		keys = append(keys, named{p, p})
	}
	for _, k := range []named{
		{"enter", "Enter"}, {" ", "Space"}, {"up", "Up"}, {"down", "Down"}, {"left", "Left"}, {"right", "Right"},
		{"home", "Home"}, {"end", "End"}, {"backspace", "Backspace"}, {"delete", "Delete"},
		{"page_up", "PageUp"}, {"page_down", "PageDown"}, {"insert", "Insert"},
	} {
		keys = append(keys, k)
	}
	var out []recorderChord
	for _, base := range []gfx.Modifiers{ui.Ctrl, ui.Alt, ui.Ctrl | ui.Alt} {
		for _, shift := range []gfx.Modifiers{0, ui.Shift} {
			mods := base | shift
			for _, k := range keys {
				out = append(out, recorderChord{mods: mods, key: k.key, accel: strings.Join(append(modifierNames(mods), k.accel), "+")})
			}
		}
	}
	return out
}

// modifierNames 按渲染层的顺序（CommandOrControl、Alt、Shift）列出修饰键的 accelerator 名。
func modifierNames(m gfx.Modifiers) []string {
	var keys []string
	if m&(gfx.ModifierControl|gfx.ModifierMeta) != 0 {
		keys = append(keys, "CommandOrControl")
	}
	if m&gfx.ModifierAlt != 0 {
		keys = append(keys, "Alt")
	}
	if m&gfx.ModifierShift != 0 {
		keys = append(keys, "Shift")
	}
	return keys
}

// namedAccelerators 是命名键对应的 accelerator 名（全局热键注册接受的写法，与渲染层的大写 e.key 不同：
// 后者的「ARROWUP」之类注册不上）。
var namedAccelerators = map[gfx.Key]string{
	gfx.KeyEnter: "Enter", gfx.KeySpace: "Space", gfx.KeyLeft: "Left", gfx.KeyRight: "Right", gfx.KeyUp: "Up", gfx.KeyDown: "Down",
	gfx.KeyHome: "Home", gfx.KeyEnd: "End", gfx.KeyBackspace: "Backspace", gfx.KeyDelete: "Delete",
	gfx.KeyPageUp: "PageUp", gfx.KeyPageDown: "PageDown", gfx.KeyInsert: "Insert",
	gfx.KeyF1: "F1", gfx.KeyF2: "F2", gfx.KeyF3: "F3", gfx.KeyF4: "F4", gfx.KeyF5: "F5", gfx.KeyF6: "F6",
	gfx.KeyF7: "F7", gfx.KeyF8: "F8", gfx.KeyF9: "F9", gfx.KeyF10: "F10", gfx.KeyF11: "F11", gfx.KeyF12: "F12",
}

// keyAccelerator 把一次按键换成 accelerator 里的键名；不是能注册的键（输入法组字、非 ASCII 字符）返回 false。
func keyAccelerator(e gfx.KeyEvent) (string, bool) {
	if name, ok := namedAccelerators[e.Key]; ok {
		return name, true
	}
	if e.Key != gfx.KeyUnknown || e.Text == "" {
		return "", false
	}
	runes := []rune(e.Text)
	if len(runes) != 1 {
		return "", false
	}
	switch r := runes[0]; {
	case r == ' ':
		return "Space", true
	case r > ' ' && r < 0x7f:
		return strings.ToUpper(string(r)), true
	}
	return "", false
}

// formatAccelerator 是录制框里显示的快捷键（渲染层 formatShortcut，Windows 分支）：空值显示「未设置」，
// CommandOrControl、Control 显示为 Ctrl，Meta 显示为 ⌘，加号两侧补空格。
func formatAccelerator(s string) string {
	if s == "" {
		return locales.SettingsNotSet()
	}
	s = strings.Replace(s, "CommandOrControl", "Ctrl", 1)
	s = strings.Replace(s, "Control", "Ctrl", 1)
	s = strings.Replace(s, "Meta", "⌘", 1)
	return strings.ReplaceAll(s, "+", " + ")
}

// init 装好事件回调（只装一次）。
func (r *recorder) init() {
	if r.h.Key != nil {
		return
	}
	r.h.Key = r.key
	r.h.Pointer = func(e gfx.PointerEvent) gfx.EventResult {
		if e.Kind == gfx.PointerDown && !r.rec {
			r.rec = true // 已经有焦点时再点一下也重新开始录制（焦点由无到有那一刻由声明处补上）
			return gfx.EventResult{Repaint: true}
		}
		return gfx.EventResult{}
	}
}

// stop 结束录制并清掉中间态。
func (r *recorder) stop() {
	r.rec, r.temp = false, nil
}

// key 处理录制框得到的按键：Tab 交给焦点遍历（失焦即取消）；未录制时 Enter 与空格开始录制（键盘可达）；
// 录制中 Esc 取消，其余键与修饰键拼成快捷键，修饰键加普通键即完成。
func (r *recorder) key(e gfx.KeyEvent) gfx.EventResult {
	if e.Release || e.Key == gfx.KeyTab {
		return gfx.EventResult{}
	}
	if !r.rec {
		if e.Key == gfx.KeyEnter || e.Key == gfx.KeySpace || e.Text == " " {
			r.rec = true
			return gfx.EventResult{Handled: true, Repaint: true}
		}
		return gfx.EventResult{}
	}
	if e.Key == gfx.KeyEscape {
		r.stop()
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	name, ok := keyAccelerator(e)
	if !ok {
		return gfx.EventResult{Handled: true}
	}
	keys := append(modifierNames(e.Modifiers), name)
	if len(keys) >= 2 {
		r.finish(strings.Join(keys, "+"))
	} else {
		r.temp = keys
	}
	return gfx.EventResult{Handled: true, Repaint: true}
}

// finish 在录到完整和弦时结束录制，快捷键留到下次声明时交给调用方（保存在声明处做，与点击同一路径）。
func (r *recorder) finish(accel string) {
	r.stop()
	r.pending, r.hasPending = accel, true
}

// declare 声明录制框，shortcut 是当前已保存的快捷键；录到完整和弦的那一帧调用 onDone。
func (r *recorder) declare(l *kit.Look, c ui.C, name, shortcut string, onDone func(accel string)) {
	t := l.T
	r.init()
	box := ui.Control(c, name, &r.h).Width(l.Rem(10)).Height(l.Rem(2.25)).Shrink(0).Center()
	st := box.State()
	if st.Focused && !r.focused {
		r.rec = true // onFocus
	}
	if !st.Focused {
		r.stop() // onBlur
	}
	r.focused = st.Focused
	if r.rec {
		for _, ch := range recorderChords {
			if box.Shortcut(ch.mods, ch.key) {
				r.finish(ch.accel)
				break
			}
		}
	}
	if r.hasPending {
		accel := r.pending
		r.pending, r.hasPending = "", false
		onDone(accel)
	}

	rec, hovered, focused := r.rec, st.Hovered, st.Focused
	text, ink := formatAccelerator(shortcut), t.Foreground
	switch {
	case rec && len(r.temp) > 0:
		text, ink = formatAccelerator(strings.Join(r.temp, "+")), t.Primary
	case rec:
		text, ink = locales.SettingsPressKeys(), t.Primary
	case hovered:
		ink = t.AccentForeground
	}
	radius := l.Rem(0.375) // rounded-md
	box.Draw(func(d *ui.DrawContext) {
		surface := kit.Surface{Bg: t.SettingsInputBg, Border: t.SettingsInputBorder, BorderW: 1, Radius: radius, Shadows: kit.ShadowSM}
		switch {
		case rec: // border-primary ring-2 ring-primary/20 bg-primary/5
			l.PaintRingBand(d, 2, t.Primary.WithAlpha(0.2), radius)
			surface.Bg, surface.Border = t.Primary.WithAlpha(0.05), t.Primary
		case hovered: // hover:bg-accent/70
			surface.Bg = t.Accent.WithAlpha(0.7)
		}
		if focused && !rec { // 全局 input:focus-visible 的 ring-2 ring-primary/50
			l.PaintRingBand(d, 2, t.Primary.WithAlpha(0.5), radius)
		}
		l.PaintSurface(d, surface)
	}).Children(func() {
		l.Text(c.Key("text"), text, kit.TextXS, kit.WeightMedium, ink, kit.Centered())
	})
}
