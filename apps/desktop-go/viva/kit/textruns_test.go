package kit

import (
	"math"
	"slices"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// 每一段文字各是一个 Label，位置首尾相接；整段只有空白的不画，只占它的宽度。
func TestTextRunsDrawsOneLabelPerRunEndToEnd(t *testing.T) {
	h := openPage(t, still(), 400, 100, func(l *Look, c ui.C) {
		l.TextRuns(c.Key("runs"), TextXS, WeightNormal, l.T.Foreground, "20480", " ", "字")
	})
	texts := h.session.Snapshot().Texts
	var values []string
	for _, text := range texts {
		values = append(values, text.Value)
	}
	if want := []string{"20480", "字"}; !slices.Equal(values, want) {
		t.Fatalf("画出的 Label = %q，期望 %q（空白段不画 Label）", values, want)
	}
	// 「字」从「20480」之后再空一个空格的宽度开始。
	var canvas *gfx.Canvas
	openPage(t, still(), 400, 100, func(_ *Look, c ui.C) { canvas = c.Canvas() })
	size := h.look.Rem(TextXS.Size)
	wantX := texts[0].Bounds.X + canvas.TextWidthWeight("20480", size, WeightNormal) + canvas.TextWidthWeight(" ", size, WeightNormal)
	if got := texts[1].Bounds.X; math.Abs(float64(got-wantX)) > 0.01 {
		t.Fatalf("第二段起点 x = %v，期望 %v（第一段宽度加一个空格）", got, wantX)
	}
}

// 段首的空白算在这一段里：Label 的原文带着它，起点就在上一段的末尾。
func TestTextRunsKeepsLeadingSpaceInsideTheRun(t *testing.T) {
	h := openPage(t, still(), 400, 100, func(l *Look, c ui.C) {
		l.TextRuns(c.Key("runs"), TextXS, WeightNormal, l.T.Foreground, "处理中", " \u00b7 语音转写")
	})
	texts := h.session.Snapshot().Texts
	if len(texts) != 2 || texts[1].Value != " \u00b7 语音转写" {
		t.Fatalf("第二段应保留段首空白：%+v", texts)
	}
	var canvas *gfx.Canvas
	openPage(t, still(), 400, 100, func(_ *Look, c ui.C) { canvas = c.Canvas() })
	first := canvas.TextWidthWeight("处理中", h.look.Rem(TextXS.Size), WeightNormal)
	if got := texts[1].Bounds.X - texts[0].Bounds.X; math.Abs(float64(got-first)) > 0.01 {
		t.Fatalf("第二段应接在第一段之后（相隔 %v）：实际相隔 %v", first, got)
	}
}
