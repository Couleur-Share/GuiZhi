package ask

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件对应 ask-history.store.ts：游标翻页、筛选、过时响应丢弃、刷新不「缩回去」、改标题与置顶。

type historyRig struct {
	t     *testing.T
	svc   *servicestest.AskFake
	store *HistoryStore
	log   *recLog
	notif *kit.Notifier
}

func newHistoryRig(t *testing.T, svc services.Ask, fake *servicestest.AskFake) *historyRig {
	t.Helper()
	r := &historyRig{t: t, svc: fake, log: &recLog{}, notif: kit.NewNotifier(nil)}
	r.store = NewHistoryStore(svc, kit.Guard{Notify: r.notif, Log: r.log}, nil)
	return r
}

func seedSessions(fake *servicestest.AskFake, n int) {
	for i := range n {
		rec := services.AskSessionRecord{MessagesJSON: "[]"}
		rec.ID, rec.Title = fmt.Sprintf("s%03d", i), fmt.Sprintf("title %03d", i)
		fake.PutSession(rec)
	}
}

func TestHistoryPagesThroughCursor(t *testing.T) {
	fake := &servicestest.AskFake{}
	seedSessions(fake, 120)
	r := newHistoryRig(t, fake, fake)
	r.store.Load(false)
	v := r.store.View()
	if len(v.Entries) != 50 || !v.HasMore || v.Loading || v.Error != "" {
		t.Fatalf("第一页 50 条：%d more=%v", len(v.Entries), v.HasMore)
	}
	r.store.Load(true)
	r.store.Load(true)
	v = r.store.View()
	if len(v.Entries) != 120 || v.HasMore {
		t.Fatalf("三页读完：%d more=%v", len(v.Entries), v.HasMore)
	}
	n := len(fake.Queries())
	r.store.Load(true)
	if len(fake.Queries()) != n {
		t.Fatal("没有更多时不再请求")
	}
	ids := map[string]bool{}
	for _, e := range v.Entries {
		ids[e.ID] = true
	}
	if len(ids) != 120 {
		t.Fatal("条目不应重复")
	}
}

func TestHistoryRefreshKeepsExpandedCount(t *testing.T) {
	fake := &servicestest.AskFake{}
	seedSessions(fake, 120)
	r := newHistoryRig(t, fake, fake)
	r.store.Load(false)
	r.store.Load(true)
	if got := len(r.store.View().Entries); got != 100 {
		t.Fatalf("翻开两页：%d", got)
	}
	r.store.Load(false) // 刷新：不能缩回第一页
	v := r.store.View()
	if len(v.Entries) != 100 || !v.HasMore {
		t.Fatalf("刷新后应补齐已翻开的条数：%d more=%v", len(v.Entries), v.HasMore)
	}
}

func TestHistoryFilterResetsAndPassesQuery(t *testing.T) {
	fake := &servicestest.AskFake{}
	seedSessions(fake, 60)
	art := services.AskSessionRecord{MessagesJSON: "[]"}
	art.ID, art.Title, art.Scope, art.ItemID, art.ArticleTitle = "art", "about item", services.AskScopeArticle, "item-1", "Item"
	fake.PutSession(art)
	r := newHistoryRig(t, fake, fake)
	r.store.Load(false)
	r.store.Filter(services.AskSessionQuery{Scope: services.AskScopeArticle, ItemID: "item-1"})
	v := r.store.View()
	if len(v.Entries) != 1 || v.Entries[0].ID != "art" || v.HasMore || v.Query.ItemID != "item-1" {
		t.Fatalf("%+v", v)
	}
	last := fake.Queries()[len(fake.Queries())-1]
	if last.Scope != services.AskScopeArticle || last.ItemID != "item-1" || last.Limit != 50 || last.Cursor != nil {
		t.Fatalf("筛选条件应原样传给服务，且从第一页开始：%+v", last)
	}
	if len(v.Articles) == 0 {
		t.Fatal("关联文章候选来自第一页，换筛选后仍应保留或更新")
	}
}

// slowAsk 让指定的查询卡住，用来制造「先发后到」。
type slowAsk struct {
	*servicestest.AskFake
	mu     sync.Mutex
	holds  map[string]chan struct{}
	waited map[string]chan struct{}
}

func (s *slowAsk) QuerySessions(ctx context.Context, q services.AskSessionQuery) (services.AskSessionPage, error) {
	s.mu.Lock()
	hold := s.holds[q.Search]
	arrived := s.waited[q.Search]
	s.mu.Unlock()
	if arrived != nil {
		close(arrived)
	}
	page, err := s.AskFake.QuerySessions(ctx, q)
	if hold != nil {
		<-hold // 结果已算好，晚点才交出去：模拟慢响应
		page.Entries = append(slices.Clone(page.Entries), services.AskSessionMeta{ID: "stale-" + q.Search})
	}
	return page, err
}

func TestHistoryStaleResponseIsDropped(t *testing.T) {
	fake := &servicestest.AskFake{}
	seedSessions(fake, 5)
	slow := &slowAsk{AskFake: fake, holds: map[string]chan struct{}{"old": make(chan struct{})}, waited: map[string]chan struct{}{"old": make(chan struct{})}}
	r := newHistoryRig(t, slow, fake)
	wait := goAsync(func() { r.store.Filter(services.AskSessionQuery{Search: "old"}) })
	<-slow.waited["old"]
	r.store.Filter(services.AskSessionQuery{Search: "new"}) // 新条件先返回
	newEntries := slices.Clone(r.store.View().Entries)
	close(slow.holds["old"])
	wait()
	v := r.store.View()
	for _, e := range v.Entries {
		if e.ID == "stale-old" {
			t.Fatalf("过时的响应混进了新结果：%+v", v.Entries)
		}
	}
	if v.Query.Search != "new" || v.Loading || !slices.EqualFunc(v.Entries, newEntries, func(a, b services.AskSessionMeta) bool { return a.ID == b.ID }) {
		t.Fatalf("过时的响应不能覆盖新结果，也不能把 Loading 弄乱：%+v", v)
	}
}

func TestHistoryErrorKeepsEntriesAndRetries(t *testing.T) {
	fake := &servicestest.AskFake{}
	seedSessions(fake, 3)
	r := newHistoryRig(t, fake, fake)
	r.store.Load(false)
	fake.Fail(servicestest.AskOpQuery, errBoom)
	r.store.Load(false)
	v := r.store.View()
	if v.Error != "boom" || v.Loading || len(v.Entries) != 3 {
		t.Fatalf("失败要有错误态并保留已有条目：%+v", v)
	}
	fake.Fail(servicestest.AskOpQuery, nil)
	r.store.Load(false)
	if v := r.store.View(); v.Error != "" || len(v.Entries) != 3 {
		t.Fatalf("重试应恢复：%+v", v)
	}
}

func TestHistoryLoadMoreIgnoredWhileLoading(t *testing.T) {
	fake := &servicestest.AskFake{}
	seedSessions(fake, 80)
	slow := &slowAsk{AskFake: fake, holds: map[string]chan struct{}{"": make(chan struct{})}, waited: map[string]chan struct{}{"": make(chan struct{})}}
	r := newHistoryRig(t, slow, fake)
	wait := goAsync(func() { r.store.Load(false) })
	<-slow.waited[""]
	if !r.store.View().Loading {
		t.Fatal("加载中应是 Loading")
	}
	r.store.Load(true) // 正在加载：忽略
	close(slow.holds[""])
	wait()
	if got := len(fake.Queries()); got != 1 {
		t.Fatalf("加载中不该再发请求：%d", got)
	}
	slow.mu.Lock()
	slow.holds, slow.waited = nil, nil
	slow.mu.Unlock()
}

// pagedAsk 按游标返回事先摆好的页，用来制造「翻页期间列表前移、边界上的条目出现两次」。
type pagedAsk struct {
	*servicestest.AskFake
}

func (pagedAsk) QuerySessions(_ context.Context, q services.AskSessionQuery) (services.AskSessionPage, error) {
	meta := func(id string) services.AskSessionMeta { return services.AskSessionMeta{ID: id, Title: id} }
	if q.Cursor == nil {
		next := "c1"
		return services.AskSessionPage{Entries: []services.AskSessionMeta{meta("a"), meta("b")}, NextCursor: &next}, nil
	}
	return services.AskSessionPage{Entries: []services.AskSessionMeta{meta("b"), meta("c")}}, nil
}

func TestHistoryLoadMoreDedupesBoundaryEntries(t *testing.T) {
	fake := &servicestest.AskFake{}
	r := newHistoryRig(t, pagedAsk{fake}, fake)
	r.store.Load(false)
	r.store.Load(true)
	var ids []string
	for _, e := range r.store.View().Entries {
		ids = append(ids, e.ID)
	}
	if !slices.Equal(ids, []string{"a", "b", "c"}) || r.store.View().HasMore {
		t.Fatalf("翻页合并按 ID 去重：%v", ids)
	}
}
func TestHistoryUpdateSuccessReloadsAndFailureToasts(t *testing.T) {
	fake := &servicestest.AskFake{}
	seedSessions(fake, 3)
	r := newHistoryRig(t, fake, fake)
	r.store.Load(false)
	title := "renamed"
	if !r.store.Update("s001", services.AskSessionMetaPatch{Title: &title}) {
		t.Fatal("改标题应成功")
	}
	found := false
	for _, e := range r.store.View().Entries {
		if e.ID == "s001" {
			found = e.Title == "renamed"
		}
	}
	if !found {
		t.Fatal("成功后应重读列表，显示新标题")
	}
	pin := true
	fake.Fail(servicestest.AskOpMeta, errBoom)
	if r.store.Update("s001", services.AskSessionMetaPatch{Pinned: &pin}) {
		t.Fatal("失败应返回 false")
	}
	got := r.notif.Queued()
	last := got[len(got)-1]
	if last.Kind != kit.ToastError || last.Message != locales.CommonOperationFailed(locales.AskActionUpdateSession()) || last.Detail != "boom" {
		t.Fatalf("失败要弹出带原因的提示：%+v", last)
	}
	if ms := fake.Metas(); len(ms) != 2 || ms[1].Patch.Pinned == nil {
		t.Fatalf("%+v", ms)
	}
}
