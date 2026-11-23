package overlays

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

type paletteRig struct {
	*rig
	p       *Palette
	lib     *servicestest.Library
	wiki    *servicestest.WikiFake
	imports *servicestest.Imports
	backup  *servicestest.Backup
	calls   []string // 钩子调用记录
}

func newPaletteRig(t *testing.T) *paletteRig {
	t.Helper()
	pr := &paletteRig{rig: newRig(t), lib: &servicestest.Library{}, wiki: &servicestest.WikiFake{}, imports: &servicestest.Imports{}, backup: &servicestest.Backup{}}
	p := NewPalette(pr.guard, pr.invalidate)
	p.Env = pr.env()
	p.Library, p.Wiki, p.Imports, p.Backup = pr.lib, pr.wiki, pr.imports, pr.backup
	rec := func(name string) func() { return func() { pr.calls = append(pr.calls, name) } }
	rec1 := func(name string) func(string) {
		return func(arg string) { pr.calls = append(pr.calls, name+":"+arg) }
	}
	p.Hooks = PaletteHooks{
		QuickCapture: rec("capture"), OpenInbox: rec("inbox"), BuildSemanticIndex: rec("index"), CompileWiki: rec("wiki-compile"),
		OpenLibraryItem: rec1("item"), OpenWikiPage: rec1("wiki"), SearchImportTasks: rec1("task"), OpenSettingsSection: rec1("settings"),
	}
	pr.p = p
	pr.p.Open()
	return pr
}

func keys(v PaletteView) []string {
	out := make([]string, len(v.Items))
	for i, it := range v.Items {
		out[i] = it.Key
	}
	return out
}

func (pr *paletteRig) seedRemote() {
	pr.lib.SeedEntries([]services.LibraryEntry{{ID: "i1", Title: "alpha note", Snippet: "first"}, {ID: "i2", Title: "other", Snippet: "alpha inside"}})
	pr.wiki.PutPage(services.WikiPage{ID: "w1", Title: "alpha wiki", Summary: "wiki summary"})
	pr.imports.Set(services.ImportPage{Entries: []services.ImportTask{
		{ID: "t1", DisplayName: "alpha task", Status: "failed", Error: "boom"},
		{ID: "t2", DisplayName: "alpha two", Status: "done"},
	}}, nil)
}

func TestPaletteOpenShowsStaticItems(t *testing.T) {
	pr := newPaletteRig(t)
	v := pr.p.View()
	if !v.Open || v.Query != "" || v.Active != 0 || v.Searching {
		t.Fatalf("打开后的状态不对：%+v", v)
	}
	want := []string{PaletteKeyCapture, PaletteKeyInbox, PaletteKeyBackup, PaletteKeyIndex, PaletteKeyWiki,
		"setting:general", "setting:capture", "setting:ai", "setting:data", "setting:appearance"}
	if got := keys(v); !slices.Equal(got, want) {
		t.Fatalf("空查询应是 5 个动作加前 5 个设置分区：\n got %v\nwant %v", got, want)
	}
	if v.Items[0].Group != locales.CommandPaletteGroupActions() || v.Items[5].Group != locales.SettingsTitle() {
		t.Errorf("分组标题不对：%q / %q", v.Items[0].Group, v.Items[5].Group)
	}
	if pr.timers.count() != 0 {
		t.Error("空查询不应排远程搜索")
	}
}

func TestPaletteStaticFilterIsImmediateAndRemoteIsDebounced(t *testing.T) {
	pr := newPaletteRig(t)
	pr.p.SetQuery(strings.ToUpper(locales.CommandPaletteCompileWiki()))
	v := pr.p.View()
	if !v.Searching {
		t.Error("远程搜索排队时应标记搜索中")
	}
	if got := keys(v); !slices.Contains(got, PaletteKeyWiki) || slices.Contains(got, PaletteKeyCapture) {
		t.Errorf("静态动作应立即按标题（不分大小写）过滤：%v", got)
	}
	if pr.timers.count() != 1 || pr.timers.delay(0) != 150*time.Millisecond {
		t.Fatalf("应排一个 150ms 的防抖：%d", pr.timers.count())
	}
	if len(pr.lib.ListQueries()) != 0 {
		t.Error("防抖到期前不应请求")
	}
}

func TestPaletteDebounceDropsSupersededQuery(t *testing.T) {
	pr := newPaletteRig(t)
	pr.seedRemote()
	pr.p.SetQuery("alp")
	pr.p.SetQuery("alpha")
	if pr.timers.count() != 2 {
		t.Fatalf("两次输入应排两个定时器：%d", pr.timers.count())
	}
	pr.timers.fire(0) // 被取代的那个
	if len(pr.lib.ListQueries()) != 0 {
		t.Fatal("被取代的防抖回调不应发请求")
	}
	pr.timers.fire(1)

	lq := pr.lib.ListQueries()
	if len(lq) != 1 || lq[0].Scope != services.LibraryScopeAll || lq[0].Search != "alpha" || lq[0].SearchMode != services.LibrarySearchRecall || lq[0].Limit != 5 {
		t.Errorf("知识库查询不对：%+v", lq)
	}
	if ws := pr.wiki.Searches(); len(ws) != 1 || ws[0].Query != "alpha" || ws[0].Limit != 5 {
		t.Errorf("Wiki 查询不对：%+v", ws)
	}
	iq := pr.imports.Queries()
	if len(iq) != 1 || iq[0].Status != services.ImportFilterAll || iq[0].Query != "alpha" || iq[0].PageSize != 20 {
		t.Errorf("采集任务查询不对：%+v", iq)
	}
	v := pr.p.View()
	if v.Searching || len(v.FailedSources) != 0 {
		t.Errorf("搜索完成后的状态不对：%+v", v)
	}
	want := []string{"knowledge:i1", "knowledge:i2", "wiki:w1", "task:t1", "task:t2"}
	if got := keys(v); !slices.Equal(got, want) {
		t.Errorf("结果顺序应为 动作→知识→Wiki→任务→设置：\n got %v\nwant %v", got, want)
	}
	if v.Items[2].Group != "Wiki" || v.Items[3].Group != locales.ImportsTitle() {
		t.Errorf("分组标题不对：%q %q", v.Items[2].Group, v.Items[3].Group)
	}
	if v.Items[3].Subtitle != "failed · boom" || v.Items[4].Subtitle != "done" {
		t.Errorf("任务副标题应是状态加错误：%q / %q", v.Items[3].Subtitle, v.Items[4].Subtitle)
	}
}

func TestPaletteRemoteResultsCappedAtFive(t *testing.T) {
	pr := newPaletteRig(t)
	var entries []services.LibraryEntry
	var tasks []services.ImportTask
	for i := range 8 {
		entries = append(entries, services.LibraryEntry{ID: fmt.Sprintf("i%d", i), Title: "alpha"})
		tasks = append(tasks, services.ImportTask{ID: fmt.Sprintf("t%d", i), DisplayName: "alpha"})
	}
	pr.lib.SeedEntries(entries)
	pr.imports.Set(services.ImportPage{Entries: tasks}, nil)
	pr.p.SetQuery("alpha")
	pr.timers.fire(0)
	n := map[PaletteKind]int{}
	for _, it := range pr.p.View().Items {
		n[it.Kind]++
	}
	if n[PaletteKnowledge] != 5 || n[PaletteTask] != 5 {
		t.Errorf("每个来源最多 5 条：%v", n)
	}
}

func TestPaletteSettingsMatchKeywordsInBothLanguages(t *testing.T) {
	pr := newPaletteRig(t)
	for query, section := range map[string]string{"backup": "setting:data", "hotkeys": "setting:shortcuts", "model": "setting:ai"} {
		pr.p.SetQuery(query)
		if !slices.Contains(keys(pr.p.View()), section) {
			t.Errorf("%q 应命中 %s：%v", query, section, keys(pr.p.View()))
		}
	}
	// 中文关键词来自嵌入数据文件，不写在 Go 字面量里
	cn := strings.Fields(settingsKeywords["data"])[0]
	pr.p.SetQuery(cn)
	if !slices.Contains(keys(pr.p.View()), "setting:data") {
		t.Errorf("中文关键词 %q 应命中数据分区", cn)
	}
}

func TestPaletteSourceFailureIsIsolatedAndLogged(t *testing.T) {
	pr := newPaletteRig(t)
	pr.seedRemote()
	pr.wiki.Fail(servicestest.WikiOpSearch, errors.New("wiki index offline"))
	pr.p.SetQuery("alpha")
	pr.timers.fire(0)
	v := pr.p.View()
	if !slices.Equal(v.FailedSources, []string{SourceWiki}) {
		t.Errorf("应只报告 Wiki 失败：%v", v.FailedSources)
	}
	if got := keys(v); !slices.Contains(got, "knowledge:i1") || !slices.Contains(got, "task:t1") || slices.Contains(got, "wiki:w1") {
		t.Errorf("其余来源应照常显示：%v", got)
	}
	logs := pr.log.entries()
	if len(logs) != 1 || !strings.HasPrefix(logs[0], "palette.search.wiki|") || !strings.Contains(logs[0], "wiki index offline") {
		t.Errorf("失败应记日志：%v", logs)
	}
	if len(pr.toasts()) != 0 {
		t.Error("来源失败不应弹提示（每次按键都会触发）")
	}
}

func TestPaletteStaleResponseIsDiscarded(t *testing.T) {
	pr := newPaletteRig(t)
	pr.p.Env = pr.asyncEnv()
	pr.seedRemote()
	release, arrived := pr.wiki.Hold(servicestest.WikiOpSearch)

	pr.p.SetQuery("alpha")
	pr.timers.fire(0)
	<-arrived // 第一次搜索卡在 Wiki
	pr.p.SetQuery("beta")
	release()
	pr.wait()

	v := pr.p.View()
	if v.Query != "beta" || !v.Searching {
		t.Fatalf("新查询仍在等防抖：%+v", v)
	}
	for _, k := range keys(v) {
		if strings.HasPrefix(k, "knowledge:") || strings.HasPrefix(k, "wiki:") || strings.HasPrefix(k, "task:") {
			t.Fatalf("过期的结果不应显示：%v", keys(v))
		}
	}
	if len(pr.log.entries()) != 0 {
		t.Errorf("被取消的请求不是失败，不应留日志：%v", pr.log.entries())
	}

	pr.timers.fire(1)
	pr.wait()
	if v := pr.p.View(); v.Searching {
		t.Error("新查询完成后应结束搜索中")
	}
	if got := len(pr.wiki.Searches()); got != 2 {
		t.Errorf("Wiki 应收到两次查询：%d", got)
	}
}

func TestPaletteCloseAndReopenInvalidateInFlightSearch(t *testing.T) {
	pr := newPaletteRig(t)
	pr.p.Env = pr.asyncEnv()
	pr.seedRemote()
	release, arrived := pr.wiki.Hold(servicestest.WikiOpSearch)
	pr.p.SetQuery("alpha")
	pr.timers.fire(0)
	<-arrived
	pr.p.Close()
	pr.p.Open() // 重新打开：清空
	release()
	pr.wait()
	v := pr.p.View()
	if v.Query != "" || v.Searching || slices.ContainsFunc(keys(v), func(k string) bool { return strings.HasPrefix(k, "knowledge:") }) {
		t.Errorf("关闭再打开后晚到的结果不应落入新会话：%+v", v)
	}
}

func TestPaletteBlankQueryClearsRemote(t *testing.T) {
	pr := newPaletteRig(t)
	pr.seedRemote()
	pr.p.SetQuery("alpha")
	pr.timers.fire(0)
	pr.p.SetQuery("   ")
	v := pr.p.View()
	if v.Searching || slices.ContainsFunc(keys(v), func(k string) bool { return strings.HasPrefix(k, "knowledge:") }) {
		t.Errorf("空白查询应清掉远程结果：%+v", v)
	}
	if pr.timers.count() != 1 {
		t.Errorf("空白查询不应再排搜索：%d", pr.timers.count())
	}
}

func TestPaletteKeyboardNavigation(t *testing.T) {
	pr := newPaletteRig(t)
	pr.p.Move(-1)
	if pr.p.View().Active != 0 {
		t.Error("向上不应越过第一项")
	}
	pr.p.Move(1)
	pr.p.Move(1)
	if pr.p.View().Active != 2 {
		t.Errorf("应移到第三项：%d", pr.p.View().Active)
	}
	for range 50 {
		pr.p.Move(1)
	}
	if v := pr.p.View(); v.Active != len(v.Items)-1 {
		t.Errorf("向下应夹在最后一项：%d", v.Active)
	}
	pr.p.SetActive(3)
	pr.p.SetActive(99)
	if pr.p.View().Active != 3 {
		t.Error("越界的悬停应忽略")
	}
	pr.p.SetQuery("x")
	if pr.p.View().Active != 0 {
		t.Error("改查询后选中项回到第一项")
	}
}

func TestPaletteActiveResetsWhenResultCountChanges(t *testing.T) {
	pr := newPaletteRig(t)
	pr.lib.SeedEntries([]services.LibraryEntry{{ID: "i1", Title: "s one"}, {ID: "i2", Title: "s two"}})
	pr.p.SetQuery("s") // 设置分区的英文关键词里一定有 s，所以静态项至少两个
	before := len(pr.p.View().Items)
	pr.p.Move(1)
	if pr.p.View().Active != 1 {
		t.Fatalf("测试前提：静态项应不少于两个（%d）", before)
	}
	pr.timers.fire(0)
	if after := len(pr.p.View().Items); after == before {
		t.Fatalf("测试前提：结果数应变化 %d → %d", before, after)
	}
	if pr.p.View().Active != 0 {
		t.Error("结果数变化后选中项应回到第一项")
	}
}

func TestPaletteRunDispatchesHooksAndCloses(t *testing.T) {
	pr := newPaletteRig(t)
	pr.seedRemote()
	cases := []struct {
		key  string
		want string
	}{
		{PaletteKeyCapture, "capture"}, {PaletteKeyInbox, "inbox"}, {PaletteKeyIndex, "index"}, {PaletteKeyWiki, "wiki-compile"},
		{"knowledge:i1", "item:i1"}, {"wiki:w1", "wiki:w1"}, {"task:t1", "task:alpha task"}, {"setting:data", "settings:data"},
	}
	for _, c := range cases {
		pr.calls = nil
		pr.p.Open() // 静态项（动作、设置）在空查询里；远程项要先搜出来
		if strings.HasPrefix(c.key, "knowledge:") || strings.HasPrefix(c.key, "wiki:") || strings.HasPrefix(c.key, "task:") {
			pr.p.SetQuery("alpha")
			pr.timers.fire(pr.timers.count() - 1)
		}
		idx := slices.Index(keys(pr.p.View()), c.key)
		if idx < 0 {
			t.Fatalf("%s 不在结果里：%v", c.key, keys(pr.p.View()))
		}
		if !pr.p.Run(idx) {
			t.Fatalf("%s 应执行成功", c.key)
		}
		if !slices.Equal(pr.calls, []string{c.want}) {
			t.Errorf("%s：钩子调用 %v，期望 %q", c.key, pr.calls, c.want)
		}
		if pr.p.View().Open {
			t.Errorf("%s：执行后应关闭面板", c.key)
		}
	}
	if pr.p.Run(0) {
		t.Error("面板关闭后 Run 应返回 false")
	}
}

func TestPaletteRunActiveAndOutOfRange(t *testing.T) {
	pr := newPaletteRig(t)
	pr.p.Move(1)
	if !pr.p.RunActive() || !slices.Equal(pr.calls, []string{"inbox"}) {
		t.Errorf("回车应执行选中项：%v", pr.calls)
	}
	pr.p.Open()
	if pr.p.Run(-1) || pr.p.Run(999) {
		t.Error("越界应返回 false")
	}
	if !pr.p.View().Open {
		t.Error("越界不应关闭面板")
	}
}

func TestPaletteRunWithNoResultsDoesNothing(t *testing.T) {
	pr := newPaletteRig(t)
	pr.p.SetQuery("zzz-no-such-thing")
	pr.timers.fire(0)
	if len(pr.p.View().Items) != 0 {
		t.Fatal("测试前提：应没有任何结果")
	}
	if pr.p.RunActive() {
		t.Error("没有结果时回车不应执行")
	}
	if !pr.p.View().Open {
		t.Error("没有结果时回车不应关闭面板")
	}
}

func runBackupAction(pr *paletteRig) {
	pr.p.Open()
	idx := slices.Index(keys(pr.p.View()), PaletteKeyBackup)
	pr.p.Run(idx)
}

func TestPaletteBackupNotInitializedOpensDataSettings(t *testing.T) {
	pr := newPaletteRig(t)
	uiCalls := 0
	pr.p.UI = func(fn func()) { uiCalls++; fn() }
	runBackupAction(pr)
	if !slices.Equal(pr.calls, []string{"settings:data"}) || uiCalls != 1 {
		t.Errorf("应经界面线程打开数据分区：%v ui=%d", pr.calls, uiCalls)
	}
	if tt := pr.lastToast(); tt.Kind != kit.ToastWarning || tt.Message != locales.CommandPaletteBackupNotInitialized() {
		t.Errorf("应提示尚未初始化：%+v", tt)
	}
	if n := len(pr.backup.CallsOf("CreateSnapshot")); n != 0 {
		t.Errorf("未初始化不应创建快照：%d", n)
	}
}

func TestPaletteBackupCreatesSnapshotWithSuccessToast(t *testing.T) {
	pr := newPaletteRig(t)
	pr.backup.SetStatus(services.RepositoryStatus{Initialized: true})
	runBackupAction(pr)
	if n := len(pr.backup.CallsOf("CreateSnapshot")); n != 1 {
		t.Fatalf("应创建一次快照：%d", n)
	}
	if tt := pr.lastToast(); tt.Kind != kit.ToastSuccess || tt.Message != locales.CommandPaletteBackupDone() {
		t.Errorf("应提示完成：%+v", tt)
	}
}

func TestPaletteBackupFailureShowsDetailAndLogs(t *testing.T) {
	pr := newPaletteRig(t)
	pr.backup.SetStatus(services.RepositoryStatus{Initialized: true})
	pr.backup.Fail("CreateSnapshot", errors.New("disk is full"))
	runBackupAction(pr)
	tt := pr.lastToast()
	if tt.Kind != kit.ToastError || tt.Message != locales.SettingsBackupFailed() || !strings.Contains(tt.Detail, "disk is full") {
		t.Errorf("应弹出带原因的失败提示：%+v", tt)
	}
	if len(pr.log.entries()) == 0 {
		t.Error("失败应留日志")
	}
}

func TestPaletteBackupStatusFailureIsReported(t *testing.T) {
	pr := newPaletteRig(t)
	pr.backup.Fail("RepositoryStatus", errors.New("keyring locked"))
	runBackupAction(pr)
	if tt := pr.lastToast(); tt.Kind != kit.ToastError || !strings.Contains(tt.Detail, "keyring locked") {
		t.Errorf("读不出仓库状态应如实报错：%+v", tt)
	}
	if n := len(pr.backup.CallsOf("CreateSnapshot")); n != 0 {
		t.Error("读不出状态不应继续备份")
	}
}

func TestPaletteBackupDoesNotStack(t *testing.T) {
	pr := newPaletteRig(t)
	pr.p.Env = pr.asyncEnv()
	pr.backup.SetStatus(services.RepositoryStatus{Initialized: true})
	release := pr.backup.Hold("CreateSnapshot")
	runBackupAction(pr)
	deadline := time.Now().Add(5 * time.Second)
	for len(pr.backup.CallsOf("CreateSnapshot")) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("备份没有开始")
		}
		time.Sleep(time.Millisecond)
	}
	runBackupAction(pr) // 第一次还没结束：不能叠起来
	release()
	pr.wait()
	if n := len(pr.backup.CallsOf("CreateSnapshot")); n != 1 {
		t.Errorf("同时只应有一次备份：%d", n)
	}
	runBackupAction(pr)
	pr.wait()
	if n := len(pr.backup.CallsOf("CreateSnapshot")); n != 2 {
		t.Errorf("结束后应能再次备份：%d", n)
	}
}

func TestPaletteNilServicesAreTolerated(t *testing.T) {
	pr := newPaletteRig(t)
	pr.p.Library, pr.p.Wiki, pr.p.Imports, pr.p.Backup = nil, nil, nil, nil
	pr.p.SetQuery("alpha")
	pr.timers.fire(0)
	if v := pr.p.View(); v.Searching || len(v.FailedSources) != 0 {
		t.Errorf("没接服务时搜索应直接结束：%+v", v)
	}
	runBackupAction(pr)
	if tt := pr.lastToast(); tt.Kind != kit.ToastError {
		t.Errorf("备份服务不可用应如实报错：%+v", tt)
	}
}
