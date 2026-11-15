package aiflow

// 语义索引中 TS 与 Go 有意不同、或 golden 无法覆盖的行为。

import (
	"strings"
	"testing"
)

// 孤立代理项是已知且有意的差异（见 jsx.go 文件头）：分块的切口落在代理对中间时，Go 在该处落成 U+FFFD，
// 不产生非法 UTF-8；TS 会发出孤立代理项。这里固定 Go 的行为，golden 不覆盖这种输入。
func TestBuildEmbeddingChunksSplitsSurrogatePairAsReplacementChar(t *testing.T) {
	// 表情占码元 699–700，第一块 [0,700) 恰好切在它中间；第二块从 620 开始，完整包含它
	content := goldenCJKText(699) + "😀" + goldenCJKText(300)
	chunks := BuildEmbeddingChunks("", content, nil)
	if len(chunks) != 2 {
		t.Fatalf("应分成 2 块，实际 %d", len(chunks))
	}
	if !strings.HasSuffix(chunks[0], "\uFFFD") {
		t.Fatalf("第一块的切口落在代理对中间，应以 U+FFFD 收尾：%q", u16Slice(chunks[0], 695, 700))
	}
	if !strings.Contains(chunks[1], "😀") {
		t.Fatal("第二块与第一块有 80 码元重叠，应完整包含该表情")
	}
}

func TestBuildEmbeddingChunksTitlePrefixAndEmptyInput(t *testing.T) {
	if got := BuildEmbeddingChunks(" ", " \n", nil); got == nil || len(got) != 0 {
		t.Fatalf("标题与正文都为空白时应返回空切片（非 nil，序列化为 []）：%#v", got)
	}
	if got := BuildEmbeddingChunks("标题", "", nil); len(got) != 1 || got[0] != "标题" {
		t.Fatalf("只有标题的条目，标题本身就是可嵌入文本：%q", got)
	}
	got := BuildEmbeddingChunks("标题", "正文", nil)
	if len(got) != 1 || got[0] != "《标题》\n正文" {
		t.Fatalf("单块应带标题前缀：%q", got)
	}
}
