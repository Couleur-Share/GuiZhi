package main

import (
	"math"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/tools/iconcompile"
)

// 椭圆弧展开的几何守卫（台账 B-005）：展开后的三次曲线必须落在椭圆上、端点精确、没有椭圆弧的路径逐字节不变。

const svgHead = `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">`

func svgOf(inner string) []byte { return []byte(svgHead + inner + `</svg>`) }

// cubicPoint 取三次曲线上参数 t 处的点。
func cubicPoint(p0, c1, c2, p1 gfx.Point, t float64) (float64, float64) {
	u := 1 - t
	f := func(a, b, c, d float32) float64 {
		return u*u*u*float64(a) + 3*u*u*t*float64(b) + 3*u*t*t*float64(c) + t*t*t*float64(d)
	}
	return f(p0.X, c1.X, c2.X, p1.X), f(p0.Y, c1.Y, c2.Y, p1.Y)
}

// 数据库图标的圆柱底边：M3 5V19A9 3 0 0 0 21 19V5。圆心 (12,19)，半径 9×3，扫过 −180°，最低点 y=22。
func TestExpandDatabaseArcLandsOnEllipse(t *testing.T) {
	icon, err := iconcompile.Compile("db", mustExpand(t, svgOf(`<path d="M3 5V19A9 3 0 0 0 21 19V5"/>`)))
	if err != nil {
		t.Fatal(err)
	}
	var cur gfx.Point
	maxY, curves := 0.0, 0
	for _, c := range icon.Commands {
		switch c.Kind {
		case gfx.PathCommandMove, gfx.PathCommandLine:
			cur = c.Point
		case gfx.PathCommandCubic:
			curves++
			for i := 0; i <= 32; i++ {
				x, y := cubicPoint(cur, c.Control1, c.Control2, c.Point, float64(i)/32)
				// 椭圆方程：((x−12)/9)² + ((y−19)/3)² = 1；90° 一段的三次曲线近似误差 < 3e-4。
				if res := math.Abs(((x-12)/9)*((x-12)/9) + ((y-19)/3)*((y-19)/3) - 1); res > 1e-3 {
					t.Fatalf("曲线上的点 (%.4f, %.4f) 偏离椭圆：残差 %.5f", x, y, res)
				}
				maxY = math.Max(maxY, y)
			}
			cur = c.Point
		}
	}
	if curves != 2 {
		t.Errorf("半个椭圆应展开成 2 段（每段 90°），实际 %d 段", curves)
	}
	// 修复前 Viva 把这个最低点算成 23.16（16px 下 14.909px），正确是 22（14.667px）。
	if math.Abs(maxY-22) > 0.01 {
		t.Errorf("圆柱底边的最低点 y = %.4f，期望 22（16px 下 14.667px）", maxY)
	}
}

// 没有椭圆弧的图标（直线、圆弧、元素）不改一个字节：生成物只有含椭圆弧的图标会变。
func TestExpandLeavesOtherIconsByteIdentical(t *testing.T) {
	cases := map[string]string{
		"直线与曲线":            `<path d="M3 5V19L21 19C21 10 12 2 3 5Z"/>`,
		"圆弧（rx=ry）":        `<path d="M12 3a9 9 0 1 0 9 9"/>`,
		"相对的圆弧与标志位连写":      `<path d="m5 12a7 7 0 00-0 0"/>`,
		"椭圆元素走 Viva 自己的路径": `<ellipse cx="12" cy="5" rx="9" ry="3"/>`,
		"圆与线":              `<circle cx="12" cy="12" r="10"/><path d="M12 6v6l4 2"/>`,
		"零半径的弧由 Viva 当直线":  `<path d="M2 2A0 3 0 0 0 9 9"/>`,
	}
	for name, inner := range cases {
		src := svgOf(inner)
		got, err := expandEllipticalArcs(src)
		if err != nil {
			t.Fatalf("%s：%v", name, err)
		}
		if string(got) != string(src) {
			t.Errorf("%s：不含椭圆弧的 SVG 被改动了：\n%s", name, got)
		}
	}
}

// 只改含椭圆弧的 <path d>，同一个 SVG 里的其余元素与路径逐字节保留。
func TestExpandOnlyRewritesEllipticalPaths(t *testing.T) {
	plain := `<path d="M12 6v6l4 2"/>`
	src := svgOf(`<circle cx="12" cy="12" r="10"/>` + plain + `<path d="M3 12A9 3 0 0 0 21 12"/>`)
	got := string(mustExpand(t, src))
	if !strings.Contains(got, `<circle cx="12" cy="12" r="10"/>`+plain) {
		t.Errorf("圆与普通路径应原样保留：%s", got)
	}
	if strings.Contains(got, "A9 3") || !strings.Contains(got, "C") {
		t.Errorf("椭圆弧应改写成三次曲线：%s", got)
	}
}

// 旋转的椭圆、相对坐标、大弧与扫描方向、标志位与数字连写：展开后的点都在对应的椭圆上，端点精确。
func TestExpandRotatedRelativeAndFlagForms(t *testing.T) {
	type arc struct {
		name         string
		d            string
		x0, y0       float64 // 弧的起点
		rx, ry, phi  float64
		large, sweep bool
		x1, y1       float64
	}
	cases := []arc{
		{"旋转 30°、大弧、顺时针", "M4 6A8 4 30 1 1 15 11", 4, 6, 8, 4, 30, true, true, 15, 11},
		{"相对坐标的小弧", "M10 10a8 4 0 0 1 10 5", 10, 10, 8, 4, 0, false, true, 20, 15},
		{"标志位与数字连写", "M3 12a9 3 0 0021 0", 3, 12, 9, 3, 0, false, false, 24, 12},
		{"半径太小要放大", "M0 0A1 5 0 0 0 20 0", 0, 0, 1, 5, 0, false, false, 20, 0},
		{"两个标志位连写成 11", "M2 20A10 4 45 1115 5", 2, 20, 10, 4, 45, true, true, 15, 5},
	}
	for _, c := range cases {
		got, err := expandEllipticalArcs(svgOf(`<path d="` + c.d + `"/>`))
		if err != nil {
			t.Fatalf("%s：%v", c.name, err)
		}
		icon, err := iconcompile.Compile("arc", got)
		if err != nil {
			t.Fatalf("%s：编译失败：%v", c.name, err)
		}
		cx, cy, rx, ry, _, total := arcCenter(c.x0, c.y0, c.rx, c.ry, c.phi, c.large, c.sweep, c.x1, c.y1)
		// 标志位的语义：sweep=1 沿角度增大的方向（屏幕上顺时针），large=1 取大于半圈的那一条。
		if c.sweep != (total > 0) {
			t.Errorf("%s：sweep=%v 但扫过的角度是 %.3f 弧度", c.name, c.sweep, total)
		}
		if c.large != (math.Abs(total) > math.Pi+1e-9) {
			t.Errorf("%s：large=%v 但扫过的角度是 %.3f 弧度", c.name, c.large, total)
		}
		phi := c.phi * math.Pi / 180
		var cur gfx.Point
		var last gfx.Point
		for _, cmd := range icon.Commands {
			switch cmd.Kind {
			case gfx.PathCommandMove:
				cur = cmd.Point
			case gfx.PathCommandCubic:
				for i := 0; i <= 24; i++ {
					x, y := cubicPoint(cur, cmd.Control1, cmd.Control2, cmd.Point, float64(i)/24)
					ux := (math.Cos(phi)*(x-cx) + math.Sin(phi)*(y-cy)) / rx
					uy := (-math.Sin(phi)*(x-cx) + math.Cos(phi)*(y-cy)) / ry
					if res := math.Abs(ux*ux + uy*uy - 1); res > 1e-3 {
						t.Fatalf("%s：点 (%.3f, %.3f) 偏离椭圆，残差 %.5f", c.name, x, y, res)
					}
				}
				cur, last = cmd.Point, cmd.Point
			}
		}
		if math.Abs(float64(last.X)-c.x1) > 1e-4 || math.Abs(float64(last.Y)-c.y1) > 1e-4 {
			t.Errorf("%s：终点 (%.4f, %.4f) 不是弧的端点 (%.4f, %.4f)", c.name, last.X, last.Y, c.x1, c.y1)
		}
	}
}

// 中心参数化：数据库图标的底边圆心在弦的中点，起始角 π，扫过 −π。
func TestArcCenter(t *testing.T) {
	cx, cy, rx, ry, start, sweep := arcCenter(3, 19, 9, 3, 0, false, false, 21, 19)
	if math.Abs(cx-12) > 1e-9 || math.Abs(cy-19) > 1e-9 || rx != 9 || ry != 3 {
		t.Errorf("圆心/半径 = (%v, %v) %v×%v，期望 (12, 19) 9×3", cx, cy, rx, ry)
	}
	if math.Abs(math.Abs(start)-math.Pi) > 1e-9 || math.Abs(sweep+math.Pi) > 1e-9 {
		t.Errorf("起始角 %v、扫过 %v，期望 ±π 与 −π", start, sweep)
	}
}

// 读不懂的路径数据要报错，而不是悄悄漏掉一段弧。
func TestExpandRejectsMalformedPathData(t *testing.T) {
	for _, d := range []string{"M3 5 X 4 4", "M3 5A9 3 0 0", "M3 5 L 4"} {
		if _, err := expandEllipticalArcs(svgOf(`<path d="` + d + `"/>`)); err == nil {
			t.Errorf("%q：应当报错", d)
		}
	}
}

func mustExpand(t *testing.T, src []byte) []byte {
	t.Helper()
	out, err := expandEllipticalArcs(src)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
