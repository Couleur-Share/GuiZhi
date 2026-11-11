// 对拍 cleanCss：Go 的 CleanCSS 与产品 cleanCss（css-tree）比较。
// 输入是 59 个真实网页里的全部 style 属性与 <style> 块，加 40 条攻击用例，各用两种 URL 映射。
// 比较前把两边输出规范化：去掉空白、url() 引号、统一引号。
// 用法：go run ./cmd/compare-css <golden 目录>
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"guizhi-mygo-readability/extract"
)

type item struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	CSS  string `json:"css"`
	Map  string `json:"map"`
	Out  string `json:"out"`
}

var (
	urlDQ = regexp.MustCompile(`url\("([^"]*)"\)`)
	urlSQ = regexp.MustCompile(`url\('([^']*)'\)`)
	ws    = regexp.MustCompile(`\s+`)
)

// 产品输出里会保留 /*! ... */ 注释，以及值为空的声明（如 vertical-align:;），这两类对渲染没有影响，比较时忽略。
var (
	cssComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
	emptyDecl  = regexp.MustCompile(`(^|[;{])[-a-zA-Z]+:(;|})`)
)

func norm(s string) string {
	s = cssComment.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "{;", "{")
	for emptyDecl.MatchString(s) {
		s = emptyDecl.ReplaceAllString(s, "$1$2")
	}
	s = urlSQ.ReplaceAllString(urlDQ.ReplaceAllString(s, "url($1)"), "url($1)")
	s = strings.ReplaceAll(s, "'", "\"")
	s = strings.ReplaceAll(s, "{;", "{")
	return ws.ReplaceAllString(s, "")
}

// declSet 把输出拆成「声明」集合，用来区分「丢了/多了哪些声明」。
func declSet(s string) map[string]bool {
	m := map[string]bool{}
	for _, part := range regexp.MustCompile(`[;{}]`).Split(s, -1) {
		if part != "" {
			m[part] = true
		}
	}
	return m
}

func main() {
	data, _ := os.ReadFile(filepath.Join(os.Args[1], "css-golden.json"))
	var items []item
	_ = json.Unmarshal(data, &items)
	const hash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	mappers := map[string]extract.CSSURLMapper{
		"none": func(string) (string, bool) { return "", false },
		"all":  func(string) (string, bool) { return "local-image://wechat-" + hash + ".png", true },
	}
	type stat struct{ n, same, goLess, goMore, both int }
	stats := map[string]*stat{}
	var diffs []string
	for _, it := range items {
		goOut := extract.CleanCSS(it.CSS, mappers[it.Map], it.Kind == "sheet")
		key := it.Kind
		if strings.HasPrefix(it.ID, "attack-") {
			key = "attack-" + it.Kind
		}
		st := stats[key]
		if st == nil {
			st = &stat{}
			stats[key] = st
		}
		st.n++
		a, b := norm(it.Out), norm(goOut)
		if a == b {
			st.same++
			continue
		}
		ts, gs := declSet(a), declSet(b)
		less, more := 0, 0
		for d := range ts {
			if !gs[d] {
				less++
			}
		}
		for d := range gs {
			if !ts[d] {
				more++
			}
		}
		switch {
		case less > 0 && more == 0:
			st.goLess++
		case more > 0 && less == 0:
			st.goMore++
		default:
			st.both++
		}
		i := 0
		for i < len(a) && i < len(b) && a[i] == b[i] {
			i++
		}
		lo := max(0, i-80)
		win := func(s string) string { return s[min(lo, len(s)):min(len(s), i+160)] }
		diffs = append(diffs, fmt.Sprintf("[%s] %s\n   ts: …%s\n   go: …%s", key, it.ID, win(a), win(b)))
	}
	keys := []string{}
	for k := range stats {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Printf("%-14s %7s %7s %12s %12s %10s\n", "类别", "总数", "一致", "Go 少声明", "Go 多声明", "两边都有差异")
	for _, k := range keys {
		s := stats[k]
		fmt.Printf("%-14s %7d %7d %12d %12d %10d\n", k, s.n, s.same, s.goLess, s.goMore, s.both)
	}
	limit := 40
	if v := os.Getenv("SHOW"); v != "" {
		fmt.Sscan(v, &limit)
	}
	fmt.Println("不一致示例：")
	for i, d := range diffs {
		if i >= limit {
			break
		}
		fmt.Println(d)
	}
}

func clip(s string) string {
	r := []rune(strings.ReplaceAll(s, "\n", "↵"))
	if len(r) > 240 {
		return string(r[:240]) + "…"
	}
	return string(r)
}
