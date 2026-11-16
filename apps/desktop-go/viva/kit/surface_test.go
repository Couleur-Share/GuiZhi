package kit

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

func TestSnapRect(t *testing.T) {
	cases := []struct {
		name  string
		in    gfx.Rect
		scale float32
		want  gfx.Rect
	}{
		// 2026-11-16 在小字号档的卡片上量到：Chromium 画 357～468（高 111），不是 Viva 默认的 357～469。
		{"起点终点各自取整", gfx.Rect{X: 21, Y: 356.75, Width: 484, Height: 111.5}, 1, gfx.Rect{X: 21, Y: 357, Width: 484, Height: 111}},
		{"半个像素向上", gfx.Rect{X: 0.5, Y: 0.5, Width: 10, Height: 10}, 1, gfx.Rect{X: 1, Y: 1, Width: 10, Height: 10}},
		{"整数不变", gfx.Rect{X: 3, Y: 4, Width: 5, Height: 6}, 1, gfx.Rect{X: 3, Y: 4, Width: 5, Height: 6}},
		{"200% 缩放按设备像素取整", gfx.Rect{X: 0.3, Y: 0.3, Width: 10.1, Height: 10.1}, 2, gfx.Rect{X: 0.5, Y: 0.5, Width: 10, Height: 10}},
		{"非零尺寸至少一个设备像素", gfx.Rect{X: 0.1, Y: 0.1, Width: 0.2, Height: 0.2}, 1, gfx.Rect{X: 0, Y: 0, Width: 1, Height: 1}},
	}
	for _, c := range cases {
		if got := SnapRect(c.in, c.scale); got != c.want {
			t.Errorf("%s：SnapRect(%v, %v) = %v，期望 %v", c.name, c.in, c.scale, got, c.want)
		}
	}
}

// TestPaintSurfaceSnapsLikeChromium 用无头渲染核对：小数布局下表面的背景矩形边缘与 SnapRect 一致。
func TestPaintSurfaceSnapsLikeChromium(t *testing.T) {
	look := NewLook(DefaultSpec())
	surface := Surface{Bg: tokens.RGBA(10, 20, 30, 1), Radius: 8}
	page := ui.NewPage(ui.PageConfig{View: func(c ui.C) {
		ui.Column(c).PadTop(356.75).PadLeft(21).Children(func() {
			ui.Column(c).Width(100).Height(111.5).Draw(func(d *ui.DrawContext) {
				look.PaintSurface(d, surface)
			})
		})
	}})
	var canvas gfx.Canvas
	session, err := headless.Open(page.Config(app.Config{Title: "surface", Width: 200, Height: 600}), &canvas, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	for i := 0; i < 3; i++ {
		if err := session.Frame(); err != nil {
			t.Fatal(err)
		}
	}
	want := gfx.Rect{X: 21, Y: 357, Width: 100, Height: 111}
	for _, p := range session.Snapshot().Primitives {
		if p.Kind == app.RectanglePrimitive && p.Bounds == want {
			return
		}
	}
	t.Errorf("没有找到边缘吸附为 %v 的背景矩形；图元：%v", want, session.Snapshot().Primitives)
}
