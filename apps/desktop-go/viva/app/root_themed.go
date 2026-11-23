package app

import (
	"github.com/vivaui/viva/ui"

	themedscreen "guizhi-mygo-shell/viva/screens/themed"
)

// mountThemed 装配主题阅读（AI 阅读，台账 B-064 / B-074）：它不是一个模块，而是三处入口共用的一套界面——
// 知识库详情阅读区的「AI 阅读」版本、处理中心的「主题页任务」页、设置 AI 分区里的「联网搜索」。
// Services.Themed 为空（装配不全的自动化实例）时不装配：三处入口保持原样（「AI 阅读」与「主题页任务」只说明去向，设置里没有联网搜索块）。
// 必须在 mountLibrary、mountInbox 之后调用：要把界面挂到它们上面。
func (r *Root) mountThemed(invalidate func()) {
	svc := r.opts.Services.Themed
	if svc == nil {
		return
	}
	s := themedscreen.New(svc, r.guard, invalidate)
	s.SetContext(r.ctx)
	s.Hooks = themedscreen.Hooks{
		OpenSettingsAI: r.openSettingsAI,
		OpenExternal:   r.opts.Services.OpenExternal,
		OpenItem:       r.openThemedItem,
	}
	r.themed = s
	if r.library != nil {
		r.library.Themed = s
	}
	if r.inbox != nil {
		r.inbox.Hooks.ThemedTasks = func(c ui.C, back func()) { s.TasksPage(c, back) }
	}
	r.settings.ReadingSearch = s.SearchPanel
}

// openThemedItem 是任务列表里的「查看主题页」：把条目的阅读标签记成任务对应的那份内容，再在知识库里打开它
// （渲染层 patchContentReadingMemory + setScope("all") + selectItem + setAppModule("library")）。
func (r *Root) openThemedItem(itemID, sourceKind string) {
	if r.library != nil {
		r.library.ShowThemedTask(itemID, sourceKind)
	}
	r.openLibraryItem(r.ctx, itemID)
}
