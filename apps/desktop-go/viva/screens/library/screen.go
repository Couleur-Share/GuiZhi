package library

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/reader"
)

// Screen 是「知识库」模块的二级侧栏界面（渲染层 components/library/SidebarLibraryPanel）：
// 范围页签、智能视图、合集、平台、标签与回收站；工作区随后续里程碑迁入。
//
// 状态都在 Store 里；Screen 只保存界面自己的东西：行菜单、新建与重命名弹窗、智能视图的列表与弹窗、行悬停的分组登记。
// 所有方法都在界面线程里调用。
type Screen struct {
	Look  *kit.Look
	Store *Store
	// Ctx 是后台请求用的上下文（应用生命周期）。
	Ctx context.Context
	// Guard 是变更类操作的失败通道（与 Store 里的是同一个）；Guard.Notify 同时是提示条的入口。
	Guard kit.Guard
	// Prefs 是智能视图的存储（界面偏好）；nil 时智能视图只留在内存里。
	Prefs services.Prefs

	// Now 与 NewID 给智能视图的创建用；对照与测试注入固定值。空表示取系统时钟与随机 ID。
	Now   func() time.Time
	NewID func() string
	// OpenExternal 用系统默认浏览器打开链接（来源 chip 的链接）；nil 时点击弹失败提示（测试与对照里不接线）。
	OpenExternal func(url string) error
	// Imports 是采集队列（来源 chip 旁的「重新采集并待确认」入队）；nil 时点击弹失败提示。
	Imports services.Imports
	// HideDetail 为真时详情栏内容隐去、表格行与 Enter 只选中不开详情浮层：对照场景专用，口径与采集端的 HIDE_DETAIL_CSS 一致——
	// 渲染层一侧同样只隐去详情内容、不改布局（M2-4 之前的场景用，它们比的是列表栏、侧栏与批量条）。生产里恒为假。
	HideDetail bool
	// EditorPreview 报告设置里「编辑器默认预览」是否开着（每次现读）；nil 取默认值（开）。
	EditorPreview func() bool
	// ReaderToolbar 为真时即使没有接阅读岛宿主，工具栏里也画读者工具条（原文 / AI 阅读 / 目录 / 编辑）：对照场景专用，
	// 用来单独比较这一组控件而不拉起 WebView。生产里接了宿主（SetReader）就总是画。
	ReaderToolbar bool

	// Loc 与 Locale 决定表格里时间的写法（今天写时分，其余写日期）；空表示本地时区与中文。
	Loc    *time.Location
	Locale kit.DateLocale

	// readerHost 是阅读区的岛宿主（SetReader）；nil 时阅读区留白。
	readerHost *reader.Host

	table      tableState // 表格视图的界面状态（列配置、二维滚动、分隔条拖拽；table.go）
	groups     kit.HoverGroups
	cardGroups kit.HoverGroups // 工作区卡片的悬停分组：卡片里嵌着的类型图标（带 title）悬停时，卡片仍算悬停
	pane       listPane        // 工作区里列表栏的界面状态（pane.go）
	searchStop func()          // 顶栏搜索的防抖排期（search.go）
	flex       kit.FlexColumn  // 侧栏列：定高的 flex 列，内容溢出时行按 flex-shrink 压低（台账 B-034）
	showUnused bool            // 标签分区的「显示未使用的标签」开关；刻意不持久化，退出模块即复位（渲染层同）
	menu       rowMenu
	edit       editDialog
	smart      smartViews
	bulk       bulkState // 批量与条目操作的界面状态：条目菜单、批量条的浮层、确认框、结果条的展开（bulk.go）
	detail     detailUI  // 详情栏的界面状态：标题框、头部菜单、通知条、内容面板、专注阅读与详情浮层（detail.go）

	// mounted 与 shown 是渲染层「挂载」的对应：侧栏这一帧没画（切到别的模块、设置页，或侧栏收起）就当作卸载，
	// 下次再画时重新进入（EndFrame）。
	mounted, shown bool

	// wsShown 是这一帧画了工作区；wsActive 是上一帧画了（EndFrame 把前者交给后者）：窗口级键盘（keyboard.go）只在工作区挂着时生效，
	// 按键发生在两次绘制之间，所以看上一帧的。
	wsShown, wsActive bool

	// detailShown 与 detailActive 同理，对应 ItemDetail 是否挂载（Ctrl+S 的前提）：卡片视图与专注阅读里它常驻，表格视图里只在详情浮层打开时。
	detailShown, detailActive bool
	mu                        sync.Mutex // 保护下面由后台写入的字段：界面线程在帧首取走
	finished                  *editOutcome
	confirmDone               *confirmOutcome // 确认框后台动作的结果（settleBulk 在帧首取走）
	confirmGen                uint64          // 确认框的代次：resetBulk 时递增，在途动作的结果据此作废
}

// nowTime 是此刻。
func (s *Screen) nowTime() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// loc 是时间的时区。
func (s *Screen) loc() *time.Location {
	if s.Loc != nil {
		return s.Loc
	}
	return time.Local
}

// newID 生成智能视图的 ID：渲染层用 crypto.randomUUID，这里取 16 个随机字节的十六进制。
func (s *Screen) newID() string {
	if s.NewID != nil {
		return s.NewID()
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return time.Now().Format("20060102150405.000000000")
	}
	return hex.EncodeToString(raw[:])
}

// EndFrame 在每一帧结束时由根调用：这一帧没有画侧栏，就当作卸载，下次再画时重新进入。
func (s *Screen) EndFrame() {
	if !s.shown {
		s.mounted = false
	}
	s.shown = false
	was := s.wsActive
	s.wsActive, s.wsShown = s.wsShown, false
	s.detailActive, s.detailShown = s.detailShown, false
	if !s.wsActive {
		s.pane.reveal = cardReveal{} // 工作区卸载：下次挂载时已有选中行要重新滚进视野
	}
	if was && !s.wsActive {
		s.resetBulk() // 工作区卸载：菜单、浮层与确认框都是它的组件状态，随之丢掉
		// 卸载（切换模块）前保存未落盘内容（渲染层 LibraryWorkspace 的卸载 effect：flushPendingSave，不等结果）。
		s.Store.Spawn(func() { s.Store.FlushPendingSave(s.Ctx) })
		s.detail = detailUI{} // 详情栏、专注阅读（离开知识库即退出）与详情浮层同理
	}
	if s.readerHost != nil {
		s.readerHost.EndFrame() // 这一帧阅读区没画出来（切了模块、关了详情、换到没有内容的标签）：关岛
	}
}

// Enter 在知识库模块出现时调用（渲染层 LibraryWorkspace 与侧栏挂载）：重新读取智能视图与全部数据，
// 并把只属于这次挂载的界面状态（未使用标签的开关、菜单、弹窗）复位。
func (s *Screen) Enter() {
	s.showUnused = false
	s.menu = rowMenu{rects: s.menu.rects}
	s.edit = editDialog{}
	s.resetBulk()
	s.smart.reset(ReadSavedViews(s.Prefs))
	s.Store.Load(s.Ctx)
}

// Panel 是二级侧栏里的内容。外层是 flex h-full min-h-0 flex-col overflow-y-auto px-2 pb-3：内容溢出时在这里滚动，
// 回收站一块 mt-auto 贴在底部（内容不满一屏时）。
func (s *Screen) Panel(c ui.C) {
	l := s.Look
	s.ensureMounted()
	s.groups.Begin()
	s.settleEdit()
	v := s.Store.View()

	l.ScrollAreaView(c.Key("panel-scroll"), [4]float32{0, l.Space(2), l.Space(3), l.Space(2)}, func(view kit.ScrollView) {
		// 列的每个孩子都要在声明之前登记 flex 属性（s.flex.Item，一一对应、同序）；回收站块的 mt-auto 在 FlexColumn 里解算。
		s.flex.Begin(view.Viewport)
		s.flex.Box(c.Key("body")).Children(func() {
			s.scopeTabs(c, v)
			s.smartViewsSection(c, v)
			s.collectionsSection(c, v, view)
			s.platformsSection(c, v)
			s.tagsSection(c, v, view)
			s.trashSection(c, v)
		})
	}).Grow(1).FillWidth().MinHeight(0)

	s.rowMenuView(c, v)
	s.editDialogView(c, v)
	s.smartDialogView(c, v)
}
