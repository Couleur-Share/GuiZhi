package kit

import (
	"strings"
	"testing"
	"time"
)

// 本文件测摘要用的 Markdown 解析（markdown.go）与排版前的块结构：解析是纯函数，没有窗口。

func kinds(blocks []MdBlock) []MdKind {
	out := make([]MdKind, len(blocks))
	for i, b := range blocks {
		out[i] = b.Kind
	}
	return out
}

func TestParseMarkdownBlocks(t *testing.T) {
	t.Parallel()
	src := "# 标题一\n\n段落一\n续行\n\n- 甲\n- 乙\n\n1. 一\n2. 二\n\n> 引用\n\n---\n\n```go\nx := 1\n\ny := 2\n```\n\n    缩进代码\n\n尾段"
	got := ParseMarkdown(src)
	want := []MdKind{MdHeading, MdParagraph, MdList, MdList, MdQuote, MdRule, MdCode, MdCode, MdParagraph}
	if k := kinds(got); len(k) != len(want) {
		t.Fatalf("块种类 %v，期望 %v", k, want)
	} else {
		for i := range want {
			if k[i] != want[i] {
				t.Fatalf("第 %d 块种类 %v，期望 %v（全部 %v）", i, k[i], want[i], k)
			}
		}
	}
	if got[0].Level != 1 || MdPlain(got[0].Spans) != "标题一" {
		t.Errorf("标题：%+v", got[0])
	}
	if p := MdPlain(got[1].Spans); p != "段落一\n续行" {
		t.Errorf("软换行保留在文字里（排版时按空白折叠成一个空格）：%q", p)
	}
	if !got[3].Ordered || got[3].Start != 1 || len(got[3].Items) != 2 {
		t.Errorf("有序列表：%+v", got[3])
	}
	if got[6].Text != "x := 1\n\ny := 2" && got[6].Text != "x := 1\n\ny := 2\n" {
		t.Errorf("围栏代码保留空行：%q", got[6].Text)
	}
	if got[6].Lang != "go" {
		t.Errorf("围栏语言：%q", got[6].Lang)
	}
}

func TestParseMarkdownInline(t *testing.T) {
	t.Parallel()
	spans := ParseMarkdown("普通 **粗体** 与 *斜体* 以及 `代码` 和 ~~删除~~ 还有 [链接](https://example.com)。")[0].Spans
	has := func(match func(MdSpan) bool, text string) {
		t.Helper()
		for _, s := range spans {
			if match(s) && strings.Contains(s.Text, text) {
				return
			}
		}
		t.Errorf("缺少带样式的「%s」：%+v", text, spans)
	}
	has(func(s MdSpan) bool { return s.Bold }, "粗体")
	has(func(s MdSpan) bool { return s.Italic }, "斜体")
	has(func(s MdSpan) bool { return s.Code }, "代码")
	has(func(s MdSpan) bool { return s.Strike }, "删除")
	has(func(s MdSpan) bool { return s.Href == "https://example.com" }, "链接")
}

// 不安全的链接目标不当成链接（只留文字），未闭合的强调按字面输出。
func TestParseMarkdownIsConservative(t *testing.T) {
	t.Parallel()
	spans := ParseMarkdown("[点我](javascript:alert(1)) 与 **没有闭合")[0].Spans
	for _, s := range spans {
		if s.Href != "" {
			t.Errorf("javascript: 目标不该成为链接：%+v", s)
		}
	}
	if p := MdPlain(spans); !strings.Contains(p, "点我") || !strings.Contains(p, "**没有闭合") {
		t.Errorf("文字应原样保留：%q", p)
	}
}

// 嵌套列表：子项挂在父项下；紧凑与宽松列表的区别保留到块上。
func TestParseMarkdownNestedLists(t *testing.T) {
	t.Parallel()
	got := ParseMarkdown("- 甲\n  - 甲一\n  - 甲二\n- 乙\n")
	if len(got) != 1 || got[0].Kind != MdList || len(got[0].Items) != 2 {
		t.Fatalf("应是一个两项的列表：%+v", got)
	}
	first := got[0].Items[0]
	if len(first) != 2 || first[1].Kind != MdList || len(first[1].Items) != 2 {
		t.Fatalf("第一项里应有段落与一个两项的子列表：%+v", first)
	}
}

// 病态输入不能卡死或爆栈：超深嵌套、成千上万的强调符、未闭合的围栏。
func TestParseMarkdownSurvivesPathologicalInput(t *testing.T) {
	t.Parallel()
	inputs := map[string]string{
		"深引用":   strings.Repeat("> ", 500) + "x",
		"深列表":   strings.Repeat("  ", 1) + strings.Repeat("- a\n"+strings.Repeat(" ", 2), 300),
		"强调符":   strings.Repeat("*a ", 20000),
		"未闭合围栏": "```\n" + strings.Repeat("行\n", 5000),
		"空":     "",
		"只有空白":  "  \n\n \t\n",
	}
	for name, src := range inputs {
		done := make(chan struct{})
		go func() {
			defer close(done)
			ParseMarkdown(src)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("%s：解析超时", name)
		}
	}
}
