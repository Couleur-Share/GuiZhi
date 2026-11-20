package notes

// 论坛条目正文的分段与逐楼回复的还原（packages/shared/utils/forum-note.ts）：
// 讨论总结 / 主楼正文 / 逐楼回复三个小节，小节标题是详情页分段的锚点，常量必须与 TS 一致。
// 写回方向（formatForumReplyBlock、upsertForumSummarySection、replaceForumRepliesSection）随媒体动作迁入（台账 B-060），本包不含。

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// 小节标题。
const (
	ForumSummaryHeading = "## 讨论总结"
	ForumBodyHeading    = "## 正文"
	ForumRepliesHeading = "## 讨论"
)

var (
	reRepliesHeading = regexp.MustCompile(`^##` + sp + `*讨论(?:（(?:楼主` + sp + `*)?\d+` + sp + `*条(?:` + sp + `*·` + sp + `*原帖共` + sp + `*\d+` + sp + `*条)?）)?$`)
	reSummaryHeading = regexp.MustCompile(`^##` + sp + `*讨论总结$`)
	reBodyHeading    = regexp.MustCompile(`^##` + sp + `*正文$`)
	reReplyHeadBold  = regexp.MustCompile(`^\*\*(\d+)` + sp + `*楼` + sp + `*·` + sp + `*(.+?)\*\*$`)
	reReplyHeadH3    = regexp.MustCompile(`^###` + sp + `*(\d+)` + sp + `*楼` + sp + `*·` + sp + `*(.+)$`)
	reReplyToLine    = regexp.MustCompile(`^>` + sp + `*回复` + sp + `*@(.+?)(?:（(\d+)` + sp + `*楼）)?：(.*)$`)
	reSnippetBr      = regexp.MustCompile(`(?i)<br` + sp + `*/?>`)
	reSnippetP       = regexp.MustCompile(`(?i)</?p>`)
	reSnippetWS      = regexp.MustCompile(sp + `+`)
	reBoldFloor      = regexp.MustCompile(`(?m)^\*\*\d+` + sp + `*楼`)
)

// ForumSections 是论坛条目正文按小节切出的三段（均不含小节标题；没有的小节为空串）。
type ForumSections struct{ Summary, Body, Replies string }

// SplitForumSections 按小节标题切分（splitForumNoteSections）。用户可能在详情页编辑过正文，
// 找不到的小节返回空串，首个小节标题之前的内容一律归入正文，不丢内容。
func SplitForumSections(content string) ForumSections {
	stripped := content
	if body, ok := VideoMetaBody(content); ok {
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
		case reSummaryHeading.MatchString(t):
			markers = append(markers, marker{"summary", i})
		case reBodyHeading.MatchString(t):
			markers = append(markers, marker{"body", i})
		case reRepliesHeading.MatchString(t):
			markers = append(markers, marker{"replies", i})
		}
	}
	var s ForumSections
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

// ForumReplyTo 是一条回复的「回复了谁」：作者、可选的楼层、被回复内容的摘要。
type ForumReplyTo struct {
	Author string
	// Floor 在 HasFloor 为真时有意义（TS 的 floor?: number）。
	Floor    int
	HasFloor bool
	Snippet  string
}

// ForumReply 是一条逐楼回复。
type ForumReply struct {
	Floor   int
	Author  string
	Content string
	ReplyTo *ForumReplyTo
}

// NormalizeForumSnippet 整理被回复楼的摘要（normalizeForumSnippet）：摘要给人扫读，<br> 与 <p> 收成空白，
// 截断到 maxLen 个 UTF-16 码元。
func NormalizeForumSnippet(raw string, maxLen int) string {
	s := reSnippetBr.ReplaceAllString(raw, " ")
	s = reSnippetP.ReplaceAllString(s, " ")
	s = reSnippetWS.ReplaceAllString(s, " ")
	return utf16Slice(jsTrim(s), maxLen)
}

// ParseForumReplies 从已入库正文还原逐楼回复（parseForumReplies），认旧的 `**N 楼 · 作者**` 与新的 `### N 楼 · 作者`。
func ParseForumReplies(content string) []ForumReply {
	if section := SplitForumSections(content).Replies; section != "" {
		return ParseForumReplySection(section)
	}
	first := ""
	if t := strings.Split(jsTrim(content), "\n"); len(t) > 0 {
		first = t[0]
	}
	if reReplyHeadH3.MatchString(first) || reReplyHeadBold.MatchString(first) || strings.Contains(content, "\n### ") || reBoldFloor.MatchString(content) {
		return ParseForumReplySection(content)
	}
	return nil
}

// ParseForumReplySection 解析「## 讨论」小节内部（不含标题本身）。
func ParseForumReplySection(section string) []ForumReply {
	if jsTrim(section) == "" {
		return nil
	}
	var entries []ForumReply
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
		entries = append(entries, ForumReply{Floor: floor, Author: author, Content: text, ReplyTo: replyTo})
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

func splitReplyToPrefix(raw string) (*ForumReplyTo, string) {
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
	rt := &ForumReplyTo{Author: jsTrim(m[1]), Snippet: NormalizeForumSnippet(m[3], 200)}
	if m[2] != "" {
		if n, err := strconv.Atoi(m[2]); err == nil {
			rt.Floor, rt.HasFloor = n, true
		}
	}
	return rt, jsTrim(strings.Join(lines[i+1:], "\n"))
}

// ResolveReplyTargetFloor 找出一条回复指向的楼层（resolveReplyTargetFloor）：优先写明的楼层，否则按作者唯一匹配。
func ResolveReplyTargetFloor(replies []ForumReply, to ForumReplyTo) (int, bool) {
	if to.HasFloor {
		for _, r := range replies {
			if r.Floor == to.Floor {
				return to.Floor, true
			}
		}
		return 0, false
	}
	author := strings.ToLower(jsTrim(to.Author))
	if author == "" {
		return 0, false
	}
	hit, n := 0, 0
	for _, r := range replies {
		if strings.ToLower(jsTrim(r.Author)) == author {
			hit, n = r.Floor, n+1
		}
	}
	if n != 1 {
		return 0, false // 重名或不存在：没有唯一的目标（TS 返回 null）
	}
	return hit, true
}

// FilterForumReplies 按关键词筛楼层（filterForumReplies）：扫楼层号、作者、正文、被回复摘要，大小写不敏感；
// 关键词为空（去掉首尾空白之后）时原样返回。
func FilterForumReplies(replies []ForumReply, query string) []ForumReply {
	needle := strings.ToLower(jsTrim(query))
	if needle == "" {
		return replies
	}
	var out []ForumReply
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
