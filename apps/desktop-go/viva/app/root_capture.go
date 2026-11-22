package app

// 快速采集对话框的接线：对话框本身在 viva/screens/capture，这里把它接到外壳（顶栏新建、托盘、快捷键）、
// 采集队列（入队后刷新）与知识库（空白笔记）上。

import (
	"guizhi-mygo-shell/viva/locales"
	capturescreen "guizhi-mygo-shell/viva/screens/capture"
	"guizhi-mygo-shell/viva/screens/shell"
)

// mountCapture 装配快速采集对话框（渲染层 components/capture/CaptureDialog）。
// 没有采集服务（装配不全的自动化实例）时什么都不装：入口留空，托盘与快捷键命令按「尚未迁移」留痕。
func (r *Root) mountCapture(invalidate func()) {
	svc := r.opts.Services.Imports
	if svc == nil {
		return
	}
	d := &capturescreen.Dialog{
		Ctx: r.ctx, Guard: r.guard, Imports: svc, Library: r.opts.Services.Library, Wake: invalidate,
		Hooks: capturescreen.Hooks{
			// 入队成功：队列立刻刷新（渲染层 import.store 的 enqueue 之后重新取列表）。回调在后台任务里，Fetch 可以阻塞。
			OnEnqueued: func() {
				if r.importsStore != nil {
					r.importsStore.Fetch(r.ctx)
				}
			},
		},
	}
	r.capture = d
	r.win.OnNewCapture = func() { r.openCapture() }
	r.win.OnImportFiles = d.ImportFiles
	r.win.Dialogs = d.View
	if r.library != nil {
		// 空白笔记要知识库来建；没有知识库时对话框不显示这个按钮，顶栏菜单项也没有动作。
		d.Hooks.BlankNote = r.newBlankNote
		r.win.OnNewBlankNote = func() { r.newBlankNote("") }
	}
}

// openCapture 打开快速采集；没有装配采集时返回 false。界面线程。
func (r *Root) openCapture() bool {
	if r.capture == nil {
		return false
	}
	r.capture.Open()
	return true
}

// newBlankNote 切到知识库、在 collectionID 合集里（空是不归合集）新建一条空白条目并打开它，标题取得焦点
// （渲染层 setAppModule("library") 之后 createItem）。界面线程调用；新建在后台做，失败有提示。
func (r *Root) newBlankNote(collectionID string) {
	if r.library == nil {
		return
	}
	r.win.Module, r.win.Page = shell.ModuleLibrary, shell.PageHome
	store := r.library.Store
	store.Spawn(func() {
		r.guard.Run(locales.CaptureBlankNote(), func() error { return store.CreateItem(r.ctx, collectionID) })
	})
}
