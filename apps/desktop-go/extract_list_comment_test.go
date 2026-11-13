package main

import (
	"strings"
	"testing"

	"guizhi-mygo-shell/extract"
)

// 真实维基页暴露：html-to-markdown 在相邻的两个列表之间插入字面量 <!--THE END-->，
// Turndown 不会。这行注释会进入条目正文、被全文索引（"THE END" 能搜到）、也会进入 AI 上下文。
func TestExtractAdjacentListsHaveNoEndComment(t *testing.T) {
	para := strings.Repeat("这是一段足够长的正文，用来让正文提取选中文章区域而不是整页。", 12)
	page := `<!doctype html><html><head><title>相邻列表</title></head><body><article><h1>相邻列表</h1><p>` + para + `</p>
<ul><li>第一组甲</li><li>第一组乙</li></ul>
<ul><li>第二组甲</li><li>第二组乙</li></ul>
<p>` + para + `</p></article></body></html>`
	a := extract.ExtractWebArticle(page, "https://example.com/lists", 200)
	if a.Markdown == "" {
		t.Fatalf("没有提取到正文：%+v", a)
	}
	if strings.Contains(a.Markdown, "<!--") || strings.Contains(a.Markdown, "THE END") {
		t.Fatalf("正文里出现了列表分隔注释：\n%s", a.Markdown)
	}
	for _, want := range []string{"第一组甲", "第一组乙", "第二组甲", "第二组乙"} {
		if !strings.Contains(a.Markdown, want) {
			t.Errorf("丢了列表项 %q", want)
		}
	}
}
