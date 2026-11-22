package app

import (
	"context"
	"time"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
	inboxscreen "guizhi-mygo-shell/viva/screens/inbox"
	settingsscreen "guizhi-mygo-shell/viva/screens/settings"
	"guizhi-mygo-shell/viva/screens/shell"
)

// inboxCountsDelay 是知识库计数变化后合并刷新处理中心的等待（渲染层 App.tsx 的 600 ms 定时器）。
// 包变量只为测试缩短等待；生产不改。
var inboxCountsDelay = 600 * time.Millisecond

// inboxWatch 是处理中心对知识库计数的监听：新建、删除、归类会改变待处理数，计数变了就合并刷新角标。
type inboxWatch struct {
	seen  bool
	last  [3]int // 未归合集、全部、回收站
	timer *time.Timer
}

// mountInbox 装配处理中心模块（渲染层 components/inbox）：Store 持有列表与选择，Screen 是侧栏与工作区。
// Services.Inbox 为空（装配不全的自动化实例）时是 nil，rail 与工作区按「尚未迁移」处理。必须在 mountImports 与 mountLibrary 之后调用：
// 它要接到这两个模块的 Store 上。
func (r *Root) mountInbox(invalidate func()) {
	svc := r.opts.Services.Inbox
	if svc == nil {
		return
	}
	store := inboxscreen.NewStore(svc, r.guard, invalidate)
	store.OnOpenSettingsAI = r.openSettingsAI
	store.OnOpenItem = r.openLibraryItem
	// Wiki 编译预览随 Wiki 模块迁入：点「查看范围和任务」只说明去向，不假装有预览（Wiki 模块自己的占位页同样说明未迁移）。
	store.OnWikiPreview = func() {
		action := locales.InboxWikiCandidatesView()
		r.notifier.Error(locales.CommonOperationFailed(action), locales.NativeShellNotMigrated(locales.NavWiki()))
	}
	hooks := inboxscreen.Hooks{}
	if r.library != nil {
		lib := r.library.Store
		hooks.Collections = func() []services.LibraryCollection { return lib.View().Collections }
		hooks.LoadCollections = lib.FetchCollections
		// 智能归类写入后刷新合集与知识库视图（渲染层 fetchCollections + refreshKnowledge）。
		store.OnClassified = func(ctx context.Context) {
			lib.FetchCollections(ctx)
			lib.RefreshAll(ctx)
		}
		store.CollectionNames = func() []string {
			collections := lib.View().Collections
			names := make([]string, 0, len(collections))
			for _, c := range collections {
				names = append(names, c.Name)
			}
			return names
		}
	}
	if r.importsStore != nil {
		imports := r.importsStore
		store.RefreshImports = imports.Fetch
		// 采集任务完成、失败、重复，或撤销窗口里的移除提交之后：处理中心的待处理数变了。
		imports.RefreshInbox = func() { store.Spawn(func() { store.Refresh(r.ctx) }) }
	}
	r.inboxStore = store
	r.inbox = &inboxscreen.Screen{Store: store, Ctx: r.ctx, Guard: r.guard, Hooks: hooks}
	r.win.Modules[shell.ModuleInbox] = shell.ModuleView{Panel: r.inbox.Panel, Workspace: r.inbox.Workspace}
}

// openSettingsAI 打开设置页的 AI 分区。可能来自后台 goroutine（智能归类、语义索引的回执），窗口状态只能在界面线程上改，
// 所以经 runOnUI 排队。
func (r *Root) openSettingsAI() {
	r.runOnUI(func() {
		r.settings.Active = settingsscreen.SectionAI
		r.win.Page = shell.PageSettings
	})
}

// openLibraryItem 在知识库里打开条目（渲染层 openItem）：切到知识库模块、范围设为全部、选中该条目。
// 知识库没有装配时只切模块（落在「尚未迁移」的占位上）。
func (r *Root) openLibraryItem(ctx context.Context, itemID string) {
	r.runOnUI(func() { r.win.Module, r.win.Page = shell.ModuleLibrary, shell.PageHome })
	if r.library == nil {
		return
	}
	store := r.library.Store
	store.SetScope(ctx, services.LibraryScopeAll)
	store.SelectItem(itemID)
}

// startInbox 在第一帧读一次处理中心（rail 角标要在任何模块里都准）。
func (r *Root) startInbox() {
	if r.inboxStore == nil {
		return
	}
	r.inboxStore.Spawn(func() { r.inboxStore.Refresh(r.ctx) })
}

// syncInbox 每帧把处理中心的待处理数交给 rail 角标，并在知识库计数变化时排一次合并刷新。
func (r *Root) syncInbox() {
	if r.inboxStore == nil {
		return
	}
	r.win.InboxTotal = r.inboxStore.View().Total
	if r.library == nil {
		return
	}
	counts := r.library.Store.View().Counts
	if counts == nil {
		return
	}
	cur := [3]int{counts.Uncategorized, counts.All, counts.Trash}
	w := &r.inboxWatch
	if !w.seen || cur == w.last {
		w.seen, w.last = true, cur
		return
	}
	w.last = cur
	if w.timer != nil {
		w.timer.Stop()
	}
	w.timer = time.AfterFunc(inboxCountsDelay, func() {
		r.inboxStore.Spawn(func() { r.inboxStore.Refresh(r.ctx) })
	})
}

// closeInbox 释放处理中心的定时器与在途的智能归类。
func (r *Root) closeInbox() {
	if t := r.inboxWatch.timer; t != nil {
		t.Stop()
	}
	if r.inboxStore != nil {
		r.inboxStore.Close()
	}
}
