package parity

import "testing"

// TestRowRectsMergesFragmentsOfOneLine：预排版文字的 Range 矩形把同一行拆成几块
// （行首缩进空白、挂在行尾的空格），按行比较前必须并成一块，否则行数会被高估。
func TestRowRectsMergesFragmentsOfOneLine(t *testing.T) {
	got := rowRects([]Box{
		{8, 10, 24, 16}, // 行首缩进空白
		{32, 10, 120, 16},
		{152, 10, 4, 16}, // 折行处挂在行尾的空格
		{8, 26, 96, 16},
		{104, 26.3, 40, 16}, // y 相差 0.3 < 0.5，仍是同一行
	})
	if len(got) != 2 {
		t.Fatalf("行数 = %d，想要 2：%v", len(got), got)
	}
	if got[0] != (Box{8, 10, 148, 16}) {
		t.Errorf("第 1 行 = %v，想要 [8 10 148 16]", got[0])
	}
	if got[1].X() != 8 || got[1].W() != 136 {
		t.Errorf("第 2 行 = %v，想要从 x=8 起宽 136", got[1])
	}
}

// TestRowRectsKeepsDistinctRows：y 相差 ≥ 0.5 的相邻片段是两行，哪怕水平上相连；没有片段时返回空。
func TestRowRectsKeepsDistinctRows(t *testing.T) {
	rows := rowRects([]Box{{0, 0, 50, 16}, {0, 16, 50, 16}, {0, 32.5, 50, 16}})
	if len(rows) != 3 {
		t.Fatalf("行数 = %d，想要 3：%v", len(rows), rows)
	}
	if got := rowRects(nil); len(got) != 0 {
		t.Errorf("空输入应得空结果，得到 %v", got)
	}
}
