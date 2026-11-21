package kit

import (
	"strconv"
	"strings"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// 悬停提示（渲染层 components/ui/TooltipLayer.tsx，台账 B-011）
//
// 渲染层的 TooltipLayer 接管所有带 title 的元素：指针悬停（或键盘聚焦）到一个带 title 的元素上 320 ms 后，
// 在它下方 8px 处出一个自绘气泡（居中、夹回视口 8px 以内、下方放不下就翻到上方）；移开、按下、按键、滚动、窗口失焦都会收起；
// 点击过的元素在指针离开之前不再提示（点击后按钮拿到焦点，不能让气泡盖在这一下点开的菜单上）。
//
// 收起（渲染层的 dismiss()）之后宿主就被忘掉了，要等一个「新事件」才会再排期：指针进入别的元素（pointerover）、
// 键盘焦点换了（focusin）。所以按键、滚动、失焦收起之后，指针还停在原处、焦点还在原处时不会重新出气泡。
// 这里用 hushHover / hushFocus 记下收起那一刻的悬停与焦点，指针或焦点一换就解除。
//
// Viva 自带的 Box.Tooltip 是即时出现、自带样式与箭头，与渲染层的延迟、外观、位置都对不上，所以这里自己画：
// 宿主只需在命中区上声明提示文字（gfx.HitOptions.Hint，也是它的可访问描述），由窗口根上的提示层统一出气泡。
//
// 帧内的先后：气泡是否出现由「传感器」在绘制时按指针所在的命中区定下（那时本帧所有命中区都已登记），
// 下一帧才据此声明气泡；状态一变就请求一帧，所以看不见这一帧的滞后。

const (
	// TooltipDelay 是悬停到出气泡的等待（TooltipLayer 的 SHOW_DELAY_MS）。它是防误触的延迟、不是装饰，动效关闭档也不压缩。
	TooltipDelay = 320 * time.Millisecond

	tooltipMargin float32 = 8 // 气泡与视口边缘的最小留白（VIEWPORT_MARGIN）
	tooltipGap    float32 = 8 // 气泡与宿主的间距（TRIGGER_GAP）

	// activePoll 是有气泡显示（或排期）时探测窗口是否仍在前台的间隔。Viva 没有窗口失活的回调
	//（OnActivate 只在变成活动窗口时触发），渲染层的 window blur 监听只能靠轮询近似：失焦之后至多晚这么久才收起。
	activePoll = 250 * time.Millisecond

	// tabScrollWindow 是 Tab 触发滚动之后，对随后到达的新焦点「不出气泡」的有效期（B-032），从滚动那一帧起算。
	// 渲染层里新焦点的 focusin 与滚动引起的 scroll 事件是同一次 focus() 的先后两步，所以这个焦点不出气泡；
	// Viva 里滚动与聚焦隔着一到三帧（续走 Tab、AutoFocus 请求），这点时间足够，又短到不会压住之后真正无关的焦点变化。
	// 超时没有等到焦点也不要紧：到期即失效，退回「照常出气泡」。
	tabScrollWindow = 200 * time.Millisecond
)

// tooltipHost 是一个提示宿主的身份：带提示的命中区自己的 ID；由容器提供提示时（容器没有 ID）按它的外框与文字认。
type tooltipHost struct {
	id     gfx.ID
	bounds gfx.Rect
	text   string
}

// TooltipState 是提示层跨帧的状态，由窗口持有一份。
type TooltipState struct {
	host       tooltipHost   // 正在计时或正在显示的宿主
	since      time.Duration // 宿主进入悬停（或聚焦）的时刻
	anchor     gfx.Rect      // 宿主的外框：变了说明它被滚动或重排，气泡收起重新计时
	text       string        // 气泡文字（宿主的 Hint）
	suppressed gfx.ID        // 被按下过的宿主：指针离开它之前不再提示
	shown      bool          // 气泡是否应当显示

	// Active 报告窗口是否是前台窗口（渲染层 window blur 的对应物，见 activePoll）；nil 表示不探测（无头测试与对照场景）。
	// 只在有气泡显示或排期时调用。
	Active func() bool

	keyed      bool   // 有按键到达（Look.OnKey 经 noteKey 置位）：传感器下一次运行时收起
	hushHover  gfx.ID // 收起时指针所在的命中区：指针进入别的命中区之前不再提示
	hushFocus  gfx.ID // 收起时的键盘焦点：焦点换了之前不再提示
	lastFocus  gfx.ID // 上一次传感器运行时的键盘焦点：按键先于焦点移动，收起要记下「按键之前」的焦点
	wasActive  bool   // 上一次探测时窗口在前台
	waitActive bool   // 失焦收起之后在等窗口回到前台（回来时键盘焦点的控件像 focusin 那样重新排期）

	tabScroll      bool          // 刚有 Tab 为了把下一个停靠点滚进视野而滚动（TabKey 经 noteTabScroll 置位）：窗口内到达的新焦点不出气泡
	tabScrollBegun bool          // 有效期是否已经起算（传感器在滚动之后的第一次运行里起算）
	tabScrollUntil time.Duration // 有效期的终点
}

// noteTabScroll 记下「这个 Tab 先滚动、后聚焦」（B-032）：渲染层里聚焦的 focusin 紧接着被滚动的 scroll 事件收起，
// 新焦点不出气泡（见 tabScrollWindow）。传感器在之后的有效期内一直把键盘焦点当作「已收起」。
// 与有没有气泡在显示无关：旧的气泡由按键与滚动各自收起。
func (st *TooltipState) noteTabScroll() {
	st.tabScroll, st.tabScrollBegun = true, false
}

// endTabScroll 让「Tab 滚动」的压制失效：每次新的按键到达时先调（这次按键若又是滚动的 Tab，随后会重新置位）——
// 不带滚动的按键与 Tab 到达的焦点是新事件，照常排期。
func (st *TooltipState) endTabScroll() {
	st.tabScroll, st.tabScrollBegun = false, false
}

// noteKey 记下一次按键（渲染层 TooltipLayer 的 keydown 监听）：有气泡显示或排期时返回真，调用方要补一次重绘，
// 让传感器收起它；没有就什么也不做。
func (st *TooltipState) noteKey() bool {
	if !st.shown && st.host == (tooltipHost{}) {
		return false
	}
	st.keyed = true
	return true
}

// armPoll 在有气泡显示、排期或在等窗口回到前台时安排下一次探测。
func (st *TooltipState) armPoll(canvas *gfx.Canvas) {
	if st.Active != nil && (st.shown || st.host != (tooltipHost{}) || st.waitActive) {
		canvas.RequestFrameIn(activePoll)
	}
}

// hide 收起气泡并忘掉当前宿主。
func (st *TooltipState) hide(canvas *gfx.Canvas) {
	if st.shown || st.host != (tooltipHost{}) { // 宿主还在计时也算：它的描述要装回去（HintDescription）
		canvas.RequestFrame()
	}
	st.host, st.shown = tooltipHost{}, false
}

// HintGroup 声明一个带提示的分组：渲染层里挂着 title 的容器（没有自己的操作，里面的控件各管各的）。
// 提示文字同时是它的可访问描述（悬停期间除外，见 HintDescription）。它是一个不可聚焦的命中区
// （Viva 的 Container 不带提示字段），先于里面的控件登记（放在容器的 Draw 钩子里），所以里面的控件仍然先得到指针；
// 指针落在容器的空白处或里面没有提示的控件上时，宿主都是它（hintedHit 的祖先查找）。
func (l *Look) HintGroup(canvas *gfx.Canvas, id gfx.ID, bounds gfx.Rect, label, hint string) {
	canvas.HitWithOptions(id, bounds, label, gfx.HitOptions{
		Role: gfx.RoleGroup, Description: l.HintDescription(id, hint), Hint: hint, SkipFocus: true,
	})
}

// HintDescription 给带提示的命中区算它的可访问描述：渲染层的 title 同时是可访问描述，
// 但 TooltipLayer 在悬停（或聚焦）一开始就把宿主的 title 摘下来，移开才装回去——所以宿主在被提示期间没有描述。
// id 是命中区自己的 ID，hint 是它平时的描述（调用方已把「与名称相同」的情形折成空串）。
func (l *Look) HintDescription(id gfx.ID, hint string) string {
	if l.tips != nil && !id.IsZero() && l.tips.host.id == id {
		return ""
	}
	return hint
}

// hintedHit 找 id 这个命中区的提示宿主，第二个返回值为假表示没有：
//   - 命中区自己带提示，宿主就是它；
//   - 否则取包着它的最近一个带提示的分组（HintGroup：同一层、外框包住它、先于它登记）——对应渲染层
//     `closest("[title]")`：title 挂在容器上时，指针在容器里任何一处（包括里面的按钮上）都显示它。
func hintedHit(canvas *gfx.Canvas, id gfx.ID) (tooltipHost, bool) {
	if id.IsZero() {
		return tooltipHost{}, false
	}
	hits := canvas.Hits()
	for i := len(hits) - 1; i >= 0; i-- {
		h := hits[i]
		if h.ID != id || h.Block {
			continue
		}
		if h.Hint != "" {
			return tooltipHost{id: h.ID, bounds: h.Bounds, text: h.Hint}, true
		}
		for j := i - 1; j >= 0; j-- {
			a := hits[j]
			if a.Role == gfx.RoleGroup && a.Hint != "" && !a.Block && a.Layer == h.Layer && rectContains(a.Bounds, h.Bounds) {
				return tooltipHost{id: a.ID, bounds: a.Bounds, text: a.Hint}, true
			}
		}
		return tooltipHost{}, false
	}
	return tooltipHost{}, false
}

// update 是传感器：每一帧绘制时按指针与键盘焦点所在的命中区推进状态。scrolled 是这次绘制里有没有滚动区滚动过。
func (st *TooltipState) update(canvas *gfx.Canvas, scrolled bool) {
	now := canvas.Now()
	hovered, focused := canvas.Hovered(), canvas.FocusVisible()
	before := st.lastFocus
	st.lastFocus = focused
	defer st.armPoll(canvas)

	// 窗口失焦（window blur）：有气泡显示或排期时才探测，从前台变成不在前台的那一刻收起。
	// 只在「变化」时收起：窗口不在前台时指针悬停到别的宿主，渲染层照样出提示（pointerover 不看窗口焦点）。
	if st.Active != nil && (st.shown || st.host != (tooltipHost{}) || st.waitActive) {
		active := st.Active()
		pending := st.shown || st.host != (tooltipHost{})
		switch {
		case st.waitActive && active:
			st.waitActive, st.hushFocus = false, gfx.ID{} // 回到前台：键盘焦点的控件重新排期（focusin）
		case st.wasActive && !active && pending:
			st.hide(canvas)
			st.hushHover, st.hushFocus, st.waitActive = hovered, focused, true
			st.wasActive = false
			return
		}
		st.wasActive = active
	}

	// Tab 为了聚焦而先滚动（B-032）：渲染层的 focusin 之后紧跟 scroll，新焦点出不了气泡。
	// 有效期内每次运行都把当前键盘焦点记成「已收起」（焦点可能先后落在出发点、再落到目标上），期满后焦点保持原处，
	// 之后真正的焦点变化照常解除、排期。
	if st.tabScroll {
		switch {
		case !st.tabScrollBegun:
			st.tabScrollBegun, st.tabScrollUntil = true, now+tabScrollWindow
		case now >= st.tabScrollUntil:
			st.tabScroll, st.tabScrollBegun = false, false
		}
		if st.tabScroll {
			st.hushFocus = focused
			canvas.RequestFrameIn(tabScrollWindow) // 没有别的事件时也要有一帧收到期，不然压制会一直挂着
		}
	}

	// 按键（keydown，捕获阶段，任何键都算）：收起；指针没动、键盘焦点没换之前不再提示。
	// Tab 把焦点移走时，新焦点与 hushFocus（按键之前的焦点）不同，照常排期。
	if st.keyed {
		st.keyed = false
		st.hide(canvas)
		st.hushHover, st.hushFocus = hovered, before
		return
	}

	// 滚动（scroll，捕获阶段，任何滚动区都算）：收起。滚动期间内容在指针下面换来换去，不解除、也不重新记录——
	// 渲染层要等滚动停下、指针底下的元素确实换了（pointerover）才再排期。
	if scrolled {
		st.hide(canvas)
		if st.hushHover.IsZero() {
			st.hushHover = hovered
		}
		if st.hushFocus.IsZero() {
			st.hushFocus = focused
		}
		return
	}

	// 新事件：指针进入了别的命中区（pointerover）、键盘焦点换了（focusin），解除对应来源的「不再提示」。
	if hovered != st.hushHover {
		st.hushHover = gfx.ID{}
	}
	if focused != st.hushFocus {
		st.hushFocus = gfx.ID{}
	}

	// 按下：宿主在指针离开之前不再提示（pointerdown → pressedRef），并立即收起。
	if pressed := canvas.Pressed(); !pressed.IsZero() {
		st.suppressed = pressed
		st.hide(canvas)
		return
	}
	if !st.suppressed.IsZero() {
		if hovered == st.suppressed {
			st.hide(canvas)
			return
		}
		st.suppressed = gfx.ID{}
	}

	// 宿主：指针所在的带提示的命中区（或包着它的带提示的容器）；没有就看键盘聚焦的那个（focusin 同样排期）。
	// 收起之后还没有新事件的那一路（hushHover / hushFocus）不当宿主。
	if hovered == st.hushHover {
		hovered = gfx.ID{}
	}
	if focused == st.hushFocus {
		focused = gfx.ID{}
	}
	host, ok := hintedHit(canvas, hovered)
	if !ok {
		host, ok = hintedHit(canvas, focused)
	}
	if !ok {
		st.hide(canvas)
		return
	}

	// 换了宿主，或宿主被滚走（外框变了）：从头计时。指针在同一个宿主里的子元素之间移动不算换宿主。
	if host != st.host {
		// 宿主一换，描述也跟着变（HintDescription）：无论气泡显没显示，都要有一帧把新的描述声明出去。
		canvas.RequestFrame()
		st.host, st.since, st.shown = host, now, false
		if st.Active != nil {
			st.wasActive = st.Active() // 窗口此刻不在前台时，气泡照常出（渲染层不看窗口焦点），之后才有「失焦」可言
		}
	}
	st.anchor, st.text = host.bounds, host.text

	if remaining := TooltipDelay - (now - st.since); remaining > 0 {
		canvas.RequestFrameIn(remaining)
		return
	}
	if !st.shown {
		st.shown = true
		canvas.RequestFrame() // 下一帧声明气泡
	}
}

// rectContains 报告 outer 是否（含边界，容差 0.5px）完全包住 inner。
func rectContains(outer, inner gfx.Rect) bool {
	const eps = 0.5
	return inner.X >= outer.X-eps && inner.Y >= outer.Y-eps &&
		inner.Right() <= outer.Right()+eps && inner.Bottom() <= outer.Bottom()+eps
}

// tooltipRect 按 TooltipLayer 的算法定位气泡：水平居中对着宿主，中心夹在「半宽 + 8」与「视口宽 − 半宽 − 8」之间；
// 垂直放在宿主下方 8px，下方放不下（下边缘 + 8 越过视口底）就翻到上方，上方也放不下时贴着视口顶部的 8px 留白。
// 锚点是宿主的上边缘：按下边缘算会低整整一个宿主的高度，气泡直接压在宿主身上。
func tooltipRect(anchor gfx.Rect, w, h, vw, vh float32) gfx.Rect {
	center := anchor.X + anchor.Width/2
	left := min(max(center, w/2+tooltipMargin), vw-w/2-tooltipMargin)
	below := anchor.Y + anchor.Height + tooltipGap
	top := below
	if below+h+tooltipMargin > vh {
		top = max(tooltipMargin, anchor.Y-tooltipGap-h)
	}
	return gfx.Rect{X: left - w/2, Y: top, Width: w, Height: h}
}

// tooltipSourceLines 是气泡文字按换行符切出的逻辑行：行内连续空白折成一个空格（white-space: pre-line）。
func tooltipSourceLines(text string) []string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for i, line := range lines {
		lines[i] = strings.Join(strings.Fields(line), " ")
	}
	return lines
}

// tooltipMaxContent 是 w-max 的内容宽度：每个逻辑行不折行时的宽度里最宽的一个。
// 气泡宽度由它（再夹到 max-w-xs）决定，而不是折行之后最宽的那一行——折行后行尾放不满，
// 按后者算会让气泡比渲染层窄。
func tooltipMaxContent(text string, measure func(string) float32) float32 {
	var content float32
	for _, line := range tooltipSourceLines(text) {
		content = max(content, measure(line))
	}
	return content
}

// tooltipLines 把气泡文字排成行：换行符保留，放不下的行在最大宽度内按断行机会折行（break-words）。
func tooltipLines(text string, width float32, measure func(string) float32) []string {
	var out []string
	for _, line := range tooltipSourceLines(text) {
		if measure(line) <= width {
			out = append(out, line)
			continue
		}
		// 折行处的空格悬挂在行尾，不占宽度也不显示。
		for _, wrapped := range WrapText(line, width, measure) {
			out = append(out, strings.TrimRight(wrapped, " "))
		}
	}
	return out
}

// Tooltips 声明提示层：放在窗口根上、所有页面与浮层之后。传感器是一块不吸收指针的整窗浮层，
// 气泡是它之上的一块面板（pointer-events-none：不挡任何点击）。
func (l *Look) Tooltips(c ui.C, st *TooltipState) {
	l.tips = st
	bounds := c.Bounds()
	ui.Overlay(c.Key("tooltip-sensor"), ui.OverlaySpec{Band: bandTooltip, Bounds: bounds}).
		Draw(func(d *ui.DrawContext) { st.update(d.Canvas, l.takeScrolled()) })
	if st.shown {
		l.tooltipBubble(c.Key("tooltip-bubble"), st, bounds)
	}
}

// tooltipBubble 声明气泡：rounded-md border border-border bg-popover px-2 py-1 text-xs leading-relaxed text-popover-foreground
// shadow-lg，w-max max-w-xs，淡入 duration-quick。
func (l *Look) tooltipBubble(c ui.C, st *TooltipState, viewport gfx.Rect) {
	t := l.T
	size := l.Rem(0.75)
	line := size * 1.625 // leading-relaxed
	padX, padY := l.Space(2), l.Space(1)
	maxWidth := l.Rem(20) // max-w-xs（border-box）
	canvas := c.Canvas()
	measure := func(s string) float32 { return canvas.TextWidthWeight(s, size, WeightNormal) }

	lines := tooltipLines(st.text, maxWidth-2-2*padX, measure)
	width := min(tooltipMaxContent(st.text, measure)+2*padX+2, maxWidth) // w-max max-w-xs：折行不缩窄气泡
	height := float32(len(lines))*line + 2*padY + 2
	rect := tooltipRect(st.anchor, width, height, viewport.Width, viewport.Height)

	surface := Surface{Bg: t.Popover, Border: t.Border, BorderW: 1, Radius: l.Rem(0.75) - 2, Shadows: ShadowLG}
	id := ui.ID(c)
	motion := l.Motion(DurationQuick, EaseDefault)
	text := st.text
	l.floatingPanel(c, bandTooltip, rect).
		PadSides(1+padY, 1+padX, 1+padY, 1+padX).
		Draw(func(d *ui.DrawContext) {
			enterPresence(d.Canvas, id, 0, motion, 1, center(d.Rect))
			l.PaintSurface(d, surface)
			d.Canvas.ContainerWithOptions(d.Rect, gfx.RoleGroup, strings.TrimSpace(text), gfx.HitOptions{})
		}).
		DrawOver(func(d *ui.DrawContext) { d.Canvas.EndPresence() }).
		Children(func() {
			for i, ln := range lines {
				l.TextPx(c.Key("line"+strconv.Itoa(i)), ln, size, line, WeightNormal, t.PopoverForeground)
			}
		})
}
