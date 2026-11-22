package app

import (
	"guizhi-mygo-shell/aiflow"
	"guizhi-mygo-shell/native/services"
	askscreen "guizhi-mygo-shell/viva/screens/ask"
	"guizhi-mygo-shell/viva/screens/shell"
)

// mountAsk 装配 AI 问答模块（渲染层 components/ask）：Store 持有活跃会话与流式提问，HistoryStore 持有历史列表与筛选，
// Setup 持有文本模型与语义索引状态，Screen 是二级侧栏与工作区。
// Services.Ask 为空（装配不全的自动化实例）时不装配，rail 与工作区按「尚未迁移」处理。必须在 mountImports 与 mountLibrary 之后调用：
// 要接到这两个模块上（「就此提问」与彻底删除后的证据清除）。
func (r *Root) mountAsk(invalidate func()) {
	svc := r.opts.Services.Ask
	if svc == nil {
		return
	}
	store := askscreen.NewStore(svc, r.guard, invalidate)
	store.Ctx = r.ctx
	store.Prefs = r.opts.Services.Prefs
	store.OnOpenSettingsAI = r.openSettingsAI
	store.OnOpenArticle = func(itemID string) { r.openLibraryItem(r.ctx, itemID) }

	history := askscreen.NewHistoryStore(svc, r.guard, invalidate)
	history.Ctx = r.ctx
	// 会话保存或删除之后刷新历史列表（渲染层的 "article-ask-saved" 事件）。回调可能来自任意 goroutine，读列表放到后台。
	store.OnSaved = func() { store.Spawn(func() { history.Load(false) }) }

	// Services.Inbox 同时提供语义索引的状态与手动索引；没有处理中心的实例不出语义索引卡。
	var semantic askscreen.Semantic
	if r.opts.Services.Inbox != nil {
		semantic = r.opts.Services.Inbox
	}
	setup := askscreen.NewSetup(svc, semantic, r.guard, invalidate)
	setup.Ctx = r.ctx
	setup.OnOpenSettingsAI = r.openSettingsAI

	r.askStore = store
	r.ask = &askscreen.Screen{
		Store: store, History: history, Setup: setup, Ctx: r.ctx, Guard: r.guard,
		Hooks: askscreen.Hooks{
			OpenSource: r.openAskSource, OpenURL: r.opts.Services.OpenExternal, LocateArticle: r.locateArticle,
		},
		Now: r.opts.Services.Now,
		// 输入框里 Enter 发送、Shift+Enter 换行：Shift 的状态读系统键盘。
		ShiftHeld: shiftHeld,
	}
	r.win.Modules[shell.ModuleAsk] = shell.ModuleView{Panel: r.ask.Panel, Workspace: r.ask.Workspace}

	// B-054：知识库彻底删除并选了「同时清除问答证据」之后，已打开的会话里的证据片段按清除标记重新脱敏，
	// 不会再次展示旧证据（渲染层 clearCachedConversationEvidence）。回调在知识库的后台 goroutine 里调用，Store 本身并发安全。
	if r.library != nil {
		r.library.Store.OnEvidenceCleared = store.ClearEvidenceForItems
		// B-061：知识库详情右侧的「围绕本文提问」栏与全局问答里的本文对话读写同一个 ArticleStore（r.ask.Store.Article()）。
		r.library.ArticleAsk = r.ask
	}
	// 导入结果里的「就此提问」：新建会话（不污染刚浏览的历史会话）、预填草稿（只预填不发送）、切到问答。
	if r.imports != nil {
		r.imports.Hooks.AskAboutItem = r.askAboutItem
	}
}

// askAboutItem 围绕新入库的内容开始提问（渲染层 askAboutItem）：在界面线程里被调用（点击处理里）。
func (r *Root) askAboutItem(draft string) {
	if r.ask == nil {
		return
	}
	r.win.Module, r.win.Page = shell.ModuleAsk, shell.PageHome
	r.ask.SetDraft(draft)
	store := r.askStore
	store.Spawn(func() { store.NewSession() })
	if r.invalidate != nil {
		r.invalidate()
	}
}

// openAskSource 打开问答来源（渲染层 onOpenSource）：条目来源在知识库里打开；Wiki 页来源切到 Wiki 模块并选中该页
// （没有 Wiki 服务的实例只切模块，落在「尚未迁移」的占位页上）。
func (r *Root) openAskSource(source services.AskSource) {
	if source.Kind == "wiki" {
		r.openWikiPage(source.RefID)
		return
	}
	r.openLibraryItem(r.ctx, source.RefID)
}

// locateArticle 把全局问答里的文章来源定位到知识库阅读区（渲染层 locateArticleSource）：来源所属的文章已经由调用方打开，
// 定位本身要在界面线程上排进知识库详情的帧循环里。任意 goroutine；知识库没有装配时按没找到回报。
func (r *Root) locateArticle(source aiflow.ArticleSource, done func(found bool)) {
	r.runOnUI(func() {
		if r.library == nil {
			done(false)
			return
		}
		r.library.LocateArticle(source, done)
	})
}

// closeAsk 取消在途的问答请求。
func (r *Root) closeAsk() {
	if r.askStore != nil {
		r.askStore.Stop()
	}
}
