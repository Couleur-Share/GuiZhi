// Package imports 是「采集」模块（导入任务队列）的原生页面。
//
// 数据流：后台 goroutine 经 services.Imports 取一页任务，整理成只读快照（行的展示文本在这里一次算好，
// 渲染函数里不做解析与格式化——MyGo 每帧重建控件树，重活会被乘上帧数），再 Invalidate 触发重绘。
// 视图只读快照；筛选、搜索与列表滚动状态只在 UI 线程读写。
//
// Package imports is the native page of the import-task queue.
package imports

import (
	"context"
	"sync"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"guizhi-mygo-shell/native/kit"
	"guizhi-mygo-shell/native/kit/icons"
	"guizhi-mygo-shell/native/locales"
	"guizhi-mygo-shell/native/services"
)

const (
	pageSize        = 100
	defaultDebounce = 150 * time.Millisecond
)

// Options 是 Screen 的依赖。
type Options struct {
	Imports services.Imports
	// Invalidate 请求重绘，可在任意 goroutine 调用；nil 时不重绘（测试自行驱动帧）。
	Invalidate func()
	// Now 返回当前时间；nil 用 time.Now。
	Now func() time.Time
	// Locale 返回当前语言；nil 用 i18n.Locale。
	Locale func() string
	// Debounce 是筛选/搜索变化到发起查询的间隔；0 取默认 150ms，负数表示立即。
	Debounce time.Duration
}

// query 是一次查询的筛选条件（与 UI 状态的拷贝，供后台 goroutine 使用）。
type query struct {
	filter string
	text   string
}

// Screen 是页面状态。
type Screen struct {
	o Options

	mu     sync.Mutex // 保护下面四项：后台写、UI 线程读
	snap   snapshot
	seq    uint64
	cur    query       // View 每帧写入的当前条件，供事件触发的刷新使用
	timer  *time.Timer // 去抖定时器
	cancel func()      // OnChange 订阅的取消函数
	closed bool

	// 仅 UI 线程读写
	filterIdx int
	text      string
	list      ui.ListState
	lastKey   query
	started   bool
}

// New 创建页面；调用 Start 之后才订阅事件并加载。
func New(o Options) *Screen {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Locale == nil {
		o.Locale = i18n.Locale
	}
	if o.Debounce == 0 {
		o.Debounce = defaultDebounce
	}
	return &Screen{o: o}
}

// Start 订阅任务变化并发起首次加载。
func (s *Screen) Start() {
	s.mu.Lock()
	if s.cancel == nil && !s.closed {
		s.cancel = s.o.Imports.OnChange(s.onChange)
	}
	s.mu.Unlock()
	// 首次加载放到后台：Start 在 UI 线程上被调用，同步查库会让窗口在数据库慢或被占用时卡住不出画面。
	go s.reload(query{filter: services.ImportFilterAll})
}

// Close 取消订阅与待执行的查询；之后到达的响应被丢弃。
func (s *Screen) Close() {
	s.mu.Lock()
	s.closed = true
	if s.timer != nil {
		s.timer.Stop()
	}
	cancel := s.cancel
	s.cancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *Screen) invalidate() {
	if s.o.Invalidate != nil {
		s.o.Invalidate()
	}
}

// onChange 在任务状态变化时被调用（任意 goroutine）：去抖后按当前条件刷新。
func (s *Screen) onChange() {
	s.mu.Lock()
	q := s.cur
	s.mu.Unlock()
	s.schedule(q)
}

// schedule 去抖地发起一次查询：连续多次触发只执行最后一次。
func (s *Screen) schedule(q query) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if s.timer != nil {
		s.timer.Stop()
	}
	if s.o.Debounce < 0 {
		go s.reload(q)
		return
	}
	s.timer = time.AfterFunc(s.o.Debounce, func() { s.reload(q) })
}

// reload 同步执行一次查询并应用结果；以序号丢弃过期响应（后发起的查询先返回时不被先发起的覆盖）。
func (s *Screen) reload(q query) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.seq++
	seq := s.seq
	s.snap.loading = true
	s.mu.Unlock()

	page, err := s.o.Imports.List(context.Background(), services.ImportQuery{
		Origin: "all", Status: q.filter, Query: q.text, PageSize: pageSize,
	})

	s.mu.Lock()
	if s.closed || seq != s.seq {
		s.mu.Unlock()
		return
	}
	if err != nil {
		// 失败保留上一份数据与总数不展示：错误态由视图用 LoadErrorState 呈现，不渲染成空态。
		s.snap.loading, s.snap.err = false, err.Error()
	} else {
		s.snap = buildSnapshot(page, q, s.o.Now(), s.o.Locale())
	}
	s.mu.Unlock()
	s.invalidate()
}

func (s *Screen) snapshot() snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snap
}

// Ready 报告首次加载是否已有结果（成功或失败都算），可在任意 goroutine 调用。
// -bench 的就绪标记用它判断「首屏已有真实内容」。
func (s *Screen) Ready() bool {
	snap := s.snapshot()
	return snap.loaded || snap.err != ""
}

// filters 是左侧「任务概览」里的过滤项（顺序与渲染层 SidebarImportsPanel 一致）。
var filters = []struct {
	id    string
	label func() string
}{
	{services.ImportFilterAll, locales.ImportsPanelAll},
	{services.ImportFilterActive, locales.ImportsPanelActive},
	{services.ImportStatusCompleted, locales.ImportsPanelCompleted},
	{services.ImportFilterDegraded, locales.ImportsPanelDegraded},
	{services.ImportStatusDuplicate, locales.ImportsPanelDuplicate},
	{services.ImportStatusFailed, locales.ImportsPanelFailed},
	{services.ImportStatusCanceled, locales.ImportsPanelCanceled},
}

// View 绘制页面。
func (s *Screen) View(c *ui.Context, t *kit.Look) {
	q := query{filter: filters[s.filterIdx].id, text: s.text}
	if !s.started {
		s.started, s.lastKey = true, q
	} else if q != s.lastKey {
		s.lastKey = q
		s.schedule(q)
	}
	s.mu.Lock()
	s.cur = q
	s.mu.Unlock()
	snap := s.snapshot()

	ui.Column(c).Fill().AlignItems(ui.Stretch).Children(func() {
		s.header(c, t, snap)
		s.toolbar(c, t, snap)
		switch {
		case snap.err != "":
			kit.LoadErrorState(c, t, snap.err, func() { go s.reload(q) })
		case !snap.loaded:
			ui.Column(c).Grow(1).Center().Gap(8).Children(func() {
				ui.Spinner(c)
				ui.Text(c, locales.CommonLoading()).TextColor(t.TextMuted)
			})
		default:
			s.body(c, t, snap, q)
		}
	})
}

func (s *Screen) header(c *ui.Context, t *kit.Look, snap snapshot) {
	ui.Row(c).Padding(12, 16).Gap(12).AlignItems(ui.Center).Height(56).
		BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		kit.Icon(c, icons.Download, 18).TextColor(t.TextMuted)
		ui.Text(c, locales.ImportsTitle()).FontSize(16).FontWeight(600)
		if snap.loaded && snap.err == "" {
			summary := locales.ImportsSummaryTotal(snap.total)
			if snap.active > 0 {
				summary = locales.ImportsSummaryActive(snap.total, snap.active)
			}
			ui.Text(c, summary).FontSize(12).TextColor(t.TextMuted)
		}
		ui.Box(c).Grow(1)
		ui.SearchField(c, &s.text).Width(260).Label(locales.ImportsSearchPlaceholder()).
			Placeholder(locales.ImportsSearchPlaceholder())
	})
}

func (s *Screen) toolbar(c *ui.Context, t *kit.Look, snap snapshot) {
	ui.Row(c).Padding(8, 16).Gap(12).AlignItems(ui.Center).Children(func() {
		labels := make([]string, len(filters))
		for i, f := range filters {
			labels[i] = f.label()
			if n := snap.counts[f.id]; n > 0 && f.id != services.ImportFilterAll {
				labels[i] += " " + itoa(n)
			}
		}
		ui.Segmented(c, &s.filterIdx, labels...)
	})
}

func (s *Screen) body(c *ui.Context, t *kit.Look, snap snapshot, q query) {
	if len(snap.rows) == 0 {
		s.empty(c, t, q)
		return
	}
	ui.List(c, &s.list, len(snap.rows), func(i int) {
		drawRow(c, t, snap.rows[i])
	}).Grow(1).Padding(0, 16, 16, 16).Gap(6)
}

// empty 区分「队列本来就空」和「筛选后没有」：后者给出清除筛选的出口。
func (s *Screen) empty(c *ui.Context, t *kit.Look, q query) {
	filtered := q.filter != services.ImportFilterAll || q.text != ""
	ui.Column(c).Grow(1).Center().Gap(8).Padding(24).Children(func() {
		if filtered {
			ui.Text(c, locales.ImportsFilteredEmpty()).TextColor(t.TextMuted)
			if kit.OutlineButton(c, t, icons.RotateCcw, locales.ImportsResetFilter()) {
				s.filterIdx, s.text = 0, ""
			}
			return
		}
		ui.Text(c, locales.ImportsEmpty()).FontSize(14).FontWeight(500)
		ui.Text(c, locales.ImportsEmptyDesc()).FontSize(12).TextColor(t.TextMuted).MaxWidth(420).Wrap()
	})
}
