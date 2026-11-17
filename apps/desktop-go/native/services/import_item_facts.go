package services

import (
	"regexp"
	"strings"
	"unicode/utf16"
)

// ImportItemFacts 是采集任务详情弹窗要看的「条目事实」：成果卡（正文字数、文字稿字数、媒体个数、标签个数）
// 与来源刷新对照（标题、正文字数）。只带数字与标题，不带正文——详情弹窗不需要把整篇文章搬进界面。
//
// 字数按渲染层的口径算：trim 之后的 UTF-16 长度（JavaScript 的 `content.trim().length`），
// 所以含 emoji 等补充平面字符时一个字符算两个，与旧界面显示的数字一致。
type ImportItemFacts struct {
	ID               string   `json:"id"`
	Title            string   `json:"title"`
	BodyLength       int      `json:"bodyLength"`
	TranscriptLength int      `json:"transcriptLength"`
	AssetCount       int      `json:"assetCount"`
	Tags             []string `json:"tags"`
}

// localAssetRefPattern 对应 packages/shared/utils/media-refs.ts 的 `${protocol}://([A-Za-z0-9_.-]+)`。
var localAssetRefPattern = regexp.MustCompile(`(?:local-image|local-video)://([A-Za-z0-9_.-]+)`)

// ComputeImportItemFacts 由条目的内容算出事实。
func ComputeImportItemFacts(id, title, content, transcript string, tags []string) ImportItemFacts {
	return ImportItemFacts{
		ID:               id,
		Title:            title,
		BodyLength:       jsTrimmedLength(content),
		TranscriptLength: jsTrimmedLength(transcript),
		AssetCount:       countLocalAssetRefs(content),
		Tags:             tags,
	}
}

// countLocalAssetRefs 是 extractAllLocalAssetRefs(content).length：两种协议的引用合并，按文件名去重，
// 文件名必须是安全的（不含路径、不以点开头）。
func countLocalAssetRefs(content string) int {
	if content == "" {
		return 0
	}
	names := map[string]struct{}{}
	for _, m := range localAssetRefPattern.FindAllStringSubmatch(content, -1) {
		name := m[1]
		if name == "." || name == ".." || strings.HasPrefix(name, ".") {
			continue
		}
		names[name] = struct{}{}
	}
	return len(names)
}

// jsTrimmedLength 是 JavaScript 里 `s.trim().length`：按 ECMAScript 的空白集合去掉两端，再数 UTF-16 码元。
func jsTrimmedLength(s string) int {
	return len(utf16.Encode([]rune(strings.TrimFunc(s, isJSSpace))))
}

// isJSSpace 是 ECMAScript 的 WhiteSpace 与 LineTerminator（String.prototype.trim 去掉的字符）。
// 与 Go 的 unicode.IsSpace 不同：JavaScript 去掉 U+FEFF 而不去 U+0085。
func isJSSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0x00A0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}
