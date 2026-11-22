package kit

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// 提示的外观与交互，复刻渲染层 components/ui/Toast.tsx（Tailwind 类见各处注释）。
//
// 与渲染层的差异（台账 A-007）：卡片背后的 backdrop-blur-md 没有对应原语，深色下 20% 不透明度的底色改为
// 垫一块不透明的页面底色再叠色，页面底色均匀时与渲染层逐像素一致；背后是图文混排的页面时，渲染层是一团模糊，
// 这里是平整的底色。

// toastDetailPx 是详情原文与「复制」按钮的字号：text-[11px]，px 字面量，不随字号档缩放。
const toastDetailPx float32 = 11

// toastTint 是一类提示的配色：渲染层写死的 Tailwind 色阶（浅色 *-50 / *-200，深色 *-900/20 / *-800，图标 *-500）。
type toastTint struct {
	fill, border, icon tokens.Color
	glyph              gfx.Icon
}

func (l *Look) toastTint(kind ToastKind) toastTint {
	var hue string
	var glyph gfx.Icon
	switch kind {
	case ToastSuccess:
		hue, glyph = "green", icons.CircleCheck
	case ToastError:
		hue, glyph = "red", icons.CircleX
	case ToastWarning:
		hue, glyph = "yellow", icons.TriangleAlert
	default:
		hue, glyph = "blue", icons.Info
	}
	tint := toastTint{icon: tokens.Tailwind(hue + "-500"), glyph: glyph}
	if l.Dark {
		tint.fill = tokens.Tailwind(hue + "-900").WithAlpha(0.2)
		tint.border = tokens.Tailwind(hue + "-800")
	} else {
		tint.fill = tokens.Tailwind(hue + "-50")
		tint.border = tokens.Tailwind(hue + "-200")
	}
	return tint
}

// wash 是淡淡盖上去的一层明暗相反色：浅色盖黑、深色盖白，两档强度各自指定（bg-black/5 dark:bg-white/10 一类写法）。
func (l *Look) wash(light, dark float32) tokens.Color {
	if l.Dark {
		return tokens.RGBA(255, 255, 255, dark)
	}
	return tokens.RGBA(0, 0, 0, light)
}

// Toasts 画窗口的提示：在页面视图里每帧调用一次，放在最后声明（它是覆盖层）。
// 先领走 Notifier 里排队的操作并推进计时，再按渲染层 Toast 的样子画：右下角、最新的在下、
// 错误与警告不自动消失、「查看详情」展开后可复制原文。没有提示时什么都不声明。
func (l *Look) Toasts(c ui.C, n *Notifier) {
	if n == nil {
		return
	}
	canvas := c.Canvas()
	now := canvas.Now()
	if next, ok := n.advance(now); ok {
		canvas.RequestFrameIn(next - now)
	}
	if len(n.toasts) == 0 {
		return
	}
	width := l.toastStackWidth(canvas, n.toasts)
	// 容器：fixed bottom-6 right-6 z-[99999] flex flex-col gap-3；宽度是最宽那张卡片的宽度，卡片都撑满它
	// 卡片的命中区借用打开着的 Modal 的作用域（渲染层里提示在窗口之上，窗口打开时照样能点关闭钮与复制）。
	var popScope func()
	host := ui.Overlay(c.Key("toasts"), ui.OverlaySpec{Band: bandToast, Bounds: c.Bounds()}).
		Justify(ui.End).Items(ui.End).Pad(l.Space(6)).
		Draw(func(d *ui.DrawContext) { popScope = l.pushModalScope(d.Canvas) }).
		DrawOver(func(d *ui.DrawContext) { popScope() })
	host.Children(func() {
		ui.Column(c.Key("stack")).Width(width).Gap(l.Space(3)).Children(func() {
			for _, t := range n.toasts {
				l.toastCard(c, n, t, width, now)
			}
		})
	})
}

// toastStackWidth 是容器的宽度：每张卡片 max-w-[26rem]，容器是 shrink-to-fit，取最宽的那张。
// 宽度要在声明之前就定下来：详情原文的折行取决于它（见 WrapMono）。
func (l *Look) toastStackWidth(canvas *gfx.Canvas, toasts []*toast) float32 {
	maxW := l.Rem(26)
	var w float32
	for _, t := range toasts {
		w = max(w, min(maxW, l.toastIntrinsic(canvas, t)))
	}
	return w
}

// toastIntrinsic 是一张卡片不受 max-w 限制时的宽度（max-content）：边框与内边距加上内容里最宽的一行。
func (l *Look) toastIntrinsic(canvas *gfx.Canvas, t *toast) float32 {
	chrome := 2 * (1 + l.Space(4))
	sm := l.Rem(0.875)
	// 主行：图标 + 文案 + [动作] + 关闭钮，之间 gap-3；关闭钮 = p-1 加 16px 图标
	row := l.Rem(1.25) + l.Space(3) + canvas.TextWidthWeight(t.message, sm, WeightSemibold) + l.Space(3) + l.Rem(1) + 2*l.Space(1)
	if t.action != "" {
		row += l.Space(3) + 2*l.Space(2) + canvas.TextWidthWeight(t.action, sm, WeightSemibold)
	}
	w := row
	if t.detail != "" {
		label := locales.CommonViewDetail()
		if t.expanded {
			label = locales.CommonCollapse()
		}
		toggle := l.Rem(0.875) + l.Space(0.5) + canvas.TextWidthWeight(label, l.Rem(0.75), WeightMedium)
		inner := toggle
		if t.expanded {
			inner = max(inner, 2*l.Space(2)+monoMaxContent(t.detail, toastDetailPx))
		}
		w = max(w, l.Space(8)+inner)
	}
	return chrome + w
}

// monoMaxContent 是详情原文不折行时最长一行的宽度。
func monoMaxContent(text string, size float32) float32 {
	var w float32
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		w = max(w, MonoWidth(expandTabs(line), size))
	}
	return w
}

// toastCard 声明一张提示卡片。
//
// 渲染层的生命周期：出现时 animate-in slide-in-from-right-10 fade-in duration-base ease-enter；关掉后加 leaving 类，
// animate-out slide-out-to-right-10 fade-out duration-quick ease-exit，再过 duration-quick + 20 ms 才卸载。
// tailwindcss-animate 不设 fill-mode，退场动画播完（120 ms）到卸载（140 ms）之间卡片会以原样回到画面里一下——
// 渲染层的真实行为，这里照做（leaving 的后 20 ms 画静态卡片）。
// 退场动画由 Viva 回放最后一帧：卡片这一帧不再声明 Presence，版面里用同样大小的空位占着，卸载时才一次让出。
func (l *Look) toastCard(c ui.C, n *Notifier, t *toast, width float32, now time.Duration) {
	key := "toast-" + strconv.FormatUint(t.seq, 10)
	if t.leaving && now-t.leaveAt < l.Dur(DurationQuick) {
		ui.Column(c.Key(key)).Height(t.size.Y)
		return
	}
	live := !t.leaving
	tint := l.toastTint(t.kind)
	radius := l.Rem(1) // rounded-2xl
	scope := ui.ID(c.Key("scope-" + key))
	card := ui.Column(c.Key(key)).PadX(1 + l.Space(4)).PadY(1 + l.Space(3)).
		Draw(func(d *ui.DrawContext) {
			t.size = gfx.Point{X: d.Rect.Width, Y: d.Rect.Height}
			if live {
				l.beginToastPresence(d, scope, t)
			}
			l.paintToastSurface(d, tint, radius)
			// 卡片是 pointer-events-auto：按在卡片的边距或文字上不能落到下面的页面
			d.Canvas.Block(d.Rect)
		}).
		DrawOver(func(d *ui.DrawContext) {
			if live {
				d.Canvas.EndPresence()
			}
		})
	card.Children(func() {
		l.toastMainRow(c, n, t, tint, now)
		if t.detail != "" {
			l.toastDetail(c, n, t, width, now)
		}
	})
}

// beginToastPresence 开启卡片的入退场：右侧滑入并淡入、向右滑出并淡出，偏移 10 × 0.25rem。
//
// Viva 的 Presence 把入场与退场共用一个 Motion：入场的参数在入场开始那一帧取走，之后每一帧改交退场的参数，
// 撤掉卡片时 Viva 按最后一帧的参数回放退场（见 Notifier.toast.painted）。
func (l *Look) beginToastPresence(d *ui.DrawContext, scope gfx.ID, t *toast) {
	motion := l.Motion(DurationBase, EaseEnter)
	if t.painted {
		motion = l.Motion(DurationQuick, EaseExit)
	}
	t.painted = true
	offset := gfx.Point{X: l.Rem(2.5)}
	d.Canvas.BeginPresence(scope, 0, gfx.Presence{Motion: motion, Fade: true, SlideIn: offset, SlideOut: offset}, center(d.Rect))
}

// paintToastSurface 画卡片的底：垫层、阴影（shadow-2xl）、底色与边框。
func (l *Look) paintToastSurface(d *ui.DrawContext, tint toastTint, radius float32) {
	if tint.fill.A < 1 {
		// backdrop-blur-md 的替身（A-007）：先垫一块不透明的页面底色，半透明的底色再叠在它上面。垫层在所有卡片阴影之下。
		d.Canvas.PushLayer(uint16(bandToastUnder))
		d.Canvas.Rounded(SnapRect(d.Rect, d.Canvas.PixelScale()), radius, d.Fade(l.Col(l.T.Background)))
		d.Canvas.PopLayer()
	}
	l.PaintSurface(d, Surface{Bg: tint.fill, Border: tint.border, BorderW: 1, Radius: radius, Shadows: Shadow2XL})
}

// pressable 声明一个带命中区的按钮盒子，返回盒子与悬停进度（0…1，transition-colors 150 ms）。
// 盒子的圆角、悬停底色与键盘聚焦的环（全局 button:focus-visible 的 ring-2 ring-primary/50）都在这里画。
// hoverBg 的透明度是悬停完成时的值；零值表示悬停不改底色。
func (l *Look) pressable(c ui.C, id gfx.ID, label string, radius float32, hoverBg tokens.Color, opts gfx.HitOptions) (ui.Box, float32) {
	box := ui.Row(c).HitWith(id, label, opts)
	p := l.hoverTo(c.Canvas(), id, 0, box.State().Hovered, DurationDefault)
	return box.Draw(func(d *ui.DrawContext) {
		if d.State.FocusVisible {
			l.PaintRing(d, 2, l.T.Primary.WithAlpha(0.5), radius)
		}
		if hoverBg.A > 0 && p > 0 {
			l.PaintSurface(d, Surface{Bg: hoverBg.WithAlpha(hoverBg.A * p), Radius: radius})
		}
	}), p
}

// toastMainRow 是卡片的主行：flex items-center gap-3，图标 / 文案 / [动作] / 关闭。
// 用 items-center：关闭按钮比文字高，顶对齐会让图标和文案贴着上沿、下面多出一截空白。
func (l *Look) toastMainRow(c ui.C, n *Notifier, t *toast, tint toastTint, now time.Duration) {
	_ = n
	ui.Row(c.Key("row")).Items(ui.Center).Gap(l.Space(3)).Children(func() {
		l.Icon(c.Key("icon"), tint.glyph, l.Rem(1.25), tint.icon)
		sm := l.Rem(0.875)
		l.TextPx(c.Key("message"), t.message, sm, sm*1.375, WeightSemibold, l.T.Foreground, Wrapped()).
			Grow(1).Shrink(1).MinWidth(0)
		if t.action != "" {
			// shrink-0 rounded-lg px-2 py-1 text-sm font-semibold text-primary hover:bg-primary/10
			id := ui.ID(c.Key("action-id"))
			box, _ := l.pressable(c.Key("action"), id, t.action, l.Rem(0.75), l.T.Primary.WithAlpha(0.1),
				gfx.HitOptions{Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand})
			box = box.Shrink(0).Items(ui.Center).PadX(l.Space(2)).PadY(l.Space(1))
			if box.Clicked() {
				if t.onAction != nil {
					t.onAction()
				}
				t.startLeave(now)
			}
			box.Children(func() {
				l.Text(c.Key("label"), t.action, TextSM, WeightSemibold, l.T.Primary)
			})
		}
		// shrink-0 rounded-lg p-1 hover:bg-black/10 dark:hover:bg-white/10；aria-label 是「关闭」
		id := ui.ID(c.Key("close-id"))
		side := l.Rem(1) + 2*l.Space(1)
		box, _ := l.pressable(c.Key("close"), id, locales.CommonClose(), l.Rem(0.75), l.wash(0.1, 0.1),
			gfx.HitOptions{Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand})
		box = box.Size(side, side).Shrink(0).Justify(ui.Center).Items(ui.Center)
		if box.Clicked() {
			t.startLeave(now)
		}
		box.Children(func() {
			l.Icon(c.Key("x"), icons.X, l.Rem(1), l.T.MutedForeground)
		})
	})
}

// bodyStrut 是正文行盒的基线：卡片里的内联按钮（详情折叠钮、复制钮）坐在继承自 body 的行盒里
// （字号 1rem、行高 1.5），行盒的高度与按钮的位置由这根「支柱」与按钮自己的基线共同决定。
// 返回基线到行盒顶的距离与行高。
func (l *Look) bodyStrut() (ascent, line float32) {
	scale := l.Scale
	if scale <= 0 {
		scale = 1
	}
	size, line := l.Rem(1), l.Rem(1.5)
	a := float32(ChromeBaselineRow(WeightNormal, float64(size*scale), float64(line*scale))) / scale
	return a, line
}

// inlineLine 算一行里只放一个 inline-flex 按钮时的行盒（Chromium 行盒算法）：按钮的外边距盒在基线之上
// aBtn、之下 dBtn（inline-flex 的基线是第一个子项的基线；图标没有文字基线，取它的下边缘）。
// 行盒高度 = 基线之上较高者 + 基线之下较深者；返回外边距盒顶到行盒顶的距离与行盒高度。
func (l *Look) inlineLine(aBtn, dBtn float32) (offset, height float32) {
	as, line := l.bodyStrut()
	ds := line - as
	top := max(as, aBtn)
	return top - aBtn, top + max(ds, dBtn)
}

// toastDetail 是折叠在主行下面的报错原文：pl-8（与文案对齐，不钻到图标下面）> mt-1.5 > 折叠钮 [+ 展开块]。
// 展开而不是直接铺开：提示语要短才有人读，但原因不能因此丢掉；「复制」是这一块的重点——
// 用户拿到原文才谈得上去搜或者提 issue。
func (l *Look) toastDetail(c ui.C, n *Notifier, t *toast, cardWidth float32, now time.Duration) {
	ui.Column(c.Key("detail")).PadLeft(l.Space(8)).Children(func() {
		ui.Column(c.Key("detail-box")).MarginTop(l.Space(1.5)).Children(func() {
			l.toastToggle(c, t)
			if t.expanded {
				l.toastDetailBody(c, n, t, cardWidth, now)
			}
		})
	})
}

// toastToggle 是「查看详情 / 收起」：inline-flex items-center gap-0.5 rounded-md text-xs font-medium
// text-muted-foreground transition-colors hover:text-foreground，图标 h-3.5 w-3.5 展开时转 90°（transition-transform）。
func (l *Look) toastToggle(c ui.C, t *toast) {
	chevron, line := l.Rem(0.875), l.Rem(1) // text-xs 的行高 1rem
	aBtn := line - (line-chevron)/2         // 基线 = 图标的下边缘
	off, height := l.inlineLine(aBtn, line-aBtn)
	ui.Row(c.Key("toggle-line")).Height(height).Items(ui.Start).Children(func() {
		id := ui.ID(c.Key("toggle-id"))
		label := locales.CommonViewDetail()
		if t.expanded {
			label = locales.CommonCollapse()
		}
		box, p := l.pressable(c.Key("toggle"), id, label, l.Rem(0.625), tokens.Color{},
			gfx.HitOptions{Role: gfx.RoleButton, Expandable: true, Expanded: t.expanded, Cursor: gfx.PointerCursorHand})
		box = box.Height(line).Items(ui.Center).Gap(l.Space(0.5)).MarginTop(off)
		if box.Clicked() {
			t.expanded = !t.expanded
		}
		fg := MixToken(l.T.MutedForeground, l.T.Foreground, p)
		target := float32(0)
		if t.expanded {
			target = 1
		}
		angle := c.Canvas().Animate(id, slotChevron, target, l.Motion(DurationDefault, EaseStandard))
		box.Children(func() {
			l.toastChevron(c.Key("chevron"), chevron, angle, fg)
			l.Text(c.Key("label"), label, TextXS, WeightMedium, fg)
		})
	})
}

// toastChevron 画折叠钮里的 ChevronRight：turn 是 0…1 的转动进度，1 即 rotate-90。
// 静止（0° 与 90°）时用精确栅格化的图标——lucide 的 chevron-down 正是 chevron-right 转 90° 的结果；只有转动中途才走 Viva 的旋转绘制。
func (l *Look) toastChevron(c ui.C, size, turn float32, color tokens.Color) ui.Box {
	col := l.Col(color)
	return ui.Slot(c).Size(size, size).Shrink(0).Draw(func(d *ui.DrawContext) {
		mark := l.markIcon(d)
		tint := d.Fade(col)
		switch {
		case turn <= 0.001:
			PaintIcon(d.Canvas, icons.ChevronRight, d.Rect, tint)
		case turn >= 0.999:
			PaintIcon(d.Canvas, icons.ChevronDown, d.Rect, tint)
		default:
			d.Canvas.DrawIconTransformed(icons.ChevronRight, d.Rect, tint, gfx.IconTransform{Rotate: math.Pi / 2 * turn})
		}
		l.reportIcon(d, mark)
	})
}

// toastDetailBody 是展开的原文块：mt-1.5 rounded-lg bg-black/5 p-2 dark:bg-white/5，
// 里面是等宽原文（max-h-40 可滚动）与「复制」按钮。
func (l *Look) toastDetailBody(c ui.C, n *Notifier, t *toast, cardWidth float32, now time.Duration) {
	body := ui.Column(c.Key("body")).MarginTop(l.Space(1.5)).Pad(l.Space(2)).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, Surface{Bg: l.wash(0.05, 0.05), Radius: l.Rem(0.75)})
		})
	body.Children(func() {
		// 原文宽度：卡片内宽 − pl-8 − 展开块的 p-2
		wrapW := cardWidth - 2*(1+l.Space(4)) - l.Space(8) - 2*l.Space(2)
		lineH, maxH := toastDetailPx*1.625, l.Rem(10) // leading-relaxed；max-h-40
		lines := WrapMono(t.detail, wrapW, toastDetailPx)
		scrolls := float32(len(lines))*lineH > maxH+0.01
		if scrolls {
			// 竖向滚动条占去 8px 宽度，折行要按变窄后的宽度重排
			lines = WrapMono(t.detail, wrapW-scrollbarPx, toastDetailPx)
		}
		text := strings.Join(lines, "\n")
		pre := func() {
			// MaxLines(0)：不限行数，并认 \n 为硬换行（单行文字会把换行符当空格）；每行都已按渲染层的宽度折好，Viva 不会再折
			box := ui.Text(c.Key("pre"), text).Mono().MaxLines(0).FontSize(toastDetailPx).LineHeight(lineH).
				Color(l.Col(l.T.MutedForeground)).FillWidth()
			// Mono 文字与渲染层 font-mono（Consolas 的行盒度量）的基线差零到一行（台账 B-022，见 baseline_mono.go）。
			shiftBaseline(box, MonoBaselineShift(toastDetailPx, toastDetailPx, lineH, l.Scale))
		}
		if scrolls {
			l.ScrollArea(c.Key("pre-scroll"), [4]float32{}, pre).Height(maxH)
		} else {
			ui.Column(c.Key("pre-box")).Children(pre)
		}
		l.toastCopyLine(c, n, t, now)
	})
}

// toastCopyLine 是「复制」按钮所在的行：按钮 mt-1.5 inline-flex items-center gap-1 rounded-md px-1.5 py-0.5
// text-[11px] font-medium text-muted-foreground hover:bg-black/5 hover:text-foreground dark:hover:bg-white/10。
func (l *Look) toastCopyLine(c ui.C, n *Notifier, t *toast, now time.Duration) {
	icon := l.Rem(0.75)
	content := toastDetailPx * 1.5 // 按钮文字的行高：从 body 继承的 1.5 倍
	py, mt := l.Space(0.5), l.Space(1.5)
	btnH := 2*py + max(icon, content)
	// 基线 = 图标的下边缘；外边距盒（含 mt-1.5）在基线之上 mt + 基线位置，之下 btnH − 基线位置
	baseline := py + (max(icon, content)-icon)/2 + icon
	off, height := l.inlineLine(mt+baseline, btnH-baseline)
	ui.Row(c.Key("copy-line")).Height(height).Items(ui.Start).Children(func() {
		id := ui.ID(c.Key("copy-id"))
		label := locales.CommonCopy()
		if t.copied {
			label = locales.ToastCopied()
		}
		box, p := l.pressable(c.Key("copy"), id, label, l.Rem(0.625), l.wash(0.05, 0.1),
			gfx.HitOptions{Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand})
		box = box.Height(btnH).Items(ui.Center).PadX(l.Space(1.5)).Gap(l.Space(1)).MarginTop(off + mt)
		if box.Clicked() {
			// 复制不了也没关系：原文就摊在上面，用户照样读得到
			if err := n.copyText(t.detail); err == nil {
				t.copied = true
				t.copyExpires = append(t.copyExpires, now+toastCopiedFor)
			}
		}
		fg := MixToken(l.T.MutedForeground, l.T.Foreground, p)
		box.Children(func() {
			l.Icon(c.Key("icon"), icons.Copy, icon, fg)
			l.TextPx(c.Key("label"), label, toastDetailPx, content, WeightMedium, fg)
		})
	})
}
