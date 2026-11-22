package wiki

import (
	"slices"
	"strings"
	"testing"
)

func TestSlugify(t *testing.T) {
	cases := []struct {
		in         string
		occurrence int
		want       string
	}{
		{"Hello World", 0, "hello-world"},
		{"  Hello   World  ", 0, "hello-world"},
		{"Hello World", 2, "hello-world-2"},
		{"中文 标题", 0, "中文-标题"},
		{"全角　空格", 0, "全角-空格"},
		{"**Bold** `code` [link](x)", 0, "bold-code-linkx"},
		{"What's new? (v2.0)", 0, "whats-new-v20"},
		{"a - b", 0, "a---b"},
		{"1. 第一步", 0, "1-第一步"},
		{"???", 0, "section"},
		{"", 0, "section"},
		{"???", 3, "section-3"},
		{"ÀÉ Ünï", 0, "àé-ünï"},
	}
	for _, c := range cases {
		if got := Slugify(c.in, c.occurrence); got != c.want {
			t.Errorf("Slugify(%q,%d)=%q want %q", c.in, c.occurrence, got, c.want)
		}
	}
}

func TestExtractTocBasics(t *testing.T) {
	body := "# Title\n\nintro\n\n## First\ntext\n### Sub A\n#### too deep\n## Second ##\n### Sub B\n"
	got := ExtractToc(body)
	want := []TocEntry{
		{2, "First", "first"}, {3, "Sub A", "sub-a"}, {2, "Second", "second"}, {3, "Sub B", "sub-b"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestExtractTocSkipsFences(t *testing.T) {
	body := "## Real\n```bash\n## comment in code\n### another\n```\n## After\n~~~\n## tilde fence\n~~~\n### Tail"
	got := ExtractToc(body)
	var texts []string
	for _, e := range got {
		texts = append(texts, e.Text)
	}
	if !slices.Equal(texts, []string{"Real", "After", "Tail"}) {
		t.Fatalf("围栏代码块里的 # 不是标题：%v", texts)
	}
}

func TestExtractTocDuplicateSlugs(t *testing.T) {
	got := ExtractToc("## Same\n## Same\n### Same\n## Other")
	slugs := make([]string, len(got))
	for i, e := range got {
		slugs[i] = e.Slug
	}
	if !slices.Equal(slugs, []string{"same", "same-1", "same-2", "other"}) {
		t.Fatalf("同名标题追加序号：%v", slugs)
	}
}

func TestExtractTocEdgeCases(t *testing.T) {
	if got := ExtractToc(""); len(got) != 0 {
		t.Fatalf("空正文：%+v", got)
	}
	if got := ExtractToc("##\n## \n##   ##\n#hashtag\n##nospace"); len(got) != 0 {
		t.Fatalf("空标题与没有空格的 # 都不算标题：%+v", got)
	}
	crlf := ExtractToc("## One\r\n\r\ntext\r\n### Two\r\n")
	if len(crlf) != 2 || crlf[0].Text != "One" || crlf[1].Text != "Two" {
		t.Fatalf("CRLF 换行：%+v", crlf)
	}
	indented := ExtractToc("   ## Indented\n\t### Tab")
	if len(indented) != 2 {
		t.Fatalf("行首空白不影响：%+v", indented)
	}
	if got := ExtractToc("## 概念 与 实体"); len(got) != 1 || got[0].Slug != "概念-与-实体" {
		t.Fatalf("中文标题：%+v", got)
	}
	unclosed := ExtractToc("```\n## inside\nnever closed\n## also inside")
	if len(unclosed) != 0 {
		t.Fatalf("未闭合的围栏吞掉后面全部内容：%+v", unclosed)
	}
}

func TestExtractTocLargeBody(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 2000; i++ {
		b.WriteString("## Section\n\ntext\n\n")
	}
	got := ExtractToc(b.String())
	if len(got) != 2000 || got[1999].Slug != "section-1999" {
		t.Fatalf("大正文：%d 项，末项 %q", len(got), got[len(got)-1].Slug)
	}
}

func TestShowToc(t *testing.T) {
	if ShowToc(make([]TocEntry, TocMinHeadings-1)) || !ShowToc(make([]TocEntry, TocMinHeadings)) {
		t.Fatal("标题数不少于门槛才显示目录")
	}
}

func TestRenderedBodyStripsDuplicateTitle(t *testing.T) {
	if got := RenderedBody("# My Page\n\nbody", "My Page"); got != "body" {
		t.Fatalf("got %q", got)
	}
	if got := RenderedBody("# Other\n\nbody", "My Page"); got != "# Other\n\nbody" {
		t.Fatalf("标题不同不剥：%q", got)
	}
	if got := RenderedBody("body\n# My Page", "My Page"); got != "body\n# My Page" {
		t.Fatalf("只处理第一行：%q", got)
	}
}
