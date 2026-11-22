package wiki

import (
	"context"
	"encoding/json"
	"time"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/kit"
)

// Hooks 是 Wiki 界面需要外壳代办的事。来源条目的打开挂在 Store.OnOpenSource 上；这里放设置页的跳转与外链。
// 都可以为空（单测里没有外壳）；为空时对应入口无反应或不出现。
type Hooks struct {
	// OpenSettingsAI 去设置页的 AI 分区（没有 Wiki 模型时编译对话框里的「前往配置」）。界面线程。
	OpenSettingsAI func()
	// OpenSettingsGeneral 去设置页的「通用」分区（工具栏的「自动编译」指示：开关本体在那里）。界面线程。
	OpenSettingsGeneral func()
	// AutoCompileEnabled 报告设置里的自动编译是否开启；每帧现读。为空按关闭处理。
	AutoCompileEnabled func() bool
	// OpenExternal 用系统浏览器打开正文里的 http(s) 外链；为空时点击按「无法打开」处理。界面线程。
	OpenExternal func(url string) error
}

// searchDebounce 是目录搜索框的防抖：停止输入 250 ms 后才发起查询（渲染层 WikiCatalogList 的 SEARCH_DEBOUNCE_MS）。
const searchDebounce = 250 * time.Millisecond

// catalogPaneBounds 是目录栏宽度的缺省与上下限（渲染层 WIKI_CATALOG_PANE_WIDTH_*）。
var catalogPaneBounds = settings.PaneWidths[2]

// Screen 是「Wiki」模块的界面（渲染层 SidebarWikiPanel / WikiWorkspace 与其下的目录、详情、图谱、编译对话框）：
// 二级侧栏里的筛选，工作区里的顶栏、目录加详情或关系图谱。
//
// 状态都在 Store 里；Screen 只保存界面自己的东西：搜索框的内容、目录栏宽度、详情的编辑器与菜单、图谱的缩放与平移、
// 编译对话框的开合状态、挂载记号。所有方法都在界面线程里调用；Store 的方法大多会读服务，一律经 Store.spawn 放到后台，
// 不阻塞界面线程（单测里 spawn 是同步的）。
type Screen struct {
	Look  *kit.Look
	Store *Store
	// Ctx 是后台请求用的上下文（应用生命周期）。
	Ctx context.Context
	// Guard 是变更类操作的失败通道（与 Store 里的是同一个）。
	Guard kit.Guard
	// Prefs 保存目录栏宽度；为空时只留在内存里。
	Prefs services.Prefs
	Hooks Hooks

	// Now 返回当前时间；对照与测试注入固定时钟。空表示取系统时钟。
	Now func() time.Time
	// Loc 与 Locale 决定时间的写法；空表示本地时区与中文。
	Loc    *time.Location
	Locale kit.DateLocale
	// After 在 d 之后调用 fn（在另一个 goroutine 里），返回取消函数；防抖用。空表示取系统定时器，测试注入手动计时。
	After func(d time.Duration, fn func()) (stop func())

	// 目录列：搜索框与它的防抖。
	search      ui.Field
	searchSeen  string // 搜索框上一次被读到的文字：与它不同就是用户又敲了字，重排防抖
	searchStop  func()
	catalogLive bool          // 目录列挂载中：它卸载（切到图谱、没有页面）再挂载时搜索框回到 Store 里的查询（渲染层 useState(searchQuery)）
	catalogSeen bool          // 这一帧画了目录列（EndFrame 据此判断卸载）
	window      kit.RowWindow // 目录行的窗口化声明：只声明视口附近的行

	// 目录栏宽度（偏好）：stored 是已保存的值，pane 是拖动状态。
	paneLoaded bool
	stored     float32
	pane       kit.ColumnResizerState

	// loading 是首次读取期间转圈的延迟登场（delayed-fade-in）。
	loading kit.Delayed

	detail  detailUI
	graph   graphUI
	compile compileUI

	// mounted 与 shown 是渲染层「挂载」的对应：这一帧没画工作区（切到别的模块、设置页）就当作卸载，
	// 下次再画时重新进入并重读（EndFrame）。
	mounted, shown bool
}

// nowTime 是此刻（对照里是固定时钟）。
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

// after 排一次延后执行；返回取消函数。
func (s *Screen) after(d time.Duration, fn func()) func() {
	if s.After != nil {
		return s.After(d, fn)
	}
	timer := time.AfterFunc(d, fn)
	return func() { timer.Stop() }
}

// spawn 把耗时动作放到后台（测试里同步执行）。
func (s *Screen) spawn(fn func()) { s.Store.spawn(fn) }

// enter 在侧栏或工作区出现时调用：第一次出现（或卸载之后再出现）重读目录与状态（渲染层 WikiWorkspace 挂载时的 refresh）。
func (s *Screen) enter() {
	s.shown = true
	if s.mounted {
		return
	}
	s.mounted = true
	s.spawn(func() { s.Store.Refresh() })
}

// EndFrame 在每一帧结束时由根调用：这一帧没有画 Wiki，就当作卸载，下次再画时重新进入。
func (s *Screen) EndFrame() {
	if !s.shown {
		s.mounted = false
		s.detail.unmount()
	}
	s.shown = false
	if !s.catalogSeen {
		// 目录列这一帧没画（图谱视图、没有页面、离开了 Wiki）：它卸载了，没触发的防抖也随之清掉。
		s.catalogLive = false
		s.stopSearchTimer()
	}
	s.catalogSeen = false
	// 图谱这一帧没画：它卸载了，视角下次进入回到初始；滚轮钩子只在上一整帧画了图谱时接。
	s.graph.live = s.graph.drawn
	if !s.graph.drawn {
		s.graph.mounted, s.graph.dragging = false, false
	}
	s.graph.drawn = false
}

// OpenCompileDialog 打开全文编译对话框（工具栏的编译钮、更新建议里的「选择资料升级」、处理中心的 Wiki 入口都走它；
// 渲染层的 wiki-compile-preview 事件）。界面线程调用。
func (s *Screen) OpenCompileDialog() {
	s.spawn(func() { s.Store.Compile().Open() })
}

// SelectPage 选中并打开一页（问答来源里的 Wiki 页面）。界面线程调用；读取在后台跑。
func (s *Screen) SelectPage(id string) {
	s.spawn(func() {
		s.Store.SetViewMode(ViewCatalog)
		s.Store.SelectPage(id)
	})
}

// loadPane 读取目录栏宽度（只读一次）。
func (s *Screen) loadPane() {
	if s.paneLoaded {
		return
	}
	s.paneLoaded = true
	s.stored = float32(catalogPaneBounds.Default)
	if s.Prefs == nil {
		return
	}
	if raw, ok := s.Prefs.Get(settings.PrefWikiCatalogPaneWidth); ok {
		var width float64
		if json.Unmarshal(raw, &width) == nil {
			s.stored = float32(catalogPaneBounds.Clamp(width))
		}
	}
}

// savePaneWidth 保存目录栏宽度（拖动结束、按键、双击之后）；保存失败只记日志（下次启动回到默认，不影响当前使用）。
func (s *Screen) savePaneWidth(width float32) {
	s.stored = width
	if s.Prefs == nil {
		return
	}
	if err := s.Prefs.Set(settings.PrefWikiCatalogPaneWidth, float64(width)); err != nil {
		s.Guard.Background("wiki.catalogWidth", err)
	}
}
