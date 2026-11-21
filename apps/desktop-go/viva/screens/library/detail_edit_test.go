package library

import (
	"strings"
	"testing"

	"guizhi-mygo-shell/viva/reader"
)

// 编辑器的改动只更新已有的草稿：没有这份草稿（「完成编辑」成功删掉之后迟到的改动、没进过编辑态的条目）不能把它复活；
// 讨论、文字稿这类不是原文来源的标签不记。
func TestRecordDraftUpdatesOnlyAnExistingDraftOfTheRightSource(t *testing.T) {
	t.Parallel()
	s := &Screen{}
	e := &s.detail.edit
	e.drafts = map[string]editDraft{
		"a:body":    {text: "旧", baseline: "正文基线"},
		"a:summary": {text: "总结", baseline: "总结基线"},
	}

	s.recordDraft("a", reader.TabBody, "新")
	if got := e.drafts["a:body"]; got != (editDraft{text: "新", baseline: "正文基线", touched: true}) {
		t.Errorf("正文草稿换全文并记为改过、基线不动：%+v", got)
	}
	if got := e.drafts["a:summary"]; got != (editDraft{text: "总结", baseline: "总结基线"}) {
		t.Errorf("别的来源的草稿不受影响：%+v", got)
	}

	s.recordDraft("a", reader.TabSummary, "总结二")
	if got := e.drafts["a:summary"].text; got != "总结二" {
		t.Errorf("讨论总结标签的改动记在总结草稿上：%q", got)
	}

	s.recordDraft("b", reader.TabBody, "没进过编辑态")
	s.recordDraft("a", reader.TabReplies, "讨论标签不是原文来源")
	s.recordDraft("a", reader.TabTranscript, "文字稿也不是")
	if len(e.drafts) != 2 {
		t.Errorf("不该凭空多出草稿：%+v", e.drafts)
	}
	if got := e.drafts["a:body"].text; got != "新" {
		t.Errorf("非原文来源的标签不记：%q", got)
	}

	delete(e.drafts, "a:body")
	s.recordDraft("a", reader.TabBody, "迟到的改动")
	if _, resurrected := e.drafts["a:body"]; resurrected {
		t.Error("草稿删掉之后，迟到的改动不能把它复活")
	}
}

func TestDraftKeyIsItemAndSource(t *testing.T) {
	t.Parallel()
	if got := draftKey("id-1", sourceSummary); got != "id-1:summary" {
		t.Errorf("草稿键与渲染层 editKey 同形：%q", got)
	}
	for tab, want := range map[reader.Tab]string{reader.TabBody: sourceBody, reader.TabSummary: sourceSummary} {
		if got, ok := sourceKindOfTab(tab); !ok || got != want {
			t.Errorf("%s → %q %v，期望 %q", tab, got, ok, want)
		}
	}
	for _, tab := range []reader.Tab{reader.TabReplies, reader.TabImages, reader.TabRecognized, reader.TabTranscript, ""} {
		if _, ok := sourceKindOfTab(tab); ok {
			t.Errorf("%q 不是原文来源", tab)
		}
	}
	for tab, want := range map[ReadingTab]string{TabSummary: sourceSummary, TabBody: sourceBody, TabReplies: sourceBody, TabTranscript: sourceBody} {
		if got := sourceKindOfActive(tab); got != want {
			t.Errorf("标签 %q 的来源：%q，期望 %q（只有讨论总结标签是 summary，其余都当正文）", tab, got, want)
		}
	}
}

// 只在字节数超过上限时才逐个数码元（每个字节至多对应一个码元）；增补平面的字符占两个码元，所以 4 字节的表情算 2 个。
func TestUTF16ExceedsCountsCodeUnitsLikeTheRenderer(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		s     string
		limit int
		n     int
		over  bool
	}{
		{"", 10, 0, false},
		{"abc", 10, 0, false},
		{"abc", 3, 0, false},
		{"中文", 10, 0, false}, // 6 字节没超过上限 10：码元数更不可能超过
		{"中文", 5, 2, false},  // 6 字节超过上限 5，数出来 2 个码元：没超
		{"😀", 3, 2, false},   // 4 字节、一个增补平面字符 = 2 个码元
		{strings.Repeat("😀", 4), 7, 8, true},
		{strings.Repeat("😀", 4), 8, 8, false},
		{strings.Repeat("a", 20), 10, 20, true},
	} {
		if n, over := utf16Exceeds(c.s, c.limit); n != c.n || over != c.over {
			t.Errorf("utf16Exceeds(%q, %d) = %d, %v，期望 %d, %v", c.s, c.limit, n, over, c.n, c.over)
		}
	}
}
