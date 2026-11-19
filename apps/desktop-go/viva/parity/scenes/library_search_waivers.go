package scenes

import (
	"strings"

	"guizhi-mygo-shell/viva/parity"
)

// 搜索命中词高亮（台账 B-041）的场景登记。
//
// 命中词把一段标题或摘要拆成几个文本节点（前后的零碎文字与 mark），对照里每个节点单独成一行文字。
// A-014 / A-026 的放过项原先按「整段标题 / 摘要」写 Subject，对这些片段不再命中，所以这里按查询词把命中条目的
// 标题与摘要切成与渲染层相同的片段，再对不含命中词的普通片段逐片段登记同样的放过项（范围与大小上限同整段的登记）。
// 命中词本身（mark 里的字）不登记：它是一个短词、不会折行，字偶距的后果远小于阈值，实测没有用到登记，
// 不留闲置的放过项（放宽一个用不到的上限，只会让以后命中词的宽度错误悄悄通过）。
//
// 表格的摘要是单行省略号，首个普通片段与整段摘要同头，已被 snippetKerningWaivers 覆盖，所以摘要片段只为卡片登记。
// 以后夹具里出现「摘要以命中词开头」之类的新情形，再按证据补登记。

// snippetWrapWidthCap 是卡片摘要折行点翻转的宽度上限：差一个折行单位（词或标点处的一段），实测「accent、」48.81px（12px）；
// 大字号档按字号放大。同一处更大的偏差照常判定。
const snippetWrapWidthCap float64 = 60

// markFragment 是一段文字按查询词切开之后的一个片段。
type markFragment struct {
	text string
	hit  bool // 是命中词本身（渲染层包进 <mark> 的那一段）
}

// markFragments 把 text 按 query 切成命中词与前后的零碎文字，规则同渲染层的 highlightText：
// 先去掉查询词两端的空白，不区分大小写，从左到右不重叠。没有命中返回整段（hit 为假）。
func markFragments(text, query string) []markFragment {
	query = strings.TrimSpace(query)
	lower, needle := strings.ToLower(text), strings.ToLower(query)
	if query == "" || len(lower) != len(text) || len(needle) != len(query) {
		return []markFragment{{text: text}} // 大小写转换改变了字节数（极少见）：不拆，宁可漏登记让对照报出来
	}
	var out []markFragment
	pos := 0
	for {
		i := strings.Index(lower[pos:], needle)
		if i < 0 {
			break
		}
		if i > 0 {
			out = append(out, markFragment{text: text[pos : pos+i]})
		}
		out = append(out, markFragment{text: text[pos+i : pos+i+len(needle)], hit: true})
		pos += i + len(needle)
	}
	if pos < len(text) {
		out = append(out, markFragment{text: text[pos:]})
	}
	if len(out) == 0 {
		return []markFragment{{text: text}}
	}
	return out
}

// matchedEntryTexts 返回「数据」夹具里查询词命中的条目的标题与摘要（命中就是标题或摘要含查询词，同渲染层与假服务的搜索）。
func matchedEntryTexts(query string) (titles, snippets []string) {
	fix, err := parity.LoadLibraryFixture()
	if err != nil {
		panic(err) // 夹具是编译进来的常量，解码失败是程序错误
	}
	needle := strings.ToLower(strings.TrimSpace(query))
	for _, e := range fix.Variant("data").Entries {
		if !strings.Contains(strings.ToLower(e.Title), needle) && !strings.Contains(strings.ToLower(e.Snippet), needle) {
			continue
		}
		titles = append(titles, e.Title)
		snippets = append(snippets, e.Snippet)
	}
	return titles, snippets
}

// searchTitleKerningWaivers 登记 A-026 在命中词拆开的标题片段上的后果：标题是 font-medium（500），
// 带拉丁字母的普通片段在两个引擎里宽度不同（Viva 对 500 字重做字偶距、Chromium 不做），范围与上限同整段标题的 semiboldKerningWaivers。
// 证据：table-search-hit-latin 在 1920×1080 的「ing Data-Intensive Applications 读书笔记」片段，Viva 比渲染层窄 1.01px（阈值 1.00）。
func searchTitleKerningWaivers(query string) []parity.Waiver {
	titles, _ := matchedEntryTexts(query)
	seen := map[string]bool{}
	var out []parity.Waiver
	for _, title := range titles {
		for _, frag := range markFragments(title, query) {
			if frag.hit || !hasLatinLetter(frag.text) || seen[frag.text] {
				continue
			}
			seen[frag.text] = true
			out = append(out,
				parity.Waiver{
					Ledger: "A-026", Layer: parity.LayerL2, Kind: "text-width", Subject: frag.text, MaxDelta: semiboldWidthCap,
					Note: "字偶距：Viva 对 500 字重做字偶距、Chromium 不做，带拉丁字母的标题片段偏窄（命中词把标题拆成片段，同 A-026 的整段标题）",
				},
				parity.Waiver{
					Ledger: "A-026", Layer: parity.LayerL2, Kind: "ink", Subject: frag.text, MaxDelta: semiboldInkCap,
					Note: "同上：宽度差累积成墨迹剖面的最佳对齐位移（只管横向，纵向错位照常判定）",
				})
		}
	}
	return out
}

// cardSnippetKerningWaivers 登记 A-014 在卡片摘要片段上的后果：摘要是 text-xs 400 字重，含拉丁字母时 Chromium 做字偶距、Viva 不做。
// 摘要折行，整行偏宽还会让折行点差一个词（后果见台账 A-014 的 2026-11-20 补充）：宽度上限放宽到一个折行单位，
// 墨迹总量比跟着偏（被挪到下一行的那个词改变了这一行的墨迹）。Subject 取片段的开头 10 个字符。
func cardSnippetKerningWaivers(query string) []parity.Waiver {
	_, snippets := matchedEntryTexts(query)
	seen := map[string]bool{}
	var out []parity.Waiver
	for _, snippet := range snippets {
		for _, frag := range markFragments(snippet, query) {
			head := headRunes(frag.text, 10)
			if frag.hit || !hasLatinLetter(frag.text) || seen[head] {
				continue
			}
			seen[head] = true
			out = append(out,
				parity.Waiver{
					Ledger: "A-014", Layer: parity.LayerL2, Kind: "text-width", Subject: head, MaxDelta: snippetWrapWidthCap,
					Note: "字偶距：Chromium 做、Viva 不做，含拉丁字母的摘要片段偏宽（行宽恰在折行边界上时，折行点差一个词）",
				},
				parity.Waiver{
					Ledger: "A-014", Layer: parity.LayerL2, Kind: "ink", Subject: head, MaxDelta: 2,
					Note: "同上：整行宽度差累积成墨迹剖面的最佳对齐位移（只管横向）",
				},
				parity.Waiver{
					Ledger: "A-014", Layer: parity.LayerL2, Kind: "ink-mass", Subject: head, MaxDelta: 0.3,
					Note: "折行点差一个词：这一行的墨迹比 React 少（或多）那个词的笔画",
				})
		}
	}
	return out
}

// tinyGlyphInkWaivers 登记 G-016：孤立的小字形（命中词后面的「.」）的墨迹总量比不稳定。
// 这类文字只有几个像素的笔画，墨迹总量由亚像素相位与相邻命中词底色的抗锯齿边缘决定：
// 「.」紧贴命中词的右缘，Latin 400 字重字偶距使它整体右移 0.12px，边缘覆盖的像素列多了一小条，墨迹总量比 2.19–2.40
// （只出在 list-search-hit-latin 的中档字号，12 个变体）。位置、宽度与 L3 像素照常比较（L3 不遮罩文字），不受影响。
//
// 被截断的行尾「…」曾在这里有一条登记（深色主题下墨迹总量比 1.16–1.17）；省略号改按渲染层实测的画法（宿主色、压在最后一枚命中词的
// 右内边距上，台账 B-041 的 R1–R4）之后，T0 / T1 / T2 里都没有再用到它，已删除——登记只收窄、不留闲置的放过项。
func tinyGlyphInkWaivers() []parity.Waiver {
	return []parity.Waiver{
		{
			Ledger: "G-016", Layer: parity.LayerL2, Kind: "ink-mass", Subject: "「.」", Dir: 1, MinDelta: 0.5, MaxDelta: 1.6,
			Note: "命中词后面孤立的「.」：笔画只有几个像素，紧贴命中词底色的抗锯齿边缘，墨迹总量比不稳定",
		},
	}
}
