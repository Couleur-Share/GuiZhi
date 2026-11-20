package kit

import (
	"image"
	"math"
	"testing"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// 台账 B-043：<summary> 前面的 disclosure 标记。下面的读数是渲染层（Chromium）在 DPR 1 下的实测：
// 小、中、大字号档（text-xs = 10.5、12、13.5px，行高 14、16、18）各采一份「逐条结果」的 React 参照，
// 标记之后的文字离 <summary> 左缘 711/64、813/64、915/64 像素，朝下三角形的面积与质心如 disclosureCases。
type disclosureCase struct {
	name        string
	size, line  float32
	top         float32 // 行盒顶（逻辑像素）：.5 处的行盒顶会让基线四舍五入到下一个整像素
	advance     float32 // 渲染层读数：文字离 <summary> 左缘
	area        float64 // 朝下三角形的覆盖面积（像素）
	cx, cy      float64 // 质心，相对行盒左上角
	closedArea  float64 // 朝右三角形的覆盖面积；0 表示没有这一档的读数
	closedCX    float64
	closedCY    float64
	baselineRow float32
}

var disclosureCases = []disclosureCase{
	{name: "small", size: 10.5, line: 14, top: 108.5, advance: 711.0 / 64, area: 21.186, cx: 3.470, cy: 7.088, baselineRow: 11},
	{name: "medium", size: 12, line: 16, top: 124, advance: 813.0 / 64, area: 27.030, cx: 3.961, cy: 6.896,
		closedArea: 26.961, closedCX: 2.295, closedCY: 8.048, baselineRow: 12},
	{name: "large", size: 13.5, line: 18, top: 139.5, advance: 915.0 / 64, area: 34.576, cx: 4.458, cy: 8.809, baselineRow: 14},
}

func TestChromeDisclosureMarkerAdvanceMatchesTheRenderLayer(t *testing.T) {
	for _, c := range disclosureCases {
		m := ChromeDisclosureMarker(c.size, c.line, 1)
		if d := math.Abs(float64(m.Advance - c.advance)); d > 1.0/128 {
			t.Errorf("%s：文字让出 %.4f，渲染层读数 %.4f", c.name, m.Advance, c.advance)
		}
		if m.Baseline != c.baselineRow {
			t.Errorf("%s：基线行 %v，期望 %v", c.name, m.Baseline, c.baselineRow)
		}
		if d := math.Abs(float64(m.Side - c.size*2/3)); d > 1e-4 {
			t.Errorf("%s：三角形长边 %v，期望字号的 2/3 = %v", c.name, m.Side, c.size*2/3)
		}
	}
}

// paintedMarker 把标记画在行盒顶 (0, top) 的行盒里（白底、黑色，缩放 1），返回行盒周围的整张图。
func paintedMarker(t *testing.T, c disclosureCase, open bool) image.Image {
	t.Helper()
	m := ChromeDisclosureMarker(c.size, c.line, 1)
	return renderCells(t, 1, cellW, func(cell ui.C, _ int) {
		ui.Column(cell).Width(cellW).PadTop(c.top - 100*float32(math.Floor(float64(c.top)/100))).Children(func() {
			ui.Slot(cell).Size(m.Advance, c.line).Shrink(0).Draw(func(d *ui.DrawContext) {
				PaintDisclosure(d, d.Rect, m, open, tokens.RGBA(0, 0, 0, 1))
			})
		})
	})
}

// coverageOf 返回图里黑色覆盖面积与质心（相对行盒左上角 (0, rowTop)；白底、黑墨，覆盖率 = 1 − 灰度）。
func coverageOf(img image.Image, rowTop float64) (area, cx, cy float64) {
	b := img.Bounds()
	var mx, my float64
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, _, _, _ := img.At(x, y).RGBA()
			cov := 1 - float64(r)/65535
			area += cov
			mx += cov * (float64(x) + 0.5)
			my += cov * (float64(y) + 0.5)
		}
	}
	if area == 0 {
		return 0, 0, 0
	}
	return area, mx / area, my/area - rowTop
}

// 标记的面积与质心对上渲染层读数：面积 ±4%、质心 ±0.12 像素（等边三角形与实测的三角形只差边缘抗锯齿的覆盖率）。
// 行盒顶取 .5 的两档（小、大）同时验证基线四舍五入。
func TestPaintDisclosureMatchesTheRenderLayerCoverage(t *testing.T) {
	for _, c := range disclosureCases {
		rowTop := float64(c.top - 100*float32(math.Floor(float64(c.top)/100)))
		area, cx, cy := coverageOf(paintedMarker(t, c, true), rowTop)
		if math.Abs(area-c.area) > c.area*0.04 || math.Abs(cx-c.cx) > 0.12 || math.Abs(cy-c.cy) > 0.12 {
			t.Errorf("%s 朝下：面积 %.3f 质心 (%.3f, %.3f)，渲染层读数 面积 %.3f 质心 (%.3f, %.3f)", c.name, area, cx, cy, c.area, c.cx, c.cy)
		}
		if c.closedArea == 0 {
			continue
		}
		area, cx, cy = coverageOf(paintedMarker(t, c, false), rowTop)
		if math.Abs(area-c.closedArea) > c.closedArea*0.04 || math.Abs(cx-c.closedCX) > 0.12 || math.Abs(cy-c.closedCY) > 0.12 {
			t.Errorf("%s 朝右：面积 %.3f 质心 (%.3f, %.3f)，渲染层读数 面积 %.3f 质心 (%.3f, %.3f)", c.name, area, cx, cy, c.closedArea, c.closedCX, c.closedCY)
		}
	}
}

// 零或负的缩放按 1 处理。
func TestChromeDisclosureMarkerToleratesAnInvalidScale(t *testing.T) {
	a := ChromeDisclosureMarker(12, 16, 0)
	b := ChromeDisclosureMarker(12, 16, 1)
	if a != b {
		t.Errorf("缩放 0 应按 1：%+v 与 %+v", a, b)
	}
}
