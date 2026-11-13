package main

// 导入管线里的纯函数：对应 services/import/{url-normalize,source-identity,review-assessment}.ts、
// shared/utils/url-text.ts，以及 connectors.ts 里文本类连接器的标题规则。
// 全部由 make-golden.mts 里的 importUtil 基准逐例对拍。

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	whatwg "github.com/nlnwa/whatwg-url/url"
)

// ---- localeCompare（ICU 根排序，覆盖 ASCII；非 ASCII 退化为码点序并排在字母之后） ----

const icuASCIIOrder = "\t\n\v\f\r _-,;:!?.'\"()[]{}@*/\\&#%`^+<=>|~$0123456789"

var icuRank = func() map[rune]int {
	m := map[rune]int{}
	for i, r := range icuASCIIOrder {
		m[r] = i + 1
	}
	return m
}()

// icuPrimary 返回主排序权重：标点/符号 < 数字 < 字母（不分大小写）< 其它字符。
func icuPrimary(r rune) int {
	if v, ok := icuRank[r]; ok {
		return v
	}
	switch {
	case r >= 'a' && r <= 'z':
		return 100 + int(r-'a')
	case r >= 'A' && r <= 'Z':
		return 100 + int(r-'A')
	case r < 0x20 || r == 0x7f:
		return 0 // 控制字符在 ICU 里可忽略
	}
	return 1000 + int(r)
}

// localeCompare 对应 a.localeCompare(b)：先比主权重序列，再比大小写（小写在前）。
func localeCompare(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	var pa, pb []int
	for _, r := range ra {
		if p := icuPrimary(r); p != 0 {
			pa = append(pa, p)
		}
	}
	for _, r := range rb {
		if p := icuPrimary(r); p != 0 {
			pb = append(pb, p)
		}
	}
	for i := 0; i < len(pa) && i < len(pb); i++ {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1
			}
			return 1
		}
	}
	if len(pa) != len(pb) {
		if len(pa) < len(pb) {
			return -1
		}
		return 1
	}
	// 主权重相同：大小写（小写 < 大写）
	var ca, cb []rune
	for _, r := range ra {
		if icuPrimary(r) != 0 {
			ca = append(ca, r)
		}
	}
	for _, r := range rb {
		if icuPrimary(r) != 0 {
			cb = append(cb, r)
		}
	}
	for i := 0; i < len(ca) && i < len(cb); i++ {
		if ca[i] != cb[i] {
			upperA, upperB := ca[i] >= 'A' && ca[i] <= 'Z', cb[i] >= 'A' && cb[i] <= 'Z'
			if upperA != upperB {
				if upperB {
					return -1
				}
				return 1
			}
			if ca[i] < cb[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

// ---- URL 规范化 ----

var trackingParamRes = func() []*regexp.Regexp {
	var out []*regexp.Regexp
	for _, p := range []string{`^utm_`, `^fbclid$`, `^gclid$`, `^spm$`, `^share_source$`, `^share_medium$`, `^share_plat$`,
		`^share_session_id$`, `^share_tag$`, `^from_source$`, `^vd_source$`, `^ref$`, `^refer$`, `^source$`} {
		out = append(out, regexp.MustCompile(`(?i)`+p))
	}
	return out
}()

func isTrackingParam(name string) bool {
	for _, re := range trackingParamRes {
		if re.MatchString(name) {
			return true
		}
	}
	return false
}

var httpPrefixRe = regexp.MustCompile(`(?i)^https?://`)

func isHTTPURL(value string) bool {
	trimmed := jsTrim(value)
	if !httpPrefixRe.MatchString(trimmed) {
		return false
	}
	u, err := whatwg.Parse(trimmed)
	if err != nil {
		return false
	}
	return u.Protocol() == "http:" || u.Protocol() == "https:"
}

// normalizeURL 对应 normalizeUrl；无法规范化返回 ok=false（JS 的 null）。
func normalizeURL(raw string) (string, bool) {
	trimmed := jsTrim(raw)
	if !isHTTPURL(trimmed) {
		return "", false
	}
	u, err := whatwg.Parse(trimmed)
	if err != nil {
		return "", false
	}
	u.SetHash("")
	var kept [][2]string
	for _, p := range parseFormURLEncoded(strings.TrimPrefix(u.Search(), "?")) {
		if !isTrackingParam(p[0]) {
			kept = append(kept, p)
		}
	}
	sort.SliceStable(kept, func(i, j int) bool { return localeCompare(kept[i][0], kept[j][0]) < 0 })
	// parsed.search = ""：先清空；随后 append 才会按 x-www-form-urlencoded 重写查询串
	u.SetSearch("")
	if len(kept) > 0 {
		u.SetSearch(serializeFormURLEncoded(kept))
	}
	normalized := u.Href(false)
	if u.Pathname() != "/" && strings.HasSuffix(normalized, "/") && u.Search() == "" {
		normalized = normalized[:len(normalized)-1]
	}
	return normalized, true
}

// ---- 从文本里抠链接 ----

func isHTTPURLLike(value string) bool {
	trimmed := jsTrim(value)
	if trimmed == "" || strings.IndexFunc(trimmed, jsSpace) >= 0 || !httpPrefixRe.MatchString(trimmed) {
		return false
	}
	u, err := whatwg.Parse(trimmed)
	return err == nil && (u.Protocol() == "http:" || u.Protocol() == "https:")
}

const urlStopChars = "<>\"'`，。！？；：、（）【】《》「」『』…“”‘’"

func urlTailUnbalanced(url string) bool {
	last, _ := utf8.DecodeLastRuneInString(url)
	switch last {
	case ')':
		return strings.Count(url, ")") > strings.Count(url, "(")
	case ']':
		return strings.Count(url, "]") > strings.Count(url, "[")
	}
	return false
}

func trimURLTail(value string) string {
	url := value
	for url != "" {
		last, size := utf8.DecodeLastRuneInString(url)
		if !strings.ContainsRune(".,;:!?", last) && !urlTailUnbalanced(url) {
			break
		}
		url = url[:len(url)-size]
	}
	return url
}

// extractURLsFromText 对应 extractUrlsFromText：按出现顺序、去重。
func extractURLsFromText(value string) []string {
	var urls []string
	seen := map[string]bool{}
	for i := 0; i < len(value); {
		// 不区分大小写匹配 http:// 或 https://
		rest := value[i:]
		n := 0
		switch {
		case len(rest) >= 8 && strings.EqualFold(rest[:8], "https://"):
			n = 8
		case len(rest) >= 7 && strings.EqualFold(rest[:7], "http://"):
			n = 7
		}
		if n == 0 {
			i++
			continue
		}
		j := i + n
		for j < len(value) {
			r, size := utf8.DecodeRuneInString(value[j:])
			if jsSpace(r) || strings.ContainsRune(urlStopChars, r) {
				break
			}
			j += size
		}
		if j == i+n { // [^…]+ 至少一个字符
			i += n
			continue
		}
		url := trimURLTail(value[i:j])
		if isHTTPURLLike(url) && !seen[url] {
			seen[url] = true
			urls = append(urls, url)
		}
		i = j
	}
	return urls
}

// ---- 来源标识 ----

var (
	nodeseekPostRe = regexp.MustCompile(`^/post-([1-9]\d*)-[1-9]\d*/?$`)
	xhsHostRe      = regexp.MustCompile(`(^|\.)xiaohongshu\.com$`)
	xhsPathRe      = regexp.MustCompile(`(?i)/(?:explore|discovery/item)/([a-f\d]{24})`)
	douyinHostRe   = regexp.MustCompile(`(^|\.)douyin\.com$`)
	douyinPathRe   = regexp.MustCompile(`/(?:video|note|slides)/(\d{6,})`)
	digits6Re      = regexp.MustCompile(`^\d{6,}$`)
	bilibiliHostRe = regexp.MustCompile(`(^|\.)bilibili\.com$`)
	bilibiliPathRe = regexp.MustCompile(`(?i)/video/(BV[\da-z]+)`)
	youtubeHostRe  = regexp.MustCompile(`(^|\.)youtube\.com$`)
	youtubePathRe  = regexp.MustCompile(`/(?:shorts|live)/([^/]+)`)
	youtubeIDRe    = regexp.MustCompile(`^[\w-]{11}$`)
)

// searchParamGet 对应 url.searchParams.get(name)：第一个同名参数，没有则 ok=false。
func searchParamGet(u *whatwg.Url, name string) (string, bool) {
	for _, p := range parseFormURLEncoded(strings.TrimPrefix(u.Search(), "?")) {
		if p[0] == name {
			return p[1], true
		}
	}
	return "", false
}

// sourceIdentity 对应 sourceIdentity：同一内容的不同分享链接收敛到同一个标识；解析失败返回 ok=false。
func sourceIdentity(raw string) (string, bool) {
	url := raw
	if urls := extractURLsFromText(raw); len(urls) > 0 {
		url = urls[0]
	}
	u, err := whatwg.Parse(url)
	if err != nil {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	path := u.Pathname()
	if host == "nodeseek.com" || host == "www.nodeseek.com" {
		if m := nodeseekPostRe.FindStringSubmatch(path); m != nil {
			return "https://www.nodeseek.com/post-" + m[1] + "-1", true
		}
	}
	if xhsHostRe.MatchString(host) {
		if m := xhsPathRe.FindStringSubmatch(path); m != nil {
			return "https://www.xiaohongshu.com/explore/" + strings.ToLower(m[1]), true
		}
	}
	if douyinHostRe.MatchString(host) {
		id, ok := "", false
		if m := douyinPathRe.FindStringSubmatch(path); m != nil {
			id, ok = m[1], true
		} else {
			id, ok = searchParamGet(u, "modal_id")
		}
		if ok && id != "" && digits6Re.MatchString(id) {
			return "https://www.douyin.com/video/" + id, true
		}
	}
	if bilibiliHostRe.MatchString(host) {
		if m := bilibiliPathRe.FindStringSubmatch(path); m != nil {
			return "https://www.bilibili.com/video/" + m[1], true
		}
	}
	if host == "youtu.be" || youtubeHostRe.MatchString(host) {
		id, found := "", false
		if host == "youtu.be" {
			id, found = strings.TrimPrefix(path, "/"), true
			if path == "" {
				id = ""
			}
		} else if v, ok := searchParamGet(u, "v"); ok {
			id, found = v, true
		} else if m := youtubePathRe.FindStringSubmatch(path); m != nil {
			id, found = m[1], true
		}
		if found && id != "" && youtubeIDRe.MatchString(id) {
			return "https://www.youtube.com/watch?v=" + id, true
		}
	}
	return normalizeURL(url)
}

// ---- 导入复核评估（review-assessment.ts） ----

const minParsedBodyChars = 80

var (
	jsWhitespaceClass = `[\t\n\v\f\r \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`
	reviewImageRe     = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)
	reviewLinkRe      = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	reviewMarkRe      = regexp.MustCompile("[`*_#>|~-]")
	reviewSpaceRe     = regexp.MustCompile(jsWhitespaceClass + `+`)
)

// stripLeadingQuoteBlock 对应 /^(?:>[^\n]*(?:\r?\n|$))+\s*/u：去掉开头连续的引用行与随后的空白。
func stripLeadingQuoteBlock(s string) string {
	i := 0
	for i < len(s) && s[i] == '>' {
		nl := strings.IndexByte(s[i:], '\n')
		if nl < 0 {
			i = len(s)
		} else {
			i += nl + 1
		}
	}
	if i == 0 {
		return s
	}
	rest := s[i:]
	return strings.TrimLeftFunc(rest, jsSpace)
}

func toReviewableText(content string) string {
	s := stripLeadingQuoteBlock(content)
	s = reviewImageRe.ReplaceAllString(s, "$1")
	s = reviewLinkRe.ReplaceAllString(s, "$1")
	s = reviewMarkRe.ReplaceAllString(s, " ")
	s = reviewSpaceRe.ReplaceAllString(s, " ")
	return jsTrim(s)
}

func hasUnreadableChars(content string) bool {
	for _, r := range content {
		if r == 0xFFFD || r <= 0x08 || r == 0x0B || r == 0x0C || (r >= 0x0E && r <= 0x1F) {
			return true
		}
	}
	return false
}

// assessImportReview 返回复核原因（去重保序），无原因则为空切片。
func assessImportReview(content, itemType, warningReason, sourceKind string) []string {
	reasons := []string{}
	if w := jsTrim(warningReason); w != "" {
		reasons = append(reasons, w)
	}
	if hasUnreadableChars(content) {
		reasons = append(reasons, "解析文本含有不可识别字符，建议与原始文件核对。")
	}
	body := toReviewableText(content)
	if n := utf16Len(body); sourceKind != "text" && (itemType == "webpage" || itemType == "document" || itemType == "forum") &&
		n > 0 && n < minParsedBodyChars {
		reasons = append(reasons, "解析出的正文仅 "+strconv.Itoa(n)+" 字，建议确认是否漏掉了正文或结构内容。")
	}
	seen := map[string]bool{}
	out := []string{}
	for _, r := range reasons {
		if !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	return out
}

// ---- 文本类连接器的标题规则 ----

const titleMaxLength = 120

// firstLineTitle 对应 connectors.ts 的 firstLineTitle。
func firstLineTitle(text string) string {
	t := jsTrim(text)
	if i := strings.IndexByte(t, '\n'); i >= 0 {
		t = t[:i]
	}
	t = strings.TrimSuffix(t, "\r")
	// .replace(/^#{1,6}\s+/, "")
	n := 0
	for n < len(t) && t[n] == '#' {
		n++
	}
	if n >= 1 && n <= 6 {
		if r, _ := utf8.DecodeRuneInString(t[n:]); r != utf8.RuneError && jsSpace(r) {
			t = strings.TrimLeftFunc(t[n:], jsSpace)
		}
	}
	t = jsTrim(t)
	if utf16Len(t) > titleMaxLength {
		return utf16Prefix(t, titleMaxLength) + "…"
	}
	return t
}
