package inbox

import (
	"context"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// Hooks 是处理中心需要外壳代办的事：批量整理下拉里的合集。跨模块跳转（打开条目、去配置模型、Wiki 预览）挂在 Store 的钩子上。
// 都可以为空（单测里没有外壳）；为空时下拉没有选项。
type Hooks struct {
	// Collections 返回现有合集，批量整理的「归入合集」下拉用（渲染层 useKnowledgeStore 的 collections）；每帧现读。
	Collections func() []services.LibraryCollection
	// LoadCollections 在进入模块时读取合集：知识库没打开过时合集列表是空的（渲染层靠全局的 fetchCollections）。在后台调用。
	LoadCollections func(ctx context.Context)
	// ThemedTasks 画「主题页任务」页（主题阅读的任务列表，台账 B-064）：代替工作区的内容，每一帧调用；back 回到处理中心。
	// 为空（没接主题阅读）时入口退回成「尚未迁移」的说明。
	ThemedTasks func(c ui.C, back func())
}

// themedTasksMilestone 与 themedTasksLedger 是「主题页任务」入口在没接线时的去向：主题阅读（AI 阅读）随 M3 迁入，台账 B-064。
const (
	themedTasksMilestone = "M3"
	themedTasksLedger    = "B-064"
)

// Screen 是「处理中心」模块的界面（渲染层 InboxWorkspace / SidebarInboxPanel）：二级侧栏里的筛选，工作区里的待办列表。
//
// 状态都在 Store 里；Screen 只保存界面自己的东西：合集下拉的开合、标签输入框的内容、挂载记号。
// 所有方法都在界面线程里调用；耗时的动作经 Store.Spawn 放到后台，不阻塞界面线程。
type Screen struct {
	Look  *kit.Look
	Store *Store
	// Ctx 是后台请求用的上下文（应用生命周期）。
	Ctx context.Context
	// Guard 是变更类操作的失败通道（与 Store 里的是同一个）；Guard.Notify 同时是「尚未迁移」提示的入口。
	Guard kit.Guard
	Hooks Hooks

	collection kit.SelectState // 批量整理的合集下拉
	tags       ui.Field        // 批量整理的标签输入框
	tagsPushed string          // 最近一次与 Store 对齐过的标签文本：输入框与 Store 谁先变，靠它分辨
	tagsFocus  bool            // 上一帧标签输入框是否有焦点（边框高亮跟它走，晚一帧不可见）

	window kit.RowWindow // 卡片列表的窗口化声明：只声明视口附近的行（rows.go 的 itemList）
	leadH  float32       // 列表之上的提示条与错误条上一帧的高度：窗口把滚动几何换算成列表自己的坐标时要扣掉

	// mounted 与 shown 是渲染层「挂载」的对应：这一帧没画工作区（切到别的模块、设置页）就当作卸载，
	// 下次再画时重新进入并重读（EndFrame）。
	mounted, shown bool
	// themedTasks 为真时工作区显示「主题页任务」页（渲染层 InboxWorkspace 的 showThemedTasks）；离开处理中心即复位。
	themedTasks bool
}

// enter 在工作区或侧栏出现时调用：第一次出现（或卸载之后再出现）重读列表与 Wiki、语义索引的状态，
// 并读一次合集（渲染层 InboxWorkspace 挂载时的 useEffect → refresh）。
// 重读同时刷新模型是否就绪，所以从设置里配好模型回来，「尚未配置文本模型」的提示不会滞后。
func (s *Screen) enter() {
	s.shown = true
	if s.mounted {
		return
	}
	s.mounted = true
	s.Store.Spawn(func() { s.Store.Refresh(s.Ctx) })
	if load := s.Hooks.LoadCollections; load != nil {
		s.Store.Spawn(func() { load(s.Ctx) })
	}
}

// EndFrame 在每一帧结束时由根调用：这一帧没有画处理中心，就当作卸载，下次再画时重新进入。
func (s *Screen) EndFrame() {
	if !s.shown {
		s.mounted = false
		s.collection.Open = false
		s.themedTasks = false
	}
	s.shown = false
}

// refresh 在后台重读（页头的刷新钮、局部错误条与读取失败态里的重试）。
func (s *Screen) refresh() {
	s.Store.Spawn(func() { s.Store.Refresh(s.Ctx) })
}

// notMigrated 是尚未迁移的入口的统一反应：弹一条失败提示，把它归属的里程碑与台账编号写进可复制的详情。
// 不进应用错误日志：它不是故障，点一次记一条只会淹没真正的错误。
func (s *Screen) notMigrated(action, milestone, ledger string) {
	if s.Guard.Notify == nil {
		return
	}
	s.Guard.Notify.Error(locales.CommonOperationFailed(action), locales.NativeLibraryNotMigrated(action, milestone, ledger))
}
