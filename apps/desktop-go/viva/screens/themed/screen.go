package themed

import (
	"context"
	"time"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/reader"
)

// Hooks 是主题阅读界面需要外壳代办的事。都可以为空（单测里没有外壳）；为空时对应入口无反应或不出现。
type Hooks struct {
	// OpenSettingsAI 去设置页的 AI 分区（缺文本模型、缺生图模型、缺联网搜索时的「配置」入口）。界面线程。
	OpenSettingsAI func()
	// OpenExternal 用系统浏览器打开参考资料的链接（只放行 http / https）。界面线程。
	OpenExternal func(url string) error
	// OpenItem 在知识库里打开任务对应的文章，并切到那一份内容的 AI 阅读（任务列表里的「查看主题页」）。界面线程。
	OpenItem func(itemID, sourceKind string)
}

// Subject 是知识库详情这一帧交给主题阅读的「正在读什么」。值类型、可比较：没变就不重复调用状态层。
type Subject struct {
	ItemID string
	// Title 是条目标题：生成设置对话框的副标题。
	Title string
	// Tab 是阅读面板的标签（TabBody / TabSummary 以外的标签没有 AI 阅读）。
	Tab string
	// Enabled 为 false 时这篇内容没有 AI 阅读（例如在网页快照里）。
	Enabled bool
	// ReadOnly 是条目在回收站里：页面仍可查看与导出，不能生成、调整、删除。
	ReadOnly bool
	// Editing 是知识库正在编辑原文：AI 阅读退出显示，编辑结束后回到原文。
	Editing bool
	// ContentKey 是标题、来源与正文的摘要：原文改了要重读（过期标记随之变化）。
	ContentKey string
}

// Screen 是主题阅读（AI 阅读）的界面：知识库详情里的「AI 阅读」版本、处理中心的「主题页任务」页，以及设置里的联网搜索。
//
// 状态都在各个 Store 里（见 reading.go、tasks.go、search.go、mode.go、document.go）；Screen 只保存界面自己的东西：
// 对话框与菜单的开合、表单当前值、输入框内容与挂载记号。方法都在界面线程里调用；Store 的方法大多会读服务，
// 一律经各自的 Spawn 放到后台，不阻塞界面线程（单测里 Spawn 是同步的）。
//
// 阅读页本体由阅读岛显示：当前格式页面与生成途中的预览由外壳渲染成静态文档（services.ThemedReading.Render），
// 旧格式页面的文档随读取结果一起到；二者都以 reader.ViewSnapshot 放进岛的沙箱 iframe（IslandView）。
// 岛里的页面没有桥接脚本：高度固定、页内滚动，没有目录、页内查找与交互（见台账说明）。
type Screen struct {
	Look  *kit.Look
	Guard kit.Guard
	Hooks Hooks
	// BlankPage 为 true 时没有岛宿主的页面占位（pageShell）画成纯空白：对照里渲染层一侧的页面本体被隐去（B-064），
	// 占位的标题行与文档容器不应该出现在可访问树与文字里。生产环境有宿主，不会走到占位。
	BlankPage bool
	// Loc 与 Locale 是日期的时区与区域写法（渲染层的 toLocaleDateString 跟系统走）；零值取本机时区与中文。
	Loc    *time.Location
	Locale kit.DateLocale

	Mode    *ModeStore
	Reading *ReadingStore
	Docs    *DocLoader
	Tasks   *TaskList
	Search  *SearchSettings

	// 绑定：Bind 在每一帧由知识库详情调用，EndFrame 据此判断详情离开了画面（切走、关闭）。
	sub      Subject
	subSet   bool
	bound    bool
	attached bool
	// 读取参数的上一次取值：没变就不重复 Open（Open 每次都会请求重绘）。
	openedKey string
	// pendingItem 与 pendingKind 是任务列表要求「打开后直接看 AI 阅读」的那一份内容。
	pendingItem, pendingKind string

	// 这一帧的派生（Bind 里算好）：阅读视图、面板布局、页面文档。
	view      ReadingView
	pane      PaneModel
	doc       DocView
	docOK     bool
	docWanted bool

	setup  setupUI
	del    kit.ConfirmState
	tools  toolsUI
	status statusUI
	// bodyH 是阅读区主体（任务状态、读取提示、空态）上一帧画出来的自然高度，居中溢出时用来算顶上被推出视口的那一半。
	bodyH float32

	tasks  tasksUI
	search searchUI
}

// New 创建主题阅读界面。svc 不能为空。invalidate 在状态变化后请求重绘（可为空）。
func New(svc services.ThemedReading, guard kit.Guard, invalidate func()) *Screen {
	s := &Screen{
		Guard:   guard,
		Mode:    NewModeStore(svc, guard, invalidate),
		Reading: NewReadingStore(svc, guard, invalidate),
		Docs:    NewDocLoader(svc, guard, invalidate),
		Tasks:   NewTaskList(svc, guard, invalidate),
		Search:  NewSearchSettings(svc, guard, invalidate),
	}
	// 删除主题页成功后退回原文（渲染层 onPageRemoved → 切回原文版本）。
	s.Reading.OnPageRemoved = func() { s.Mode.Select(false) }
	s.Tasks.OnOpen = s.openTask
	return s
}

// SetContext 设置发给服务的请求的根上下文（应用生命周期）。
func (s *Screen) SetContext(ctx context.Context) {
	s.Mode.Ctx, s.Reading.Ctx, s.Docs.Ctx, s.Tasks.Ctx, s.Search.Ctx = ctx, ctx, ctx, ctx, ctx
}

// SetSpawn 统一设置各个 Store 与界面放后台动作的方式（测试注入同步执行，保持确定性；nil 恢复为开 goroutine）。
func (s *Screen) SetSpawn(fn func(func())) {
	if fn == nil {
		fn = func(f func()) { go f() }
	}
	s.Mode.Spawn, s.Reading.Spawn, s.Docs.Spawn, s.Tasks.Spawn = fn, fn, fn, fn
}

// Close 在窗口关闭时调用：退订进度（不取消在执行的任务），清掉输入框里的密钥。
func (s *Screen) Close() {
	s.Reading.Detach()
	s.Tasks.Detach()
	s.Search.SetKey("")
}

// openTask 是任务列表里的「查看主题页」：让下一次绑定到这份内容时直接进入 AI 阅读，再请外壳切到知识库。
func (s *Screen) openTask(t services.ThemedTask) {
	s.pendingItem, s.pendingKind = t.ItemID, t.SourceKind
	if s.Hooks.OpenItem != nil {
		s.Hooks.OpenItem(t.ItemID, t.SourceKind)
	}
}

// Bind 在知识库详情画阅读区之前每一帧调用一次：把「正在读什么」交给状态层，返回此刻的阅读模式。
// 这一帧之后到 EndFrame 之前，Toolbar / Area / IslandView / Dialogs 都以它为准。
func (s *Screen) Bind(sub Subject) ModeView {
	s.bound = true
	s.docWanted = false
	if !s.subSet || sub != s.sub {
		s.sub, s.subSet = sub, true
		s.Mode.Open(sub.ItemID, sub.Tab, sub.Enabled)
	}
	mv := s.Mode.View()
	switch {
	case sub.Editing && !mv.Editing:
		s.Mode.StartEditing()
	case !sub.Editing && mv.Editing:
		s.Mode.FinishEditing()
	}
	if s.pendingItem != "" && s.pendingItem == sub.ItemID && s.pendingKind == SourceKindOf(sub.Tab) && sub.Enabled && !sub.Editing {
		s.Mode.Select(true)
		s.pendingItem, s.pendingKind = "", ""
	}
	mv = s.Mode.View()

	if mv.Available {
		// 读取参数没变就不重复 Open：它每次都请求重绘，逐帧调用会让界面停不下来。
		key := mv.ItemID + "\x00" + mv.SourceKind + "\x00" + sub.ContentKey + "\x00" + boolKey(mv.Active)
		if key != s.openedKey || !s.attached {
			s.openedKey = key
			s.Reading.Open(mv.ItemID, mv.SourceKind, sub.ContentKey, mv.Active)
		}
		if !s.attached {
			s.attached = true
			s.Reading.Attach()
		}
	} else if s.attached {
		s.release()
	}
	s.view = s.Reading.View()
	s.pane = s.view.Pane(sub.ReadOnly, true)
	return mv
}

func boolKey(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// release 放开这份内容：退订进度（绝不取消在执行的任务）、清掉文档缓存与所有对话框状态。
func (s *Screen) release() {
	s.Reading.Detach()
	s.Reading.Open("", "", "", false) // 回到未选中：下次绑定同一份内容时按重新挂载处理（对话框、预览、固定的页面都复位）
	s.Docs.Reset()
	s.attached, s.openedKey = false, ""
	s.view, s.pane, s.doc = ReadingView{}, PaneModel{}, DocView{}
}

// EndFrame 在每一帧结束时由根调用：这一帧没有绑定（详情关了、切到别的条目前的空档、离开知识库），就放开阅读状态；
// 任务列表与搜索设置这一帧没画，也当作卸载。
func (s *Screen) EndFrame() {
	if !s.bound && s.subSet {
		s.subSet = false
		s.Mode.Open("", TabBody, false)
		if s.attached {
			s.release()
		}
	}
	s.bound = false
	s.endTasks()
	s.endSearch()
}

// ModeView 返回当前阅读模式（知识库的工具栏据此画「原文 / AI 阅读」）。
func (s *Screen) ModeView() ModeView { return s.Mode.View() }

// Active 报告这一帧阅读区显示的是 AI 阅读页。
func (s *Screen) Active() bool { return s.bound && s.Mode.View().Active }

// Pane 返回这一帧的面板布局（测试与知识库详情用）。
func (s *Screen) Pane() PaneModel { return s.pane }

// spawn 把耗时动作放到后台（测试里同步执行）。
func (s *Screen) spawn(fn func()) {
	if s.Reading.Spawn != nil {
		s.Reading.Spawn(fn)
		return
	}
	go fn()
}

// ---- 阅读岛 ----

// IslandView 是阅读岛这一帧该显示的视图：页面文档已就绪时是 ViewSnapshot，其余（读取中、失败、任务状态、空态）为 false，由 Area 画原生内容。
// 文档放进岛的沙箱 iframe：它自带的内容安全策略不放行脚本，所以页面是静态的（旧格式页面的内联脚本同样被岛的策略挡掉）。
func (s *Screen) IslandView() (reader.View, bool) {
	if !s.bound || !s.Mode.View().Active {
		return reader.View{}, false
	}
	doc, ok := s.pageDoc()
	if !ok || doc.Doc == "" {
		return reader.View{}, false
	}
	return reader.View{
		Kind: reader.ViewSnapshot, SnapshotDoc: doc.Doc, SnapshotInstanceID: s.view.InstanceID,
		SnapshotVersionID: doc.VersionID, SnapshotWidth: reader.SnapshotWidthFluid, SnapshotAssets: []string{},
	}, true
}

// pageDoc 是这一帧页面的文档：旧格式取读取结果自带的，当前格式与预览向文档读取器要；没有页面可渲染时 ok 为 false。
func (s *Screen) pageDoc() (DocView, bool) {
	if s.docWanted {
		return s.doc, s.docOK
	}
	s.docWanted, s.doc, s.docOK = true, DocView{}, false
	switch s.pane.Body {
	case BodyLegacyFrame:
		page := s.view.Page()
		s.doc, s.docOK = DocView{Key: "legacy", Doc: s.view.Result.Document, VersionID: page.ID}, true
	case BodyNativePage:
		page := s.view.ReadingPage()
		in := services.ThemedRenderInput{
			ThemedRequest: services.ThemedRequest{ItemID: s.view.ItemID, SourceKind: s.view.SourceKind},
			InstanceID:    s.view.InstanceID,
		}
		revision := 0
		if s.view.ShowingPreview() && s.view.Task != nil {
			in.TaskID, revision = s.view.Task.ID, s.view.Preview.Revision
		} else {
			in.VersionID = page.ID
		}
		s.doc, s.docOK = s.Docs.Want(in, DocKey(in, page.UpdatedAt, revision)), true
		if s.doc.VersionID == "" {
			s.doc.VersionID = page.ID
		}
	}
	return s.doc, s.docOK
}
