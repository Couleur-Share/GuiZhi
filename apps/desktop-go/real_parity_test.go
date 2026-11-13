package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"guizhi-mygo-shell/extract"
)

var (
	// html-to-markdown 把有序列表序号补零对齐（"01."），Turndown 不补（"1."）；两者渲染结果相同
	zeroPadOrdered = regexp.MustCompile(`(?m)^0+(\d+\.)`)
	// 无路径 URL：TS 经 URL 序列化补尾斜杠，Go 保持 HTML 原文
	hostOnlySlash = regexp.MustCompile(`(\]\(https?://[^/\s)]+)/(\)|\s)`)
)

// 内容层判据：去掉 Markdown 与标点，只比较字母/数字/汉字词元的顺序。相同 = 没丢字、没乱序，剩余只是写法差异。
var wordTokens = regexp.MustCompile(`[\p{L}\p{N}]+`)

var unescapeMD = regexp.MustCompile("\\\\([\\\\`*_{}\\[\\]()#+\\-.!~>|<])")

// 真实页面正文对拍（默认跳过）：GUIZHI_REAL_PARITY_DIR 指向 verify/real-parity.mts 的输出目录。
// 对同一份真实 HTML，比较 TS extractWebArticle 与 Go extract.ExtractWebArticle 的来源、标题与 Markdown。
// Markdown 与 Turndown 的字节差异按迁移文档的约定是已接受差异：这里先按字节比较，不等时再给出
// 「折叠空白后是否一致」「首个差异位置」，由人判断。
func TestRealPageParity(t *testing.T) {
	dir := os.Getenv("GUIZHI_REAL_PARITY_DIR")
	if dir == "" {
		t.Skip("未设置 GUIZHI_REAL_PARITY_DIR")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.ts.json"))
	if len(files) == 0 {
		t.Fatalf("%s 里没有 *.ts.json", dir)
	}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var ts struct {
			ID, URL, Source, Title, Markdown string
			Status, ReadabilityChars         int
			RegionChars                      int
			Error                            *string
		}
		if err := json.Unmarshal(raw, &ts); err != nil {
			t.Fatal(err)
		}
		t.Run(ts.ID, func(t *testing.T) {
			html, err := os.ReadFile(strings.TrimSuffix(f, ".ts.json") + ".html")
			if err != nil {
				t.Fatal(err)
			}
			a := extract.ExtractWebArticle(string(html), ts.URL, ts.Status)
			t.Logf("TS：来源 %s 标题 %q Markdown %d 字符 readability=%d region=%d", ts.Source, ts.Title, len([]rune(ts.Markdown)), ts.ReadabilityChars, ts.RegionChars)
			t.Logf("Go：来源 %s 标题 %q Markdown %d 字符 readability=%d region=%d", a.Source, a.Title, len([]rune(a.Markdown)), a.ReadabilityChars, a.RegionChars)
			if a.Source != ts.Source {
				t.Errorf("来源不一致：Go=%s TS=%s", a.Source, ts.Source)
			}
			if a.Title != ts.Title {
				t.Errorf("标题不一致：Go=%q TS=%q", a.Title, ts.Title)
			}
			if a.Markdown == ts.Markdown {
				t.Logf("Markdown 逐字节一致")
				return
			}
			fold := func(s string) string { return strings.Join(strings.Fields(s), " ") }
			// 已接受的字节差异（MYGO-MIGRATION.md「正文提取等价性」，2026-11-11 决定）：
			// Turndown 用反斜杠转义标记字符，html-to-markdown 不转义；html-to-markdown 把链接 URL 里的
			// 括号/方括号/空格百分号编码。归一化这两类后仍不一致才是新的差异。
			accepted := func(s string) string {
				s = unescapeMD.ReplaceAllString(s, "$1")
				s = zeroPadOrdered.ReplaceAllString(s, "$1")
				s = hostOnlySlash.ReplaceAllString(s, "$1$2")
				s = strings.NewReplacer("%28", "(", "%29", ")", "%5B", "[", "%5D", "]", "%20", " ", "%2F", "/").Replace(s)
				return fold(s)
			}
			if accepted(a.Markdown) == accepted(ts.Markdown) {
				t.Logf("Markdown 字节不同，但仅有已接受的差异（反斜杠转义、链接 URL 百分号编码）；归一化后一致")
				return
			}
			gt, tt := wordTokens.FindAllString(accepted(a.Markdown), -1), wordTokens.FindAllString(accepted(ts.Markdown), -1)
			k := 0
			for k < len(gt) && k < len(tt) && gt[k] == tt[k] {
				k++
			}
			tokensEqual := len(gt) == len(tt) && k == len(gt)
			t.Logf("内容层（词元序列）：Go %d 个 / TS %d 个，序列一致=%v", len(gt), len(tt), tokensEqual)
			if !tokensEqual {
				around := func(ts []string) string { return strings.Join(ts[max(0, k-6):min(len(ts), k+8)], " ") }
				t.Logf("首个词元分歧在第 %d 个：\n  Go：%s\n  TS：%s", k, around(gt), around(tt))
			}
			// 逐行比较归一化后的两份 Markdown，列出前 10 处差异的最小片段（去掉公共前后缀），便于分类
			gl, wl := strings.Split(a.Markdown, "\n"), strings.Split(ts.Markdown, "\n")
			var diffs []string
			for n := 0; n < len(gl) && n < len(wl) && len(diffs) < 10; n++ {
				ga, wa := accepted(gl[n]), accepted(wl[n])
				if ga == wa {
					continue
				}
				gr, wr := []rune(ga), []rune(wa)
				p := 0
				for p < len(gr) && p < len(wr) && gr[p] == wr[p] {
					p++
				}
				s := 0
				for s < len(gr)-p && s < len(wr)-p && gr[len(gr)-1-s] == wr[len(wr)-1-s] {
					s++
				}
				lo := max(0, p-12)
				head := func(l string) string { return string([]rune(l)[:min(48, len([]rune(l)))]) }
				diffs = append(diffs, "行 "+strconv.Itoa(n+1)+"：Go …"+string(gr[lo:len(gr)-s])+"…  |  TS …"+string(wr[lo:len(wr)-s])+"…\n      原行开头 Go="+strconv.Quote(head(gl[n]))+" TS="+strconv.Quote(head(wl[n])))
			}
			t.Logf("行数 Go=%d TS=%d；前 %d 处行内差异：\n  %s", len(gl), len(wl), len(diffs), strings.Join(diffs, "\n  "))
			if tokensEqual {
				t.Logf("内容层一致：文字与链接的词元序列逐一相同，余下仅为写法差异（斜体链接的嵌套顺序、页脚句号换行等），按已接受处理")
				return
			}
			g, w := []rune(accepted(a.Markdown)), []rune(accepted(ts.Markdown)) // 在归一化后的串上定位剩余差异
			i := 0
			for i < len(g) && i < len(w) && g[i] == w[i] {
				i++
			}
			ctx := func(r []rune) string {
				lo, hi := max(0, i-30), min(len(r), i+50)
				return strings.ReplaceAll(string(r[lo:hi]), "\n", "⏎")
			}
			t.Errorf("Markdown 不一致：首个差异在第 %d 个字符；折叠空白后一致=%v\n  Go：…%s\n  TS：…%s", i, fold(a.Markdown) == fold(ts.Markdown), ctx(g), ctx(w))
		})
	}
}
