package kit

import (
	"math"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// noKernGolden 是 2026-11-19 在 React 参照里量到的宽度（button 外框宽与文字行矩形宽；font-medium，设备缩放 1）：
// 小字号档（根字号 14 px）的 text-sm 是 12.25 px，中档的是 14 px。
// 来源：parity\react-m22-t2、react-b042-1920 里 table / table-columns 场景的 facts.json（钮宽 = 文字宽，没有内边距）。
var noKernGolden = []struct {
	text  string
	size  float32
	width float64
}{
	{"技术论坛：SQLite WAL 模式的备份坑", 12.25, 206.78},
	{"YouTube: The Future of Local-First Software", 12.25, 253.89},
	{"Transformer 架构详解（视频课程第 3 讲）", 12.25, 234.06},
	{"YouTube: The Future of Local-First", 14, 227.531},
	{"Software", 14, 58.844},
}

// canvasOf 取一块无头画布，量字宽用。
func canvasOf(t *testing.T) (*Look, *gfx.Canvas) {
	t.Helper()
	var canvas *gfx.Canvas
	h := openPage(t, still(), 400, 100, func(_ *Look, c ui.C) { canvas = c.Canvas() })
	return h.look, canvas
}

// 逐字符求和等于渲染层不做字偶距的宽度（误差在 LayoutUnit 的 1/64 px 之内），而 Viva 整行量到的宽度（带字偶距）更窄：
// 这是 TextNoKern 占位的依据（台账 A-026：字符宽度两边一致，差的只是字偶对）。
func TestNoKernWidthMatchesChromium(t *testing.T) {
	look, canvas := canvasOf(t)
	t.Run("渲染层实测宽度", func(t *testing.T) {
		skipWin11FontMetricsInCI(t)
		for _, g := range noKernGolden {
			got := float64(look.NoKernWidth(canvas, g.text, g.size, WeightMedium))
			if math.Abs(got-g.width) > 0.03 {
				t.Errorf("%q（%v px）：逐字符求和 %.3f，渲染层 %.3f", g.text, g.size, got, g.width)
			}
		}
	})
	// 带字偶距的整行宽度比逐字符求和窄（含拉丁字母的标题）；CI 的英文版 Windows Server 回退字体没有对应字偶对，两者相等，故门控。
	t.Run("拉丁字偶距", func(t *testing.T) {
		skipWin11FontMetricsInCI(t)
		title := "YouTube: The Future of Local-First Software"
		kerned := float64(canvas.TextWidthWeight(title, 14, WeightMedium))
		plain := float64(look.NoKernWidth(canvas, title, 14, WeightMedium))
		if kerned >= plain-1 {
			t.Errorf("整行量宽 %.3f 应明显窄于逐字符求和 %.3f（字偶距）", kerned, plain)
		}
	})
	// 汉字的字宽两边一致（没有字偶对），CI 照常检查。
	if got, want := look.NoKernWidth(canvas, "汉字", 14, WeightMedium), canvas.TextWidthWeight("汉字", 14, WeightMedium); math.Abs(float64(got-want)) > 1e-3 {
		t.Errorf("汉字没有字偶对：逐字符求和 %v 应等于整行量宽 %v", got, want)
	}
}

// 折行翻转：可用宽度 284.125 px 落在 Viva 的整行宽（带字偶距，放得下）与渲染层的整行宽（不做字偶距，放不下）之间，
// 渲染层折成两行，第一行到「Local-First」，第二行是「Software」（facts.json 里两个文字矩形宽 227.531 与 58.844）。
func TestWrapNoKernFlipsLikeChromium(t *testing.T) {
	skipWin11FontMetricsInCI(t)
	look, canvas := canvasOf(t)
	title := "YouTube: The Future of Local-First Software"
	const avail = 284.125
	if kerned := canvas.TextWidthWeight(title, 14, WeightMedium); kerned > avail {
		t.Fatalf("前提不成立：带字偶距的整行宽 %v 应放得下 %v", kerned, avail)
	}
	layout := &noKernLayout{look: look, canvas: canvas, text: collapseSpaces(title), size: 14, weight: WeightMedium, line: 20}
	lines := layout.linesAt(avail)
	want := []string{"YouTube: The Future of Local-First ", "Software"}
	if len(lines) != 2 || strings.TrimRight(lines[0], " ") != strings.TrimRight(want[0], " ") || lines[1] != want[1] {
		t.Fatalf("折行 = %q，期望 %q", lines, want)
	}
	// 宽一点（放得下整行）就不折。
	if lines := layout.linesAt(float32(layout.measure(layout.text)) + 0.5); len(lines) != 1 {
		t.Fatalf("放得下时应一行：%q", lines)
	}
}

// 超过行数上限时最后一行以省略号收尾，且带省略号的宽度不超过可用宽度；不超限的文字原样。
func TestNoKernClampEndsWithEllipsis(t *testing.T) {
	look, canvas := canvasOf(t)
	text := "Designing Data-Intensive Applications and the long road to reliable local-first software"
	layout := &noKernLayout{look: look, canvas: canvas, text: text, size: 14, weight: WeightMedium, line: 20, maxLines: 2}
	const avail = 200
	lines := layout.linesAt(avail)
	if len(lines) != 2 {
		t.Fatalf("至多两行，得到 %d 行：%q", len(lines), lines)
	}
	if !strings.HasSuffix(lines[1], "\u2026") {
		t.Fatalf("最后一行应以省略号收尾：%q", lines[1])
	}
	if w := layout.measure(lines[1]); w > avail+1e-3 {
		t.Fatalf("带省略号的最后一行宽 %v 超过可用宽度 %v", w, avail)
	}
	// 第一行没有被动过，也放得下。
	if w := layout.measure(strings.TrimRight(lines[0], " ")); w > avail+1e-3 {
		t.Fatalf("第一行宽 %v 超过可用宽度 %v", w, avail)
	}
	// 第二行恰好排满（再加省略号就放不下）：要从行尾退几个字符，保留的部分是自然第二行的前缀。
	natural := (&noKernLayout{look: look, canvas: canvas, text: text, size: 14, weight: WeightMedium, line: 20}).linesAt(avail)
	tightWidth := float32(max(layout.measure(strings.TrimRight(natural[0], " ")), layout.measure(strings.TrimRight(natural[1], " ")))) + 0.5
	cut := (&noKernLayout{look: look, canvas: canvas, text: text, size: 14, weight: WeightMedium, line: 20, maxLines: 2}).linesAt(tightWidth)
	kept := strings.TrimSuffix(cut[1], "\u2026")
	if !strings.HasSuffix(cut[1], "\u2026") || !strings.HasPrefix(natural[1], kept) || len(kept) >= len(strings.TrimRight(natural[1], " ")) {
		t.Fatalf("排满的第二行应从行尾退几个字符再接省略号：自然 %q，截断 %q", natural[1], cut[1])
	}
	if w := layout.measure(cut[1]); w > tightWidth+1e-3 {
		t.Fatalf("截断后的最后一行宽 %v 超过可用宽度 %v", w, tightWidth)
	}
	short := &noKernLayout{look: look, canvas: canvas, text: "Short title", size: 14, weight: WeightMedium, line: 20, maxLines: 2}
	if got := short.linesAt(avail); len(got) != 1 || got[0] != "Short title" {
		t.Fatalf("没超限的文字应原样：%q", got)
	}
}

// 首尾空白去掉、连续空白折成一个空格（white-space: normal），换行、回车与制表符当空格；
// 不换行空格、全角空格、各种宽度的空格、换页不折叠（Chromium 实测，见 isCollapsibleSpace）。
func TestCollapseSpaces(t *testing.T) {
	for in, want := range map[string]string{
		"  a  b\t\nc  ":   "a b c",
		"a \r\n b":        "a b",
		"":                "",
		"   ":             "",
		"一  二":            "一 二",
		"x":               "x",
		"第一章\u3000总则":     "第一章\u3000总则",
		"A\u00a0\u00a0B":  "A\u00a0\u00a0B",
		"a\u2003b\u2009c": "a\u2003b\u2009c",
		"a\fb":            "a\fb",
		" \u3000 a":       "\u3000 a",
		"a\u3000 \u3000b": "a\u3000 \u3000b",
		"a \u00a0 b":      "a \u00a0 b",
	} {
		if got := collapseSpaces(in); got != want {
			t.Errorf("collapseSpaces(%q) = %q，期望 %q", in, got, want)
		}
	}
}

// 在版面里：按内容定宽的父盒里，文字盒的宽度是不做字偶距的整行宽；父盒窄于它时折成两行，行距是行高。
func TestTextNoKernLayoutInPage(t *testing.T) {
	title := "YouTube: The Future of Local-First Software"
	var look *Look
	natural := openPage(t, still(), 600, 200, func(l *Look, c ui.C) {
		look = l
		// 与知识库表格的标题钮同构：行里一个可收缩的列，文字盒在里面。
		ui.Row(c.Key("row")).Children(func() {
			ui.Column(c.Key("btn")).Shrink(1).MinWidth(0).Children(func() {
				l.TextNoKern(c.Key("t"), title, 14, 20, WeightMedium, l.T.Foreground, NoKernOptions{MaxLines: 2})
			})
		})
	})
	texts := natural.session.Snapshot().Texts
	if len(texts) != 1 {
		t.Fatalf("放得下时应一个 Label：%+v", texts)
	}
	var canvas *gfx.Canvas
	openPage(t, still(), 400, 100, func(_ *Look, c ui.C) { canvas = c.Canvas() })
	if want := look.NoKernWidth(canvas, title, 14, WeightMedium); math.Abs(float64(texts[0].Bounds.Width-want)) > 0.01 {
		t.Errorf("按内容定宽：文字盒宽 %v，期望不做字偶距的整行宽 %v", texts[0].Bounds.Width, want)
	}

	narrow := openPage(t, still(), 600, 200, func(l *Look, c ui.C) {
		ui.Column(c.Key("host")).Width(284.125).Children(func() {
			l.TextNoKern(c.Key("t"), title, 14, 20, WeightMedium, l.T.Foreground, NoKernOptions{MaxLines: 2})
		})
	})
	lines := narrow.session.Snapshot().Texts
	if len(lines) != 2 {
		t.Fatalf("窄于整行宽时应两个 Label：%+v", lines)
	}
	if dy := lines[1].Bounds.Y - lines[0].Bounds.Y; math.Abs(float64(dy-20)) > 0.01 {
		t.Errorf("两行相隔 %v，期望一个行高 20", dy)
	}
	if lines[0].Value != "YouTube: The Future of Local-First" || lines[1].Value != "Software" {
		t.Errorf("两行内容 = %q / %q", lines[0].Value, lines[1].Value)
	}
}

// 悬停下划线逐行各画一条：行盒顶按设备像素取整，线在「基线行 + 1」，宽度是这一行文字的占位宽度。
func TestTextNoKernUnderlinesEveryLine(t *testing.T) {
	const text = "本地优先软件本地优先软件本地优先软件" // 18 个全角字：200 px 宽的盒子里第一行 14 个、第二行 4 个
	white := gfx.RGBA(255, 255, 255, 1)
	black := tokens.RGBA(0, 0, 0, 1)
	h := openPage(t, still(), 400, 160, func(l *Look, c ui.C) {
		ui.Column(c).Fill().Background(white).Children(func() {
			ui.Column(c.Key("host")).Absolute().Left(10).Top(10).Width(200).Children(func() {
				l.TextNoKern(c.Key("t"), text, 14, 20, WeightMedium, black, NoKernOptions{Underline: true})
			})
		})
	})
	h.frames(3)
	img, err := h.session.Image()
	if err != nil {
		t.Fatal(err)
	}
	// 14 px、行高 20：下划线行 − round(行盒顶) = 16（underlineCases）。
	for i, chars := range []int{14, 4} {
		top := 10 + i*20
		width := float64(chars) * 14
		rows := underlineRows(img, 8, top-2, int(width)+6, 30, width*0.98) // 整行宽的深色只有下划线（字形的笔画行远不到这么满）
		if want := top + 16; len(rows) != 1 || rows[0] != want {
			t.Errorf("第 %d 行：下划线行 %v，期望只有第 %d 行", i+1, rows, want)
		}
	}
}

// 每帧每行标题都要排一次：冷（缓存里没有，折一个两行标题）与热（缓存命中，稳态下每帧的开销）各量一次，
// 防止逐字符求和成为帧耗时的大头（完成定义第 4 条）。
func benchmarkTextNoKernLines(b *testing.B, cold bool) {
	t := &testing.T{}
	look, canvas := canvasOf(t)
	layout := &noKernLayout{look: look, canvas: canvas, text: collapseSpaces("YouTube: The Future of Local-First Software 与 技术论坛：SQLite WAL 模式的备份坑"), size: 14, weight: WeightMedium, line: 20, maxLines: 2}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		layout.filled = false
		if cold {
			clear(look.noKern(canvas).lines)
		}
		_ = layout.linesAt(260)
	}
}

func BenchmarkTextNoKernLinesCold(b *testing.B) { benchmarkTextNoKernLines(b, true) }
func BenchmarkTextNoKernLinesWarm(b *testing.B) { benchmarkTextNoKernLines(b, false) }
