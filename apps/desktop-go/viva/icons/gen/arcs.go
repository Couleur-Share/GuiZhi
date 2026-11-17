package main

// 椭圆弧展开（台账 B-005）。
//
// Viva 自带的图标编译器（tools/iconcompile/compile.go 的 emitArc）把 SVG 弧展开成三次曲线时，
// 控制点沿椭圆切线的偏移写成了 (-tangent·ry·sin, tangent·rx·cos)，正确应为 (-tangent·rx·sin, tangent·ry·cos)：
// rx 与 ry 对调。圆弧（rx = ry）不受影响，半径不等的椭圆弧控制点错位——lucide 的 Database、DatabaseBackup 两枚图标
// 的圆柱底边就是椭圆弧（`A9 3`），16px 下缘中心线偏 0.24px。
//
// 这里在交给 Viva 编译之前，把 <path d> 里半径不等的椭圆弧改写成三次曲线（按 W3C SVG 实现说明 F.6.5/F.6.6，
// 切线取对）；圆弧、直线、其余路径与其余元素一律原样保留，所以没有椭圆弧的图标生成结果逐字节不变。

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// pathDataAttr 匹配 <path> 等元素上的 d="…" 属性。生成脚本（gen-native-icons.mjs）总是写双引号、
// 属性名前总有一个空白，所以不会误中 id="…" 之类的属性。
var pathDataAttr = regexp.MustCompile(`(\sd=")([^"]*)(")`)

// expandEllipticalArcs 改写 SVG 源码里每个 d 属性中半径不等的椭圆弧；没有椭圆弧时原样返回 src。
func expandEllipticalArcs(src []byte) ([]byte, error) {
	var firstErr error
	out := pathDataAttr.ReplaceAllFunc(src, func(match []byte) []byte {
		parts := pathDataAttr.FindSubmatch(match)
		expanded, changed, err := expandPathArcs(string(parts[2]))
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return match
		}
		if !changed {
			return match
		}
		return []byte(string(parts[1]) + expanded + string(parts[3]))
	})
	if firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

// pathReader 按 SVG 路径数据的文法逐个读记号：分隔符是空白与逗号，数字可以连写（"1-2"、"1.5.5"），
// 圆弧的两个标志位各占一个字符且可以与后面的数字连写（"0 00-18 0"）。
type pathReader struct {
	s   string
	pos int
}

func (r *pathReader) skip() {
	for r.pos < len(r.s) {
		switch r.s[r.pos] {
		case ' ', '\t', '\n', '\r', '\f', ',':
			r.pos++
		default:
			return
		}
	}
}

// command 读下一个命令字母；读完或遇到不认识的字符时返回 false。
func (r *pathReader) command() (byte, bool) {
	r.skip()
	if r.pos >= len(r.s) {
		return 0, false
	}
	c := r.s[r.pos]
	if !strings.ContainsRune("MmLlHhVvCcSsQqTtAaZz", rune(c)) {
		return 0, false
	}
	r.pos++
	return c, true
}

// hasNumber 报告下一个记号是否以数字开头（命令的隐式重复参数组）。
func (r *pathReader) hasNumber() bool {
	r.skip()
	if r.pos >= len(r.s) {
		return false
	}
	c := r.s[r.pos]
	return c == '-' || c == '+' || c == '.' || (c >= '0' && c <= '9')
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

func (r *pathReader) number() (float64, bool) {
	r.skip()
	start, i, digits := r.pos, r.pos, 0
	if i < len(r.s) && (r.s[i] == '+' || r.s[i] == '-') {
		i++
	}
	for i < len(r.s) && isDigit(r.s[i]) {
		i++
		digits++
	}
	if i < len(r.s) && r.s[i] == '.' {
		i++
		for i < len(r.s) && isDigit(r.s[i]) {
			i++
			digits++
		}
	}
	if digits == 0 {
		return 0, false
	}
	if i < len(r.s) && (r.s[i] == 'e' || r.s[i] == 'E') {
		j := i + 1
		if j < len(r.s) && (r.s[j] == '+' || r.s[j] == '-') {
			j++
		}
		k := j
		for k < len(r.s) && isDigit(r.s[k]) {
			k++
		}
		if k > j {
			i = k
		}
	}
	v, err := strconv.ParseFloat(r.s[start:i], 64)
	if err != nil {
		return 0, false
	}
	r.pos = i
	return v, true
}

// flag 读圆弧的标志位：单个字符 0 或 1。
func (r *pathReader) flag() (float64, bool) {
	r.skip()
	if r.pos < len(r.s) && (r.s[r.pos] == '0' || r.s[r.pos] == '1') {
		v := float64(r.s[r.pos] - '0')
		r.pos++
		return v, true
	}
	return 0, false
}

// args 读一组参数：圆弧的第 4、5 个是标志位，其余是数字。
func (r *pathReader) args(cmd byte) ([]float64, bool) {
	n := argCount(cmd)
	out := make([]float64, 0, n)
	for i := 0; i < n; i++ {
		var v float64
		var ok bool
		if (cmd == 'A' || cmd == 'a') && (i == 3 || i == 4) {
			v, ok = r.flag()
		} else {
			v, ok = r.number()
		}
		if !ok {
			return nil, false
		}
		out = append(out, v)
	}
	return out, true
}

func argCount(cmd byte) int {
	switch cmd {
	case 'M', 'm', 'L', 'l', 'T', 't':
		return 2
	case 'H', 'h', 'V', 'v':
		return 1
	case 'C', 'c':
		return 6
	case 'S', 's', 'Q', 'q':
		return 4
	case 'A', 'a':
		return 7
	}
	return 0 // Z
}

func fmtNum(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// expandPathArcs 解析一段路径数据，把半径不等的椭圆弧换成绝对坐标的三次曲线；其余命令保持原有的相对/绝对写法。
// changed 为假表示没有需要改写的弧，调用方应保留原文。数据读不懂时返回错误（宁可让生成失败，也不悄悄漏掉一段弧）。
func expandPathArcs(d string) (out string, changed bool, err error) {
	r := &pathReader{s: d}
	var b strings.Builder
	var cx, cy, sx, sy float64 // 当前点与子路径起点（绝对坐标）
	write := func(cmd byte, args ...float64) {
		b.WriteByte(cmd)
		for i, a := range args {
			if i > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(fmtNum(a))
		}
	}
	for {
		cmd, ok := r.command()
		if !ok {
			r.skip()
			if r.pos < len(r.s) {
				return "", false, fmt.Errorf("路径数据在第 %d 个字符处读不懂：%q", r.pos, d)
			}
			break
		}
		rel := cmd >= 'a'
		up := cmd &^ 0x20
		if up == 'Z' {
			b.WriteByte(cmd)
			cx, cy = sx, sy
			continue
		}
		first := true
		for {
			a, ok := r.args(cmd)
			if !ok {
				return "", false, fmt.Errorf("命令 %c 的参数读不懂（第 %d 个字符附近）：%q", cmd, r.pos, d)
			}
			ox, oy := 0.0, 0.0
			if rel {
				ox, oy = cx, cy
			}
			switch up {
			case 'M':
				if first {
					write(cmd, a...)
					sx, sy = a[0]+ox, a[1]+oy
				} else if rel { // 第一对之后的坐标是隐式 lineto，写法与相对/绝对保持一致
					write('l', a...)
				} else {
					write('L', a...)
				}
				cx, cy = a[0]+ox, a[1]+oy
			case 'L', 'T':
				write(cmd, a...)
				cx, cy = a[0]+ox, a[1]+oy
			case 'H':
				write(cmd, a...)
				cx = a[0] + ox
			case 'V':
				write(cmd, a...)
				cy = a[0] + oy
			case 'C':
				write(cmd, a...)
				cx, cy = a[4]+ox, a[5]+oy
			case 'S', 'Q':
				write(cmd, a...)
				cx, cy = a[2]+ox, a[3]+oy
			case 'A':
				ex, ey := a[5]+ox, a[6]+oy
				rx, ry := math.Abs(a[0]), math.Abs(a[1])
				if rx != 0 && ry != 0 && rx != ry && (ex != cx || ey != cy) {
					for _, c := range arcToCubics(cx, cy, rx, ry, a[2], a[3] != 0, a[4] != 0, ex, ey) {
						write('C', c[:]...)
					}
					changed = true
				} else {
					write(cmd, a...)
				}
				cx, cy = ex, ey
			}
			first = false
			if !r.hasNumber() {
				break
			}
		}
	}
	if !changed {
		return d, false, nil
	}
	return b.String(), true, nil
}

// arcCenter 把圆弧的端点参数化换成中心参数化（W3C SVG 实现说明 F.6.5）。
// 半径不够大时按 F.6.6 放大；返回中心、放大后的半径、起始角与扫过的角度（弧度，扫过的角度带符号）。
func arcCenter(x0, y0, rx, ry, rotationDeg float64, largeArc, sweep bool, x, y float64) (centerX, centerY, outRx, outRy, startAngle, sweepAngle float64) {
	phi := rotationDeg * math.Pi / 180
	cos, sin := math.Cos(phi), math.Sin(phi)
	dx, dy := (x0-x)/2, (y0-y)/2
	x1 := cos*dx + sin*dy
	y1 := -sin*dx + cos*dy
	if lambda := x1*x1/(rx*rx) + y1*y1/(ry*ry); lambda > 1 {
		s := math.Sqrt(lambda)
		rx, ry = rx*s, ry*s
	}
	num := rx*rx*ry*ry - rx*rx*y1*y1 - ry*ry*x1*x1
	den := rx*rx*y1*y1 + ry*ry*x1*x1
	coef := 0.0
	if den > 0 {
		coef = math.Sqrt(math.Max(0, num/den))
	}
	if largeArc == sweep {
		coef = -coef
	}
	cxp := coef * rx * y1 / ry
	cyp := -coef * ry * x1 / rx
	centerX = cos*cxp - sin*cyp + (x0+x)/2
	centerY = sin*cxp + cos*cyp + (y0+y)/2
	startAngle = math.Atan2((y1-cyp)/ry, (x1-cxp)/rx)
	endAngle := math.Atan2((-y1-cyp)/ry, (-x1-cxp)/rx)
	sweepAngle = endAngle - startAngle
	if sweep && sweepAngle < 0 {
		sweepAngle += 2 * math.Pi
	}
	if !sweep && sweepAngle > 0 {
		sweepAngle -= 2 * math.Pi
	}
	return centerX, centerY, rx, ry, startAngle, sweepAngle
}

// arcToCubics 把一段椭圆弧展开成若干三次曲线（每段不超过 90°），每项是 c1x c1y c2x c2y x y（绝对坐标）。
// 椭圆上参数角 a 的点是 (rx·cos a, ry·sin a)，切向是 (−rx·sin a, ry·cos a)；控制点 = 端点 ± (4/3)·tan(Δ/4)·切向。
func arcToCubics(x0, y0, rx, ry, rotationDeg float64, largeArc, sweep bool, x, y float64) [][6]float64 {
	cx, cy, rx, ry, start, total := arcCenter(x0, y0, rx, ry, rotationDeg, largeArc, sweep, x, y)
	phi := rotationDeg * math.Pi / 180
	cos, sin := math.Cos(phi), math.Sin(phi)
	segments := int(math.Ceil(math.Abs(total)/(math.Pi/2) - 1e-9))
	if segments < 1 {
		segments = 1
	}
	delta := total / float64(segments)
	handle := 4.0 / 3.0 * math.Tan(delta/4)
	place := func(px, py float64) (float64, float64) { return cos*px - sin*py + cx, sin*px + cos*py + cy }
	out := make([][6]float64, 0, segments)
	for i := 0; i < segments; i++ {
		a0 := start + delta*float64(i)
		a1 := a0 + delta
		p0x, p0y := rx*math.Cos(a0), ry*math.Sin(a0)
		p1x, p1y := rx*math.Cos(a1), ry*math.Sin(a1)
		t0x, t0y := -rx*math.Sin(a0), ry*math.Cos(a0)
		t1x, t1y := -rx*math.Sin(a1), ry*math.Cos(a1)
		c1x, c1y := place(p0x+handle*t0x, p0y+handle*t0y)
		c2x, c2y := place(p1x-handle*t1x, p1y-handle*t1y)
		ex, ey := place(p1x, p1y)
		if i == segments-1 { // 终点取原始端点，避免浮点误差逐段累积
			ex, ey = x, y
		}
		out = append(out, [6]float64{c1x, c1y, c2x, c2y, ex, ey})
	}
	return out
}
