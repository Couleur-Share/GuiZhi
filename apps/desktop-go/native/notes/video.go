package notes

// 视频条目元数据引用块的解析（packages/shared/utils/video-meta.ts 的 parseVideoMetaBlock）：
// 在线视频导入时正文开头写入 `> 平台：… · 作者：… · 时长：…`（AI 换标题后追加 `> 原标题：…` 行）。
// 正文仍是数据载体（全文检索、导出、旧条目兼容），渲染视图把该块剥离出来交给界面展示。

import "strings"

// VideoMeta 是解析出的元数据块；Body 是去掉元数据引用块后的正文。
type VideoMeta struct {
	Platform, Author string
	// Duration 是已格式化的时长（如 12:52）。
	Duration      string
	OriginalTitle string
	// Description 是平台简介（导入时压缩为单行写入引用块）。
	Description string
	// TranscriptSource 说明文字稿来自发布者字幕、平台 AI 字幕还是音频识别。
	TranscriptSource string
	Body             string
}

// hasLabelAfterQuote 对应 /^>\s*平台[:：]/。
func hasLabelAfterQuote(line string) bool {
	rest := strings.TrimLeftFunc(line[1:], jsSpace)
	return strings.HasPrefix(rest, "平台:") || strings.HasPrefix(rest, "平台：")
}

// stripQuoteMarker 对应 line.replace(/^>\s?/, "").trim()。
func stripQuoteMarker(line string) string {
	if strings.HasPrefix(line, ">") {
		line = line[1:]
		if len(line) > 0 {
			if r := []rune(line)[0]; jsSpace(r) {
				line = line[len(string(r)):]
			}
		}
	}
	return jsTrim(line)
}

// matchLabel 对应 /^LABEL[:：]\s*(.+)$/：返回冒号之后去掉前导空白的内容。
func matchLabel(text, label string) (string, bool) {
	if !strings.HasPrefix(text, label) {
		return "", false
	}
	rest := text[len(label):]
	if strings.HasPrefix(rest, ":") {
		rest = rest[1:]
	} else if strings.HasPrefix(rest, "：") {
		rest = rest[len("："):]
	} else {
		return "", false
	}
	rest = strings.TrimLeftFunc(rest, jsSpace)
	if !jsDotRest(rest) {
		return "", false
	}
	return rest, true
}

// ParseVideoMeta 解析正文开头的视频元数据引用块。只有首行是 `> 平台：…` 时才视为元数据块（普通引用不受影响），
// 否则返回 nil，调用方按原文处理。
func ParseVideoMeta(content string) *VideoMeta {
	lines := strings.Split(content, "\n")
	first := lines[0]
	if !strings.HasPrefix(first, ">") || !hasLabelAfterQuote(first) {
		return nil
	}
	meta := &VideoMeta{}
	end := 0
	for end < len(lines) && strings.HasPrefix(lines[end], ">") {
		text := stripQuoteMarker(lines[end])
		if v, ok := matchLabel(text, "原标题"); ok {
			meta.OriginalTitle = jsTrim(v)
		} else if v, ok := matchLabel(text, "简介"); ok {
			// 简介整行取值，不参与 · 拆分（内容本身可能含 · 字符）
			meta.Description = jsTrim(v)
		} else if v, ok := matchLabel(text, "文字稿来源"); ok {
			meta.TranscriptSource = jsTrim(v)
		} else {
			// 首行形如「平台：X · 作者：Y · 时长：Z」，字段间以 · 分隔
			for _, part := range strings.Split(text, "·") {
				part = jsTrim(part)
				for _, key := range []string{"平台", "作者", "时长"} {
					v, ok := matchLabel(part, key)
					if !ok {
						continue
					}
					v = jsTrim(v)
					switch key {
					case "平台":
						meta.Platform = v
					case "作者":
						meta.Author = v
					case "时长":
						meta.Duration = v
					}
					break
				}
			}
		}
		end++
	}
	meta.Body = strings.TrimLeft(strings.Join(lines[end:], "\n"), "\n")
	return meta
}

// VideoMetaBody 返回去掉元数据引用块后的正文；没有元数据块时 ok 为假（对应 parseVideoMetaBlock(content)?.body）。
func VideoMetaBody(content string) (body string, ok bool) {
	meta := ParseVideoMeta(content)
	if meta == nil {
		return "", false
	}
	return meta.Body, true
}
