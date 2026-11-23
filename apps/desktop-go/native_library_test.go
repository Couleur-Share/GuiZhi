package main

import (
	"encoding/json"
	"testing"

	"guizhi-mygo-shell/native/services"
)

// newLibraryAdapterForTest 在真实的临时库上登记知识库的处理器，返回接到它们的适配器。
func newLibraryAdapterForTest(t *testing.T) (libraryAdapter, *Bridge) {
	t.Helper()
	_, paths, db := openAdapterTestDB(t)
	b := &Bridge{handlers: map[string]handler{}, sends: map[string]func([]json.RawMessage){}, seen: map[string]int{}, missing: map[string]int{}, db: db}
	l := &Library{db: db, dbPath: paths.DatabasePath(), paths: paths}
	l.register(b)
	l.registerCatalogWrite(b)
	l.registerWrite(b)
	l.registerList(b)
	return libraryAdapter{b: b}, b
}

// 合集经真实处理器往返：创建带图标与不带图标的合集，列表按创建顺序给出；清掉图标后读回空串；
// 删除返回「是否真的删掉」，第二次删返回 false 而不是错误；删合集后其中的条目留作未归合集（计数随之变化）。
func TestLibraryAdapterCollectionsAgainstRealHandlers(t *testing.T) {
	ctx := t.Context()
	svc, b := newLibraryAdapterForTest(t)

	work, err := svc.CreateCollection(ctx, "工作", "💼")
	if err != nil || work.ID == "" || work.Name != "工作" || work.Icon != "💼" {
		t.Fatalf("创建带图标的合集：%+v %v", work, err)
	}
	plain, err := svc.CreateCollection(ctx, "随手记", "")
	if err != nil || plain.ID == "" || plain.Icon != "" {
		t.Fatalf("不带图标的合集应读回空串（数据库里是 NULL）：%+v %v", plain, err)
	}

	list, err := svc.Collections(ctx)
	if err != nil || len(list) != 2 || list[0].ID != work.ID || list[1].ID != plain.ID {
		t.Fatalf("列表应按创建顺序给出两个合集：%+v %v", list, err)
	}

	if err := svc.UpdateCollection(ctx, work.ID, "工作库", ""); err != nil {
		t.Fatalf("改名并清掉图标：%v", err)
	}
	list, _ = svc.Collections(ctx)
	if list[0].Name != "工作库" || list[0].Icon != "" {
		t.Fatalf("改名与清图标应落盘：%+v", list[0])
	}

	// 一个条目放进「工作库」，再删合集：条目留作未归合集。
	if _, err := callAs[struct {
		ID string `json:"id"`
	}](ctx, b, "knowledge:create", map[string]any{"title": "条目", "content": "正文", "collectionId": work.ID}); err != nil {
		t.Fatal(err)
	}
	before, err := svc.Counts(ctx, services.LibraryFacetQuery{Scope: services.LibraryScopeAll})
	if err != nil || before.All != 1 || before.Uncategorized != 0 || before.ByCollection[work.ID] != 1 {
		t.Fatalf("删合集前的计数：%+v %v", before, err)
	}
	removed, err := svc.DeleteCollection(ctx, work.ID)
	if err != nil || !removed {
		t.Fatalf("删除存在的合集应返回 true：%v %v", removed, err)
	}
	after, _ := svc.Counts(ctx, services.LibraryFacetQuery{Scope: services.LibraryScopeAll})
	if after.All != 1 || after.Uncategorized != 1 || len(after.ByCollection) != 0 {
		t.Fatalf("删合集后条目应留作未归合集：%+v", after)
	}
	removed, err = svc.DeleteCollection(ctx, work.ID)
	if err != nil || removed {
		t.Fatalf("再删一次应返回 false 而不是错误：%v %v", removed, err)
	}
}

// 标签与分面计数经真实处理器往返：标签按名称排序，行上的数字取分面计数（统计某一类分面时忽略同类条件，保留其余条件）。
func TestLibraryAdapterTagsAndFacetCountsAgainstRealHandlers(t *testing.T) {
	ctx := t.Context()
	svc, b := newLibraryAdapterForTest(t)

	col, err := svc.CreateCollection(ctx, "合集甲", "")
	if err != nil {
		t.Fatal(err)
	}
	create := func(title, collectionID string, tags ...string) {
		t.Helper()
		in := map[string]any{"title": title, "content": title, "tagNames": tags}
		if collectionID != "" {
			in["collectionId"] = collectionID
		}
		if _, err := callAs[json.RawMessage](ctx, b, "knowledge:create", in); err != nil {
			t.Fatal(err)
		}
	}
	create("甲", col.ID, "beta", "alpha")
	create("乙", "", "alpha")

	tags, err := svc.Tags(ctx)
	if err != nil || len(tags) != 2 || tags[0].Name != "alpha" || tags[1].Name != "beta" {
		t.Fatalf("标签应按名称排序：%+v %v", tags, err)
	}
	alpha, beta := tags[0], tags[1]

	all, err := svc.Counts(ctx, services.LibraryFacetQuery{Scope: services.LibraryScopeAll})
	if err != nil || all.All != 2 || all.ByTag[alpha.ID] != 2 || all.ByTag[beta.ID] != 1 || all.ByCollection[col.ID] != 1 {
		t.Fatalf("全部范围下的计数：%+v %v", all, err)
	}
	// 选中合集后：标签的数字只数合集里的条目（保留合集条件）；合集自己的数字忽略合集条件，仍是全局的。
	inCol, err := svc.Counts(ctx, services.LibraryFacetQuery{Scope: services.LibraryScopeAll, CollectionID: col.ID})
	if err != nil || inCol.ByTag[alpha.ID] != 1 || inCol.ByTag[beta.ID] != 1 || inCol.ByCollection[col.ID] != 1 {
		t.Fatalf("选中合集后的分面计数：%+v %v", inCol, err)
	}

	// 改名换色后重新读回，排序随名称变化。
	zeta, blue := "zeta", "blue"
	if err := svc.UpdateTag(ctx, alpha.ID, services.LibraryTagPatch{Name: &zeta, ColorKey: &blue}); err != nil {
		t.Fatalf("改名换色：%v", err)
	}
	tags, _ = svc.Tags(ctx)
	if len(tags) != 2 || tags[0].Name != "beta" || tags[1].Name != "zeta" || tags[1].ColorKey != "blue" {
		t.Fatalf("改名换色后的标签：%+v", tags)
	}
	// 只换色不改名：名称保持。
	pink := "pink"
	if err := svc.UpdateTag(ctx, beta.ID, services.LibraryTagPatch{ColorKey: &pink}); err != nil {
		t.Fatal(err)
	}
	tags, _ = svc.Tags(ctx)
	if tags[0].Name != "beta" || tags[0].ColorKey != "pink" {
		t.Fatalf("只换色不应改名：%+v", tags[0])
	}

	removed, err := svc.DeleteTag(ctx, beta.ID)
	if err != nil || !removed {
		t.Fatalf("删除存在的标签应返回 true：%v %v", removed, err)
	}
	removed, err = svc.DeleteTag(ctx, beta.ID)
	if err != nil || removed {
		t.Fatalf("再删一次应返回 false 而不是错误：%v %v", removed, err)
	}
}

// 条目列表经真实处理器往返：DTO 与处理器输出对得上（合集、标签按名称排序、置顶与收藏、范围的含义），
// 置顶条目排在最前，游标翻页取到不重叠的两页，换了筛选之后旧游标被拒绝（界面必须在筛选变化时丢掉游标）。
func TestLibraryAdapterListAgainstRealHandlers(t *testing.T) {
	ctx := t.Context()
	svc, b := newLibraryAdapterForTest(t)

	col, err := svc.CreateCollection(ctx, "合集甲", "")
	if err != nil {
		t.Fatal(err)
	}
	create := func(in map[string]any) string {
		t.Helper()
		item, err := callAs[struct {
			ID string `json:"id"`
		}](ctx, b, "knowledge:create", in)
		if err != nil || item.ID == "" {
			t.Fatalf("创建条目 %v：%v", in, err)
		}
		return item.ID
	}
	patch := func(id string, in map[string]any) {
		t.Helper()
		if _, err := b.call(ctx, "knowledge:update", id, in); err != nil {
			t.Fatalf("修改条目 %s %v：%v", id, in, err)
		}
	}
	first := create(map[string]any{"title": "甲", "content": "关于量子退火的笔记", "collectionId": col.ID, "tagNames": []string{"beta", "alpha"}})
	pinned := create(map[string]any{"title": "乙", "content": "乙的正文", "tagNames": []string{"alpha"}})
	patch(pinned, map[string]any{"isPinned": true})
	starred := create(map[string]any{"title": "丙", "content": "丙的正文"})
	patch(starred, map[string]any{"isFavorite": true})
	archived := create(map[string]any{"title": "丁", "content": "丁的正文"})
	patch(archived, map[string]any{"status": "archived"})

	ids := func(p services.LibraryPage) []string {
		out := make([]string, len(p.Entries))
		for i, e := range p.Entries {
			out[i] = e.ID
		}
		return out
	}

	all, err := svc.List(ctx, services.LibraryListQuery{Scope: services.LibraryScopeAll, Limit: 10})
	if err != nil || all.Total != 3 || len(all.Entries) != 3 || all.NextCursor != "" {
		t.Fatalf("全部范围（不含归档）：%+v %v", all, err)
	}
	if all.Entries[0].ID != pinned || !all.Entries[0].IsPinned {
		t.Fatalf("置顶条目应排在最前：%v", ids(all))
	}
	var got services.LibraryEntry
	for _, e := range all.Entries {
		if e.ID == first {
			got = e
		}
	}
	if got.Title != "甲" || got.ItemType != services.LibraryTypeNote || got.Status != services.LibraryStatusActive ||
		got.CollectionID != col.ID || got.IsPinned || got.IsFavorite || got.DeletedAt != 0 || got.CreatedAt == 0 || got.UpdatedAt == 0 {
		t.Fatalf("字段映射：%+v", got)
	}
	if len(got.Tags) != 2 || got.Tags[0].Name != "alpha" || got.Tags[1].Name != "beta" || got.Tags[0].ID == "" || got.Tags[0].ColorKey == "" {
		t.Fatalf("标签应按名称排序并带上颜色：%+v", got.Tags)
	}
	if got.ReviewStatus != "clear" || got.ReviewReasons == nil {
		t.Fatalf("复核状态缺省为 clear，原因不为 nil：%q %v", got.ReviewStatus, got.ReviewReasons)
	}

	for _, c := range []struct {
		name  string
		query services.LibraryListQuery
		want  []string
	}{
		{"收藏", services.LibraryListQuery{Scope: services.LibraryScopeFavorites}, []string{starred}},
		{"归档", services.LibraryListQuery{Scope: services.LibraryScopeArchived}, []string{archived}},
		{"未归合集（不含归档）", services.LibraryListQuery{Scope: services.LibraryScopeUncategorized}, []string{pinned, starred}},
		{"合集分面", services.LibraryListQuery{Scope: services.LibraryScopeAll, CollectionID: col.ID}, []string{first}},
		{"搜索", services.LibraryListQuery{Scope: services.LibraryScopeAll, Search: "量子退火"}, []string{first}},
	} {
		c.query.Limit = 10
		page, err := svc.List(ctx, c.query)
		if err != nil || len(page.Entries) != len(c.want) || page.Total != len(c.want) {
			t.Fatalf("%s：%+v %v", c.name, page, err)
		}
		for i, id := range c.want {
			if page.Entries[i].ID != id {
				t.Fatalf("%s：应为 %v，实际 %v", c.name, c.want, ids(page))
			}
		}
	}

	// 游标翻页：两页不重叠、总数不变；换了筛选之后旧游标被拒绝。
	one, err := svc.List(ctx, services.LibraryListQuery{Scope: services.LibraryScopeAll, Limit: 2})
	if err != nil || len(one.Entries) != 2 || one.Total != 3 || one.NextCursor == "" {
		t.Fatalf("第一页：%+v %v", one, err)
	}
	two, err := svc.List(ctx, services.LibraryListQuery{Scope: services.LibraryScopeAll, Limit: 2, Cursor: one.NextCursor})
	if err != nil || len(two.Entries) != 1 || two.Total != 3 || two.NextCursor != "" {
		t.Fatalf("第二页：%+v %v", two, err)
	}
	for _, e := range two.Entries {
		for _, prev := range one.Entries {
			if e.ID == prev.ID {
				t.Fatalf("两页不应重叠：%v / %v", ids(one), ids(two))
			}
		}
	}
	if _, err := svc.List(ctx, services.LibraryListQuery{Scope: services.LibraryScopeFavorites, Limit: 2, Cursor: one.NextCursor}); err == nil {
		t.Fatal("换了筛选之后，旧游标应被拒绝")
	}
}

// 搜索模式经真实处理器往返：命令面板用召回式（多词取并集），默认的短语式要求词连在一起；
// 空的 SearchMode 保持处理器的默认，不改变既有列表的行为。
func TestLibraryAdapterListSearchModeAgainstRealHandlers(t *testing.T) {
	ctx := t.Context()
	svc, b := newLibraryAdapterForTest(t)
	for _, in := range []map[string]any{
		{"title": "甲", "content": "关于量子退火的笔记"},
		{"title": "乙", "content": "咖啡豆的烘焙曲线"},
		{"title": "丙", "content": "与搜索词无关的正文"},
	} {
		if _, err := b.call(ctx, "knowledge:create", in); err != nil {
			t.Fatalf("创建条目 %v：%v", in, err)
		}
	}
	titles := func(q services.LibraryListQuery) []string {
		t.Helper()
		q.Scope, q.Limit = services.LibraryScopeAll, 10
		page, err := svc.List(ctx, q)
		if err != nil {
			t.Fatalf("搜索 %+v：%v", q, err)
		}
		out := make([]string, len(page.Entries))
		for i, e := range page.Entries {
			out[i] = e.Title
		}
		return out
	}
	const query = "量子退火 咖啡豆"
	if got := titles(services.LibraryListQuery{Search: query}); len(got) != 0 {
		t.Fatalf("短语式不应命中分散在两条里的两个词：%v", got)
	}
	got := titles(services.LibraryListQuery{Search: query, SearchMode: services.LibrarySearchRecall})
	if len(got) != 2 || got[0] == "丙" || got[1] == "丙" {
		t.Fatalf("召回式应命中甲与乙：%v", got)
	}
}
