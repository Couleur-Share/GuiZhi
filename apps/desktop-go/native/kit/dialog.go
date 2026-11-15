package kit

import (
	"sync"

	"github.com/egoist/mygo/ui"

	"guizhi-mygo-shell/native/kit/icons"
	"guizhi-mygo-shell/native/locales"
)

// 对话框的尺寸：对应渲染层 ConfirmDialog 的 max-w-sm 与 UnsavedChangesDialog 的 max-w-md。
const (
	confirmWidth = 384
	unsavedWidth = 448
	dialogPad    = 24
	dialogEdge   = 16 // 窗口很窄时，对话框离窗口边至少这么远（外层 p-4）
	dialogBtnH   = 40
)

// busy 是对话框里一次后台操作的状态：goroutine 写，UI 线程读。
type busy struct {
	mu       sync.Mutex
	running  bool
	finished bool // 成功完成，等 UI 线程关闭对话框
}

func (b *busy) isRunning() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.running
}

// begin 标记开始；已有操作在跑返回 false（连点确认不能重复执行）。
func (b *busy) begin() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.running {
		return false
	}
	b.running, b.finished = true, false
	return true
}

func (b *busy) end(ok bool) {
	b.mu.Lock()
	b.running, b.finished = false, ok
	b.mu.Unlock()
}

// takeFinished 取走「成功完成」标记：返回 true 一次。
func (b *busy) takeFinished() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	f := b.finished
	b.finished = false
	return f
}

// dialogButtonKind 是对话框按钮的配色类别。
type dialogButtonKind int

const (
	btnSecondary   dialogButtonKind = iota // 取消：面色 + 描边，悬停 accent
	btnPrimary                             // 确认 / 保存：主题色
	btnDestructive                         // 危险确认：red-600
	btnDiscard                             // 不保存：描边，红字，悬停 destructive/10
)

// dialogButtonColors 是对话框按钮的底色、字色与描边（hover 为悬停态）。
// 取值照渲染层：确认 bg-primary hover:bg-primary/90；危险 bg-red-600 hover:bg-red-700 + 白字；
// 取消 app-wallpaper-surface + hover:bg-accent；不保存 text-destructive + hover:bg-destructive/10。
// 对比度测试（TestPresetsMeetContrast）检查的就是这里返回的配对，不是另写一份。
func (l *Look) dialogButtonColors(kind dialogButtonKind, hover bool) (bg, fg, border ui.Color) {
	switch kind {
	case btnPrimary:
		bg, fg, border = l.T.Primary, l.T.PrimaryForeground, ui.Transparent
		if hover {
			bg = over(l.T.Primary.Alpha(0.9), l.solid(l.T.UiPanelStrongBg))
		}
	case btnDestructive:
		bg, fg, border = tw("red-600"), ui.Hex("#ffffff"), ui.Transparent
		if hover {
			bg = tw("red-700")
		}
	case btnDiscard:
		bg, fg, border = l.Surface, l.T.Destructive, l.Border
		if hover {
			bg = over(l.T.Destructive.Alpha(0.1), l.solid(l.Surface))
		}
	default:
		bg, fg, border = l.Surface, l.T.Foreground, l.Border
		if hover {
			bg = l.SurfaceHover
		}
	}
	return bg, fg, border
}

// dialogButton 画对话框底部的一枚按钮（h-10、圆角 8、text-sm font-medium），平分一行；
// disabled 时按渲染层的写法降低不透明度，spinner 为 true 时在文字前转圈。
func dialogButton(c *ui.Context, l *Look, kind dialogButtonKind, label string, disabled, spinner bool) ui.Element {
	b := ui.ButtonBase(c).Label(label)
	if disabled {
		b = b.Disabled(true)
	}
	bg, fg, border := l.dialogButtonColors(kind, !disabled && b.Hovered())
	opacity := float32(1)
	if disabled {
		opacity = 0.5
		if kind == btnPrimary || kind == btnDestructive {
			opacity = 0.6
		}
	}
	b.Height(dialogBtnH).Grow(1).Shrink(1).Padding(0, 16).Gap(8).Center().Radius(8).Border(1, border).Background(bg).Opacity(opacity).
		Children(func() {
			if spinner {
				ui.Spinner(c).Size(16, 16).TextColor(fg)
			}
			ui.Text(c, label).FontSize(14).FontWeight(500).TextColor(fg)
		})
	return b
}

// dialogSpec 是一个对话框的静态内容：宽度、图标圈（可无）、标题与说明。
type dialogSpec struct {
	width           float32
	glyph           icons.Name
	glyphFg, discBg ui.Color
	hasGlyph        bool
	title, message  string
}

// dialogFrame 画一个居中的模态对话框：遮罩、面板、图标圈、标题、说明，按钮由 buttons 画。
// 遮罩与面板取渲染层的 bg-background/60 与 app-wallpaper-panel-strong；没有背景模糊，面板底色预先合成成不透明。
// Escape 或点遮罩时 DialogBase 把 *open 置 false，由调用方决定是否真的关闭。
func dialogFrame(c *ui.Context, l *Look, open *bool, s dialogSpec, buttons func(panel ui.Element)) {
	ui.DialogBase(c, open, func(backdrop, panel ui.Element) {
		backdrop.Background(l.T.Background.Alpha(0.6)).Padding(dialogEdge)
		panel.Column().Width(s.width).MaxWidthPercent(100).Padding(dialogPad).Radius(12).AlignItems(ui.Stretch).
			Border(1, l.T.UiPanelStrongBorder).Background(l.solid(l.T.UiPanelStrongBg)).Role(ui.RoleAlertDialog).Description(s.message)
		l.T.UiPanelStrongShadow.On(panel)
		if s.title != "" {
			panel.Label(s.title)
		}
		panel.Children(func() {
			if s.hasGlyph {
				ui.Column(c).AlignSelf(ui.Center).Size(48, 48).Radius(24).Center().Background(s.discBg).Margin(0, 0, 16, 0).Children(func() {
					Icon(c, s.glyph, 24).TextColor(s.glyphFg)
				})
			}
			if s.title != "" {
				ui.Text(c, s.title).FontSize(18).FontWeight(600).TextAlign(ui.Center).TextColor(l.T.Foreground).Margin(0, 0, 8, 0)
			}
			ui.Text(c, s.message).FontSize(14).TextAlign(ui.Center).TextColor(l.T.MutedForeground).Margin(0, 0, 24, 0)
			buttons(panel)
		})
	})
}

// ConfirmOptions 描述一次确认。
type ConfirmOptions struct {
	Title       string // 可空
	Message     string
	ConfirmText string // 空取「确认」
	CancelText  string // 空取「取消」
	// Destructive 让确认钮变红并带警示图标：删除、清空这类不可逆操作。
	Destructive bool

	// OnConfirm 在用户确认后执行：对话框先关闭，再在 UI 线程调用它。
	OnConfirm func()

	// Action 是需要时间、可能失败的确认动作，与 OnConfirm 二选一（两者都有时用 Action）。
	// 它在后台 goroutine 里经 Guard 执行：期间对话框显示加载态且不可关闭（Esc、点遮罩、Enter 都无效）；
	// 成功后关闭并调用 OnDone（UI 线程）；失败时 Guard 已弹出带原因的错误提示，对话框保持打开——
	// 用户可以重试或取消，而不是失败后对话框消失、什么都没发生。
	// 动作在别的 goroutine 里跑：它要用的数据在 Open 之前取好，不要读写界面状态。
	Action func() error
	// ActionName 是失败提示里的动作名（已本地化，如「删除」）；空取 Title。
	ActionName string
	Guard      Guard
	OnDone     func()
}

// Confirm 是确认对话框，对应渲染层 ConfirmDialog。需要确认的页面持有一个 Confirm，
// 每帧在视图里调用 View，要确认时 Open。
//
// 键盘与渲染层一致：初始焦点在「取消」（防误操作），Esc 取消，Enter 确认——
// 不论焦点在哪个按钮上。
type Confirm struct {
	wake func()
	b    busy

	// 仅 UI 线程读写
	open  bool
	fresh bool // 刚打开、尚未给初始焦点
	opts  ConfirmOptions
}

// NewConfirm 创建确认对话框。wake 请求重绘（后台动作结束时用，可在任意 goroutine 调用）。
func NewConfirm(wake func()) *Confirm { return &Confirm{wake: wake} }

// Open 打开对话框（UI 线程）。
func (d *Confirm) Open(o ConfirmOptions) { d.open, d.fresh, d.opts = true, true, o }

// Close 关闭对话框（UI 线程）。后台动作还在跑时关闭不会中止它。
func (d *Confirm) Close() { d.open = false }

// Visible 报告对话框是否打开。
func (d *Confirm) Visible() bool { return d.open }

// View 画对话框；没打开时什么都不画。
func (d *Confirm) View(c *ui.Context, l *Look) {
	if d.b.takeFinished() {
		d.open = false
		if done := d.opts.OnDone; done != nil {
			done()
		}
	}
	if !d.open {
		return
	}
	loading := d.b.isRunning()
	o := d.opts
	confirmText, cancelText := o.ConfirmText, o.CancelText
	if confirmText == "" {
		confirmText = locales.CommonConfirm()
	}
	if cancelText == "" {
		cancelText = locales.CommonCancel()
	}
	spec := dialogSpec{width: confirmWidth, title: o.Title, message: o.Message}
	kind := btnPrimary
	if o.Destructive {
		kind = btnDestructive
		spec.hasGlyph, spec.glyph = true, icons.TriangleAlert
		spec.glyphFg, spec.discBg = tw("red-600"), tw("red-100")
		if l.Dark {
			spec.glyphFg = tw("red-400")
			spec.discBg = over(tw("red-900").Alpha(0.3), l.solid(l.T.UiPanelStrongBg))
		}
	}

	var cancelHit, confirmHit bool
	var cancel ui.Element
	open := true
	dialogFrame(c, l, &open, spec, func(panel ui.Element) {
		// Enter 在整个面板内都是确认（与渲染层一致）；加载态里不响应
		if !loading && panel.Shortcut(0, ui.KeyEnter) {
			confirmHit = true
		}
		ui.Row(c).Gap(12).Children(func() {
			cancel = dialogButton(c, l, btnSecondary, cancelText, loading, false)
			if cancel.Clicked() {
				cancelHit = true
			}
			confirm := dialogButton(c, l, kind, confirmText, loading, loading)
			if confirm.Clicked() {
				confirmHit = true
			}
		})
	})
	d.focusInitial(cancel)
	switch {
	case loading:
		// 动作在跑：忽略一切关闭与重复确认。对话框关不掉比「关掉了动作还在后台跑、结果无处可报」好
	case confirmHit:
		d.confirm()
	case cancelHit || !open: // !open：Esc 或点了遮罩
		d.open = false
	}
}

// focusInitial 在对话框刚打开的那一帧把焦点交给 initial（取消：防误操作）。
// 必须在 DialogBase 返回之后才调：它在 Overlay 建完时才记下「打开前谁有焦点」，
// 用于关闭后交还；在里面用 AutoFocus 会让它记成对话框自己的按钮，关闭后焦点无处可回。
func (d *Confirm) focusInitial(initial ui.Element) {
	if d.fresh {
		d.fresh = false
		initial.Focus()
	}
}

func (d *Confirm) confirm() {
	o := d.opts
	if o.Action == nil {
		d.open = false
		if o.OnConfirm != nil {
			o.OnConfirm()
		}
		return
	}
	if !d.b.begin() {
		return
	}
	name := o.ActionName
	if name == "" {
		name = o.Title
	}
	o.Guard.Go(name, o.Action, func(ok bool) {
		d.b.end(ok)
		if d.wake != nil {
			d.wake()
		}
	})
}

// UnsavedOptions 描述一次「有未保存的更改」的询问。
type UnsavedOptions struct {
	// Save 保存更改。在后台 goroutine 里经 Guard 执行，期间保存钮显示加载态、对话框不可关闭；
	// 成功后关闭并调用 OnSaved（UI 线程）；失败时 Guard 已弹出带原因的错误提示，对话框保持打开——
	// 保存失败了还让用户「不保存」地离开，等于悄悄丢数据。需要的数据在 Open 之前取好。
	Save       func() error
	ActionName string // 失败提示里的动作名（已本地化，如「保存笔记」）；空取「保存」
	Guard      Guard
	OnSaved    func()
	// OnDiscard 在用户选「不保存」后调用：对话框先关闭。
	OnDiscard func()
	// OnCancel 在用户取消（取消钮、Esc、点遮罩）后调用，可为 nil：对话框先关闭，留在原处继续编辑。
	OnCancel func()
}

// Unsaved 是「未保存的更改」对话框，对应渲染层 UnsavedChangesDialog：取消 / 不保存 / 保存三个选择。
// 初始焦点在「保存」。与确认框不同，Enter 只触发当前焦点的按钮（渲染层同样没有全局 Enter）。
type Unsaved struct {
	wake func()
	b    busy

	// 仅 UI 线程读写
	open  bool
	fresh bool // 刚打开、尚未给初始焦点
	opts  UnsavedOptions
}

// NewUnsaved 创建「未保存的更改」对话框；wake 同 NewConfirm。
func NewUnsaved(wake func()) *Unsaved { return &Unsaved{wake: wake} }

// Open 打开对话框（UI 线程）。
func (d *Unsaved) Open(o UnsavedOptions) { d.open, d.fresh, d.opts = true, true, o }

// Close 关闭对话框（UI 线程）。
func (d *Unsaved) Close() { d.open = false }

// Visible 报告对话框是否打开。
func (d *Unsaved) Visible() bool { return d.open }

// View 画对话框；没打开时什么都不画。
func (d *Unsaved) View(c *ui.Context, l *Look) {
	if d.b.takeFinished() {
		d.open = false
		if cb := d.opts.OnSaved; cb != nil {
			cb()
		}
	}
	if !d.open {
		return
	}
	loading := d.b.isRunning()
	spec := dialogSpec{
		width: unsavedWidth, title: locales.CommonUnsavedChanges(), message: locales.CommonUnsavedChangesMessage(),
		hasGlyph: true, glyph: icons.CircleAlert, glyphFg: tw("amber-600"), discBg: tw("amber-100"),
	}
	if l.Dark {
		spec.glyphFg = tw("amber-400")
		spec.discBg = over(tw("amber-900").Alpha(0.3), l.solid(l.T.UiPanelStrongBg))
	}

	var cancelHit, discardHit, saveHit bool
	var save ui.Element
	open := true
	dialogFrame(c, l, &open, spec, func(panel ui.Element) {
		ui.Row(c).Gap(12).Children(func() {
			if dialogButton(c, l, btnSecondary, locales.CommonCancel(), loading, false).Clicked() {
				cancelHit = true
			}
			if dialogButton(c, l, btnDiscard, locales.CommonDiscardChanges(), loading, false).Clicked() {
				discardHit = true
			}
			save = dialogButton(c, l, btnPrimary, locales.CommonSave(), loading, loading)
			if save.Clicked() {
				saveHit = true
			}
		})
	})
	if d.fresh { // 理由见 Confirm.focusInitial
		d.fresh = false
		save.Focus()
	}
	switch {
	case loading:
	case saveHit:
		d.save()
	case discardHit:
		d.open = false
		if d.opts.OnDiscard != nil {
			d.opts.OnDiscard()
		}
	case cancelHit || !open:
		d.open = false
		if d.opts.OnCancel != nil {
			d.opts.OnCancel()
		}
	}
}

func (d *Unsaved) save() {
	o := d.opts
	if o.Save == nil {
		d.open = false
		if o.OnSaved != nil {
			o.OnSaved()
		}
		return
	}
	if !d.b.begin() {
		return
	}
	name := o.ActionName
	if name == "" {
		name = locales.CommonSave()
	}
	o.Guard.Go(name, o.Save, func(ok bool) {
		d.b.end(ok)
		if d.wake != nil {
			d.wake()
		}
	})
}
