package kit

import (
	"math"
	"strings"
	"testing"

	"github.com/vivaui/viva/ui"
)

// fragText 把一行里各段的那一截连成这一行的文字。
func fragText(line []MonoFrag) string {
	var b strings.Builder
	for _, f := range line {
		b.WriteString(f.Text)
	}
	return b.String()
}

// 折行与「把几段拼成一段再折」完全一致，只是每一行再拆回各段：每行文字相同，各截的 Run 与它在整段里的范围对得上。
func TestWrapMonoRunsBreaksLikeTheJoinedParagraph(t *testing.T) {
	label := "我的修改："
	value := "第一行内容比较长需要在盒子里折成好几行 and some latin words here\nsecond hard line\n\n末行"
	for _, width := range []float32{0, 90, 160, 250, 400} {
		want := WrapMono(label+value, width, 14)
		got := WrapMonoRuns([]string{label, value}, width, 14)
		if len(got) != len(want) {
			t.Fatalf("width %v：行数 %d，期望 %d", width, len(got), len(want))
		}
		for i := range want {
			if fragText(got[i]) != want[i] {
				t.Errorf("width %v 第 %d 行：%q，期望 %q", width, i+1, fragText(got[i]), want[i])
			}
		}
		// 只有第一行含标签；标签完整地出现在第一行（它比最窄的盒子宽时除外：任意字符处断开，标签可以跨行）。
		if width >= 90 {
			if len(got[0]) == 0 || got[0][0].Run != 0 || got[0][0].Text != label {
				t.Errorf("width %v：第一行应以完整的标签开头，实际 %+v", width, got[0])
			}
			for i, line := range got[1:] {
				for _, f := range line {
					if f.Run != 1 {
						t.Errorf("width %v 第 %d 行不该再有标签：%+v", width, i+2, line)
					}
				}
			}
		}
	}
}

// 空行（硬换行之间）没有任何一截；值里的硬换行不进任何一行；文字不丢。
func TestWrapMonoRunsKeepsHardBreaksAndEmptyLines(t *testing.T) {
	got := WrapMonoRuns([]string{"A:", "x\n\ny"}, 400, 14)
	if len(got) != 3 {
		t.Fatalf("应是三行（含一个空行），实际 %d：%+v", len(got), got)
	}
	if fragText(got[0]) != "A:x" || len(got[1]) != 0 || fragText(got[2]) != "y" {
		t.Errorf("行内容不对：%+v", got)
	}
	if len(got[0]) != 2 || got[0][0] != (MonoFrag{0, "A:"}) || got[0][1] != (MonoFrag{1, "x"}) {
		t.Errorf("第一行应是标签与值的开头两截：%+v", got[0])
	}
	// 回车换行统一成换行；制表符按列展开（列从行首起算，标签也占列）。
	got = WrapMonoRuns([]string{"ab", "\tc\r\nd"}, 400, 14)
	if len(got) != 2 || fragText(got[0]) != "ab      c" || fragText(got[1]) != "d" {
		t.Errorf("制表符与回车换行处理不对：%+v", got)
	}
	// 空值：只剩标签一截。
	got = WrapMonoRuns([]string{"A:", ""}, 400, 14)
	if len(got) != 1 || len(got[0]) != 1 || got[0][0] != (MonoFrag{0, "A:"}) {
		t.Errorf("空值应只剩标签：%+v", got)
	}
}

// 两栏等宽多段在 Viva 里的画法：标签与内容各是一个文本节点（折行时每行各一截），第二行回到盒子的左缘，
// 同一行的几截首尾相接；一行里只有一截的行与 MonoParagraph 逐像素相同。
func TestMonoParagraphRunsDrawsOneLabelPerRunPerLine(t *testing.T) {
	const size, lineH = 14, 20
	label := "我的修改："
	value := "本地优先的知识库软件在窄栏里折行 plain ascii words wrap"
	var lines [][]MonoFrag
	h := openPage(t, still(), 300, 300, func(l *Look, c ui.C) {
		lines = WrapMonoRuns([]string{label, value}, 160, size)
		ui.Column(c).Pad(10).Children(func() {
			l.MonoParagraphRuns(c.Key("pre"), lines, size, lineH, l.T.Foreground)
		})
	})
	if len(lines) < 3 {
		t.Fatalf("前提：160 宽应折成至少三行，实际 %d", len(lines))
	}
	texts := h.session.Snapshot().Texts
	want := 0
	for _, line := range lines {
		want += len(line)
	}
	if len(texts) != want {
		t.Fatalf("每行每段一个 Label，应有 %d 个，实际 %d：%+v", want, len(texts), texts)
	}
	// 第一行：标签与内容的开头首尾相接，同一行；其余各行回到同一个左缘。
	left := texts[0].Bounds.X
	if texts[0].Value != label || texts[1].Value != lines[0][1].Text {
		t.Fatalf("第一行应是标签与内容的开头：%q %q", texts[0].Value, texts[1].Value)
	}
	if got, w := texts[1].Bounds.X, texts[0].Bounds.X+texts[0].Bounds.Width; math.Abs(float64(got-w)) > 0.01 {
		t.Errorf("内容应紧接标签：标签右缘 %v，内容左缘 %v", w, got)
	}
	if math.Abs(float64(texts[1].Bounds.Y-texts[0].Bounds.Y)) > 1.01 {
		t.Errorf("标签与内容的开头应在同一行：%v %v", texts[0].Bounds.Y, texts[1].Bounds.Y)
	}
	for i := 2; i < len(texts); i++ {
		if math.Abs(float64(texts[i].Bounds.X-left)) > 0.01 {
			t.Errorf("第 %d 个文字应回到盒子的左缘 %v，实际 %v（%q）", i+1, left, texts[i].Bounds.X, texts[i].Value)
		}
	}
	// 各行文字拼回内容。
	var joined strings.Builder
	for _, tx := range texts[1:] {
		joined.WriteString(tx.Value)
	}
	if joined.String() != value {
		t.Errorf("内容各截连起来应是原文：%q", joined.String())
	}
}

// 只有一段时画法与 MonoParagraph 完全相同（位置、宽度、字体）。
func TestMonoParagraphRunsSingleRunMatchesMonoParagraph(t *testing.T) {
	const size, lineH = 12, 16
	text := "本地优先 mixed 内容 line one that wraps in a narrow box\nsecond"
	render := func(draw func(l *Look, c ui.C)) []float32 {
		h := openPage(t, still(), 200, 300, func(l *Look, c ui.C) {
			ui.Column(c).Pad(10).Children(func() { draw(l, c) })
		})
		var out []float32
		for _, tx := range h.session.Snapshot().Texts {
			out = append(out, tx.Bounds.X, tx.Bounds.Y, tx.Bounds.Width, tx.Bounds.Height)
		}
		return out
	}
	a := render(func(l *Look, c ui.C) {
		l.MonoParagraph(c.Key("pre"), WrapMono(text, 150, size), size, lineH, l.T.Foreground)
	})
	b := render(func(l *Look, c ui.C) {
		l.MonoParagraphRuns(c.Key("pre"), WrapMonoRuns([]string{text}, 150, size), size, lineH, l.T.Foreground)
	})
	if len(a) == 0 || len(a) != len(b) {
		t.Fatalf("文字个数不同：%d 对 %d", len(a)/4, len(b)/4)
	}
	for i := range a {
		if math.Abs(float64(a[i]-b[i])) > 0.01 {
			t.Fatalf("第 %d 个读数不同：%v 对 %v", i, a[i], b[i])
		}
	}
}

// 行内不是最后一截的 Mono 文字放进渲染层字距宽度的槽：下一截从槽的右缘起，不被 Viva 字格取整后的宽度推向右边。
func TestMonoParagraphRunsLatinLabelLeavesAChromeWidthSlot(t *testing.T) {
	const size, lineH = 14, 20
	label := "My changes: "
	value := "plain value"
	var lines [][]MonoFrag
	h := openPage(t, still(), 400, 100, func(l *Look, c ui.C) {
		lines = WrapMonoRuns([]string{label, value}, 380, size)
		ui.Column(c).Pad(10).Children(func() {
			l.MonoParagraphRuns(c.Key("pre"), lines, size, lineH, l.T.Foreground)
		})
	})
	texts := h.session.Snapshot().Texts
	if len(texts) != 2 {
		t.Fatalf("应有标签与内容两个 Label，实际 %+v", texts)
	}
	slot := MonoWidth(label, size)
	if got, want := texts[1].Bounds.X-texts[0].Bounds.X, slot; math.Abs(float64(got-want)) > 0.01 {
		t.Errorf("内容应从标签起点右移渲染层字距宽度 %v，实际 %v", want, got)
	}
}
