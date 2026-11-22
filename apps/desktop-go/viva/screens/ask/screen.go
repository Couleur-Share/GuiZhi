package ask

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/aiflow"
	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
)

// Hooks 是问答界面需要外壳代办的事。跨模块的跳转（去设置、打开文章）挂在 Store 与 Setup 的钩子上，这里只放来源的打开。
// 可以为空（单测里没有外壳）；为空时对应入口无反应。
type Hooks struct {
	// OpenSource 打开一条引用来源：Wiki 页面切到 Wiki 模块，知识条目切到知识库并选中它（渲染层 AskMessageCard 的 useOpenSource）。
	// 点「查看当前版本」时调用，界面线程。
	OpenSource func(source services.AskSource)
	// OpenURL 用系统浏览器打开网页来源（围绕本文的联网来源，渲染层 window.open）；为空时点击按「无法打开」处理。界面线程。
	OpenURL func(url string) error
	// LocateArticle 把文章来源定位到知识库阅读区里的原文位置：来源所属的文章已经由调用方打开（全局问答的「定位」先走
	// OpenArticle），这里等阅读区就绪后按来源的视图与摘录查找；结果经 done 回报。可在任意 goroutine 调用（由外壳切到界面线程）。
	// 为空时定位按失败处理。
	LocateArticle func(source aiflow.ArticleSource, done func(found bool))
}

// searchDebounce 是历史搜索框的防抖：停止输入 250 ms 后才按新搜索词筛选（渲染层 HistoryControls 的 setTimeout）。
const searchDebounce = 250 * time.Millisecond

// highlightFor 是点了来源标记之后来源 chip 的高亮时长（渲染层 focusCitation 的 2 s）；copiedFor 是「已复制」回显的时长（1.5 s）。
const (
	highlightFor = 2 * time.Second
	copiedFor    = 1500 * time.Millisecond
)

// Screen 是「AI 问答」模块的界面（渲染层 SidebarAskPanel / AskWorkspace）：二级侧栏里的新对话、语义索引卡、历史会话，
// 工作区里的多轮问答。
//
// 状态在三个仓库里：Store（活跃会话、流式提问）、History（历史列表与筛选）、Setup（文本模型与语义索引）；
// Screen 只保存界面自己的东西：输入框内容、下拉与对话框的开合、每轮消息卡的展开状态、挂载记号。
// 所有方法都在界面线程里调用；耗时的动作经 Store.spawn 放到后台，不阻塞界面线程。
type Screen struct {
	Look    *kit.Look
	Store   *Store
	History *HistoryStore
	Setup   *Setup
	// Ctx 是后台请求用的上下文（应用生命周期）。
	Ctx context.Context
	// Guard 是变更类操作的失败通道（与 Store 里的是同一个）；Guard.Notify 同时是复制失败等提示的入口。
	Guard kit.Guard
	Hooks Hooks

	// Now 返回当前时间；对照与测试注入固定时钟。空表示取系统时钟。
	Now func() time.Time
	// Loc 与 Locale 决定时间的写法；空表示本地时区与中文。
	Loc    *time.Location
	Locale kit.DateLocale
	// ShiftHeld 报告此刻是否按着 Shift：区分「Enter 发送」与「Shift+Enter 换行」。空表示不按着（Enter 总是发送）。
	// 窗口里由根接到系统键盘状态；测试注入。
	ShiftHeld func() bool
	// After 在 d 之后调用 fn（在另一个 goroutine 里），返回取消函数；防抖用。空表示取系统定时器，测试注入手动计时。
	After func(d time.Duration, fn func()) (stop func())

	// 侧栏：搜索框与三个筛选下拉、改名对话框、删除确认、会话行的悬停分组。
	search      ui.Field
	searchSeen  string // 搜索框上一次被读到的文字：与它不同就是用户又敲了字，重排防抖
	searchStop  func()
	searchFocus bool // 上一帧搜索框是否有焦点（边框高亮跟它走，晚一帧不可见）
	scope       kit.SelectState
	rng         kit.SelectState
	article     kit.SelectState
	rename      renameState
	confirm     kit.ConfirmState
	deleting    *services.AskSessionMeta // 删除确认框里的会话；nil 表示没开
	groups      kit.HoverGroups

	// 工作区：每轮消息卡的悬停分组（group/turn：动作行在行内悬停或聚焦时才显现）。
	turns kit.HoverGroups

	// 输入框与滚动跟随。
	draft      ui.Field
	draftSeen  string  // 上一帧输入框里的内容：比它多出一个换行就是按了 Enter（takeEnter）
	draftFocus bool    // 上一帧输入框是否有焦点
	focusDraft bool    // 下一帧让输入框取得焦点（示例问题、从导入结果带来的草稿）
	inputWidth float32 // 输入框文字区上一帧的宽度：算折行的行数
	inputRows  int     // 输入框文字目前折成几行（含显式换行）
	pending    string  // 外部带来的待预填草稿
	hasPending bool
	viewKey    string // 上一帧显示的会话：换会话时滚到底
	toEnd      bool   // 下一帧把消息列表滚到底（换会话、发出问题、点「回到最新」）
	// awayFromEnd 为真表示消息列表滚离底部超过 80px（上一帧的读数，「回到最新」据此显示）。
	awayFromEnd bool
	// titleWidth 是会话行标题那一行可用的内容宽度（上一帧的读数，0 表示还没量到）：标题拆成几个文本节点时，据此判断前缀放不放得下。
	titleWidth float32
	loading    kit.Delayed // 首次读取的转圈延迟登场

	cards map[string]*cardState // 每轮消息卡的展开状态，按消息 id

	// art 是围绕本文面板的界面状态（article_panel.go）：知识库详情的右侧栏与全局问答里的本文对话共用一份，
	// 它们读写同一个 ArticleStore（渲染层同一个模块级 store）；artHistory 是「本文历史」的列表（第二个 HistoryStore，筛选条件互不影响）。
	art        articleUI
	artHistory *HistoryStore

	// mounted 与 shown 是渲染层「挂载」的对应：这一帧没画侧栏与工作区（切到别的模块、设置页）就当作卸载，
	// 下次再画时重新进入并重读（EndFrame）。
	mounted, shown bool
}

// renameState 是改名对话框：session 在界面线程里读写；保存在后台跑，结果经原子量回到界面线程。
type renameState struct {
	session *services.AskSessionMeta
	field   ui.Field
	modal   kit.ModalState
	busy    atomic.Bool
	saved   atomic.Bool // 后台保存成功：下一帧关掉对话框
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

// SetDraft 给输入框预填一段草稿（渲染层 uiStore.setAskDraft）：只预填不自动发送，用户先确认问题再发，
// 一次误点不会消耗模型调用。工作区下一次绘制时取走并让输入框聚焦。界面线程调用。
func (s *Screen) SetDraft(text string) {
	s.pending, s.hasPending = text, true
}

// enter 在侧栏或工作区出现时调用：第一次出现（或卸载之后再出现）恢复上次会话、读历史列表、查模型与语义索引状态
// （渲染层两个组件挂载时的 useEffect）。
func (s *Screen) enter() {
	s.shown = true
	if s.mounted {
		return
	}
	s.mounted = true
	s.spawn(func() { s.Store.Initialize() })
	s.spawn(func() { s.History.Load(false) })
	s.spawn(func() { s.Setup.Refresh() })
}

// EndFrame 在每一帧结束时由根调用：这一帧没有画问答模块，就当作卸载，下次再画时重新进入。
func (s *Screen) EndFrame() {
	if !s.shown {
		s.mounted = false
		s.scope.Open, s.rng.Open, s.article.Open = false, false, false
	}
	s.shown = false
	s.endArticleFrame()
}

// card 取（或建）一轮消息卡的展开状态。
func (s *Screen) card(id string) *cardState {
	if s.cards == nil {
		s.cards = map[string]*cardState{}
	}
	st := s.cards[id]
	if st == nil {
		st = &cardState{}
		s.cards[id] = st
	}
	return st
}

// pruneCards 丢掉已经不在消息列表里的卡片状态（删除单轮、换会话之后）。
func (s *Screen) pruneCards(messages []services.AskMessage) {
	if len(s.cards) == 0 {
		return
	}
	live := make(map[string]struct{}, len(messages))
	for _, m := range messages {
		live[m.ID] = struct{}{}
	}
	for id := range s.cards {
		if _, ok := live[id]; !ok {
			delete(s.cards, id)
		}
	}
}
