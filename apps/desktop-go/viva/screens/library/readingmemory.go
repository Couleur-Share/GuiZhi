package library

import (
	"encoding/json"
	"math"
	"sort"
	"time"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/settings"
)

// 内容面板的阅读位置记忆（渲染层 components/library/reading-memory.ts）：每个条目记住上次停在哪个标签、各标签滚到哪、
// 讨论的搜索词与目录开关。渲染层存在 localStorage，这里存在界面偏好 settings.PrefContentReading（旧数据由 legacy 迁移带过来），
// 值是「条目 ID → 记忆」的对象，最多 100 项，满了淘汰最久没动过的；坏数据静默丢弃（整份不是对象就当没有，单项不合法就跳过那一项）。

// ReadingTab 是内容面板的标签（渲染层 ReadingPanelTab）。
type ReadingTab string

// 内容面板的六个标签。
const (
	TabBody       ReadingTab = "body"
	TabTranscript ReadingTab = "transcript"
	TabImages     ReadingTab = "images"
	TabRecognized ReadingTab = "recognized"
	TabSummary    ReadingTab = "summary"
	TabReplies    ReadingTab = "replies"
)

// maxReadingEntries 是记忆里最多留的条目数（渲染层 MAX_ENTRIES）。
const maxReadingEntries = 100

// validReadingTab 报告 s 是不是认得的标签名。
func validReadingTab(s string) bool {
	switch ReadingTab(s) {
	case TabBody, TabTranscript, TabImages, TabRecognized, TabSummary, TabReplies:
		return true
	}
	return false
}

// ReadingMemory 是一个条目的阅读位置。
type ReadingMemory struct {
	Tab ReadingTab
	// ScrollTopByTab 是各标签的滚动位置（逻辑像素，不小于 0）；没记过的标签没有键。
	ScrollTopByTab map[ReadingTab]float64
	// RepliesQuery、CatalogOpen 是讨论标签的搜索词与目录开关；nil 表示没记过（与空串、false 不同：没记过时按楼层数取目录默认开合）。
	RepliesQuery *string
	CatalogOpen  *bool
	// ThemedBySource 是正文与讨论总结各自记住的「AI 主题视图」开关（键是 "body"、"summary"）。
	ThemedBySource map[string]bool
	// UpdatedAt 是最后一次更新的毫秒时间戳，用来淘汰最旧的条目。
	UpdatedAt float64
}

// ReadingPatch 是对一条记忆的局部更新（渲染层 patchContentReadingMemory 的 patch）：没给的字段（空串、nil）保持原值，
// 两张表按键合并而不是整张替换。
type ReadingPatch struct {
	Tab            ReadingTab
	ScrollTopByTab map[ReadingTab]float64
	RepliesQuery   *string
	CatalogOpen    *bool
	ThemedBySource map[string]bool
}

// readingWire 是写进偏好的形状，与渲染层写出的 JSON 一致：两张表恒为对象，没记过的两项不写。
type readingWire struct {
	Tab            string             `json:"tab"`
	ScrollTopByTab map[string]float64 `json:"scrollTopByTab"`
	RepliesQuery   *string            `json:"repliesQuery,omitempty"`
	CatalogOpen    *bool              `json:"catalogOpen,omitempty"`
	ThemedBySource map[string]bool    `json:"themedBySource"`
	UpdatedAt      float64            `json:"updatedAt"`
}

// readReadingStore 读出整份记忆：整份不是对象就当没有，不合法的单项跳过。now 补给缺 updatedAt 的项（渲染层 Date.now()）。
func readReadingStore(prefs services.Prefs, now time.Time) map[string]ReadingMemory {
	out := map[string]ReadingMemory{}
	if prefs == nil {
		return out
	}
	raw, ok := prefs.Get(settings.PrefContentReading)
	if !ok {
		return out
	}
	var items map[string]json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return out
	}
	for id, item := range items {
		if mem, ok := normalizeReading(item, now); ok {
			out[id] = mem
		}
	}
	return out
}

// normalizeReading 把一项存下来的值整理成记忆；tab 不是认得的名字就整项丢弃，其余字段类型不对就当没有。
func normalizeReading(raw json.RawMessage, now time.Time) (ReadingMemory, bool) {
	var obj map[string]any
	if json.Unmarshal(raw, &obj) != nil {
		return ReadingMemory{}, false
	}
	tab, ok := obj["tab"].(string)
	if !ok || !validReadingTab(tab) {
		return ReadingMemory{}, false
	}
	mem := ReadingMemory{
		Tab:            ReadingTab(tab),
		ScrollTopByTab: map[ReadingTab]float64{},
		ThemedBySource: map[string]bool{},
		UpdatedAt:      float64(now.UnixMilli()),
	}
	if scrolls, ok := obj["scrollTopByTab"].(map[string]any); ok {
		for key, value := range scrolls {
			if top, ok := value.(float64); ok && validReadingTab(key) && !math.IsNaN(top) && !math.IsInf(top, 0) {
				mem.ScrollTopByTab[ReadingTab(key)] = math.Max(0, top)
			}
		}
	}
	if q, ok := obj["repliesQuery"].(string); ok {
		mem.RepliesQuery = &q
	}
	if open, ok := obj["catalogOpen"].(bool); ok {
		mem.CatalogOpen = &open
	}
	if themed, ok := obj["themedBySource"].(map[string]any); ok {
		for _, key := range []string{"body", "summary"} {
			if on, ok := themed[key].(bool); ok {
				mem.ThemedBySource[key] = on
			}
		}
	}
	if at, ok := obj["updatedAt"].(float64); ok && !math.IsNaN(at) && !math.IsInf(at, 0) {
		mem.UpdatedAt = at
	}
	return mem, true
}

// LoadReadingMemory 读一个条目的阅读位置；没有存储、没记过或那一项坏了都是 false。
func LoadReadingMemory(prefs services.Prefs, itemID string, now time.Time) (ReadingMemory, bool) {
	mem, ok := readReadingStore(prefs, now)[itemID]
	return mem, ok
}

// PatchReadingMemory 更新一个条目的阅读位置并整份写回（渲染层 patchContentReadingMemory）：tab 缺省沿用旧值、旧值也没有就是正文；
// 条目超过 100 个时淘汰最久没更新的。没有偏好存储时什么都不做；写入失败返回错误（调用方记日志，不打断阅读）。
func PatchReadingMemory(prefs services.Prefs, itemID string, patch ReadingPatch, now time.Time) error {
	if prefs == nil {
		return nil
	}
	store := readReadingStore(prefs, now)
	prev, hadPrev := store[itemID]
	next := ReadingMemory{
		Tab:            TabBody,
		ScrollTopByTab: map[ReadingTab]float64{},
		RepliesQuery:   patch.RepliesQuery,
		CatalogOpen:    patch.CatalogOpen,
		ThemedBySource: map[string]bool{},
		UpdatedAt:      float64(now.UnixMilli()),
	}
	if hadPrev {
		next.Tab = prev.Tab
		for k, v := range prev.ScrollTopByTab {
			next.ScrollTopByTab[k] = v
		}
		if next.RepliesQuery == nil {
			next.RepliesQuery = prev.RepliesQuery
		}
		if next.CatalogOpen == nil {
			next.CatalogOpen = prev.CatalogOpen
		}
		for k, v := range prev.ThemedBySource {
			next.ThemedBySource[k] = v
		}
	}
	if patch.Tab != "" {
		next.Tab = patch.Tab
	}
	for k, v := range patch.ScrollTopByTab {
		next.ScrollTopByTab[k] = v
	}
	for k, v := range patch.ThemedBySource {
		next.ThemedBySource[k] = v
	}
	store[itemID] = next
	return prefs.Set(settings.PrefContentReading, wireReadingStore(trimReadingStore(store)))
}

// trimReadingStore 把条目数压到上限：留下最近更新的，同一毫秒的按 ID 排（渲染层按对象插入顺序，这里没有顺序可依）。
func trimReadingStore(store map[string]ReadingMemory) map[string]ReadingMemory {
	if len(store) <= maxReadingEntries {
		return store
	}
	ids := make([]string, 0, len(store))
	for id := range store {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := store[ids[i]].UpdatedAt, store[ids[j]].UpdatedAt
		if a != b {
			return a > b
		}
		return ids[i] < ids[j]
	})
	out := make(map[string]ReadingMemory, maxReadingEntries)
	for _, id := range ids[:maxReadingEntries] {
		out[id] = store[id]
	}
	return out
}

// wireReadingStore 把记忆换成写进偏好的形状。
func wireReadingStore(store map[string]ReadingMemory) map[string]readingWire {
	out := make(map[string]readingWire, len(store))
	for id, mem := range store {
		scrolls := make(map[string]float64, len(mem.ScrollTopByTab))
		for k, v := range mem.ScrollTopByTab {
			scrolls[string(k)] = v
		}
		out[id] = readingWire{
			Tab: string(mem.Tab), ScrollTopByTab: scrolls, RepliesQuery: mem.RepliesQuery, CatalogOpen: mem.CatalogOpen,
			ThemedBySource: mem.ThemedBySource, UpdatedAt: mem.UpdatedAt,
		}
	}
	return out
}
