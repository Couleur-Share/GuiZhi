package wiki

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"guizhi-mygo-shell/aiflow"
)

// TocMinHeadings 是页内目录出现的门槛：标题少于这个数不值得占一块目录。
const TocMinHeadings = 3

var (
	reHeadingLine  = regexp.MustCompile(`^(#{1,6})[` + jsSpaceClass + `]+(.*)$`)
	reFence        = regexp.MustCompile("^(```|~~~)")
	reTrailingHash = regexp.MustCompile(`[` + jsSpaceClass + `]*#+[` + jsSpaceClass + `]*$`)
)

// jsSpaceClass 是 jsSpace 的正则字符类写法（放进 [...] 里用）。
const jsSpaceClass = `\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}`

// TocEntry 是页内目录的一项。
type TocEntry struct {
	// Level 是标题层级：2 或 3（一级标题已被 StripDuplicateTitleHeading 剥掉，更深的层级不进目录）。
	Level int
	Text  string
	// Slug 与渲染出的标题锚点一致，目录据此跳转。
	Slug string
}

// Slugify 把标题文字转成锚点：去掉 Markdown 记号与括号，空白换成 -，只留字母、数字与 -，转小写；
// 空结果取 "section"。同名标题追加出现序号（occurrence > 0 时），保证页内锚点唯一。
func Slugify(text string, occurrence int) string {
	var b strings.Builder
	spaceRun := false
	for _, r := range jsTrim(text) {
		switch r {
		case '*', '_', '`', '[', ']', '(', ')':
			continue
		}
		if jsSpace(r) {
			if !spaceRun {
				b.WriteByte('-')
			}
			spaceRun = true
			continue
		}
		spaceRun = false
		if unicode.IsLetter(r) || unicode.IsNumber(r) || r == '-' {
			b.WriteRune(r)
		}
	}
	base := strings.ToLower(b.String())
	if base == "" {
		base = "section"
	}
	if occurrence > 0 {
		return base + "-" + strconv.Itoa(occurrence)
	}
	return base
}

// ExtractToc 抽取页内目录（二、三级标题）。跳过围栏代码块内的 # 开头行——Shell 注释和 Markdown 标题长得一样，
// 不跳过的话代码块里的每行注释都会变成一个目录项。
func ExtractToc(body string) []TocEntry {
	var entries []TocEntry
	used := map[string]int{}
	inFence := false
	for _, raw := range splitLines(body) {
		line := jsTrim(raw)
		if reFence.MatchString(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		m := reHeadingLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		level := len(m[1])
		text := jsTrim(reTrailingHash.ReplaceAllString(jsTrim(m[2]), ""))
		if level < 2 || level > 3 || text == "" {
			continue
		}
		base := Slugify(text, 0)
		occurrence := used[base]
		used[base] = occurrence + 1
		entries = append(entries, TocEntry{Level: level, Text: text, Slug: Slugify(text, occurrence)})
	}
	return entries
}

// ShowToc 报告目录是否值得显示（标题数不少于 TocMinHeadings）。
func ShowToc(entries []TocEntry) bool { return len(entries) >= TocMinHeadings }

// splitLines 按 \r?\n 拆行。
func splitLines(s string) []string {
	return strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
}

// RenderedBody 是详情页实际渲染的正文：老页面的正文里还留着与标题重复的一级标题（落库侧的清洗只对新编译生效），
// 渲染时再剥一次。
func RenderedBody(body, title string) string {
	return aiflow.StripDuplicateTitleHeading(body, title)
}
