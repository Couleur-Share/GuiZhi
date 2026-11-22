package ask

import (
	"unicode/utf8"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 围绕本文的输入区（渲染层 ChatComposer）：多行输入框加一枚「发送 / 停止」钮。
// 与全局问答的输入区（composer.go）是两套版面：这里的输入框 rows=2、rounded-lg、最高 136px，按钮是带字的主色钮；
// Enter 判定共用同一个纯函数 withoutInsertedNewline 与 Screen.ShiftHeld。

// articleInputMaxRunes 是输入框的字数上限（渲染层 maxLength={8000}）。
const articleInputMaxRunes = 8000

// chatInput 是围绕本文输入框的界面状态：编辑控件本身，加上它与 ArticleStore 里草稿的对账记号。
// 草稿的权威副本在 ArticleStore（按 DraftKey 保存，切走再回来还在）；控件里的文字每帧与它对账。
type chatInput struct {
	field ui.Field
	key   string // 当前绑定的草稿键；换了键就从 store 重新取文字
	// stored 是 store 里这份草稿最近一次读到或写入的内容：store 里的内容与它不同就是被程序改写了（发送后清空、选段提问、换会话）。
	stored string
	seen   string // 上一帧控件里的内容：比它多出一个换行就是按了 Enter（takeArticleEnter）
	focus  bool   // 上一帧输入框是否有焦点
	// focusNext 让下一帧输入框取得焦点（面板打开时，渲染层 requestAnimationFrame 里 textarea.focus）。
	focusNext bool
	width     float32 // 输入框文字区上一帧的宽度：算折行的行数
	rows      int
}

// composerSpec 是一次声明输入区所需的东西。
type composerSpec struct {
	Key      string // 草稿键
	Running  bool   // 有一轮在回答：按钮是「停止」，Enter 不发送
	Disabled bool   // 提交中、加载中或读取失败：按钮与 Enter 都不发送
	OnSend   func()
	OnStop   func()
}

// syncDraft 让控件里的文字与 store 里的草稿对账：键变了或 store 被程序改写就以 store 为准，
// 否则控件里被用户敲出的变化写回 store。
func (s *Screen) syncDraft(in *chatInput, key string) {
	art := s.Store.Article()
	stored := art.Draft(key).Text
	switch {
	case in.key != key || stored != in.stored:
		in.key, in.stored = key, stored
		in.field.SetText(stored)
		in.seen = stored
	default:
		if cur := in.field.Text(); cur != stored {
			art.SetDraft(key, cur)
			in.stored = cur
		}
	}
}

// flushDraft 把控件里此刻的文字立即写回 store：发送与「关闭联网后发送」读的是 store 里的草稿，不能落后一帧。
func (s *Screen) flushDraft(in *chatInput) {
	cur := in.field.Text()
	if cur != in.stored {
		s.Store.Article().SetDraft(in.key, cur)
		in.stored = cur
	}
}

// clearDraftInput 程序性地清空控件与 store 里的草稿（新对话、从历史打开了别的会话）。
func (s *Screen) clearDraftInput(in *chatInput, key string) {
	if in.key == key {
		in.field.SetText("")
		in.seen, in.stored = "", ""
	}
	s.Store.Article().SetDraft(key, "")
	s.Store.Article().SetQuote(key, nil)
}

// takeArticleEnter 把输入框里「按了 Enter」变成发送（渲染层 onKeyDown：Enter 无 Shift、非输入法组词、没在回答、没禁用时发送并拦下换行）。
// 判定方式与全局输入框相同（见 takeEnter）：下一帧看内容比上一帧恰好多一个换行、又没按 Shift。
// 回答中或禁用时 Enter 按渲染层原样什么也不拦：换行留在输入框里。
func (s *Screen) takeArticleEnter(in *chatInput, can bool, send func()) {
	cur := in.field.Text()
	prev := in.seen
	in.seen = cur
	if cur == prev || !in.focus || !can {
		return
	}
	text, ok := withoutInsertedNewline(prev, cur)
	if !ok || s.ShiftHeld != nil && s.ShiftHeld() {
		return
	}
	in.field.SetText(text)
	in.seen = text
	send()
}

// limitRunes 把超过字数上限的内容截回上限（粘贴长文时 maxLength 的效果）。
func limitRunes(in *chatInput) {
	cur := in.field.Text()
	if utf8.RuneCountInString(cur) <= articleInputMaxRunes {
		return
	}
	cur = string([]rune(cur)[:articleInputMaxRunes])
	in.field.SetText(cur)
	in.seen = cur
}

// chatComposer 是输入区：flex items-end gap-2，输入框（flex-1）与右边的发送 / 停止钮。
func (s *Screen) chatComposer(c ui.C, in *chatInput, spec composerSpec) {
	l := s.Look
	send := func() {
		s.flushDraft(in)
		spec.OnSend()
	}
	s.syncDraft(in, spec.Key)
	s.takeArticleEnter(in, !spec.Running && !spec.Disabled, send)
	limitRunes(in)
	s.flushDraft(in)

	ui.Row(c.Key("composer")).FillWidth().Items(ui.End).Gap(l.Space(2)).Children(func() {
		s.chatInputBox(c, in)
		s.chatSendButton(c, in, spec, send)
	})
}

// chatInputBox 是输入框：min-h-12 min-w-0 flex-1 resize-none rounded-lg border border-border bg-background px-3 py-2 text-sm，
// 聚焦时边框 primary 并带全局的 ring。rows=2 且随内容增长到 136px：Viva 多行框每行高是字号加 7（渲染层 20px，台账 B-062 同一成因）。
func (s *Screen) chatInputBox(c ui.C, in *chatInput) {
	l := s.Look
	t := l.T
	canvas := c.Canvas()
	lineH := l.Theme.Metrics.FontSizeM + 7
	padX, padY := l.Space(3), l.Space(2)
	in.rows = countWrappedRows(canvas, in.field.Text(), in.width, l.Theme.Metrics.FontSizeM)
	h := min(max(float32(max(in.rows, 2))*lineH+2*padY+2, l.Rem(3)), askInputMaxHeight)
	radius := l.Rem(0.5)
	focused := in.focus

	c = c.Key("input-box")
	ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Height(h).Pad(1).
		Draw(func(d *ui.DrawContext) {
			in.width = d.Rect.Width - 2 - 2*padX
			border := t.Border
			if focused {
				border = t.Primary
				l.PaintInputRing(d, 1, radius)
			}
			l.PaintSurface(d, kit.Surface{Bg: t.Background, Border: border, BorderW: 1, Radius: radius})
		}).
		Children(func() {
			box := l.TextArea(c.Key("input"), &in.field, locales.ArticleAskQuestion(), locales.ArticleAskPlaceholder(), kit.TextAreaSpec{
				Height: h - 2, PadX: padX, PadY: padY, Color: t.Foreground,
				Placeholder: t.MutedForeground.WithAlpha(0.7), PlaceholderLeading: l.Rem(1.25),
			}).FillWidth()
			if in.focusNext {
				box = box.AutoFocus()
				in.focusNext = false
			}
			in.focus = box.State().Focused
		})
}

// chatSendButton 是 shrink-0 rounded-lg bg-primary px-3 py-2 text-sm text-primary-foreground disabled:opacity-50：
// 回答中是「停止」（始终可点），否则是「发送」（禁用或输入为空时不可点）。
func (s *Screen) chatSendButton(c ui.C, in *chatInput, spec composerSpec, send func()) {
	l := s.Look
	t := l.T
	label := locales.AskSend()
	if spec.Running {
		label = locales.AskStop()
	}
	disabled := !spec.Running && (spec.Disabled || jsTrim(in.field.Text()) == "")
	b := l.Button(c.Key("send"), kit.ButtonSpec{Label: label, Disabled: disabled, Center: true}, kit.ButtonStyle{
		PadX: l.Space(3), PadY: l.Space(2), Radius: l.Rem(0.5), Text: kit.TextSM, Weight: kit.WeightNormal,
		Fg: t.PrimaryForeground, Bg: t.Primary, DisabledOpacity: 0.5, Surface: t.Background,
	}).Shrink(0)
	if !b.Clicked() {
		return
	}
	if spec.Running {
		spec.OnStop()
		return
	}
	send()
}
