package ask

import (
	"slices"
	"strings"
	"testing"
)

// 以下用例与渲染层 tests/unit/components/qa-citations.test.ts 逐条对应，期望串一字不差。

var validOrdinals = []int{1, 2}

func TestLinkifySingleOrdinalWholeMarkerBecomesLink(t *testing.T) {
	got := LinkifyCitations("优先 DDNS + 反向代理 [1]。", validOrdinals, nil)
	want := "优先 DDNS + 反向代理 [\\[1\\]](" + CiteHrefPrefix + "1)。"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestLinkifyFullWidthBracketsAreRecognized(t *testing.T) {
	got := LinkifyCitations("见【2】", validOrdinals, nil)
	if want := "见[\\[2\\]](" + CiteHrefPrefix + "2)"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestLinkifyMultipleOrdinalsEachLinkedBracketsLiteral(t *testing.T) {
	got := LinkifyCitations("两处都提到 [1,2]", validOrdinals, nil)
	want := "两处都提到 \\[[1](" + CiteHrefPrefix + "1), [2](" + CiteHrefPrefix + "2)\\]"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestLinkifyUnknownOrdinalsStayPlainNotDeadLinks(t *testing.T) {
	if got := LinkifyCitations("参见 [9]", validOrdinals, nil); got != "参见 [9]" {
		t.Fatalf("没有来源的编号应原样保留：%q", got)
	}
	// 混合时只链得上的那个成链
	got := LinkifyCitations("[2,9]", validOrdinals, nil)
	if want := "\\[[2](" + CiteHrefPrefix + "2), 9\\]"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestLinkifySkipsInlineCode(t *testing.T) {
	// 数组下标、正则字符组长得和引用一模一样
	got := LinkifyCitations("用 `items[1]` 取值 [1]", validOrdinals, nil)
	if want := "用 `items[1]` 取值 [\\[1\\]](" + CiteHrefPrefix + "1)"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestLinkifySkipsFencedCodeBlocks(t *testing.T) {
	markdown := strings.Join([]string{"前言 [1]", "```js", "const a = arr[1];", "```", "后记 [2]"}, "\n")
	got := LinkifyCitations(markdown, validOrdinals, nil)
	for _, want := range []string{"const a = arr[1];", "前言 [\\[1\\]](" + CiteHrefPrefix + "1)", "后记 [\\[2\\]](" + CiteHrefPrefix + "2)"} {
		if !strings.Contains(got, want) {
			t.Fatalf("输出应包含 %q：%q", want, got)
		}
	}
	// ~~~ 围栏同样跳过，且围栏未闭合时后面整段都当代码。
	tilde := "~~~\narr[1]\n~~~\n[1]"
	if got := LinkifyCitations(tilde, validOrdinals, nil); !strings.Contains(got, "arr[1]") || !strings.HasSuffix(got, "(#qa-cite=1)") {
		t.Fatalf("波浪线围栏：%q", got)
	}
	open := "```\narr[1]\n[2]"
	if got := LinkifyCitations(open, validOrdinals, nil); got != open {
		t.Fatalf("围栏没闭合时其后的内容都不改写：%q", got)
	}
}

func TestLinkifyReturnsInputWhenNoSourcesOrEmpty(t *testing.T) {
	if got := LinkifyCitations("有 [1] 标记", nil, nil); got != "有 [1] 标记" {
		t.Fatalf("没有来源：%q", got)
	}
	if got := LinkifyCitations("", validOrdinals, nil); got != "" {
		t.Fatalf("空正文：%q", got)
	}
}

func TestLinkifyDoesNotTouchMarkdownLinks(t *testing.T) {
	got := LinkifyCitations("见 [文档](https://example.com) 与 [1]", validOrdinals, nil)
	if want := "见 [文档](https://example.com) 与 [\\[1\\]](" + CiteHrefPrefix + "1)"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestCitationLabelsFollowFirstAppearanceThenUnmentioned(t *testing.T) {
	const answer = "先看 [7]，再看 [3]；两者都提到 [3、12]。代码 `a[5]` 不算。"
	ordinals := []int{3, 5, 7, 12}
	labels := BuildCitationLabels(answer, ordinals)
	if got := labels.Ordinals(); !slices.Equal(got, []int{7, 3, 12, 5}) {
		t.Fatalf("展示顺序：%v", got)
	}
	for original, want := range map[int]int{7: 1, 3: 2, 12: 3, 5: 4} {
		if got, ok := labels.Get(original); !ok || got != want {
			t.Fatalf("编号 %d 应展示为 %d：%d %v", original, want, got, ok)
		}
	}
	if _, ok := labels.Get(99); ok {
		t.Fatal("没登记的编号不应有展示编号")
	}
	if got := labels.LabelOf(99); got != 99 {
		t.Fatalf("没登记的编号沿用原编号：%d", got)
	}

	linked := LinkifyCitations(answer, ordinals, labels)
	for _, want := range []string{
		"[\\[1\\]](" + CiteHrefPrefix + "7)",
		"\\[[2](" + CiteHrefPrefix + "3), [3](" + CiteHrefPrefix + "12)\\]",
		"`a[5]`",
	} {
		if !strings.Contains(linked, want) {
			t.Fatalf("展示编号显示、链接仍指向原始编号，缺 %q：%q", want, linked)
		}
	}

	if got := RenumberCitations(answer, ordinals, labels); got != "先看 [1]，再看 [2]；两者都提到 [2, 3]。代码 `a[5]` 不算。" {
		t.Fatalf("复制文本与屏幕编号一致：%q", got)
	}
}

func TestCitationLabelsDropUnresolvableOrdinalsToAvoidCollisions(t *testing.T) {
	const text = "见 [9] 与 [3, 2]"
	labels := BuildCitationLabels(text, []int{3})
	got := LinkifyCitations(text, []int{3}, labels)
	if want := "见  与 [\\[1\\]](" + CiteHrefPrefix + "3)"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	// 一个都对不上的标记整个去掉（给了展示编号时），复制文本同理。
	if got := RenumberCitations("无依据 [9]。", []int{3}, labels); got != "无依据 。" {
		t.Fatalf("复制时无来源的标记去掉：%q", got)
	}
}

// JS 的 \s 比 RE2 宽：不间断空格与全角空格也算标记里的空白。
func TestCitationMarkerAcceptsJSWhitespace(t *testing.T) {
	for _, text := range []string{"见 [\u00a01\u00a0]", "见 [　1　]", "见 [1\u00a0,\u00a02]"} {
		if got := LinkifyCitations(text, validOrdinals, nil); !strings.Contains(got, CiteHrefPrefix) {
			t.Fatalf("%q 应被识别为引用：%q", text, got)
		}
	}
	// 超过三位数的编号不是引用标记。
	if got := LinkifyCitations("见 [1234]", []int{1234}, nil); got != "见 [1234]" {
		t.Fatalf("四位数不是引用：%q", got)
	}
}

func TestParseCiteHref(t *testing.T) {
	for href, want := range map[string]int{"#qa-cite=1": 1, "#qa-cite=12": 12, "#qa-cite=007": 7} {
		if got, ok := ParseCiteHref(href); !ok || got != want {
			t.Fatalf("%s：%d %v", href, got, ok)
		}
	}
	for _, href := range []string{"", "#qa-cite=", "#qa-cite=abc", "#qa-cite=1e2", "#qa-cite=-1", "https://example.com", "#other=1", "#qa-cite=99999999999999999999"} {
		if _, ok := ParseCiteHref(href); ok {
			t.Fatalf("%q 不应被识别为引用锚点", href)
		}
	}
}

func TestLinkifyAndParseRoundTrip(t *testing.T) {
	linked := LinkifyCitations("见 [2]", validOrdinals, nil)
	start := strings.Index(linked, "(")
	end := strings.LastIndex(linked, ")")
	if got, ok := ParseCiteHref(linked[start+1 : end]); !ok || got != 2 {
		t.Fatalf("生成的链接应能还原编号：%q → %d %v", linked, got, ok)
	}
}

func TestSortOrdinalsByLabelIsStable(t *testing.T) {
	labels := BuildCitationLabels("先 [7] 后 [3]", []int{3, 5, 7})
	if got := SortOrdinalsByLabel([]int{3, 5, 7}, labels); !slices.Equal(got, []int{7, 3, 5}) {
		t.Fatalf("按展示编号排：%v", got)
	}
	// 没有展示编号时按原编号。
	if got := SortOrdinalsByLabel([]int{5, 3, 7}, nil); !slices.Equal(got, []int{3, 5, 7}) {
		t.Fatalf("无展示编号：%v", got)
	}
}
