package app

// 全局浮层的接线（台账 B-008、B-016、B-026，M3-6a）：命令面板、更新对话框、首次使用设置清单、旧版数据迁移对话框，
// 以及窗口内（局部）快捷键。状态与界面在 viva/screens/overlays，这里把它们接到窗口、各模块、外壳与设置页上：
//   - 顶栏搜索入口与更新按钮：OnOpenPalette、OnUpdate 与每帧同步的更新提示；
//   - 命令面板的动作：切模块、跳设置分区、快速采集、语义索引、Wiki 编译、打开条目 / Wiki 页 / 采集任务；
//   - 局部快捷键：窗口按键先问 LocalShortcuts（焦点在文字框时放行打字，在快捷键录制框时一律不抢）；
//   - 自动弹出（更新检查、旧库探测、设置清单）只在真实外壳实例里启动（Options.AutoOverlays），截图、对照与无头测试不会冒出对话框。

import (
	"sync/atomic"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/screens/overlays"
	settingsscreen "guizhi-mygo-shell/viva/screens/settings"
	"guizhi-mygo-shell/viva/screens/shell"
)

// overlayState 是 Root 里全局浮层的状态：各 Store、界面与外壳的窗口入口。
type overlayState struct {
	screen    *overlays.Screen
	palette   *overlays.Palette
	update    *overlays.Update
	setup     *overlays.Setup
	migration *overlays.Migration
	shortcuts *overlays.LocalShortcuts

	// host 是外壳给的窗口入口（Run 在窗口打开后登记）：局部「显示 / 隐藏窗口」与更新检查的「窗口可见」要读它。无外壳时为空。
	host atomic.Pointer[Host]
}

// mountOverlays 装配全局浮层。必须在 mountCapture 之后调用（要把浮层接在快速采集对话框之后声明），
// 也要在 mountWiki、mountInbox 之后（命令面板的动作用到它们）。服务缺哪个，对应的来源或动作就留空或如实提示。
func (r *Root) mountOverlays(invalidate func()) {
	svc := r.opts.Services
	o := &r.overlay

	p := overlays.NewPalette(r.guard, invalidate)
	p.Ctx = r.ctx
	p.Library, p.Wiki, p.Imports, p.Backup = svc.Library, svc.Wiki, svc.Imports, svc.Backup
	p.UI = r.runOnUI
	p.Hooks = overlays.PaletteHooks{
		QuickCapture:        func() { r.runOnUI(func() { r.applyCommand(CommandShortcutNewItem) }) },
		OpenInbox:           func() { r.openModule(shell.ModuleInbox) },
		BuildSemanticIndex:  r.buildSemanticIndex,
		CompileWiki:         r.openWikiCompile,
		OpenLibraryItem:     func(itemID string) { r.openLibraryItem(r.ctx, itemID) },
		OpenWikiPage:        r.openWikiPage,
		SearchImportTasks:   r.searchImportTasks,
		OpenSettingsSection: r.openSettingsSection,
	}

	u := overlays.NewUpdate(svc.Updater, r.guard, invalidate)
	u.Ctx = r.ctx
	u.Options = func() services.UpdateCheckOptions {
		ui := r.store.Values().UI
		return services.UpdateCheckOptions{UseMirror: ui.UseUpdateMirror, Channel: ui.UpdateChannel}
	}
	u.CurrentVersion = func() string { return r.opts.AppVersion }
	u.AutoCheckEnabled = func() bool { return r.store.Values().UI.AutoCheckUpdate }
	u.WindowVisible = func() bool {
		h := o.host.Load()
		return h == nil || h.Visible()
	}

	s := overlays.NewSetup(r.guard, invalidate)
	s.Ctx = r.ctx
	s.AI, s.Prefs, s.CaptureTools = svc.AI, svc.Prefs, svc.CaptureTools
	s.Hooks = overlays.SetupHooks{
		StartCapture:        func() { r.runOnUI(func() { r.applyCommand(CommandShortcutNewItem) }) },
		OpenSettingsSection: r.openSettingsSection,
		RequestAIQuickSetup: func() { r.runOnUI(func() { r.settings.OpenAIQuickSetup() }) },
	}

	m := overlays.NewMigration(r.guard, invalidate)
	m.Ctx = r.ctx
	m.Service, m.Prefs = svc.Migration, svc.Prefs
	// 迁入的数据要在各视图里刷新：知识库列表与计数、合集、标签（渲染层迁移完成后的 refresh）。回调在后台线程里。
	m.OnMigrated = r.refreshAfterMigration
	// 迁移确认先于设置清单：探测有了结论再评估设置清单；确认页开着时清单被推迟，关闭后补上。
	m.OnDetected = func() {
		if svc.AI != nil {
			s.Evaluate()
		}
	}
	m.OnClose = s.Unblocked
	s.Blocked = m.IsOpen

	sc := overlays.NewLocalShortcuts(r.guard)
	sc.Ctx = r.ctx
	sc.Service = svc.Shortcuts
	sc.Hooks = overlays.ShortcutHooks{
		ToggleWindow: r.toggleWindow,
		NewItem:      func() { r.applyCommand(CommandShortcutNewItem) },
		Search:       p.Open,
		Settings:     func() { r.win.Page = shell.PageSettings },
	}

	o.palette, o.update, o.setup, o.migration, o.shortcuts = p, u, s, m, sc
	o.screen = &overlays.Screen{
		Palette: p, Update: u, Setup: s, Migration: m, OpenExternal: svc.OpenExternal, Guard: r.guard,
	}

	// 窗口入口：顶栏搜索入口、更新按钮，以及设置页的「设置引导」与快捷键保存之后的重读。
	r.win.OnOpenPalette = p.Open
	r.win.OnUpdate = u.Open
	r.settings.OpenSetupGuide = s.RequestOpen
	r.settings.OnShortcutsChanged = sc.Reload

	// 浮层声明在快速采集对话框之后：命令面板盖在它上面。
	prev := r.win.Dialogs
	r.win.Dialogs = func(c ui.C) {
		if prev != nil {
			prev(c)
		}
		o.screen.View(c)
	}
}

// syncOverlays 每帧把更新状态交给顶栏的更新按钮（有可用或已下载的新版本才显示）。界面线程。
func (r *Root) syncOverlays() {
	v := r.overlay.update.View()
	r.win.Update, r.win.UpdateVersion = v.PillVersion != "", v.PillVersion
}

// startOverlays 在第一帧启动浮层的后台工作：订阅更新推送、读已保存的快捷键；真实外壳实例（AutoOverlays）再启动自动更新检查、
// 旧库探测与设置清单评估。没有对应服务的流程不启动。
func (r *Root) startOverlays() {
	o := &r.overlay
	o.update.Attach()
	o.shortcuts.Reload()
	if !r.opts.AutoOverlays {
		return
	}
	svc := r.opts.Services
	if svc.Updater != nil {
		o.update.Start()
	}
	switch {
	case svc.Migration != nil:
		o.migration.Detect() // 有了结论之后（OnDetected）再评估设置清单
	case svc.AI != nil:
		o.setup.Evaluate()
	}
}

// closeOverlays 停掉浮层的后台工作：更新订阅与定时检查、进行中的面板搜索。
func (r *Root) closeOverlays() {
	r.overlay.update.Detach()
	r.overlay.palette.Close()
}

// attachHost 登记外壳给的窗口入口（Run 在窗口打开后调用）。
func (r *Root) attachHost(h *Host) { r.overlay.host.Store(h) }

// windowShown 在窗口从托盘重新显示之后调用：补做隐藏期间被跳过的自动更新检查。任意 goroutine。
func (r *Root) windowShown() {
	r.runOnUI(func() { r.overlay.update.OnVisibilityChanged(true) })
}

// toggleWindow 是局部的 showApp：窗口可见就收到托盘，否则显示。没有外壳时什么也不做。界面线程。
func (r *Root) toggleWindow() {
	h := r.overlay.host.Load()
	if h == nil {
		return
	}
	if h.Visible() {
		h.Hide()
		return
	}
	h.Show()
}

// openModule 切到某个模块的主页。可能来自任意 goroutine，窗口状态经 runOnUI 排到界面线程。
func (r *Root) openModule(m shell.Module) {
	r.runOnUI(func() { r.win.Module, r.win.Page = m, shell.PageHome })
}

// openSettingsSection 打开设置页的某一分区（general、capture、ai、data、appearance、shortcuts 等）。
func (r *Root) openSettingsSection(section string) {
	r.runOnUI(func() {
		r.settings.Active = settingsscreen.SectionID(section)
		r.win.Page = shell.PageSettings
	})
}

// searchImportTasks 切到采集模块并把搜索词设为任务的显示名（渲染层命令面板的任务项）。
func (r *Root) searchImportTasks(displayName string) {
	r.runOnUI(func() {
		r.win.Module, r.win.Page = shell.ModuleImports, shell.PageHome
		if r.importsStore != nil {
			r.importsStore.SetQuery(displayName)
		}
	})
}

// buildSemanticIndex 是命令面板的「立即建立语义索引」：与处理中心聚合行的「立即执行」同一个动作，进度与失败都由它提示。
// 没有处理中心服务的实例如实说明去向。
func (r *Root) buildSemanticIndex() {
	if r.inboxStore == nil {
		action := locales.CommandPaletteBuildSemanticIndex()
		r.notifier.Error(locales.CommonOperationFailed(action), locales.NativeShellNotMigrated(locales.NavInbox()))
		return
	}
	store := r.inboxStore
	store.Spawn(func() { store.RunSemanticNow(r.ctx) })
}

// refreshAfterMigration 在旧库迁移成功后刷新迁入数据的各视图：知识库列表与计数、合集、标签，以及处理中心的待处理数。后台线程。
func (r *Root) refreshAfterMigration() {
	if r.library != nil {
		lib := r.library.Store
		lib.FetchCollections(r.ctx)
		lib.FetchTags(r.ctx)
		lib.RefreshAll(r.ctx)
	}
	if r.inboxStore != nil {
		r.inboxStore.Refresh(r.ctx)
	}
}

// localShortcut 把一次窗口按键交给局部快捷键：匹配了就执行并返回 true（按键被消化）。
// 焦点在快捷键录制框时一律不抢（录制框要收下所有组合），在文字输入框时不带 Ctrl / Alt 的组合是在打字、放行。
func (r *Root) localShortcut(e gfx.KeyEvent) bool {
	sc := r.overlay.shortcuts
	if sc == nil {
		return false
	}
	focus := overlays.FocusNone
	switch {
	case r.settings != nil && r.settings.ShortcutRecording():
		focus = overlays.FocusRecorder
	case kit.FocusOf(r.canvas).Editable():
		focus = overlays.FocusTextInput
	}
	return sc.Handle(e, focus)
}
