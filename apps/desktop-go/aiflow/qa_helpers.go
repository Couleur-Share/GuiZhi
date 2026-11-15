package aiflow

// 全局问答里的纯函数（对应 qa.ts 的 tryParseAction / extractCitedOrdinals / locateNormalized /
// extractReadWindow / matchWikiPages / resolveBodyLinks）。
//
// 与 TS 对拍的要点：长度与下标按 UTF-16 码元，\s 与 trim 用 JS 的空白集，toLowerCase 走完整 Unicode 映射。

import (
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

type agentActionKind int

const (
	actionSearch agentActionKind = iota + 1
	actionRead
	actionAnswer
)

// agentAction 对应 AgentAction：模型输出的一个动作。
type agentAction struct {
	kind   agentActionKind
	query  string  // search
	target float64 // read：取整后的编号，用 float64 是因为模型可能给出任意大的数，报错文案要按 JS 的数字格式打印
	text   string  // answer
}

// tryParseAction 解析模型输出的动作 JSON：容忍围栏与前后缀文字（取第一个 { 到最后一个 }）；
// action 的写法容忍大小写与首尾空白（也容忍单元素数组，JS 的 String(["search"]) 就是 "search"）；
// read 的 target 容忍字符串（取其中的数字）。解析不出返回 ok=false。
func tryParseAction(text string) (action agentAction, ok bool) {
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return agentAction{}, false
	}
	root, err := parseJSON(text[start : end+1])
	if err != nil {
		return agentAction{}, false
	}
	record, isObject := root.(map[string]any)
	if !isObject {
		return agentAction{}, false
	}

	name := "" // String(record.action ?? "")：缺省与 null 都是空串
	if value := record["action"]; value != nil {
		name = jsLower(jsTrim(jsToString(value)))
	}
	switch name {
	case "search":
		query, _ := record["query"].(string)
		if query = jsTrim(query); query != "" {
			return agentAction{kind: actionSearch, query: query}, true
		}
	case "read":
		switch target := record["target"].(type) {
		case float64:
			if !math.IsInf(target, 0) && !math.IsNaN(target) {
				return agentAction{kind: actionRead, target: math.Trunc(target)}, true
			}
		case string:
			var digits strings.Builder
			for _, r := range target { // replace(/\D/g, "")
				if r >= '0' && r <= '9' {
					digits.WriteRune(r)
				}
			}
			if digits.Len() > 0 {
				number, _ := strconv.ParseFloat(digits.String(), 64) // parseInt(digits, 10)；位数过多时与 JS 一样溢出为 Infinity
				return agentAction{kind: actionRead, target: number}, true
			}
		}
	case "answer":
		if answer, isString := record["text"].(string); isString {
			return agentAction{kind: actionAnswer, text: answer}, true
		}
	}
	return agentAction{}, false
}

// reCitation 对应 /[[【]\s*(\d{1,3}(?:\s*[,，、]\s*\d{1,3})*)\s*[\]】]/g。
var reCitation = regexp.MustCompile(
	`[\[【][` + jsSpaceClass + `]*([0-9]{1,3}(?:[` + jsSpaceClass + `]*[,，、][` + jsSpaceClass + `]*[0-9]{1,3})*)[` + jsSpaceClass + `]*[\]】]`)

// extractCitedOrdinals 从回答文本提取被引用的资料编号：匹配 [1] / 【2】 及复合标注（[1、3]、[1,2]），
// 不在有效集合内的编号视为模型幻觉忽略。
func extractCitedOrdinals(answerText string, validOrdinals map[int]bool) map[int]bool {
	cited := map[int]bool{}
	for _, match := range reCitation.FindAllStringSubmatch(answerText, -1) {
		for _, token := range strings.FieldsFunc(match[1], func(r rune) bool { return r == ',' || r == '，' || r == '、' }) {
			if ordinal, err := strconv.Atoi(jsTrim(token)); err == nil && validOrdinals[ordinal] {
				cited[ordinal] = true
			}
		}
	}
	return cited
}

// locateNormalized 在空白归一后的原文里定位片段，返回它在原文中的起始下标（UTF-16 码元）；找不到返回 -1。
//
// 检索返回的片段是压过空白的，直接 indexOf 原文匹配不上。
func locateNormalized(haystack, needle string) int {
	return locateNormalizedUnits(utf16.Encode([]rune(haystack)), needle)
}

func locateNormalizedUnits(haystack []uint16, needle string) int {
	trimmedNeedle := utf16.Encode([]rune(jsTrim(jsCollapseSpaces(needle, " "))))
	if len(haystack) == 0 || len(trimmedNeedle) < 4 {
		return -1
	}

	// 空白连续段压成一个空格（开头的空白段整个丢掉），offsets 记每个保留码元在原文里的下标；
	// 空白段的下标取该段第一个空白字符的位置。
	normalized := make([]uint16, 0, len(haystack))
	offsets := make([]int, 0, len(haystack))
	inWhitespace := false
	for index, unit := range haystack {
		if jsSpace(rune(unit)) {
			if !inWhitespace && len(normalized) > 0 {
				normalized = append(normalized, ' ')
				offsets = append(offsets, index)
			}
			inWhitespace = true
			continue
		}
		inWhitespace = false
		normalized = append(normalized, unit)
		offsets = append(offsets, index)
	}

	for at := 0; at+len(trimmedNeedle) <= len(normalized); at++ {
		if unitsEqual(normalized[at:at+len(trimmedNeedle)], trimmedNeedle) {
			return offsets[at]
		}
	}
	return -1
}

func unitsEqual(a, b []uint16) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// extractReadWindow 取文档中包含命中片段的一段窗口；没有线索或定位失败时退回开头。
//
// 窗口不是以命中点居中，而是留三成前文——上文往往是这段的铺垫，
// 全砍掉会让模型看不懂命中的这几句在讲什么。
func extractReadWindow(text string, limit int, hint string) string {
	units := utf16.Encode([]rune(text))
	if len(units) <= limit {
		return text
	}
	hitIndex := -1
	if hint != "" {
		hitIndex = locateNormalizedUnits(units, hint)
	}
	if hitIndex < 0 {
		return TruncateText(text, limit)
	}

	lead := int(math.Floor(float64(limit) * 0.3))
	start := max(0, min(hitIndex-lead, len(units)-limit))
	end := min(len(units), start+limit)
	// 切口落在代理对中间时该处是 U+FFFD（TS 会留下孤立代理项，见 jsx.go 的说明）。
	body := string(utf16.Decode(units[start:end]))

	var b strings.Builder
	if start > 0 {
		b.WriteString("…（前文略）\n")
	}
	b.WriteString(body)
	if end < len(units) {
		b.WriteString("\n…（后文略）")
	}
	return b.String()
}

// matchWikiPages 做 Wiki 页面匹配：标题 / 别名 / 摘要与检索词双向包含计分（个人库规模内存匹配足够）。
func matchWikiPages(catalog []WikiCatalogEntry, query string) []WikiCatalogEntry {
	trimmed := jsTrim(query)
	if trimmed == "" {
		return nil
	}
	queryLower := jsLower(trimmed)
	tokens := jsFields(trimmed)

	type candidate struct {
		entry WikiCatalogEntry
		score int
	}
	var candidates []candidate
	for _, entry := range catalog {
		score := 0
		titleLower := jsLower(entry.Title)
		if strings.Contains(queryLower, titleLower) {
			score += 10
		}
		summaryLower := jsLower(entry.Summary)
		for _, token := range tokens {
			tokenLower := jsLower(token)
			if strings.Contains(titleLower, tokenLower) {
				score += 6
			}
			if strings.Contains(summaryLower, tokenLower) {
				score += 2
			}
		}
		for _, alias := range ParseAliases(entry.AliasesJSON) {
			aliasLower := jsLower(alias)
			if strings.Contains(queryLower, aliasLower) {
				score += 5
			}
			for _, token := range tokens {
				if strings.Contains(aliasLower, jsLower(token)) {
					score += 4
				}
			}
		}
		if score > 0 {
			candidates = append(candidates, candidate{entry, score})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })
	out := make([]WikiCatalogEntry, len(candidates))
	for i, c := range candidates {
		out[i] = c.entry
	}
	return out
}

// resolveBodyLinks 解析页面正文里的 [[链接]] 并按目录（标题）定位目标页；结果按首次出现的顺序、同一页只出现一次。
func resolveBodyLinks(body string, catalog []WikiCatalogEntry) []WikiCatalogEntry {
	byNormalized := map[string]int{} // 规范化标题 → 目录下标，同名取第一个
	for i, entry := range catalog {
		if _, seen := byNormalized[entry.NormalizedTitle]; !seen {
			byNormalized[entry.NormalizedTitle] = i
		}
	}

	var resolved []WikiCatalogEntry
	seen := map[int]bool{}
	for _, match := range reWikiLink.FindAllStringSubmatch(body, -1) {
		inner := match[1]
		if separator := strings.Index(inner, "|"); separator >= 0 {
			inner = inner[:separator]
		}
		target := jsTrim(inner)
		if target == "" {
			continue
		}
		if index, ok := byNormalized[NormalizeWikiTitle(target)]; ok && !seen[index] {
			seen[index] = true
			resolved = append(resolved, catalog[index])
		}
	}
	return resolved
}
