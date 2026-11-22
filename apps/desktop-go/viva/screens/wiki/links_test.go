package wiki

import (
	"slices"
	"strings"
	"testing"

	"guizhi-mygo-shell/native/services"
)

func TestEncodeURIComponentMatchesJS(t *testing.T) {
	cases := map[string]string{
		"abc-_.!~*'()XYZ019": "abc-_.!~*'()XYZ019",
		"a b/c?d=é":          "a%20b%2Fc%3Fd%3D%C3%A9",
		"概念":                 "%E6%A6%82%E5%BF%B5",
		"100%":               "100%25",
		"a+b&c#d":            "a%2Bb%26c%23d",
		"😀":                  "%F0%9F%98%80",
		"":                   "",
	}
	for in, want := range cases {
		if got := encodeURIComponent(in); got != want {
			t.Errorf("encodeURIComponent(%q)=%q want %q", in, got, want)
		}
	}
}

func TestPreprocessLinks(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"简单链接", "see [[Alpha]] now", "see [Alpha](#wiki=Alpha) now"},
		{"带显示文字", "[[Alpha Topic|the first]]", "[the first](#wiki=Alpha%20Topic)"},
		{"两侧空白被去掉", "[[  Alpha  |  shown  ]]", "[shown](#wiki=Alpha)"},
		{"中文目标编码", "[[概念]]", "[概念](#wiki=%E6%A6%82%E5%BF%B5)"},
		{"空目标只留显示文字", "x [[ |only text]] y", "x only text y"},
		{"空目标空显示", "x [[ ]] y", "x  y"},
		{"同一行多个", "[[A]] and [[B|b]]", "[A](#wiki=A) and [b](#wiki=B)"},
		{"没有闭合的保持原样", "[[unclosed and ]single]", "[[unclosed and ]single]"},
		{"嵌套括号不匹配", "[[a[b]]]", "[[a[b]]]"},
		{"只有第一个竖线分隔", "[[a|b|c]]", "[b|c](#wiki=a)"},
		{"没有链接", "plain text", "plain text"},
		{"空串", "", ""},
	}
	for _, c := range cases {
		if got := PreprocessLinks(c.in); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestParseHrefRoundTrip(t *testing.T) {
	for _, target := range []string{"Alpha", "Alpha Topic", "概念 (v2)", "a/b?c=d&e#f", "100%", "😀 emoji", "plus+sign"} {
		md := PreprocessLinks("[[" + target + "]]")
		href := md[strings.LastIndex(md, "](")+2 : len(md)-1]
		got, ok := ParseHref(href)
		if !ok || got != target {
			t.Errorf("往返 %q：got %q ok=%v (href %q)", target, got, ok, href)
		}
	}
}

func TestParseHrefRejects(t *testing.T) {
	for _, href := range []string{"", "https://example.com", "#other=x", "wiki=abc", "#wiki=%E0%A4%A", "#wiki=%zz"} {
		if got, ok := ParseHref(href); ok {
			t.Errorf("ParseHref(%q) 应失败，得到 %q", href, got)
		}
	}
	if got, ok := ParseHref("#wiki="); !ok || got != "" {
		t.Fatalf("空目标是合法的 Wiki 链接（目标为空）：%q %v", got, ok)
	}
}

func TestExtractLinks(t *testing.T) {
	body := "intro [[Alpha]] mid [[Beta|b]] again [[Alpha]] [[ |skip]] [[ ]] end"
	got := ExtractLinks(body)
	want := []Link{{"Alpha", "Alpha"}, {"Beta", "b"}, {"Alpha", "Alpha"}}
	if !slices.Equal(got, want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
	if got := ExtractLinks("nothing here"); len(got) != 0 {
		t.Fatalf("没有链接：%+v", got)
	}
}

func aliasJSON(s string) *string { return &s }

func linkCatalog() []services.WikiCatalogEntry {
	return []services.WikiCatalogEntry{
		{ID: "p1", Title: "Alpha Topic", NormalizedTitle: "ALPHA TOPIC", AliasesJSON: aliasJSON(`["First","Beta Topic"]`)},
		{ID: "p2", Title: "Beta Topic", NormalizedTitle: "BETA TOPIC"},
		{ID: "p3", Title: "概念", NormalizedTitle: "概念", AliasesJSON: aliasJSON(`["Concept"]`)},
	}
}

func TestResolveTarget(t *testing.T) {
	cat := linkCatalog()
	cases := []struct {
		target string
		id     string
		ok     bool
	}{
		{"Alpha Topic", "p1", true},
		{"alpha   topic", "p1", true}, // 大小写与连续空白不影响
		{"  ALPHA TOPIC ", "p1", true},
		{"First", "p1", true}, // 别名
		{"concept", "p3", true},
		{"概念", "p3", true},
		{"Beta Topic", "p2", true}, // 标题优先于别名（p1 的别名里也有 Beta Topic）
		{"Missing", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		id, ok := ResolveTarget(cat, c.target)
		if id != c.id || ok != c.ok {
			t.Errorf("ResolveTarget(%q)=(%q,%v) want (%q,%v)", c.target, id, ok, c.id, c.ok)
		}
	}
	if _, ok := ResolveTarget(nil, "x"); ok {
		t.Fatal("空目录什么也解析不到")
	}
}

func TestLinkTargets(t *testing.T) {
	cat := linkCatalog()
	body := "see [[Alpha Topic]] and [[alpha topic|again]] and [[Concept]] and [[Nope]] and [[First]]"
	got := LinkTargets(body, cat)
	want := []string{"ALPHA TOPIC", "概念"}
	if !slices.Equal(got, want) {
		t.Fatalf("目标应去重并按规范化标题给出：got %v want %v", got, want)
	}
	if got := LinkTargets("no links", cat); got == nil || len(got) != 0 {
		t.Fatalf("没有链接应是空切片：%#v", got)
	}
	if got := LinkTargets("[[x]]", nil); len(got) != 0 {
		t.Fatalf("空目录解析不到任何目标：%v", got)
	}
}
