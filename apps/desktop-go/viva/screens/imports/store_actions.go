package imports

import (
	"context"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// ImportFiles 弹出系统文件选择框，把所选文件加入采集队列（渲染层 SidebarImportsPanel 与 ImportsEmptyState 里的 importFiles）。
//
// 取消选择什么都不做；入队失败弹出带原因的错误提示（原文折叠在「查看详情」里），成功弹出「已加入采集队列（N 项）」。
// 选择框本身出错（渲染层是未处理的 Promise 拒绝，用户什么都看不到）也要让用户知道，所以走变更失败通道。
// 对话框会阻塞：调用方放在后台执行。
func (s *Store) ImportFiles(ctx context.Context) {
	files, err := s.svc.SelectFiles(ctx)
	if err != nil {
		s.guard.Report(locales.HeaderNewImportFiles(), err)
		return
	}
	if len(files) == 0 {
		return
	}
	inputs := make([]services.EnqueueImportInput, len(files))
	for i, path := range files {
		inputs[i] = services.EnqueueImportInput{Kind: "file", Input: path}
	}
	if _, err := s.Enqueue(ctx, inputs); err != nil {
		message := err.Error()
		s.guard.Background(locales.HeaderNewImportFiles(), err) // 提示文案是专用的，日志仍然要留
		if s.guard.Notify != nil {
			s.guard.Notify.Error(locales.CaptureEnqueueFailed(message), message)
		}
		return
	}
	if s.guard.Notify != nil {
		s.guard.Notify.Success(locales.CaptureEnqueued(len(files)))
	}
}

// RemoveWithUndo 从列表删除任务：先隐藏，窗口内可在提示条上撤销，之后才真正提交（渲染层 useRemoveTasksWithUndo）。
// 此前这个动作不确认也不能撤销，下一行滑到指针下面时连点两下就会误删。没有可移除的任务（都在运行）时什么都不做。
func (s *Store) RemoveWithUndo(ctx context.Context, ids []string) {
	removal := s.ScheduleRemove(ctx, ids, 0)
	if removal == nil {
		return
	}
	if s.guard.Notify != nil {
		s.guard.Notify.Undo(locales.ImportsRemovedFromList(removal.Count), locales.CommonUndo(), removal.Undo)
	}
}

// Wake 请求界面重绘：界面自己持有的状态（比如异步预览出来的确认框）在后台改变后调用。
func (s *Store) Wake() { s.wake() }
