package wiki

import (
	"net/url"
	"regexp"
	"strings"

	"guizhi-mygo-shell/aiflow"
	"guizhi-mygo-shell/native/services"
)

// HrefPrefix 是 [[链接]] 预处理后的 fragment 链接前缀（`#wiki=<编码目标>`）：渲染器遇到它按页内跳转处理，
// 其余 http(s) 链接才交给系统浏览器。
const HrefPrefix = "#wiki="

var reWikiLink = regexp.MustCompile(`\[\[([^\[\]]+)\]\]`)

// jsSpace 是 JS 的 \s / String.prototype.trim 空白集（含 U+00A0、全角空格、BOM；RE2 的 \s 只有 ASCII 空白）。
func jsSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

func jsTrim(s string) string { return strings.TrimFunc(s, jsSpace) }

// encodeURIComponent 与 JS 同名函数一致：保留 A-Za-z0-9 与 - _ . ! ~ * ' ( )，其余按 UTF-8 逐字节百分号编码
// （url.QueryEscape 会把空格编成 +，PathEscape 又多放行了一批符号，都不等价）。
func encodeURIComponent(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '-', c == '_', c == '.', c == '!', c == '~', c == '*', c == '\'', c == '(', c == ')':
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&15])
		}
	}
	return b.String()
}

// PreprocessLinks 把 [[目标|显示]] / [[目标]] 改写成 Markdown 链接 `[显示](#wiki=<编码目标>)`，交给 Markdown 渲染。
// 目标为空的（如 [[ |x]]）只留显示文字。
func PreprocessLinks(body string) string {
	return reWikiLink.ReplaceAllStringFunc(body, func(match string) string {
		inner := reWikiLink.FindStringSubmatch(match)[1]
		target, display := splitLink(inner)
		if target == "" {
			return display
		}
		safe := strings.NewReplacer("[", `\[`, "]", `\]`).Replace(display)
		return "[" + safe + "](" + HrefPrefix + encodeURIComponent(target) + ")"
	})
}

// splitLink 拆开 [[ ]] 里的文本：`目标|显示`，没有 | 时显示等于目标；两侧都去空白。
func splitLink(inner string) (target, display string) {
	if i := strings.Index(inner, "|"); i >= 0 {
		return jsTrim(inner[:i]), jsTrim(inner[i+1:])
	}
	target = jsTrim(inner)
	return target, target
}

// Link 是正文里的一个 [[链接]]。
type Link struct {
	Target  string
	Display string
}

// ExtractLinks 按出现顺序列出正文里的全部 [[链接]]（含重复；目标为空的跳过）。
func ExtractLinks(body string) []Link {
	var out []Link
	for _, m := range reWikiLink.FindAllStringSubmatch(body, -1) {
		target, display := splitLink(m[1])
		if target != "" {
			out = append(out, Link{Target: target, Display: display})
		}
	}
	return out
}

// ParseHref 从预处理后的链接地址里还原 [[链接]] 的目标；不是 Wiki 链接，或百分号编码损坏（JS 里 decodeURIComponent 会抛），返回 false。
func ParseHref(href string) (target string, ok bool) {
	rest, found := strings.CutPrefix(href, HrefPrefix)
	if !found {
		return "", false
	}
	decoded, err := url.PathUnescape(rest)
	if err != nil {
		return "", false
	}
	return decoded, true
}

// ResolveTarget 按 [[链接]] 目标找页面：规范化标题精确匹配优先，其次别名。找不到返回 false。
func ResolveTarget(catalog []services.WikiCatalogEntry, target string) (pageID string, ok bool) {
	normalized := aiflow.NormalizeWikiTitle(target)
	for _, e := range catalog {
		if e.NormalizedTitle == normalized {
			return e.ID, true
		}
	}
	for _, e := range catalog {
		for _, alias := range aiflow.ParseAliases(e.AliasesJSON) {
			if aiflow.NormalizeWikiTitle(alias) == normalized {
				return e.ID, true
			}
		}
	}
	return "", false
}

func toFlowCatalog(catalog []services.WikiCatalogEntry) []aiflow.WikiCatalogEntry {
	out := make([]aiflow.WikiCatalogEntry, len(catalog))
	for i, e := range catalog {
		out[i] = aiflow.WikiCatalogEntry{
			ID: e.ID, Title: e.Title, NormalizedTitle: e.NormalizedTitle, Kind: aiflow.WikiPageKind(e.Kind), Summary: e.Summary,
			AliasesJSON: e.AliasesJSON, ManualEditedAt: e.ManualEditedAt, UpdatedAt: e.UpdatedAt,
		}
	}
	return out
}

// LinkTargets 解析正文里能对上现有页面的 [[链接]]，返回去重的目标页规范化标题——保存页面时交给后端重建这一页的出链，
// 否则图谱与反向链接会停留在改动之前。解析失败的链接不进结果（保存时正文原样写入，不改写）。
func LinkTargets(body string, catalog []services.WikiCatalogEntry) []string {
	_, targets := aiflow.CleanWikiLinks(body, aiflow.BuildLinkResolver(toFlowCatalog(catalog), nil))
	if targets == nil {
		return []string{}
	}
	return targets
}
