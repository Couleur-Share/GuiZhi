package kit

import "testing"

// TestBulletGeomMatchesChromium 把无序列表记号的几何钉在渲染层的实测上（M2-6c，app.window.library.detail-tools-summary-expanded 的
// 小 / 中 / 大三档字号）：React 里 li 的行高是 1.6 倍字号（LayoutUnit 截断），圆点的墨迹相对 li 左上角——
//
//	14px：x ∈ [-16, -12)，边长 5，顶 = 行盒顶 + 9
//	16px：x ∈ [-17, -12)，边长 5，顶 = 行盒顶 + 11
//	18px：x ∈ [-19, -13)，边长 6，顶 = 行盒顶 + 11
//
// 顶边的小数部分由调用方按 Chromium 的规则取整，这里只钉整数部分与边长。
func TestBulletGeomMatchesChromium(t *testing.T) {
	cases := []struct {
		size           float32
		wantDx, wantDy float32
		wantSide       float32
	}{
		{14, -16, 9, 5},
		{16, -17, 11, 5},
		{18, -19, 11, 6},
	}
	for _, c := range cases {
		dx, dy, side := bulletGeom(c.size, layoutUnit(proseLineHeight*c.size), 1)
		if dx != c.wantDx || dy != c.wantDy || side != c.wantSide {
			t.Errorf("字号 %.0fpx：得到 dx=%v dy=%v 边长=%v，期望 dx=%v dy=%v 边长=%v", c.size, dx, dy, side, c.wantDx, c.wantDy, c.wantSide)
		}
	}
}

// TestLayoutUnitTruncatesTo64th 钉住 Chromium 的 LayoutUnit 截断：16px 的 1.6 倍行高是 25.59375，不是 25.6。
func TestLayoutUnitTruncatesTo64th(t *testing.T) {
	cases := map[float32]float32{25.6: 25.59375, 22.4: 22.390625, 28.8: 28.796875, 24: 24, 5.6: 5.59375}
	for in, want := range cases {
		if got := layoutUnit(in); got != want {
			t.Errorf("layoutUnit(%v) = %v，期望 %v", in, got, want)
		}
	}
}

// TestProseMarkerShapes 无序列表的记号是图形（按嵌套深度轮换），有序列表的记号是文字。
func TestProseMarkerShapes(t *testing.T) {
	ul := &MdBlock{Kind: MdList}
	want := []proseBullet{bulletDisc, bulletCircle, bulletSquare, bulletDisc}
	for depth, shape := range want {
		if text, got := proseMarker(ul, 0, depth); got != shape || text != "" {
			t.Errorf("无序列表深度 %d：得到 %q/%v，期望图形 %v", depth, text, got, shape)
		}
	}
	ol := &MdBlock{Kind: MdList, Ordered: true, Start: 3}
	if text, shape := proseMarker(ol, 1, 0); text != "4." || shape != bulletNone {
		t.Errorf("有序列表：得到 %q/%v，期望 \"4.\" 与文字记号", text, shape)
	}
}
