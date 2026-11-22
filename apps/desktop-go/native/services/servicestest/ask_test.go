package servicestest

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"guizhi-mygo-shell/aiflow"
	"guizhi-mygo-shell/native/services"
)

func askArticleSave(id, itemID string) services.SaveAskSessionInput {
	web := false
	return services.SaveAskSessionInput{
		ID: id, Title: "t-" + id, MessagesJSON: "[]", Scope: services.AskScopeArticle, ItemID: itemID, ArticleTitle: "A-" + itemID,
		WebEnabled: &web, Target: &aiflow.ArticleTarget{ItemID: itemID, View: "body"},
	}
}

func TestAskFakeSaveGetListAndTitles(t *testing.T) {
	ctx := t.Context()
	f := &AskFake{}
	first, err := f.SaveSession(ctx, services.SaveAskSessionInput{ID: "s1", Title: "one", MessagesJSON: `[{"question":"q"}]`})
	if err != nil || first.CreatedAt == 0 || first.UpdatedAt != first.CreatedAt {
		t.Fatalf("首次保存：%+v %v", first, err)
	}
	again, err := f.SaveSession(ctx, services.SaveAskSessionInput{ID: "s1", Title: "one-b", MessagesJSON: "[]"})
	if err != nil || again.CreatedAt != first.CreatedAt || again.UpdatedAt <= first.UpdatedAt {
		t.Fatalf("再次保存应保留创建时间并推进更新时间：%+v %v", again, err)
	}
	if _, err := f.SaveSession(ctx, askArticleSave("a1", "item-1")); err != nil {
		t.Fatal(err)
	}
	bad := askArticleSave("a2", "item-1")
	bad.Target.ItemID = "other"
	if _, err := f.SaveSession(ctx, bad); err == nil {
		t.Fatal("本文会话的目标与文章不一致应被拒绝")
	}

	got, err := f.GetSession(ctx, "a1")
	if err != nil || got == nil || !got.IsArticle() || got.WebEnabledOrDefault() || got.Target == nil {
		t.Fatalf("读本文会话：%+v %v", got, err)
	}
	if missing, err := f.GetSession(ctx, "nope"); err != nil || missing != nil {
		t.Fatalf("不存在应返回 nil：%v %v", missing, err)
	}
	list, _ := f.ListSessions(ctx, services.AskSessionFilter{Scope: services.AskScopeArticle})
	if len(list) != 1 || list[0].ID != "a1" {
		t.Fatalf("按范围筛选：%+v", list)
	}
	if list, _ = f.ListSessions(ctx, services.AskSessionFilter{}); len(list) != 2 || list[0].ID != "a1" {
		t.Fatalf("最近更新的在前：%+v", list)
	}

	if ok, _ := f.DeleteSession(ctx, "s1"); !ok || f.SessionCount() != 1 {
		t.Fatal("删除应成功")
	}
	if ok, _ := f.DeleteSession(ctx, "s1"); ok {
		t.Fatal("重复删除应返回 false")
	}
	if len(f.Saves()) != 4 || len(f.Deletes()) != 2 || len(f.Gets()) != 2 || len(f.Lists()) != 2 {
		t.Fatalf("调用记录不对：saves=%d deletes=%d gets=%d lists=%d", len(f.Saves()), len(f.Deletes()), len(f.Gets()), len(f.Lists()))
	}
}

func TestAskFakeQueryPagingCursorAndArticles(t *testing.T) {
	ctx := t.Context()
	f := &AskFake{}
	for _, id := range []string{"s1", "s2", "s3"} {
		if _, err := f.SaveSession(ctx, services.SaveAskSessionInput{ID: id, Title: id, MessagesJSON: "[]"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.SaveSession(ctx, askArticleSave("a1", "item-1")); err != nil {
		t.Fatal(err)
	}
	page, err := f.QuerySessions(ctx, services.AskSessionQuery{Limit: 3})
	if err != nil || len(page.Entries) != 3 || page.NextCursor == nil || len(page.Articles) != 1 || page.Articles[0].ID != "item-1" {
		t.Fatalf("第一页：%+v %v", page, err)
	}
	rest, err := f.QuerySessions(ctx, services.AskSessionQuery{Limit: 3, Cursor: page.NextCursor})
	if err != nil || len(rest.Entries) != 1 || rest.NextCursor != nil || rest.Articles != nil {
		t.Fatalf("第二页没有游标与文章列表：%+v %v", rest, err)
	}
	if _, err := f.QuerySessions(ctx, services.AskSessionQuery{Limit: 3, Search: "x", Cursor: page.NextCursor}); err == nil {
		t.Fatal("筛选变化后旧游标应报错")
	}
	byScope, _ := f.QuerySessions(ctx, services.AskSessionQuery{Scope: services.AskScopeKnowledge})
	if len(byScope.Entries) != 3 {
		t.Fatalf("按范围筛选：%+v", byScope.Entries)
	}
	bySearch, _ := f.QuerySessions(ctx, services.AskSessionQuery{Search: "S2"})
	if len(bySearch.Entries) != 1 || bySearch.Entries[0].ID != "s2" {
		t.Fatalf("搜索应不分大小写命中标题：%+v", bySearch.Entries)
	}
	rec3, _ := f.Session("s3")
	from := rec3.UpdatedAt
	byFrom, _ := f.QuerySessions(ctx, services.AskSessionQuery{From: &from})
	if len(byFrom.Entries) != 2 {
		t.Fatalf("按更新时间下界筛选：%+v", byFrom.Entries)
	}
}

func TestAskFakeMetaPinnedRenamedAndFailures(t *testing.T) {
	ctx := t.Context()
	f := &AskFake{}
	for _, id := range []string{"s1", "s2"} {
		if _, err := f.SaveSession(ctx, services.SaveAskSessionInput{ID: id, Title: id, MessagesJSON: "[]"}); err != nil {
			t.Fatal(err)
		}
	}
	title, pinned := "  renamed  ", true
	rec, err := f.UpdateSessionMeta(ctx, "s1", services.AskSessionMetaPatch{Title: &title, Pinned: &pinned})
	if err != nil || rec.Title != "renamed" || !rec.Pinned {
		t.Fatalf("改标题与置顶：%+v %v", rec, err)
	}
	saved, _ := f.SaveSession(ctx, services.SaveAskSessionInput{ID: "s1", Title: "auto", MessagesJSON: "[]"})
	if saved.Title != "renamed" {
		t.Fatalf("保存不应覆盖改过的标题：%q", saved.Title)
	}
	if list, _ := f.ListSessions(ctx, services.AskSessionFilter{}); list[0].ID != "s1" {
		t.Fatalf("置顶的排在最前：%+v", list)
	}
	empty := " "
	if _, err := f.UpdateSessionMeta(ctx, "s1", services.AskSessionMetaPatch{Title: &empty}); err == nil {
		t.Fatal("空标题应被拒绝")
	}
	if _, err := f.UpdateSessionMeta(ctx, "nope", services.AskSessionMetaPatch{Pinned: &pinned}); err == nil {
		t.Fatal("会话不存在应报错")
	}

	boom := errors.New("disk full")
	f.Fail(AskOpSave, boom)
	if _, err := f.SaveSession(ctx, services.SaveAskSessionInput{ID: "s9", Title: "x", MessagesJSON: "[]"}); !errors.Is(err, boom) {
		t.Fatalf("注入失败：%v", err)
	}
	if _, ok := f.Session("s9"); ok {
		t.Fatal("失败的保存不应改状态")
	}
	f.Fail(AskOpSave, nil)
	f.FailSaves(2, boom)
	for i := range 3 {
		_, err := f.SaveSession(ctx, services.SaveAskSessionInput{ID: "s9", Title: "x", MessagesJSON: "[]"})
		if (i < 2) != (err != nil) {
			t.Fatalf("前两次保存失败、第三次成功，第 %d 次：%v", i+1, err)
		}
	}
}

func TestAskFakeHoldSaveBlocksUntilReleasedOrCanceled(t *testing.T) {
	f := &AskFake{}
	release := f.HoldSave()
	done := make(chan error, 1)
	go func() {
		_, err := f.SaveSession(t.Context(), services.SaveAskSessionInput{ID: "s1", Title: "x", MessagesJSON: "[]"})
		done <- err
	}()
	select {
	case <-done:
		t.Fatal("放行之前保存不应返回")
	case <-time.After(30 * time.Millisecond):
	}
	if _, ok := f.Session("s1"); ok {
		t.Fatal("卡住时不应已经写入")
	}
	release()
	if err := <-done; err != nil || f.SessionCount() != 1 {
		t.Fatalf("放行后应写入：%v", err)
	}

	f.HoldSave()
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		_, err := f.SaveSession(ctx, services.SaveAskSessionInput{ID: "s2", Title: "x", MessagesJSON: "[]"})
		done <- err
	}()
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("取消应让卡住的保存返回：%v", err)
	}
}

func TestAskFakeConfigAndItems(t *testing.T) {
	ctx := t.Context()
	f := &AskFake{}
	if ok, _ := f.QAConfigured(ctx); !ok {
		t.Fatal("默认问答已配置")
	}
	if ok, _ := f.WebSearchConfigured(ctx); !ok {
		t.Fatal("默认联网搜索已配置")
	}
	f.SetQAConfigured(false)
	f.SetWebSearchConfigured(false)
	if ok, _ := f.QAConfigured(ctx); ok {
		t.Fatal("应能设为未配置")
	}
	if ok, _ := f.WebSearchConfigured(ctx); ok {
		t.Fatal("应能设为未配置")
	}
	f.PutItemTitle("i1", "Title")
	f.PutItem("i3", services.AskItem{Title: "Gone", Deleted: true})
	if item, found, _ := f.ItemInfo(ctx, "i3"); !found || !item.Deleted || item.Title != "Gone" {
		t.Fatalf("已删除的条目仍读得到：%+v %v", item, found)
	}
	if item, found, _ := f.ItemInfo(ctx, "i1"); !found || item.Deleted || item.Title != "Title" {
		t.Fatalf("摆好的标题：%+v %v", item, found)
	}
	if _, found, _ := f.ItemInfo(ctx, "i2"); found {
		t.Fatal("没摆的条目按不存在处理")
	}
	f.Fail(AskOpItemInfo, errors.New("boom"))
	if _, _, err := f.ItemInfo(ctx, "i1"); err == nil {
		t.Fatal("应注入失败")
	}
}

func TestAskFakeSanitizeClearsEvidenceOfClearedItems(t *testing.T) {
	f := &AskFake{}
	in := `[{"id":"m1","answer":"keep [1]","sources":[{"ordinal":1,"kind":"item","refId":"gone","title":"T","evidence":{"kind":"item","text":"body","capturedAt":5}},` +
		`{"ordinal":2,"kind":"item","refId":"kept","title":"K"}],"evidenceSources":[{"ordinal":3,"kind":"wiki","refId":"w","title":"W","evidence":{"kind":"wiki","sourceItemIds":["gone"],"text":"x"}}]}]`
	out, err := f.SanitizeConversation(in, []string{"gone"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, `"body"`) || strings.Contains(out, `"text":"x"`) || !strings.Contains(out, `"answer":"keep [1]"`) || !strings.Contains(out, `"title":"K"`) {
		t.Fatalf("应只清掉来自已清除条目的证据：%s", out)
	}
	if strings.Count(out, `"cleared":true`) < 4 { // 两个来源各一层，证据各一层
		t.Fatalf("清掉的来源与证据都应带 cleared 标记：%s", out)
	}
	if calls := f.Sanitizes(); len(calls) != 1 || calls[0].Cleared[0] != "gone" {
		t.Fatalf("调用记录：%+v", calls)
	}
	if _, err := f.SanitizeConversation("not json", nil); err == nil {
		t.Fatal("非法 JSON 应报错")
	}
}

func TestAskFakeKnowledgeScriptStreamsAndRecords(t *testing.T) {
	f := &AskFake{}
	src := services.AskSource{Ordinal: 1, Kind: "item", RefID: "i1", Title: "T"}
	f.QueueAsk(AskScript{
		Steps: []string{"search", "read"}, Warnings: []string{"degraded"}, Evidence: [][]services.AskSource{{src}},
		Texts: []string{"he", "hello"}, Answer: services.AskAnswer{Text: "hello", Sources: []services.AskSource{src}, Model: "m", Truncated: true},
	})
	var steps, texts, warns []string
	var evidence int
	answer, err := f.AskKnowledge(t.Context(), services.AskRequest{Question: "q", History: []services.AskTurn{{Question: "a", Answer: "b"}}}, services.AskCallbacks{
		OnStep: func(s string) { steps = append(steps, s) }, OnText: func(s string) { texts = append(texts, s) },
		OnWarning: func(s string) { warns = append(warns, s) }, OnEvidence: func(s []services.AskSource) { evidence += len(s) },
	})
	if err != nil || answer.Text != "hello" || !answer.Truncated || len(answer.Sources) != 1 {
		t.Fatalf("结果：%+v %v", answer, err)
	}
	if strings.Join(steps, ",") != "search,read" || strings.Join(texts, ",") != "he,hello" || len(warns) != 1 || evidence != 1 {
		t.Fatalf("推送：steps=%v texts=%v warns=%v evidence=%d", steps, texts, warns, evidence)
	}
	// 队列用完后走默认脚本：回显。
	echo, err := f.AskKnowledge(t.Context(), services.AskRequest{Question: "next"}, services.AskCallbacks{})
	if err != nil || echo.Text != "echo:next" {
		t.Fatalf("默认脚本：%+v %v", echo, err)
	}
	if calls := f.AskCalls(); len(calls) != 2 || calls[0].Question != "q" || len(calls[0].History) != 1 {
		t.Fatalf("调用记录：%+v", calls)
	}
	boom := errors.New("model failed")
	f.QueueAsk(AskScript{Err: boom})
	if _, err := f.AskKnowledge(t.Context(), services.AskRequest{Question: "q"}, services.AskCallbacks{}); !errors.Is(err, boom) {
		t.Fatalf("脚本里的错误应原样返回：%v", err)
	}
}

func TestAskFakeKnowledgeCancelWhileGatedStillEmitsLateTexts(t *testing.T) {
	f := &AskFake{}
	gate := make(chan struct{})
	f.QueueAsk(AskScript{Texts: []string{"partial"}, Gate: gate, After: []string{"late"}})
	ctx, cancel := context.WithCancel(t.Context())
	var mu sync.Mutex
	var texts []string
	started := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		_, err := f.AskKnowledge(ctx, services.AskRequest{Question: "q"}, services.AskCallbacks{OnText: func(s string) {
			mu.Lock()
			texts = append(texts, s)
			mu.Unlock()
			select {
			case started <- struct{}{}:
			default:
			}
		}})
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("取消应按 context.Canceled 结束：%v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(texts, ",") != "partial,late" {
		t.Fatalf("取消之后迟到的文本应照样推送：%v", texts)
	}
}

func TestAskFakeArticleScriptPatchesGateAndDefault(t *testing.T) {
	f := &AskFake{}
	step := "read"
	f.QueueArticle(ArticleScript{Patches: []aiflow.ArticlePatch{{Step: &step}}, Err: errors.New("article failed")})
	var patches []aiflow.ArticlePatch
	collect := func(p aiflow.ArticlePatch) { patches = append(patches, p) }
	req := services.ArticleAskRequest{Question: "q", Target: aiflow.ArticleTarget{ItemID: "i1", View: "body"}, RequestID: "r1"}
	if err := f.AskArticle(t.Context(), req, collect); err == nil || len(patches) != 1 || *patches[0].Step != "read" {
		t.Fatalf("脚本的补丁与错误：%v %+v", err, patches)
	}
	patches = nil
	if err := f.AskArticle(t.Context(), req, collect); err != nil || len(patches) != 1 || *patches[0].Answer != "echo:q" || *patches[0].Status != "done" || !patches[0].ClearStep {
		t.Fatalf("默认脚本应回显并完成：%v %+v", err, patches)
	}

	gate := make(chan struct{})
	late := "late"
	f.QueueArticle(ArticleScript{Gate: gate, After: []aiflow.ArticlePatch{{Answer: &late}}})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	patches = nil
	if err := f.AskArticle(ctx, req, collect); !errors.Is(err, context.Canceled) || len(patches) != 1 {
		t.Fatalf("取消应结束卡住的执行并推送迟到的补丁：%v %+v", err, patches)
	}
	if calls := f.ArticleCalls(); len(calls) != 3 || calls[0].RequestID != "r1" {
		t.Fatalf("调用记录：%+v", calls)
	}
}
