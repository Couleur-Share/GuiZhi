package library

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"guizhi-mygo-shell/native/settings"
)

// 本文件对应渲染层 reading-memory.ts：读取时的归一化（坏数据静默丢弃）、局部更新的合并规则、100 项上限、写回的形状。

func memAt(ms int64) time.Time { return time.UnixMilli(ms) }

func putReading(m *memPrefs, raw string) { m.data[settings.PrefContentReading] = json.RawMessage(raw) }

// 渲染层迁移过来的数据照原样读出。
func TestReadingMemoryReadsMigratedData(t *testing.T) {
	p := newMemPrefs()
	putReading(p, `{"item1":{"tab":"body","scrollTopByTab":{"body":120},"updatedAt":1}}`)
	mem, ok := LoadReadingMemory(p, "item1", memAt(9))
	if !ok || mem.Tab != TabBody || mem.ScrollTopByTab[TabBody] != 120 || mem.UpdatedAt != 1 {
		t.Fatalf("读出 %+v, %v", mem, ok)
	}
	if mem.RepliesQuery != nil || mem.CatalogOpen != nil {
		t.Fatalf("没记过的两项应是 nil：%+v", mem)
	}
	if _, ok := LoadReadingMemory(p, "other", memAt(9)); ok {
		t.Fatal("没记过的条目不该有记忆")
	}
}

// 坏数据静默丢弃：整份不是对象当没有；单项 tab 不认得就跳过那一项；字段类型不对当没有；滚动位置不小于 0。
func TestReadingMemoryDropsBadData(t *testing.T) {
	for _, raw := range []string{`[]`, `"x"`, `7`, `null`, `{bad json`} {
		p := newMemPrefs()
		putReading(p, raw)
		if _, ok := LoadReadingMemory(p, "a", memAt(1)); ok {
			t.Errorf("%s 不该读出任何条目", raw)
		}
	}
	p := newMemPrefs()
	putReading(p, `{
		"badtab": {"tab":"nope","scrollTopByTab":{}},
		"notobj": 5,
		"arr": [],
		"notab": {"scrollTopByTab":{}},
		"tabnum": {"tab":3},
		"weird": {
			"tab":"replies",
			"scrollTopByTab":{"replies":-30,"body":"12","unknown":4,"summary":8.5},
			"repliesQuery":5,
			"catalogOpen":"yes",
			"themedBySource":{"body":true,"summary":"x","other":true},
			"updatedAt":"later"
		},
		"scrollarr": {"tab":"body","scrollTopByTab":[1,2],"themedBySource":[true]}
	}`)
	for _, id := range []string{"badtab", "notobj", "arr", "notab", "tabnum"} {
		if _, ok := LoadReadingMemory(p, id, memAt(1)); ok {
			t.Errorf("%s 该被丢弃", id)
		}
	}
	mem, ok := LoadReadingMemory(p, "weird", memAt(777))
	if !ok || mem.Tab != TabReplies {
		t.Fatalf("weird：%+v, %v", mem, ok)
	}
	if got := mem.ScrollTopByTab; len(got) != 2 || got[TabReplies] != 0 || got[TabSummary] != 8.5 {
		t.Errorf("滚动位置应只留认得的标签的数字、负数按 0：%v", got)
	}
	if mem.RepliesQuery != nil || mem.CatalogOpen != nil {
		t.Errorf("类型不对的两项应当没有：%+v", mem)
	}
	if got := mem.ThemedBySource; len(got) != 1 || !got["body"] {
		t.Errorf("主题视图只认 body、summary 两个键的布尔值：%v", got)
	}
	if mem.UpdatedAt != 777 {
		t.Errorf("没有合法 updatedAt 时取当前时间：%v", mem.UpdatedAt)
	}
	arr, ok := LoadReadingMemory(p, "scrollarr", memAt(1))
	if !ok || len(arr.ScrollTopByTab) != 0 || len(arr.ThemedBySource) != 0 {
		t.Errorf("两张表是数组时当空表：%+v, %v", arr, ok)
	}
}

// 局部更新的合并：没给的字段保持原值，两张表按键合并；tab 缺省沿用旧值，旧值也没有就是正文；空串与 nil 不同。
func TestPatchReadingMemoryMergesLikeTheRenderer(t *testing.T) {
	p := newMemPrefs()
	str := func(s string) *string { return &s }
	flag := func(b bool) *bool { return &b }
	patch := func(at int64, patch ReadingPatch) ReadingMemory {
		t.Helper()
		if err := PatchReadingMemory(p, "a", patch, memAt(at)); err != nil {
			t.Fatal(err)
		}
		mem, ok := LoadReadingMemory(p, "a", memAt(0))
		if !ok {
			t.Fatal("写完应当读得回来")
		}
		return mem
	}

	mem := patch(10, ReadingPatch{ScrollTopByTab: map[ReadingTab]float64{TabBody: 5}})
	if mem.Tab != TabBody || mem.ScrollTopByTab[TabBody] != 5 || mem.UpdatedAt != 10 {
		t.Fatalf("没有旧值也没给 tab 应是正文：%+v", mem)
	}
	mem = patch(20, ReadingPatch{Tab: TabReplies, RepliesQuery: str("楼主"), CatalogOpen: flag(true)})
	if mem.Tab != TabReplies || *mem.RepliesQuery != "楼主" || !*mem.CatalogOpen || mem.ScrollTopByTab[TabBody] != 5 {
		t.Fatalf("换标签不该丢别的标签的滚动位置：%+v", mem)
	}
	mem = patch(30, ReadingPatch{ScrollTopByTab: map[ReadingTab]float64{TabReplies: 70}})
	if mem.Tab != TabReplies || *mem.RepliesQuery != "楼主" || mem.CatalogOpen == nil || !*mem.CatalogOpen || mem.ScrollTopByTab[TabBody] != 5 || mem.ScrollTopByTab[TabReplies] != 70 {
		t.Fatalf("只给滚动位置时 tab、搜索词、目录保持，滚动按键合并：%+v", mem)
	}
	mem = patch(40, ReadingPatch{RepliesQuery: str(""), CatalogOpen: flag(false)})
	if mem.RepliesQuery == nil || *mem.RepliesQuery != "" || mem.CatalogOpen == nil || *mem.CatalogOpen {
		t.Fatalf("空串与 false 是明确的值，要覆盖旧值：%+v", mem)
	}
	mem = patch(50, ReadingPatch{ThemedBySource: map[string]bool{"body": true}})
	mem = patch(60, ReadingPatch{ThemedBySource: map[string]bool{"summary": true}})
	if !mem.ThemedBySource["body"] || !mem.ThemedBySource["summary"] || mem.UpdatedAt != 60 {
		t.Fatalf("主题视图按键合并：%+v", mem)
	}
}

// 写进偏好的形状与渲染层写出的 JSON 一致：两张表恒为对象，没记过的搜索词与目录开关不写。
func TestReadingMemoryWireShape(t *testing.T) {
	p := newMemPrefs()
	if err := PatchReadingMemory(p, "it", ReadingPatch{Tab: TabSummary}, memAt(1700)); err != nil {
		t.Fatal(err)
	}
	want := `{"it":{"tab":"summary","scrollTopByTab":{},"themedBySource":{},"updatedAt":1700}}`
	if got := string(p.data[settings.PrefContentReading]); got != want {
		t.Fatalf("写出的形状：\n  got  %s\n  want %s", got, want)
	}
	q, open := "查", true
	if err := PatchReadingMemory(p, "it", ReadingPatch{RepliesQuery: &q, CatalogOpen: &open, ScrollTopByTab: map[ReadingTab]float64{TabSummary: 12.5}}, memAt(1800)); err != nil {
		t.Fatal(err)
	}
	want = `{"it":{"tab":"summary","scrollTopByTab":{"summary":12.5},"repliesQuery":"查","catalogOpen":true,"themedBySource":{},"updatedAt":1800}}`
	if got := string(p.data[settings.PrefContentReading]); got != want {
		t.Fatalf("带搜索词与目录的形状：\n  got  %s\n  want %s", got, want)
	}
}

// 最多留 100 个条目，满了淘汰最久没更新的；同一毫秒的按 ID 排（有确定的结果）。
func TestReadingMemoryKeepsTheNewest100(t *testing.T) {
	p := newMemPrefs()
	for i := 0; i < maxReadingEntries+1; i++ {
		if err := PatchReadingMemory(p, fmt.Sprintf("item-%03d", i), ReadingPatch{}, memAt(int64(1000+i))); err != nil {
			t.Fatal(err)
		}
	}
	store := readReadingStore(p, memAt(0))
	if len(store) != maxReadingEntries {
		t.Fatalf("条目数 %d，应为 %d", len(store), maxReadingEntries)
	}
	if _, ok := store["item-000"]; ok {
		t.Error("最旧的一条应被淘汰")
	}
	if _, ok := store["item-100"]; !ok {
		t.Error("最新的一条应保留")
	}

	// 再改一条旧的：它变成最新，被淘汰的换成次旧的。
	if err := PatchReadingMemory(p, "item-001", ReadingPatch{Tab: TabImages}, memAt(5000)); err != nil {
		t.Fatal(err)
	}
	if err := PatchReadingMemory(p, "fresh", ReadingPatch{}, memAt(5001)); err != nil {
		t.Fatal(err)
	}
	store = readReadingStore(p, memAt(0))
	if _, ok := store["item-001"]; !ok || store["item-001"].Tab != TabImages {
		t.Error("刚更新过的旧条目不该被淘汰")
	}
	if _, ok := store["item-002"]; ok {
		t.Error("次旧的一条应被淘汰")
	}

	tied := newMemPrefs()
	for i := 0; i < maxReadingEntries+1; i++ {
		if err := PatchReadingMemory(tied, fmt.Sprintf("t-%03d", i), ReadingPatch{}, memAt(7)); err != nil {
			t.Fatal(err)
		}
	}
	got := readReadingStore(tied, memAt(0))
	if _, ok := got["t-100"]; ok || len(got) != maxReadingEntries {
		t.Errorf("同一毫秒时 ID 大的先淘汰：%d 项，t-100 存在=%v", len(got), ok)
	}
}

// 没有偏好存储时什么都不做；写入失败把错误交给调用方，不吞掉也不 panic。
func TestReadingMemoryWithoutPrefsAndOnWriteFailure(t *testing.T) {
	if _, ok := LoadReadingMemory(nil, "a", memAt(1)); ok {
		t.Error("没有存储时没有记忆")
	}
	if err := PatchReadingMemory(nil, "a", ReadingPatch{Tab: TabBody}, memAt(1)); err != nil {
		t.Errorf("没有存储时不该报错：%v", err)
	}
	p := newMemPrefs()
	boom := errors.New("磁盘满")
	p.setErr = boom
	if err := PatchReadingMemory(p, "a", ReadingPatch{Tab: TabBody}, memAt(1)); !errors.Is(err, boom) {
		t.Errorf("写入失败应返回原错误：%v", err)
	}
}

// 六个标签都能存、能读回来；不认得的名字整项丢弃（见 TestReadingMemoryDropsBadData）。
func TestEveryPanelTabRoundTrips(t *testing.T) {
	for _, tab := range []ReadingTab{TabBody, TabTranscript, TabImages, TabRecognized, TabSummary, TabReplies} {
		p := newMemPrefs()
		if err := PatchReadingMemory(p, "a", ReadingPatch{Tab: tab, ScrollTopByTab: map[ReadingTab]float64{tab: 42}}, memAt(1)); err != nil {
			t.Fatal(err)
		}
		mem, ok := LoadReadingMemory(p, "a", memAt(2))
		if !ok || mem.Tab != tab || mem.ScrollTopByTab[tab] != 42 {
			t.Errorf("标签 %s 存取不回：%+v, %v", tab, mem, ok)
		}
	}
}
