package parity

import (
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/viva/kit"
)

// 台账 G-011：文字的内容区（基线上 A 行、下 D 行）被裁剪线整个切掉才算看不见，
// 只有行盒里上下的行距碰到裁剪区不算——渲染层的 Range 外框就是内容区。
func TestContentClippedOutUsesTheContentAreaNotTheLineBox(t *testing.T) {
	const size = 15.75
	a, d := kit.ChromeContentRows(gfx.WeightRegular, size)
	if a != 16 || d != 4 {
		t.Fatalf("15.75px 常规字重的 A、D = %d、%d，期望 16、4（内容区高 20，与渲染层 Range 外框一致）", a, d)
	}
	text := func(clipY, clipH float32) gfx.Text {
		return gfx.Text{
			Style:   gfx.TextStyle{Size: size, Weight: gfx.WeightRegular},
			HasClip: true,
			ClipOf:  gfx.Rect{X: 0, Y: clipY, Width: 300, Height: clipH},
		}
	}
	const baseline = 49.75 // 内容区 [33.75, 53.75]
	cases := []struct {
		name string
		text gfx.Text
		want bool
	}{
		{"裁剪区上沿压在内容区下沿之下 0.25（行距里的 1.25 像素相交不算）", text(54, 100), true},
		{"裁剪区上沿恰好等于内容区下沿", text(53.75, 100), true},
		{"裁剪区上沿切进内容区 0.25：看得见字形的最下一排", text(53.5, 100), false},
		{"内容区整个在裁剪区里", text(0, 200), false},
		{"裁剪区下沿等于内容区上沿：整个在线之上", text(-100, 133.75), true},
		{"裁剪区下沿切进内容区", text(-100, 134), false},
	}
	for _, c := range cases {
		if got := contentClippedOut(c.text, baseline); got != c.want {
			t.Errorf("%s：contentClippedOut = %v，期望 %v", c.name, got, c.want)
		}
	}
	// 没有裁剪的文字永远不受这条口径影响。
	free := gfx.Text{Style: gfx.TextStyle{Size: size, Weight: gfx.WeightRegular}}
	if contentClippedOut(free, baseline) {
		t.Error("没有裁剪的文字不应判为被裁掉")
	}
}
