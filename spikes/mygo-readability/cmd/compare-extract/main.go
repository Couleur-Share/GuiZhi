// 整条提取链路对拍：Go 移植的 ExtractWebArticle 与 TS 的 extractWebArticle 在 59 个真实网页上比较。
// 用法：go run -tags patched "-modfile=go.patched.mod" ./cmd/compare-extract <样本目录> <golden 目录>
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"guizhi-mygo-readability/extract"
)

type golden struct {
	ID               string `json:"id"`
	Variant          string `json:"variant"`
	URL              string `json:"url"`
	Status           int    `json:"status"`
	Source           string `json:"source"`
	ReadabilityChars int    `json:"readabilityChars"`
	RegionChars      int    `json:"regionChars"`
	Title            string `json:"title"`
	Markdown         string `json:"markdown"`
	Error            string `json:"error"`
}

func squash(s string) []rune {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if !unicode.IsSpace(r) && r != 0xFEFF {
			out = append(out, r)
		}
	}
	return out
}

// textOnly 去掉 Markdown 语法字符，只留文字，用来区分「选错了内容」与「只是语法写法不同」。
func textOnly(s string) []rune {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if unicode.IsSpace(r) || r == 0xFEFF {
			continue
		}
		switch r {
		case '*', '_', '`', '#', '>', '-', '\\', '[', ']', '(', ')', '|', '!', '~', '+':
			continue
		}
		out = append(out, r)
	}
	return out
}

func f1(a, b []rune) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1
	}
	const k = 5
	grams := func(x []rune) map[string]int {
		m := map[string]int{}
		for i := 0; i+k <= len(x); i++ {
			m[string(x[i:i+k])]++
		}
		if len(x) < k && len(x) > 0 {
			m[string(x)]++
		}
		return m
	}
	ga, gb := grams(a), grams(b)
	total, overlap := 0, 0
	for _, c := range ga {
		total += c
	}
	for g, c := range gb {
		total += c
		if d := ga[g]; d < c {
			overlap += d
		} else {
			overlap += c
		}
	}
	if total == 0 {
		return 1
	}
	return 2 * float64(overlap) / float64(total)
}

type row struct {
	id                 string
	srcOK, charsOK     bool
	titleOK            bool
	exact              bool
	fMD, fText         float64
	jsSrc, goSrc       string
	jsRead, goRead     int
	jsRegion, goRegion int
}

func main() {
	evalDir, goldDir := os.Args[1], os.Args[2]
	raw, err := os.ReadFile(filepath.Join(goldDir, "article-golden.json"))
	if err != nil {
		panic(err)
	}
	var all []golden
	json.Unmarshal(raw, &all)
	var rows []row
	for _, g := range all {
		html, err := os.ReadFile(filepath.Join(evalDir, "cache", g.ID, g.Variant+".html"))
		if err != nil {
			panic(err)
		}
		a := extract.ExtractWebArticle(string(html), g.URL, g.Status)
		if want := os.Getenv("DEBUG_ID"); want != "" && strings.HasPrefix(g.ID, want) {
			os.WriteFile(filepath.Join(os.TempDir(), "extract-js.md"), []byte(g.Markdown), 0o644)
			os.WriteFile(filepath.Join(os.TempDir(), "extract-go.md"), []byte(a.Markdown), 0o644)
		}
		r := row{
			id: g.ID, srcOK: a.Source == g.Source, charsOK: a.ReadabilityChars == g.ReadabilityChars && a.RegionChars == g.RegionChars,
			titleOK: a.Title == g.Title, exact: a.Markdown == g.Markdown,
			fMD: f1(squash(g.Markdown), squash(a.Markdown)), fText: f1(textOnly(g.Markdown), textOnly(a.Markdown)),
			jsSrc: g.Source, goSrc: a.Source, jsRead: g.ReadabilityChars, goRead: a.ReadabilityChars, jsRegion: g.RegionChars, goRegion: a.RegionChars,
		}
		rows = append(rows, r)
	}
	n := len(rows)
	cnt := func(f func(row) bool) int {
		c := 0
		for _, r := range rows {
			if f(r) {
				c++
			}
		}
		return c
	}
	fmt.Printf("共 %d 页\n", n)
	fmt.Printf("正文来源一致：%d；readability/region 段落字数都一致：%d；标题一致：%d\n",
		cnt(func(r row) bool { return r.srcOK }), cnt(func(r row) bool { return r.charsOK }), cnt(func(r row) bool { return r.titleOK }))
	fmt.Printf("Markdown 逐字相同：%d\n", cnt(func(r row) bool { return r.exact }))
	bucket := func(get func(row) float64) string {
		a, b, c, d := 0, 0, 0, 0
		for _, r := range rows {
			switch v := get(r); {
			case v >= 0.99:
				a++
			case v >= 0.95:
				b++
			case v >= 0.80:
				c++
			default:
				d++
			}
		}
		return fmt.Sprintf("≥0.99:%d  0.95–0.99:%d  0.80–0.95:%d  <0.80:%d", a, b, c, d)
	}
	fmt.Printf("Markdown 去空白相似度：%s\n", bucket(func(r row) float64 { return r.fMD }))
	fmt.Printf("纯文字相似度（去掉 Markdown 符号）：%s\n", bucket(func(r row) float64 { return r.fText }))
	fmt.Println("段落字数不一致的页面：")
	for _, r := range rows {
		if !r.charsOK {
			fmt.Printf("  read=%d/%d region=%d/%d text=%.3f src=%s  %s\n", r.jsRead, r.goRead, r.jsRegion, r.goRegion, r.fText, r.jsSrc, r.id)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].fText < rows[j].fText })
	fmt.Println("纯文字相似度最低的 10 页：")
	for i := 0; i < len(rows) && i < 10; i++ {
		r := rows[i]
		fmt.Printf("  text=%.3f md=%.3f src(js/go)=%s/%s read=%d/%d region=%d/%d  %s\n", r.fText, r.fMD, r.jsSrc, r.goSrc, r.jsRead, r.goRead, r.jsRegion, r.goRegion, r.id)
	}
}
