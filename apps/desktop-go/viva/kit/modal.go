package kit

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/tokens"
)

// Modal（渲染层 components/ui/Modal.tsx）
//
// 模态窗口：整窗的半透明遮罩（bg-background/60 加 backdrop-blur-md）与居中的面板。
//   - 面板外观取 app-wallpaper-panel-strong：令牌背景、边框、阴影（带 !important，盖掉 shadow-[0_0_100px_…] 与 border-border），圆角 rounded-2xl；
//   - 页眉（有标题时）：px-6 py-4，下边框，外观取 app-wallpaper-surface；标题 text-xl font-bold，副标题 text-xs 至多两行，右侧是页眉操作与关闭钮；
//   - 内容区：min-h-0 flex-1，默认 p-6 且纵向可滚动（面板高度随内容增长，最高 85vh）；
//   - Esc 与点遮罩关闭（可分别关掉）；Tab 在面板内循环；关闭后焦点回到打开之前的控件；
//   - 入场：遮罩淡入、面板淡入 + 从 96% 放大 + 自下 16px 滑入，180 ms ease-enter；退场 120 ms ease-exit（渲染层 200 ms 后卸载）；
//   - 遮罩后面的模糊没有（台账 A-007：Viva 没有模糊原语），其余与渲染层一致。

// ModalSize 是 Modal 的尺寸档（SIZE_CONFIG）。零值是默认的 md。
type ModalSize uint8

const (
	ModalMD ModalSize = iota
	ModalSM
	ModalLG
	ModalXL
	Modal2XL
	ModalFull
	// ModalFullscreen 四周各留 64px，宽高随窗口（calc(100vw - 128px)）。
	ModalFullscreen
)

// maxWidth 是尺寸档的最大宽度（px 字面量，不随字号档缩放）。
func (s ModalSize) maxWidth() float32 {
	switch s {
	case ModalSM:
		return 400
	case ModalLG:
		return 600
	case ModalXL:
		return 800
	case Modal2XL:
		return 1000
	case ModalFull:
		return 1200
	default:
		return 500
	}
}

// ModalSpec 描述一个 Modal。Open 为假时什么都不画。
type ModalSpec struct {
	Open bool
	Size ModalSize
	// Title 为空时没有页眉（也没有关闭钮）；此时可访问名取 DialogLabel（渲染层 aria-label=t("common.dialog")）。
	Title       string
	DialogLabel string
	// Subtitle 画页眉标题下面的副标题（text-xs muted，至多两行）；SubtitleText 是它的纯文字，作对话框的可访问描述。
	Subtitle     func(c ui.C)
	SubtitleText string
	// HeaderActions 画页眉右侧、关闭钮之前的操作。
	HeaderActions func(c ui.C)
	// CloseLabel 是关闭钮的可访问名（t("common.close")）。
	CloseLabel string
	// NoClose 隐藏关闭钮（showCloseButton=false）；NoBackdropClose、NoEscape 关掉点遮罩与 Esc 关闭。
	NoClose, NoBackdropClose, NoEscape bool
	// RawBody 对应 contentClassName 覆盖了默认的 `p-6 overflow-y-auto`：内容区不加内边距也不滚动，由内容自己处理。
	RawBody bool
	// Body 画内容区。
	Body func(c ui.C)
}

// ModalState 是 Modal 的界面状态：由持有它的屏幕保存。每一帧都要经过 Modal，Open 为假的那一帧复位。
type ModalState struct {
	opened bool
	scope  gfx.ID // 打开时登记在 Look.modalScope 里的焦点作用域，关闭时据此撤销
}

const (
	modalViewportMaxHeight = 0.85 // maxHeight: 85vh
	modalEnterScale        = 0.96 // scale-enter-from
	modalSlide             = 16   // translate-y-4
	modalBackdropAlpha     = 0.6  // bg-background/60
	modalHeaderPadX        = 6    // px-6（Space 刻度）
	modalHeaderPadY        = 4    // py-4
)

// Modal 声明模态窗口，返回这一帧是否被要求关闭（点了关闭钮、遮罩，或按了 Esc）。
// 调用方在收到 true 时自己把 Open 置假：关闭是否成立（例如有未保存的修改）由它决定。
func (l *Look) Modal(c ui.C, st *ModalState, spec ModalSpec) bool {
	if !spec.Open {
		if st.opened && l.modalScope == st.scope {
			l.modalScope = gfx.ID{}
		}
		st.opened = false
		return false
	}
	t := l.T
	closed := false

	enter := l.Motion(DurationBase, EaseEnter)
	exit := l.Motion(DurationQuick, EaseExit)

	label := spec.Title
	if label == "" {
		label = spec.DialogLabel
	}

	var (
		scope    = ui.ID(c.Key("scope"))
		scrimID  = ui.ID(c.Key("scrim"))
		radius   = l.Rem(1) // rounded-2xl
		surface  = Surface{Bg: t.UiPanelStrongBg, Border: t.UiPanelStrongBorder, BorderW: 1, Radius: radius, Shadow: t.UiPanelStrongShadow}
		fullscr  = spec.Size == ModalFullscreen
		inset    = l.Space(4) // p-4
		bounds   = c.Bounds()
		maxWidth = spec.Size.maxWidth()
		slide    = gfx.Point{Y: modalSlide}
	)
	if fullscr {
		inset = l.Space(16) // p-16：离开系统窗口控件
	}
	// 整窗一层：遮罩（吸收背后的点击）与面板在同一个焦点作用域里，与 Viva 自带 Dialog 同一套次序——
	// 先进入作用域、画遮罩并登记它的命中区，面板随后声明，最后在作用域还开着时认领焦点陷阱（Tab 与指针都只在窗口里）。
	host := ui.Overlay(c.Key("host"), ui.OverlaySpec{Band: bandModal, Bounds: bounds}).
		Justify(ui.Center).Items(ui.Center).Pad(inset).
		Draw(func(d *ui.DrawContext) {
			d.Canvas.PushFocusScope(scope)
			l.paintModalScrim(d, scrimID, enter, exit, spec.NoEscape)
		}).
		DrawOver(func(d *ui.DrawContext) {
			d.Canvas.TrapFocus(scope)
			d.Canvas.PopFocusScope()
		})
	l.modalScope, st.scope = scope, scope
	host.Children(func() {
		// 面板：border（1px）在盒内，页眉与内容区缩进一个边框宽（Chromium 的 border-box + overflow-hidden）。
		panel := ui.Column(c.Key("panel")).FillWidth().MaxWidth(maxWidth).MinHeight(0).Pad(1)
		if fullscr {
			panel = panel.MaxWidth(bounds.Width - 2*inset).Height(bounds.Height - 2*inset)
		} else {
			panel = panel.MaxHeight(bounds.Height * modalViewportMaxHeight)
		}
		panel = panel.Clip().
			Draw(func(d *ui.DrawContext) {
				modalPresence(d.Canvas, scope, enter, exit, slide, center(d.Rect))
				l.PaintSurface(d, surface)
				// 面板是不透明的：按在面板上不能落到下面的遮罩（那会关闭窗口）。
				d.Canvas.Block(d.Rect)
				d.Canvas.ContainerWithOptions(d.Rect, gfx.RoleGroup, label, gfx.HitOptions{Description: spec.SubtitleText})
			}).
			DrawOver(func(d *ui.DrawContext) { d.Canvas.EndPresence() })
		panel.Children(func() {
			if spec.Title != "" {
				if l.modalHeader(c.Key("header"), spec, radius-1) {
					closed = true
				}
			}
			body := func() {
				if spec.Body != nil {
					spec.Body(c)
				}
			}
			if spec.RawBody {
				ui.Column(c.Key("body")).FillWidth().Grow(1).Shrink(1).MinHeight(0).Children(body)
				return
			}
			pad := l.Space(6) // p-6
			// 内容区的高度随内容增长，直到面板的 85vh：滚动容器自己不报内容高度，除非给它设个上限（Viva 的约定）。
			l.ScrollArea(c.Key("body"), [4]float32{pad, pad, pad, pad}, body).
				Grow(1).Shrink(1).MinHeight(0).MaxHeight(bounds.Height * modalViewportMaxHeight)
		})
	})
	st.opened = true

	// 点遮罩与 Esc 都以对遮罩的一次点击送达：指针点的带点击次数，Esc 的次数为 0；两者可分别关掉。
	if canvas := c.Canvas(); canvas.Clicked(scrimID) {
		if info, _ := canvas.ClickInfo(); info.Clicks > 0 {
			closed = closed || !spec.NoBackdropClose
		} else {
			closed = closed || !spec.NoEscape
		}
	}
	return closed
}

// paintModalScrim 画遮罩（bg-background/60，随窗口淡入淡出）并登记它的命中区；Esc 作用于它。
func (l *Look) paintModalScrim(d *ui.DrawContext, id gfx.ID, enter, exit gfx.Motion, noEscape bool) {
	canvas := d.Canvas
	if enter.Duration > 0 {
		fadePresence(canvas, id, enter, exit, center(d.Rect))
	}
	canvas.Fill(d.Rect, d.Fade(l.Col(l.T.Background.WithAlpha(modalBackdropAlpha))))
	if enter.Duration > 0 {
		canvas.EndPresence()
	}
	canvas.HitWithOptions(id, d.Rect, "", gfx.HitOptions{Role: gfx.RoleButton, SkipFocus: true, Backdrop: true})
	if !noEscape {
		canvas.SetEscapeTarget(id)
	}
}

// pushModalScope 让一个浮层的命中区归入打开着的 Modal 的焦点作用域，返回配对的弹出函数；没有 Modal 时什么都不做。
// Modal 把焦点与指针限制在自己的作用域里，渲染层里高于它的浮层（Toast）仍要能点，所以它们借用同一个作用域。
func (l *Look) pushModalScope(canvas *gfx.Canvas) func() {
	if l.modalScope.IsZero() {
		return func() {}
	}
	canvas.PushFocusScope(l.modalScope)
	return canvas.PopFocusScope
}

// modalPresence 开启面板的入场：淡入 + 从 96% 放大 + 自下 16px 滑入（180 ms，ease-enter）；入场播完之后改用退场的
// 时长与曲线（120 ms，ease-exit），撤掉面板时按它回放（渲染层的退场用 duration-quick ease-exit 走回起始态）。
func modalPresence(canvas *gfx.Canvas, id gfx.ID, enter, exit gfx.Motion, slide, pivot gfx.Point) {
	if enter.Duration <= 0 {
		canvas.BeginPresence(id, 0, gfx.Presence{}, pivot)
		return
	}
	motion := exit
	if _, playing := canvas.PlayOnce(id, 0, true, enter.Duration); playing {
		motion = enter
	}
	canvas.BeginPresence(id, 0, gfx.Presence{Motion: motion, Fade: true, Scale: modalEnterScale, SlideIn: slide, SlideOut: slide}, pivot)
}

// modalHeader 声明页眉并返回关闭钮是否被点击。innerRadius 是面板圆角减去边框：页眉的上圆角跟着面板的 overflow-hidden 走。
func (l *Look) modalHeader(c ui.C, spec ModalSpec, innerRadius float32) bool {
	t := l.T
	closed := false
	padX, padY := l.Space(modalHeaderPadX), l.Space(modalHeaderPadY)
	ui.Row(c).FillWidth().Shrink(0).Items(ui.Center).Justify(ui.Between).
		PadSides(padY, padX, padY+1, padX). // 下边框 1px 在盒内
		Draw(func(d *ui.DrawContext) {
			scale := d.Canvas.PixelScale()
			r := SnapRect(d.Rect, scale)
			// 页眉背景：只有上面两个角随面板圆角（overflow-hidden），下面是直边——用页眉的外框裁剪一块向下延长的圆角矩形。
			d.Canvas.Clip(r)
			ext := gfx.Rect{X: r.X, Y: r.Y, Width: r.Width, Height: r.Height + innerRadius}
			if t.UiSurfaceBg.A > 0 {
				d.Canvas.Rounded(ext, innerRadius, d.Fade(l.Col(t.UiSurfaceBg)))
			}
			d.Canvas.Unclip()
			d.Canvas.Fill(gfx.Rect{X: r.X, Y: r.Y + r.Height - 1, Width: r.Width, Height: 1}, d.Fade(l.Col(t.UiSurfaceBorder)))
		}).
		Children(func() {
			// 左：标题与副标题（flex-1 min-w-0）。
			ui.Column(c.Key("titles")).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				l.modalTitle(c.Key("title"), spec.Title)
				if spec.Subtitle != nil {
					ui.Column(c.Key("subtitle")).FillWidth().MarginTop(l.Space(0.5)).Children(func() { spec.Subtitle(c) })
				}
			})
			// 右：页眉操作、分隔线与关闭钮（ml-4 gap-3，不收缩）。
			ui.Row(c.Key("actions")).Shrink(0).MarginLeft(l.Space(4)).Items(ui.Center).Justify(ui.End).Gap(l.Space(3)).Children(func() {
				if spec.HeaderActions != nil {
					spec.HeaderActions(c)
				}
				if spec.NoClose {
					return
				}
				if spec.HeaderActions != nil {
					// w-[1px] h-4 bg-border mx-1
					ui.Row(c.Key("divider")).Size(1, l.Space(4)).Shrink(0).MarginSides(0, l.Space(1), 0, l.Space(1)).
						Draw(func(d *ui.DrawContext) { l.PaintSurface(d, Surface{Bg: t.Border}) })
				}
				if l.modalClose(c.Key("close"), spec.CloseLabel) {
					closed = true
				}
			})
		})
	return closed
}

// modalTitle 是页眉标题：text-xl font-bold tracking-tight text-foreground truncate，二级标题。
// tracking-tight（-0.025em）Viva 的文字引擎没有字距，按自然字宽排（台账 A-013）。
func (l *Look) modalTitle(c ui.C, title string) ui.Box {
	return l.Heading(c, 2, title, TextXL, WeightBold, l.T.Foreground, Ellipsis()).FillWidth()
}

// modalClose 是 Modal 页眉的关闭钮：p-2 rounded-xl text-muted-foreground hover:text-foreground hover:bg-accent，图标 w-5 h-5，transition duration-base。
func (l *Look) modalClose(c ui.C, label string) bool { return l.closeButton(c, label, DurationBase) }

// closeButton 是对话框页眉右端的关闭钮；dur 是悬停过渡的档位（Modal 写 transition duration-base，CloseDialog 只写 transition-colors，
// 用 Tailwind 的默认 150 ms）。
func (l *Look) closeButton(c ui.C, label string, dur DurationName) bool {
	t := l.T
	id := ui.ID(c)
	size := l.Rem(2.25) // p-2（8px）+ 20px 图标 + p-2
	box := ui.Row(c).Size(size, size).Shrink(0).Items(ui.Center).Justify(ui.Center).
		HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand})
	hovered := box.State().Hovered
	p := l.hoverTo(c.Canvas(), id, 0, hovered, dur)
	fg := MixToken(t.MutedForeground, t.Foreground, p)
	box = box.Draw(func(d *ui.DrawContext) {
		if d.State.FocusVisible {
			l.PaintRing(d, 2, l.ringColor(), l.Rem(0.75))
		}
		bg := tokens.Color{}
		if p > 0 {
			bg = t.Accent.WithAlpha(t.Accent.A * p)
		}
		l.PaintSurface(d, Surface{Bg: bg, Radius: l.Rem(0.75)})
	})
	box.Children(func() { l.Icon(c.Key("icon"), icons.X, l.Rem(1.25), fg) })
	return box.Clicked()
}
