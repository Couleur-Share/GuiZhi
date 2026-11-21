package kit

import (
	"strings"
	"testing"
)

// 本文件测 pre-wrap 排版（prewrap.go，台账 B-067）的纯排版部分：换行规整、制表符展开、命中位置、宽度缓存。
// 绘制与滚动定位在 viva/screens/library 的文字稿测试里经界面测过。

func preBuilderOf(t *testing.T) *markBuilder {
	t.Helper()
	look, canvas := canvasOf(t)
	return &markBuilder{look: look, canvas: canvas, size: 16, weight: WeightNormal, pad: look.Space(0.5)}
}

// rowText 是一个视觉行里所有片段的文字连起来。
func rowText(row []markFrag) string {
	var b strings.Builder
	for _, f := range row {
		b.WriteString(f.text)
	}
	return b.String()
}

// \r\n 与单独的 \r 都按一个硬换行算；空行占一行（渲染层 whitespace-pre-wrap 里的换行）。
func TestPreWrapNormalizesLineBreaks(t *testing.T) {
	b := preBuilderOf(t)
	res := b.buildPreWrap("甲\r\n乙\r丙\n\n丁", "", 600)
	want := []string{"甲", "乙", "丙", "", "丁"}
	if len(res.rows) != len(want) {
		t.Fatalf("应排成 %d 行，实际 %d 行：%+v", len(want), len(res.rows), res.rows)
	}
	for i, w := range want {
		if got := rowText(res.rows[i]); got != w {
			t.Errorf("第 %d 行应是 %q，实际 %q", i+1, w, got)
		}
	}
	if len(res.boxes) != len(res.rows) {
		t.Errorf("底色行数应与文字行数一致：%d 对 %d", len(res.boxes), len(res.rows))
	}
}

// 制表符按 tab-size 8 展开到下一个 8 的整数倍列，行首缩进、连续空格都留着。
func TestPreWrapExpandsTabsAndKeepsSpaces(t *testing.T) {
	b := preBuilderOf(t)
	res := b.buildPreWrap("a\tb\n\t缩进\nx  y", "", 600)
	want := []string{"a" + strings.Repeat(" ", 7) + "b", strings.Repeat(" ", 8) + "缩进", "x  y"}
	if len(res.rows) != len(want) {
		t.Fatalf("应排成 %d 行，实际 %d 行：%+v", len(want), len(res.rows), res.rows)
	}
	for i, w := range want {
		if got := rowText(res.rows[i]); got != w {
			t.Errorf("第 %d 行应是 %q，实际 %q", i+1, w, got)
		}
	}
}

// 命中不跨过换行：每一硬行各自匹配；marks 与 markEnds 记的是每处命中起点与末尾所在的视觉行。
func TestPreWrapMarksStayOnTheirHardLines(t *testing.T) {
	b := preBuilderOf(t)
	res := b.buildPreWrap("命运石\n无关\n石之门命运", "命运", 600)
	if len(res.marks) != 2 || res.marks[0] != 0 || res.marks[1] != 2 {
		t.Fatalf("两处命中应分别在第 0、2 行：%v", res.marks)
	}
	if res.markEnds[0] != 0 || res.markEnds[1] != 2 {
		t.Errorf("没有折行时命中末行就是起行：%v", res.markEnds)
	}
	// 命中词横跨换行的不算命中。
	if res := b.buildPreWrap("命\n运", "命运", 600); len(res.marks) != 0 {
		t.Errorf("命中不该跨过换行：%v", res.marks)
	}
}

// 整份结果缓存在 Look 里按宽度分键：同一份文字换了宽度要重排，不能拿上一个宽度的结果。
func TestPreWrapResultIsKeyedByWidth(t *testing.T) {
	look, canvas := canvasOf(t)
	text := strings.Repeat("宽度会影响折行。", 40)
	mk := func() *preWrapLayout {
		return &preWrapLayout{look: look, canvas: canvas, text: text, size: 16, weight: WeightNormal, line: 32, pad: look.Space(0.5)}
	}
	wide := len(mk().resultAt(800).rows)
	narrow := len(mk().resultAt(200).rows)
	if narrow <= wide {
		t.Fatalf("窄了行数应更多：800 px → %d 行，200 px → %d 行", wide, narrow)
	}
	if again := len(mk().resultAt(800).rows); again != wide {
		t.Errorf("换回原宽度应得到原来的行数：%d，实际 %d", wide, again)
	}
	// 同一宽度复用同一份结果（稳态帧不重排）。
	if a, b := mk().resultAt(800), mk().resultAt(800); a != b {
		t.Error("同一宽度应复用缓存里的同一份结果")
	}
}
