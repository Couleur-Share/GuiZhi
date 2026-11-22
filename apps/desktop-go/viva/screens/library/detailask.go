package library

import (
	"errors"
	"strings"
	"sync/atomic"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/aiflow"
	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/reader"
	askscreen "guizhi-mygo-shell/viva/screens/ask"
)

// 围绕本文提问（渲染层 ArticleAskReader，台账 B-061）：详情内容面板右侧并排的一栏，读写 ask 屏的 ArticleStore，
// 界面本身（头部、历史、消息、输入）在 viva/screens/ask，这里只管「栏」——开合、宽度、提问目标、换条目的重置、来源定位。
//
// 栏始终与正文并排、让出宽度，不盖在正文上（渲染层注释同）：阅读区（含阅读岛）取剩下的左边，岛的矩形随之收窄。
// 工具栏的「提问」钮开合这一栏，与文章工具侧栏互斥（渲染层 onToggleAsk 先 setToolsItemId(null)）。
//
// 选段提问条（阅读区里选中文字后浮出的「解释这段 / 围绕这段提问」）的数据源是岛里页面的选区事件，岛协议 v1 没有这条消息
// （见 viva/reader/protocol.go：只有查找、滚动、目录等），所以条本身没有画；AskSelection 是它落下后要调的入口，已接好并有测试，
// 协议补上选区消息时只需把事件接到这里（台账 B-061 的余项）。

const (
	// 栏宽对应 w-[400px]；宽窄档的分界与最窄宽度同渲染层（clientWidth >= 960；w-[min(400px,48%)] min-w-[280px]）。
	askPaneWidth float32 = 400
	askWideMin   float32 = 960
	askNarrowMin float32 = 280
	askNarrowPct float32 = 48
	// askLocateWindow 是一次定位从发起到出结果的上限：条目打开、视图切换与页面回报都算在里面
	// （渲染层是 15 × 100ms 等视图加 2.5s 等回报）。
	askLocateWindow = 4 * time.Second
)

// askUI 是栏的界面状态：随详情挂载而生，工作区卸载与详情重挂时复位（remountDetail、EndFrame）。
type askUI struct {
	// openID 是开着提问栏的条目（渲染层 askItemId）；itemID 是上一帧挂载的条目——换了就停掉在途的回答（渲染层 key=ask:<id> 的卸载）。
	openID, itemID string
	// rootW 是栏所在行的宽度（上一帧量到的，首帧为 0 按窄档摆）：决定栏是固定 400px 还是 min(400px,48%)。
	rootW float32
	// prompt 与 promptKey 是选段提问给栏的提示语；selection 是选段所在的阅读目标（AskSelection）。
	prompt    string
	promptKey int
	selection *aiflow.ArticleTarget
	// submitted 由后台发送成功回调置位，下一帧在界面线程清掉 selection 与 prompt，避免下一问重复附带旧选段。
	submitted *atomic.Bool
	// locate 是在途的来源定位。
	locate *locateState
}

// locateState 是一次「定位到原文」：先把阅读区切到来源所在的视图，视图对上后用查找条把原文片段找出来并滚到命中处，
// 命中数经页面回报；结果经 done 交回（只调一次）。
type locateState struct {
	source   aiflow.ArticleSource
	text     string
	done     func(bool)
	finished atomic.Bool
	deadline time.Time
	switched bool
	sent     bool
}

func (l *locateState) finish(found bool) {
	if l.finished.CompareAndSwap(false, true) {
		l.done(found)
	}
}

// askOpenFor 报告这一条的提问栏是不是开着。
func (s *Screen) askOpenFor(id string) bool {
	return id != "" && s.ArticleAsk != nil && s.detail.ask.openID == id
}

// toggleAsk 开合提问栏：打开时收起文章工具侧栏。
func (s *Screen) toggleAsk(id string) {
	a := &s.detail.ask
	if a.openID == id {
		a.openID = ""
		return
	}
	s.closeTools()
	a.openID = id
}

// closeAsk 收起提问栏（栏里的 × 与 Esc；不停回答，渲染层面板卸载不 stop，× 自己先 stop 并保存）。
func (s *Screen) closeAsk() { s.detail.ask.openID = "" }

// releaseAsk 停掉文章问答在途的回答（阅读器卸载：换条目、关详情、离开知识库）。
func (s *Screen) releaseAsk() {
	if s.ArticleAsk != nil {
		s.ArticleAsk.ReleaseArticle()
	}
}

// syncAsk 在条目换了时收起栏并停掉在途的回答；条目没有时（空选中）同理。
func (s *Screen) syncAsk(id string) {
	a := &s.detail.ask
	if a.itemID == id {
		return
	}
	if a.itemID != "" {
		s.releaseAsk()
	}
	a.itemID = id
	if a.openID != id {
		a.openID = ""
	}
	a.prompt, a.selection = "", nil
}

// resetAsk 在详情卸载或重挂时调用：停掉回答、清空状态。在途的定位（来源在别的条目里，正等它打开）默认带过去，
// 工作区卸载时不带（dropLocate）：没有地方再画它了，按失败回报。
func (s *Screen) resetAsk(dropLocate bool) {
	a := &s.detail.ask
	if a.itemID != "" || a.openID != "" {
		s.releaseAsk()
	}
	locate := a.locate
	if dropLocate && locate != nil {
		locate.finish(false)
		locate = nil
	}
	a.openID, a.itemID, a.rootW = "", "", 0
	a.prompt, a.promptKey, a.selection = "", 0, nil
	a.submitted = nil
	a.locate = locate
}

// askEscape 处理 Esc：焦点落在提问栏里时收起它（停止回答并保存，由栏自己完成），并告诉调用方按键被拦下了。
func (s *Screen) askEscape(canvas *gfx.Canvas) bool {
	if s.ArticleAsk == nil || s.detail.ask.openID == "" {
		return false
	}
	return s.ArticleAsk.ArticleEscape(canvas)
}

// articleTargetOf 是阅读区此刻的提问目标（渲染层 data-article-target）：公众号原文快照 → snapshot（带版本）；
// 主题阅读的原文视图 → themed（带来源：正文或总结）；配图标签取 body；其余取当前标签。
func articleTargetOf(itemID string, view reader.View, active ReadingTab) aiflow.ArticleTarget {
	t := aiflow.ArticleTarget{ItemID: itemID, View: string(active)}
	switch view.Kind {
	case reader.ViewSnapshot:
		t.View = "snapshot"
		if view.SnapshotVersionID != "" {
			version := view.SnapshotVersionID
			t.VersionID = &version
		}
	case reader.ViewOriginal:
		t.View = "themed"
		kind := view.SourceKind
		t.SourceKind = &kind
	default:
		if active == TabImages {
			t.View = "body"
		}
	}
	return t
}

// 提问失败的原因（渲染层 ArticleAskReader 里同样的两句）：经提示条的详情给用户看。
var (
	errAskEditing = errors.New("请先完成编辑并保存，再围绕本文提问")
	errAskUnsaved = errors.New("正文尚未保存，请重试保存")
)

// askResolver 是发送前取目标的闭包（渲染层 resolveTarget）：在后台线程调用，只读这一帧捕获的值与线程安全的 Store 方法。
// 编辑态里不能提问（草稿不在库里）；正文还有没落盘的改动先落盘，落不了就报错。
func (s *Screen) askResolver(target aiflow.ArticleTarget, editing bool) func() (aiflow.ArticleTarget, error) {
	store, ctx := s.Store, s.Ctx
	return func() (aiflow.ArticleTarget, error) {
		if editing {
			return aiflow.ArticleTarget{}, errAskEditing
		}
		if !store.FlushPendingSave(ctx) || store.View().Detail.Unsaved {
			return aiflow.ArticleTarget{}, errAskUnsaved
		}
		return target, nil
	}
}

// askReader 包住内容面板：左边是内容面板（含阅读岛）取剩余宽度，右边是提问栏（relative flex min-h-0 flex-1）。
// 提问栏没开时就是一格直通的列，版面与没有这一层时一致。
func (s *Screen) askReader(c ui.C, v View, item services.LibraryItem, caps ItemCaps) {
	a := &s.detail.ask
	s.syncAsk(item.ID)
	if a.submitted != nil && a.submitted.Swap(false) {
		a.selection, a.prompt = nil, ""
	}
	open := s.askOpenFor(item.ID) && !caps.Trashed
	s.stepLocate(c, item, caps)
	ui.Row(c.Key("ask-reader")).FillWidth().Grow(1).MinHeight(0).Draw(func(d *ui.DrawContext) {
		a.rootW = d.Rect.Width
	}).Children(func() {
		ui.Column(c.Key("ask-content")).Grow(1).MinWidth(0).FillHeight().Children(func() {
			s.contentPanel(c, v, item, caps)
		})
		if open {
			s.askAside(c, item, caps)
		}
	})
}

// askAside 是提问栏：h-full shrink-0 border-l border-border；宽屏固定 400px，窄屏 min(400px,48%) 且不小于 280px。
func (s *Screen) askAside(c ui.C, item services.LibraryItem, caps ItemCaps) {
	l := s.Look
	t := l.T
	a := &s.detail.ask
	p := &s.detail.panel
	active := p.activeTab(caps)
	view := s.readerViewOf(item, caps, active)
	target := articleTargetOf(item.ID, view, active)
	if a.selection != nil && a.selection.ItemID == item.ID {
		target.Selection, target.Floor = a.selection.Selection, a.selection.Floor
	}
	if a.submitted == nil {
		a.submitted = &atomic.Bool{}
	}
	submitted := a.submitted
	spec := askscreen.ArticleSpec{
		Target: target, Prompt: a.prompt, PromptKey: a.promptKey,
		ResolveTarget: s.askResolver(target, s.editing(item, active)),
		OnClose:       s.closeAsk,
		OnLocate:      s.LocateArticle,
		OnSubmitted:   func() { submitted.Store(true) },
		AutoFocus:     true,
	}
	col := ui.Column(c.Key("ask-aside")).FillHeight().Shrink(0).PadSides(0, 0, 0, 1).Width(askPaneWidth)
	if a.rootW < askWideMin {
		col = col.MaxWidthPercent(askNarrowPct).MinWidth(askNarrowMin)
	}
	col.Draw(func(d *ui.DrawContext) {
		l.PaintEdge(d, kit.EdgeLeft, 1, t.Border)
	}).Children(func() {
		s.ArticleAsk.ArticlePanel(c, spec)
	})
}

// AskSelection 是选段提问条的落点（渲染层 askSelection）：阅读区里选中了文字（可带楼层）后，条上的两个钮之一被点——
// explain 为真是「解释这段」（带提示语），否则是「围绕这段提问」。目标在编辑态里不成立，失败以带原因的提示条告知；成功打开提问栏。
func (s *Screen) AskSelection(itemID, text string, floor *int64, explain bool) {
	a := &s.detail.ask
	text = strings.TrimSpace(text)
	if s.ArticleAsk == nil || text == "" || a.itemID != itemID {
		return
	}
	if s.detail.edit.key != "" {
		if n := s.Guard.Notify; n != nil {
			n.Error(locales.ArticleAskSendFailed(), errAskEditing.Error())
		}
		return
	}
	if a.submitted != nil {
		a.submitted.Store(false)
	}
	a.selection = &aiflow.ArticleTarget{ItemID: itemID, Selection: &text, Floor: floor}
	a.prompt = ""
	if explain {
		a.prompt = locales.ArticleAskExplainPrompt()
	}
	a.promptKey++
	s.closeTools()
	a.openID = itemID
}

// LocateArticle 把一条文章来源定位到阅读区里的原文（渲染层 locateArticleSource）：切到来源所在的视图（标签或公众号快照）、
// 对得上版本，再用查找条把原文片段（取选段或摘录的前 180 字）找出来；找到与否经 done 回报。
// 界面线程调用，done 可在任意 goroutine 调用且只调一次。来源所在的条目还没打开时（全局问答里点来源）等它打开，等不到按失败回报。
// 渲染层定位前还会核对来源的内容指纹（经 articleAsk:context 服务）；这里没有对应的服务入口，不核对，台账 B-061 的余项。
func (s *Screen) LocateArticle(source aiflow.ArticleSource, done func(found bool)) {
	a := &s.detail.ask
	if a.locate != nil {
		a.locate.finish(false)
		a.locate = nil
	}
	if source.Target == nil {
		done(false)
		return
	}
	text := source.Text
	if source.Target.Selection != nil && *source.Target.Selection != "" {
		text = *source.Target.Selection
	}
	text = strings.Join(strings.Fields(text), " ")
	if r := []rune(text); len(r) > 180 {
		text = string(r[:180])
	}
	if text == "" {
		done(false)
		return
	}
	a.locate = &locateState{source: source, text: text, done: done, deadline: s.nowTime().Add(askLocateWindow)}
}

// stepLocate 每帧推进在途的定位（详情画出来之后，条目 ID 与视图都在手）。
func (s *Screen) stepLocate(c ui.C, item services.LibraryItem, caps ItemCaps) {
	a := &s.detail.ask
	loc := a.locate
	if loc == nil {
		return
	}
	if loc.finished.Load() {
		a.locate = nil
		return
	}
	c.Canvas().RequestFrame()
	if s.nowTime().After(loc.deadline) {
		loc.finish(false)
		a.locate = nil
		return
	}
	target := loc.source.Target
	if item.ID != target.ItemID {
		return // 条目还在打开
	}
	p := &s.detail.panel
	if p.itemID != item.ID {
		return // 内容面板还停在上一条（本帧稍后的 syncPanel 才会换成这一条）：下一帧再看视图
	}
	if s.detail.edit.key != "" {
		loc.finish(false)
		a.locate = nil
		return
	}
	active := p.activeTab(caps)
	current := articleTargetOf(item.ID, s.readerViewOf(item, caps, active), active)
	if !sameArticleView(current, *target) {
		// 只使用匹配版本，不把历史引用定位到新版：切不过去就等到期按失败回报。
		if !loc.switched {
			loc.switched = true
			s.navigateTo(item, caps, *target)
		}
		return
	}
	if loc.sent {
		return
	}
	loc.sent = true
	p.findOpen, p.findQuery, p.findActive, p.findCount = true, loc.text, 0, 0
	p.find.SetText(loc.text)
	if !s.readerLive(item, caps, active) {
		// 没有阅读岛（文字稿是原生面板；单测与对照）：在条目文字里直接判断。
		found := strings.Contains(strings.Join(strings.Fields(item.Content), " "), loc.text) ||
			strings.Contains(strings.Join(strings.Fields(caps.Transcript), " "), loc.text)
		loc.finish(found)
		a.locate = nil
	}
}

// sameArticleView 判断两个目标指的是不是同一个阅读视图（视图名与版本都相同；themed 再看来源）。
func sameArticleView(a, b aiflow.ArticleTarget) bool {
	if a.View != b.View || derefStr(a.VersionID) != derefStr(b.VersionID) {
		return false
	}
	return a.View != "themed" || derefStr(a.SourceKind) == derefStr(b.SourceKind)
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// navigateTo 把阅读区切到目标的视图（渲染层 article-ask-navigate 事件）：公众号快照开关与标签；不存在的标签不切（之后按失败回报）。
func (s *Screen) navigateTo(item services.LibraryItem, caps ItemCaps, target aiflow.ArticleTarget) {
	p := &s.detail.panel
	if target.View == "snapshot" {
		if caps.IsWechat && !p.snapshotView {
			p.snapshotView = true
			p.findActive, p.findCount = 0, 0
		}
		return
	}
	if caps.IsWechat && p.snapshotView {
		p.snapshotView = false
		p.findActive, p.findCount = 0, 0
	}
	tab := ReadingTab(target.View)
	switch target.View {
	case "themed":
		tab = TabBody
		if derefStr(target.SourceKind) == sourceSummary {
			tab = TabSummary
		}
	}
	if p.restorable(tab, caps, item) && p.activeTab(caps) != tab {
		s.changeTab(item, caps, tab)
	}
}

// locateMatches 是页面回报的查找命中数：在途的定位在查找词发出之后收到的第一份回报就是结果。
func (s *Screen) locateMatches(count int) {
	a := &s.detail.ask
	if loc := a.locate; loc != nil && loc.sent {
		loc.finish(count > 0)
		a.locate = nil
	}
}
