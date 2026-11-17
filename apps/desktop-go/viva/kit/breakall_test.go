package kit

import (
	"slices"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// cell10 是每个字符 10 宽的度量：断点只取决于规则，不取决于字体。
func cell10(s string) float32 { return float32(len([]rune(s))) * 10 }

func TestWrapBreakAllRules(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		width float32
		want  []string
	}{
		{"空文字是一行空串", "", 100, []string{""}},
		{"放得下不折", "abcdefghij", 100, []string{"abcdefghij"}},
		{"行满处逐字折，不找词边界", "abcdefghij", 45, []string{"abcd", "efgh", "ij"}},
		{"硬换行保留", "ab\ncd", 100, []string{"ab", "cd"}},
		{"宽度不大于零只按硬换行切", "abc def\nghi", 0, []string{"abc def", "ghi"}},
		{"CRLF 当作一个换行", "ab\r\ncd", 100, []string{"ab", "cd"}},
		{"斜杠之前不能断：退到前一个字符之前", "aaaaaaaaaa/bbb", 105, []string{"aaaaaaaaa", "a/bbb"}},
		{"斜杠之后可以断（后面是数字也一样）", "ab/1234", 35, []string{"ab/", "123", "4"}},
		{"连字符之前不能断", "aaa-bbb", 35, []string{"aa", "a-b", "bb"}},
		{"小数点之前不能断", "aaa.bbb", 35, []string{"aa", "a.b", "bb"}},
		{"汉字之间随处可断", "汉字汉字汉字", 40, []string{"汉字汉字", "汉字"}},
		{"避头：关闭类标点不起行", "汉字汉，字", 25, []string{"汉字", "汉，", "字"}},
		{"避尾：开启类括号不落行尾", "abc（d", 45, []string{"abc", "（d"}},
		{"空格悬挂在行尾，换行后行首的空格去掉", "aaaa bbbb", 45, []string{"aaaa", "bbbb"}},
		{"整词放得下也在行满处断（不把整词移到下一行）", "aa bbbbbb", 55, []string{"aa bb", "bbbb"}},
		{"一个字符也放不下时它独占一行", "汉字", 5, []string{"汉", "字"}},
		{"没有任何允许的断点时在最后放得下的位置硬断", "///////", 35, []string{"///", "///", "/"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := WrapBreakAll(tc.text, tc.width, cell10)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("WrapBreakAll(%q, %v)\n got: %q\nwant: %q", tc.text, tc.width, got, tc.want)
			}
		})
	}
}

// 网址的第一行取自 React 参照（dlg-detail-failed，中号字、模态内容宽 750 的一半 367px）：
// 「https://www.zhihu.com/question/123456789/answer/98765432」之后换行，剩下「1」，而不是 Viva 默认的在「answer/」之后换行。
func TestWrapBreakAllFillsTheLineLikeChromium(t *testing.T) {
	const url = "https://www.zhihu.com/question/123456789/answer/987654321"
	// 度量要用装好界面字体的画布：取无头会话里那一块。
	var canvas *gfx.Canvas
	openPage(t, still(), 400, 200, func(_ *Look, c ui.C) { canvas = c.Canvas() })
	measure := func(s string) float32 { return canvas.TextWidthWeight(s, 12, WeightNormal) }
	got := WrapBreakAll(url, 367, measure)
	want := []string{"https://www.zhihu.com/question/123456789/answer/98765432", "1"}
	if !slices.Equal(got, want) {
		t.Fatalf("网址折行不对\n got: %q\nwant: %q", got, want)
	}
	if def := canvas.WrapLines(nil, url, 12, WeightNormal, 367, 0); len(def) == 2 && def[0] == want[0] {
		t.Fatalf("Viva 默认折行已经与 break-all 一致（%q），这个自定义折行可以退役", def)
	}
}

func TestTextBreakAllSizesItselfByTheLinesItBreaks(t *testing.T) {
	const url = "https://www.zhihu.com/question/123456789/answer/987654321"
	h := openPage(t, still(), 400, 200, func(l *Look, c ui.C) {
		ui.Column(c.Key("page")).Width(367).Children(func() {
			l.TextBreakAll(c.Key("url"), url, TextXS, WeightNormal, l.T.Foreground)
			l.Text(c.Key("after"), "after", TextXS, WeightNormal, l.T.Foreground)
		})
	})
	var lines []string
	for _, text := range h.session.Texts() {
		if text != "after" {
			lines = append(lines, text)
		}
	}
	want := []string{"https://www.zhihu.com/question/123456789/answer/98765432", "1"}
	if !slices.Equal(lines, want) {
		t.Fatalf("画出的行不对：%q，期望 %q", lines, want)
	}
	// 下面的元素落在两行之后：两行各 16px。
	var afterY float32 = -1
	for _, text := range h.session.Snapshot().Texts {
		if text.Value == "after" {
			afterY = text.Bounds.Y
		}
	}
	if afterY < 32 {
		t.Fatalf("两行文字占 32px，下面的元素应在 y≥32，实际在 %v", afterY)
	}
}

func TestTextBreakAllWidthChangeReflows(t *testing.T) {
	text := strings.Repeat("a", 60)
	narrow := openPage(t, still(), 400, 200, func(l *Look, c ui.C) {
		ui.Column(c.Key("page")).Width(100).Children(func() {
			l.TextBreakAll(c.Key("t"), text, TextXS, WeightNormal, l.T.Foreground)
		})
	})
	wide := openPage(t, still(), 400, 200, func(l *Look, c ui.C) {
		ui.Column(c.Key("page")).Width(300).Children(func() {
			l.TextBreakAll(c.Key("t"), text, TextXS, WeightNormal, l.T.Foreground)
		})
	})
	if n, w := len(narrow.session.Texts()), len(wide.session.Texts()); n <= w {
		t.Fatalf("窄容器应比宽容器折出更多行：窄 %d 行，宽 %d 行", n, w)
	}
	if got := strings.Join(narrow.session.Texts(), ""); got != text {
		t.Fatalf("折行不得增删字符：%q", got)
	}
}
