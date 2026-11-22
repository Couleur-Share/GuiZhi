package ask

import (
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// askInputMaxHeight 是输入框的最大高度（渲染层 ASK_INPUT_MAX_HEIGHT_PX：六行正文加上下内边距）：
// 再长就让输入框自己滚动，不能吞掉回答区。px 字面量，不随字号档缩放。
const askInputMaxHeight float32 = 136

// composer 是底部输入区：shrink-0 border-t border-border px-5 py-3，里面 mx-auto max-w-3xl 的一行：
// 输入框，右边是发送钮（有问题在回答时换成停止钮），底部对齐（items-end）。
func (s *Screen) composer(c ui.C, v View) {
	l := s.Look
	t := l.T
	s.takeEnter(v)
	ui.Column(c.Key("composer")).FillWidth().Shrink(0).PadSides(l.Space(3)+1, l.Space(5), l.Space(3), l.Space(5)).
		Draw(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeTop, 1, t.Border) }).
		Children(func() {
			ui.Row(c.Key("row")).FillWidth().MaxWidth(l.Rem(48)).Self(ui.Center).Items(ui.End).Gap(l.Space(2)).Children(func() {
				s.draftBox(c)
				s.sendOrStop(c, v)
			})
		})
}

// draftBox 是输入框：min-h-9 flex-1 resize-none rounded-xl border border-border bg-background/60 px-3.5 py-2 text-sm，
// 占位符 muted-foreground/70，聚焦时边框 primary/50 并带全局的 ring-2 ring-primary/50。
//
// 高度随内容增长（折行也算，不只按换行符），最少 36px、最多 136px。Viva 多行框每行高是字号加 7（渲染层 text-sm 是 20px，
// 台账 B-062 同一成因），所以上下内边距按「单行时恰好 36px」反推，多行时每行比渲染层高 1px。
func (s *Screen) draftBox(c ui.C) {
	l := s.Look
	t := l.T
	canvas := c.Canvas()
	minH := l.Rem(2.25)
	lineH := l.Theme.Metrics.FontSizeM + 7
	padX := l.Space(3.5)
	padY := max((minH-2-lineH)/2, 0)

	s.inputRows = s.countRows(canvas, s.draft.Text())
	h := min(max(float32(s.inputRows)*lineH+2*padY+2, minH), askInputMaxHeight)
	radius := l.Rem(0.75)
	focused := s.draftFocus

	c = c.Key("draft-box")
	ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Height(h).Pad(1).
		Draw(func(d *ui.DrawContext) {
			s.inputWidth = d.Rect.Width - 2 - 2*padX
			border := t.Border
			if focused {
				border = t.Primary.WithAlpha(0.5)
				l.PaintInputRing(d, 1, radius)
			}
			l.PaintSurface(d, kit.Surface{Bg: t.Background.WithAlpha(0.6), Border: border, BorderW: 1, Radius: radius})
		}).
		Children(func() {
			box := l.TextArea(c.Key("input"), &s.draft, locales.AskPlaceholder(), locales.AskPlaceholder(), kit.TextAreaSpec{
				Height: h - 2, PadX: padX, PadY: padY, Color: t.Foreground,
				Placeholder: t.MutedForeground.WithAlpha(0.7), PlaceholderLeading: l.Rem(1.25),
			}).FillWidth()
			if s.focusDraft {
				box = box.AutoFocus()
				s.focusDraft = false
			}
			s.draftFocus = box.State().Focused
		})
}

// countRows 数输入内容折成几行（含显式换行；至少一行）：按上一帧量到的文字区宽度折行，宽度没量到之前只按换行符算。
func (s *Screen) countRows(canvas *gfx.Canvas, text string) int {
	if text == "" {
		return 1
	}
	size := s.Look.Theme.Metrics.FontSizeM
	rows := 0
	for _, para := range strings.Split(text, "\n") {
		if para == "" || s.inputWidth <= 0 {
			rows++
			continue
		}
		for from := 0; from >= 0; {
			_, next := canvas.WrapRow(para, from, size, gfx.WeightRegular, s.inputWidth)
			rows++
			if next <= from {
				break
			}
			from = next
		}
	}
	return rows
}

// sendOrStop 是输入框右边的钮：h-9 w-9 shrink-0 rounded-xl。有问题在回答时是描边的「停止」（方块图标），
// 否则是主色的「发送」（纸飞机），输入为空时禁用（disabled:opacity-50）。
func (s *Screen) sendOrStop(c ui.C, v View) {
	l := s.Look
	t := l.T
	size := l.Rem(2.25)
	if v.IsRunning {
		label := locales.AskStop()
		b := l.Button(c.Key("stop"), kit.ButtonSpec{Name: label, Hint: label, Icon: icons.Square, Center: true}, kit.ButtonStyle{
			Height: size, Width: size, Radius: l.Rem(0.75), Fg: t.Foreground, Border: t.Border, BorderW: 1,
			BgHover: t.Muted.WithAlpha(0.6), Transition: kit.DurationDefault,
		}).Shrink(0)
		if b.Clicked() {
			s.Store.Stop()
		}
		return
	}
	label := locales.AskSend()
	empty := jsTrim(s.draft.Text()) == ""
	b := l.Button(c.Key("send"), kit.ButtonSpec{Name: label, Hint: label, Icon: icons.Send, Center: true, Disabled: empty}, kit.ButtonStyle{
		Height: size, Width: size, Radius: l.Rem(0.75), Fg: t.PrimaryForeground, Bg: t.Primary, BgHover: t.Primary.WithAlpha(0.9),
		Transition: kit.DurationDefault, DisabledOpacity: 0.5, Surface: t.UiSectionBg,
	}).Shrink(0)
	if b.Clicked() {
		s.submit()
	}
}

// submit 发出输入框里的问题：去掉首尾空白后为空、或正有一轮在回答时什么也不做；发出后清空输入框并回到底部跟随。
func (s *Screen) submit() {
	question := jsTrim(s.draft.Text())
	if question == "" || s.Store.View().IsRunning {
		return
	}
	s.setDraft("")
	s.toEnd = true
	s.spawn(func() { s.Store.Ask(question) })
}

// setDraft 程序性地改写输入框内容，并同步「上一帧的内容」：takeEnter 只把用户在输入框里敲出的换行当作 Enter，
// 程序写入（示例、外部草稿、发送后清空）不能被误认。
func (s *Screen) setDraft(text string) {
	s.draft.SetText(text)
	s.draftSeen = text
}

// takeEnter 把输入框里「按了 Enter」变成发送（渲染层 onKeyDown：Enter 无 Shift 时 preventDefault 并发送，Shift+Enter 换行）。
//
// Viva 的多行框在 Windows 上由系统文字层处理 Enter，直接插入换行，既不经窗口的 OnKey 也不经 OnShortcut（只有 Ctrl/Meta+Enter 会先被
// 应用看到），所以拦不到按键本身；改为在下一帧看内容：比上一帧恰好多出一个换行（在末尾，或是单独插入在某处），
// 又没有按着 Shift，就是 Enter——把这个换行去掉，发送其余的文字。输入法正在组词时的 Enter 只是提交候选、不产生换行，
// 自然不会触发（渲染层没有为组合态做判断，这里反而更稳）。Shift 的真实状态由 ShiftHeld 提供（根接到系统键盘状态，V-a 真机验证）。
//
// 已知边界：恰好只粘贴一个换行会被当成 Enter；同一帧里先敲字再按 Enter 且插入点不在末尾时，换行留着、不发送。
func (s *Screen) takeEnter(v View) {
	cur := s.draft.Text()
	prev := s.draftSeen
	s.draftSeen = cur
	if cur == prev || !s.draftFocus {
		return
	}
	text, ok := withoutInsertedNewline(prev, cur)
	if !ok || s.ShiftHeld != nil && s.ShiftHeld() {
		return
	}
	// 去掉这个换行；回答中或只有空白时到此为止（Enter 不换行、也不发送，草稿原样保留）。
	s.draft.SetText(text)
	s.draftSeen = text
	if !v.IsRunning {
		s.submit()
	}
}

// withoutInsertedNewline 判断 cur 是否是在 prev 上恰好多敲了一个换行：换行在末尾（其前可以还有同一帧里敲的字），
// 或者 cur 就是 prev 在某处插入一个换行。是的话返回去掉这个换行后的文字。
func withoutInsertedNewline(prev, cur string) (string, bool) {
	if strings.Count(cur, "\n") != strings.Count(prev, "\n")+1 {
		return "", false
	}
	if head, ok := strings.CutSuffix(cur, "\n"); ok && strings.HasPrefix(head, prev) {
		return head, true
	}
	for i := 0; i <= len(prev); i++ {
		if cur == prev[:i]+"\n"+prev[i:] {
			return prev, true
		}
	}
	return "", false
}

