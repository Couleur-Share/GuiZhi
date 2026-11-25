package kit

import (
	"image"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// 命中词的匹配照 highlightText：命中词去首尾空白，大小写不敏感，从左到右、互不重叠。
func TestMarkSpans(t *testing.T) {
	cases := []struct {
		name, text, query string
		want              []MarkSpan
	}{
		{"汉字多处", "好的设计是尽可能少的设计。", "设计", []MarkSpan{{6, 12}, {30, 36}}},
		{"大小写不敏感", "Designing Data: design DESIGN", "design", []MarkSpan{{0, 6}, {16, 22}, {23, 29}}},
		{"命中词的首尾空白去掉", "a design b", "  design  ", []MarkSpan{{2, 8}}},
		{"互不重叠", "aaaa", "aa", []MarkSpan{{0, 2}, {2, 4}}},
		{"重叠的只取靠左的", "aaa", "aa", []MarkSpan{{0, 2}}},
		{"命中词含空格，原文逐字符比较", "a b  b", "a b", []MarkSpan{{0, 3}}},
		{"没有命中", "没有这个词", "设计", nil},
		{"空命中词", "设计", "", nil},
		{"只有空白的命中词", "设计", "   ", nil},
		{"空文字", "", "设计", nil},
	}
	for _, tc := range cases {
		got := MarkSpans(tc.text, tc.query)
		if len(got) != len(tc.want) {
			t.Errorf("%s：MarkSpans(%q,%q) = %v，期望 %v", tc.name, tc.text, tc.query, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%s：第 %d 处 = %v，期望 %v", tc.name, i, got[i], tc.want[i])
			}
		}
		if HasMarks(tc.text, tc.query) != (len(tc.want) > 0) {
			t.Errorf("%s：HasMarks 与 MarkSpans 不一致", tc.name)
		}
	}
}

// 切段与空白折叠：整段文字按 white-space: normal 折叠（跨段也折叠），空白开始的那一段保有折叠后的空格；首尾空白去掉。
func TestNewMarkModel(t *testing.T) {
	type run struct {
		text   string
		marked bool
	}
	cases := []struct {
		name, text, query string
		wantFull          string
		wantRuns          []run
	}{
		{"普通", "好的设计是", "设计", "好的设计是", []run{{"好的", false}, {"设计", true}, {"是", false}}},
		{"折叠的空格属于空白开始的那一段", "foo   bar \n baz", "bar", "foo bar baz", []run{{"foo ", false}, {"bar", true}, {" baz", false}}},
		{"命中段内部的空白折叠（命中范围由外部给定）", "a  b   c", "", "a b c", []run{{"a b", true}, {" c", false}}},
		{"首尾空白去掉", "  设计  ", "设计", "设计", []run{{"设计", true}}},
		{"命中词在开头", "design system", "design", "design system", []run{{"design", true}, {" system", false}}},
		// 只有空格、制表符、换行、回车折叠；全角空格、不换行空格原样保留（Chromium 实测，isCollapsibleSpace）。
		{"全角空格不折叠", "第一章\u3000设计\u3000\u3000总则", "设计", "第一章\u3000设计\u3000\u3000总则", []run{{"第一章\u3000", false}, {"设计", true}, {"\u3000\u3000总则", false}}},
		{"不换行空格不折叠，回车折叠", "a\u00a0\u00a0design\r\n\r\nb", "design", "a\u00a0\u00a0design b", []run{{"a\u00a0\u00a0", false}, {"design", true}, {" b", false}}},
	}
	for _, tc := range cases {
		spans := MarkSpans(tc.text, tc.query)
		// 命中词里的连续空白在原文里是原样的，所以空白折叠之后「a  b」要靠比较折叠后的文字命中；这里只测切段。
		if tc.query == "" {
			spans = []MarkSpan{{0, 4}}
		}
		m := newMarkModel(tc.text, spans)
		if m.full != tc.wantFull {
			t.Errorf("%s：折叠后 = %q，期望 %q", tc.name, m.full, tc.wantFull)
			continue
		}
		if len(m.starts) != len(tc.wantRuns) {
			t.Errorf("%s：段数 %d，期望 %d：%v", tc.name, len(m.starts), len(tc.wantRuns), m)
			continue
		}
		for i, want := range tc.wantRuns {
			if got := m.full[m.starts[i]:m.ends[i]]; got != want.text || m.marked[i] != want.marked {
				t.Errorf("%s：第 %d 段 = %q（命中 %v），期望 %q（命中 %v）", tc.name, i, got, m.marked[i], want.text, want.marked)
			}
		}
	}
}

// wantFrag 是期望的一个文字片段：位置与宽度的单位是逻辑像素，相对盒的左缘。
type wantFrag struct {
	text   string
	marked bool
	x, w   float64
	pl, pr float64
}

// layoutMarks 用与界面相同的排版规则排 text：内边距取 l.Space(0.5)（根字号 16 px 时是 2 px）。
func layoutMarks(t *testing.T, text, query string, size, weight, width float32, maxLines int, truncate bool) *markResult {
	t.Helper()
	look, canvas := canvasOf(t)
	spans := MarkSpans(text, query)
	if len(spans) == 0 {
		t.Fatalf("%q 里没有 %q 的命中", text, query)
	}
	if pad := look.Space(0.5); pad != 2 {
		t.Fatalf("前提不成立：内边距应是 2 px，得到 %v", pad)
	}
	b := &markBuilder{look: look, canvas: canvas, m: newMarkModel(text, spans), size: size, weight: weight, pad: look.Space(0.5)}
	return b.layout(width, maxLines, truncate)
}

// checkLines 逐行逐片段对照：文字、命中与否、内边距精确一致；位置与宽度允许 tol 的差（常规字重的字偶距、LayoutUnit 的取整）。
// 期望宽度为负表示不比较宽度（省略号的字宽跟字体走，渲染层里没有单独的矩形可对）。
func checkLines(t *testing.T, name string, got *markResult, want [][]wantFrag, tol float64) {
	t.Helper()
	if len(got.lines) != len(want) {
		t.Errorf("%s：行数 %d，期望 %d：%+v", name, len(got.lines), len(want), got.lines)
		return
	}
	for i, row := range want {
		if len(got.lines[i]) != len(row) {
			t.Errorf("%s：第 %d 行片段数 %d，期望 %d：%+v", name, i+1, len(got.lines[i]), len(row), got.lines[i])
			continue
		}
		for j, w := range row {
			g := got.lines[i][j]
			if g.text != w.text || g.marked != w.marked || float64(g.pl) != w.pl || float64(g.pr) != w.pr ||
				math.Abs(float64(g.x)-w.x) > tol || (w.w >= 0 && math.Abs(float64(g.w)-w.w) > tol) {
				t.Errorf("%s：第 %d 行第 %d 片 = {%q 命中 %v x=%.3f w=%.3f 内边距 %v/%v}，期望 {%q 命中 %v x=%.3f w=%.3f 内边距 %v/%v}",
					name, i+1, j+1, g.text, g.marked, g.x, g.w, g.pl, g.pr, w.text, w.marked, w.x, w.w, w.pl, w.pr)
			}
		}
	}
}

// skipWin11FontMetricsInCI 给硬编码了 Windows 11 中文版 Chromium 实测拉丁字宽的用例做门控：
// CI（Windows Server 英文版）的字形度量与参照不同，设了 CI 且没设 GUIZHI_RUN_WIN11_PARITY=1 时跳过；本地（无 CI）照常全跑。
func skipWin11FontMetricsInCI(t *testing.T) {
	t.Helper()
	if os.Getenv("CI") != "" && os.Getenv("GUIZHI_RUN_WIN11_PARITY") != "1" {
		t.Skip("CI 的 Windows Server 英文环境字形度量与 Win11 中文版参照不同；本地 Win11 或设置 GUIZHI_RUN_WIN11_PARITY=1 时执行")
	}
}

// 2026-11-20 在 React 参照里量到的几何（scripts/parity/tools/probe-highlight.mjs，设备缩放 1，根字号 16 px，light-royal-blue）：
// 文字矩形的左缘减宿主左缘 = x，mark 的 getClientRects 比文字宽两侧各 2 px 的内边距。
func TestMarkLayoutMatchesChromium(t *testing.T) {
	t.Run("卡片标题 14px/500：一行里夹一个命中", func(t *testing.T) {
		// 宿主 217 宽：产品 28 + 底色 32（内含设计 28）+ 原则：极简与克制 112。
		got := layoutMarks(t, "产品设计原则：极简与克制", "设计", 14, WeightMedium, 217, 0, false)
		checkLines(t, "title", got, [][]wantFrag{{
			{"产品", false, 0, 28, 0, 0},
			{"设计", true, 30, 28, 2, 2},
			{"原则：极简与克制", false, 60, 112, 0, 0},
		}}, 0.01)
		if math.Abs(float64(got.natural)-172) > 0.01 {
			t.Errorf("自然宽度 %v，期望 172（含两侧内边距）", got.natural)
		}
	})
	t.Run("卡片标题：命中词在开头（拉丁，不做字偶距）", func(t *testing.T) {
		skipWin11FontMetricsInCI(t)
		// mark 的文字 44.578 宽，后面接的文字从 48.578 起；第一行放到「Applications」，第二行是「读书笔记」。
		got := layoutMarks(t, "Designing Data-Intensive Applications 读书笔记", "Design", 14, WeightMedium, 257, 0, false)
		if len(got.lines) != 2 {
			t.Fatalf("应折成两行：%+v", got.lines)
		}
		first := got.lines[0]
		if len(first) != 2 || !first[0].marked || first[0].text != "Design" || math.Abs(float64(first[0].x)-2) > 1e-3 || math.Abs(float64(first[0].w)-44.578) > 0.03 {
			t.Fatalf("第一行第一片应是命中的 Design（x=2 w=44.578）：%+v", first)
		}
		if math.Abs(float64(first[1].x)-48.578) > 0.03 || first[1].text != "ing Data-Intensive Applications" {
			t.Errorf("第一行第二片 = %+v，期望 ing Data-Intensive Applications 起于 48.578（行尾空格不进片段）", first[1])
		}
		checkLines(t, "title", &markResult{lines: got.lines[1:]}, [][]wantFrag{{{"读书笔记", false, 0, 56, 0, 0}}}, 0.03)
	})
	t.Run("卡片标题：命中词落在折行后的第三行", func(t *testing.T) {
		// 前两行各 18 个汉字（252）；第三行：两个汉字 28 + 底色 32 + 取舍与实现笔记 98。
		got := layoutMarks(t, "一条标题很长很长、需要折成两行才能放下的知识条目：关于本地优先的个人知识库的设计取舍与实现笔记", "设计", 14, WeightMedium, 257, 0, false)
		if len(got.lines) != 3 {
			t.Fatalf("应折成三行：%+v", got.lines)
		}
		for i := 0; i < 2; i++ {
			if len(got.lines[i]) != 1 || got.lines[i][0].marked || math.Abs(float64(got.lines[i][0].w)-252) > 0.01 {
				t.Errorf("第 %d 行应是一整段 252 宽的普通文字：%+v", i+1, got.lines[i])
			}
		}
		checkLines(t, "title", &markResult{lines: got.lines[2:]}, [][]wantFrag{{
			{"库的", false, 0, 28, 0, 0}, {"设计", true, 30, 28, 2, 2}, {"取舍与实现笔记", false, 60, 98, 0, 0},
		}}, 0.01)
	})
	t.Run("卡片摘要 12px/400：两处命中，折成两行", func(t *testing.T) {
		got := layoutMarks(t, "好的设计是尽可能少的设计。每一个元素都应当有存在的理由。", "设计", 12, WeightNormal, 217, 0, false)
		checkLines(t, "snippet", got, [][]wantFrag{
			{
				{"好的", false, 0, 24, 0, 0}, {"设计", true, 26, 24, 2, 2}, {"是尽可能少的", false, 52, 72, 0, 0},
				{"设计", true, 126, 24, 2, 2}, {"。每一个元", false, 152, 60, 0, 0},
			},
			{{"素都应当有存在的理由。", false, 0, 132, 0, 0}},
		}, 0.01)
	})
	t.Run("卡片摘要：拉丁词的命中，命中词前的空格留在前一段里", func(t *testing.T) {
		skipWin11FontMetricsInCI(t)
		got := layoutMarks(t, "A talk about CRDTs, sync engines and offline-first design.", "design", 12, WeightNormal, 257, 0, false)
		if len(got.lines) != 2 || len(got.lines[1]) != 3 {
			t.Fatalf("应折成两行，第二行三片：%+v", got.lines)
		}
		// 第二行：offline-first␠ 67.109 | design（左内边距 2，文字 38.313） | .（2.891）。
		second := got.lines[1]
		if second[0].text != "offline-first " || second[1].text != "design" || second[2].text != "." {
			t.Fatalf("第二行的片段文字 = %q %q %q", second[0].text, second[1].text, second[2].text)
		}
		// 常规字重的拉丁词用 Viva 整行量宽（带字偶距），与渲染层的整形差一点：实测「offline-first␠」宽 67.242 对 67.109（差 0.13 px，
		// 其余两片差不到 0.01 px），所以这里的容差是 0.2 px，不是 CJK 与 500 字重那样的 0.01。
		for i, want := range []struct{ x, w float64 }{{0, 67.109}, {69.109, 38.313}, {109.422, 2.891}} {
			if math.Abs(float64(second[i].x)-want.x) > 0.2 || math.Abs(float64(second[i].w)-want.w) > 0.2 {
				t.Errorf("第二行第 %d 片：x=%.3f w=%.3f，期望 x=%.3f w=%.3f", i+1, second[i].x, second[i].w, want.x, want.w)
			}
		}
	})
	t.Run("表格标题：180 宽的钮里折行，内边距算进断点", func(t *testing.T) {
		// 宽 168：产品 28 + 底色 32 + 原则：极简与克 98 = 158 放得下，再加「制」14 放不下，折到第二行。
		got := layoutMarks(t, "产品设计原则：极简与克制", "设计", 14, WeightMedium, 168, 2, false)
		checkLines(t, "title", got, [][]wantFrag{
			{{"产品", false, 0, 28, 0, 0}, {"设计", true, 30, 28, 2, 2}, {"原则：极简与克", false, 60, 98, 0, 0}},
			{{"制", false, 0, 14, 0, 0}},
		}, 0.01)
		// 刚好放得下（172）就不折。
		got = layoutMarks(t, "产品设计原则：极简与克制", "设计", 14, WeightMedium, 172, 2, false)
		if len(got.lines) != 1 {
			t.Errorf("宽 172 刚好放得下，应一行：%+v", got.lines)
		}
	})
	t.Run("表格标题：命中词开头，内边距在行首占位", func(t *testing.T) {
		got := layoutMarks(t, "设计系统：语义令牌清单", "设计", 14, WeightMedium, 158, 2, false)
		checkLines(t, "title", got, [][]wantFrag{{{"设计", true, 2, 28, 2, 2}, {"系统：语义令牌清单", false, 32, 126, 0, 0}}}, 0.01)
	})
}

// wantBox 是期望的一块底色：x0、x1 相对盒的左缘（含内边距），圆角是否出现在左右两侧。
type wantBox struct {
	x0, x1         float64
	roundL, roundR bool
}

// checkBoxes 逐块对照一行的底色矩形（容差 tol）。
func checkBoxes(t *testing.T, name string, got []markBox, want []wantBox, tol float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s：底色 %d 块，期望 %d：%+v", name, len(got), len(want), got)
		return
	}
	for i, w := range want {
		g := got[i]
		if math.Abs(float64(g.x0)-w.x0) > tol || math.Abs(float64(g.x1)-w.x1) > tol || g.roundL != w.roundL || g.roundR != w.roundR {
			t.Errorf("%s：第 %d 块底色 = [%.3f, %.3f] 圆角 %v/%v，期望 [%.3f, %.3f] 圆角 %v/%v",
				name, i+1, g.x0, g.x1, g.roundL, g.roundR, w.x0, w.x1, w.roundL, w.roundR)
		}
	}
}

// 省略号：取最长的前缀使「前缀文字的右缘 + 省略号」放得下（末尾命中词的右内边距不算），省略号是宿主字色的普通文字；
// 被省略号盖掉的命中词没有文字、底色还在原位置，伸出盒子右缘的部分被裁平。
func TestMarkEllipsisMatchesChromium(t *testing.T) {
	t.Run("摘要 truncate：前缀收在命中词末尾，省略号压在右内边距上", func(t *testing.T) {
		// 168 宽：好的 24 + 底色 28 + 是尽可能少的 72 + 底色 28 = 152；命中词文字的右缘 150 加省略号 9.76 放得下，
		// 再加「。」（从 152 起）放不下，所以只留到命中词末尾；省略号从 150 起（不是 152），单独成片。
		got := layoutMarks(t, "好的设计是尽可能少的设计。每一个元素都应当有存在的理由。", "设计", 12, WeightNormal, 168, 0, true)
		checkLines(t, "snippet", got, [][]wantFrag{{
			{"好的", false, 0, 24, 0, 0}, {"设计", true, 26, 24, 2, 2}, {"是尽可能少的", false, 52, 72, 0, 0},
			{"设计", true, 126, 24, 2, 2}, {"…", false, 150, -1, 0, 0},
		}}, 0.05)
		checkBoxes(t, "snippet", got.boxes[0], []wantBox{{24, 52, true, true}, {124, 152, true, true}}, 0.05)
	})
	t.Run("摘要 truncate：前缀留五个字，省略号接在同一段普通文字后", func(t *testing.T) {
		// 168 宽：从 92 起可用 76；五个字 60 加省略号 12 = 72 放得下，六个字 84 放不下。
		got := layoutMarks(t, "《设计中的设计》《规训与惩罚》《失控》。", "设计", 12, WeightNormal, 168, 0, true)
		checkLines(t, "snippet", got, [][]wantFrag{{
			{"《", false, 0, 12, 0, 0}, {"设计", true, 14, 24, 2, 2}, {"中的", false, 40, 24, 0, 0},
			{"设计", true, 66, 24, 2, 2}, {"》《规训与…", false, 92, -1, 0, 0},
		}}, 0.05)
		checkBoxes(t, "snippet", got.boxes[0], []wantBox{{12, 40, true, true}, {64, 92, true, true}}, 0.05)
	})
	t.Run("摘要 truncate：截断落在命中词里，省略号是宿主字色的单独一片，底色照整个命中词铺", func(t *testing.T) {
		// 命中词「与惩」起于 132（文字 134、底色 132–160）。166 宽：「惩」放不下（命中词文字右缘 158 + 省略号 9.76 > 166），
		// 只留「与」：右缘 146；省略号从 146 起，底色还是整个命中词（160 在盒内，右侧有圆角）。
		got := layoutMarks(t, "《设计中的设计》《规训与惩罚》《失控》。", "与惩", 12, WeightNormal, 166, 0, true)
		checkLines(t, "snippet", got, [][]wantFrag{{
			{"《设计中的设计》《规训", false, 0, 132, 0, 0}, {"与", true, 134, 12, 2, 0}, {"…", false, 146, -1, 0, 0},
		}}, 0.05)
		checkBoxes(t, "snippet", got.boxes[0], []wantBox{{132, 160, true, true}}, 0.05)
	})
	t.Run("摘要 truncate：命中词恰好在截断点起头，底色照原位置铺、在盒子右缘被裁平", func(t *testing.T) {
		// 150 宽：「与」放不下（146 + 省略号 9.76 > 150），前缀只到命中词起点 132，省略号从 132 起（压在左内边距上）；
		// 命中词底色 132–160 伸出右缘 150，在 150 处裁平。2026-11-20 的 T2 在大字号档的表格摘要里撞上的正是这一种。
		got := layoutMarks(t, "《设计中的设计》《规训与惩罚》《失控》。", "与惩", 12, WeightNormal, 150, 0, true)
		checkLines(t, "snippet", got, [][]wantFrag{{
			{"《设计中的设计》《规训", false, 0, 132, 0, 0}, {"…", false, 132, -1, 0, 0},
		}}, 0.05)
		checkBoxes(t, "snippet", got.boxes[0], []wantBox{{132, 150, true, false}}, 0.05)
	})
	t.Run("标题 line-clamp-2：截断落在命中词里", func(t *testing.T) {
		// 208 宽，命中词「关于本地」跨第二、三行；第二行没有省略号时放到「关于本」（底色 154–198，右侧是平的）；
		// 加省略号放不下「本」，只留「关于」：右缘 184，省略号从 184 起，底色仍是 154–198。
		text := "一条标题很长很长、需要折成两行才能放下的知识条目：关于本地优先的个人知识库的设计取舍与实现笔记"
		got := layoutMarks(t, text, "关于本地", 14, WeightMedium, 208, 2, false)
		if len(got.lines) != 2 {
			t.Fatalf("clamp 之后应是两行：%+v", got.lines)
		}
		checkLines(t, "title", &markResult{lines: got.lines[1:]}, [][]wantFrag{{
			{"行才能放下的知识条目：", false, 0, 154, 0, 0}, {"关于", true, 156, 28, 2, 0}, {"…", false, 184, -1, 0, 0},
		}}, 0.01)
		checkBoxes(t, "title", got.boxes[1], []wantBox{{154, 198, true, false}}, 0.01)
	})
	t.Run("标题 line-clamp-2：前缀恰好收在命中词末尾，省略号压在右内边距上", func(t *testing.T) {
		// 命中词「条目：关于」文字右缘 184、连右内边距 186；加省略号 198 ≤ 208 放得下，再加「本」放不下，所以在命中词末尾截断；
		// 省略号从 184 起，底色 112–186 照旧。
		text := "一条标题很长很长、需要折成两行才能放下的知识条目：关于本地优先的个人知识库的设计取舍与实现笔记"
		got := layoutMarks(t, text, "条目：关于", 14, WeightMedium, 208, 2, false)
		if len(got.lines) != 2 {
			t.Fatalf("clamp 之后应是两行：%+v", got.lines)
		}
		checkLines(t, "title", &markResult{lines: got.lines[1:]}, [][]wantFrag{{
			{"行才能放下的知识", false, 0, 112, 0, 0}, {"条目：关于", true, 114, 70, 2, 2}, {"…", false, 184, -1, 0, 0},
		}}, 0.01)
		checkBoxes(t, "title", got.boxes[1], []wantBox{{112, 186, true, true}}, 0.01)
	})
	t.Run("标题 line-clamp-2：整行放得下加省略号，省略号并进末尾的普通文字片段", func(t *testing.T) {
		// 15.75px/500、189 宽：第二行没有省略号时是「成两行才能放下的知识条」（173.25，「目：」按禁则留给下一行），
		// 加省略号 15.75 刚好 189，没有字符被截掉（k == z）；被截掉的第一个字符与末尾文字同属一段普通文字，省略号并进同一片
		// （渲染层的文本节点就是这一整段，片段数要一致）。
		text := "一条标题很长很长、需要折成两行才能放下的知识条目：关于本地优先的个人知识库的设计取舍与实现笔记"
		got := layoutMarks(t, text, "设计", 15.75, WeightMedium, 189, 2, false)
		if len(got.lines) != 2 {
			t.Fatalf("clamp 之后应是两行：%+v", got.lines)
		}
		checkLines(t, "title", &markResult{lines: got.lines[1:]}, [][]wantFrag{{
			{"成两行才能放下的知识条…", false, 0, -1, 0, 0},
		}}, 0.01)
		if len(got.boxes[1]) != 0 {
			t.Errorf("命中词在被 clamp 掉的第三行，第二行不该有底色：%+v", got.boxes[1])
		}
	})
	t.Run("放得下的文字不加省略号", func(t *testing.T) {
		got := layoutMarks(t, "产品设计原则：极简与克制", "设计", 14, WeightMedium, 172, 2, true)
		if len(got.lines) != 1 || got.lines[0][len(got.lines[0])-1].text != "原则：极简与克制" {
			t.Errorf("放得下的文字应原样：%+v", got.lines)
		}
	})
}

// 与 React 参照逐宽度对过的省略号版面。数据来自 2026-11-20 的 probe-mark-ellipsis（D:\Project\GuiZhi-bench\tools\probe-mark-ellipsis.mjs，
// 在真实渲染层里按容器宽度扫描、逐像素读底色与省略号墨迹）：14px/400 的汉字，前缀是 10 个「好」（140），
// 其后依次是命中词「设」（文字 142–156、底色 140–158）、「好好」（158–186）、命中词「设」（底色 186–204）、六个「好」。
// 省略号 11.39 宽，墨迹的列号相对它的起点（起点 126 时墨迹在 127、128、131、132、135、136）。
func TestMarkEllipsisAgainstReactProbe(t *testing.T) {
	const text = "好好好好好好好好好好设好好设好好好好好好"
	cases := []struct {
		width float32
		want  []wantFrag // 文字片段（省略号宽度不比）
		boxes []wantBox  // React 里读到的底色范围：[最左, 最右 + 1)
	}{
		// 切在前缀的第十个字之前：省略号并进前缀；命中词的底色（140 起）露在省略号后面，被盒子右缘裁平。
		{150, []wantFrag{{"好好好好好好好好好…", false, 0, -1, 0, 0}}, []wantBox{{140, 150, true, false}}},
		// 前缀十个字放得下，命中词的字加内边距放不下：省略号从前缀末尾（140）起，单独成片，压在底色上；底色 140–153、156。
		{153, []wantFrag{{"好好好好好好好好好好", false, 0, 140, 0, 0}, {"…", false, 140, -1, 0, 0}}, []wantBox{{140, 153, true, false}}},
		{156, []wantFrag{{"好好好好好好好好好好", false, 0, 140, 0, 0}, {"…", false, 140, -1, 0, 0}}, []wantBox{{140, 156, true, false}}},
		// 底色整块（140–158）都在盒内：右侧有圆角。
		{159, []wantFrag{{"好好好好好好好好好好", false, 0, 140, 0, 0}, {"…", false, 140, -1, 0, 0}}, []wantBox{{140, 158, true, true}}},
		{165, []wantFrag{{"好好好好好好好好好好", false, 0, 140, 0, 0}, {"…", false, 140, -1, 0, 0}}, []wantBox{{140, 158, true, true}}},
		// 168：命中词的字放得下（文字右缘 156 + 省略号 11.39 = 167.39 ≤ 168，右内边距不算）；省略号从 156 起，压在右内边距上。
		{168, []wantFrag{{"好好好好好好好好好好", false, 0, 140, 0, 0}, {"设", true, 142, 14, 2, 2}, {"…", false, 156, -1, 0, 0}}, []wantBox{{140, 158, true, true}}},
		{180, []wantFrag{{"好好好好好好好好好好", false, 0, 140, 0, 0}, {"设", true, 142, 14, 2, 2}, {"…", false, 156, -1, 0, 0}}, []wantBox{{140, 158, true, true}}},
		// 184：中间的「好好」留一个字，省略号并进它（起点 172）；第二个命中词的底色从 186 起，还在盒外。
		{184, []wantFrag{{"好好好好好好好好好好", false, 0, 140, 0, 0}, {"设", true, 142, 14, 2, 2}, {"好…", false, 158, -1, 0, 0}}, []wantBox{{140, 158, true, true}}},
		// 188：第二个命中词的底色 186–204 露出 2 px，被右缘裁平。
		{188, []wantFrag{{"好好好好好好好好好好", false, 0, 140, 0, 0}, {"设", true, 142, 14, 2, 2}, {"好…", false, 158, -1, 0, 0}}, []wantBox{{140, 158, true, true}, {186, 188, true, false}}},
		// 196：「好好」的第二个字要 186 + 11.39 = 197.39 才放得下，所以仍只留一个字；198 起留两个字，被截掉的第一个字是第二个命中词的
		// 起头，省略号单独成片、起于 186（压在左内边距上），第二个命中词的底色 186–198 露在后面。
		{196, []wantFrag{{"好好好好好好好好好好", false, 0, 140, 0, 0}, {"设", true, 142, 14, 2, 2}, {"好…", false, 158, -1, 0, 0}}, []wantBox{{140, 158, true, true}, {186, 196, true, false}}},
		{198, []wantFrag{{"好好好好好好好好好好", false, 0, 140, 0, 0}, {"设", true, 142, 14, 2, 2}, {"好好", false, 158, 28, 0, 0}, {"…", false, 186, -1, 0, 0}}, []wantBox{{140, 158, true, true}, {186, 198, true, false}}},
	}
	for _, tc := range cases {
		got := layoutMarks(t, text, "设", 14, WeightNormal, tc.width, 0, true)
		name := "宽 " + strconv.FormatFloat(float64(tc.width), 'f', -1, 32)
		checkLines(t, name, got, [][]wantFrag{tc.want}, 0.05)
		checkBoxes(t, name, got.boxes[0], tc.boxes, 0.05)
	}
}

// 在页面里：命中的片段各是一个 Label，底色在文字下面、范围是文字内容区加两侧内边距，圆角是 rounded-sm；没有命中返回 false。
func TestTextMarkedPaintsBackground(t *testing.T) {
	white := gfx.RGBA(255, 255, 255, 1)
	black := tokens.RGBA(0, 0, 0, 1)
	const left, top = 10, 10
	h := openPage(t, still(), 320, 80, func(l *Look, c ui.C) {
		ui.Column(c).Fill().Background(white).Children(func() {
			ui.Column(c.Key("box")).Absolute().Left(left).Top(top).Width(217).Children(func() {
				box, ok := l.TextMarked(c.Key("m"), "产品设计原则：极简与克制", "设计", 14, 19.25, WeightMedium, black, MarkOptions{})
				if !ok {
					t.Error("有命中时应返回 true")
				}
				_ = box
			})
			ui.Column(c.Key("none")).Absolute().Left(left).Top(50).Width(217).Children(func() {
				if _, ok := l.TextMarked(c.Key("n"), "产品设计原则", "不存在", 14, 19.25, WeightMedium, black, MarkOptions{}); ok {
					t.Error("没有命中应返回 false，由调用方改用普通文字")
				}
			})
		})
	})
	h.frames(3)
	texts := h.session.Snapshot().Texts
	if len(texts) != 3 {
		t.Fatalf("三个片段应是三个 Label：%+v", texts)
	}
	// 字色：命中词是 amber-950（不随宿主字色），其余片段是宿主字色。
	hostColor, markColor := h.look.Col(black), h.look.Col(tokens.Tailwind("amber-950"))
	for _, tx := range texts {
		want := hostColor
		if tx.Value == "设计" {
			want = markColor
		}
		if tx.Style.Color != want {
			t.Errorf("%q 的字色 %v，期望 %v", tx.Value, tx.Style.Color, want)
		}
	}
	img, err := h.session.Image()
	if err != nil {
		t.Fatal(err)
	}
	// 底色：14px/500 的内容区高 19（A 15 + D 4），顶与行盒顶对齐；底色 x 范围 [38, 70)，中线一行两侧内边距处是琥珀色。
	amber := func(x, y int) bool {
		r, g, b, _ := img.At(x, y).RGBA()
		r8, g8, b8 := int(r>>8), int(g>>8), int(b>>8)
		// 251,191,36 在白底上叠 0.9：约 (252, 197, 58)。
		return abs(r8-252) <= 3 && abs(g8-197) <= 3 && abs(b8-58) <= 3
	}
	mid := top + 9
	for _, x := range []int{38, 39, 68, 69} {
		if !amber(x, mid) {
			t.Errorf("(%d,%d) 应是琥珀底色：%v", x, mid, pixelOf(img, x, mid))
		}
	}
	// 底色上下不超出内容区：行盒顶上一行与内容区底下一行都是白底。
	for _, y := range []int{top - 1, top + 19} {
		if amber(39, y) {
			t.Errorf("(39,%d) 在内容区之外，不该有底色", y)
		}
	}
	// 圆角：左上角那一格被圆角切掉。
	if amber(38, top) {
		t.Errorf("(38,%d) 是底色的左上角，应被 rounded-sm 的圆角切掉", top)
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func pixelOf(img image.Image, x, y int) [3]uint32 {
	r, g, b, _ := img.At(x, y).RGBA()
	return [3]uint32{r >> 8, g >> 8, b >> 8}
}

// 命中模型按（原文，命中词）缓存：稳态帧里列表每帧都把可见的每一行问一遍，不能每次都逐字扫描、分配
// （1 万条库的表格每页 100 行、开着搜索时，没有这层缓存稳态帧均值 2.23 ms，越过完成定义第 4 条的 2 ms）。
func TestMarkModelOfIsCached(t *testing.T) {
	look, canvas := canvasOf(t)
	const text = "产品设计原则：极简与克制"
	a := look.markModelOf(canvas, text, "设计")
	if a == nil || a.full != text {
		t.Fatalf("有命中时应返回折叠后的模型：%+v", a)
	}
	if b := look.markModelOf(canvas, text, "设计"); b != a {
		t.Error("同一（原文，命中词）应返回缓存里的同一个模型")
	}
	if c := look.markModelOf(canvas, text, "原则"); c == a || c == nil {
		t.Errorf("命中词不同应是另一个模型：%p 对 %p", c, a)
	}
	if look.markModelOf(canvas, text, "不存在") != nil {
		t.Error("没有命中应返回 nil")
	}
	// 稳态：命中与没有命中两种结果都不再分配。
	if n := testing.AllocsPerRun(100, func() {
		look.markModelOf(canvas, text, "设计")
		look.markModelOf(canvas, text, "不存在")
	}); n != 0 {
		t.Errorf("稳态下每次查询应零分配，得到 %v", n)
	}
	// TextMarked 把命中词两端的空白去掉后再查：带空白的写法落在同一个缓存项上。
	var pageCanvas *gfx.Canvas
	h := openPage(t, still(), 200, 60, func(l *Look, c ui.C) {
		pageCanvas = c.Canvas()
		ui.Column(c).Fill().Children(func() {
			l.TextMarked(c.Key("a"), text, "设计", 14, 19.25, WeightMedium, tokens.RGBA(0, 0, 0, 1), MarkOptions{})
			l.TextMarked(c.Key("b"), text, "  设计 ", 14, 19.25, WeightMedium, tokens.RGBA(0, 0, 0, 1), MarkOptions{})
		})
	})
	h.frames(2)
	if n := len(h.look.noKern(pageCanvas).models); n != 1 {
		t.Errorf("两种写法应共用一个缓存项，得到 %d 项", n)
	}
	// 缓存满了整体清空重来，不无限增长。
	for i := 0; i < noKernLinesMax+10; i++ {
		look.markModelOf(canvas, strings.Repeat("好", 1+i%50)+strconv.Itoa(i), "好")
	}
	if n := len(look.noKern(canvas).models); n > noKernLinesMax {
		t.Errorf("缓存条数 %d 超过上限 %d", n, noKernLinesMax)
	}
}
