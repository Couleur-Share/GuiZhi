package aiflow

// Wiki 页面草稿的解析与清洗（对应 services/knowledge-ai/wiki-compile.ts，
// 加上 shared/utils/wiki-body.ts 的 stripDuplicateTitleHeading）：
// 从模型输出提取页面 JSON、净化字段、按标题与别名解析 [[链接]]。全文编译器、问答与页面手动编辑共用。

import (
	"errors"
	"regexp"
	"strings"
)

const (
	wikiMaxPagesPerItem  = 4
	wikiMaxAliasCount    = 5
	wikiMaxAliasLength   = 60
	wikiTitleMaxLength   = 120
	wikiSummaryMaxLength = 300
	wikiBodyMaxLength    = 8000
)

// reWikiLink 对应 /\[\[([^[\]]+)\]\]/g。
var reWikiLink = regexp.MustCompile(`\[\[([^\[\]]+)\]\]`)

// NormalizeWikiTitle 对应 normalizeWikiTitle：去首尾空白、内部连续空白折叠、不区分大小写
// （[[链接]] 锚点与唯一性判定）。大小写走完整的 Unicode 映射（ß → SS），与 JS 的 toUpperCase 一致。
func NormalizeWikiTitle(title string) string {
	return jsUpper(strings.Join(jsFields(title), " "))
}

// ParseAliases 对应 parseAliases：别名 JSON 数组里的字符串；空值、非法 JSON、非数组都按空处理。
func ParseAliases(aliasesJSON *string) []string {
	out := []string{}
	if aliasesJSON == nil || *aliasesJSON == "" {
		return out
	}
	parsed, err := parseJSON(*aliasesJSON)
	if err != nil {
		return out
	}
	list, ok := parsed.([]any)
	if !ok {
		return out
	}
	for _, v := range list {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// ParseWikiResponse 对应 parseWikiResponse：从模型输出提取页面 JSON（容忍代码块围栏与前后缀文字）。
// 返回的元素保持 JSON 解码后的原样（对象为 map[string]any，其余类型交给 SanitizePages 判断）；
// ok=false 对应 TS 的 null。空数组 {"pages":[]} 是合法结果（ok=true），不能与 null 混同。
func ParseWikiResponse(text string) (pages []any, ok bool) {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return nil, false
	}
	parsed, err := parseJSON(text[start : end+1])
	if err != nil {
		return nil, false
	}
	root, isObject := parsed.(map[string]any)
	if !isObject {
		return nil, false
	}
	list, isArray := root["pages"].([]any)
	if !isArray {
		return nil, false
	}
	return list, true
}

func wikiParseKind(kind any) WikiPageKind {
	normalized := ""
	if s, ok := kind.(string); ok {
		normalized = jsLower(jsTrim(s))
	}
	switch normalized {
	case "entity":
		return WikiKindEntity
	case "concept":
		return WikiKindConcept
	}
	return WikiKindTopic
}

// wikiFirstLine 对应 firstLine：非字符串按空串；取首个换行之前的部分并去空白。
func wikiFirstLine(text any) string {
	trimmed := ""
	if s, ok := text.(string); ok {
		trimmed = jsTrim(s)
	}
	if i := strings.Index(trimmed, "\n"); i >= 0 {
		return jsTrim(trimmed[:i])
	}
	return trimmed
}

// wikiTrimmedString 对应 `typeof x === "string" ? x.trim() : ""`。
func wikiTrimmedString(v any) string {
	if s, ok := v.(string); ok {
		return jsTrim(s)
	}
	return ""
}

// errWikiNullPage 复现 V8 对 null 元素读属性的报错原文：这条文案会作为分块失败原因展示给用户。
var errWikiNullPage = errors.New("Cannot read properties of null (reading 'title')")

// SanitizePages 对应 sanitizePages：净化模型输出——字段截断、类别解析、批内按规范化标题去重，上限 4 页。
// pages 里出现 null 元素时 TS 会在读 .title 处抛 TypeError，这里返回同文案的错误；
// 数字、字符串、数组等其他非对象元素读出的字段都是 undefined，按空标题跳过。
func SanitizePages(pages []any) ([]WikiPageDraft, error) {
	drafts := []WikiPageDraft{}
	seen := map[string]bool{}
	for _, raw := range pages {
		if len(drafts) >= wikiMaxPagesPerItem {
			break
		}
		if raw == nil {
			return nil, errWikiNullPage
		}
		dto, _ := raw.(map[string]any) // 非对象：所有字段都按 undefined（nil map 读取安全）

		title := TruncatePlain(wikiTrimmedString(dto["title"]), wikiTitleMaxLength)
		if title == "" {
			continue
		}
		normalized := NormalizeWikiTitle(title)
		if seen[normalized] {
			continue
		}
		seen[normalized] = true

		// 详情页头部已经渲染了 title，模型还常在正文首行放一个同名一级标题，落库前剥掉
		body := StripDuplicateTitleHeading(TruncatePlain(wikiTrimmedString(dto["body"]), wikiBodyMaxLength), title)
		if body == "" {
			continue
		}

		aliases := []string{}
		if list, ok := dto["aliases"].([]any); ok {
			for _, v := range list {
				s, isString := v.(string)
				if !isString {
					continue
				}
				alias := jsTrim(s)
				if alias == "" || u16Len(alias) > wikiMaxAliasLength || NormalizeWikiTitle(alias) == normalized {
					continue
				}
				duplicated := false
				for _, existing := range aliases {
					if existing == alias {
						duplicated = true
						break
					}
				}
				if duplicated {
					continue
				}
				aliases = append(aliases, alias)
			}
		}
		if len(aliases) > wikiMaxAliasCount {
			aliases = aliases[:wikiMaxAliasCount]
		}

		var aliasesJSON *string
		if len(aliases) > 0 {
			text := jsonText(aliases)
			aliasesJSON = &text
		}
		drafts = append(drafts, WikiPageDraft{
			Title:           title,
			NormalizedTitle: normalized,
			Kind:            wikiParseKind(dto["kind"]),
			Summary:         TruncatePlain(wikiFirstLine(dto["summary"]), wikiSummaryMaxLength),
			Body:            body,
			AliasesJSON:     aliasesJSON,
		})
	}
	return drafts, nil
}

// BuildLinkResolver 对应 buildLinkResolver：规范化的标题/别名 → 目标页规范显示标题。
// 标题优先于别名；冲突先到先得（批内页先于目录页——本批是最新状态）。
func BuildLinkResolver(catalog []WikiCatalogEntry, batch []WikiPageDraft) map[string]string {
	resolver := map[string]string{}
	for _, draft := range batch {
		resolver[draft.NormalizedTitle] = draft.Title
	}
	for _, entry := range catalog {
		if _, has := resolver[entry.NormalizedTitle]; !has {
			resolver[entry.NormalizedTitle] = entry.Title
		}
	}
	for _, draft := range batch {
		for _, alias := range ParseAliases(draft.AliasesJSON) {
			key := NormalizeWikiTitle(alias)
			if _, has := resolver[key]; !has {
				resolver[key] = draft.Title
			}
		}
	}
	for _, entry := range catalog {
		for _, alias := range ParseAliases(entry.AliasesJSON) {
			key := NormalizeWikiTitle(alias)
			if _, has := resolver[key]; !has {
				resolver[key] = entry.Title
			}
		}
	}
	return resolver
}

// CleanWikiLinks 对应 cleanWikiLinks：清洗正文里的 [[链接]]——能按标题/别名解析的重写为 [[目标页标题]]
// 或 [[目标页标题|显示文字]]，解析失败的去掉括号降级为纯文本；返回清洗后的正文与去重的目标集合（规范化标题）。
func CleanWikiLinks(body string, resolver map[string]string) (cleaned string, targets []string) {
	targets = []string{}
	cleaned = reWikiLink.ReplaceAllStringFunc(body, func(match string) string {
		inner := reWikiLink.FindStringSubmatch(match)[1]
		var target, display string
		if i := strings.Index(inner, "|"); i >= 0 {
			target = jsTrim(inner[:i])
			display = jsTrim(inner[i+1:])
		} else {
			target = jsTrim(inner)
			display = target
		}
		if target == "" {
			return display
		}
		canonicalTitle := resolver[NormalizeWikiTitle(target)]
		if canonicalTitle == "" { // JS 里空串与 undefined 同为假值
			return display
		}
		canonicalNormalized := NormalizeWikiTitle(canonicalTitle)
		seen := false
		for _, t := range targets {
			if t == canonicalNormalized {
				seen = true
				break
			}
		}
		if !seen {
			targets = append(targets, canonicalNormalized)
		}
		if display == "" || display == canonicalTitle {
			return "[[" + canonicalTitle + "]]"
		}
		return "[[" + canonicalTitle + "|" + display + "]]"
	})
	return cleaned, targets
}

// WikiKindLabel 对应 wikiKindLabel。
func WikiKindLabel(kind WikiPageKind) string {
	switch kind {
	case WikiKindEntity:
		return "实体"
	case WikiKindConcept:
		return "概念"
	}
	return "主题"
}

// ---- wiki-body.ts：正文开头与页面标题重复的标题行 ----

// reHeadingLine 对应 /^(#{1,6})\s+(.*)$/：\s 是 JS 的空白集合，. 不含 \n \r U+2028 U+2029。
var reHeadingLine = regexp.MustCompile(`^(#{1,6})[` + jsSpaceClass + `]+([^\n\r\x{2028}\x{2029}]*)$`)

var reLineBreak = regexp.MustCompile(`\r?\n`)

var headingPunctuation = strings.NewReplacer("（", "(", "）", ")", "【", "[", "】", "]", "*", "", "_", "", "`", "")

// normalizeHeadingText 对应 normalizeHeadingText：标题比对用的规范化——忽略大小写、强调符、全角/半角括号差异。
// 空白整个去掉而不是折叠：中英混排时半角括号前后习惯留空格，全角括号则不留，折叠的话两种写法仍不相等。
func normalizeHeadingText(text string) string {
	return jsUpper(jsRemoveSpaces(headingPunctuation.Replace(text)))
}

// StripDuplicateTitleHeading 对应 stripDuplicateTitleHeading：剥掉正文开头与页面标题重复的标题行。
// 只处理正文的第一个非空行，且只在它是标题、文字与页面标题一致时才剥——正文中间的同名小标题是作者的结构选择。
func StripDuplicateTitleHeading(body, title string) string {
	wanted := normalizeHeadingText(title)
	if wanted == "" {
		return body
	}
	lines := reLineBreak.Split(body, -1)
	firstIndex := -1
	for i, line := range lines {
		if jsTrim(line) != "" {
			firstIndex = i
			break
		}
	}
	if firstIndex < 0 {
		return body
	}
	match := reHeadingLine.FindStringSubmatch(jsTrim(lines[firstIndex]))
	if match == nil || normalizeHeadingText(match[2]) != wanted {
		return body
	}
	// 连同紧随其后的空行一起去掉，避免正文顶部留一段空白
	next := firstIndex + 1
	for next < len(lines) && jsTrim(lines[next]) == "" {
		next++
	}
	return strings.Join(lines[next:], "\n")
}
