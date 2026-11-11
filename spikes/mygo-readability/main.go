// 正文提取等价性对拍：Go 的 readeck/go-readability v2（对标 Readability.js 0.6.0）
// 对缓存的真实网页提取正文，与 JS（@mozilla/readability 0.6.0 + linkedom）逐页比较。
// 用法：go run . <样本目录> <golden 目录> [mode]    mode 默认 stock
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	readability "codeberg.org/readeck/go-readability/v2"
)

type golden struct {
	ID      string `json:"id"`
	Variant string `json:"variant"`
	Mode    string `json:"mode"`
	URL     string `json:"url"`
	Result  *struct {
		Title string `json:"title"`
		Text  string `json:"text"`
		Error string `json:"error"`
	} `json:"result"`
}

// jsSpace 是 JavaScript 正则 \s 的字符集（与 Go 的 unicode.IsSpace 不同：JS 含 U+FEFF，不含 U+0085）。
func jsSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xA0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}

func squash(s string) []rune {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if !jsSpace(r) {
			out = append(out, r)
		}
	}
	return out
}

// f1 是两段文本去空白后按 5 个字符滑窗的多重集合重合度（1 表示逐字相同）。
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
	var total, overlap int
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
	id, variant string
	f1          float64
	jsLen       int
	goLen       int
	note        string
}

func main() {
	if len(os.Args) < 3 {
		fmt.Println("用法：go run . <样本目录> <golden 目录> [mode]")
		os.Exit(2)
	}
	evalDir, goldDir := os.Args[1], os.Args[2]
	mode := "stock"
	if len(os.Args) > 3 {
		mode = os.Args[3]
	}
	raw, err := os.ReadFile(filepath.Join(goldDir, "golden.json"))
	if err != nil {
		panic(err)
	}
	var all []golden
	if err := json.Unmarshal(raw, &all); err != nil {
		panic(err)
	}

	// 产品按静态/渲染分流决定每个样本实际提取哪份 HTML（routes.json 由 route-check.mts 生成）；只比较实际使用的那份。
	routes := map[string]string{}
	if b, err := os.ReadFile(filepath.Join(goldDir, "routes.json")); err == nil {
		json.Unmarshal(b, &routes)
	}
	var rows []row
	titleEq, titleN := 0, 0
	for _, g := range all {
		if g.Mode != mode || (len(routes) > 0 && routes[g.ID] != g.Variant) {
			continue
		}
		html, err := os.ReadFile(filepath.Join(evalDir, "cache", g.ID, g.Variant+".html"))
		if err != nil {
			panic(err)
		}
		src := string(html)
		if !strings.Contains(strings.ToLower(src), "<html") { // 与 TS 的 parsePage 一致
			src = "<html><body>" + src + "</body></html>"
		}
		u, _ := url.Parse(g.URL)
		p := readability.NewParser()
		p.CharThresholds = 100
		if mode == "product" {
			applyProductOptions(&p)
		}
		art, perr := p.Parse(strings.NewReader(src), u)

		var goText, goTitle string
		goNull := perr != nil || art.Node == nil
		if !goNull {
			var buf bytes.Buffer
			art.RenderText(&buf)
			goText, goTitle = buf.String(), art.Title()
		}
		r := row{id: g.ID, variant: g.Variant}
		switch {
		case g.Result == nil && goNull:
			r.f1, r.note = 1, "双方都无结果"
		case g.Result == nil || goNull:
			r.f1, r.note = 0, fmt.Sprintf("只有一方有结果（js 有=%v go 有=%v）", g.Result != nil, !goNull)
		default:
			a, b := squash(g.Result.Text), squash(goText)
			if want := os.Getenv("DEBUG_ID"); want != "" && strings.HasPrefix(g.ID, want) {
				showDiff(a, b)
			}
			r.f1, r.jsLen, r.goLen = f1(a, b), len(a), len(b)
			titleN++
			if g.Result.Title == goTitle {
				titleEq++
			}
		}
		rows = append(rows, r)
	}

	exact, ge99, ge95, ge80, lt80 := 0, 0, 0, 0, 0
	for _, r := range rows {
		switch {
		case r.f1 == 1:
			exact++
		case r.f1 >= 0.99:
			ge99++
		case r.f1 >= 0.95:
			ge95++
		case r.f1 >= 0.80:
			ge80++
		default:
			lt80++
		}
	}
	fmt.Printf("模式 %s，共 %d 份输入\n", mode, len(rows))
	fmt.Printf("  逐字相同（去空白后）：%d\n  相似度 ≥0.99：%d\n  0.95–0.99：%d\n  0.80–0.95：%d\n  <0.80：%d\n", exact, ge99, ge95, ge80, lt80)
	fmt.Printf("  标题完全一致：%d/%d\n", titleEq, titleN)
	sort.Slice(rows, func(i, j int) bool { return rows[i].f1 < rows[j].f1 })
	fmt.Println("差异最大的 12 份：")
	for i := 0; i < len(rows) && i < 12 && rows[i].f1 < 1; i++ {
		r := rows[i]
		fmt.Printf("  %.3f  js=%6d go=%6d  %-8s %s %s\n", r.f1, r.jsLen, r.goLen, r.variant, r.id, r.note)
	}
}

// showDiff 打印两段去空白文本第一处与最后一处分歧的上下文，用来判断差异成因。
func showDiff(a, b []rune) {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	ja, jb := len(a), len(b)
	for ja > i && jb > i && a[ja-1] == b[jb-1] {
		ja--
		jb--
	}
	clip := func(x []rune, from, to int) string {
		if from < 0 {
			from = 0
		}
		if to > len(x) {
			to = len(x)
		}
		if to-from > 220 {
			return string(x[from:from+110]) + " … " + string(x[to-110:to])
		}
		return string(x[from:to])
	}
	fmt.Printf("  首个分歧位置 %d；JS 独有 %d 字，Go 独有 %d 字\n  JS 独有：%s\n  Go 独有：%s\n", i, ja-i, jb-i, clip(a, i, ja), clip(b, i, jb))
}