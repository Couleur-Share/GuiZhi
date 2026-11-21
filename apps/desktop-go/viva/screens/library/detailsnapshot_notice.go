package library

import (
	"strconv"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/tokens"
)

// 「一段文字，紧跟一枚行内按钮」的通知块（渲染层 WebSnapshotPane 里的读取失败与没有快照两条）：
//
//	<div class="p-3 text-sm text-destructive" role="alert">{error}<button class="rounded-lg border px-3 py-1.5 text-xs">重试读取</button></div>
//	<p class="p-3 text-sm text-muted-foreground">{missing}<button class="… ml-2">补采网页快照</button></p>
//
// 文字与按钮是同一个行盒里的行内内容：按钮（inline-block）与父级文字的支柱（text-sm 的字号与行高）按基线对齐，
// 文字折行后按钮接在最后一行的文字之后，放不下就整个落到下一行（行首同样留着 ml-2）；按钮里的文字不设颜色，继承父级的文字色
// （Tailwind 的 button { color: inherit }），所以「重试读取」是红色、「补采网页快照」是弱化色。
// Viva 没有行内排版：这里按 Chromium 的规则自己排——折行用 kit.WrapText（与 kit.TextFlow 同一套断行机会），
// 行盒的高度与文字、按钮的纵向位置按支柱与按钮的基线算（kit.InlineBoxDrop 的同一模型，这里要的是两者各自的偏移与行盒总高）。

// snapshotInlineNotice 画一条这样的通知块：text 是文字，color 是文字色（按钮的文字继承它），gap 是按钮的左外边距（0 或 ml-2），
// label 是按钮文字；alert 为真时整块是 role="alert"（可访问树里是 status，立即播报）。返回按钮这一帧是否被点。
// 宽度取上一帧量到的内容宽（sn.noticeW，变了就再出一帧），首帧按一行排。
func (s *Screen) snapshotInlineNotice(c ui.C, key, text string, color tokens.Color, gap float32, label string, disabled, alert bool) bool {
	l := s.Look
	sn := &s.detail.panel.snap
	canvas := c.Canvas()
	size, line := l.Rem(0.875), l.Rem(1.25) // text-sm：0.875rem、行高 1.25rem，随字号档缩放
	measure := func(str string) float32 { return canvas.TextWidthWeight(str, size, kit.WeightNormal) }

	// 按钮的外形：宽 = 文字 + 两侧内边距 + 两侧边框，高 = 一行（text-xs 的 1rem）+ 上下内边距 + 上下边框；
	// 基线离按钮顶 = 上边框 + 上内边距 + 一行文字的基线（kit.ChromeBaselineRow）。
	bs := s.snapshotButtonStyle()
	bs.Fg = color
	btnSize, btnLine := l.Rem(0.75), l.Rem(1)
	btnW := canvas.TextWidthWeight(label, btnSize, kit.WeightNormal) + 2*(bs.PadX+bs.BorderW)
	btnH := btnLine + 2*(bs.PadY+bs.BorderW)
	btnBase := bs.PadY + bs.BorderW + float32(kit.ChromeBaselineRow(kit.WeightNormal, float64(btnSize*l.Scale), float64(btnLine*l.Scale)))/l.Scale

	// 行盒：支柱基线之上 As、之下 Ds；按钮基线之上 btnBase、之下 btnH−btnBase；行盒取两者较大的上沿与下沿。
	as := float32(kit.ChromeBaselineRow(kit.WeightNormal, float64(size*l.Scale), float64(line*l.Scale))) / l.Scale
	ascent := max(as, btnBase)
	boxH := ascent + max(line-as, btnH-btnBase) // 带按钮的那一行的行盒高
	textTop, btnTop := ascent-as, ascent-btnBase

	var rows []string
	if sn.noticeW > 0 && text != "" {
		rows = kit.WrapText(text, sn.noticeW, measure)
	} else {
		rows = []string{text}
	}
	// 按钮接在最后一行文字之后放得下就同行（行宽按行尾去掉悬挂空白算），放不下落到下一行。
	lastW := measure(strings.TrimRight(rows[len(rows)-1], " \t"))
	inline := sn.noticeW <= 0 || lastW+gap+btnW <= sn.noticeW+0.01

	clicked := false
	button := func() {
		ui.Row(c.Key(key + "-btn-box")).Shrink(0).MarginTop(btnTop).Children(func() {
			clicked = l.Button(c.Key(key+"-btn"), kit.ButtonSpec{Label: label, Disabled: disabled}, bs).Clicked()
		})
	}
	outer := ui.Column(c.Key(key)).FillWidth().Shrink(0).Pad(l.Space(3))
	if alert {
		outer = outer.Draw(func(d *ui.DrawContext) {
			d.Canvas.ContainerWithOptions(d.Rect, gfx.RoleStatus, "", gfx.HitOptions{Live: gfx.LiveAssertive})
		})
	}
	outer.Children(func() {
		ui.Column(c.Key(key + "-body")).FillWidth().Draw(func(d *ui.DrawContext) {
			if w := d.Rect.Width; w > 0 && abs32(w-sn.noticeW) > 0.01 {
				sn.noticeW = w
				d.Canvas.RequestFrame()
			}
		}).Children(func() {
			for i, row := range rows {
				last := i == len(rows)-1
				h, top := line, float32(0)
				if last && inline {
					h, top = boxH, textTop
				}
				ui.Row(c.Key(key + "-row" + strconv.Itoa(i))).FillWidth().Height(h).Items(ui.Start).Children(func() {
					shown := strings.TrimRight(row, " \t") // 行尾的空白在 Chromium 里悬挂：不占宽，文字仍是完整的一截
					if strings.TrimSpace(row) != "" {
						l.TextPx(c.Key(key+"-text"+strconv.Itoa(i)), row, size, line, kit.WeightNormal, color).Width(measure(shown)).Shrink(0).MarginTop(top)
					}
					if last && inline {
						ui.Column(c.Key(key + "-gap")).Width(gap).Shrink(0)
						button()
					}
				})
			}
			if !inline {
				ui.Row(c.Key(key + "-row-btn")).FillWidth().Height(boxH).Items(ui.Start).Children(func() {
					ui.Column(c.Key(key + "-gap")).Width(gap).Shrink(0)
					button()
				})
			}
		})
	})
	return clicked
}
