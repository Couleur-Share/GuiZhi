package main

// NGA 抓取的纯文本部分，对应 services/import/nga.ts 与 nga-entities.ts：实体解码、lite=js 响应解析、Cookie 合并、
// BBCode → Markdown、回复上下文抽取。网络与线程组装在 forum_nga.go。
// 正则里的 \s 一律换成 JS 的空白集（fsp），\d 与 JS 一致（仅 ASCII）。

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

const (
	ngaOrigin           = "https://bbs.nga.cn"
	ngaUA               = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	ngaDefaultPageSize  = 20
	ngaImageLimit       = 80 // 全帖附件图入库上限；超出保留外链
	ngaSummaryMaxPages  = 12 // 为讨论总结采样的最大页数（含第 1 页）
	ngaOpMaxPages       = 40 // 楼主发言按 authorid 拉取时的页数上限
	ngaDefaultAttach    = "https://img.nga.cn/attachments"
	ngaReplySnippetMax  = 200
	ngaParentFetchMax   = 30 // 按 pid 补拉父楼的次数上限
	ngaCenterTitleLimit = 48
)

func ngaCanonicalURL(topicID string) string { return "https://bbs.nga.cn/read.php?tid=" + topicID }

// jsReplaceAll 对应 `s.replace(/re/g, (m, ...groups) => ...)`：fn 收到 [整体, 分组 1…]，未参与匹配的分组为 ""。
func jsReplaceAll(re *regexp.Regexp, s string, fn func(m []string) string) string {
	locs := re.FindAllStringSubmatchIndex(s, -1)
	if locs == nil {
		return s
	}
	var b strings.Builder
	last := 0
	for _, loc := range locs {
		b.WriteString(s[last:loc[0]])
		m := make([]string, len(loc)/2)
		for i := range m {
			if loc[2*i] >= 0 {
				m[i] = s[loc[2*i]:loc[2*i+1]]
			}
		}
		b.WriteString(fn(m))
		last = loc[1]
	}
	b.WriteString(s[last:])
	return b.String()
}

// ---- nga-entities.ts ----

var (
	reNumEntityDec = regexp.MustCompile(`&#(\d{1,7});`)
	reNumEntityHex = regexp.MustCompile(`(?i)&#x([0-9a-f]{1,6});`)
)

func ngaDecodeEntities(v string) string {
	// 顺序重要：先 &amp; 再其余，与 TS 逐个 replace 一致（&amp;lt; 会被连续解成 <）
	v = strings.ReplaceAll(v, "&amp;", "&")
	v = strings.ReplaceAll(v, "&lt;", "<")
	v = strings.ReplaceAll(v, "&gt;", ">")
	v = strings.ReplaceAll(v, "&quot;", `"`)
	v = strings.ReplaceAll(v, "&#39;", "'")
	v = strings.ReplaceAll(v, "&nbsp;", " ")
	return v
}

func ngaCodePointString(match string, code int64) string {
	if code > 0 && code <= 0x10ffff {
		return string(rune(code))
	}
	return match
}

func ngaDecodeNumericEntities(v string) string {
	v = jsReplaceAll(reNumEntityDec, v, func(m []string) string {
		n, _ := strconv.ParseInt(m[1], 10, 64)
		return ngaCodePointString(m[0], n)
	})
	return jsReplaceAll(reNumEntityHex, v, func(m []string) string {
		n, _ := strconv.ParseInt(m[1], 16, 64)
		return ngaCodePointString(m[0], n)
	})
}

// decodeNgaSubject 帖子标题在 NGA 数据里常被转义两次（&amp;amp;），按轮解码到不再变化；标题按纯文本展示。
func decodeNgaSubject(v string) string {
	cur := v
	for pass := 0; pass < 3; pass++ {
		next := ngaDecodeNumericEntities(ngaDecodeEntities(cur))
		if next == cur {
			break
		}
		cur = next
	}
	return cur
}

// ---- 平台解析错误（shared/utils/platform-parse-error.ts） ----

type platformParseError struct{ Code, msg string }

func (e *platformParseError) Error() string { return e.msg }

var reParseCodePrefix = regexp.MustCompile(`^\[([a-z_]+)\]` + fsp + `*`)

func newPlatformParseError(code, message string) error {
	body := jsTrim(reParseCodePrefix.ReplaceAllString(message, ""))
	return &platformParseError{Code: code, msg: "[" + code + "] " + body}
}

// ---- lite=js 响应 ----

var (
	reGuestJS     = regexp.MustCompile(`guestJs=([^;'"` + jsSpaceClass + `]+)`)
	reTrailSemi   = regexp.MustCompile(`;+` + fsp + `*$`)
	reHTMLTagAny  = regexp.MustCompile(`<[^>]+>`)
	reNgaAnyTag   = regexp.MustCompile(`(?i)\[/?[a-z0-9*]+(?:=[^\]]*)?\]`)
	reHTTPPrefix  = regexp.MustCompile(`(?i)^https?://`)
	reLeadDotSlsh = regexp.MustCompile(`^\./`)
	reLeadSlashes = regexp.MustCompile(`^/+`)
	reMDImage     = regexp.MustCompile(`!\[[^\]]*\]\(([^)]+)\)`)
)

// extractGuestJS 从挑战正文里抠 guestJs 值。
func extractGuestJS(text string) (string, bool) {
	m := reGuestJS.FindStringSubmatch(text)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// escapeControlCharsInJSONStrings：lite=js 不是严格 JSON，字符串字面量里常有未转义的控制字符；只处理字符串内部。
func escapeControlCharsInJSONStrings(source string) string {
	var out strings.Builder
	inString, escaped := false, false
	for _, ch := range source {
		if inString {
			switch {
			case escaped:
				out.WriteRune(ch)
				escaped = false
			case ch == '\\':
				out.WriteRune(ch)
				escaped = true
			case ch == '"':
				out.WriteRune(ch)
				inString = false
			case ch < 0x20:
				switch ch {
				case 8:
					out.WriteString(`\b`)
				case 9:
					out.WriteString(`\t`)
				case 10:
					out.WriteString(`\n`)
				case 12:
					out.WriteString(`\f`)
				case 13:
					out.WriteString(`\r`)
				default:
					out.WriteString(fmt.Sprintf(`\u%04x`, ch))
				}
			default:
				out.WriteRune(ch)
			}
			continue
		}
		if ch == '"' {
			inString = true
		}
		out.WriteRune(ch)
	}
	return out.String()
}

// parseNgaStore 剥掉 `window.script_muti_get_var_store=` 前缀并解析；失败返回 structure_missing。
func parseNgaStore(text string) (any, error) {
	const marker = "window.script_muti_get_var_store="
	jsonText := jsTrim(text)
	if i := strings.Index(text, marker); i >= 0 {
		jsonText = jsTrim(text[i+len(marker):])
	}
	trimmed := reTrailSemi.ReplaceAllString(jsonText, "")
	v, err := jsParse(escapeControlCharsInJSONStrings(trimmed))
	if err != nil {
		return nil, newPlatformParseError("structure_missing", "NGA 接口响应无法解析")
	}
	return v, nil
}

// mergeNgaCookies 把 Set-Cookie / guestJs 合并进 Cookie 头（保持首次出现的顺序）。
func mergeNgaCookies(existing string, setCookies []string, bodyText string) string {
	var keys []string
	jar := map[string]string{}
	put := func(k, v string) {
		if _, ok := jar[k]; !ok {
			keys = append(keys, k)
		}
		jar[k] = v
	}
	for _, part := range strings.Split(existing, ";") {
		t := jsTrim(part)
		if eq := strings.Index(t, "="); eq > 0 {
			put(t[:eq], t[eq+1:])
		}
	}
	for _, part := range setCookies {
		if eq := strings.Index(part, "="); eq > 0 {
			put(part[:eq], part[eq+1:])
		}
	}
	if g, ok := extractGuestJS(bodyText); ok {
		put("guestJs", g)
	}
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + jar[k]
	}
	return strings.Join(parts, "; ")
}

// ---- 附件与图片地址 ----

// resolveNgaImageURL 相对附件路径 / 绝对 URL → 可下载地址；空返回 ok=false。
func resolveNgaImageURL(raw, attachBase string) (string, bool) {
	value := jsTrim(raw)
	if value == "" {
		return "", false
	}
	if reHTTPPrefix.MatchString(value) {
		return value, true
	}
	rel := reLeadSlashes.ReplaceAllString(reLeadDotSlsh.ReplaceAllString(value, ""), "")
	if rel == "" {
		return "", false
	}
	return strings.TrimSuffix(attachBase, "/") + "/" + rel, true
}

// ---- BBCode → Markdown ----

var (
	reBr          = regexp.MustCompile(`(?i)<br` + fsp + `*/?>`)
	reCollapse    = regexp.MustCompile(`(?i)\[collapse(?:=([^\]]+))?\]([\s\S]*?)\[/collapse\]`)
	reColorBlock  = regexp.MustCompile(`(?i)\[color=([^\]]+)\]([\s\S]*?)\[/color\]`)
	reQuoteBlock  = regexp.MustCompile(`(?i)\[quote\]([\s\S]*?)\[/quote\]`)
	reListOpen    = regexp.MustCompile(`(?i)\[list\]`)
	reListClose   = regexp.MustCompile(`(?i)\[/list\]`)
	reListItem    = regexp.MustCompile(`(?i)\[\*\]`)
	reImgBlock    = regexp.MustCompile(`(?i)\[img\]([\s\S]*?)\[/img\]`)
	reURLEq       = regexp.MustCompile(`(?i)\[url=([^\]]+)\]([\s\S]*?)\[/url\]`)
	reURLPlain    = regexp.MustCompile(`(?i)\[url\]([\s\S]*?)\[/url\]`)
	reHBlock      = regexp.MustCompile(`(?i)\[h\]([\s\S]*?)\[/h\]`)
	reAlignCenter = regexp.MustCompile(`(?i)\[align=center\]([\s\S]*?)\[/align\]`)
	reSizeBold    = regexp.MustCompile(`(?i)\[size=(\d+)%\]` + fsp + `*\[b\]([\s\S]*?)\[/b\]` + fsp + `*\[/size\]`)
	reBoldSize    = regexp.MustCompile(`(?i)\[b\]` + fsp + `*\[size=(\d+)%\]([\s\S]*?)\[/size\]` + fsp + `*\[/b\]`)
	reSizeFontOpn = regexp.MustCompile(`(?i)\[(?:size|font)(?:=[^\]]*)?\]`)
	reSizeFontCls = regexp.MustCompile(`(?i)\[/(?:size|font)\]`)
	reBoldBlock   = regexp.MustCompile(`(?i)\[b\]([\s\S]*?)\[/b\]`)
	reItalicBlock = regexp.MustCompile(`(?i)\[i\]([\s\S]*?)\[/i\]`)
	reUnderBlock  = regexp.MustCompile(`(?i)\[u\]([\s\S]*?)\[/u\]`)
	reDelBlock    = regexp.MustCompile(`(?i)\[del\]([\s\S]*?)\[/del\]`)
	reStrikeBlock = regexp.MustCompile(`(?i)\[strikeout\]([\s\S]*?)\[/strikeout\]`)
	reLeftoverOpn = regexp.MustCompile(`(?i)\[(?:align|color|l)(?:=[^\]]*)?\]`)
	reLeftoverCls = regexp.MustCompile(`(?i)\[/(?:align|color|h|l|b|i|u|del|strikeout)\]`)
	reSmiley      = regexp.MustCompile(`(?i)\[s:[^\]]+\]`)
	reNewlinesN   = regexp.MustCompile(`\n+`)
	reInlineOpn   = regexp.MustCompile(`(?i)\[(?:size|color|font|b|i|u)(?:=[^\]]*)?\]`)
	reInlineCls   = regexp.MustCompile(`(?i)\[/(?:size|color|font|b|i|u)\]`)
	reListLead    = regexp.MustCompile(`^[-*+>]` + fsp)
	reLeadHTTP    = regexp.MustCompile(`(?i)^https?://`)
	rePidBlock    = regexp.MustCompile(`(?i)\[pid=[^\]]*\][\s\S]*?\[/pid\]`)
	rePostByBold  = regexp.MustCompile(`(?i)\[b\]` + fsp + `*Post by[\s\S]*?\[/b\]`)
	reReplyToRep  = regexp.MustCompile(`(?i)Reply to Reply Post by[^\n]*`)
	rePostByLine  = regexp.MustCompile(`(?i)Post by` + fsp + `*\[[^\]]*\][^\n]*`)
	reUIDTags     = regexp.MustCompile(`(?i)\[uid[^\]]*\]|\[/uid\]`)
	reTripleNL    = regexp.MustCompile(`\n{3,}`)
	reWSLead      = regexp.MustCompile(`^` + fsp + `+`)
	reWSTrail     = regexp.MustCompile(fsp + `+$`)
)

var forumColorClasses = map[string]string{
	"red": "forum-color-red", "blue": "forum-color-blue", "green": "forum-color-green", "orange": "forum-color-orange",
	"purple": "forum-color-purple", "gray": "forum-color-muted", "grey": "forum-color-muted", "silver": "forum-color-muted",
}

// forumColorClass 强调色白名单 class；泥潭偶发写 skyblue / royalblue 等，归到蓝。
func forumColorClass(raw string) string {
	key := strings.TrimPrefix(strings.ToLower(jsTrim(raw)), "#")
	if c, ok := forumColorClasses[key]; ok {
		return c
	}
	switch {
	case strings.Contains(key, "red") || key == "crimson":
		return "forum-color-red"
	case strings.Contains(key, "blue"):
		return "forum-color-blue"
	case strings.Contains(key, "green"):
		return "forum-color-green"
	}
	return ""
}

func escapeHTMLText(v string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(v)
}

// stripInlineBbcode 剥掉字号/颜色/粗斜体等，只留纯文本，用于判断是不是章节标题。
func stripInlineBbcode(raw string) string {
	s := reInlineOpn.ReplaceAllString(raw, "")
	s = reInlineCls.ReplaceAllString(s, "")
	return reBr.ReplaceAllString(s, "\n")
}

func collapseNewlinesTrim(s string) string { return jsTrim(reNewlinesN.ReplaceAllString(s, " ")) }

func formatSizedBold(sizePercent float64, body string) string {
	title := collapseNewlinesTrim(stripInlineBbcode(body))
	if title == "" {
		return ""
	}
	if !math.IsNaN(sizePercent) && sizePercent >= 120 && utf16Len(title) <= ngaCenterTitleLimit && !reListLead.MatchString(title) {
		return "\n\n## " + title + "\n\n"
	}
	return "<strong>" + title + "</strong>"
}

// extractCenteredHeading 居中块是否是「章节标题」：剥壳后只有一行、不太长、不像列表/链接。
func extractCenteredHeading(body string) (string, bool) {
	var plain []string
	for _, line := range strings.Split(stripInlineBbcode(body), "\n") {
		if t := jsTrim(line); t != "" {
			plain = append(plain, t)
		}
	}
	if len(plain) != 1 {
		return "", false
	}
	title := plain[0]
	if n := utf16Len(title); n == 0 || n > ngaCenterTitleLimit {
		return "", false
	}
	if reListLead.MatchString(title) || reLeadHTTP.MatchString(title) {
		return "", false
	}
	return title, true
}

// formatBoldMarkdown `[b]…[/b]` → `<strong>`（不用 `**`：CommonMark 在中文标点旁判不成对强调）。
func formatBoldMarkdown(body string) string {
	plain := stripInlineBbcode(body)
	trimmed := reWSTrail.ReplaceAllString(reWSLead.ReplaceAllString(plain, ""), "")
	if trimmed == "" {
		return ""
	}
	var lines []string
	for _, line := range strings.Split(trimmed, "\n") {
		if t := jsTrim(line); t != "" {
			lines = append(lines, t)
		}
	}
	switch {
	case len(lines) == 0:
		return ""
	case len(lines) == 1:
		return "<strong>" + lines[0] + "</strong>"
	case utf16Len(trimmed) > 200:
		return strings.Join(lines, "\n\n")
	}
	for i, l := range lines {
		lines[i] = "<strong>" + l + "</strong>"
	}
	return strings.Join(lines, "\n\n")
}

// stripReplyHeaderNoise 引用块里的 Reply / Post by 样板行，转引用时清掉。
func stripReplyHeaderNoise(quoteBody string) string {
	s := rePidBlock.ReplaceAllString(quoteBody, "")
	s = rePostByBold.ReplaceAllString(s, "")
	s = reReplyToRep.ReplaceAllString(s, "")
	s = rePostByLine.ReplaceAllString(s, "")
	return reUIDTags.ReplaceAllString(s, "")
}

// ngaBbcodeToMarkdown NGA BBCode → Markdown。只覆盖常见标签；未知标签剥壳留文本。
// imageMap 有命中时把 [img] 换成 local-image://，否则保留 https 外链。
func ngaBbcodeToMarkdown(raw, attachBase string, imageMap map[string]string) string {
	if attachBase == "" {
		attachBase = ngaDefaultAttach
	}
	text := strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(ngaDecodeEntities(raw))
	text = reBr.ReplaceAllString(text, "\n")

	text = jsReplaceAll(reCollapse, text, func(m []string) string {
		title := jsTrim(m[1])
		if title == "" {
			title = "展开"
		}
		return "\n\n<details>\n<summary>" + escapeHTMLText(title) + "</summary>\n\n" + jsTrim(m[2]) + "\n\n</details>\n\n"
	})
	text = jsReplaceAll(reColorBlock, text, func(m []string) string {
		cls := forumColorClass(m[1])
		inner := jsTrim(m[2])
		if inner == "" {
			return ""
		}
		if cls != "" {
			return `<span class="` + cls + `">` + inner + `</span>`
		}
		return inner
	})
	text = jsReplaceAll(reQuoteBlock, text, func(m []string) string {
		lines := strings.Split(jsTrim(stripReplyHeaderNoise(m[1])), "\n")
		for i, l := range lines {
			lines[i] = "> " + l
		}
		return strings.Join(lines, "\n")
	})
	text = reListOpen.ReplaceAllString(text, "\n")
	text = reListClose.ReplaceAllString(text, "\n")
	text = reListItem.ReplaceAllString(text, "\n- ")

	text = jsReplaceAll(reImgBlock, text, func(m []string) string {
		resolved, ok := resolveNgaImageURL(m[1], attachBase)
		if !ok {
			return ""
		}
		target := resolved
		if local, ok := imageMap[resolved]; ok {
			target = local
		}
		return "\n\n![图](" + target + ")\n\n"
	})
	text = jsReplaceAll(reURLEq, text, func(m []string) string {
		label := jsTrim(m[2])
		if label == "" {
			label = m[1]
		}
		return "[" + label + "](" + jsTrim(m[1]) + ")"
	})
	text = jsReplaceAll(reURLPlain, text, func(m []string) string {
		clean := jsTrim(m[1])
		return "[" + clean + "](" + clean + ")"
	})
	text = jsReplaceAll(reHBlock, text, func(m []string) string {
		if title := collapseNewlinesTrim(stripInlineBbcode(m[1])); title != "" {
			return "\n\n## " + title + "\n\n"
		}
		return "\n\n---\n\n"
	})
	text = jsReplaceAll(reAlignCenter, text, func(m []string) string {
		if title, ok := extractCenteredHeading(m[1]); ok {
			return "\n\n## " + title + "\n\n"
		}
		return "\n\n" + m[1] + "\n\n"
	})
	sizeNum := func(s string) float64 { n, _ := strconv.ParseFloat(s, 64); return n }
	text = jsReplaceAll(reSizeBold, text, func(m []string) string { return formatSizedBold(sizeNum(m[1]), m[2]) })
	text = jsReplaceAll(reBoldSize, text, func(m []string) string { return formatSizedBold(sizeNum(m[1]), m[2]) })

	text = reSizeFontOpn.ReplaceAllString(text, "")
	text = reSizeFontCls.ReplaceAllString(text, "")

	text = jsReplaceAll(reBoldBlock, text, func(m []string) string { return formatBoldMarkdown(m[1]) })
	text = jsReplaceAll(reItalicBlock, text, func(m []string) string {
		if inner := collapseNewlinesTrim(stripInlineBbcode(m[1])); inner != "" {
			return "<em>" + inner + "</em>"
		}
		return ""
	})
	text = jsReplaceAll(reUnderBlock, text, func(m []string) string { return jsTrim(stripInlineBbcode(m[1])) })
	strike := func(m []string) string {
		if inner := collapseNewlinesTrim(stripInlineBbcode(m[1])); inner != "" {
			return "~~" + inner + "~~"
		}
		return ""
	}
	text = jsReplaceAll(reDelBlock, text, strike)
	text = jsReplaceAll(reStrikeBlock, text, strike)

	text = reLeftoverOpn.ReplaceAllString(text, "")
	text = reLeftoverCls.ReplaceAllString(text, "")
	text = reSmiley.ReplaceAllString(text, "")
	text = reNgaAnyTag.ReplaceAllString(text, "")
	return jsTrim(reTripleNL.ReplaceAllString(text, "\n\n"))
}

// ---- 回复上下文 ----

type ngaReplyContext struct {
	HasReplyTo bool
	Author     string
	Snippet    string
	Pid        float64 // 0 表示没有
	HasPid     bool
	Content    string // 去掉首条「回复引用头」后的 BBCode
}

var (
	reReplyQuote    = regexp.MustCompile(`(?i)^(` + fsp + `*)\[quote\]([\s\S]*?)\[/quote\]`)
	reHasPidTag     = regexp.MustCompile(`(?i)\[pid=`)
	reHasPostBy     = regexp.MustCompile(`(?i)Post by`)
	reHasReplyReply = regexp.MustCompile(`(?i)Reply to Reply Post by`)
	rePidValue      = regexp.MustCompile(`(?i)\[pid=(\d+)(?:,[^\]]*)?\]`)
	reUIDName       = regexp.MustCompile(`(?i)Post by` + fsp + `*\[uid[^\]]*\]` + fsp + `*([^\[]+?)` + fsp + `*\[/uid\]`)
	rePlainNameA    = regexp.MustCompile(`(?i)Post by` + fsp + `+([^\n(#]+)`)
	rePlainNameB    = regexp.MustCompile(`(?i)Reply to Reply Post by` + fsp + `+([^\n(#]+)`)
)

// extractNgaReplyContext 从楼主回复原文抽出被回复楼的上下文；命中带 pid / Post by 的首条 [quote] 时剥掉它。
func extractNgaReplyContext(raw string) ngaReplyContext {
	text := ngaDecodeEntities(raw)
	loc := reReplyQuote.FindStringSubmatchIndex(text)
	if loc == nil {
		return ngaReplyContext{Content: text}
	}
	quoteBody := text[loc[4]:loc[5]]
	if !(reHasPidTag.MatchString(quoteBody) || reHasPostBy.MatchString(quoteBody) || reHasReplyReply.MatchString(quoteBody)) {
		return ngaReplyContext{Content: text}
	}
	var pid float64
	hasPid := false
	if m := rePidValue.FindStringSubmatch(quoteBody); m != nil {
		if n, err := strconv.ParseFloat(m[1], 64); err == nil && !math.IsInf(n, 0) {
			pid, hasPid = n, true
		}
	}
	author := ""
	if m := reUIDName.FindStringSubmatch(quoteBody); m != nil {
		author = jsTrim(m[1])
	} else if m := rePlainNameA.FindStringSubmatch(quoteBody); m != nil {
		author = jsTrim(m[1])
	} else if m := rePlainNameB.FindStringSubmatch(quoteBody); m != nil {
		author = jsTrim(m[1])
	}
	snippet := normalizeForumSnippet(reNgaAnyTag.ReplaceAllString(stripReplyHeaderNoise(quoteBody), ""), ngaReplySnippetMax)
	rest := reWSLead.ReplaceAllString(text[loc[1]:], "")
	if author == "" && snippet == "" && !hasPid {
		return ngaReplyContext{Content: text}
	}
	if author == "" {
		author = "某人"
	}
	return ngaReplyContext{HasReplyTo: true, Author: author, Snippet: snippet, Pid: pid, HasPid: hasPid, Content: rest}
}

func plainSnippetFromBbcode(raw string) string {
	return normalizeForumSnippet(reNgaAnyTag.ReplaceAllString(stripInlineBbcode(raw), ""), ngaReplySnippetMax)
}
