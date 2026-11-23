package overlays

// 全局命令面板（GlobalCommandPalette.tsx）：一个搜索框加分组结果列表，输入时在知识库、Wiki、采集任务里做远程搜索，
// 同时匹配静态的「常用动作」与设置分区，上下键选择、回车执行、Esc 关闭。
//
// 本文件是它的状态层：打开与关闭、查询防抖、过期响应丢弃、结果合并与选中项、执行动作。绘制在 Root 里（后续步骤）。

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

const (
	// paletteDebounce 是输入停止多久之后才发起远程搜索（渲染层 150 ms）。
	paletteDebounce = 150 * time.Millisecond
	// paletteRemoteLimit 是每个来源最多显示几条（渲染层 5）。
	paletteRemoteLimit = 5
	// paletteTaskPageSize 是采集任务搜索请求的页大小（渲染层 20，取回后只留前 5 条）。
	paletteTaskPageSize = 20
	// paletteSettingsLimit 是设置分区最多显示几条（渲染层 slice(0, 5)）。
	paletteSettingsLimit = 5
)

// PaletteKind 是结果项的类别：界面据此选图标。
type PaletteKind uint8

const (
	PaletteAction PaletteKind = iota
	PaletteKnowledge
	PaletteWiki
	PaletteTask
	PaletteSetting
)

// 静态动作的键（PaletteItem.Key）。
const (
	PaletteKeyCapture = "action:capture"
	PaletteKeyInbox   = "action:inbox"
	PaletteKeyBackup  = "action:backup"
	PaletteKeyIndex   = "action:index"
	PaletteKeyWiki    = "action:wiki"
)

// 搜索来源（PaletteView.FailedSources 的元素）。
const (
	SourceKnowledge = "knowledge"
	SourceWiki      = "wiki"
	SourceTasks     = "tasks"
)

// settingsKeywords 是设置分区的检索关键词：不随界面语言切换，保留中文关键词并附英文关键词，中英文界面都能搜到
// （渲染层 SETTINGS 的 keywords）。中文放在数据文件里，因为界面文案不能以字面量形式写在 Go 代码里（nocjk 门禁）；
// palette_keywords_test.go 把它与渲染层源码逐项核对。
//
//go:embed palette_keywords.json
var settingsKeywordsJSON []byte

// paletteSettings 是设置分区项，顺序与渲染层 SETTINGS 一致；title 取当前界面语言的文案。
var paletteSettings = []struct {
	id    string
	title func() string
}{
	{"general", locales.SettingsGeneral},
	{"capture", locales.SettingsCaptureAndTranscription},
	{"ai", locales.CommandPaletteSettingsAi},
	{"data", locales.CommandPaletteSettingsData},
	{"appearance", locales.CommandPaletteSettingsAppearance},
	{"shortcuts", locales.CommandPaletteSettingsShortcuts},
}

var settingsKeywords = func() map[string]string {
	var m map[string]string
	if err := json.Unmarshal(settingsKeywordsJSON, &m); err != nil {
		panic(err) // 嵌入的常量解码失败是程序错误
	}
	return m
}()

// PaletteItem 是结果列表里的一项。
type PaletteItem struct {
	// Key 是稳定标识（动作为 PaletteKey*，其余是「类别:id」），界面用它做列表项的 key。
	Key  string
	Kind PaletteKind
	// Group 是分组标题：相邻两项的 Group 不同时，界面在后一项上方画分组标题。
	Group    string
	Title    string
	Subtitle string
	// Arg 是执行时交给钩子的参数：条目与 Wiki 页面是 id，采集任务是显示名，设置是分区 id；动作为空。
	Arg string
}

// PaletteView 是命令面板的快照。
type PaletteView struct {
	Open  bool
	Query string
	Items []PaletteItem
	// Active 是选中项的下标（没有结果时为 0，此时回车不执行任何东西）。
	Active int
	// Searching 为真表示远程搜索在防抖等待中或请求在途（界面可显示转圈；结果列表仍是上一次的结果）。
	Searching bool
	// FailedSources 是最近一次远程搜索里失败的来源（Source*）：它们没有结果、也已记日志。
	// 渲染层把任何一个来源失败都吞成「全部没有结果」；这里只让失败的那一个来源为空，并让界面有机会如实说明。
	FailedSources []string
}

// PaletteHooks 是执行结果项的出口。除备份外都在调用 Run 的线程（界面线程）里同步调用；为 nil 的钩子什么也不做。
type PaletteHooks struct {
	// QuickCapture 打开快速采集（渲染层派发 shortcut:newItem）。
	QuickCapture func()
	// OpenInbox 切到处理中心。
	OpenInbox func()
	// BuildSemanticIndex 立即建立语义索引（与处理中心「建立语义索引」同一动作，失败与进度提示由它负责）。
	BuildSemanticIndex func()
	// CompileWiki 打开 Wiki 全文编译对话框。
	CompileWiki func()
	// OpenLibraryItem 切到知识库、范围回到「全部」并选中条目。
	OpenLibraryItem func(itemID string)
	// OpenWikiPage 切到 Wiki 并选中页面。
	OpenWikiPage func(pageID string)
	// SearchImportTasks 切到采集模块并把搜索词设为任务显示名。
	SearchImportTasks func(displayName string)
	// OpenSettingsSection 打开设置页的某一节（general、capture、ai、data、appearance、shortcuts）。
	OpenSettingsSection func(section string)
}

// Palette 管理命令面板。方法可从任意 goroutine 调用。
type Palette struct {
	Env
	Library services.Library
	Wiki    services.Wiki
	Imports services.Imports
	Backup  services.Backup
	Guard   kit.Guard
	Hooks   PaletteHooks
	// UI 把一个动作交给界面线程执行；备份在后台发现仓库没有初始化时，要经它去打开设置页。nil 时直接在当前线程调用。
	UI func(fn func())

	invalidate func()

	mu         sync.Mutex
	open       bool
	query      string
	remote     paletteRemote
	failed     []string
	active     int
	gen        uint64 // 每次查询变化、关闭与重新打开加一：晚到的搜索结果据此判断自己是不是过期
	pending    bool
	cancel     context.CancelFunc
	backupBusy bool
}

type paletteRemote struct {
	knowledge []services.LibraryEntry
	wiki      []services.WikiSearchHit
	tasks     []services.ImportTask
}

// NewPalette 创建命令面板；invalidate 在状态变化后请求窗口重绘。
func NewPalette(guard kit.Guard, invalidate func()) *Palette {
	return &Palette{Guard: guard, invalidate: invalidate}
}

// View 返回当前快照。
func (p *Palette) View() PaletteView {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.viewLocked()
}

func (p *Palette) viewLocked() PaletteView {
	v := PaletteView{Open: p.open, Query: p.query, Active: p.active, Searching: p.pending, FailedSources: slices.Clone(p.failed)}
	if p.open {
		v.Items = p.itemsLocked()
	}
	return v
}

// itemsLocked 是当前的结果列表：静态动作（按标题过滤）、知识条目、Wiki 页面、采集任务、设置分区，顺序与渲染层一致。
func (p *Palette) itemsLocked() []PaletteItem {
	normalized := strings.ToLower(strings.TrimSpace(p.query))
	var items []PaletteItem
	actions := locales.CommandPaletteGroupActions()
	for _, a := range []struct{ key, title string }{
		{PaletteKeyCapture, locales.CaptureTitle()},
		{PaletteKeyInbox, locales.CommandPaletteOpenInbox()},
		{PaletteKeyBackup, locales.SettingsFullBackupNow()},
		{PaletteKeyIndex, locales.CommandPaletteBuildSemanticIndex()},
		{PaletteKeyWiki, locales.CommandPaletteCompileWiki()},
	} {
		if normalized == "" || strings.Contains(strings.ToLower(a.title), normalized) {
			items = append(items, PaletteItem{Key: a.key, Kind: PaletteAction, Group: actions, Title: a.title})
		}
	}
	for _, e := range p.remote.knowledge {
		items = append(items, PaletteItem{Key: "knowledge:" + e.ID, Kind: PaletteKnowledge, Group: locales.CommandPaletteGroupKnowledge(), Title: e.Title, Subtitle: e.Snippet, Arg: e.ID})
	}
	for _, w := range p.remote.wiki {
		items = append(items, PaletteItem{Key: "wiki:" + w.ID, Kind: PaletteWiki, Group: "Wiki", Title: w.Title, Subtitle: w.Summary, Arg: w.ID})
	}
	for _, t := range p.remote.tasks {
		sub := t.Status
		if t.Error != "" {
			sub += " · " + t.Error
		}
		items = append(items, PaletteItem{Key: "task:" + t.ID, Kind: PaletteTask, Group: locales.ImportsTitle(), Title: t.DisplayName, Subtitle: sub, Arg: t.DisplayName})
	}
	shown := 0
	for _, s := range paletteSettings {
		if shown == paletteSettingsLimit {
			break
		}
		title := s.title()
		if normalized == "" || strings.Contains(strings.ToLower(title+" "+settingsKeywords[s.id]), normalized) {
			items = append(items, PaletteItem{Key: "setting:" + s.id, Kind: PaletteSetting, Group: locales.SettingsTitle(), Title: title, Arg: s.id})
			shown++
		}
	}
	return items
}

func (p *Palette) wake() { wake(p.invalidate) }

// cancelInFlightLocked 作废在途的远程搜索：代数加一，并取消它的请求。
func (p *Palette) cancelInFlightLocked() {
	p.gen++
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.pending = false
}

// Open 打开面板：清空搜索词与上一次的结果（渲染层 show）。已经打开时同样重置。
func (p *Palette) Open() {
	p.mu.Lock()
	p.cancelInFlightLocked()
	p.open, p.query, p.active = true, "", 0
	p.remote, p.failed = paletteRemote{}, nil
	p.mu.Unlock()
	p.wake()
}

// Close 关闭面板，并作废在途的搜索。
func (p *Palette) Close() {
	p.mu.Lock()
	wasOpen := p.open
	p.cancelInFlightLocked()
	p.open = false
	p.mu.Unlock()
	if wasOpen {
		p.wake()
	}
}

// SetQuery 设置搜索词：静态项立即按新词过滤，远程搜索防抖后发起；词为空白时清掉远程结果。选中项回到第一项。
func (p *Palette) SetQuery(q string) {
	p.mu.Lock()
	if !p.open || q == p.query {
		p.mu.Unlock()
		return
	}
	p.query, p.active = q, 0
	p.cancelInFlightLocked()
	text := strings.TrimSpace(q)
	if text == "" {
		p.remote, p.failed = paletteRemote{}, nil
		p.mu.Unlock()
		p.wake()
		return
	}
	p.pending = true
	gen := p.gen
	p.mu.Unlock()
	p.wake()
	p.after(paletteDebounce, func() { p.search(gen, text) })
}

// search 在防抖到期后发起三个来源的并行搜索；每个来源独立成败，过期的结果（代数变了）整体丢弃。
func (p *Palette) search(gen uint64, text string) {
	p.mu.Lock()
	if gen != p.gen || !p.open {
		p.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(p.context())
	p.cancel = cancel
	p.mu.Unlock()

	p.spawn(func() {
		defer cancel()
		var (
			r      paletteRemote
			failed []string
			fmu    sync.Mutex
			wg     sync.WaitGroup
		)
		fail := func(source string, err error) {
			if ctx.Err() != nil {
				return // 被新的查询取消：不是失败
			}
			p.Guard.Background("palette.search."+source, err)
			fmu.Lock()
			failed = append(failed, source)
			fmu.Unlock()
		}
		run := func(fn func()) {
			wg.Add(1)
			go func() { defer wg.Done(); fn() }()
		}
		if p.Library != nil {
			run(func() {
				page, err := p.Library.List(ctx, services.LibraryListQuery{
					Scope: services.LibraryScopeAll, Search: text, SearchMode: services.LibrarySearchRecall, Limit: paletteRemoteLimit,
				})
				if err != nil {
					fail(SourceKnowledge, err)
					return
				}
				r.knowledge = firstN(page.Entries, paletteRemoteLimit)
			})
		}
		if p.Wiki != nil {
			run(func() {
				hits, err := p.Wiki.Search(ctx, text, paletteRemoteLimit)
				if err != nil {
					fail(SourceWiki, err)
					return
				}
				r.wiki = firstN(hits, paletteRemoteLimit)
			})
		}
		if p.Imports != nil {
			run(func() {
				page, err := p.Imports.List(ctx, services.ImportQuery{Status: services.ImportFilterAll, Query: text, PageSize: paletteTaskPageSize})
				if err != nil {
					fail(SourceTasks, err)
					return
				}
				r.tasks = firstN(page.Entries, paletteRemoteLimit)
			})
		}
		wg.Wait()

		p.mu.Lock()
		if gen != p.gen || !p.open {
			p.mu.Unlock()
			return
		}
		before := len(p.itemsLocked())
		p.remote, p.failed, p.pending = r, failed, false
		slices.Sort(p.failed)
		if len(p.itemsLocked()) != before {
			p.active = 0 // 渲染层：结果数变化时选中项回到第一项
		}
		p.cancel = nil
		p.mu.Unlock()
		p.wake()
	})
}

func firstN[T any](s []T, n int) []T {
	if len(s) > n {
		s = s[:n]
	}
	return slices.Clone(s)
}

// Move 把选中项上下移动（delta 为 ±1），夹在结果范围内（渲染层 ArrowDown / ArrowUp）。
func (p *Palette) Move(delta int) {
	p.mu.Lock()
	n := len(p.itemsLocked())
	next := min(max(p.active+delta, 0), max(n-1, 0))
	changed := next != p.active
	p.active = next
	p.mu.Unlock()
	if changed {
		p.wake()
	}
}

// SetActive 设置选中项（鼠标悬停），越界忽略。
func (p *Palette) SetActive(i int) {
	p.mu.Lock()
	ok := i >= 0 && i < len(p.itemsLocked()) && i != p.active
	if ok {
		p.active = i
	}
	p.mu.Unlock()
	if ok {
		p.wake()
	}
}

// RunActive 执行选中项（回车）；没有结果时什么也不做并返回 false。
func (p *Palette) RunActive() bool {
	p.mu.Lock()
	i := p.active
	p.mu.Unlock()
	return p.Run(i)
}

// Run 关闭面板并执行第 i 项（点击或回车）；越界返回 false。
func (p *Palette) Run(i int) bool {
	p.mu.Lock()
	items := p.itemsLocked()
	if !p.open || i < 0 || i >= len(items) {
		p.mu.Unlock()
		return false
	}
	it := items[i]
	p.cancelInFlightLocked()
	p.open = false
	p.mu.Unlock()
	p.wake()

	h := p.Hooks
	switch it.Kind {
	case PaletteAction:
		switch it.Key {
		case PaletteKeyCapture:
			call(h.QuickCapture)
		case PaletteKeyInbox:
			call(h.OpenInbox)
		case PaletteKeyBackup:
			p.runBackup()
		case PaletteKeyIndex:
			call(h.BuildSemanticIndex)
		case PaletteKeyWiki:
			call(h.CompileWiki)
		}
	case PaletteKnowledge:
		call1(h.OpenLibraryItem, it.Arg)
	case PaletteWiki:
		call1(h.OpenWikiPage, it.Arg)
	case PaletteTask:
		call1(h.SearchImportTasks, it.Arg)
	case PaletteSetting:
		call1(h.OpenSettingsSection, it.Arg)
	}
	return true
}

func call(fn func()) {
	if fn != nil {
		fn()
	}
}

func call1(fn func(string), arg string) {
	if fn != nil {
		fn(arg)
	}
}

var errBackupUnavailable = errors.New("backup service is not available")

// runBackup 是「立即完整备份」：先看仓库有没有初始化，没有就去设置的「数据」分区并提示；有就做一次完整备份。
// 在后台执行；同一时刻只做一次（面板关闭后可以再打开，不能叠起来两次备份）。
func (p *Palette) runBackup() {
	p.mu.Lock()
	if p.backupBusy {
		p.mu.Unlock()
		return
	}
	p.backupBusy = true
	p.mu.Unlock()

	p.spawn(func() {
		defer func() {
			p.mu.Lock()
			p.backupBusy = false
			p.mu.Unlock()
		}()
		ctx := p.context()
		action := locales.SettingsFullBackupNow()
		if p.Backup == nil {
			p.Guard.Report(action, errBackupUnavailable)
			return
		}
		status, err := p.Backup.RepositoryStatus(ctx)
		if err != nil {
			p.Guard.Report(action, err)
			return
		}
		if !status.Initialized {
			if open := p.Hooks.OpenSettingsSection; open != nil {
				p.onUI(func() { open("data") })
			}
			if p.Guard.Notify != nil {
				p.Guard.Notify.Warning(locales.CommandPaletteBackupNotInitialized())
			}
			return
		}
		// 恢复口令留空：密钥环可用时仓库自行解锁（渲染层同样不传口令，失败原因照实展示）
		if _, err := p.Backup.CreateSnapshot(ctx, ""); err != nil {
			p.Guard.Fail(locales.SettingsBackupFailed(), "palette.backup", err)
			return
		}
		p.Guard.Succeed(locales.CommandPaletteBackupDone())
	})
}

func (p *Palette) onUI(fn func()) {
	if p.UI != nil {
		p.UI(fn)
		return
	}
	fn()
}
