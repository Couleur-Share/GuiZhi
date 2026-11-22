package settings

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"
	"unicode/utf16"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// 「数据与备份」分区三块（备份、完整备份仓库、配置迁移）共用的小零件：格式化、按钮外观、胶囊徽标、带标签的文本框与折叠行。
// 外观取自渲染层 DataSettings / RepositoryBackupSection / config-transfer 里的 Tailwind 类。

// jsLen 是 JS 字符串的 length（UTF-16 码元数）：口令长度的门槛按它算，与渲染层的 `password.length` 一致。
func jsLen(s string) int { return len(utf16.Encode([]rune(s))) }

// formatBytes 是备份文件大小的显示。withBytes 为真时不足 1 KB 的显示为「N B」（DataSettings），否则一律按 KB 起算（RepositoryBackupSection）。
func formatBytes(n int64, withBytes bool) string {
	switch {
	case withBytes && n < 1024:
		return strconv.FormatInt(n, 10) + " B"
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
}

// formatDateTime 是备份时间的显示（渲染层 toLocaleString）。compact 为真时是年月日时分、日期时间各两位（DataSettings 指定的格式）；
// 否则是默认格式（带秒，月日不补零；RepositoryBackupSection）。区域取系统区域（kit.ParseDateLocale），不是界面语言：英文界面配中文系统时，渲染层照样写成「2026/11/21 14:30:12」。
func formatDateTime(ms int64, compact bool, locale kit.DateLocale) string {
	at := time.UnixMilli(ms).Local()
	en := locale != kit.DateLocaleZH && locale != "" // 渲染层的 toLocaleString() 取浏览器区域（系统区域），不随界面语言；零值按中文
	switch {
	case compact && en:
		return at.Format("01/02/2006, 03:04 PM")
	case compact:
		return at.Format("2006/01/02 15:04")
	case en:
		return at.Format("1/2/2006, 3:04:05 PM")
	}
	return at.Format("2006/1/2 15:04:05")
}

// ── 按钮外观 ──

// btnShape 是一枚按钮的尺寸：高、横向内边距、图文间距（都是逻辑像素）与字号档、字重。
type btnShape struct {
	H, PadX, Gap float32
	Text         kit.TextStyle
	Weight       float32
}

// shape 按 Tailwind 尺寸换算：h 是 rem，padX 与 gap 是 Space 刻度。
func shape(l *kit.Look, hRem, padX, gap float32, text kit.TextStyle, weight float32) btnShape {
	return btnShape{H: l.Rem(hRem), PadX: l.Space(padX), Gap: l.Space(gap), Text: text, Weight: weight}
}

func (b btnShape) base(l *kit.Look) kit.ButtonStyle {
	return kit.ButtonStyle{
		Height: b.H, PadX: b.PadX, Gap: b.Gap, Radius: l.Rem(0.75), // rounded-lg：本应用的 lg 是 --radius
		Text: b.Text, Weight: b.Weight, Transition: kit.DurationDefault,
	}
}

// primaryBtn 是 bg-primary text-primary-foreground hover:bg-primary/90。
func primaryBtn(l *kit.Look, b btnShape) kit.ButtonStyle {
	t := l.T
	st := b.base(l)
	st.Fg, st.Bg, st.BgHover = t.PrimaryForeground, t.Primary, t.Primary.WithAlpha(0.9)
	return st
}

// destructiveBtn 是 bg-destructive text-destructive-foreground hover:bg-destructive/90。
func destructiveBtn(l *kit.Look, b btnShape) kit.ButtonStyle {
	t := l.T
	st := b.base(l)
	st.Fg, st.Bg, st.BgHover = t.DestructiveForeground, t.Destructive, t.Destructive.WithAlpha(0.9)
	return st
}

// outlineBtn 是 border border-border text-foreground，悬停底色由调用方定（hover:bg-muted/60 或 hover:bg-muted）。
func outlineBtn(l *kit.Look, b btnShape, hover tokens.Color) kit.ButtonStyle {
	t := l.T
	st := b.base(l)
	st.Fg, st.Border, st.BorderW, st.BgHover = t.Foreground, t.Border, 1, hover
	return st
}

// dialogGhostBtn 是对话框里的「取消」：border-border/70 text-muted-foreground，悬停边框、底色与文字都转向 primary。
func dialogGhostBtn(l *kit.Look, b btnShape) kit.ButtonStyle {
	t := l.T
	st := b.base(l)
	st.Fg, st.FgHover = t.MutedForeground, t.Primary
	st.Border, st.BorderHover, st.BorderW = t.Border.WithAlpha(0.7), t.Primary.WithAlpha(0.5), 1
	st.BgHover = t.Primary.WithAlpha(0.05)
	return st
}

// iconBtn 是纯图标按钮：边长 size 的方块，muted-foreground，悬停时底色与文字色变化。
func iconBtn(l *kit.Look, size float32, hoverBg, hoverFg tokens.Color) kit.ButtonStyle {
	t := l.T
	return kit.ButtonStyle{
		Height: size, Width: size, Radius: l.Rem(0.75), Transition: kit.DurationDefault,
		Fg: t.MutedForeground, FgHover: hoverFg, BgHover: hoverBg,
	}
}

// btn 声明一枚按钮并返回是否被点击。busy 时图标换成转圈（渲染层 Loader2Icon animate-spin 顶替图标），按钮同时禁用。
func btn(l *kit.Look, c ui.C, spec kit.ButtonSpec, st kit.ButtonStyle, busy bool) bool {
	if busy {
		spec.Busy, spec.Icon = true, gfx.Icon{}
		spec.Disabled = true
	}
	st.DisabledOpacity = disabledOpacity(st)
	return l.Button(c, spec, st).Clicked()
}

func disabledOpacity(st kit.ButtonStyle) float32 {
	if st.DisabledOpacity != 0 {
		return st.DisabledOpacity
	}
	return 0.5 // disabled:opacity-50
}

// spinnerBox 是列表加载中的占位：h-24 居中的小转圈（Spinner size="sm" tone="muted"）。
func spinnerBox(l *kit.Look, c ui.C) {
	ui.Row(c).FillWidth().Height(l.Rem(6)).Items(ui.Center).Justify(ui.Center).Children(func() {
		l.Spinner(c.Key("spinner"), l.Rem(1), l.T.MutedForeground)
	})
}

// ── 胶囊徽标 ──

// pill 画一枚圆角全满的徽标：px-2 py-0.5，文字 text-xs 或 text-[11px]。
func pill(l *kit.Look, c ui.C, text string, bg, fg tokens.Color, px float32, shrink0 bool) {
	row := ui.Row(c).Items(ui.Center).PadSides(l.Space(0.5), l.Space(2), l.Space(0.5), l.Space(2)).
		Draw(func(d *ui.DrawContext) { l.PaintSurface(d, kit.Surface{Bg: bg, Radius: d.Rect.Height / 2}) })
	if shrink0 {
		row = row.Shrink(0)
	}
	row.Children(func() {
		if px > 0 {
			l.TextPx(c.Key("text"), text, px, px*1.5, kit.WeightNormal, fg) // text-[11px]：px 字面量，行高继承 1.5
			return
		}
		l.Text(c.Key("text"), text, kit.TextXS, kit.WeightNormal, fg)
	})
}

// backupKindBadge 是旧快照行首的来源徽标（BackupKindBadge）：手动 primary，自动 muted，升级前琥珀，恢复前紫罗兰。
func backupKindBadge(l *kit.Look, c ui.C, kind string) {
	t := l.T
	pick := func(light, dark string) tokens.Color {
		if l.Dark {
			return tokens.Tailwind(dark)
		}
		return tokens.Tailwind(light)
	}
	label, bg, fg := kind, t.Muted, t.MutedForeground
	switch kind {
	case "manual":
		label, bg, fg = locales.SettingsBackupKindManual(), t.Primary.WithAlpha(0.1), t.Primary
	case "auto":
		label = locales.SettingsBackupKindAuto()
	case "pre-update":
		label, bg, fg = locales.SettingsBackupKindPreUpdate(), tokens.Tailwind("amber-500").WithAlpha(0.15), pick("amber-600", "amber-400")
	case "pre-restore":
		label, bg, fg = locales.SettingsBackupKindPreRestore(), tokens.Tailwind("violet-500").WithAlpha(0.15), pick("violet-600", "violet-400")
	}
	pill(l, c, label, bg, fg, 11, true)
}

// ── 文本框 ──

// labeledField 是 components/ui/Input：可选的标签（text-sm font-medium）、文本框（h-10）与错误行（text-xs text-destructive）纵排，gap-1.5。
// width 为 0 时撑满容器；secure 为真时内容是圆点。返回里面的输入元素，调用方用 Changed() 感知编辑。
func labeledField(l *kit.Look, c ui.C, f *ui.Field, label, placeholder, errText string, width float32, secure bool) ui.Box {
	return namedField(l, c, f, label, "", placeholder, errText, width, secure)
}

// intrinsicInputWidth 是没有显式宽度的 <input>（size=20，px-4，border）的外框宽度。
// 组件库 Input 把 className（如 w-56）挂在 <input> 上，外层 div 是 flex 子项按内容收缩，w-full 因此退回固有宽度：
// 内容宽度 = 20 × 字体平均字符宽，并取整到整像素（实测 text-sm 三个字号档：小 135、中 155、大 174，即 31/56em 的 20 倍向下取整）。
func intrinsicInputWidth(l *kit.Look) float32 {
	content := float64(20*31) / 56 * float64(l.Rem(0.875))
	return 2 + 2*l.Rem(1) + float32(math.Floor(content+1e-3))
}

// namedField 同 labeledField，另可给出与可见标签不同的可访问名（aria-label）；name 为空时取标签，再取占位文字。
func namedField(l *kit.Look, c ui.C, f *ui.Field, label, name, placeholder, errText string, width float32, secure bool) ui.Box {
	t := l.T
	col := ui.Column(c).Gap(l.Space(1.5))
	if width > 0 {
		col = col.Width(width).Shrink(0)
	} else {
		col = col.FillWidth()
	}
	var input ui.Box
	col.Children(func() {
		if label != "" {
			l.Text(c.Key("label"), label, kit.TextSM, kit.WeightMedium, t.Foreground)
		}
		f.Secure = secure
		if name == "" {
			name = label
		}
		if name == "" {
			name = placeholder
		}
		input = l.FieldInput(c.Key("input"), f, kit.FieldSpec{Name: name, Placeholder: placeholder, PlaceholderInk: t.MutedForeground})
		if errText != "" {
			l.Text(c.Key("error"), errText, kit.TextXS, kit.WeightNormal, t.Destructive, kit.Wrapped())
		}
	})
	return input
}

// ── 折叠行 ──

// summaryRow 声明一行 <summary>：行内的 disclosure 标记加文字，整行可点；返回是否被点击（调用方切换展开）。
func summaryRow(l *kit.Look, c ui.C, name string, open bool, fg tokens.Color) bool {
	size := kit.TextSM
	marker := kit.ChromeDisclosureMarker(l.Rem(size.Size), l.Rem(size.Line), l.Scale)
	sc := c.Key("summary")
	row := ui.Row(sc).FillWidth().Items(ui.Center).PadSides(0, 0, 0, marker.Advance).
		HitWith(ui.ID(sc), name, gfx.HitOptions{Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand, Expandable: true, Expanded: open}).
		Draw(func(d *ui.DrawContext) { kit.PaintDisclosure(d, d.Rect, marker, open, fg) })
	l.TabStop(row)
	row.Children(func() { l.Text(sc.Key("label"), name, size, kit.WeightMedium, fg) })
	return row.Clicked()
}

// iconOf 给一枚按钮配图标（0.875rem 的小图标用于 h-8 按钮，其余 1rem）。
func iconOf(l *kit.Look, ic gfx.Icon, small bool) (gfx.Icon, float32) {
	if small {
		return ic, l.Rem(0.875)
	}
	return ic, l.Rem(1)
}

// ── 后台工作 ──

// worker 把后台线程里的结果交回窗口线程：work 在后台 goroutine 里跑，返回一个在窗口线程执行的收尾函数（改界面状态）。
// 分区被切走（close）后收尾函数一律丢弃；work 里直接调 Guard 报告失败（Guard 任意 goroutine 可用），所以切走之后失败仍会提示。
// 零值可用。
type worker struct {
	mb     mailbox
	ctx    context.Context
	cancel context.CancelFunc
}

func (w *worker) context() context.Context {
	if w.ctx == nil {
		w.ctx, w.cancel = context.WithCancel(context.Background())
	}
	return w.ctx
}

// load 跑一次只读的后台请求：分区被切走时请求被取消，结果不再回来。
func (w *worker) load(wake func(), work func(ctx context.Context) func()) {
	ctx := w.context()
	go func() {
		done := work(ctx)
		if ctx.Err() != nil {
			return
		}
		w.mb.post(wake, done)
	}()
}

// act 跑一次会改数据的后台操作：不随分区切走而取消（备份、恢复、导入做到一半不能被打断），只是切走之后界面不再收到收尾。
func (w *worker) act(wake func(), work func() func()) {
	go func() { w.mb.post(wake, work()) }()
}

func (w *worker) drain() { w.mb.drain() }

func (w *worker) close() {
	w.mb.close()
	if w.cancel != nil {
		w.cancel()
	}
}
