package kit

import (
	"fmt"
	"image"
	"math"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
)

// cellW、cellH 是并排图标测试里每一格默认的宽高（逻辑像素，缩放 1）；格宽可以按测试另给。
const cellW, cellH = 100, 80

// renderCells 开一个无头会话（白底，缩放 1），把 n 个格子并排放在一页上，draw 在第 i 格里声明内容；返回整页图像。
// 一个测试只开一个会话：每个会话要 0.3 s 左右，逐个位置各开一个会让整个包的测试时间越过每包 10 s 的线。
func renderCells(t *testing.T, n, width int, draw func(c ui.C, i int)) image.Image {
	t.Helper()
	white := gfx.RGBA(255, 255, 255, 1)
	page := ui.NewPage(ui.PageConfig{Background: &white, View: func(c ui.C) {
		ui.Row(c).Children(func() {
			for i := 0; i < n; i++ {
				draw(c.Key(fmt.Sprintf("cell%d", i)), i)
			}
		})
	}})
	var canvas gfx.Canvas
	session, err := headless.Open(page.Config(app.Config{Title: t.Name(), Width: n * width, Height: cellH}), &canvas, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	for i := 0; i < 3; i++ {
		if err := session.Frame(); err != nil {
			t.Fatal(err)
		}
	}
	img, err := session.Image()
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// inkBoxOf 返回与背景（左上角像素）有差异的像素的外包围盒；一个像素都没有返回空矩形。
func inkBoxOf(img image.Image) image.Rectangle {
	b := img.Bounds()
	r0, g0, b0, _ := img.At(b.Min.X, b.Min.Y).RGBA()
	var box image.Rectangle
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			if r != r0 || g != g0 || bl != b0 {
				p := image.Rect(x, y, x+1, y+1)
				if box.Empty() {
					box = p
				} else {
					box = box.Union(p)
				}
			}
		}
	}
	return box
}

// cellInk 取第 i 格（格宽 width）里的墨迹包围盒，坐标换算成相对该格左上角。
func cellInk(img image.Image, i, width int) image.Rectangle {
	cell := image.Rect(i*width, 0, (i+1)*width, cellH)
	box := inkBoxOf(img.(interface {
		SubImage(image.Rectangle) image.Image
	}).SubImage(cell))
	if box.Empty() {
		return box
	}
	return box.Sub(image.Pt(i*width, 0))
}

// nativeInkBoxes 用 PaintIcon 把多色品牌图标并排画进若干格：第 i 格里图标盒子（side×side）的左上角在 (20+offs[i], 20+offs[i])。
// 返回每格里墨迹的包围盒。
func nativeInkBoxes(t *testing.T, side float32, offs []float32) []image.Rectangle {
	t.Helper()
	img := renderCells(t, len(offs), cellW, func(c ui.C, i int) {
		ui.Column(c).Width(cellW).PadLeft(20 + offs[i]).PadTop(20 + offs[i]).Children(func() {
			ui.Slot(c).Size(side, side).Shrink(0).Draw(func(d *ui.DrawContext) {
				PaintIcon(d.Canvas, icons.BrandYoutube, d.Rect, gfx.RGBA(0, 0, 0, 1))
			})
		})
	})
	out := make([]image.Rectangle, len(offs))
	for i := range offs {
		out[i] = cellInk(img, i, cellW)
	}
	return out
}

// 多色与填充图标走 Viva 原生 DrawIcon，它按「居中位置」取整原点；渲染层是「原点吸附到最近的整设备像素」。
// 12.25px 的图标落在 .375 上时两种规则差一整行（居中位置 .5 向上取整），B-042：表格小字号档的平台图标墨迹低一行。
// PaintIcon 要让原生路径也吸附原点：.375 与整数位置画在同一行，.5 才进到下一行（半个像素向上，LayoutUnit::Round）。
func TestPaintIconNativeSnapsOriginLikeChromium(t *testing.T) {
	offs := []float32{0, 0.375, 0.49, 0.5, 0.875}
	shift := []int{0, 0, 0, 1, 1} // 相对整数位置的整像素位移
	names := []string{"整数位置", ".375（B-042 的失败位置）", ".49 仍在下沿以内", ".5 向上取整", ".875 取到下一格"}
	boxes := nativeInkBoxes(t, 12.25, offs)
	base := boxes[0]
	if base.Empty() {
		t.Fatal("基准位置没有画出图标")
	}
	for i, got := range boxes {
		if got.Min.X-base.Min.X != shift[i] || got.Min.Y-base.Min.Y != shift[i] {
			t.Errorf("%s：墨迹起点位移 (%d, %d)，要 (%d, %d)（基准 %v，得 %v）",
				names[i], got.Min.X-base.Min.X, got.Min.Y-base.Min.Y, shift[i], shift[i], base, got)
		}
		if got.Dx() != base.Dx() || got.Dy() != base.Dy() {
			t.Errorf("%s：墨迹宽高 %dx%d 与基准 %dx%d 不同（边长应一律取整成 12）", names[i], got.Dx(), got.Dy(), base.Dx(), base.Dy())
		}
	}
}

// 整数边长（h-4 等，中字号档）的图标落在 .375 上：吸附与居中取整得同一个原点，改动不能让这种常见情形变样。
func TestPaintIconNativeIntegralSideUnchanged(t *testing.T) {
	boxes := nativeInkBoxes(t, 14, []float32{0, 0.125, 0.375, 0.49, 0.5})
	base := boxes[0]
	if base.Empty() {
		t.Fatal("基准位置没有画出图标")
	}
	for i, off := range []float32{0.125, 0.375, 0.49} {
		if got := boxes[i+1]; got != base {
			t.Errorf("14px 图标在 +%v 处的墨迹 %v 与整数位置 %v 不同", off, got, base)
		}
	}
	if got := boxes[4]; got.Min.X-base.Min.X != 1 || got.Min.Y-base.Min.Y != 1 {
		t.Errorf("14px 图标在 +0.5 处应整体下移右移一格：基准 %v，得 %v", base, got)
	}
}

// nativeIconBounds 的数值：原点按 iconPlacement 吸附，边长取整，缩放不为 1 时换算成逻辑像素。
func TestNativeIconBoundsValues(t *testing.T) {
	cases := []struct {
		name   string
		scale  float32
		bounds gfx.Rect
		want   gfx.Rect
	}{
		{"小字号档表格平台图标（.375 落在下沿以内）", 1, gfx.Rect{X: 1090.25, Y: 292.375, Width: 12.25, Height: 12.25}, gfx.Rect{X: 1090, Y: 292, Width: 12, Height: 12}},
		{"小数 .875 取到下一格", 1, gfx.Rect{X: 1100.75, Y: 245.875, Width: 12.25, Height: 12.25}, gfx.Rect{X: 1101, Y: 246, Width: 12, Height: 12}},
		{"恰好半个像素向上", 1, gfx.Rect{X: 100.5, Y: 292.5, Width: 12.25, Height: 12.25}, gfx.Rect{X: 101, Y: 293, Width: 12, Height: 12}},
		{"大字号档 15.75 取整成 16", 1, gfx.Rect{X: 10.25, Y: 20.25, Width: 15.75, Height: 15.75}, gfx.Rect{X: 10, Y: 20, Width: 16, Height: 16}},
		{"盒子不是方块时取居中的方块", 1, gfx.Rect{X: 10, Y: 20, Width: 12.25, Height: 20}, gfx.Rect{X: 10, Y: 24, Width: 12, Height: 12}},
		// 设备像素：x 12.5 → 13、y 25.5 → 26、边长 17.5 → 18；除以 1.25 换回逻辑像素。
		{"缩放 1.25：设备像素上吸附，再换回逻辑像素", 1.25, gfx.Rect{X: 10, Y: 20.4, Width: 14, Height: 14}, gfx.Rect{X: 10.4, Y: 20.8, Width: 14.4, Height: 14.4}},
	}
	for _, c := range cases {
		var canvas gfx.Canvas
		canvas.SetPixelScale(c.scale)
		got := nativeIconBounds(&canvas, c.bounds)
		for name, pair := range map[string][2]float32{
			"X": {got.X, c.want.X}, "Y": {got.Y, c.want.Y}, "宽": {got.Width, c.want.Width}, "高": {got.Height, c.want.Height},
		} {
			if math.Abs(float64(pair[0]-pair[1])) > 1e-4 {
				t.Errorf("%s：%s = %v，要 %v（得 %+v）", c.name, name, pair[0], pair[1], got)
			}
		}
	}
	// 边长为零：原样交还，由 DrawIcon 自己什么也不画。
	var canvas gfx.Canvas
	canvas.SetPixelScale(1)
	zero := gfx.Rect{X: 5, Y: 6}
	if got := nativeIconBounds(&canvas, zero); got != zero {
		t.Errorf("零边长应原样交还，得 %+v", got)
	}
}
