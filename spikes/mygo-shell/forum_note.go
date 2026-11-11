package main

// 对应 packages/shared/utils/forum-note.ts：论坛条目正文的分段（讨论总结 / 主楼正文 / 逐楼回复）、
// 回复块格式化、从正文还原回复、总结与回复小节的写回。小节标题是详情页分段的锚点，常量必须与 TS 一致。

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

const (
	forumSummaryHeading   = "## 讨论总结"
	forumBodyHeading      = "## 正文"
	forumRepliesHeading   = "## 讨论"
	forumSummaryStaleNote = "> 讨论内容已刷新，当前总结基于刷新前的楼层，请重新生成。"
)

// fsp 对应 JS 正则里的 \s（与 RE2 的 \s 不同）。
var fsp = "[" + jsSpaceClass + "]"

var (
	reForumRepliesHeading = regexp.MustCompile(`^##` + fsp + `*讨论(?:（(?:楼主` + fsp + `*)?\d+` + fsp + `*条(?:` + fsp + `*·` + fsp + `*原帖共` + fsp + `*\d+` + fsp + `*条)?）)?$`)
	reForumSummaryHeading = regexp.MustCompile(`^##` + fsp + `*讨论总结$`)
	reForumBodyHeading    = regexp.MustCompile(`^##` + fsp + `*正文$`)
	reReplyHeadBold       = regexp.MustCompile(`^\*\*(\d+)` + fsp + `*楼` + fsp + `*·` + fsp + `*(.+?)\*\*$`)
	reReplyHeadH3         = regexp.MustCompile(`^###` + fsp + `*(\d+)` + fsp + `*楼` + fsp + `*·` + fsp + `*(.+)$`)
	reReplyToLine         = regexp.MustCompile(`^>` + fsp + `*回复` + fsp + `*@(.+?)(?:（(\d+)` + fsp + `*楼）)?：(.*)$`)
	reSectionHeadingLine  = regexp.MustCompile(`^##` + fsp)
	reSummaryNoteLine     = regexp.MustCompile(`^>` + fsp + `*(?:未配置文本模型|讨论总结生成失败|讨论内容已刷新，当前总结基于刷新前的楼层)`)
	reForumBr             = regexp.MustCompile(`(?i)<br` + fsp + `*/?>`)
	reForumP              = regexp.MustCompile(`(?i)</?p>`)
	reForumWS             = regexp.MustCompile(fsp + `+`)
	reForumTripleNL       = regexp.MustCompile(`\n{3,}`)
	reForumBoldFloor      = regexp.MustCompile(`(?m)^\*\*\d+` + fsp + `*楼`)
)

type forumNoteSections struct{ Summary, Body, Replies string }

// splitForumNoteSections 按小节标题切分；找不到的小节为空串，首个标题之前的内容归入正文。
func splitForumNoteSections(content string) forumNoteSections {
	stripped := content
	if body, ok := videoMetaBody(content); ok {
		stripped = body
	}
	lines := strings.Split(stripped, "\n")
	type marker struct {
		kind  string
		index int
	}
	var markers []marker
	for i, line := range lines {
		t := jsTrim(line)
		switch {
		case reForumSummaryHeading.MatchString(t):
			markers = append(markers, marker{"summary", i})
		case reForumBodyHeading.MatchString(t):
			markers = append(markers, marker{"body", i})
		case reForumRepliesHeading.MatchString(t):
			markers = append(markers, marker{"replies", i})
		}
	}
	var s forumNoteSections
	set := func(kind, text string) {
		p := map[string]*string{"summary": &s.Summary, "body": &s.Body, "replies": &s.Replies}[kind]
		if *p != "" {
			*p += "\n\n" + text
		} else {
			*p = text
		}
	}
	first := len(lines)
	if len(markers) > 0 {
		first = markers[0].index
	}
	for order, m := range markers {
		end := len(lines)
		if order+1 < len(markers) {
			end = markers[order+1].index
		}
		set(m.kind, jsTrim(strings.Join(lines[m.index+1:end], "\n")))
	}
	if pre := jsTrim(strings.Join(lines[:first], "\n")); pre != "" {
		if s.Body != "" {
			s.Body = pre + "\n\n" + s.Body
		} else {
			s.Body = pre
		}
	}
	return s
}

type forumReplyTo struct {
	Author   string
	Floor    *int
	Snippet  string
	HasFloor bool
}

type forumReplyEntry struct {
	Floor   int
	Author  string
	Content string
	ReplyTo *forumReplyTo
}

// normalizeForumSnippet 对应 normalizeForumSnippet：摘要给人扫读，<br> 与 <p> 收成空白，截断到 maxLen 个 UTF-16 码元。
func normalizeForumSnippet(raw string, maxLen int) string {
	s := reForumBr.ReplaceAllString(raw, " ")
	s = reForumP.ReplaceAllString(s, " ")
	s = reForumWS.ReplaceAllString(s, " ")
	return utf16Slice(jsTrim(s), maxLen)
}

// formatForumReplyBlock 对应 formatForumReplyBlock：楼层用 ###，上下文用引用行，正文另起。
func formatForumReplyBlock(floor int, author, content string, replyTo *forumReplyTo) string {
	if author == "" {
		author = "匿名"
	}
	parts := []string{fmt.Sprintf("### %d 楼 · %s", floor, author)}
	if replyTo != nil {
		who := jsTrim(replyTo.Author)
		if who == "" {
			who = "某人"
		}
		snippet := normalizeForumSnippet(replyTo.Snippet, 200)
		floorPart := ""
		if replyTo.Floor != nil {
			floorPart = fmt.Sprintf("（%d 楼）", *replyTo.Floor)
		}
		if snippet != "" {
			parts = append(parts, "> 回复 @"+who+floorPart+"："+snippet)
		} else {
			parts = append(parts, "> 回复 @"+who+floorPart+"：")
		}
	}
	if jsTrim(content) != "" {
		parts = append(parts, jsTrim(content))
	}
	return strings.Join(parts, "\n\n")
}

// parseForumReplies 对应 parseForumReplies：从已入库正文还原逐楼回复，认旧的 **N 楼 · 作者** 与新的 ### N 楼 · 作者。
func parseForumReplies(content string) []forumReplyEntry {
	if section := splitForumNoteSections(content).Replies; section != "" {
		return parseForumReplySection(section)
	}
	first := ""
	if t := strings.Split(jsTrim(content), "\n"); len(t) > 0 {
		first = t[0]
	}
	if reReplyHeadH3.MatchString(first) || reReplyHeadBold.MatchString(first) || strings.Contains(content, "\n### ") || reForumBoldFloor.MatchString(content) {
		return parseForumReplySection(content)
	}
	return nil
}

// parseForumReplySection 解析「## 讨论」小节内部（不含标题本身）。
func parseForumReplySection(section string) []forumReplyEntry {
	if jsTrim(section) == "" {
		return nil
	}
	var entries []forumReplyEntry
	floor, author := 0, ""
	var body []string
	flush := func() {
		if floor <= 0 {
			return
		}
		replyTo, text := splitReplyToPrefix(strings.Join(body, "\n"))
		if text == "" && replyTo == nil {
			return
		}
		entries = append(entries, forumReplyEntry{Floor: floor, Author: author, Content: text, ReplyTo: replyTo})
	}
	for _, line := range strings.Split(section, "\n") {
		t := jsTrim(line)
		head := reReplyHeadH3.FindStringSubmatch(t)
		if head == nil {
			head = reReplyHeadBold.FindStringSubmatch(t)
		}
		if head != nil {
			flush()
			floor, _ = strconv.Atoi(head[1])
			author = jsTrim(head[2])
			body = nil
			continue
		}
		body = append(body, line)
	}
	flush()
	return entries
}

func splitReplyToPrefix(raw string) (*forumReplyTo, string) {
	lines := strings.Split(raw, "\n")
	i := 0
	for i < len(lines) && jsTrim(lines[i]) == "" {
		i++
	}
	if i >= len(lines) {
		return nil, ""
	}
	m := reReplyToLine.FindStringSubmatch(jsTrim(lines[i]))
	if m == nil {
		return nil, jsTrim(raw)
	}
	rt := &forumReplyTo{Author: jsTrim(m[1]), Snippet: normalizeForumSnippet(m[3], 200)}
	if m[2] != "" {
		if n, err := strconv.Atoi(m[2]); err == nil {
			rt.Floor, rt.HasFloor = &n, true
		}
	}
	return rt, jsTrim(strings.Join(lines[i+1:], "\n"))
}

// resolveReplyTargetFloor 对应 resolveReplyTargetFloor：优先写明的楼层，否则按作者唯一匹配。
func resolveReplyTargetFloor(replies []forumReplyEntry, replyTo forumReplyTo) (int, bool) {
	if replyTo.Floor != nil {
		for _, r := range replies {
			if r.Floor == *replyTo.Floor {
				return *replyTo.Floor, true
			}
		}
		return 0, false
	}
	author := strings.ToLower(jsTrim(replyTo.Author))
	if author == "" {
		return 0, false
	}
	hit, n := 0, 0
	for _, r := range replies {
		if strings.ToLower(jsTrim(r.Author)) == author {
			hit, n = r.Floor, n+1
		}
	}
	return hit, n == 1
}

// filterForumReplies 对应 filterForumReplies：扫楼层号、作者、正文、被回复摘要，大小写不敏感。
func filterForumReplies(replies []forumReplyEntry, query string) []forumReplyEntry {
	needle := strings.ToLower(jsTrim(query))
	if needle == "" {
		return replies
	}
	var out []forumReplyEntry
	for _, r := range replies {
		rtAuthor, rtSnippet := "", ""
		if r.ReplyTo != nil {
			rtAuthor, rtSnippet = r.ReplyTo.Author, r.ReplyTo.Snippet
		}
		text := strings.ToLower(strings.Join([]string{strconv.Itoa(r.Floor), fmt.Sprintf("%d 楼", r.Floor), r.Author, r.Content, rtAuthor, rtSnippet}, "\n"))
		if strings.Contains(text, needle) {
			out = append(out, r)
		}
	}
	return out
}

func forumJoinBlocks(lines []string) string {
	return jsTrim(reForumTripleNL.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}

func forumFindLine(lines []string, re *regexp.Regexp) int {
	for i, l := range lines {
		if re.MatchString(jsTrim(l)) {
			return i
		}
	}
	return -1
}

// upsertForumSummarySection 对应 upsertForumSummarySection：写回讨论总结，并清掉采集期留下的状态注记。
func upsertForumSummarySection(content, summary string) string {
	var lines []string
	for _, l := range strings.Split(content, "\n") {
		if !reSummaryNoteLine.MatchString(jsTrim(l)) {
			lines = append(lines, l)
		}
	}
	section := append([]string{forumSummaryHeading, ""}, strings.Split(jsTrim(summary), "\n")...)
	if start := forumFindLine(lines, reForumSummaryHeading); start >= 0 {
		end := len(lines)
		for i := start + 1; i < len(lines); i++ {
			if reSectionHeadingLine.MatchString(jsTrim(lines[i])) {
				end = i
				break
			}
		}
		out := append([]string{}, lines[:start]...)
		out = append(out, section...)
		out = append(out, "")
		out = append(out, lines[end:]...)
		return forumJoinBlocks(out)
	}
	insertAt := forumFindLine(lines, reSectionHeadingLine)
	if insertAt < 0 {
		insertAt = len(lines)
	}
	out := append([]string{}, lines[:insertAt]...)
	out = append(out, "")
	out = append(out, section...)
	out = append(out, "")
	out = append(out, lines[insertAt:]...)
	return forumJoinBlocks(out)
}

// replaceForumRepliesSection 对应 replaceForumRepliesSection：用最新抓到的逐楼内容替换「讨论」小节，已有总结加过期提示。
func replaceForumRepliesSection(content string, replies []forumReplyEntry) string {
	lines := strings.Split(content, "\n")
	blocks := make([]string, len(replies))
	for i, r := range replies {
		blocks[i] = formatForumReplyBlock(r.Floor, r.Author, r.Content, r.ReplyTo)
	}
	replyBody := strings.Join(blocks, "\n\n")
	section := []string{fmt.Sprintf("%s（%d 条）", forumRepliesHeading, len(replies))}
	if replyBody != "" {
		section = append(section, "")
		section = append(section, strings.Split(replyBody, "\n")...)
	}
	var next []string
	if start := forumFindLine(lines, reForumRepliesHeading); start >= 0 {
		end := len(lines)
		for i := start + 1; i < len(lines); i++ {
			if reSectionHeadingLine.MatchString(jsTrim(lines[i])) {
				end = i
				break
			}
		}
		next = append(next, lines[:start]...)
		next = append(next, section...)
		next = append(next, "")
		next = append(next, lines[end:]...)
	} else {
		next = append(next, lines...)
		next = append(next, "")
		next = append(next, section...)
	}
	if jsTrim(splitForumNoteSections(content).Summary) != "" {
		marked := false
		for _, l := range next {
			if jsTrim(l) == forumSummaryStaleNote {
				marked = true
				break
			}
		}
		if idx := forumFindLine(next, reForumSummaryHeading); idx >= 0 && !marked {
			next = append(next[:idx+1], append([]string{"", forumSummaryStaleNote}, next[idx+1:]...)...)
		}
	}
	return forumJoinBlocks(next)
}

var _ = math.MaxInt
