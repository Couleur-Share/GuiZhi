package notes

import (
	"regexp"
	"slices"
	"strings"
)

// 媒体资产引用与总结小节的判定（packages/shared/utils/media-refs 与 media-summary 里详情用得着的两个）。
// 详情据此决定转写入口（本地视频资产）与总结按钮的文案（已有总结小节则是「重新生成」）。

// 资产引用的协议（渲染层 LocalAssetProtocol）。
const (
	LocalImage = "local-image"
	LocalVideo = "local-video"
)

// assetRefPatterns 对应 `${protocol}://([A-Za-z0-9_.-]+)`：文件名取到第一个不属于字符集的字符为止。
var assetRefPatterns = map[string]*regexp.Regexp{
	LocalImage: regexp.MustCompile(`local-image://([A-Za-z0-9_.-]+)`),
	LocalVideo: regexp.MustCompile(`local-video://([A-Za-z0-9_.-]+)`),
}

// LocalAssetRef 取内容里第一个该协议的资产引用的文件名（extractLocalAssetRef）；没有、协议不认识都是 ("", false)。
// 与 extractLocalAssetRefs 不同，这里不做安全文件名过滤（渲染层的单个版本也不过滤）：只用来判断「有没有」。
func LocalAssetRef(content, protocol string) (string, bool) {
	re := assetRefPatterns[protocol]
	if re == nil {
		return "", false
	}
	m := re.FindStringSubmatch(content)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// isSafeAssetFileName 对应 packages/shared/utils/media-refs.ts 的 isSafeAssetFileName。
func isSafeAssetFileName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.HasPrefix(name, ".")
}

// ExtractLocalAssetRefs 按出现顺序提取并去重某一协议的安全资产文件名（extractLocalAssetRefs）。
func ExtractLocalAssetRefs(content, protocol string) []string {
	re := assetRefPatterns[protocol]
	if content == "" || re == nil {
		return nil
	}
	var refs []string
	for _, m := range re.FindAllStringSubmatch(content, -1) {
		name := m[1]
		if isSafeAssetFileName(name) && !slices.Contains(refs, name) {
			refs = append(refs, name)
		}
	}
	return refs
}

// HasMediaSummarySection 报告内容里有没有「## 视频总结」或「## 音频总结」小节标题行（hasMediaSummarySection）：
// 逐行按 JS 的 trim 去掉首尾空白之后整行比较，所以 CRLF 换行也认。
func HasMediaSummarySection(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		switch jsTrim(line) {
		case VideoSummaryHeading, AudioSummaryHeading:
			return true
		}
	}
	return false
}
