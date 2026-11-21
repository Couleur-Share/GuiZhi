package kit

import (
	"math"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// flowPage 在 width 宽的容器里排 runs，返回排出来的 Label 与画布（量宽度用）。
func flowPage(t *testing.T, width float32, runs ...string) (*harness, *gfx.Canvas) {
	t.Helper()
	var canvas *gfx.Canvas
	h := openPage(t, still(), 400, 200, func(l *Look, c ui.C) {
		canvas = c.Canvas()
		ui.Column(c).Pad(10).Children(func() {
			l.TextFlow(c.Key("flow"), TextXS, WeightMedium, l.T.Foreground, width, runs...)
		})
	})
	return h, canvas
}

// 后一段接在前一段之后排，放不下就在后一段里换行，第二行回到容器的左缘；每一段在每一行里的那一截是一个 Label。
func TestTextFlowWrapsTheLastRunOnToTheNextLine(t *testing.T) {
	runs := []string{"所选版本 ·", "如何构建本地优先的个人知识库（更新）"}
	h, canvas := flowPage(t, 168, runs...)
	texts := h.session.Snapshot().Texts
	if len(texts) != 3 {
		t.Fatalf("应有三个 Label（前一段、后一段在第一行的一截、后一段在第二行的一截），实际 %d 个：%+v", len(texts), texts)
	}
	if texts[0].Value != runs[0] {
		t.Fatalf("第一个 Label = %q，期望整段 %q", texts[0].Value, runs[0])
	}
	if got := texts[1].Value + texts[2].Value; got != runs[1] {
		t.Fatalf("后一段的两截拼起来 = %q，期望 %q", got, runs[1])
	}
	size, line := h.look.Rem(TextXS.Size), h.look.Rem(TextXS.Line)
	// 第一行：后一段紧接在前一段之后，并且整行放得下容器宽度。
	wantX := texts[0].Bounds.X + canvas.TextWidthWeight(runs[0], size, WeightMedium)
	if got := texts[1].Bounds.X; math.Abs(float64(got-wantX)) > 0.01 {
		t.Fatalf("第一行的后一段起点 x = %v，期望 %v（前一段之后）", got, wantX)
	}
	if end := texts[1].Bounds.X + canvas.TextWidthWeight(texts[1].Value, size, WeightMedium); end-texts[0].Bounds.X > 168+0.01 {
		t.Fatalf("第一行排到 %v 宽，超过容器 168", end-texts[0].Bounds.X)
	}
	if texts[1].Bounds.Y != texts[0].Bounds.Y {
		t.Fatalf("同一行的两截 Y 不同：%v、%v", texts[0].Bounds.Y, texts[1].Bounds.Y)
	}
	// 第二行：回到左缘，低一个行高。
	if texts[2].Bounds.X != texts[0].Bounds.X {
		t.Fatalf("第二行起点 x = %v，期望回到左缘 %v", texts[2].Bounds.X, texts[0].Bounds.X)
	}
	if got := texts[2].Bounds.Y - texts[0].Bounds.Y; math.Abs(float64(got-line)) > 0.01 {
		t.Fatalf("第二行比第一行低 %v，期望一个行高 %v", got, line)
	}
}

// 容器宽度还不知道（首帧，宽度 0）时按一行排：各段首尾相接，没有折行。
func TestTextFlowIsOneLineWhileTheWidthIsUnknown(t *testing.T) {
	h, canvas := flowPage(t, 0, "所选版本 ·", "如何构建本地优先的个人知识库（更新）")
	texts := h.session.Snapshot().Texts
	if len(texts) != 2 || texts[0].Bounds.Y != texts[1].Bounds.Y {
		t.Fatalf("宽度未知时应是同一行的两个 Label：%+v", texts)
	}
	size := h.look.Rem(TextXS.Size)
	if got, want := texts[1].Bounds.X-texts[0].Bounds.X, canvas.TextWidthWeight("所选版本 ·", size, WeightMedium); math.Abs(float64(got-want)) > 0.01 {
		t.Fatalf("两段相隔 %v，期望前一段的宽度 %v", got, want)
	}
}

// flowBox 把 TextFlow（或居中版）放进一个 width 宽的盒子（左上角 (10, 10)），返回排出来的 Label 与画布。
func flowBox(t *testing.T, width float32, center bool, runs ...string) (*harness, *gfx.Canvas) {
	t.Helper()
	var canvas *gfx.Canvas
	h := openPage(t, still(), 400, 200, func(l *Look, c ui.C) {
		canvas = c.Canvas()
		ui.Column(c).Pad(10).Items(ui.Start).Children(func() {
			ui.Column(c.Key("box")).Width(width).Children(func() {
				if center {
					l.TextFlowCentered(c.Key("flow"), TextXS, WeightNormal, l.T.Foreground, width, runs...)
					return
				}
				l.TextFlow(c.Key("flow"), TextXS, WeightNormal, l.T.Foreground, width, runs...)
			})
		})
	})
	return h, canvas
}

// 行尾的空白悬挂：落在行尾的那一截，Label 的宽度只算到最后一个非空白字符（文字仍是完整的一截，拼得回整段）。
func TestTextFlowTrailingSpaceHangsAtTheEndOfALine(t *testing.T) {
	probe := func(width float32) []string {
		h, canvas := flowBox(t, width, false, "ab ", "cd")
		size := h.look.Rem(TextXS.Size)
		w := canvas.TextWidthWeight("ab", size, WeightNormal)
		texts := h.session.Snapshot().Texts
		var got []string
		for _, tx := range texts {
			got = append(got, tx.Value)
		}
		if len(texts) != 2 || texts[0].Value != "ab " || texts[1].Value != "cd" {
			return got
		}
		if texts[1].Bounds.Y <= texts[0].Bounds.Y {
			t.Errorf("宽度 %v 里「cd」应折到下一行：%+v", width, texts)
		}
		if math.Abs(float64(texts[0].Bounds.Width-w)) > 0.01 {
			t.Errorf("行尾的「ab 」宽度 = %v，期望只算到「ab」的 %v（尾随空白悬挂）", texts[0].Bounds.Width, w)
		}
		return got
	}
	// 容器只比「ab」宽一点：「ab 」之后的空格留在第一行尾，「cd」折到第二行。
	h, canvas := flowBox(t, 100, false, "ab ", "cd")
	size := h.look.Rem(TextXS.Size)
	narrow := canvas.TextWidthWeight("ab", size, WeightNormal) + 1
	if got := probe(narrow); len(got) != 2 || got[0] != "ab " || got[1] != "cd" {
		t.Fatalf("两个 Label 的文字 = %q，期望 [\"ab \" \"cd\"]（拼起来是整段）", got)
	}
}

// 居中版：折成几行，每一行的几截作为一个整体各自居中（尾随空白不算在行宽里）；一行里各截仍首尾相接，下一行低一个行高。
func TestTextFlowCenteredCentersEachWrappedLine(t *testing.T) {
	runs := []string{"Source versions (2)", " · New source version available", " · Summary needs refreshing"}
	const width float32 = 327.5
	h, canvas := flowBox(t, width, true, runs...)
	size, line := h.look.Rem(TextXS.Size), h.look.Rem(TextXS.Line)
	w := func(s string) float32 { return canvas.TextWidthWeight(s, size, WeightNormal) }
	texts := h.session.Snapshot().Texts
	if len(texts) != 4 {
		t.Fatalf("应有四个 Label（前两段各一个，第三段在第一行的一截与第二行的一截），实际 %d 个：%+v", len(texts), texts)
	}
	if got := texts[2].Value + texts[3].Value; got != runs[2] {
		t.Fatalf("第三段的两截拼起来 = %q，期望 %q", got, runs[2])
	}
	if texts[2].Value != " · " {
		t.Fatalf("第一行末尾那一截是 %q，期望带着行尾空格的「 · 」", texts[2].Value)
	}
	row1 := w(runs[0]) + w(runs[1]) + w(" ·") // 行尾的空格悬挂，不算
	row2 := w("Summary needs refreshing")
	if row1 > width || row1+w(" ")+w("Summary") <= width {
		t.Fatalf("测试前提不成立：第一行宽 %v，应放得下前三截而放不下「Summary」（容器 %v）", row1, width)
	}
	near := func(got, want float32, what string) {
		t.Helper()
		if math.Abs(float64(got-want)) > 0.02 {
			t.Errorf("%s = %v，期望 %v", what, got, want)
		}
	}
	near(texts[0].Bounds.X, 10+(width-row1)/2, "第一行起点")
	near(texts[1].Bounds.X, texts[0].Bounds.X+w(runs[0]), "第二截接在第一截之后")
	near(texts[2].Bounds.X, texts[1].Bounds.X+w(runs[1]), "第三段第一行的一截接在第二截之后")
	near(texts[2].Bounds.Width, w(" ·"), "行尾那一截的宽度（空格悬挂）")
	near(texts[3].Bounds.X, 10+(width-row2)/2, "第二行起点")
	near(texts[3].Bounds.Y-texts[0].Bounds.Y, line, "第二行比第一行低一个行高")
}

// 整段只有空白的不画 Label，只占它的宽度（同 TextRuns）；折行不会把一段劈成空的 Label。
func TestTextFlowKeepsBlankRunsAsSpaceOnly(t *testing.T) {
	h, canvas := flowPage(t, 300, "甲", " ", "乙")
	texts := h.session.Snapshot().Texts
	if len(texts) != 2 || texts[0].Value != "甲" || texts[1].Value != "乙" {
		t.Fatalf("空白段不画 Label：%+v", texts)
	}
	size := h.look.Rem(TextXS.Size)
	want := canvas.TextWidthWeight("甲", size, WeightMedium) + canvas.TextWidthWeight(" ", size, WeightMedium)
	if got := texts[1].Bounds.X - texts[0].Bounds.X; math.Abs(float64(got-want)) > 0.01 {
		t.Fatalf("「乙」相隔 %v，期望「甲」加一个空格的宽度 %v", got, want)
	}
}
