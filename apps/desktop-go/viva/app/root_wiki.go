package app

import (
	settingsscreen "guizhi-mygo-shell/viva/screens/settings"
	"guizhi-mygo-shell/viva/screens/shell"
	wikiscreen "guizhi-mygo-shell/viva/screens/wiki"
)

// mountWiki 装配 Wiki 模块（渲染层 components/wiki）：Store 持有目录、详情与编译状态，Screen 是二级侧栏与工作区。
// Services.Wiki 为空（装配不全的自动化实例）时不装配，rail 与工作区按「尚未迁移」处理，处理中心的 Wiki 预览入口也照旧只说明去向。
// 必须在 mountInbox 之后调用：要把处理中心的「查看范围和任务」接到编译对话框上。
func (r *Root) mountWiki(invalidate func()) {
	svc := r.opts.Services.Wiki
	if svc == nil {
		return
	}
	store := wikiscreen.NewStore(svc, r.guard, invalidate)
	store.Ctx = r.ctx
	// 来源条目在知识库里打开：回调从后台 goroutine 来，openLibraryItem 自己把窗口状态排到界面线程。
	store.OnOpenSource = func(itemID string) { r.openLibraryItem(r.ctx, itemID) }
	r.wiki = &wikiscreen.Screen{
		Store: store, Ctx: r.ctx, Guard: r.guard, Prefs: r.opts.Services.Prefs, Now: r.opts.Services.Now,
		Hooks: wikiscreen.Hooks{
			OpenSettingsAI:      r.openSettingsAI,
			OpenSettingsGeneral: r.openSettingsGeneral,
			// 自动编译开关每帧现读：设置在别处改了，指示条下一帧就跟上。
			AutoCompileEnabled: func() bool { return r.store.Values().UI.WikiCompileEnabled },
			OpenExternal:       r.opts.Services.OpenExternal,
		},
	}
	r.win.Modules[shell.ModuleWiki] = shell.ModuleView{Panel: r.wiki.Panel, Workspace: r.wiki.Workspace}
	if r.inboxStore != nil {
		// 处理中心横幅的「查看范围和任务」：切到 Wiki 模块并打开编译对话框（渲染层 onWikiPreview）。
		r.inboxStore.OnWikiPreview = r.openWikiCompile
	}
}

// openWikiCompile 切到 Wiki 模块并打开编译对话框。可能来自任意 goroutine，窗口状态经 runOnUI 排到界面线程。
func (r *Root) openWikiCompile() {
	r.runOnUI(func() {
		r.win.Module, r.win.Page = shell.ModuleWiki, shell.PageHome
		if r.wiki != nil {
			r.wiki.OpenCompileDialog()
		}
	})
}

// openWikiPage 切到 Wiki 模块并打开某个 Wiki 页（问答里的 Wiki 来源）。
func (r *Root) openWikiPage(pageID string) {
	r.runOnUI(func() {
		r.win.Module, r.win.Page = shell.ModuleWiki, shell.PageHome
		if r.wiki != nil {
			r.wiki.SelectPage(pageID)
		}
	})
}

// openSettingsGeneral 打开设置页的通用分区（自动编译开关在那里）。
func (r *Root) openSettingsGeneral() {
	r.runOnUI(func() {
		r.settings.Active = settingsscreen.SectionGeneral
		r.win.Page = shell.PageSettings
	})
}
