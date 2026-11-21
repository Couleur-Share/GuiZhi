package kit

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// 本文件把摘要用的 Markdown 解析（markdown.go）逐条行为钉住（M2-6c 变异检查）：块的边界、列表的紧凑与宽松、围栏与缩进代码、
// 行内的强调 / 删除线 / 链接 / 自动链接 / 裸地址 / 实体 / 转义 / 硬换行。解析是纯函数，没有窗口。
// 每条用例的期望都按「渲染层 react-markdown 的行为 + markdown.go 注释里写明的取舍」手工推出，不是把当前输出抄过来。

// dumpSpans 把行内片段写成一行：文字用 Go 引号，样式跟在后面（b 粗 i 斜 s 删 c 码），链接是 @地址，硬换行是 ⏎。
func dumpSpans(spans []MdSpan) string {
	parts := make([]string, 0, len(spans))
	for _, s := range spans {
		if s.Break {
			parts = append(parts, "⏎")
			continue
		}
		p := strconv.Quote(s.Text)
		if s.Bold {
			p += "b"
		}
		if s.Italic {
			p += "i"
		}
		if s.Strike {
			p += "s"
		}
		if s.Code {
			p += "c"
		}
		if s.Href != "" {
			p += "@" + s.Href
		}
		parts = append(parts, p)
	}
	return strings.Join(parts, " ")
}

// dumpBlocks 把块写成一行：P 段落（紧凑列表里的写成小写 p）、H1–H6 标题、HR 分隔线、C[语言]"原文" 代码、
// Q{…} 引用、UL{[项],[项]} / OL起始号{…} 列表（宽松的在名字后加 ~）。
func dumpBlocks(blocks []MdBlock) string {
	parts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		switch b.Kind {
		case MdParagraph:
			name := "P"
			if b.Tight {
				name = "p"
			}
			parts = append(parts, name+"("+dumpSpans(b.Spans)+")")
		case MdHeading:
			parts = append(parts, "H"+strconv.Itoa(b.Level)+"("+dumpSpans(b.Spans)+")")
		case MdRule:
			parts = append(parts, "HR")
		case MdCode:
			parts = append(parts, "C["+b.Lang+"]"+strconv.Quote(b.Text))
		case MdQuote:
			parts = append(parts, "Q{"+dumpBlocks(b.Children)+"}")
		case MdList:
			name := "UL"
			if b.Ordered {
				name = "OL" + strconv.Itoa(b.Start)
			}
			if b.Loose {
				name += "~"
			}
			items := make([]string, len(b.Items))
			for i, item := range b.Items {
				items[i] = "[" + dumpBlocks(item) + "]"
			}
			parts = append(parts, name+"{"+strings.Join(items, ",")+"}")
		}
	}
	return strings.Join(parts, " ")
}

// parseWithin 在限定时间里解析：解析卡住（游标不前进的死循环）按失败处理。
func parseWithin(t *testing.T, src string) []MdBlock {
	t.Helper()
	var out []MdBlock
	done := make(chan struct{})
	go func() {
		defer close(done)
		out = ParseMarkdown(src)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("解析超时：%q", src)
	}
	return out
}

func TestParseMarkdownGoldenBlocks(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, src, want string }{
		// 换行与制表符
		{"单独的回车是换行", "a\rb", `P("a\nb")`},
		{"回车换行是一个换行", "a\r\nb", `P("a\nb")`},
		{"行首制表符展开成 4 格缩进（缩进代码）", "\t- 项", `C[]"- 项"`},
		{"制表符按列对齐到 4 的倍数", " \t代码", `C[]"代码"`},

		// Setext 标题与段落
		{"Setext 一级", "标题\n===", `H1("标题")`},
		{"Setext 二级", "标题\n---", `H2("标题")`},
		{"Setext 多行", "甲\n乙\n===", `H1("甲\n乙")`},
		{"第一行的 === 不是标题下划线", "===", `P("===")`},
		{"缩进 4 格的 === 不是下划线", "标题\n    ===", `P("标题\n===")`},
		{"缩进 3 格的 === 是下划线", "标题\n   ===", `H1("标题")`},
		{"段落首行去掉行首空格", "  a", `P("a")`},
		{"缩进 4 格不打断段落", "para\n    # not heading", `P("para\n# not heading")`},
		{"缩进 3 格的标题打断段落", "para\n   # h", `P("para") H1("h")`},
		{"空列表项不打断段落", "para\n*\nnext", `P("para\n*\nnext")`},
		{"从 1 起的有序列表打断段落", "para\n1. x", `P("para") OL1{[p("x")]}`},
		{"不从 1 起的有序列表不打断段落", "para\n2. x", `P("para\n2. x")`},

		// 缩进代码
		{"缩进代码里的空行去前缀", "    a\n      \n    b", `C[]"a\n  \nb"`},
		{"缩进代码去掉尾部空行", "    a\n\n\nb", `C[]"a" P("b")`},

		// 围栏代码
		{"两个反引号不是围栏", "``\nx", "P(\"``\\nx\")"},
		{"反引号围栏的信息串里不能有反引号", "```a`b\nx", "P(\"```a`b\\nx\")"},
		{"围栏语言按制表符切", "```go\tfoo\nx\n```", `C[go]"x"`},
		{"波浪线围栏", "~~~\nx\n~~~", `C[]"x"`},
		{"收尾围栏不能比开头短", "````\na\n```\nb\n````", "C[]\"a\\n```\\nb\""},
		{"收尾围栏要同一种字符", "```\na\n~~~\nb\n```", "C[]\"a\\n~~~\\nb\""},
		{"缩进 4 格的围栏线不收尾", "```\na\n    ```\nb\n```", "C[]\"a\\n    ```\\nb\""},
		{"缩进 3 格的围栏线收尾", "```\na\n   ```\nb", "C[]\"a\" P(\"b\")"},
		{"围栏内容按开头的缩进去前缀", "  ```\n  a\n    b\n  ```", `C[]"a\n  b"`},
		{"围栏没有收尾就到结尾", "```\na", `C[]"a"`},

		// ATX 标题
		{"七个 # 不是标题", "####### x", `P("####### x")`},
		{"六个 # 是六级", "###### x", `H6("x")`},
		{"# 后面可以是制表符", "#\tx", `H1("x")`},
		{"# 后面没有空白不是标题", "#x", `P("#x")`},
		{"闭合的 # 去掉", "# a ##", `H1("a")`},
		{"# 前没有空白的结尾 # 保留", "# a#", `H1("a#")`},
		{"全是 # 的标题是空标题", "# ###", `H1()`},
		{"只有一个 #", "#", `H1()`},
		{"闭合 # 前可以是制表符", "# a\t##", `H1("a")`},

		// 分隔线
		{"两个减号不是分隔线", "--", `P("--")`},
		{"两个下划线不是分隔线", "__", `P("__")`},
		{"混用字符不是分隔线", "-*-", `P("-*-")`},
		{"空格分隔的分隔线", "- - -", `HR`},
		{"制表符分隔的分隔线", "*\t*\t*", `HR`},
		{"三个星号", "***", `HR`},
		{"三个下划线", "___", `HR`},

		// 引用
		{"缩进 4 格的 > 不是引用的延续", "> a\n    > b", `Q{P("a\n> b")}`},
		{"缩进 3 格的 > 是引用的延续", "> a\n   > b", `Q{P("a\nb")}`},
		{"> 后的空格只去一个", ">     code", `Q{C[]"code"}`},
		{"引用的懒惰延续", "> a\nb", `Q{P("a\nb")}`},
		{"懒惰延续被标题打断", "> a\n# h", `Q{P("a")} H1("h")`},
		{"懒惰延续被列表打断", "> a\n- x", `Q{P("a")} UL{[p("x")]}`},
		{"引用里的空行之后不能懒惰延续", "> a\n>\nb", `Q{P("a")} P("b")`},

		// 列表标记
		{"缩进 3 格的列表标记", "   - x", `UL{[p("x")]}`},
		{"懒惰延续里缩进 4 格的标记仍是延续", "1000. a\n    - b", `OL1000{[p("a") UL{[p("b")]}]}`},
		{"九位数字的有序列表", "123456789. x", `OL123456789{[p("x")]}`},
		{"十位数字不是列表", "1234567890. x", `P("1234567890. x")`},
		{"右括号分隔的有序列表", "1) a\n2) b", `OL1{[p("a")],[p("b")]}`},
		{"加号列表", "+ a", `UL{[p("a")]}`},
		{"星号列表", "* a\n* b", `UL{[p("a")],[p("b")]}`},
		{"不从 1 起的有序列表", "3. a\n4. b", `OL3{[p("a")],[p("b")]}`},
		{"有序与无序不连成一个列表", "- a\n1. b", `UL{[p("a")]} OL1{[p("b")]}`},
		{"标记后四个空格仍是段落", "-    x", `UL{[p("x")]}`},
		{"标记后五个空格是缩进代码", "-     code", `UL{[C[]"code"]}`},
		{"空项后跟缩进内容", "-\n  a", `UL{[p("a")]}`},

		// 列表项的边界、懒惰延续与宽松
		{"空行之后的行不是懒惰延续", "- a\n\nb", `UL{[p("a")]} P("b")`},
		{"懒惰延续被标题打断", "- a\n# h", `UL{[p("a")]} H1("h")`},
		{"懒惰延续被引用打断", "- a\n> q", `UL{[p("a")]} Q{P("q")}`},
		{"懒惰延续", "- a\nb", `UL{[p("a\nb")]}`},
		{"项间有空行是宽松列表", "- a\n\n- b", `UL~{[P("a")],[P("b")]}`},
		{"项内有空行分隔的段落是宽松列表", "- a\n\n  b\n- c", `UL~{[P("a") P("b")],[P("c")]}`},
		{"空行后换了列表类型不算宽松", "- a\n\n1. b", `UL{[p("a")]} OL1{[p("b")]}`},
		{"空行后的分隔线结束列表", "- a\n\n- - -", `UL{[p("a")]} HR`},
		{"分隔线结束列表", "- a\n- - -", `UL{[p("a")]} HR`},
		{"紧凑列表的段落是紧凑段落", "- a\n- b", `UL{[p("a")],[p("b")]}`},
		{"嵌套列表", "- a\n  - b\n- c", `UL{[p("a") UL{[p("b")]}],[p("c")]}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := dumpBlocks(parseWithin(t, c.src)); got != c.want {
				t.Errorf("%q\n 得到 %s\n 期望 %s", c.src, got, c.want)
			}
		})
	}
}

// blankBefore 是块与前一个兄弟块之间隔着空行：开头的空行不算，加入一块之后标记清掉。
func TestParseMarkdownBlankBefore(t *testing.T) {
	t.Parallel()
	got := ParseMarkdown("\n\na\n\nb\n# c")
	want := []bool{false, true, false}
	if len(got) != len(want) {
		t.Fatalf("应有 3 块：%s", dumpBlocks(got))
	}
	for i, b := range got {
		if b.blankBefore != want[i] {
			t.Errorf("第 %d 块 blankBefore=%v，期望 %v", i, b.blankBefore, want[i])
		}
	}
}

// 空列表项后面跟空行：游标必须前进（不前进会让 parseBlocks 死循环，故直接断言 parseList）。
func TestParseListAdvancesPastAnEmptyItem(t *testing.T) {
	t.Parallel()
	lines := strings.Split("-\n\nb", "\n")
	if _, next := parseList(lines, 0, 0); next < 1 || next > 2 {
		t.Fatalf("列表应停在空行处（下标 1，最多 2），实际 %d", next)
	}
	if got := dumpBlocks(parseWithin(t, "-\n\nb")); got != `UL{[]} P("b")` {
		t.Fatalf("得到 %s", got)
	}
}

// 块嵌套的深度上限：十二层引用之后的内容当普通段落（原文保留一个 >）。
func TestParseMarkdownNestingLimit(t *testing.T) {
	t.Parallel()
	blocks := parseWithin(t, strings.Repeat("> ", 13)+"x")
	depth := 0
	for len(blocks) == 1 && blocks[0].Kind == MdQuote {
		depth++
		blocks = blocks[0].Children
	}
	if depth != mdMaxDepth || dumpBlocks(blocks) != `P("> x")` {
		t.Fatalf("应嵌套 %d 层引用、最里面是原文段落：层数 %d，内容 %s", mdMaxDepth, depth, dumpBlocks(blocks))
	}
}

// listMarkerAt：标记的种类、缩进、内容起点与「空项」。
func TestListMarkerAt(t *testing.T) {
	t.Parallel()
	cases := []struct {
		line                  string
		nilMarker             bool
		indent, number, width int
		ordered, empty        bool
	}{
		{line: "-", indent: 0, width: 2, empty: true},
		{line: "-   ", indent: 0, width: 2, empty: true},
		{line: "  1. x", indent: 2, ordered: true, number: 1, width: 5},
		{line: "1) a", ordered: true, number: 1, width: 3},
		{line: "-    x", width: 5},
		{line: "-     x", width: 2},
		{line: "   - x", indent: 3, width: 5},
		{line: "123456789. x", ordered: true, number: 123456789, width: 11},
		{line: "-x", nilMarker: true},
		{line: "1.x", nilMarker: true},
		{line: "1234567890. x", nilMarker: true},
		{line: "    - x", nilMarker: true},
		{line: "", nilMarker: true},
		{line: "a", nilMarker: true},
	}
	for _, c := range cases {
		m := listMarkerAt(c.line)
		if c.nilMarker {
			if m != nil {
				t.Errorf("%q 不是列表项：%+v", c.line, *m)
			}
			continue
		}
		if m == nil {
			t.Errorf("%q 应是列表项", c.line)
			continue
		}
		if m.indent != c.indent || m.ordered != c.ordered || m.number != c.number || m.width != c.width || m.empty != c.empty {
			t.Errorf("%q：得到 %+v，期望缩进 %d 有序 %v 号 %d 起点 %d 空项 %v", c.line, *m, c.indent, c.ordered, c.number, c.width, c.empty)
		}
	}
}

// MdPlain：硬换行写成换行。
func TestMdPlainWritesHardBreaksAsNewlines(t *testing.T) {
	t.Parallel()
	if got := MdPlain(ParseMarkdown("甲  \n乙")[0].Spans); got != "甲\n乙" {
		t.Fatalf("硬换行应写成换行：%q", got)
	}
}

// nestLinks 把 inner 套进 n 层链接。
func nestLinks(n int, inner string) string {
	for i := 0; i < n; i++ {
		inner = "[" + inner + "](https://a.com)"
	}
	return inner
}

func TestParseInlineGolden(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, src, want string }{
		// 转义与硬换行
		{"末尾的反斜杠是字面", `a\`, `"a\\"`},
		{"反斜杠加换行是硬换行", "a\\\nb", `"a" ⏎ "b"`},
		{"反斜杠加字母保留反斜杠", `\a`, `"\\a"`},
		{"反斜杠加数字保留反斜杠", `\0`, `"\\0"`},
		{"反斜杠加汉字保留反斜杠", `\中`, `"\\中"`},
		{"两个空格加换行是硬换行", "a  \nb", `"a" ⏎ "b"`},
		{"一个空格加换行是软换行（去掉行尾空格）", "a \nb", `"a\nb"`},
		{"软换行吃掉下一行的行首空格", "a\n  b", `"a\nb"`},
		{"硬换行吃掉下一行的行首空格", "a   \n   b", `"a" ⏎ "b"`},
		{"连续两个硬换行都保留", "a\\\n\\\nb", `"a" ⏎ ⏎ "b"`},

		// 行内代码
		{"行内代码里的换行折成空格", "`a\nb`", `"a b"c`},
		{"全是空格的行内代码不去空格", "`  `", `"  "c`},
		{"首尾各一个空格去掉", "` a `", `"a"c`},
		{"只有开头的空格不去", "` a`", `" a"c`},
		{"只有结尾的空格不去", "`a `", `"a "c`},
		{"不闭合的反引号串原样输出", "``a", "\"``a\""},
		{"闭合的反引号串长度要恰好相等", "``a`b``", "\"a`b\"c"},
		{"代码里含更长的反引号串", "`a``b`", "\"a``b\"c"},
		{"去掉首尾空格之后留下反引号", "` `` `", "\"``\"c"},

		// 强调
		{"星号斜体", "*a*", `"a"i`},
		{"下划线斜体", "_a_", `"a"i`},
		{"星号粗体", "**a**", `"a"b`},
		{"下划线粗体", "__a__", `"a"b`},
		{"星号粗斜体", "***a***", `"a"bi`},
		{"下划线粗斜体", "___a___", `"a"bi`},
		{"不成对的三星号逐串输出", "***a**", `"***a**"`},
		{"单词里的下划线不是强调（字母）", "x_y_", `"x_y_"`},
		{"单词里的下划线不是强调（数字）", "1_a_", `"1_a_"`},
		{"snake_case 不被误伤", "snake_case_name", `"snake_case_name"`},
		{"收尾下划线后是字母不是强调", "_a_b", `"_a_b"`},
		{"收尾下划线后是数字不是强调", "_a_1", `"_a_1"`},
		{"收尾下划线后是空格是强调", "_a_ b", `"a"i " b"`},
		{"汉字旁的下划线不是强调", "中_a_", `"中_a_"`},
		{"开头星号后是空白不是强调", "a * b*", `"a * b*"`},
		{"结尾星号前是空白不算收尾", "*a * b*", `"a * b"i`},
		{"收尾的星号串长度要恰好相等", "*a**b*", `"a**b"i`},
		{"四个星号不是强调", "****a****", `"****a****"`},
		{"转义的星号不收尾", `*a\*b*`, `"a*b"i`},
		{"行内代码里的星号不收尾", "*a `*` b*", "\"a \"i \"*\"ic \" b\"i"},
		{"不闭合的反引号串不影响找收尾", "*x ``` y* z `` w*", "\"x ``` y\"i \" z `` w*\""},
		{"强调里叠删除线", "**a ~~b~~**", `"a "b "b"bs`},

		// 删除线
		{"双波浪线删除线", "~~a~~", `"a"s`},
		{"单波浪线不是删除线", "~a~", `"~a~"`},
		{"三波浪线不是删除线", "~~~a~~~", `"~~~a~~~"`},
		{"开头后是空白不是删除线", "~~ a~~", `"~~ a~~"`},
		{"删除线之后接普通文字", "~~a~~b", `"a"s "b"`},

		// 链接与图片
		{"链接", "[a](https://x.com)", `"a"@https://x.com`},
		{"链接之后接普通文字", "[a](https://x.com)b", `"a"@https://x.com "b"`},
		{"链接里的链接沿用外层地址", "[a [b](https://b.com)](https://a.com)", `"a b"@https://a.com`},
		{"双层方括号", "[[x](https://a.com)](https://b.com)", `"x"@https://b.com`},
		{"不安全的目标只留文字", "[a](javascript:x)", `"a"`},
		{"图片只留替代文字", "![alt](https://x.com/a.png)", `"alt"`},
		{"感叹号后不是方括号就是普通字符", "wow! [x](https://x.com)", `"wow! " "x"@https://x.com`},
		{"结尾的感叹号", "wow!", `"wow!"`},
		{"没有闭合的方括号", "[abc", `"[abc"`},
		{"没有闭合的圆括号", "[a](b", `"[a](b"`},
		{"右方括号后没有内容", "[a]", `"[a]"`},
		{"右方括号后不是左括号", "[a]x)", `"[a]x)"`},
		{"右方括号与左括号之间有空格", "[a] (b)", `"[a] (b)"`},
		{"目的地里配平的括号", "[a](https://x.com/(b))", `"a"@https://x.com/(b)`},
		{"目的地里转义的右括号", `[a](https://x.com/\))`, `"a"@https://x.com/\)`},
		{"尖括号目的地", "[a](<https://x.com/a b>)", `"a"@https://x.com/a b`},
		{"尖括号目的地里的前导空格保留", "[a](< https://x.com>)", `"a"@ https://x.com`},
		{"标题（空格）去掉", `[a](https://x.com "title")`, `"a"@https://x.com`},
		{"标题（制表符）去掉", "[a](https://x.com\t\"t\")", `"a"@https://x.com`},
		{"标题（换行）去掉", "[a](https://x.com\n\"t\")", `"a"@https://x.com`},
		{"协议名不分大小写", "[a](HTTPS://X.COM)", `"a"@HTTPS://X.COM`},
		{"mailto 链接", "[a](mailto:a@b.c)", `"a"@mailto:a@b.c`},
		{"http 链接", "[a](http://x.com)", `"a"@http://x.com`},
		{"链接里不再识别尖括号自动链接", "[<https://a.com>](https://b.com)", `"<https://a.com>"@https://b.com`},
		{"链接里不再识别裸地址", "[https://a.com](https://b.com)", `"https://a.com"@https://b.com`},

		// 嵌套深度
		{"九层链接：第九层当字面", nestLinks(9, "x"), `"[x](https://a.com)"@https://a.com`},
		{"第九层的强调当字面", nestLinks(8, "*x*"), `"*x*"@https://a.com`},
		{"强调里的下一层按加深一层算", nestLinks(7, "*[y](https://b.com)*"), `"[y](https://b.com)"i@https://a.com`},
		{"删除线里的下一层按加深一层算", nestLinks(7, "~~[y](https://b.com)~~"), `"[y](https://b.com)"s@https://a.com`},

		// 尖括号自动链接
		{"自动链接", "<https://a.com>", `"https://a.com"@https://a.com`},
		{"mailto 自动链接", "<mailto:a@b.c>", `"mailto:a@b.c"@mailto:a@b.c`},
		{"空尖括号", "<>", `"<>"`},
		{"不安全的自动链接", "<javascript:alert(1)>", `"<javascript:alert(1)>"`},
		{"含空格不是自动链接（里面的裸地址照常是链接）", "<https://a.com b>", `"<" "https://a.com"@https://a.com " b>"`},
		{"含换行不是自动链接", "<https://a.com\nb>", `"<" "https://a.com"@https://a.com "\nb>"`},

		// 裸地址
		{"裸地址", "https://a.com", `"https://a.com"@https://a.com`},
		{"裸地址去掉句号", "see https://a.com.", `"see " "https://a.com"@https://a.com "."`},
		{"成对的括号留在地址里", "https://a.com/(x)", `"https://a.com/(x)"@https://a.com/(x)`},
		{"不成对的右括号不在地址里", "https://a.com/x)", `"https://a.com/x"@https://a.com/x ")"`},
		{"括号包着的地址", "(see https://a.com)", `"(see " "https://a.com"@https://a.com ")"`},
		{"词中的地址不是链接（字母）", "ahttps://a.com", `"ahttps://a.com"`},
		{"词中的地址不是链接（大写字母）", "Ahttps://a.com", `"Ahttps://a.com"`},
		{"词中的地址不是链接（数字）", "1https://a.com", `"1https://a.com"`},
		{"词中的地址不是链接（汉字）", "中https://a.com", `"中https://a.com"`},
		{"只有协议头不是链接", "see https:// ok", `"see https:// ok"`},
		{"h 开头的普通长词不是链接", "hellothere world", `"hellothere world"`},
		{"地址遇到小于号结束", "https://a.com<b>", `"https://a.com"@https://a.com "<b>"`},
		{"地址遇到制表符结束", "https://a.com\tb", `"https://a.com"@https://a.com "\tb"`},
		{"地址遇到换行结束", "https://a.com\nb", `"https://a.com"@https://a.com "\nb"`},
		{"http 地址", "http://a.com", `"http://a.com"@http://a.com`},

		// 实体
		{"&amp;", "&amp;", `"&"`},
		{"&lt;", "&lt;", `"<"`},
		{"&gt;", "&gt;", `">"`},
		{"&quot;", "&quot;", `"\""`},
		{"&#39;", "&#39;", `"'"`},
		{"&apos;", "&apos;", `"'"`},
		{"&nbsp;", "&nbsp;", strconv.Quote("\u00a0")},
		{"十进制数字实体", "&#65;", `"A"`},
		{"十六进制数字实体", "&#x41;", `"A"`},
		{"大写 X 的十六进制数字实体", "&#X41;", `"A"`},
		{"零码点不解码", "&#0;", `"&#0;"`},
		{"代理区码点不解码", "&#xD800;", `"&#xD800;"`},
		{"超出范围的码点不解码", "&#x110000;", `"&#x110000;"`},
		{"实体长度恰好十个字符以内解码", "&#00000065;", `"A"`},
		{"实体太长不解码", "&#000000065;", `"&#000000065;"`},
		{"未知实体原样输出", "&unknown;", `"&unknown;"`},
		{"孤立的 & 原样输出", "a & b", `"a & b"`},

		// 合并
		{"行内代码不与相邻的普通文字合并", "`a`b", `"a"c "b"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := dumpSpans(parseInline(c.src)); got != c.want {
				t.Errorf("%q\n 得到 %s\n 期望 %s", c.src, got, c.want)
			}
		})
	}
}

// 转义：ASCII 标点转成字面字符；字母、数字、汉字前的反斜杠保留。
func TestParseInlineBackslashEscapes(t *testing.T) {
	t.Parallel()
	for _, c := range "!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~" {
		if got, want := dumpSpans(parseInline(`\`+string(c))), strconv.Quote(string(c)); got != want {
			t.Errorf(`\%c：得到 %s，期望 %s`, c, got, want)
		}
	}
	for _, c := range "09azAZ" {
		if got, want := dumpSpans(parseInline(`\`+string(c))), strconv.Quote(`\`+string(c)); got != want {
			t.Errorf(`\%c：得到 %s，期望 %s`, c, got, want)
		}
	}
}

// 裸地址末尾的标点不算地址。
func TestParseInlineBareURLTrimsTrailingPunctuation(t *testing.T) {
	t.Parallel()
	for _, c := range ".,:;!?'\"*_~" {
		got := dumpSpans(parseInline("https://a.com" + string(c)))
		want := `"https://a.com"@https://a.com ` + strconv.Quote(string(c))
		if got != want {
			t.Errorf("结尾的 %q：得到 %s，期望 %s", c, got, want)
		}
	}
}
