package scenes

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/parity"
	importsscreen "guizhi-mygo-shell/viva/screens/imports"
	shellscreen "guizhi-mygo-shell/viva/screens/shell"
)

// importsOptions 对应 React 采集器 enterImports 的选项：数据、空、读取中、读取失败，以及队列是否暂停、是否还有更早的任务。
type importsOptions struct {
	mode    string // data | empty | loading | error
	paused  bool
	hasMore bool
}

// fakeImports 是采集队列的夹具实现：按服务端口径（import_list.go）从固定任务集里筛选，
// 与 React 采集器在页内替换的 window.api.import 一致（scripts/parity/lib/react-imports.mjs）。
type fakeImports struct {
	opt   importsOptions
	err   string
	conc  int
	tasks []services.ImportTask
}

func newFakeImports(fix parity.ImportsFixture, opt importsOptions) *fakeImports {
	f := &fakeImports{opt: opt, err: fix.LoadError, conc: fix.Queue.Concurrency}
	if opt.mode != "empty" {
		f.tasks = fix.Tasks
	}
	return f
}

func isMobile(t services.ImportTask) bool { return t.Origin == importsscreen.OriginMobile }

// matchesSearch 是服务端的来源与关键字筛选。
func matchesSearch(t services.ImportTask, origin, query string) bool {
	if origin == importsscreen.OriginMobile && !isMobile(t) {
		return false
	}
	if origin == importsscreen.OriginDesktop && isMobile(t) {
		return false
	}
	keyword := strings.ToLower(strings.TrimSpace(query))
	if keyword == "" {
		return true
	}
	for _, s := range []string{t.DisplayName, t.SourceInput, t.Error, t.Warning} {
		if strings.Contains(strings.ToLower(s), keyword) {
			return true
		}
	}
	return false
}

// matchesStatus 是服务端的状态筛选。
func matchesStatus(t services.ImportTask, status string) bool {
	switch status {
	case "", "all":
		return true
	case "active":
		return importsscreen.IsActive(t)
	case "degraded":
		return t.Status == services.ImportStatusCompleted && t.Warning != ""
	}
	return t.Status == status
}

func (f *fakeImports) List(ctx context.Context, q services.ImportQuery) (services.ImportPage, error) {
	switch f.opt.mode {
	case "loading":
		<-ctx.Done()
		return services.ImportPage{}, ctx.Err()
	case "error":
		return services.ImportPage{}, errors.New(f.err)
	}
	if f.opt.hasMore && q.Cursor != "" {
		return services.ImportPage{}, nil
	}
	origin := q.Origin
	if origin == "" {
		origin = importsscreen.OriginAll
	}
	var matched, filtered []services.ImportTask
	for _, t := range f.tasks {
		if !matchesSearch(t, origin, q.Query) {
			continue
		}
		matched = append(matched, t)
		if matchesStatus(t, q.Status) {
			filtered = append(filtered, t)
		}
	}
	entries := append([]services.ImportTask(nil), filtered...)
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].CreatedAt != entries[j].CreatedAt {
			return entries[i].CreatedAt > entries[j].CreatedAt
		}
		return entries[i].ID > entries[j].ID
	})
	var active []services.ImportTask
	counts := map[string]int64{}
	var degraded int64
	for _, t := range matched {
		counts[t.Status]++
		if importsscreen.IsActive(t) {
			active = append(active, t)
		}
		if t.Status == services.ImportStatusCompleted && t.Warning != "" {
			degraded++
		}
	}
	sort.SliceStable(active, func(i, j int) bool {
		if active[i].CreatedAt != active[j].CreatedAt {
			return active[i].CreatedAt < active[j].CreatedAt
		}
		return active[i].ID < active[j].ID
	})
	page := services.ImportPage{Entries: entries, Active: active, Total: int64(len(filtered)), Counts: counts, DegradedCount: degraded}
	if f.opt.hasMore {
		page.NextCursor = "parity-next"
	}
	return page, nil
}

func (f *fakeImports) OnChange(func()) func()                        { return func() {} }
func (f *fakeImports) OnTaskChange(func(services.ImportTask)) func() { return func() {} }

func (f *fakeImports) QueueState(context.Context) (services.ImportQueueState, error) {
	st := services.ImportQueueState{Paused: f.opt.paused, Concurrency: f.conc}
	for _, t := range f.tasks {
		switch t.Status {
		case services.ImportStatusProcessing:
			st.RunningCount++
		case services.ImportStatusPending:
			st.PendingCount++
		}
	}
	return st, nil
}

func (f *fakeImports) SetPaused(ctx context.Context, paused bool) (services.ImportQueueState, error) {
	f.opt.paused = paused
	return f.QueueState(ctx)
}

func (f *fakeImports) Cancel(context.Context, string) error { return nil }
func (f *fakeImports) Retry(context.Context, string, services.ImportRetryOptions) (bool, error) {
	return true, nil
}
func (f *fakeImports) Remove(context.Context, string) error { return nil }

func (f *fakeImports) clearCount(q services.ImportClearQuery) int {
	origin := q.Origin
	if origin == "" {
		origin = importsscreen.OriginAll
	}
	n := 0
	for _, t := range f.tasks {
		if !importsscreen.IsActive(t) && matchesSearch(t, origin, q.Query) && (q.Scope == "all" || matchesStatus(t, q.Status)) {
			n++
		}
	}
	return n
}

func (f *fakeImports) PreviewClearTerminal(_ context.Context, q services.ImportClearQuery) (int, error) {
	return f.clearCount(q), nil
}
func (f *fakeImports) ClearTerminal(_ context.Context, q services.ImportClearQuery) (int, error) {
	return f.clearCount(q), nil
}
func (f *fakeImports) Enqueue(context.Context, []services.EnqueueImportInput) ([]services.ImportTask, error) {
	return nil, nil
}
func (f *fakeImports) SelectFiles(context.Context) ([]string, error) { return nil, nil }

// letterSpacingWaivers 登记 A-013：二级侧栏小标题「任务概览」用了 tracking-wide（0.025em），Viva 的文字没有字距，
// 文字盒宽度按「字符数 × 字距」补齐（位置对得上），字形本身的绘制宽度窄 字符数 × 字距（12px 时 4 × 0.3 = 1.2px）。
var letterSpacingWaivers = []parity.Waiver{{
	Ledger: "A-013", Layer: parity.LayerL2, Kind: "text-width", Subject: "任务概览",
	Note: "tracking-wide 的字距：Viva 没有字距，绘制宽度窄 字符数 × 0.025em（盒宽已补齐，后面的元素位置一致）",
}}

// kerningWaivers 登记 A-014：空队列引导的来源提示里「B站 / YouTube / 抖音」含字偶对（Yo、Tu），Viva 不做字偶距，
// 整行宽 2.25px；提示块在居中的行里，同行其余提示块的图标随之偏半个宽度差（≤ 1.2px，大字号档 ≤ 1.3px），
// 个别图标落在半像素的取整边界上，墨迹整体差 1 像素。后两类按偏差大小设了上限，更大的偏差照常判定。
var kerningWaivers = []parity.Waiver{
	{
		Ledger: "A-014", Layer: parity.LayerL2, Kind: "text-width", Subject: "B站 / YouTube / 抖音",
		Note: "字偶距：Chromium 的 Yo、Tu 字偶对共 −2.25px（12px 时），Viva 不做字偶距，整行宽 2.25px",
	},
	{
		Ledger: "A-014", Layer: parity.LayerL2, Kind: "icon-box", MaxDelta: 1.5,
		Note: "居中的提示行里，上面那段文字的宽度差（A-014）使同行图标偏半个宽度差（≤ 1.3px）",
	},
	{
		Ledger: "A-014", Layer: parity.LayerL2, Kind: "ink", Subject: "图标", MaxDelta: 1,
		Note: "同上：图标位置在半像素取整边界上翻到另一侧，墨迹整体差 1 像素",
	},
	{
		Ledger: "A-014", Layer: parity.LayerL2, Kind: "ink", Subject: "网页文章", MaxDelta: 2,
		Note: "同上：该文字在含字偶对那段文字的左侧，整行居中，位置偏 Δ/2（大字号档 1.27px）再加半像素取整，墨迹最佳位移 2 像素",
	},
}

// tinyIconWaivers 登记 A-003 在小图标上的延伸：小字号档（根字号 14px）下「重复内容」标签里 8.75px 的 Copy 图标，
// 笔画只有 0.73px 宽，Skia 的采样量化与 Viva 的精确面积覆盖相差更大，墨迹总量比 1.14–1.153（校准范围 12px 以上的图标是 0.90–1.04）。
// 上限 0.16：比量到的 0.153 略宽，同一枚图标更大的偏差照常判定。
var tinyIconWaivers = []parity.Waiver{{
	// 主题名里「 8.75 8.75]」是外框的宽高：只放过这个尺寸的图标（外框写成 [x y w h]），不依赖它在页面上的位置。
	Ledger: "A-003", Layer: parity.LayerL2, Kind: "ink-mass", Subject: " 8.75 8.75]", MaxDelta: 0.16,
	Note: "8.75px 的 Copy 图标（笔画 0.73px）：Skia 的采样量化与 Viva 的精确面积覆盖差更大，墨迹量比 1.14–1.153",
}}

// kerningInkWaiver 登记一行含拉丁字偶对的长文字的墨迹位移（A-014）：Viva 不做字偶距，整行宽度比渲染层多出
// 字偶对的总和（如 yt-dlp 提示行 0.68px），整行墨迹剖面的最佳对齐位移被推到 1 像素；上限 1，更大的偏差照常判定。
func kerningInkWaiver(subject string) parity.Waiver {
	return parity.Waiver{
		Ledger: "A-014", Layer: parity.LayerL2, Kind: "ink", Subject: subject, MaxDelta: 1,
		Note: "字偶距：整行宽度差（< 1px）累积成墨迹剖面的最佳对齐位移 1 像素",
	}
}

// tooltipBreakWaivers 登记长网址气泡的折行位置偏差（A-014 的延伸）：气泡里的网址没有空格，渲染层与 Viva 都在行满处按字符折行，
// 而 Chromium 做字偶距、整行比 Viva（不做字偶距）窄——实测「https://www.zhihu.com/question/123456789/answer」47 个字符
// 渲染层 301.625px、Viva 302.332px（逐字之和，与整串相同），正好跨过内容宽度 302px：渲染层放得下的最后一个字符「r」Viva 放不下，
// 折到下一行。所以折行点差一个字符：第二行宽度差一个字符的宽度（实测 4.57px），整段墨迹随之偏一个字符。
// 外框、行数、气泡位置都一致（外框宽度折行不缩窄，见 kit.tooltipMaxContent）；上限取一个拉丁字符的宽度（8px），更大的偏差照常判定。
var tooltipBreakWaivers = []parity.Waiver{
	{
		Ledger: "A-014", Layer: parity.LayerL2, Kind: "text-width", Subject: "zhihu.com", MaxDelta: 8,
		Note: "字偶距使渲染层的整行比 Viva 窄 0.71px，行满处的折行点差一个字符：第二行宽度差 4.57px",
	},
	{
		Ledger: "A-014", Layer: parity.LayerL2, Kind: "ink", Subject: "zhihu.com", MaxDelta: 8,
		Note: "同上：折行点差一个字符，整段墨迹的最佳对齐位移差一个字符宽度",
	},
}

// importsEnv 是一次渲染里操作步骤能用到的东西，由 prepare 装配页面时填入：对照里防抖定时器是空操作，
// 需要「搜索词停了 150 ms 之后的那次读取」的步骤自己调 store.Fetch。每次渲染各有一份（见 parity.Scene.Prepare）。
type importsEnv struct {
	store *importsscreen.Store
	ctx   context.Context
	fix   parity.ImportsFixture
	// compact 表示视口处于紧凑布局（≤ 1179.98px）：二级侧栏默认收起，要先点顶栏的「展开」才点得到里面的按钮。
	compact bool
}

// importsSteps 是采集场景的操作步骤；nil 表示不操作。
type importsSteps func(s *headless.Session, e *importsEnv) error

// importsScene 是采集队列整窗（顶栏 + rail + 二级侧栏 + 任务列表）的场景：夹具与「现在」与 React 采集器共用。
//
// 页面与步骤由 Prepare 一起装配：同一个场景值会被并行渲染多次（单元测试每个样本一次），Store 与步骤用的环境必须每次渲染各一份，
// 放在场景值上共享会让一次渲染的步骤去操作另一次渲染的 Store。
func importsScene(id string, opt importsOptions, steps importsSteps, waivers ...parity.Waiver) parity.Scene {
	waivers = append(append([]parity.Waiver{}, letterSpacingWaivers...), waivers...)
	prepare := func(look *kit.Look) (func(c ui.C), func(s *headless.Session) error) {
		fix, err := parity.LoadImportsFixture()
		if err != nil {
			panic(err) // 夹具是编译进来的常量，解码失败是程序错误
		}
		ctx := context.Background()
		store := importsscreen.NewStore(newFakeImports(fix, opt), kit.Guard{}, nil)
		env := &importsEnv{store: store, ctx: ctx, fix: fix, compact: look.Viewport <= compactMaxWidth}
		// 对照要确定：不起真实定时器，后台动作同步执行。
		store.After = func(time.Duration, func()) func() { return func() {} }
		store.Spawn = func(fn func()) { fn() }
		if opt.mode != "loading" {
			store.Fetch(ctx) // 读取中场景保持「还没读到」：React 一侧的列表请求永不返回
		}
		screen := &importsscreen.Screen{
			Look: look, Store: store, Ctx: ctx,
			// 空队列引导里的快捷键提示：默认设置里的快速采集组合键。
			Hooks: importsscreen.Hooks{CaptureShortcut: func() string { return "Alt+Shift+N" }},
			Now:   func() time.Time { return time.UnixMilli(fix.Now) },
			// Asia/Shanghai 自 1991 年起没有夏令时，固定偏移 +8 即可，不依赖系统是否带时区库。
			Loc:    time.FixedZone("Asia/Shanghai", 8*3600),
			Locale: kit.ParseDateLocale(fix.Locale),
		}
		w := &shellscreen.Window{
			Look: look, Page: shellscreen.PageHome, Module: shellscreen.ModuleImports,
			OwnControls: true, SearchShortcut: "Alt+Shift+F",
			Modules: map[shellscreen.Module]shellscreen.ModuleView{
				shellscreen.ModuleImports: {Panel: screen.Panel, Workspace: screen.Workspace},
			},
		}
		view := func(c ui.C) {
			w.ActiveImports = store.View().ActiveCount
			w.View(c)
		}
		var run func(s *headless.Session) error
		if steps != nil {
			run = func(s *headless.Session) error { return steps(s, env) }
		}
		return view, run
	}
	return parity.Scene{
		ID: id,
		Build: func(look *kit.Look) func(c ui.C) {
			view, _ := prepare(look)
			return view
		},
		Prepare: prepare,
		Waivers: append(append([]parity.Waiver{}, pageSettingsWaivers...), waivers...),
	}
}

// waitLoadingDelay 让「读取中」的转圈走过 250 ms 的延迟与淡入（React 一侧等了 600 ms 才取图）。
func waitLoadingDelay(s *headless.Session, _ *importsEnv) error {
	return s.Advance(600 * time.Millisecond)
}

// 各场景的操作都以「把指针停在窗口左上角（没有悬停）」或「指针停在被悬停的那一行」收尾，与 React 采集器一一对应。

// blurX、blurY 是 rail 中段的空白处：点它让搜索框失焦（rail 只在顶部与底部有按钮）。
const blurX, blurY = 24, 400

// searchNoMatch 对应渲染层采集器的 searchImports：搜索框里填没有匹配的词，列表按它筛选，输入框失焦。
func searchNoMatch(s *headless.Session, e *importsEnv) error {
	if err := parity.ClickRole(s, gfx.RoleTextField, "搜索标题或链接"); err != nil {
		return err
	}
	if err := s.Type(e.fix.NoMatchQuery); err != nil {
		return err
	}
	if err := s.Rest(); err != nil { // 这一帧把输入框里的词推给 Store
		return err
	}
	e.store.Fetch(e.ctx) // 搜索词停了 150 ms 之后的那次读取
	if err := s.Rest(); err != nil {
		return err
	}
	if err := s.Click(blurX, blurY); err != nil {
		return err
	}
	return parity.ParkAndBlur(s)
}

// selectRows 对应渲染层采集器的 selectImportRows：依次点第 indexes 行的选择钮（行首的类型图标），再把指针移开并清掉焦点
// （点过的选择钮持有焦点，焦点在行内时动作条会浮出；渲染层采集前同样 blur）。
func selectRows(indexes ...int) importsSteps {
	return func(s *headless.Session, _ *importsEnv) error {
		for _, i := range indexes {
			if err := parity.ClickRoleNth(s, gfx.RoleButton, "选择", i); err != nil {
				return err
			}
			if err := s.Rest(); err != nil { // 第一次选择后批量条出现，列表整体下移，后面的点按用新的位置
				return err
			}
		}
		return parity.ParkAndBlur(s)
	}
}

// compactMaxWidth 是紧凑布局的视口宽度上限（渲染层 useCompactLayout：COMPACT_LAYOUT_MAX_WIDTH − 0.02）。
const compactMaxWidth float32 = 1180 - 0.02

// openCompactSidebar 对应渲染层采集器的 openCompactSidebar：紧凑布局下先点顶栏的「展开」让二级侧栏以浮层打开，
// 之后再点里面的按钮（浮层一直留着，筛选之后也不自动收起）。宽屏什么也不做。
func openCompactSidebar(s *headless.Session, e *importsEnv) error {
	if !e.compact {
		return nil
	}
	if err := parity.ClickRole(s, gfx.RoleButton, "展开"); err != nil {
		return err
	}
	return s.Rest()
}

// clickSidebarButton 对应渲染层采集器的 clickImportsButton（不带 group 时）：点二级侧栏里的按钮（状态筛选），紧凑布局先展开侧栏浮层。
// 来源筛选在工作区工具条里，不在侧栏，用 clickButton；浮层打开时还会盖住工作区。
func clickSidebarButton(name string) importsSteps {
	click := clickButton(name)
	return func(s *headless.Session, e *importsEnv) error {
		if err := openCompactSidebar(s, e); err != nil {
			return err
		}
		return click(s, e)
	}
}

// clickButton 对应渲染层采集器里 getByRole("button", { name }).click() 再把指针移开。
func clickButton(name string) importsSteps {
	return func(s *headless.Session, _ *importsEnv) error {
		if err := parity.ClickRole(s, gfx.RoleButton, name); err != nil {
			return err
		}
		if err := s.Rest(); err != nil {
			return err
		}
		return parity.ParkAndBlur(s)
	}
}

// scrollListToEnd 把任务列表滚到底（超出内容就停在末端）：指针放在列表中部。
func scrollListToEnd(s *headless.Session, _ *importsEnv) error {
	if err := parity.ScrollDown(s, 700, 500, 20000); err != nil {
		return err
	}
	if err := s.Rest(); err != nil {
		return err
	}
	return parity.ParkAndBlur(s)
}

// hoverRow 对应渲染层采集器的 hoverImportsRow：把指针停在标题为 title 的那一行的左内边距处（距行左上角 6、20），
// 行进入悬停态但指针不落在任何带提示气泡的元素上（气泡是台账 B-011 的另一个场景）。
func hoverRow(title string) importsSteps {
	return func(s *headless.Session, _ *importsEnv) error {
		if err := parity.MoveInRow(s, gfx.RoleButton, title, 6, 20); err != nil {
			return err
		}
		return s.Rest()
	}
}

// hoverButtonPrefix 对应渲染层采集器的 hoverImportsButton：把指针停在可访问名以 prefix 开头的第一个控件中心，等颜色过渡走完。
func hoverButtonPrefix(role gfx.Role, prefix string) importsSteps {
	return func(s *headless.Session, _ *importsEnv) error {
		if err := parity.MoveOverRolePrefix(s, role, prefix); err != nil {
			return err
		}
		return s.Rest()
	}
}

// hoverTooltip 对应渲染层采集器的 hoverTooltip：把指针停在宿主中心，等 320 ms 的延迟过去、气泡出现并走完入场过渡。
func hoverTooltip(move func(*headless.Session, *importsEnv) error) importsSteps {
	return func(s *headless.Session, e *importsEnv) error {
		if err := move(s, e); err != nil {
			return err
		}
		if err := s.Advance(kit.TooltipDelay + 80*time.Millisecond); err != nil {
			return err
		}
		return s.Rest()
	}
}

func init() {
	registry["app.window.imports"] = importsScene("app.window.imports", importsOptions{mode: "data"}, nil,
		append(tinyIconWaivers, kerningInkWaiver("yt-dlp"))...)
	registry["app.window.imports.loading"] = importsScene("app.window.imports.loading", importsOptions{mode: "loading"}, waitLoadingDelay)
	registry["app.window.imports.error"] = importsScene("app.window.imports.error", importsOptions{mode: "error"}, nil)
	registry["app.window.imports.empty"] = importsScene("app.window.imports.empty", importsOptions{mode: "empty"}, nil, kerningWaivers...)
	// 数据场景的状态：筛选无匹配、选中两行、按状态筛选、按来源筛选、队列暂停、还有更早的任务、行悬停、清理确认。
	data := importsOptions{mode: "data"}
	dataWaivers := append(append([]parity.Waiver{}, tinyIconWaivers...), kerningInkWaiver("yt-dlp"))
	registry["app.window.imports.filtered-empty"] = importsScene("app.window.imports.filtered-empty", data, searchNoMatch)
	registry["app.window.imports.selection"] = importsScene("app.window.imports.selection", data, selectRows(1, 3), dataWaivers...)
	registry["app.window.imports.filter-failed"] = importsScene("app.window.imports.filter-failed", data, clickSidebarButton("失败 3"), dataWaivers...)
	registry["app.window.imports.origin-mobile"] = importsScene("app.window.imports.origin-mobile", data, clickButton("手机端"), dataWaivers...)
	registry["app.window.imports.paused"] = importsScene("app.window.imports.paused", importsOptions{mode: "data", paused: true}, nil, dataWaivers...)
	registry["app.window.imports.load-more"] = importsScene("app.window.imports.load-more",
		importsOptions{mode: "data", hasMore: true}, scrollListToEnd, dataWaivers...)
	registry["app.window.imports.row-hover"] = importsScene("app.window.imports.row-hover", data, hoverRow("本地优先软件的七个理想"), dataWaivers...)
	// 悬停提示（台账 B-011）：宿主取自采集队列页——顶栏图标钮（单行短提示）、筛选到失败后第一条的任务标题（折成两行）、
	// 最靠下的任务标题（翻到上方）。气泡是 fixed 的浮层，文字的灰度抗锯齿偏差（A-009）对它成立，所以这些场景一律带上 overlayInkWaiver。
	tipWaivers := withOverlayInk(dataWaivers)
	registry["ui.tooltip.below"] = importsScene("ui.tooltip.below", data,
		hoverTooltip(func(s *headless.Session, _ *importsEnv) error { return parity.MoveOverHint(s, "收起", "展开") }), tipWaivers...)
	// 长提示折行：筛选到失败后第一条（知乎那条）的标题，原始地址 57 个字符，折成两行，气泡在标题下方。
	// 行上的时间列也有长提示，但行一悬停动作钮就把它整个盖住，鼠标几乎碰不到，不拿它当宿主（见 react-scenes.mjs）。
	longTipWaivers := append(append([]parity.Waiver{}, tipWaivers...), tooltipBreakWaivers...)
	registry["ui.tooltip.wrap"] = importsScene("ui.tooltip.wrap", data,
		hoverTooltip(func(s *headless.Session, e *importsEnv) error {
			if err := clickSidebarButton("失败 3")(s, e); err != nil {
				return err
			}
			return parity.MoveOverHint(s, e.fix.SourceInputOf("task-failed"))
		}), longTipWaivers...)
	registry["ui.tooltip.flip"] = importsScene("ui.tooltip.flip", data,
		hoverTooltip(func(s *headless.Session, e *importsEnv) error {
			return parity.MoveOverLowestHint(s, e.fix.NamedSourceInputs())
		}),
		longTipWaivers...)
	// 悬停下划线（台账 B-012）：可打开成果的任务标题（同时出标题的提示气泡，所以带 overlayInkWaiver）与行上的耗时摘要（没有提示）。
	registry["app.window.imports.title-hover"] = importsScene("app.window.imports.title-hover", data,
		hoverTooltip(func(s *headless.Session, e *importsEnv) error {
			return parity.MoveOverHint(s, e.fix.SourceInputOf("task-completed"))
		}), tipWaivers...)
	registry["app.window.imports.summary-hover"] = importsScene("app.window.imports.summary-hover", data,
		hoverButtonPrefix(gfx.RoleButton, "共 "), dataWaivers...)
	// 「清理当前筛选」的确认框：模态，遮罩背后的页面模糊（台账 A-007），浮层文字的墨迹总量偏大（A-009）。
	registry["app.window.imports.clear-confirm"] = withBackdrop(
		importsScene("app.window.imports.clear-confirm", data, clickButton("清理当前筛选"), withOverlayInk(dataWaivers)...),
		confirmBackdrop("清理采集任务历史"))
}
