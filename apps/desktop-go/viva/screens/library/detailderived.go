package library

import "guizhi-mygo-shell/native/services"

// 详情上随正文大小线性变贵的三个派生值：能力（deriveCaps 要把正文按行切开找总结小节）、来源 chip（sourceViewOf 要解析视频元数据块）、
// 字数（wordCount 要逐字符数）。它们每帧都要用，而正文可以有几 MB（长文档、整本转写）：逐帧重算一篇 1 MB 的正文，
// 稳态帧就多出 2 ms 以上（2026-11-21 M2-5 的帧耗时读数 TestDetailReaderFrameBudget 暴露，见开发文档 §8.4）。
// 输入不变、结果就不变，所以按输入缓存；这三个函数除了这里不许在别处直接调用（TestDerivedValuesAreOnlyComputedThroughTheCache）。

// derivedKey 是三个派生值读到的条目字段的全部：不在里面的字段（标题、标签、收藏、更新时间……）变了不用重算。
type derivedKey struct {
	itemType, content, transcript, sourceURI string
	deletedAt                                int64
}

// contentDerived 是最近一次的派生值与它的输入。
type contentDerived struct {
	key    derivedKey
	ok     bool
	caps   ItemCaps
	words  int
	source sourceView
	// computes 是重算次数（只给单测：核对「输入不变就不重算」）。
	computes int
}

// derivedOf 返回条目的派生值；输入与上次相同时直接返回缓存。
// 字符串比较在两边是同一块内存时是 O(1)（Go 先比长度，再比指针），正文没变时这里不会逐字节比。
func (s *Screen) derivedOf(item services.LibraryItem) *contentDerived {
	d := &s.detail.derived
	key := derivedKey{
		itemType: item.ItemType, content: item.Content, transcript: item.Transcript, sourceURI: item.SourceURI,
		deletedAt: item.DeletedAt,
	}
	if !d.ok || d.key != key {
		d.key, d.ok = key, true
		d.caps, d.words, d.source = deriveCaps(item), wordCount(item.Content), sourceViewOf(item)
		d.computes++
	}
	return d
}
