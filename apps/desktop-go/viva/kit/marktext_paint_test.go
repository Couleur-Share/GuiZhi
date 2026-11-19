package kit

import (
	"fmt"
	"image"
	"math"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// 命中词高亮的绘制层用例：圆角形状、折行处的平边、悬停下划线、文字基线与盒子高度、折行时行尾空格。
// 排版数字的对照在 marktext_test.go；这里管「画出来是什么样」，免得改动绘制时没人发现。

// amberCoverage 是像素 (x, y) 上琥珀底色的覆盖度（百分数）：白底上叠 0.9 的 amber-400 后绿通道约从 255 降到 197，
// 全覆盖约 98；文字墨迹会把数值抬得更高，圆角之外是 0。
func amberCoverage(img image.Image, x, y int) int {
	_, g, _, _ := img.At(x, y).RGBA()
	return int(math.Round((255 - float64(g>>8)) / 58 * 100))
}

// markedImage 在白底页面里画一个命中高亮的文字盒（左 10、上 10、宽 width，黑色宿主字色），返回整页画面。
func markedImage(t *testing.T, pageW, pageH int, width float32, text, query string, size, line, weight float32, opt MarkOptions) image.Image {
	t.Helper()
	white := gfx.RGBA(255, 255, 255, 1)
	black := tokens.RGBA(0, 0, 0, 1)
	h := openPage(t, still(), pageW, pageH, func(l *Look, c ui.C) {
		ui.Column(c).Fill().Background(white).Children(func() {
			ui.Column(c.Key("box")).Absolute().Left(10).Top(10).Width(width).Children(func() {
				if _, ok := l.TextMarked(c.Key("m"), text, query, size, line, weight, black, opt); !ok {
					t.Errorf("%q 里应有 %q 的命中", text, query)
				}
			})
		})
	})
	h.frames(3)
	img, err := h.session.Image()
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// 圆角的形状：rounded-sm 在根字号 16 px 时是 8 px 半径，14px/500 的底色在 x [38,70)、y [10,29)。
// 覆盖度取自与渲染层逐像素对过的画面（L3 通过）；只取没有文字墨迹的格子，免得墨迹把数值抬高。
func TestTextMarkedCornerShape(t *testing.T) {
	img := markedImage(t, 320, 80, 217, "产品设计原则：极简与克制", "设计", 14, 19.25, WeightMedium, MarkOptions{})
	cells := []struct {
		name    string
		x, y, w int
	}{
		{"左上", 41, 10, 0}, {"左上", 42, 10, 27}, {"左上", 43, 10, 55}, {"左上", 44, 10, 98},
		{"左上", 40, 11, 0}, {"左上", 41, 11, 56}, {"左上", 42, 11, 98},
		{"左上", 39, 12, 0}, {"左上", 40, 12, 98},
		{"左上", 38, 13, 0}, {"左上", 39, 13, 56}, {"左上", 40, 13, 98},
		{"左上", 38, 14, 27}, {"左上", 39, 14, 98},
		{"右上", 64, 10, 55}, {"右上", 65, 10, 20}, {"右上", 66, 10, 0},
		{"右上", 66, 11, 98}, {"右上", 67, 11, 0},
		{"右上", 67, 12, 98}, {"右上", 68, 12, 0},
		{"右上", 68, 13, 98}, {"右上", 69, 13, 0},
		{"右上", 68, 14, 98}, {"右上", 69, 14, 20},
		{"左下", 41, 28, 0}, {"左下", 42, 28, 20}, {"左下", 43, 28, 55}, {"左下", 44, 28, 98},
		{"左下", 40, 27, 0}, {"左下", 41, 27, 98},
		{"左下", 39, 26, 0}, {"左下", 40, 26, 98},
		{"左下", 38, 25, 0}, {"左下", 39, 25, 98},
		{"左下", 38, 24, 20}, {"左下", 39, 24, 98},
	}
	for _, c := range cells {
		if got := amberCoverage(img, c.x, c.y); abs(got-c.w) > 8 {
			t.Errorf("%s角 (%d,%d) 的底色覆盖度 %d%%，期望约 %d%%（半径 8 px 的圆角）", c.name, c.x, c.y, got, c.w)
		}
	}
}

// 底色在行盒里垂直居中：内容区（高 A + D）顶在行盒顶之下 floor((行高 − (A + D)) / 2) 处（半行距向下取整）。
// 下面的 (顶, 高) 是 2026-11-20 在 React 参照里量到的（D:\Project\GuiZhi-bench\tools\probe-mark-leading.mjs，
// 设备缩放 1）：界面里用到的行高都在 0 偏移的那一档，更大的行高才暴露取整规则，所以把它们一并钉住。
func TestTextMarkedVerticalCentering(t *testing.T) {
	cases := []struct {
		size, line, weight float32
		top, height        int // 底色顶相对行盒顶的偏移、底色高度
	}{
		{12, 16.5, WeightNormal, 0, 15}, {12, 18, WeightNormal, 1, 15}, {12, 20, WeightNormal, 2, 15},
		{12, 24, WeightNormal, 4, 15}, {12, 27, WeightNormal, 6, 15}, {12, 32, WeightNormal, 8, 15},
		{14, 19.25, WeightMedium, 0, 19}, {14, 24, WeightMedium, 2, 19}, {14, 28, WeightMedium, 4, 19}, {14, 33, WeightMedium, 7, 19},
		{12.25, 24, WeightMedium, 4, 16},
	}
	white := gfx.RGBA(255, 255, 255, 1)
	black := tokens.RGBA(0, 0, 0, 1)
	const pitch = 50
	h := openPage(t, still(), 320, 20+pitch*len(cases), func(l *Look, c ui.C) {
		ui.Column(c).Fill().Background(white).Children(func() {
			for i, tc := range cases {
				cc := c.Key(fmt.Sprintf("case-%d", i))
				ui.Column(cc).Absolute().Left(10).Top(float32(10+i*pitch)).Width(217).Children(func() {
					l.TextMarked(cc.Key("m"), "好的设计是尽可能少的设计", "设计", tc.size, tc.line, tc.weight, black, MarkOptions{})
				})
			}
		})
	})
	h.frames(3)
	img, err := h.session.Image()
	if err != nil {
		t.Fatal(err)
	}
	for i, tc := range cases {
		lineTop := 10 + i*pitch
		first, last := -1, -1
		for y := lineTop - 2; y < lineTop+int(tc.line)+2; y++ {
			solid := 0 // 一行里纯底色（不含文字墨迹）的像素数；圆角只切掉两端，整行至少有这么多
			for x := 0; x < 320; x++ {
				if c := amberCoverage(img, x, y); c >= 90 && c <= 105 {
					solid++
				}
			}
			if solid >= 8 {
				if first < 0 {
					first = y
				}
				last = y
			}
		}
		if first-lineTop != tc.top || last-first+1 != tc.height {
			t.Errorf("字号 %v 行高 %v 字重 %v：底色顶偏移 %d 高 %d，期望偏移 %d 高 %d", tc.size, tc.line, tc.weight, first-lineTop, last-first+1, tc.top, tc.height)
		}
	}
}

// 底色改用「缓存三角化的图标」画（markShape）：与直接画圆角路径（PaintRoundedCorners）逐像素相同，
// 只是不再每帧重新展平、三角化。覆盖两侧圆角、一侧圆一侧平、两侧都平，以及窄到半径要夹小的情形。
func TestMarkShapeMatchesRoundedPath(t *testing.T) {
	cases := []struct{ w, h, rl, rr float32 }{
		{32, 19, 8, 8}, {30, 19, 0, 8}, {30, 19, 8, 0}, {30, 19, 8, 8}, {30, 19, 0, 0}, {28, 19, 0, 0},
		{7, 19, 8, 8}, {40, 15, 8, 8}, {33, 16, 8, 8},
	}
	white := gfx.RGBA(255, 255, 255, 1)
	const pitch, offset = 30, 100
	h := openPage(t, still(), 220, 20+pitch*len(cases), func(l *Look, c ui.C) {
		ui.Column(c).Fill().Background(white).Children(func() {
			for i, tc := range cases {
				for side := 0; side < 2; side++ {
					cc := c.Key(fmt.Sprintf("shape-%d-%d", i, side))
					ui.Column(cc).Absolute().Left(float32(10+side*offset)).Top(float32(10+i*pitch)).Width(tc.w).Height(tc.h).
						Draw(func(d *ui.DrawContext) {
							if side == 0 {
								PaintRoundedCorners(d, d.Rect, markBg, tc.rl, tc.rr, tc.rr, tc.rl)
								return
							}
							snapped := SnapRect(d.Rect, d.Canvas.PixelScale())
							d.Canvas.DrawIcon(l.markShape(snapped.Width, snapped.Height, tc.rl, tc.rr), snapped, d.Fade(l.Col(markBg)))
						}).Children(func() {})
				}
			}
		})
	})
	h.frames(3)
	img, err := h.session.Image()
	if err != nil {
		t.Fatal(err)
	}
	for i, tc := range cases {
		top := 10 + i*pitch
		for y := top - 1; y <= top+int(tc.h); y++ {
			for x := 9; x <= 10+int(tc.w); x++ {
				if a, b := pixelOf(img, x, y), pixelOf(img, x+offset, y); a != b {
					t.Fatalf("用例 %+v 在 (%d,%d)：路径 %v，图标 %v", tc, x-10, y-top, a, b)
				}
			}
		}
		// 不是两张空白：盒子正中是底色。
		if c := amberCoverage(img, 10+int(tc.w)/2, top+int(tc.h)/2); c < 90 {
			t.Errorf("用例 %+v：盒子正中的底色覆盖度 %d%%，应已画上", tc, c)
		}
	}
	// 形状缓存有上限：尺寸一直在变时不无限增长。
	for i := 0; i < markShapeMax+50; i++ {
		h.look.markShape(float32(20+i), 19, 8, 8)
	}
	if n := len(h.look.markShapes); n > markShapeMax {
		t.Errorf("形状缓存 %d 项，超过上限 %d", n, markShapeMax)
	}
}

// 命中词被折成两行时，两段只在「词的起点」与「词的终点」处有圆角：第一段的右侧、第二段的左侧是平的
// （box-decoration-break: slice）。找一个恰好把「关于本地」折成两段的宽度来测，找不到就是前提不成立。
func TestTextMarkedWrapEdgesAreFlat(t *testing.T) {
	const text = "一条标题很长很长、需要折成两行才能放下的知识条目：关于本地优先的个人知识库的设计取舍与实现笔记"
	const query = "关于本地"
	const size, line = 14, 19.25
	look, canvas := canvasOf(t)
	b := &markBuilder{look: look, canvas: canvas, m: newMarkModel(text, MarkSpans(text, query)), size: size, weight: WeightMedium, pad: look.Space(0.5)}
	var (
		width float32
		res   *markResult
		at    int // 折断处所在行（第一段在 at，第二段在 at+1）
	)
	for w := float32(120); w <= 260 && res == nil; w++ {
		r := b.layout(w, 0, false)
		for i := 0; i+1 < len(r.lines); i++ {
			first, next := r.lines[i], r.lines[i+1]
			if f, g := first[len(first)-1], next[0]; f.marked && f.pr == 0 && g.marked && g.pl == 0 {
				width, res, at = w, r, i
				break
			}
		}
	}
	if res == nil {
		t.Fatal("没有找到把命中词折成两段的宽度，测试前提不成立")
	}
	img := markedImage(t, int(width)+40, 130, width, text, query, size, line, WeightMedium, MarkOptions{})
	rowTop := func(i int) int { return int(math.Floor(10 + float64(i)*line + 0.5)) } // 行盒顶（取整）= 底色顶
	f := res.lines[at][len(res.lines[at])-1]
	g := res.lines[at+1][0]
	cover := func(x, y int) int { return amberCoverage(img, x, y) }

	// 第一段：右侧平（顶行靠右缘 2 px 处仍是满底色），左侧是圆角（顶行靠左缘 1 px 处被切掉）。
	if c := cover(int(math.Floor(float64(10+f.x+f.w)))-2, rowTop(at)); c < 90 {
		t.Errorf("折断处第一段的右上角覆盖度 %d%%，应是平的（满底色）", c)
	}
	if c := cover(int(math.Ceil(float64(10+f.x-f.pl)))+1, rowTop(at)); c > 30 {
		t.Errorf("第一段的左上角覆盖度 %d%%，词的起点处应有圆角", c)
	}
	// 第二段：左侧平，右侧（词的终点）是圆角。
	if c := cover(int(math.Ceil(float64(10+g.x)))+1, rowTop(at+1)); c < 90 {
		t.Errorf("折断处第二段的左上角覆盖度 %d%%，应是平的（满底色）", c)
	}
	if c := cover(int(math.Floor(float64(10+g.x+g.w+g.pr)))-2, rowTop(at+1)); c > 30 {
		t.Errorf("第二段的右上角覆盖度 %d%%，词的终点处应有圆角", c)
	}
}

// 省略号与底色（对照 2026-11-20 的 probe-mark-ellipsis 在 React 里逐像素读到的结果，数据见 TestMarkEllipsisAgainstReactProbe）：
// 被省略号盖掉的命中词没有文字、底色照原位置铺（超出盒子右缘的部分裁平），省略号压在底色上、是宿主的字色（不是 amber-950）。
func TestTextMarkedEllipsisOverBox(t *testing.T) {
	const text = "好好好好好好好好好好设好好设好好好好好好"
	const left, top, size, line = 10, 10, 14, 20
	// 墨迹：绿通道明显低于底色（琥珀底 198、白底 255）。省略号的三个点只有两行高，抗锯齿后核心最深处也带彩边，所以只看绿通道。
	isInk := func(img image.Image, x, y int) bool {
		_, g, _, _ := img.At(x, y).RGBA()
		return g>>8 < 160
	}
	// inkColumns 是 [x0, x1) 内有墨迹的列（相对盒的左缘）。
	inkColumns := func(img image.Image, x0, x1 int) []int {
		var cols []int
		for x := left + x0; x < left+x1; x++ {
			for y := top; y < top+line; y++ {
				if isInk(img, x, y) {
					cols = append(cols, x-left)
					break
				}
			}
		}
		return cols
	}
	mid := top + 9
	asc, desc := ChromeContentRows(WeightNormal, size)
	boxTop := top + int(math.Floor((line-float64(asc+desc))/2)) // 底色的上沿行
	t.Run("宽 150：切在前缀里，命中词的底色露在省略号后面，右缘裁平", func(t *testing.T) {
		img := markedImage(t, 200, 40, 150, text, "设", size, line, WeightNormal, MarkOptions{Truncate: true})
		for x := 140; x < 150; x++ {
			if c := amberCoverage(img, left+x, mid); c < 90 {
				t.Errorf("x=%d 应是底色（命中词的底色在原位置 140 起），覆盖度 %d%%", x, c)
			}
		}
		if c := amberCoverage(img, left+149, boxTop); c < 90 {
			t.Errorf("右缘处的上沿覆盖度 %d%%，应在盒子右缘裁平（没有圆角）", c)
		}
		for x := 150; x < 170; x++ {
			if c := amberCoverage(img, left+x, mid); c > 10 {
				t.Errorf("x=%d 在盒子右缘之外，不该有底色（覆盖度 %d%%）", x, c)
			}
		}
	})
	t.Run("宽 159：命中词整块在盒内，省略号压在底色上且是宿主字色", func(t *testing.T) {
		img := markedImage(t, 200, 40, 159, text, "设", size, line, WeightNormal, MarkOptions{Truncate: true})
		for x := 140; x < 158; x++ {
			if c := amberCoverage(img, left+x, mid); c < 90 {
				t.Errorf("x=%d 应是底色，覆盖度 %d%%", x, c)
			}
		}
		if c := amberCoverage(img, left+158, mid); c > 10 {
			t.Errorf("x=158 在命中词的底色之外，覆盖度 %d%%", c)
		}
		if c := amberCoverage(img, left+157, boxTop); c > 30 {
			t.Errorf("命中词整块在盒内时右上角覆盖度 %d%%，应有圆角", c)
		}
		// 省略号起于 140（前缀末尾），墨迹压在底色上：三个点的核心列在 141–150 之内。
		if cols := inkColumns(img, 140, 158); len(cols) < 3 || cols[0] < 140 || cols[len(cols)-1] > 151 {
			t.Errorf("省略号的墨迹列 %v，期望至少三列、落在 140–151 内（宿主黑色，压在底色上）", cols)
		}
	})
	t.Run("宽 168：命中词的字放得下，省略号起于文字右缘 156，压在右内边距上", func(t *testing.T) {
		img := markedImage(t, 200, 40, 168, text, "设", size, line, WeightNormal, MarkOptions{Truncate: true})
		if c := amberCoverage(img, left+157, mid); c < 90 {
			t.Errorf("右内边距处 x=157 应是底色，覆盖度 %d%%", c)
		}
		if cols := inkColumns(img, 156, 168); len(cols) < 3 || cols[0] < 156 || cols[len(cols)-1] > 167 {
			t.Errorf("省略号的墨迹列 %v，期望至少三列、落在 156–167 内（起点 156 而不是 158）", cols)
		}
	})
}

// 悬停下划线：每个文字片段各一条，颜色是宿主字色；命中词两侧的内边距处没有线，缺口 2 px。
func TestTextMarkedUnderline(t *testing.T) {
	img := markedImage(t, 320, 80, 217, "产品设计原则：极简与克制", "设计", 14, 19.25, WeightMedium, MarkOptions{Underline: true})
	rows := underlineRows(img, 8, 8, 200, 30, 120)
	if len(rows) != 1 {
		t.Fatalf("应有且只有一行下划线（深色像素不少于 120 个），得到 %v", rows)
	}
	var runs [][2]int
	inRun := false
	for x := 0; x < 320; x++ {
		dark := darkness(img, x, rows[0]) > 0.5
		switch {
		case dark && !inRun:
			runs = append(runs, [2]int{x, x + 1})
			inRun = true
		case dark:
			runs[len(runs)-1][1] = x + 1
		default:
			inRun = false
		}
	}
	// 三段：产品 [10,38)、设计 [40,68)、原则：极简与克制 [70,182)；缺口 [38,40) 与 [68,70) 是命中词的内边距。
	want := [][2]int{{10, 38}, {40, 68}, {70, 182}}
	if len(runs) != len(want) {
		t.Fatalf("下划线应分三段 %v，得到 %v", want, runs)
	}
	for i, w := range want {
		if abs(runs[i][0]-w[0]) > 1 || abs(runs[i][1]-w[1]) > 1 {
			t.Errorf("第 %d 段下划线 %v，期望 %v", i+1, runs[i], w)
		}
	}
	// 没有 Underline 选项时不画。
	plain := markedImage(t, 320, 80, 217, "产品设计原则：极简与克制", "设计", 14, 19.25, WeightMedium, MarkOptions{})
	if rows := underlineRows(plain, 8, 8, 200, 30, 120); len(rows) != 0 {
		t.Errorf("没有 Underline 却画出了线：%v", rows)
	}
}

// 文字落在行盒里的位置带基线校正（与渲染层同行），盒子高度是「行数 × 行高」：后面接着的元素紧贴在它下面。
func TestTextMarkedLabelsAndHeight(t *testing.T) {
	const text = "一条标题很长很长、需要折成两行才能放下的知识条目：关于本地优先的个人知识库的设计取舍与实现笔记"
	const size, line = 14, 19.25
	dy := BaselineShift(WeightMedium, size, line, 1)
	after := BaselineShift(WeightNormal, 12, 16.5, 1)
	if dy == 0 || after == 0 {
		t.Fatalf("前提不成立：基线校正 %v / %v 应都非零，否则测不出「没做校正」", dy, after)
	}
	lines := len(layoutMarks(t, text, "设计", size, WeightMedium, 200, 0, false).lines)
	if lines < 2 {
		t.Fatalf("前提不成立：应折成多行，得到 %d 行", lines)
	}
	white := gfx.RGBA(255, 255, 255, 1)
	black := tokens.RGBA(0, 0, 0, 1)
	h := openPage(t, still(), 320, 160, func(l *Look, c ui.C) {
		ui.Column(c).Fill().Background(white).Children(func() {
			ui.Column(c.Key("box")).Absolute().Left(10).Top(10).Width(200).Children(func() {
				l.TextMarked(c.Key("m"), text, "设计", size, line, WeightMedium, black, MarkOptions{})
				l.TextPx(c.Key("after"), "AFTER", 12, 16.5, WeightNormal, black)
			})
		})
	})
	h.frames(3)
	var first, last *gfx.Text
	texts := h.session.Snapshot().Texts
	for i := range texts {
		if first == nil {
			first = &texts[i]
		}
		if texts[i].Value == "AFTER" {
			last = &texts[i]
		}
	}
	if first == nil || last == nil {
		t.Fatalf("没有找到首个片段或后续文字：%+v", texts)
	}
	if got := float64(first.Bounds.Y); math.Abs(got-float64(10+dy)) > 0.01 {
		t.Errorf("首个片段 Label 的 Y = %v，期望 行盒顶 10 + 基线校正 %v", got, dy)
	}
	wantGap := float64(lines)*line + float64(after) - float64(dy)
	if got := float64(last.Bounds.Y - first.Bounds.Y); math.Abs(got-wantGap) > 0.01 {
		t.Errorf("后续文字与首个片段的间距 %v，期望 %d 行 × %v + 校正差 = %v", got, lines, line, wantGap)
	}
}

// 折行时行尾的空格不占位：放得下「abcdefgh」就放在一行，哪怕带上后面的空格就放不下了；命中词落到下一行，行首没有空格。
func TestMarkWrapFitsBeforeTrailingSpace(t *testing.T) {
	look, canvas := canvasOf(t)
	const text, query = "abcdefgh ij", "ij"
	b := &markBuilder{look: look, canvas: canvas, m: newMarkModel(text, MarkSpans(text, query)), size: 12, weight: WeightNormal, pad: look.Space(0.5)}
	solid := b.width(0, 8)
	space := b.width(0, 9) - solid
	if space <= 0 {
		t.Fatalf("前提不成立：空格宽度 %v", space)
	}
	got := b.layout(solid+space/2, 0, false)
	checkLines(t, "行尾空格不占位", got, [][]wantFrag{
		{{"abcdefgh", false, 0, -1, 0, 0}},
		{{"ij", true, 2, -1, 2, 2}},
	}, 0.01)
}
