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
