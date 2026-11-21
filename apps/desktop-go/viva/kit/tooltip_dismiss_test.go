package kit

import (
	"fmt"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// 悬停提示的收起（渲染层 TooltipLayer 的 dismiss()：keydown、scroll、window blur，台账 B-017）。
// 收起之后宿主被忘掉，要等新事件（指针进入别的元素、键盘焦点换了）才再排期：
// 所以收起之后指针还停在原处、焦点还在原处时不会重新出气泡。

// shown 报告气泡是否在画面上。
func (h *harness) shown(text string) bool {
	h.t.Helper()
	_, ok := h.bubble(text)
	return ok
}

// 按键收起，指针没动就不再出；指针离开再进入后恢复。
func TestTooltipKeyDismissesAndStaysDismissed(t *testing.T) {
	h, _ := openTipPage(t)
	h.moveTo(60, 36)
	h.advance(TooltipDelay + 10*time.Millisecond)
	if !h.shown(tipA) {
		t.Fatal("悬停 320 ms 后没有出气泡")
	}
	h.keys("escape")
	if h.shown(tipA) {
		t.Fatal("按键之后气泡仍在")
	}
	h.advance(2 * TooltipDelay)
	h.frames(2)
	if h.shown(tipA) {
		t.Fatal("指针没动，按键收起之后不应再出气泡（渲染层 dismiss 之后要等新的 pointerover）")
	}
	h.moveTo(500, 400) // 离开
	h.moveTo(60, 36)   // 再进入
	h.advance(TooltipDelay + 10*time.Millisecond)
	if !h.shown(tipA) {
		t.Fatal("指针离开再进入后应当重新提示")
	}
}

// 气泡还在排期（没到 320 ms）时按键：排期取消。
func TestTooltipKeyCancelsPendingTooltip(t *testing.T) {
	h, _ := openTipPage(t)
	h.moveTo(60, 36)
	h.advance(100 * time.Millisecond)
	h.keys("escape")
	h.advance(2 * TooltipDelay)
	h.frames(2)
	if h.shown(tipA) {
		t.Fatal("排期中按了键，气泡不该再出现")
	}
}

// Tab 把焦点移到另一个宿主：按键收起旧的，新焦点照常排期（渲染层 keydown 之后接着 focusin）。
func TestTooltipTabDismissesOldAndSchedulesNewFocus(t *testing.T) {
	h, _ := openTipPage(t)
	h.keys("Tab") // 焦点在甲
	h.advance(TooltipDelay + 10*time.Millisecond)
	h.frames(2)
	if !h.shown(tipA) {
		t.Fatal("聚焦 320 ms 后没有出气泡")
	}
	h.keys("Tab") // 焦点移到己（没有提示）
	if h.shown(tipA) {
		t.Fatal("焦点移走之后旧气泡仍在")
	}
	h.keys("Tab") // 焦点移到乙
	h.advance(TooltipDelay + 10*time.Millisecond)
	h.frames(2)
	if !h.shown(tipB) {
		t.Fatal("Tab 到新宿主 320 ms 后应当出它的气泡：按键收起不该连新焦点一起压住")
	}
}

// 焦点没换（按的不是 Tab）：键盘聚焦的宿主收起之后不再出，直到焦点换走再回来。
func TestTooltipKeyOnFocusedHostStaysDismissed(t *testing.T) {
	h, _ := openTipPage(t)
	h.keys("Tab")
	h.advance(TooltipDelay + 10*time.Millisecond)
	h.frames(2)
	if !h.shown(tipA) {
		t.Fatal("聚焦 320 ms 后没有出气泡")
	}
	h.keys("escape")
	h.advance(2 * TooltipDelay)
	h.frames(2)
	if h.shown(tipA) {
		t.Fatal("焦点没有换，按键收起之后不应再出气泡")
	}
	h.keys("shift+Tab") // 焦点离开
	h.keys("Tab")       // 再回到甲：focusin，重新排期
	h.advance(TooltipDelay + 10*time.Millisecond)
	h.frames(2)
	if !h.shown(tipA) {
		t.Fatal("焦点离开再回来后应当重新提示")
	}
}

// 没有气泡显示或排期时，按键不要求重绘（窗口里每个按键都经过这里，不能白白多出一帧）；有气泡时要求重绘。
func TestLookOnKeyAsksRepaintOnlyWhenATooltipIsUp(t *testing.T) {
	h, _ := openTipPage(t)
	press := gfx.KeyEvent{Key: gfx.KeyEscape}
	if res := h.look.OnKey(press); res.Repaint {
		t.Fatal("没有气泡时按键不该要求重绘")
	}
	h.moveTo(60, 36)
	if res := h.look.OnKey(press); !res.Repaint {
		t.Fatal("气泡在排期时按键应当要求重绘（让传感器取消它）")
	}
	h.frames(3)
	h.moveTo(500, 400)
	h.moveTo(60, 36)
	h.advance(TooltipDelay + 10*time.Millisecond)
	if res := h.look.OnKey(gfx.KeyEvent{Key: gfx.KeyEscape, Release: true}); res.Repaint {
		t.Fatal("松开按键不该收起气泡（渲染层只听 keydown）")
	}
	if !h.shown(tipA) {
		t.Fatal("松开按键之后气泡不该消失")
	}
}

// ---- 滚动 ----

// tipScrollPage 是一列带提示的宿主放在滚动区里：每个 40 高，提示文字各不相同。
type tipScrollPage struct {
	st      TooltipState
	hovered gfx.ID // 最近一次绘制时 Viva 报告的悬停目标
}

func tipOf(i int) string { return fmt.Sprintf("提示 %d", i) }

func (p *tipScrollPage) view(l *Look, c ui.C) {
	ui.Column(c.Key("page")).Fill().Draw(func(d *ui.DrawContext) { p.hovered = d.Canvas.Hovered() }).Children(func() {
		l.ScrollArea(c.Key("area"), [4]float32{}, func() {
			for i := 0; i < 30; i++ {
				hc := c.Key(fmt.Sprintf("h%d", i))
				l.TabStop(ui.Row(hc).FillWidth().Height(40).HitWith(ui.ID(hc), fmt.Sprintf("宿主%d", i), gfx.HitOptions{Role: gfx.RoleButton, Hint: tipOf(i)}))
			}
		}).Fill()
	})
	l.Tooltips(c, &p.st)
}

func openTipScrollPage(t *testing.T) (*harness, *tipScrollPage) {
	t.Helper()
	p := &tipScrollPage{}
	return openPage(t, still(), 800, 600, p.view), p
}

// scrollAt 在 (x, y) 处滚一下（指针留在那里），随后像窗口消息循环那样处理滚轮钩子请求的指针移动（pumpHover），再出几帧。
func (h *harness) scrollAt(x, y, dy float32) {
	h.t.Helper()
	if err := h.session.Move(x, y); err != nil {
		h.t.Fatalf("移动指针失败：%v", err)
	}
	if err := h.session.Scroll(gfx.ScrollEvent{X: x, Y: y, DeltaY: dy, Precise: true}); err != nil {
		h.t.Fatalf("滚动失败：%v", err)
	}
	if !h.rawWheel {
		h.pumpHover()
	}
	h.frames(3)
}

// 钉住 Viva v0.6.4 自己的行为（台账 B-031）：滚轮滚动之后悬停仍指着滚走的那个元素，直到下一次指针事件。
// 这就是窗口要用 RefreshHoverAfterWheel 补一次指针移动的原因；rawWheel 让 scrollAt 不替窗口补，露出 Viva 的本来面目。
// 升级 Viva 之后这条测试若失败，说明它已经会刷新悬停：删掉 B-031、wheel.go 与窗口里的接线。
func TestVivaWheelScrollDoesNotRefreshHover(t *testing.T) {
	h, p := openTipScrollPage(t)
	h.rawWheel = true
	h.moveTo(100, 100)
	before := p.hovered
	if before.IsZero() {
		t.Fatal("指针在宿主上，Viva 没有报告悬停目标")
	}
	h.scrollAt(100, 100, 70) // 宿主 2 滚走了，指针底下换成宿主 4
	if p.hovered != before {
		t.Fatal("滚轮之后 Viva 已经刷新了悬停：B-031 不再成立，删掉登记、wheel.go 与窗口里的接线")
	}
	if len(h.hoverPosts) != 1 {
		t.Fatalf("滚轮钩子应当请求一次指针移动，实际 %d 次", len(h.hoverPosts))
	}
	h.pumpHover()
	if p.hovered == before {
		t.Fatal("滚动之后补一次指针移动，悬停应当换到指针底下的新宿主")
	}
}

// 滚动收起气泡；滚完之后指针底下还是同一个元素就不再出（没有新的 pointerover），
// 换成了别的元素就像新悬停一样排期。
func TestTooltipScrollDismissesUntilPointerMeetsAnotherHost(t *testing.T) {
	h, _ := openTipScrollPage(t)
	h.moveTo(100, 100) // 宿主 2（y 80..120）
	h.advance(TooltipDelay + 10*time.Millisecond)
	h.frames(2)
	if !h.shown(tipOf(2)) {
		t.Fatal("悬停 320 ms 后没有出气泡")
	}
	h.scrollAt(100, 100, 10) // 宿主 2 滚到 70..110，指针仍在它身上
	if h.shown(tipOf(2)) {
		t.Fatal("滚动之后气泡仍在")
	}
	h.advance(2 * TooltipDelay)
	h.frames(2)
	if h.shown(tipOf(2)) {
		t.Fatal("滚完之后指针仍在同一个宿主上，不应再出气泡")
	}
	h.scrollAt(100, 100, 60) // 共 70：指针底下换成宿主 4（90..130）
	h.advance(TooltipDelay + 10*time.Millisecond)
	h.frames(2)
	if !h.shown(tipOf(4)) {
		t.Fatal("滚完之后指针底下换了宿主，应当 320 ms 后出新宿主的气泡")
	}
}

// 滚动进行中不出气泡：每一步滚动都收起，停下之后才排期。
func TestTooltipNotShownWhileScrolling(t *testing.T) {
	h, _ := openTipScrollPage(t)
	h.moveTo(100, 100)
	for i := 0; i < 8; i++ {
		h.scrollAt(100, 100, 17)
		h.advance(100 * time.Millisecond) // 每步之间不到 320 ms，累计远超
		for k := 0; k < 30; k++ {
			if h.shown(tipOf(k)) {
				t.Fatalf("第 %d 步滚动中出了气泡 %d", i+1, k)
			}
		}
	}
	h.scrollAt(100, 100, 50) // 最后一步让指针底下换一个宿主
	h.advance(TooltipDelay + 10*time.Millisecond)
	h.frames(2)
	n := 0
	for i := 0; i < 30; i++ {
		if h.shown(tipOf(i)) {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("滚动停下 320 ms 后应当恰有一个气泡，实际 %d 个", n)
	}
}

// 键盘聚焦的提示也被滚动收起：焦点没换，滚完之后不再出（渲染层 scroll 之后宿主被忘掉，focusin 不会重发）。
func TestTooltipScrollDismissesFocusTooltip(t *testing.T) {
	h, _ := openTipScrollPage(t)
	h.keys("Tab") // 焦点在宿主 0（y 0..40）
	h.advance(TooltipDelay + 10*time.Millisecond)
	h.frames(2)
	if !h.shown(tipOf(0)) {
		t.Fatal("聚焦 320 ms 后没有出气泡")
	}
	h.scrollAt(600, 300, 10) // 指针在宿主外的空白（右侧），宿主 0 仍有一半在视野里，焦点不动
	if h.shown(tipOf(0)) {
		t.Fatal("滚动之后气泡仍在")
	}
	h.advance(3 * TooltipDelay)
	h.frames(2)
	if h.shown(tipOf(0)) {
		t.Fatal("焦点没有换，滚动收起之后不应再出气泡")
	}
}

// Tab 要先把下一个停靠点滚进视野再聚焦时不出气泡（台账 B-032，2026-11-22 在 React 里实测：focusin 之后 0.3 ms 就是 scroll 事件，
// 排期被 dismiss() 取消，之后 560 ms 内 DOM 里没有 [role=tooltip]，证据 b032-probe-prescroll.json）。
// 此前 Viva 先滚动（那一帧收起）、之后焦点才落到新控件，新焦点照常在 320 ms 后出气泡；现由 TabKey 通知提示层压住它。
func TestTooltipTabThatScrollsDoesNotShowTheFocusTooltip(t *testing.T) {
	h, _ := openTipScrollPage(t)
	for i := 0; i < 15; i++ {
		h.keys("Tab") // 宿主 0…14 都在视野里（600 / 40 = 15 个），不滚动
	}
	h.keys("Tab") // 宿主 15 完全在视野之外：先滚进视野，再聚焦
	if n, ok := h.node(gfx.RoleButton, "宿主15"); !ok || !n.Focused {
		t.Fatal("前提不成立：第 16 次 Tab 之后焦点应当落在宿主 15 上")
	}
	h.advance(TooltipDelay + 10*time.Millisecond)
	h.frames(2)
	if h.shown(tipOf(15)) {
		t.Fatal("Tab 滚动之后聚焦的控件出了气泡：渲染层里 focusin 紧接着被 scroll 收起，不出气泡")
	}
	h.advance(10 * TooltipDelay)
	h.frames(2)
	if h.shown(tipOf(15)) {
		t.Fatal("压制期过后焦点仍在原处，不该补出气泡（渲染层没有新的 focusin）")
	}
}

// 边界一：压制只管「滚动的那个 Tab」带来的焦点。不需要滚动的 Tab 正常出气泡（15 次 Tab 都在视野里，第 15 次的气泡照常出）。
func TestTooltipTabWithoutScrollStillShowsTheFocusTooltip(t *testing.T) {
	h, _ := openTipScrollPage(t)
	for i := 0; i < 15; i++ {
		h.keys("Tab")
	}
	h.advance(TooltipDelay + 10*time.Millisecond)
	h.frames(2)
	if !h.shown(tipOf(14)) {
		t.Fatal("不需要滚动的 Tab 到达的焦点没有出气泡")
	}
}

// 边界二：滚动的 Tab 之后再按一次不需要滚动的 Tab，到达的下一个焦点是新事件，照常出气泡（压制不跨按键）。
func TestTooltipNextTabAfterAScrollingTabShowsTheFocusTooltip(t *testing.T) {
	h, _ := openTipScrollPage(t)
	for i := 0; i < 16; i++ {
		h.keys("Tab") // 第 16 次滚动并聚焦宿主 15
	}
	h.advance(TooltipDelay + 10*time.Millisecond)
	h.frames(2)
	if h.shown(tipOf(15)) {
		t.Fatal("前提不成立：滚动的 Tab 到达的焦点不该出气泡")
	}
	// 滚动之后宿主 15 居中，宿主 16 紧随其后还在视野里：这一步不滚动。
	h.keys("Tab")
	h.advance(TooltipDelay + 10*time.Millisecond)
	h.frames(2)
	if !h.shown(tipOf(16)) {
		t.Fatal("滚动的 Tab 之后的下一个 Tab 到达的焦点应当照常出气泡：压制不能跨按键")
	}
}

// 边界三：压制有到期时间。滚动的 Tab 之后迟迟没有焦点变化（压制起算后过了期），之后无关的焦点变化照常出气泡；
// 不靠按键也能到期（传感器自己要一帧把它收掉）。
func TestTooltipTabScrollSuppressionExpires(t *testing.T) {
	h, p := openTipScrollPage(t)
	for i := 0; i < 15; i++ {
		h.keys("Tab")
	}
	p.st.noteTabScroll() // 模拟：一个滚动的 Tab 已经通知了提示层，但焦点一直没有到
	h.frames(2)          // 传感器运行，起算有效期
	if !p.st.tabScroll {
		t.Fatal("通知之后压制应当生效")
	}
	h.advance(tabScrollWindow + 10*time.Millisecond)
	h.frames(2)
	if p.st.tabScroll {
		t.Fatal("有效期过后压制应当失效")
	}
	// 焦点此时仍在宿主 14：它在压制里被记成「已收起」，不出气泡，因为焦点没有换。
	h.advance(2 * TooltipDelay)
	h.frames(2)
	if h.shown(tipOf(14)) {
		t.Fatal("压制期里记下的焦点没有换，期满之后不该补出气泡")
	}
	// 焦点换走再回来是新的 focusin，照常出气泡。
	h.keys("shift+Tab")
	h.keys("Tab")
	h.advance(TooltipDelay + 10*time.Millisecond)
	h.frames(2)
	if !h.shown(tipOf(14)) {
		t.Fatal("压制期满之后新的焦点变化应当照常出气泡")
	}
}

// 边界四：新按键先结束上一次的压制。滚动的 Tab 通知了提示层之后、焦点到达之前又来一个不滚动的按键，它带来的焦点照常出气泡。
func TestTooltipANewKeyEndsTheTabScrollSuppression(t *testing.T) {
	h, p := openTipScrollPage(t)
	for i := 0; i < 3; i++ {
		h.keys("Tab") // 焦点在宿主 2
	}
	p.st.noteTabScroll()
	h.keys("Tab") // 新按键：先结束压制；这个 Tab 不需要滚动，焦点落到宿主 3
	h.advance(TooltipDelay + 10*time.Millisecond)
	h.frames(2)
	if !h.shown(tipOf(3)) {
		t.Fatal("新按键之后到达的焦点应当照常出气泡：压制要在新按键处结束")
	}
}

// ---- 失焦 ----

type activeProbe struct {
	active bool
	calls  int
}

func (a *activeProbe) isActive() bool { a.calls++; return a.active }

// 失焦（window blur）：窗口不在前台时收起；回到前台后指针仍在原处也不再出（没有新的 pointerover）。
func TestTooltipBlurDismissesHoverTooltip(t *testing.T) {
	h, p := openTipPage(t)
	probe := &activeProbe{active: true}
	p.st.Active = probe.isActive
	h.moveTo(60, 36)
	h.advance(TooltipDelay + 10*time.Millisecond)
	h.frames(2)
	if !h.shown(tipA) {
		t.Fatal("悬停 320 ms 后没有出气泡")
	}
	probe.active = false
	h.advance(activePoll + 20*time.Millisecond)
	h.frames(2)
	if h.shown(tipA) {
		t.Fatal("窗口失焦之后气泡仍在")
	}
	h.advance(2 * TooltipDelay)
	if h.shown(tipA) {
		t.Fatal("失焦收起之后指针没动，不应再出气泡")
	}
	probe.active = true
	h.advance(2*activePoll + 20*time.Millisecond)
	h.advance(2 * TooltipDelay)
	h.frames(2)
	if h.shown(tipA) {
		t.Fatal("回到前台后指针仍在同一个宿主上，不应再出气泡（鼠标悬停没有 focusin）")
	}
	h.moveTo(500, 400)
	h.moveTo(60, 36)
	h.advance(TooltipDelay + 10*time.Millisecond)
	if !h.shown(tipA) {
		t.Fatal("指针离开再进入后应当重新提示")
	}
}

// 键盘聚焦的提示：失焦收起，窗口不在前台时不再出，回到前台后重新排期（渲染层回到窗口时元素重新触发 focusin）。
func TestTooltipBlurDismissesFocusTooltipAndRestoresOnReturn(t *testing.T) {
	h, p := openTipPage(t)
	probe := &activeProbe{active: true}
	p.st.Active = probe.isActive
	h.keys("Tab")
	h.advance(TooltipDelay + 10*time.Millisecond)
	h.frames(2)
	if !h.shown(tipA) {
		t.Fatal("聚焦 320 ms 后没有出气泡")
	}
	probe.active = false
	h.advance(activePoll + 20*time.Millisecond)
	h.frames(2)
	if h.shown(tipA) {
		t.Fatal("窗口失焦之后气泡仍在")
	}
	h.advance(3 * TooltipDelay)
	h.frames(2)
	if h.shown(tipA) {
		t.Fatal("窗口不在前台期间，焦点控件的气泡不该重新出现")
	}
	probe.active = true
	h.advance(activePoll + 20*time.Millisecond) // 探测到回到前台
	h.advance(TooltipDelay + 10*time.Millisecond)
	h.frames(2)
	if !h.shown(tipA) {
		t.Fatal("回到前台后焦点控件应当重新排期并出气泡")
	}
	// 再失焦一次：回到前台之后仍然能探测到「从前台变成不在前台」。
	probe.active = false
	h.advance(activePoll + 20*time.Millisecond)
	h.frames(2)
	if h.shown(tipA) {
		t.Fatal("第二次失焦之后气泡仍在")
	}
}

// 气泡还在排期（没到 320 ms）时失焦：排期取消，回到前台、指针没动也不再出。
func TestTooltipBlurCancelsPendingTooltip(t *testing.T) {
	h, p := openTipPage(t)
	probe := &activeProbe{active: true}
	p.st.Active = probe.isActive
	h.moveTo(60, 36)
	h.advance(100 * time.Millisecond)
	probe.active = false
	h.advance(activePoll + 20*time.Millisecond)
	h.advance(2 * TooltipDelay)
	h.frames(2)
	if h.shown(tipA) {
		t.Fatal("排期中失焦，气泡不该再出现")
	}
	probe.active = true
	h.advance(2*activePoll + 2*TooltipDelay)
	h.frames(2)
	if h.shown(tipA) {
		t.Fatal("回到前台后指针仍在同一个宿主上，不应再出气泡")
	}
}

// 窗口一开始不在前台、悬停出了提示，之后被激活（不收起），再失焦：仍要收起。
// 「在前台」的记录要随每次探测更新，不能只停在宿主刚换时的那一次。
func TestTooltipBlurIsDetectedAfterTheWindowWasActivated(t *testing.T) {
	h, p := openTipPage(t)
	probe := &activeProbe{active: false}
	p.st.Active = probe.isActive
	h.moveTo(60, 36)
	h.advance(TooltipDelay + 10*time.Millisecond)
	h.frames(2)
	if !h.shown(tipA) {
		t.Fatal("窗口不在前台时悬停，气泡应当照常出现")
	}
	probe.active = true
	h.advance(activePoll + 20*time.Millisecond)
	h.frames(2)
	if !h.shown(tipA) {
		t.Fatal("窗口被激活不该收起气泡")
	}
	probe.active = false
	h.advance(activePoll + 20*time.Millisecond)
	h.frames(2)
	if h.shown(tipA) {
		t.Fatal("激活之后再失焦，气泡仍在")
	}
}

// 窗口本来就不在前台时悬停：渲染层照常出提示（pointerover 不看窗口焦点），只有「从前台变成不在前台」才收起。
func TestTooltipStillShowsOnHoverInInactiveWindow(t *testing.T) {
	h, p := openTipPage(t)
	probe := &activeProbe{active: false}
	p.st.Active = probe.isActive
	h.moveTo(60, 36)
	h.advance(TooltipDelay + 10*time.Millisecond)
	h.advance(2 * activePoll)
	h.frames(2)
	if !h.shown(tipA) {
		t.Fatal("窗口不在前台时悬停，气泡应当照常出现")
	}
}

// 探测只在有气泡显示或排期时进行：没有悬停、没有焦点时一次也不问（Win32 调用不进每一帧）。
func TestTooltipProbesActiveOnlyWhileATooltipIsUp(t *testing.T) {
	h, p := openTipPage(t)
	probe := &activeProbe{active: true}
	p.st.Active = probe.isActive
	h.advance(2 * time.Second)
	h.moveTo(500, 400)
	h.frames(20)
	if probe.calls != 0 {
		t.Fatalf("没有气泡时探测了 %d 次", probe.calls)
	}
	h.moveTo(60, 36)
	h.advance(TooltipDelay + 10*time.Millisecond)
	h.advance(time.Second)
	if probe.calls == 0 {
		t.Fatal("有气泡时没有探测")
	}
	// 一秒内至多约 4 次轮询（间隔 250 ms），不会每帧都问。
	before := probe.calls
	h.advance(time.Second)
	if got := probe.calls - before; got > 6 {
		t.Fatalf("一秒内探测了 %d 次，应约每 %v 一次", got, activePoll)
	}
	h.moveTo(500, 400) // 气泡收起之后不再轮询
	after := probe.calls
	h.advance(2 * time.Second)
	if probe.calls != after {
		t.Fatalf("气泡收起之后还在探测（%d 次）", probe.calls-after)
	}
}

// 没有气泡显示或排期时，探测失焦不能让窗口空转：时间推进几秒，一帧都不该多出来
// （空闲应用的帧数与定时器数为零，是 Viva 的承诺，也是内存与功耗目标的一部分）。
func TestTooltipIdleWindowSchedulesNoFrames(t *testing.T) {
	p := &tipPage{}
	probe := &activeProbe{active: true}
	p.st.Active = probe.isActive
	paints := 0
	h := openPage(t, still(), 800, 600, func(l *Look, c ui.C) { paints++; p.view(l, c) })
	h.advance(time.Second)
	before := paints
	h.advance(5 * time.Second)
	if paints != before {
		t.Fatalf("没有气泡时 5 秒内多画了 %d 帧", paints-before)
	}
	h.moveTo(60, 36) // 有气泡时才轮询
	h.advance(TooltipDelay + 10*time.Millisecond)
	before = paints
	h.advance(2 * time.Second)
	if got := paints - before; got < 4 || got > 12 {
		t.Fatalf("气泡显示期间 2 秒内画了 %d 帧，应约每 %v 一帧", got, activePoll)
	}
}

// ---- 文字输入 ----

type tipInputPage struct {
	st      TooltipState
	field   ui.Field
	changed int // Look.Input 返回的盒子上 Changed() 为真的绘制次数：调用方仍能读到编辑
}

func (p *tipInputPage) view(l *Look, c ui.C) {
	ui.Column(c).Fill().Children(func() {
		hc := c.Key("host")
		ui.Row(hc).Absolute().Left(20).Top(20).Size(80, 32).HitWith(ui.ID(hc), "甲", gfx.HitOptions{Role: gfx.RoleButton, Hint: tipA})
		box := l.Input(c.Key("in"), &p.field, "文字", "", InputSpec{Size: 14, Weight: WeightNormal, Height: 36}).Absolute().Left(200).Top(20).Width(200)
		if box.Changed() {
			p.changed++
		}
	})
	l.Tooltips(c, &p.st)
}

// 在输入框里敲字：Viva 的 Windows 窗口把编辑键交给文字系统、不经 OnKey，Look.Input 在文字被编辑时补记一次按键，
// 气泡收起，指针没动就不再出；调用方自己读到的 Changed() 不受影响。
func TestTooltipTypingInInputDismisses(t *testing.T) {
	p := &tipInputPage{}
	h := openPage(t, still(), 800, 300, p.view)
	h.clickAt(300, 38) // 聚焦输入框
	h.moveTo(60, 36)   // 指针移到甲上
	h.advance(TooltipDelay + 10*time.Millisecond)
	h.frames(2)
	if !h.shown(tipA) {
		t.Fatal("悬停 320 ms 后没有出气泡")
	}
	if err := h.session.Type("x"); err != nil {
		t.Fatal(err)
	}
	h.frames(3)
	if got := p.field.Text(); got != "x" {
		t.Fatalf("输入框内容 %q，应为 x（测试没有真的敲进去）", got)
	}
	if h.shown(tipA) {
		t.Fatal("在输入框里敲字之后气泡仍在")
	}
	if p.changed == 0 {
		t.Fatal("调用方在 Look.Input 返回的盒子上读不到 Changed()：补记按键不该吃掉它")
	}
	h.advance(2 * TooltipDelay)
	h.frames(2)
	if h.shown(tipA) {
		t.Fatal("指针没动，敲字收起之后不应再出气泡")
	}
}
