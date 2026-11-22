package wiki

import (
	"slices"
	"testing"

	"guizhi-mygo-shell/native/services"
)

func entry(id, title, kind string, updated int64, manual bool) services.WikiCatalogEntry {
	e := services.WikiCatalogEntry{ID: id, Title: title, Kind: kind, UpdatedAt: updated}
	if manual {
		at := updated
		e.ManualEditedAt = &at
	}
	return e
}

func sampleCatalog() ([]services.WikiCatalogEntry, services.WikiBacklinkCounts) {
	return []services.WikiCatalogEntry{
		entry("a", "Alpha", services.WikiKindTopic, 10, false),
		entry("b", "beta", services.WikiKindEntity, 30, true),
		entry("c", "Gamma", services.WikiKindConcept, 20, false),
		entry("d", "Delta", services.WikiKindTopic, 20, true),
	}, services.WikiBacklinkCounts{"a": 3, "c": 1}
}

func TestCountByFilter(t *testing.T) {
	cat, links := sampleCatalog()
	got := CountByFilter(cat, links)
	want := CatalogCounts{All: 4, Topic: 2, Entity: 1, Concept: 1, Manual: 2, Orphan: 2}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
	for _, f := range Filters {
		if got.For(f) != map[CatalogFilter]int{FilterAll: 4, FilterTopic: 2, FilterEntity: 1, FilterConcept: 1, FilterManual: 2, FilterOrphan: 2}[f] {
			t.Fatalf("For(%s)=%d", f, got.For(f))
		}
	}
	if (CatalogCounts{All: 7}).For("unknown") != 7 {
		t.Fatal("未知筛选档按「全部」计")
	}
}

func TestCountByFilterEdgeCases(t *testing.T) {
	if got := CountByFilter(nil, nil); got != (CatalogCounts{}) {
		t.Fatalf("空目录全是 0：%+v", got)
	}
	got := CountByFilter([]services.WikiCatalogEntry{entry("x", "X", "mystery", 1, false)}, nil)
	if got.All != 1 || got.Topic+got.Entity+got.Concept != 0 || got.Orphan != 1 {
		t.Fatalf("未知类型只计入全部与孤立：%+v", got)
	}
}

func TestMatchesFilter(t *testing.T) {
	cat, links := sampleCatalog()
	cases := map[CatalogFilter][]string{
		FilterAll:     {"a", "b", "c", "d"},
		FilterTopic:   {"a", "d"},
		FilterEntity:  {"b"},
		FilterConcept: {"c"},
		FilterManual:  {"b", "d"},
		FilterOrphan:  {"b", "d"},
	}
	for f, want := range cases {
		var got []string
		for _, e := range cat {
			if MatchesFilter(e, f, links) {
				got = append(got, e.ID)
			}
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s: got %v want %v", f, got, want)
		}
	}
}

func TestSelectVisibleSorts(t *testing.T) {
	cat, links := sampleCatalog()
	base := VisibleParams{Catalog: cat, BacklinkCounts: links, Filter: FilterAll}
	cases := map[CatalogSort][]string{
		SortRecent: {"b", "c", "d", "a"},
		SortLinked: {"a", "c", "b", "d"}, // 被引用多的在前；同为 0 的按更新时间倒序
		SortTitle:  {"a", "b", "d", "c"}, // Alpha beta Delta Gamma：不区分大小写
	}
	for sort, want := range cases {
		p := base
		p.Sort = sort
		wantIDs(t, SelectVisible(p), want...)
	}
	p := base
	p.Sort = "bogus"
	wantIDs(t, SelectVisible(p), "b", "c", "d", "a") // 未知排序轴回退为最近更新
}

func TestSelectVisibleStableAndDoesNotMutate(t *testing.T) {
	cat := []services.WikiCatalogEntry{
		entry("1", "Same", services.WikiKindTopic, 5, false),
		entry("2", "Same", services.WikiKindTopic, 5, false),
		entry("3", "same", services.WikiKindTopic, 5, false),
	}
	before := slices.Clone(cat)
	for _, sort := range Sorts {
		got := SelectVisible(VisibleParams{Catalog: cat, Filter: FilterAll, Sort: sort})
		if sort == SortTitle {
			wantIDs(t, got, "1", "2", "3") // 大小写变体相邻，且同名保持原有顺序
		} else {
			wantIDs(t, got, "1", "2", "3") // 同分保持原有顺序
		}
	}
	if !slices.Equal(ids(cat), ids(before)) {
		t.Fatal("入参不应被改动")
	}
}

func TestSelectVisibleFilterThenSort(t *testing.T) {
	cat, links := sampleCatalog()
	got := SelectVisible(VisibleParams{Catalog: cat, BacklinkCounts: links, Filter: FilterTopic, Sort: SortTitle})
	wantIDs(t, got, "a", "d")
	got = SelectVisible(VisibleParams{Catalog: cat, BacklinkCounts: links, Filter: FilterOrphan, Sort: SortRecent})
	wantIDs(t, got, "b", "d")
}

func TestSelectVisibleSearchKeepsRankOrderAndSkipsSort(t *testing.T) {
	cat, links := sampleCatalog()
	p := VisibleParams{Catalog: cat, BacklinkCounts: links, Filter: FilterAll, Sort: SortTitle, SearchActive: true, SearchHitIDs: []string{"c", "a", "d"}}
	wantIDs(t, SelectVisible(p), "c", "a", "d") // 相关度顺序，不按标题重排
	p.Sort = SortRecent
	wantIDs(t, SelectVisible(p), "c", "a", "d")
}

func TestSelectVisibleSearchIntersectsFilter(t *testing.T) {
	cat, links := sampleCatalog()
	p := VisibleParams{Catalog: cat, BacklinkCounts: links, Filter: FilterTopic, SearchActive: true, SearchHitIDs: []string{"c", "d", "a", "b"}}
	wantIDs(t, SelectVisible(p), "d", "a") // 命中集合先限定范围，再套筛选轴
}

func TestSelectVisibleSearchEdgeCases(t *testing.T) {
	cat, links := sampleCatalog()
	zero := VisibleParams{Catalog: cat, BacklinkCounts: links, Filter: FilterAll, SearchActive: true, SearchHitIDs: []string{}}
	if got := SelectVisible(zero); len(got) != 0 {
		t.Fatalf("搜到零条应是空列表，不能回退成整个目录：%v", ids(got))
	}
	zero.SearchHitIDs = nil
	if got := SelectVisible(zero); len(got) != 0 {
		t.Fatalf("搜索中命中集合为 nil 同样是零条：%v", ids(got))
	}
	ghost := VisibleParams{Catalog: cat, Filter: FilterAll, SearchActive: true, SearchHitIDs: []string{"ghost", "a", "a", "c"}}
	wantIDs(t, SelectVisible(ghost), "a", "c") // 不在目录里的命中忽略，重复命中按第一次出现的名次
	notSearching := VisibleParams{Catalog: cat, BacklinkCounts: links, Filter: FilterAll, Sort: SortRecent, SearchHitIDs: []string{"a"}}
	if len(SelectVisible(notSearching)) != 4 {
		t.Fatal("SearchActive 为 false 时忽略命中集合")
	}
}

func TestSelectVisibleEmpty(t *testing.T) {
	if got := SelectVisible(VisibleParams{}); got == nil || len(got) != 0 {
		t.Fatalf("空目录返回空切片：%#v", got)
	}
}

func TestCompileButtonState(t *testing.T) {
	cases := []struct {
		name       string
		counts     *services.WikiCompileCounts
		pending    int
		actionable bool
	}{
		{"预览未知时不拦", nil, 0, true},
		{"只算可执行", &services.WikiCompileCounts{Ready: 4, Upgrade: 1}, 4, true},
		{"只有待升级也能打开", &services.WikiCompileCounts{Upgrade: 2}, 0, true},
		{"只有待复核也能打开", &services.WikiCompileCounts{Review: 1}, 0, true},
		{"只有等待中也能打开", &services.WikiCompileCounts{Waiting: 1}, 0, true},
		{"已最新与排除不算", &services.WikiCompileCounts{Current: 9, Excluded: 3}, 0, false},
		{"全零", &services.WikiCompileCounts{}, 0, false},
	}
	for _, c := range cases {
		pending, actionable := CompileButtonState(c.counts)
		if pending != c.pending || actionable != c.actionable {
			t.Errorf("%s: got (%d,%v) want (%d,%v)", c.name, pending, actionable, c.pending, c.actionable)
		}
	}
}

func TestCompareTitleOrdersCaseInsensitively(t *testing.T) {
	if compareTitle("apple", "Banana") >= 0 || compareTitle("Banana", "apple") <= 0 {
		t.Fatal("大小写不敏感比较")
	}
	if compareTitle("a", "A") == 0 || compareTitle("A", "A") != 0 {
		t.Fatal("同名大小写变体要有稳定的先后，完全相同才算相等")
	}
}
