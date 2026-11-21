package kit

import (
	"math"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// 本文件测 Markdown 正文的原生排版（prose.go）：块的外边距模型（折叠、字号、行高）、行内样式与装饰、折行、记号、引用、分隔线与代码块。
// 数值都取自渲染层 globals.css 的 `.prose` 规则（见 prose.go 文件头），测试里用字面量写出，不引用 prose.go 的常数——
// 常数被改动时这里要能报出来。逐像素的对拍由 viva/parity 负责，这里钉结构与数值。

// proseModel 返回 src 解析并建好排版树的结果（正文 16px，无需窗口）。
func proseModel(t *testing.T, src string) []*proseNode {
	t.Helper()
	look := NewLook(still())
	look.Scale = 1
	if look.Rem(1) != 16 {
		t.Fatalf("测试按 16px 根字号写，实际 %v", look.Rem(1))
	}
	p := &proseBuilder{l: look, base: look.Rem(1)}
	return p.build(ParseMarkdown(src), proseInherit{color: look.T.Foreground}, 0)
}

type proseWant struct{ size, line, top, bottom float32 }

func checkProseNode(t *testing.T, name string, n *proseNode, w proseWant) {
	t.Helper()
	if n.size != w.size || n.line != w.line || n.top != w.top || n.bottom != w.bottom {
		t.Errorf("%s：字号 %v 行高 %v 上 %v 下 %v，期望 字号 %v 行高 %v 上 %v 下 %v",
			name, n.size, n.line, n.top, n.bottom, w.size, w.line, w.top, w.bottom)
	}
}

// 各类块的字号、行高与折叠后的上下外边距（16px 根字号；行高与外边距截断到 1/64）。
func TestProseBlockMetricsFollowTheStylesheet(t *testing.T) {
	lu := layoutUnit
	para := proseWant{16, lu(25.6), 0, 12}
	checkProseNode(t, "段落", proseModel(t, "正文")[0], para)

	// 独占一行的粗体是小节标题：行高 1.45、上 1.15em（排第一个时为 0）、下 0.4em。
	lead := proseModel(t, "前文\n\n**小节**\n\n后文")
	checkProseNode(t, "小节标题", lead[1], proseWant{16, lu(23.2), lu(18.4), lu(6.4)})
	checkProseNode(t, "小节标题之前的段落", lead[0], para)
	checkProseNode(t, "排第一个的小节标题", proseModel(t, "**小节**\n\n后文")[0], proseWant{16, lu(23.2), 0, lu(6.4)})

	// 标题：字号 1.5 / 1.25 / 1.1 / 1 倍，行高 1.6 倍自己的字号，上 1em、下 0.5em（都按标题自己的字号）。
	for src, w := range map[string]proseWant{
		"# 标题":    {24, lu(38.4), lu(24), lu(12)},
		"## 标题":   {20, lu(32), lu(20), lu(10)},
		"### 标题":  {17.6, lu(28.16), lu(17.6), lu(8.8)},
		"#### 标题": {16, lu(25.6), lu(16), lu(8)},
	} {
		checkProseNode(t, src, proseModel(t, src)[0], w)
	}

	checkProseNode(t, "分隔线", proseModel(t, "---")[0], proseWant{16, 0, 16, 16})
	// 代码块：字号 0.9em，行高仍按正文 1.6 倍，外边距 my-3（12px）。
	checkProseNode(t, "代码块", proseModel(t, "```\nx\n```")[0], proseWant{14.4, lu(25.6), 12, 12})

	// 紧凑列表：项里的段落没有外边距，项下 0.35em，列表下 0.75em。
	tight := proseModel(t, "- 甲\n- 乙")[0]
	checkProseNode(t, "紧凑列表", tight, proseWant{16, 0, 0, 12})
	if len(tight.kids) != 2 {
		t.Fatalf("列表应有两项，实际 %d", len(tight.kids))
	}
	checkProseNode(t, "紧凑列表项", tight.kids[0], proseWant{16, 0, 0, lu(5.6)})
	checkProseNode(t, "紧凑列表项里的段落", tight.kids[0].kids[0], proseWant{16, lu(25.6), 0, 0})
	// 宽松列表：项里的段落带 0.75em 下外边距，折叠到项上（取较大者）。
	loose := proseModel(t, "- 甲\n\n- 乙")[0]
	checkProseNode(t, "宽松列表项", loose.kids[0], proseWant{16, 0, 0, 12})
	checkProseNode(t, "宽松列表项里的段落", loose.kids[0].kids[0], proseWant{16, lu(25.6), 0, 12})
}

// 容器（列表、列表项、引用）没有垂直内边距与边框，首尾外边距与首尾子块折叠：取较大者，首尾各用各的子块。
func TestProseContainersCollapseMarginsWithTheirEdgeChildren(t *testing.T) {
	lu := layoutUnit
	// 首块是标题（上外边距 24）：列表项与列表的上外边距都抬到 24。
	list := proseModel(t, "- # 标题")[0]
	if list.top != 24 || list.kids[0].top != 24 {
		t.Errorf("列表 / 项的上外边距 %v / %v，期望都折叠成首子块的 24", list.top, list.kids[0].top)
	}
	// 项里先段落（下 12）后二级标题（下 10）：项的下外边距取最后一块与项自己（0.35em）里较大的 10。
	item := proseModel(t, "- 文字\n\n  ## 标题")[0].kids[0]
	if item.bottom != 10 {
		t.Errorf("项的下外边距 %v，期望取末块（二级标题）的 10", item.bottom)
	}
	if want := lu(0.35 * 16); proseModel(t, "- 甲")[0].kids[0].bottom != want {
		t.Errorf("紧凑项的下外边距不是 0.35em")
	}
	// 引用：下外边距折叠自末块（段落 12）。
	quote := proseModel(t, "> 引文")[0]
	if quote.top != 0 || quote.bottom != 12 {
		t.Errorf("引用的外边距 上 %v 下 %v，期望 0 / 12", quote.top, quote.bottom)
	}
	// 引用里的文字是 muted 斜体；引用里的列表项也继承。
	inner := quote.kids[0]
	look := NewLook(still())
	if inner.inh.color != look.T.MutedForeground || !inner.inh.italic {
		t.Errorf("引用里的段落应是 muted 斜体：%+v", inner.inh)
	}
	li := proseModel(t, "> - 甲")[0].kids[0].kids[0]
	if li.inh.color != look.T.MutedForeground || !li.inh.italic {
		t.Errorf("引用里列表项的段落应继承 muted 斜体：%+v", li.inh)
	}
	if outer := proseModel(t, "正文")[0]; outer.inh.color != look.T.Foreground || outer.inh.italic {
		t.Errorf("引用之外应是 foreground 正文：%+v", outer.inh)
	}
	// 嵌套深度决定无序记号的形状：三层依次是实心圆、空心圆、方块。
	deep := proseModel(t, "- 甲\n  - 乙\n    - 丙")[0]
	d1 := deep.kids[0].kids[1]
	d2 := d1.kids[0].kids[1]
	if deep.kids[0].bullet != bulletDisc || d1.kids[0].bullet != bulletCircle || d2.kids[0].bullet != bulletSquare {
		t.Errorf("记号形状 %v/%v/%v，期望实心圆/空心圆/方块", deep.kids[0].bullet, d1.kids[0].bullet, d2.kids[0].bullet)
	}
	// 有序列表从起始号数起，记号带句点。
	ol := proseModel(t, "3. 甲\n4. 乙")[0]
	if ol.kids[0].marker != "3." || ol.kids[1].marker != "4." {
		t.Errorf("有序记号 %q %q，期望 3. 4.", ol.kids[0].marker, ol.kids[1].marker)
	}
}

// 小节标题的判定：段落里恰好一个纯粗体（可后跟普通文字），没有别的行内元素。
func TestProseLeadParagraphClassification(t *testing.T) {
	cases := map[string]bool{
		"**标题**":                      true,
		"**标题** 后面跟着文字":                true,
		"文字在前 **标题**":                  true,
		"**标题**  \n下一行":                true,
		"**甲** 夹着 **乙**":              false,
		"普通文字":                        false,
		"***标题***":                    false,
		"~~**标题**~~":                  false,
		"**`标题`**":                    false,
		"[**标题**](https://example.com)": false,
		"**标题** *斜体*":                  false,
		"**标题** ~~删除~~":                false,
		"**标题** `代码`":                  false,
		"**标题** [链接](https://a.example)": false,
		"*斜体*":                        false,
	}
	for src, want := range cases {
		blocks := ParseMarkdown(src)
		if len(blocks) != 1 || blocks[0].Kind != MdParagraph {
			t.Fatalf("%q 应解析成一个段落：%+v", src, blocks)
		}
		if got := isLeadPara(&blocks[0]); got != want {
			t.Errorf("isLeadPara(%q) = %v，期望 %v", src, got, want)
		}
	}
}

// ---- 渲染 ----

type proseShot struct {
	h      *harness
	canvas *gfx.Canvas
	snap   app.Snapshot
}

// proseOpen 在 width 宽的盒子里排 blocks（盒子在页面左上角，上方外边距 lead），出几帧后取快照。
func proseOpen(t *testing.T, width, lead float32, blocks []MdBlock) *proseShot {
	t.Helper()
	s := &proseShot{}
	s.h = openPage(t, still(), 400, 900, func(l *Look, c ui.C) {
		s.canvas = c.Canvas()
		ui.Column(c).Width(width).Children(func() {
			l.Prose(c.Key("prose"), blocks, lead)
		})
	})
	s.snap = s.h.session.Snapshot()
	return s
}

func para(spans ...MdSpan) MdBlock { return MdBlock{Kind: MdParagraph, Spans: spans} }
func txt(s string) MdSpan          { return MdSpan{Text: s} }

func (s *proseShot) texts() []gfx.Text { return s.snap.Texts }

// rects 是页面里画出的矩形（去掉整页背景）。
func (s *proseShot) rects() []app.Primitive {
	var out []app.Primitive
	for _, p := range s.snap.Primitives {
		if p.Kind == app.RectanglePrimitive && !(p.Bounds.Width >= 400 && p.Bounds.Height >= 900) {
			out = append(out, p)
		}
	}
	return out
}

func (s *proseShot) textAt(t *testing.T, i int) gfx.Text {
	t.Helper()
	if i >= len(s.snap.Texts) {
		t.Fatalf("只有 %d 段文字，想取第 %d 段：%+v", len(s.snap.Texts), i, s.snap.Texts)
	}
	return s.snap.Texts[i]
}

func values(ts []gfx.Text) []string {
	out := make([]string, len(ts))
	for i, x := range ts {
		out[i] = x.Value
	}
	return out
}

func approx(a, b float32) bool { return math.Abs(float64(a-b)) < 0.01 }

const proseLine16 = float32(25.59375) // 16px 的 1.6 倍行高截断到 1/64

// 同一段落折出的行：行高 25.59375 一行，每行回到左缘；行尾空格悬挂（不进 Label）；单个字符串最宽的行不超过容器。
func TestProseParagraphWrapsOnWordBoundariesWithHangingSpaces(t *testing.T) {
	blocks := []MdBlock{para(txt("aa bb cc"))}
	measure := proseOpen(t, 400, 0, blocks)
	w := func(s string) float32 { return measure.canvas.TextWidthWeight(s, 16, WeightNormal) }

	one := proseOpen(t, 400, 0, blocks)
	if got := values(one.texts()); len(got) != 1 || got[0] != "aa bb cc" {
		t.Fatalf("容器够宽时应是一行 %q：%v", "aa bb cc", got)
	}

	// 宽度只够放一个词（连同它后面的空格放不下）：行尾空格悬挂，每个词一行。
	s := proseOpen(t, max(w("aa"), w("bb"), w("cc")), 0, blocks)
	got := s.texts()
	if v := values(got); len(v) != 3 || v[0] != "aa" || v[1] != "bb" || v[2] != "cc" {
		t.Fatalf("宽度恰好放下一个词时应拆成三行：%v", v)
	}
	for i, x := range got {
		if !approx(x.Bounds.X, 0) {
			t.Errorf("第 %d 行起点 x = %v，期望回到左缘", i, x.Bounds.X)
		}
		if want := got[0].Bounds.Y + float32(i)*proseLine16; !approx(x.Bounds.Y, want) {
			t.Errorf("第 %d 行 y = %v，期望 %v（行高 25.59375）", i, x.Bounds.Y, want)
		}
	}

	// 行尾空格计入「已用宽度」：排满 "aa bb" 之后再加 "cc" 放不下，但 "aa bb" 自己恰好放得下（容差 1e-3）。
	full := w("aa") + w(" ") + w("bb")
	s = proseOpen(t, full-0.0005, 0, blocks)
	if v := values(s.texts()); len(v) != 2 || v[0] != "aa bb" || v[1] != "cc" {
		t.Fatalf("宽度恰好放下 \"aa bb\" 时应拆成 [aa bb][cc]：%v", v)
	}
	// 比 "aa bb" 窄一点（空格要占位）：第一行只剩 "aa"。
	s = proseOpen(t, full-0.5, 0, blocks)
	if v := values(s.texts()); len(v) < 2 || v[0] != "aa" || strings.Join(v, " ") != "aa bb cc" {
		t.Fatalf("宽度比 \"aa bb\" 窄时第一行只能放 \"aa\"：%q", v)
	}
}

// 一个词放不下一行时逐字符硬断，每行至少一个字符（容器再窄也不会死循环）。
func TestProseHardBreaksAWordThatDoesNotFit(t *testing.T) {
	blocks := []MdBlock{para(txt("abcdefghij"))}
	measure := proseOpen(t, 400, 0, blocks)
	w := func(s string) float32 { return measure.canvas.TextWidthWeight(s, 16, WeightNormal) }

	s := proseOpen(t, w("abcd")+0.3, 0, blocks)
	v := values(s.texts())
	if strings.Join(v, "") != "abcdefghij" || len(v) < 2 {
		t.Fatalf("长词应被硬断成多行，拼起来仍是原文：%v", v)
	}
	for _, line := range v {
		if w(line) > w("abcd")+0.3+0.01 {
			t.Errorf("行 %q 宽 %v，超过容器 %v", line, w(line), w("abcd")+0.3)
		}
	}
	// 容差：硬断时刚好排满的一行也放得下（差 0.0005 之内）。
	s = proseOpen(t, w("abcd")-0.0005, 0, blocks)
	if v := values(s.texts()); len(v) == 0 || v[0] != "abcd" {
		t.Fatalf("硬断的第一行应恰好放下 abcd：%v", v)
	}
	// 容器窄到放不下一个字符：每行一个字符，不死循环。
	s = proseOpen(t, 1, 0, blocks)
	if v := values(s.texts()); len(v) != 10 {
		t.Fatalf("容器只有 1px 宽时应每个字符一行：%v", v)
	}
	// 一个字符后面跟着能放下的内容：硬断不留空行。
	s = proseOpen(t, w("abcdefghij"), 0, blocks)
	if v := values(s.texts()); len(v) != 1 {
		t.Fatalf("整段刚好放满一行时不应多出行：%v", v)
	}
}

// 空白折叠：连续空白合成一个、段首与行尾的空白去掉、硬换行前后的空白去掉、跨片段的空白也只留一个。
func TestProseCollapsesWhitespace(t *testing.T) {
	s := proseOpen(t, 400, 0, []MdBlock{para(txt("  甲   乙  "))})
	if v := values(s.texts()); len(v) != 1 || v[0] != "甲 乙" {
		t.Errorf("段首、连续、行尾空白应折叠：%q", v)
	}
	s = proseOpen(t, 400, 0, []MdBlock{para(txt("x "), txt("  y"))})
	if v := values(s.texts()); len(v) != 2 || v[0] != "x " || v[1] != "y" {
		t.Errorf("跨片段的空白只留一个：%q", v)
	}
	s = proseOpen(t, 400, 0, []MdBlock{para(txt("甲 "), MdSpan{Break: true}, txt(" 乙"))})
	v := s.texts()
	if len(v) != 2 || v[0].Value != "甲" || v[1].Value != "乙" {
		t.Fatalf("硬换行前后的空白应去掉：%v", values(v))
	}
	if !approx(v[1].Bounds.Y-v[0].Bounds.Y, proseLine16) || !approx(v[1].Bounds.X, 0) {
		t.Errorf("硬换行后的一行应在下一行行首：%+v %+v", v[0].Bounds, v[1].Bounds)
	}
	s = proseOpen(t, 400, 0, []MdBlock{para(txt("甲"), MdSpan{Break: true}, txt("乙"), MdSpan{Break: true}, txt("丙"))})
	if v := values(s.texts()); len(v) != 3 {
		t.Errorf("两个硬换行应是三行：%v", v)
	}
	// 行内文字全是空白：不画任何文字，但仍占一行高度（后一块在它下面）。
	s = proseOpen(t, 400, 0, []MdBlock{para(txt("   ")), para(txt("后"))})
	if v := s.texts(); len(v) != 1 || v[0].Value != "后" {
		t.Fatalf("全空白的段落不画字：%v", values(v))
	}
	if y := s.texts()[0].Bounds.Y; y < proseLine16+12-1 {
		t.Errorf("全空白的段落也占一行：后一段 y = %v", y)
	}
}

// 块之间的距离：兄弟的下 / 上外边距折叠取较大者；最后一块的下外边距、首块与调用方 lead 的折叠由返回的盒子带出。
func TestProseCollapsesMarginsBetweenSiblings(t *testing.T) {
	// 段落（下 12）→ 一级标题（上 24，高 38.390625，下 12）→ 段落：间距 24 与 12。
	blocks := ParseMarkdown("甲\n\n# 题\n\n乙")
	s := proseOpen(t, 300, 0, blocks)
	got := s.texts()
	if len(got) != 3 {
		t.Fatalf("应有三段文字：%v", values(got))
	}
	if want := proseLine16 + 24 + 38.390625 + 12; !approx(got[2].Bounds.Y-got[0].Bounds.Y, want) {
		t.Errorf("两段正文相隔 %v，期望 %v（24 与 12 是折叠后的间距）", got[2].Bounds.Y-got[0].Bounds.Y, want)
	}
	// 段落 → 段落：间距是 12（不是 24，也不是 0）。
	s = proseOpen(t, 300, 0, ParseMarkdown("甲\n\n乙"))
	got = s.texts()
	if want := proseLine16 + 12; !approx(got[1].Bounds.Y-got[0].Bounds.Y, want) {
		t.Errorf("两段相隔 %v，期望 %v", got[1].Bounds.Y-got[0].Bounds.Y, want)
	}
	// 分隔线：上下各 16，与段落的 12 折叠成 16，线本身 1px。
	s = proseOpen(t, 300, 0, ParseMarkdown("甲\n\n---\n\n乙"))
	got = s.texts()
	if want := proseLine16 + 16 + 1 + 16; !approx(got[1].Bounds.Y-got[0].Bounds.Y, want) {
		t.Errorf("分隔线前后两段相隔 %v，期望 %v", got[1].Bounds.Y-got[0].Bounds.Y, want)
	}
	// lead：与首块的上外边距折叠取较大者（标题 24 比 lead 6 大；段落 0 比 lead 6 小）。
	if y := proseOpen(t, 300, 6, ParseMarkdown("# 题")).texts()[0].Bounds.Y; !approx(y-proseOpen(t, 300, 0, ParseMarkdown("# 题")).texts()[0].Bounds.Y, 0) {
		t.Errorf("lead 小于首块上外边距时不应再增高：%v", y)
	}
	y6 := proseOpen(t, 300, 6, ParseMarkdown("甲")).texts()[0].Bounds.Y
	y0 := proseOpen(t, 300, 0, ParseMarkdown("甲")).texts()[0].Bounds.Y
	if !approx(y6-y0, 6) {
		t.Errorf("lead 6 大于首块上外边距 0：应下移 6，实际 %v", y6-y0)
	}
	// 最后一块的下外边距落在盒内：盒子后面的内容（这里是另一个盒子）要隔开 12。
	h := openPage(t, still(), 400, 900, func(l *Look, c ui.C) {
		ui.Column(c).Width(300).Children(func() {
			l.Prose(c.Key("a"), ParseMarkdown("甲"), 0)
			l.Prose(c.Key("b"), ParseMarkdown("乙"), 0)
		})
	})
	tx := h.session.Snapshot().Texts
	if want := proseLine16 + 12; !approx(tx[1].Bounds.Y-tx[0].Bounds.Y, want) {
		t.Errorf("两个 Prose 盒相隔 %v，期望 %v（前一个的下外边距 12 由盒子带出，后一个的上外边距 0 折叠不掉它）", tx[1].Bounds.Y-tx[0].Bounds.Y, want)
	}
	// 空块列表：只有 lead 的上外边距、不 panic。
	proseOpen(t, 300, 6, nil)
}

// 行内样式：标题半粗、粗体 700、斜体、行内代码（0.9em 等宽）、链接 primary；引用整段 muted 斜体。
func TestProseInlineStylesReachTheLabels(t *testing.T) {
	blocks := ParseMarkdown("**粗** *斜* `code` [链](https://a.example)")
	s := proseOpen(t, 400, 0, blocks)
	look := s.h.look
	byValue := map[string]gfx.Text{}
	for _, x := range s.texts() {
		byValue[x.Value] = x
	}
	if x := byValue["粗"]; x.Style.Weight != WeightBold || x.Style.Italic {
		t.Errorf("粗体：%+v", x.Style)
	}
	if x := byValue["斜"]; !x.Style.Italic || x.Style.Weight != WeightNormal {
		t.Errorf("斜体：%+v", x.Style)
	}
	code := byValue["code"]
	if !code.Style.Mono || !approx(code.Style.Size, 14.4) {
		t.Errorf("行内代码应是 14.4px 等宽：%+v", code.Style)
	}
	link := byValue["链"]
	if gfxColor(look.T.Primary) != link.Style.Color || link.Style.Weight != WeightNormal {
		t.Errorf("链接应是 primary 正文：%+v", link.Style)
	}
	if x := byValue["粗"]; x.Style.Size != 16 || x.Style.Color != gfxColor(look.T.Foreground) {
		t.Errorf("粗体应是 16px foreground：%+v", x.Style)
	}

	// 标题半粗，字号随级别；引用 muted 斜体。
	h := proseOpen(t, 400, 0, ParseMarkdown("## 题"))
	if x := h.textAt(t, 0); x.Style.Weight != WeightSemibold || !approx(x.Style.Size, 20) {
		t.Errorf("二级标题应是 20px 半粗：%+v", x.Style)
	}
	q := proseOpen(t, 400, 0, ParseMarkdown("> 引"))
	if x := q.textAt(t, 0); !x.Style.Italic || x.Style.Color != gfxColor(look.T.MutedForeground) {
		t.Errorf("引用应是 muted 斜体：%+v", x.Style)
	}
	qb := proseOpen(t, 400, 0, ParseMarkdown("> *斜*"))
	if x := qb.textAt(t, 0); !x.Style.Italic {
		t.Errorf("引用里的斜体仍是斜体：%+v", x.Style)
	}
	qn := proseOpen(t, 400, 0, ParseMarkdown("> 引 **粗**"))
	if x := qn.textAt(t, 1); x.Style.Weight != WeightBold || !x.Style.Italic {
		t.Errorf("引用里的粗体是粗斜体：%+v", x.Style)
	}
}

// 标题进入无障碍树：角色 heading、名称是纯文字、级别是 Markdown 的级别；段落不是 heading。
func TestProseHeadingsAreAccessibleHeadings(t *testing.T) {
	s := proseOpen(t, 400, 0, ParseMarkdown("## 二级 **粗**\n\n段落"))
	n, ok := s.h.node(gfx.RoleHeading, "二级 粗")
	if !ok {
		t.Fatalf("应有名称为「二级 粗」的 heading 节点：%+v", s.h.session.Access())
	}
	if n.Level != 2 {
		t.Errorf("heading 级别 = %d，期望 2", n.Level)
	}
	for _, a := range s.h.session.Access() {
		if a.Role == gfx.RoleHeading && a.Label != "二级 粗" {
			t.Errorf("段落不应成为 heading：%+v", a)
		}
	}
}

// 删除线与下划线：删除线是 1px 的横线，宽度是文字宽度，位置在基线之上 0.3 字号（四舍五入到整像素）；
// 链接的下划线在基线之下 1px。
func TestProseStrikeAndUnderlineDecorations(t *testing.T) {
	s := proseOpen(t, 400, 0, ParseMarkdown("~~删~~ [链](https://a.example)"))
	look := s.h.look
	base := float32(ChromeBaselineRow(WeightNormal, 16, float64(proseLine16)))
	strikeY := float32(math.Floor(float64(base-0.3*16) + 0.5))
	var strike, underline []app.Primitive
	for _, p := range s.rects() {
		switch {
		case p.Color == gfxColor(look.T.Primary) && p.Bounds.Height == 1:
			underline = append(underline, p)
		case p.Color == gfxColor(look.T.Foreground) && p.Bounds.Height == 1:
			strike = append(strike, p)
		}
	}
	wDel := s.canvas.TextWidthWeight("删", 16, WeightNormal)
	if len(strike) != 1 || !approx(strike[0].Bounds.Y, strikeY) || !approx(strike[0].Bounds.Width, wDel) || !approx(strike[0].Bounds.X, 0) {
		t.Errorf("删除线 %+v，期望 x=0 y=%v 宽 %v 高 1", strike, strikeY, wDel)
	}
	wLink := s.canvas.TextWidthWeight("链", 16, WeightNormal)
	if len(underline) != 1 || !approx(underline[0].Bounds.Y, base+1) || !approx(underline[0].Bounds.Width, wLink) {
		t.Errorf("链接下划线 %+v，期望 y=%v 宽 %v 高 1", underline, base+1, wLink)
	}
	// 没有删除线与链接时一条线也没有。
	plain := proseOpen(t, 400, 0, ParseMarkdown("普通 **粗** *斜*"))
	for _, p := range plain.rects() {
		if p.Bounds.Height == 1 {
			t.Errorf("普通文字不应有横线：%+v", p)
		}
	}
}

// 行内代码：底色 muted、圆角 0.25em，左右各 0.4em 内边距、上下各 0.15em，文字与前后文字在同一条基线上。
func TestProseInlineCodeHasAChipAroundIt(t *testing.T) {
	s := proseOpen(t, 400, 0, ParseMarkdown("前 `code` 后"))
	look := s.h.look
	var chip *app.Primitive
	for _, p := range s.rects() {
		if p.Color == gfxColor(look.T.Muted) {
			p := p
			chip = &p
		}
	}
	if chip == nil {
		t.Fatalf("应有 muted 底色块：%+v", s.rects())
	}
	if !approx(chip.Radius, 0.25*14.4) {
		t.Errorf("底色块圆角 %v，期望 3.6", chip.Radius)
	}
	var code, before gfx.Text
	for _, x := range s.texts() {
		switch x.Value {
		case "code":
			code = x
		case "前 ":
			before = x
		}
	}
	pad := float32(0.4 * 14.4)
	textW := s.canvas.TextWidthStyle("code", gfx.TextStyle{Size: 14.4, Mono: true})
	if math.Abs(float64(chip.Bounds.X-(code.Bounds.X-pad))) > 1 || math.Abs(float64(chip.Bounds.Width-(textW+2*pad))) > 1 {
		t.Errorf("底色块 x=%v 宽 %v，期望 x≈%v 宽≈%v（文字两侧各 %v 内边距）", chip.Bounds.X, chip.Bounds.Width, code.Bounds.X-pad, textW+2*pad, pad)
	}
	// 文字紧接在前一段之后加左内边距：前一段 "前 " 的宽度 + 内边距。
	wBefore := s.canvas.TextWidthWeight("前 ", 16, WeightNormal)
	if !approx(code.Bounds.X, before.Bounds.X+wBefore+pad) {
		t.Errorf("代码文字 x = %v，期望 %v（前文宽度 + 左内边距）", code.Bounds.X, before.Bounds.X+wBefore+pad)
	}
	// 纵向：顶 = 基线 − A − 0.15em，高 = A + D + 0.3em（A、D 是 14.4px 等宽字体取整后的上、下伸部），再吸附到整像素。
	base := float32(ChromeBaselineRow(WeightNormal, 16, float64(proseLine16)))
	a := float32(roundHalfUp(chromeMonoFace.ascent * 14.4))
	d := float32(roundHalfUp(chromeMonoFace.descent * 14.4))
	padY := float32(0.15 * 14.4)
	want := SnapRect(gfx.Rect{X: 0, Y: base - a - padY, Width: 10, Height: a + d + 2*padY}, 1)
	if !approx(chip.Bounds.Y, want.Y) || !approx(chip.Bounds.Height, want.Height) {
		t.Errorf("底色块纵向 y=%v 高 %v，期望 y=%v 高 %v", chip.Bounds.Y, chip.Bounds.Height, want.Y, want.Height)
	}
}

// 行内代码折行：只有真正的两端才有内边距与圆角（折断处不带）。
func TestProseInlineCodeChipSplitsAcrossLines(t *testing.T) {
	blocks := ParseMarkdown("`abcdefghijkl`")
	measure := proseOpen(t, 400, 0, blocks)
	w := func(s string) float32 { return measure.canvas.TextWidthStyle(s, gfx.TextStyle{Size: 14.4, Mono: true}) }
	s := proseOpen(t, w("abcdefg"), 0, blocks)
	look := s.h.look
	var chips []app.Primitive
	for _, p := range s.rects() {
		if p.Color == gfxColor(look.T.Muted) {
			chips = append(chips, p)
		}
	}
	texts := s.texts()
	if len(texts) != 2 || len(chips) != 2 {
		t.Fatalf("应折成两行、两块底色：文字 %v 底色 %+v", values(texts), chips)
	}
	pad := float32(0.4 * 14.4)
	// 第一行：左端带内边距，右端到文字尾；第二行：左端齐文字起点，右端带内边距。
	if math.Abs(float64(chips[0].Bounds.X-(texts[0].Bounds.X-pad))) > 1 {
		t.Errorf("第一行底色左缘 %v，期望文字起点 %v 之外 %v", chips[0].Bounds.X, texts[0].Bounds.X, pad)
	}
	w1 := w(texts[0].Value)
	if math.Abs(float64(chips[0].Bounds.X+chips[0].Bounds.Width-(texts[0].Bounds.X+w1))) > 1 {
		t.Errorf("第一行底色右缘 %v，期望齐文字尾 %v（折断处不带内边距）", chips[0].Bounds.X+chips[0].Bounds.Width, texts[0].Bounds.X+w1)
	}
	if math.Abs(float64(chips[1].Bounds.X-texts[1].Bounds.X)) > 1 {
		t.Errorf("第二行底色左缘 %v，期望齐文字起点 %v", chips[1].Bounds.X, texts[1].Bounds.X)
	}
	w2 := w(texts[1].Value)
	if math.Abs(float64(chips[1].Bounds.X+chips[1].Bounds.Width-(texts[1].Bounds.X+w2+pad))) > 1 {
		t.Errorf("第二行底色右缘 %v，期望文字尾之外 %v", chips[1].Bounds.X+chips[1].Bounds.Width, texts[1].Bounds.X+w2+pad)
	}
	if !approx(texts[0].Bounds.X, pad) {
		t.Errorf("第一行文字 x = %v，期望左内边距 %v", texts[0].Bounds.X, pad)
	}
	// 内边距计入折行宽度：第一行（含左内边距）不超过容器。
	if texts[0].Bounds.X+w1 > w("abcdefg")+0.01 {
		t.Errorf("第一行排到 %v，超过容器 %v", texts[0].Bounds.X+w1, w("abcdefg"))
	}
}

// 无序记号：实心圆、空心圆（描 1px 的环）、方块，边长 5（16px），位置相对列表项左上角取整；记号颜色随继承色。
func TestProseBulletsAreShapes(t *testing.T) {
	look := NewLook(still())
	s := proseOpen(t, 300, 0, ParseMarkdown("- 甲\n  - 乙\n    - 丙"))
	rects := s.rects()
	var disc, square *app.Primitive
	for i, p := range rects {
		if p.Bounds.Width == 5 && p.Bounds.Height == 5 {
			switch p.Radius {
			case 2.5:
				disc = &rects[i]
			case 0:
				square = &rects[i]
			}
		}
	}
	if disc == nil || square == nil {
		t.Fatalf("应画出实心圆与方块：%+v", rects)
	}
	// 第一项（行盒顶 0）：x = 24（列表左内边距）− 17，y = 11；颜色 foreground。
	if disc.Bounds.X != 7 || disc.Bounds.Y != 11 || disc.Color != gfxColor(look.T.Foreground) {
		t.Errorf("实心圆 %+v，期望 x=7 y=11 foreground", *disc)
	}
	// 空心圆（第二层）：三角形拼成的环，外半径 2.5、内半径 1.5；圆心 x 在 24×2 − 17 + 2.5。
	var minX, maxX, minY, maxY = math.MaxFloat64, -math.MaxFloat64, math.MaxFloat64, -math.MaxFloat64
	for _, p := range s.snap.Primitives {
		if p.Kind != app.TrianglePrimitive {
			continue
		}
		for _, pt := range p.Points {
			minX, maxX = math.Min(minX, float64(pt.X)), math.Max(maxX, float64(pt.X))
			minY, maxY = math.Min(minY, float64(pt.Y)), math.Max(maxY, float64(pt.Y))
		}
	}
	if math.Abs((maxX-minX)-5) > 0.2 || math.Abs((maxY-minY)-5) > 0.2 {
		t.Errorf("空心圆外框 %.2f×%.2f，期望 5×5", maxX-minX, maxY-minY)
	}
	ccx, ccy := (minX+maxX)/2, (minY+maxY)/2
	minR, maxR := math.MaxFloat64, 0.0
	for _, p := range s.snap.Primitives {
		if p.Kind != app.TrianglePrimitive {
			continue
		}
		for _, pt := range p.Points {
			r := math.Hypot(float64(pt.X)-ccx, float64(pt.Y)-ccy)
			minR, maxR = math.Min(minR, r), math.Max(maxR, r)
		}
	}
	if math.Abs(maxR-2.5) > 0.15 || math.Abs(minR-1.5) > 0.15 {
		t.Errorf("空心圆环半径 内 %.2f 外 %.2f，期望 内 1.5 外 2.5（1px 描边）", minR, maxR)
	}
	if math.Abs(ccx-(24+24-17+2.5)) > 0.01 {
		t.Errorf("空心圆圆心 x = %v，期望 %v", ccx, 24+24-17+2.5)
	}
	// 方块（第三层）：x = 72 − 19 + …… 按 16px：24×3 − 17 = 55。
	if square.Bounds.X != 55 {
		t.Errorf("方块 x = %v，期望 55", square.Bounds.X)
	}
	// 引用里的列表：记号用 muted 色。
	q := proseOpen(t, 300, 0, ParseMarkdown("> - 甲"))
	var found bool
	for _, p := range q.rects() {
		if p.Bounds.Width == 5 && p.Radius == 2.5 {
			found = true
			if p.Color != gfxColor(look.T.MutedForeground) {
				t.Errorf("引用里的圆点颜色 %v，期望 muted", p.Color)
			}
		}
	}
	if !found {
		t.Errorf("引用里应有圆点：%+v", q.rects())
	}
}

// 有序记号：文字加一个尾随空格，右缘贴着内容左缘（内容在列表左内边距 24 处）；记号与第一行文字同一行，颜色随继承色。
func TestProseOrderedMarkersSitLeftOfTheContent(t *testing.T) {
	look := NewLook(still())
	s := proseOpen(t, 300, 0, ParseMarkdown("3. 甲\n4. 乙"))
	texts := s.texts()
	byValue := map[string]gfx.Text{}
	for _, x := range texts {
		byValue[x.Value] = x
	}
	m3, m4, a, b := byValue["3."], byValue["4."], byValue["甲"], byValue["乙"]
	if m3.Value == "" || m4.Value == "" {
		t.Fatalf("应有记号 3. 与 4.：%v", values(texts))
	}
	markerW := s.canvas.TextWidthWeight("3. ", 16, WeightNormal)
	if !approx(m3.Bounds.X+markerW, a.Bounds.X) || !approx(a.Bounds.X, 24) {
		t.Errorf("记号 x=%v（右缘 %v）、内容 x=%v，期望内容在 24、记号右缘（含尾随空格）贴着它", m3.Bounds.X, m3.Bounds.X+markerW, a.Bounds.X)
	}
	if !approx(m3.Bounds.Y, a.Bounds.Y) || !approx(m4.Bounds.Y, b.Bounds.Y) {
		t.Errorf("记号应与第一行文字同一行：%v/%v %v/%v", m3.Bounds.Y, a.Bounds.Y, m4.Bounds.Y, b.Bounds.Y)
	}
	if !approx(m4.Bounds.Y-m3.Bounds.Y, proseLine16+float32(math.Floor(0.35*16*64))/64) {
		t.Errorf("两项相隔 %v，期望行高 + 项下外边距 5.59375", m4.Bounds.Y-m3.Bounds.Y)
	}
	if m3.Style.Weight != WeightNormal || m3.Style.Size != 16 || m3.Style.Color != gfxColor(look.T.Foreground) {
		t.Errorf("记号应是 16px 常规字重 foreground：%+v", m3.Style)
	}
	if w := s.canvas.TextWidthWeight("3.", 16, WeightNormal); m3.Bounds.Width < w-0.01 {
		t.Errorf("记号盒宽 %v 放不下记号文字 %v", m3.Bounds.Width, w)
	}
	q := proseOpen(t, 300, 0, ParseMarkdown("> 1. 甲"))
	for _, x := range q.texts() {
		if x.Value == "1." && x.Style.Color != gfxColor(look.T.MutedForeground) {
			t.Errorf("引用里的记号颜色 %v，期望 muted", x.Style.Color)
		}
	}
}

// 引用：左边 3px primary 竖线，内容缩进 1em + 3px。分隔线：宽度占满、1px、border 色。
func TestProseQuoteBarAndRule(t *testing.T) {
	look := NewLook(still())
	q := proseOpen(t, 300, 0, ParseMarkdown("> 引文"))
	var bar *app.Primitive
	for _, p := range q.rects() {
		if p.Color == gfxColor(look.T.Primary) {
			p := p
			bar = &p
		}
	}
	if bar == nil || bar.Bounds.X != 0 || bar.Bounds.Width != 3 || bar.Bounds.Height < 25 {
		t.Fatalf("引用竖线应是 x=0 宽 3 的 primary 条：%+v", q.rects())
	}
	if x := q.textAt(t, 0).Bounds.X; !approx(x, 19) {
		t.Errorf("引用文字 x = %v，期望 16 + 3", x)
	}
	r := proseOpen(t, 300, 0, ParseMarkdown("---"))
	rects := r.rects()
	if len(rects) != 1 || rects[0].Bounds.Width != 300 || rects[0].Bounds.Height != 1 || rects[0].Color != gfxColor(look.T.Border) || rects[0].Bounds.Y != 16 {
		t.Errorf("分隔线应是 y=16 宽 300 高 1 的 border 线：%+v", rects)
	}
}

// 代码块：muted 底、圆角 0.5em、内边距 0.75em / 1em，文字 14.4px 等宽、颜色 foreground/0.92；含汉字的行用界面字体（14px）；去掉尾部换行。
func TestProseCodeBlock(t *testing.T) {
	look := NewLook(still())
	blocks := []MdBlock{{Kind: MdCode, Text: "abc\n汉字\n\n"}}
	s := proseOpen(t, 300, 0, blocks)
	var box *app.Primitive
	for _, p := range s.rects() {
		if p.Color == gfxColor(look.T.Muted) {
			p := p
			box = &p
		}
	}
	if box == nil {
		t.Fatalf("应有 muted 代码底：%+v", s.rects())
	}
	if box.Radius != 8 || box.Bounds.X != 0 || box.Bounds.Width != 300 || box.Bounds.Y != 12 {
		t.Errorf("代码底 %+v，期望 x=0 y=12 宽 300 圆角 8", *box)
	}
	// 两行（尾部空行去掉）：高 = 2 × 25.59375 + 2 × 12 = 75.1875 → 吸附成 75。
	if box.Bounds.Height != 75 {
		t.Errorf("代码底高 %v，期望 75（两行 + 上下内边距；尾部换行已去掉）", box.Bounds.Height)
	}
	texts := s.texts()
	if len(texts) != 2 {
		t.Fatalf("应画两行：%v", values(texts))
	}
	abc, han := texts[0], texts[1]
	if abc.Value != "abc" || !abc.Style.Mono || !approx(abc.Style.Size, 14.4) {
		t.Errorf("第一行应是 14.4px 等宽：%+v", abc)
	}
	if han.Value != "汉字" || han.Style.Mono || !approx(han.Style.Size, 14) {
		t.Errorf("含汉字的行应是 14px 界面字体：%+v", han)
	}
	if !approx(abc.Bounds.X, 16) || !approx(han.Bounds.X, 16) {
		t.Errorf("代码文字 x = %v / %v，期望 1em 内边距 16", abc.Bounds.X, han.Bounds.X)
	}
	if !approx(abc.Bounds.Y, 12+12) {
		t.Errorf("第一行 y = %v，期望 上外边距 12 + 内边距 12", abc.Bounds.Y)
	}
	dy := monoBaselineShift(14.4, 14, proseLine16, 1)
	if !approx(han.Bounds.Y, abc.Bounds.Y+proseLine16+dy) {
		t.Errorf("第二行 y = %v，期望一行高之下 %v 加基线平移 %v", han.Bounds.Y, abc.Bounds.Y+proseLine16, dy)
	}
	wantAlpha := float32(0.92)
	if abc.Style.Color.Alpha != wantAlpha || han.Style.Color.Alpha != wantAlpha {
		t.Errorf("代码文字透明度 %v / %v，期望 0.92", abc.Style.Color.Alpha, han.Style.Color.Alpha)
	}
	// 宽度不够时折行：一行放不下的代码被折成多行。
	n := proseOpen(t, 70, 0, []MdBlock{{Kind: MdCode, Text: "abcdefghijklmnopqrstuvwxyz"}})
	if len(n.texts()) < 2 {
		t.Errorf("窄容器里长代码应折行：%v", values(n.texts()))
	}
	// 空行不画，但占一行高度。
	e := proseOpen(t, 300, 0, []MdBlock{{Kind: MdCode, Text: "甲\n\n乙"}})
	if ts := e.texts(); len(ts) != 2 || !approx(ts[1].Bounds.Y-ts[0].Bounds.Y, 2*proseLine16) {
		t.Errorf("代码块里的空行应占一行高度：%+v", ts)
	}
}

// 一行里不同字号、字重的几截按同一条基线落笔：正文、粗体与行内代码的底边对齐到同一基线，而不是各自行盒的顶。
func TestProseMixedRunsShareOneBaseline(t *testing.T) {
	s := proseOpen(t, 400, 0, ParseMarkdown("正文 **粗体** `code`"))
	look := s.h.look
	_ = look
	byValue := map[string]gfx.Text{}
	for _, x := range s.texts() {
		byValue[x.Value] = x
	}
	norm, bold, code := byValue["正文 "], byValue["粗体"], byValue["code"]
	parentRow := float32(ChromeBaselineRow(WeightNormal, 16, float64(proseLine16)))
	// 正文：落笔位置 = 行盒顶 + (父基线行 − 自己的基线行) + 基线平移 = 基线平移。
	if want := BaselineShift(WeightNormal, 16, proseLine16, 1); !approx(norm.Bounds.Y, want) {
		t.Errorf("正文 y = %v，期望基线平移 %v", norm.Bounds.Y, want)
	}
	boldRow := float32(ChromeBaselineRow(WeightBold, 16, float64(proseLine16)))
	if want := (parentRow - boldRow) + BaselineShift(WeightBold, 16, proseLine16, 1); !approx(bold.Bounds.Y, want) {
		t.Errorf("粗体 y = %v，期望 %v（与父基线行的差 + 基线平移）", bold.Bounds.Y, want)
	}
	codeRow := float32(VivaBaselineRow(VivaLabelRows(proseLine16, 1), 14.4))
	if want := parentRow - codeRow; !approx(code.Bounds.Y, want) {
		t.Errorf("行内代码 y = %v，期望 %v（与前后文字共用基线）", code.Bounds.Y, want)
	}
	// Label 的盒宽留出 2 个字号的余量，高是行高。
	if !approx(norm.Bounds.Width, s.canvas.TextWidthWeight("正文 ", 16, WeightNormal)+32) || !approx(norm.Bounds.Height, proseLine16) {
		t.Errorf("正文 Label 盒 %+v", norm.Bounds)
	}
}

// 排版随容器宽度重排：同一份文字在窄容器里行数更多、盒子更高（量高与画共用同一宽度的折行）。
func TestProseHeightFollowsTheWidth(t *testing.T) {
	blocks := []MdBlock{para(txt("aa bb cc dd ee ff gg hh")), para(txt("尾"))}
	wide := proseOpen(t, 400, 0, blocks)
	measure := func(width float32) float32 {
		s := proseOpen(t, width, 0, blocks)
		ts := s.texts()
		return ts[len(ts)-1].Bounds.Y
	}
	if got := values(wide.texts()); len(got) != 2 {
		t.Fatalf("宽容器里应是两行（一段一行）：%v", got)
	}
	w := wide.canvas.TextWidthWeight("aa bb", 16, WeightNormal)
	narrow := measure(w + 1)
	if narrow <= wide.texts()[1].Bounds.Y+proseLine16*2 {
		t.Errorf("窄容器里后一段应被推得更低：窄 %v 宽 %v", narrow, wide.texts()[1].Bounds.Y)
	}
}

// 量宽：按一行排的最宽行就是内容的固有宽度（容器收缩到内容时用）；试算不改变真实宽度下的折行。
func TestProseMeasuresItsIntrinsicWidth(t *testing.T) {
	var layout *proseText
	var canvas *gfx.Canvas
	openPage(t, still(), 400, 300, func(l *Look, c ui.C) {
		canvas = c.Canvas()
		runs := []proseRun{{text: "aa bb cc", size: 16, weight: WeightNormal}, {brk: true}, {text: "dd", size: 16, weight: WeightNormal}}
		layout = newProseText(l, c.Canvas(), runs, 16, proseLine16, WeightNormal)
		ui.Column(c).Children(func() {})
	})
	want := canvas.TextWidthWeight("aa bb cc", 16, WeightNormal)
	if got := layout.MeasureWidth(nil); !approx(got, want) {
		t.Errorf("固有宽度 %v，期望最宽一行 %v（第二行较短，不能取最后一行）", got, want)
	}
	if lines := layout.linesAt(want / 2); len(lines) < 3 {
		t.Errorf("试算之后按窄宽度排应折成更多行：%d", len(lines))
	}
	if layout.filled && layout.width == 1e9 {
		// 试算的结果不能留给真正的宽度用（缓存按宽度比较，1e9 永远不会命中，但也不应保留）。
		t.Errorf("试算留下了缓存")
	}
}