package main

// 条目正文里与媒体相关的小节/注记工具，对应：
//   - shared/utils/media-summary.ts：「视频/音频总结」小节的定位与写入、原标题注记
//   - services/import/video-url.ts：转写来源注记（upsertTranscriptionSourceNote / stripTranscriptionNote）
//   - shared/utils/media-refs.ts：extractLocalAssetRef

import (
	"regexp"
	"strings"
)

var mediaSummaryHeadings = []string{"## 视频总结", "## 音频总结"}

// mediaSummaryHeading 按条目类型选择总结小节标题（audio → 音频总结，其余 → 视频总结）。
func mediaSummaryHeading(itemType string) string {
	if itemType == "audio" {
		return "## 音频总结"
	}
	return "## 视频总结"
}

func isSummaryHeadingLine(line string) bool {
	t := jsTrim(line)
	for _, h := range mediaSummaryHeadings {
		if t == h {
			return true
		}
	}
	return false
}

var reLocalProtocol = regexp.MustCompile(`local-[a-z]+://`)

// 首段是否为「元数据头」：`> ` 引用块（视频元信息）或本地资产引用行。
func isMetadataLeadParagraph(p string) bool {
	return strings.HasPrefix(p, "> ") || reLocalProtocol.MatchString(p)
}

// upsertMediaSummarySection 把总结写入正文：已有总结小节则原位替换，否则在元数据头之后插入，没有元数据头就置顶。
// 小节的结束边界是下一个二级标题或 `---`；插入时在小节后补一条 `---` 作为确定性的结束锚点，
// 保证重新生成只替换总结本身、不吞掉后续内容。
func upsertMediaSummarySection(content, heading, summary string) string {
	sectionLines := append([]string{heading, ""}, strings.Split(jsTrim(summary), "\n")...)
	lines := strings.Split(content, "\n")

	startIdx := -1
	for i, line := range lines {
		if isSummaryHeadingLine(line) {
			startIdx = i
			break
		}
	}
	if startIdx >= 0 {
		endIdx := len(lines)
		for i := startIdx + 1; i < len(lines); i++ {
			t := jsTrim(lines[i])
			if strings.HasPrefix(t, "## ") || t == "---" {
				endIdx = i
				break
			}
		}
		tail := lines[endIdx:]
		out := append([]string{}, lines[:startIdx]...)
		out = append(out, sectionLines...)
		if len(tail) > 0 {
			out = append(out, "")
			out = append(out, tail...)
		}
		return strings.Join(out, "\n")
	}

	if jsTrim(content) == "" {
		return strings.Join(sectionLines, "\n")
	}

	firstBlank := -1
	for i, line := range lines {
		if jsTrim(line) == "" {
			firstBlank = i
			break
		}
	}
	leadEnd := firstBlank
	if firstBlank < 0 {
		leadEnd = len(lines)
	}
	lead := jsTrim(strings.Join(lines[:leadEnd], "\n"))
	if leadEnd > 0 && isMetadataLeadParagraph(lead) {
		tail := lines[leadEnd:]
		hasTail := false
		for _, line := range tail {
			if jsTrim(line) != "" {
				hasTail = true
				break
			}
		}
		// 小节后还有内容才需要 --- 结束锚点；小节收尾时无需多余分隔线
		section := sectionLines
		if hasTail {
			section = append(append([]string{}, sectionLines...), "", "---")
		}
		out := append([]string{}, lines[:leadEnd]...)
		out = append(out, "")
		out = append(out, section...)
		out = append(out, tail...)
		return strings.Join(out, "\n")
	}

	return strings.Join(append(append([]string{}, sectionLines...), "", "---"), "\n") + "\n\n" + content
}

var reOriginalTitleLine = regexp.MustCompile(`^>[` + jsWS + `]*原标题[:：]`)

// appendOriginalTitleNote AI 标题替换原标题时，把原标题记进开头的元数据引用块（`> 原标题：xxx`），
// 保证仍能按平台原标题检索到条目。已有记录或开头不是引用块时原样返回。
func appendOriginalTitleNote(content, originalTitle string) string {
	title := jsTrim(originalTitle)
	if title == "" {
		return content
	}
	lines := strings.Split(content, "\n")
	if !strings.HasPrefix(lines[0], "> ") {
		return content
	}
	end := 0
	for end < len(lines) && strings.HasPrefix(lines[end], ">") {
		if reOriginalTitleLine.MatchString(lines[end]) {
			return content
		}
		end++
	}
	out := append([]string{}, lines[:end]...)
	out = append(out, "> 原标题："+title)
	out = append(out, lines[end:]...)
	return strings.Join(out, "\n")
}

// 转写状态注记的段落前缀（buildVideoContent 写入正文的两种模板）。
var transcriptionNotePrefixes = []string{"> 文字稿生成失败", "> 未配置「语音转写」模型", "> 文字稿来源："}

// stripTranscriptionNote 重新生成文字稿成功后，从正文中移除历史转写状态注记。
func stripTranscriptionNote(content string) string {
	var paragraphs []string
	for _, paragraph := range strings.Split(content, "\n\n") {
		var kept []string
		for _, line := range strings.Split(paragraph, "\n") {
			drop := false
			for _, p := range transcriptionNotePrefixes {
				if strings.HasPrefix(line, p) {
					drop = true
					break
				}
			}
			if !drop {
				kept = append(kept, line)
			}
		}
		if joined := strings.Join(kept, "\n"); joined != "" {
			paragraphs = append(paragraphs, joined)
		}
	}
	return strings.Join(paragraphs, "\n\n")
}

var rePlatformLine = regexp.MustCompile(`^>[` + jsWS + `]*平台[:：]`)

// upsertTranscriptionSourceNote 写入「文字稿来源」注记：来源行固定放在连续元数据引用块内，
// 这样详情、导出与全文检索都读得到。首行不是平台元数据行时原样返回。
func upsertTranscriptionSourceNote(content, source string) string {
	lines := strings.Split(stripTranscriptionNote(content), "\n")
	if !rePlatformLine.MatchString(lines[0]) {
		return content
	}
	end := 0
	for end < len(lines) && strings.HasPrefix(lines[end], ">") {
		end++
	}
	out := append([]string{}, lines[:end]...)
	out = append(out, "> 文字稿来源："+source)
	out = append(out, lines[end:]...)
	return strings.Join(out, "\n")
}

var reLocalVideoRef = regexp.MustCompile(`local-video://([A-Za-z0-9_.-]+)`)

// extractLocalVideoRef 对应 extractLocalAssetRef(content, "local-video")：取第一处引用。
func extractLocalVideoRef(content string) (string, bool) {
	m := reLocalVideoRef.FindStringSubmatch(content)
	if m == nil {
		return "", false
	}
	return m[1], true
}
