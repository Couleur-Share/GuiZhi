package scenes

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/parity"
	libraryscreen "guizhi-mygo-shell/viva/screens/library"
	shellscreen "guizhi-mygo-shell/viva/screens/shell"
)

// 知识库侧栏（M2-1）的整窗场景：rail + 顶栏 + 侧栏面板，以及面板上弹出的菜单、对话框与提示。
// 工作区在这一批场景里两侧都是空白：React 采集器把侧栏旁边的内容容器隐去（scripts/parity/lib/react-library.mjs），
// Viva 这边登记一块空工作区；列表栏（M2-2）的场景见 library_list.go（list 选项打开）。
// 夹具与 React 采集器共用（viva/parity/testdata/library-fixture.json）。

// libraryOptions 对应 React 采集器 enterLibrary 的选项。
type libraryOptions struct {
	variant    string // data | empty | long | many
	smartViews string // 夹具里的智能视图集名；空表示没有保存过

	// list 为 true 时工作区放真实的列表栏（M2-2）；右侧详情栏仍是空白（M2-4 才迁入，React 一侧同样隐去）。
	list bool
	// fault 是列表读取的故障注入（只在 list 时有意义）。
	fault listFault
	// view 是列表的视图模式偏好（card | list）；空表示没有保存过（缺省卡片）。
	view string
	// batchFail 登记批量里会失败的条目（id → 失败原因）；与 React 采集器的 config.batch.fail 对应。
	batchFail map[string]string
	// holdBatch 为 true 时，场景里的 listHoldBatches 之后的第一次批量卡在服务里不返回（批量进行中的画面，如重试失败项）：
	// 与 React 采集器的 config.batch.holdAfter 对应。卡住的批量要到画面采集完才放走，收尾的取消延后（同 faultHold）。
	holdBatch bool
}

// listFault 是列表读取的故障：与 React 采集器里 config.list.fault 一一对应。
type listFault string

const (
	faultNone listFault = ""
	// faultHold：列表请求永不返回（读取中画面）。
	faultHold listFault = "hold"
	// faultFail：列表请求失败（错误态画面）。
	faultFail listFault = "fail"
)

// listFailMessage 是故障注入时的失败原因，两侧同文。
const listFailMessage = "磁盘读取失败：知识库文件暂时无法访问"

// holdRelease 是读取中画面里，卡住的读取被放走（取消上下文）的延时：比一次渲染从收尾到采集完成长得多即可。
const holdRelease = 20 * time.Second

// libraryEnv 是一次渲染里操作步骤能用到的东西，由 prepare 装配页面时填入；每次渲染各一份（见 libraryEnvScene）。
type libraryEnv struct {
	store  *libraryscreen.Store
	screen *libraryscreen.Screen
	ctx    context.Context
	svc    *servicestest.Library
}

// parkMode 是场景收尾时对指针与焦点的处理，与 React 采集器一一对应。
type parkMode int

const (
	// parkAll：指针移开并清焦点——采集器的默认（行为与 parkPointer 一致）。
	parkAll parkMode = iota
	// parkKeepFocus：指针移开，焦点留着——React 场景的 keepFocus（菜单第一项、对话框里的名称框自动聚焦，那是用户看到的自然状态）。
	parkKeepFocus
	// parkNone：指针与焦点都留着——React 场景的 keepPointer（行悬停）。
	parkNone
)

// libraryStep 是场景的操作步骤；每一步结束时画面都已落定。look 是这次渲染的外观（需要按目标滚动时用，见 libraryReveal）。
type libraryStep func(s *headless.Session, look *kit.Look) error

// libraryScene 是知识库侧栏整窗的场景（步骤不需要碰 Store）。
func libraryScene(id string, opt libraryOptions, steps libraryStep, mode parkMode, waivers ...parity.Waiver) parity.Scene {
	var env libraryEnvStep
	if steps != nil {
		env = func(s *headless.Session, look *kit.Look, _ *libraryEnv) error { return steps(s, look) }
	}
	return libraryEnvScene(id, opt, env, mode, waivers...)
}

// libraryEnvStep 是要用到 libraryEnv（直接驱动 Store，例如搜索词防抖到点之后的那次读取）的步骤。
type libraryEnvStep func(s *headless.Session, look *kit.Look, e *libraryEnv) error

// libraryEnvScene 是知识库整窗的场景。页面与步骤由 Prepare 一起装配：同一个场景值会被并行渲染多次，
// Store 与偏好必须每次渲染各一份（见 importsScene）。
func libraryEnvScene(id string, opt libraryOptions, steps libraryEnvStep, mode parkMode, waivers ...parity.Waiver) parity.Scene {
	prepare := func(look *kit.Look) (func(c ui.C), func(s *headless.Session) error) {
		fix, err := parity.LoadLibraryFixture()
		if err != nil {
			panic(err) // 夹具是编译进来的常量，解码失败是程序错误
		}
		data := fix.Variant(opt.variant)
		svc := (&servicestest.Library{}).Seed(data.Counts, data.Collections, data.Tags)
		prefs := &servicestest.Prefs{}
		if opt.smartViews != "" {
			raw, ok := fix.SmartViews[opt.smartViews]
			if !ok {
				panic("知识库夹具里没有智能视图集 " + opt.smartViews)
			}
			prefs.Put(settings.PrefSmartViews, string(raw))
		}
		if opt.view != "" {
			prefs.Put(settings.PrefLibraryViewMode, strconv.Quote(opt.view))
		}
		notifier := kit.NewNotifier(nil)
		guard := kit.Guard{Notify: notifier}
		store := libraryscreen.NewStore(svc, guard, nil)
		ctx, cancel := context.WithCancel(context.Background())
		// 对照要确定：不起真实定时器，后台动作同步执行。
		store.After = func(time.Duration, func()) func() { return func() {} }
		store.Spawn = func(fn func()) { fn() }
		if opt.list {
			svc.SeedEntries(data.Entries)
			for id, reason := range opt.batchFail {
				svc.FailBatchItem(id, reason)
			}
			switch opt.fault {
			case faultFail:
				svc.FailOn("List", errors.New(listFailMessage))
			case faultHold:
				// 请求卡在服务里永不返回：读取要放到后台，否则整个会话被它卡住；场景收尾时取消上下文放它走。
				svc.HoldList()
				store.Spawn = func(fn func()) { go fn() }
			}
		}
		next := 0
		screen := &libraryscreen.Screen{
			Look: look, Store: store, Ctx: ctx, Guard: guard, Prefs: prefs,
			Now:    func() time.Time { return time.UnixMilli(fix.Now) },
			Loc:    time.FixedZone(fix.Timezone, 8*3600),
			Locale: kit.ParseDateLocale(fix.Locale),
			NewID:  func() string { next++; return fmt.Sprintf("parity-view-%d", next) },
		}
		workspace := blankWorkspace
		if opt.list {
			workspace = screen.Workspace
		}
		w := &shellscreen.Window{
			Look: look, Page: shellscreen.PageHome, Module: shellscreen.ModuleLibrary,
			OwnControls: true, SearchShortcut: "Alt+Shift+F", Notifier: notifier,
			Modules: map[shellscreen.Module]shellscreen.ModuleView{
				shellscreen.ModuleLibrary: {Panel: screen.Panel, Workspace: workspace},
			},
		}
		// 窗口级键盘（方向键、Home、End、空格，B-045）与生产同一处接线：经 Look.KeyHook，焦点从此刻的画布读。
		var canvas *gfx.Canvas
		look.KeyHook = func(e gfx.KeyEvent) gfx.EventResult {
			if canvas == nil {
				return gfx.EventResult{}
			}
			return screen.ListKey(e, canvas)
		}
		view := func(c ui.C) {
			canvas = c.Canvas()
			w.View(c)
			screen.EndFrame()
		}
		compact := look.Viewport <= compactMaxWidth
		env := &libraryEnv{store: store, screen: screen, ctx: ctx, svc: svc}
		run := func(s *headless.Session) error {
			if opt.fault == faultHold || opt.holdBatch {
				// 卡住的读取（或批量）要到画面采集完才放走：这里一收尾就取消的话，读取带着「context canceled」回来，
				// 读取中的画面被错误态顶掉（采集在 run 返回之后）。采集在毫秒内完成，放走的时间留得宽裕。
				time.AfterFunc(holdRelease, cancel)
			} else {
				defer cancel()
			}
			// 侧栏场景比的就是侧栏，紧凑布局下先把浮层展开；列表场景比的是工作区里的列表栏，浮层会盖住它，
			// 需要点侧栏行的场景自己开关（listOpenSidebar、listCloseSidebar；React 一侧同理，见 enterLibrary）。
			if !opt.list {
				if err := openLibraryCompactSidebar(s, compact); err != nil {
					return err
				}
			}
			if steps != nil {
				if err := steps(s, look, env); err != nil {
					return err
				}
			}
			switch mode {
			case parkAll:
				return parity.ParkAndBlur(s)
			case parkKeepFocus:
				return parity.ParkPointer(s)
			}
			return nil
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
		Waivers: append(append(append([]parity.Waiver{}, pageSettingsWaivers...), libraryBaseWaivers()...), waivers...),
	}
}

// libraryBaseWaivers 是侧栏各场景共有的已登记偏差：分区标题与图标选择器分组标题的字距（A-013）、
// 平台行「YouTube」的字偶距（A-014）、彩色表情字形的 alpha（A-023）、省略号前的空格（A-025）、
// 小字标品牌图标的墨迹（A-003）、行尾计数「1」的笔画偏淡（A-016）。
func libraryBaseWaivers() []parity.Waiver {
	var out []parity.Waiver
	for _, title := range []string{
		"智能视图", "合集", "平台", "标签", // 侧栏分区标题
		"学习与研究", "工作与项目", "技术与开发", "创作与设计", "生活与健康", "财务与商务", "旅行与自然", "标记与收纳", // 图标选择器分组标题
	} {
		out = append(out, headingTrackingWaivers(title)...)
	}
	for _, title := range englishSidebarHeadings {
		out = append(out, headingLatinTrackingWaivers(title)...)
	}
	out = append(out, youtubeKerningWaivers...)
	out = append(out, emojiWaivers...)
	out = append(out, ellipsisSpaceWaivers...)
	out = append(out, smallWordmarkIconWaivers...)
	// 行尾计数是 text-[10px]、文字色 40% 不透明的数字：「1」只有一道细竖笔，大字号档里 Viva 的墨迹总量比 0.842（阈值下限 0.85），
	// 与 A-016（小字号档的 `%`、`px`）同一类现象的延伸；位置、宽度、颜色、字号一致，其余数字（「6」「11」……）都在范围内。
	return append(out, smallLatinInkWaiver("「1」"))
}

// headingTrackingWaivers 登记 A-013 在小标题上的延伸：侧栏分区标题与图标选择器的分组标题是 text-xs font-semibold tracking-wider（0.05em），
// Viva 的文字没有字距，绘制宽度窄 字符数 × 0.05em（小、中、大字号档的 text-xs 是 10.5、12、13.5px，即每字 0.525、0.6、0.675px）；
// 文字盒宽已按字数补齐（kit.TextTracked），后面的元素位置一致。宽度差框在 [字符数×0.525−0.3, 字符数×0.675+0.3]，
// 墨迹位移的上限是宽度差的一半再加 1 像素（相关峰的取整）；范围之外的偏差照常判定。
func headingTrackingWaivers(title string) []parity.Waiver {
	return trackingWaivers(title, 0, "",
		"tracking-wider 的字距：Viva 没有字距，绘制宽度窄 字符数 × 0.05em（盒宽已补齐，后面的元素位置一致）")
}

// latinHeadingKerningCap 是英文大写标题在字距（A-013）之外再多出的字偶距宽度差的上限（A-013 的英文延伸，字偶距部分与 A-026 同类）：
// Viva 对 600 字重做字偶距、Chromium 不做，大写拉丁标题里 TA、AR、AT 这类字偶对使 Viva 的整行更窄。
// 中档字号档（text-xs = 12px）下四个标题在字距之外实测多出 1.02、0.27、1.07、1.44px（SMART VIEWS、COLLECTIONS、PLATFORMS、TAGS），
// 上限取 2px（向外取整）。**只在中档字号档的变体里放过**：小、大字号档没有实测（英文变体只做中档，台账 G-017）。
const latinHeadingKerningCap = 2.0

// englishSidebarHeadings 是侧栏分区标题在英文界面下画出来的字（`strings.ToUpper` 之后）；
// 与 locales 目录的英文一致由 library_waivers_test.go 核对，目录改了这里要跟着改。
var englishSidebarHeadings = []string{"SMART VIEWS", "COLLECTIONS", "PLATFORMS", "TAGS"}

// headingLatinTrackingWaivers 登记 A-013 在英文大写标题上的延伸：字距之外再加字偶距（latinHeadingKerningCap），只限中档字号档。
func headingLatinTrackingWaivers(title string) []parity.Waiver {
	return trackingWaivers(title, latinHeadingKerningCap, "medium",
		"tracking-wider 的字距（Viva 没有字距）加字偶距（Viva 对 600 字重做、Chromium 不做，A-026 同类）：英文大写标题的绘制宽度窄 字符数 × 0.05em 再多 ≤ 2px（盒宽已补齐，后面的元素位置一致）")
}

// trackingWaivers 是分区标题字距登记的共同写法：kerningCap 是字距之外允许多出的宽度差，variant 非空时只在该字号档放过。
func trackingWaivers(title string, kerningCap float64, variant, note string) []parity.Waiver {
	n := float64(utf8.RuneCountInString(title))
	subject := "「" + title + "」"
	return []parity.Waiver{
		{
			Ledger: "A-013", Layer: parity.LayerL2, Kind: "text-width", Subject: subject, Variant: variant,
			MinDelta: max(n*0.525-0.3, 0.01), MaxDelta: n*0.675 + 0.3 + kerningCap,
			Note: note,
		},
		{
			Ledger: "A-013", Layer: parity.LayerL2, Kind: "ink", Subject: subject, Variant: variant, MaxDelta: n*0.675/2 + 1,
			Note: "同上：字距使墨迹整体位移最多宽度差的一半（只管横向，纵向错位照常判定）",
		},
	}
}

// youtubeKerningWaivers 登记 A-014 在平台行「YouTube」上的延伸：Yo、Tu 字偶对共 −0.1886em
// （text-sm，小、中、大字号档 12.25、14、15.75px 时是 2.31、2.64、2.97px），Viva 不做字偶距，整行宽出这个数。
var youtubeKerningWaivers = []parity.Waiver{
	{
		Ledger: "A-014", Layer: parity.LayerL2, Kind: "text-width", Subject: "「YouTube」", MaxDelta: 3,
		Note: "字偶距：Yo、Tu 字偶对共 −0.1886em（14px 时 −2.64px），Viva 不做字偶距，整行宽出这个数",
	},
	{
		Ledger: "A-014", Layer: parity.LayerL2, Kind: "ink", Subject: "「YouTube」", MaxDelta: 3,
		Note: "同上：整行宽度差累积成墨迹剖面的最佳对齐位移（小、中字号档 2 像素，大字号档宽度差 2.97px、位移 3 像素，T2 实测；只管横向）",
	},
}

// emojiWaivers 登记表情字形的偏差（只限单个彩色表情的文字，见 parity.Waiver.Emoji）。
//
// A-023：渲染层的彩色字形按文字色 alpha 整体合成（侧栏行上的图标 text-sidebar-foreground/70 → 表情 70% 不透明），
// Viva 的彩色字形不理会文字色 alpha，总是满不透明，墨迹总量比渲染层偏多 1/0.7 − 1 ≈ 0.43（逐字形实测 0.41–0.65，随表情的颜色与底色而异）。
// 只放过「Viva 偏多」的方向、偏差落在 [0.40, 0.65] 的表情墨迹总量比（T1 与 T2 实测范围向外取整到 0.01）；位置、字形大小照常判定。
//
// 字形的前进宽度两边一致（Viva 引擎 1.3730em，Chromium 1.3736em），起点由 library.emojiGlyph 按 CSS 居中溢出的规则对齐，
// 所以位置与宽度不需要登记。墨迹的纵向位置也不登记：之前的「🌊 垂直 −1」不是偏差，是 Viva 的文字标签把超出行盒的字形底边裁掉了，
// 已在 library.emojiGlyph 里把标签放高解决（emojiOverflow），任何纵向错位现在都会照常判定。
var emojiWaivers = []parity.Waiver{
	{
		Ledger: "A-023", Layer: parity.LayerL2, Kind: "ink-mass", Emoji: true, Dir: 1, MinDelta: 0.40, MaxDelta: 0.65,
		Note: "彩色字形不受文字色 alpha 影响：侧栏行上的表情在渲染层是 70% 不透明，Viva 满不透明，墨迹总量偏多约 1/0.7",
	},
}

// smallWordmarkIconWaivers 登记 A-003 在小字标图标上的延伸：「小红书」的品牌图标是一行 3px 高的小字（一团极细的填充形状），
// 渲染层（Skia）的采样量化把这些细笔画画得偏淡，Viva 按精确面积覆盖，墨迹总量比 1.357（其余 16px 图标 0.90–1.05）；
// 「哔哩哔哩」的图标也是细笔画为主，小、大字号档（h-4 w-4 即 14、18px）超阈值，中档（16px）不超。
// 外框与位置一致；图标的主题名只有外框、没有身份，所以按尺寸与「Viva 偏多」加各自实测的窄区间框定，更大的偏差照常判定。
// 区间取 T2（明暗 × 6 色相 × 3 字号档的 36 个外观变体、25 个场景）实测偏差的最小与最大值，向外取整到 0.01；
// 阈值本身是 0.15，所以哔哩哔哩 18px 的下限就是阈值。
var smallWordmarkIconWaivers = wordmarkIconWaivers()

func wordmarkIconWaivers() []parity.Waiver {
	rows := []struct {
		size, name string  // 图标边长（逻辑像素，照报告里 fmtBox 的两位小数写法）与图标
		lo, hi     float64 // 墨迹总量比 −1 的实测范围（Viva 偏多）
	}{
		{"16.00", "小红书", 0.32, 0.36},
		{"14.00", "小红书", 0.33, 0.37},
		{"14.00", "哔哩哔哩", 0.18, 0.20},
		{"18.00", "小红书", 0.35, 0.39},
		{"18.00", "哔哩哔哩", 0.15, 0.21},
		// 知识库表格的来源列用 h-3.5 w-3.5（0.875rem）的图标：小、大字号档是 12.25、15.75px（中档 14px 就是上面的 14）。
		// T2 的 13 个表格场景实测：12.25px 的 96 处在 0.3841–0.3856，15.75px 的 84 处在 0.3784–0.3789（B-046）。
		{"12.25", "小红书", 0.38, 0.39},
		{"15.75", "小红书", 0.37, 0.38},
	}
	out := make([]parity.Waiver, 0, len(rows))
	for _, r := range rows {
		out = append(out, parity.Waiver{
			Ledger: "A-003", Layer: parity.LayerL2, Kind: "ink-mass", Subject: " " + r.size + " " + r.size + "]",
			Dir: 1, MinDelta: r.lo, MaxDelta: r.hi,
			Note: strings.TrimSuffix(r.size, ".00") + "px 细笔画品牌图标（" + r.name + "）：细笔画在 Skia 里偏淡，Viva 按精确面积覆盖，墨迹总量偏多",
		})
	}
	return out
}

// ellipsisSpaceWaivers 登记 A-025（由 C-006 经用户 2026-11-19 裁决接受后转入）：被省略号截断的文字，截断点前若是空格，Chromium 把空格留着（「name …」），
// Viva 的省略号逻辑去掉前缀右侧的空格（「name…」，gfx/wrap.go 的 trimSpaceRight，没有选项），省略号左移一个空格宽
// （text-sm 的小、中、大字号档 12.25、14、15.75px：空格宽 3.4、3.9、4.4px；对照里再加上省略号字形量法的零点几像素）。
// 只放过「渲染层的文字以空格加省略号收尾」的文字宽度；其余照常判定。
var ellipsisSpaceWaivers = []parity.Waiver{{
	Ledger: "A-025", Layer: parity.LayerL2, Kind: "text-width", Subject: " …」", MinDelta: 3, MaxDelta: 5.6,
	Note: "省略号前的空格：Chromium 留着，Viva 去掉，省略号左移一个空格宽（用户 2026-11-19 裁决接受，A-025）",
}}

// blankWorkspace 是还没做的知识库工作区：一块空白，只占位。
func blankWorkspace(c ui.C) {
	ui.Column(c.Key("library-workspace")).Fill().Children(func() {})
}

// openLibraryCompactSidebar 对应 React 采集器的 openCompactSidebar：紧凑布局下先点顶栏的「展开」让侧栏以浮层打开。宽屏什么也不做。
func openLibraryCompactSidebar(s *headless.Session, compact bool) error {
	if !compact {
		return nil
	}
	if err := parity.ClickRole(s, gfx.RoleButton, "展开"); err != nil {
		return err
	}
	return s.Rest()
}

// libraryModalBackdrop 是知识库对话框的遮罩：与 modalBackdrop 相同，只是「对话框打开之前」的页面要带上紧凑布局下已展开的侧栏浮层
// （对照 Resolve 渲染那一页时不跑场景的 Prepare，所以要在 BaseSteps 里补上这一步，否则补回被模态拿出可访问树的背景节点时，
// 侧栏的行、收起钮与宽度分隔条都在「打开之前」的页面里缺席，模糊背景的预测图里也没有浮层）。
// 宽屏没有「展开」按钮（侧栏本来就展开），这一步什么也不做。
//
// reveal 是打开对话框之前会被滚进视野的那一行文字（从菜单里点「重命名」的行：渲染层的 click 先把它滚进视野，
// 对话框打开时遮罩背后的侧栏就停在那个滚动位置），空表示打开之前页面没有滚动。
func libraryModalBackdrop(title, reveal string) *parity.BackdropSpec {
	b := modalBackdrop(title)
	b.BaseSteps = func(s *headless.Session, look *kit.Look) error {
		if _, collapsed := s.AccessByRole(gfx.RoleButton, "展开"); collapsed {
			if err := openLibraryCompactSidebar(s, true); err != nil {
				return err
			}
		}
		if reveal == "" {
			return nil
		}
		return libraryReveal(s, look, reveal)
	}
	return b
}

// libraryButton 是可访问名为 name 的按钮的外框。
func libraryButton(s *headless.Session, name string) (gfx.Rect, error) {
	n, ok := s.AccessByRole(gfx.RoleButton, name)
	if !ok {
		return gfx.Rect{}, fmt.Errorf("找不到按钮「%s」", name)
	}
	return n.Bounds, nil
}

// clickRolePrefix 点击第一个角色匹配、可访问名以 prefix 开头的控件的中心：对应 getByRole(role, { name: /^prefix/ }).first().click()。
func clickRolePrefix(s *headless.Session, role gfx.Role, prefix string) error {
	for _, n := range s.Access() {
		if n.Role == role && strings.HasPrefix(n.Label, prefix) {
			return s.Click(n.Bounds.X+n.Bounds.Width/2, n.Bounds.Y+n.Bounds.Height/2)
		}
	}
	return fmt.Errorf("找不到可访问名以「%s」开头的 %s", prefix, role)
}

// libraryPlain 把不需要外观的步骤（scenes.go 里各家通用的）接成 libraryStep。
func libraryPlain(step func(s *headless.Session) error) libraryStep {
	return func(s *headless.Session, _ *kit.Look) error { return step(s) }
}

// libraryReveal 对应 Playwright 的 click 之前自动做的 scrollIntoViewIfNeeded：目标在侧栏（或图标选择器）的滚动区里
// 没有完全露出时，把它滚进视野（Chromium 的 CenterIfNeeded：完全在外居中，露出一部分就近靠边，完全可见不动）。
// Viva 的可访问树里没有滚出视野的控件，所以按目标的文字行找它（Snapshot.Texts 是裁剪之前的所有文字，滚出视野的行也在），
// 再让 kit 把那一行所在的停靠点滚进来（Look.RevealNear）。anchor 是目标行里的文字；找不到文字、或文字不在任何滚动区的停靠点里
// （页签、顶栏按钮）时什么也不做，按名字找控件失败由后面的点击报错。
func libraryReveal(s *headless.Session, look *kit.Look, anchor string) error {
	for _, t := range s.Snapshot().Texts {
		if t.Value != anchor {
			continue
		}
		if look.RevealNear(t.Bounds.X+t.Bounds.Width/2, t.Bounds.Y+t.Bounds.Height/2) {
			if err := s.Frame(); err != nil { // 新的滚动位置要出一帧才生效
				return err
			}
			return s.Rest()
		}
	}
	return nil
}

// libraryRevealText 先把某行文字所在的停靠点滚进视野，再做后面的步骤：给「目标没有自己的文字」的控件用
// （分区标题上的图标按钮：文字是同一行的标题）。
func libraryRevealText(anchor string) libraryStep {
	return func(s *headless.Session, look *kit.Look) error { return libraryReveal(s, look, anchor) }
}

// libraryClickButton 点名为 name 的按钮，等画面落定；点之前像 Playwright 那样把它滚进视野。
func libraryClickButton(name string) libraryStep {
	return func(s *headless.Session, look *kit.Look) error {
		if err := libraryReveal(s, look, name); err != nil {
			return err
		}
		if err := parity.ClickRole(s, gfx.RoleButton, name); err != nil {
			return err
		}
		return s.Rest()
	}
}

// libraryClickPrefix 点名字以 prefix 开头的按钮（范围页签的名字带计数角标，分区标题上的开关带数量）。
func libraryClickPrefix(prefix string) libraryStep {
	return func(s *headless.Session, _ *kit.Look) error {
		if err := clickRolePrefix(s, gfx.RoleButton, prefix); err != nil {
			return err
		}
		return s.Rest()
	}
}

// libraryScrollSidebarToEnd 把侧栏滚到底（行很多时回收站钉在最底）。
func libraryScrollSidebarToEnd(s *headless.Session, _ *kit.Look) error {
	if err := parity.ScrollDown(s, 150, 450, 30000); err != nil {
		return err
	}
	return s.Rest()
}

// libraryChain 依次执行几步。
func libraryChain(steps ...libraryStep) libraryStep {
	return func(s *headless.Session, look *kit.Look) error {
		for _, step := range steps {
			if err := step(s, look); err != nil {
				return err
			}
		}
		return nil
	}
}

// libraryHoverRow 对应 React 采集器的 hoverRow：把指针停在行的左内边距处（距行左上角 8、半个行高）。
func libraryHoverRow(label string) libraryStep {
	return func(s *headless.Session, _ *kit.Look) error { // 渲染层只取外框、不滚动，目标本来就在视野里
		b, err := libraryButton(s, label)
		if err != nil {
			return err
		}
		if err := s.Move(b.X+8, b.Y+b.Height/2); err != nil {
			return err
		}
		return s.Rest()
	}
}

// libraryOpenRowMenu 对应 React 采集器的 openRowMenu：点行尾的「更多操作」。
// 行里的「更多操作」按钮与行同高，按垂直中心落在行内认出是哪一行的。
func libraryOpenRowMenu(label string) libraryStep {
	return func(s *headless.Session, look *kit.Look) error {
		if err := libraryReveal(s, look, label); err != nil {
			return err
		}
		row, err := libraryButton(s, label)
		if err != nil {
			return err
		}
		for _, n := range s.Access() {
			cy := n.Bounds.Y + n.Bounds.Height/2
			if n.Role == gfx.RoleButton && n.Label == "更多操作" && cy >= row.Y && cy <= row.Y+row.Height {
				if err := s.Click(n.Bounds.X+n.Bounds.Width/2, cy); err != nil {
					return err
				}
				return s.Rest()
			}
		}
		return fmt.Errorf("行「%s」没有「更多操作」按钮", label)
	}
}

// libraryRowRect 是名为 label 的行的整行外框（渲染层 rowOf：主按钮的 group/row 祖先）。Viva 里行的外框是一个没有名字的命中区
// （行的悬停分组靠它；可访问树里是无名的 button），取左缘与主按钮对齐、垂直方向包住主按钮中心的最矮的那个。
// 不能拿主按钮代替：行比主按钮矮或高的时候（小字号档按钮 28、行下限 32；大字号档被压到 32 的行里按钮仍是 36），
// 两者的上缘并不重合，而渲染层的点击位置是相对整行上缘算的（台账 B-034 的复核里发现的采集器差异，G 类）。
func libraryRowRect(s *headless.Session, label string) (gfx.Rect, error) {
	b, err := libraryButton(s, label)
	if err != nil {
		return gfx.Rect{}, err
	}
	cy := b.Y + b.Height/2
	var best gfx.Rect
	found := false
	for _, h := range s.Snapshot().Hits {
		if h.Block || h.Container || h.Label != "" {
			continue
		}
		r := h.Bounds
		if max(r.X-b.X, b.X-r.X) > 0.5 || r.Width < b.Width-0.5 || cy < r.Y || cy > r.Y+r.Height {
			continue
		}
		if !found || r.Height < best.Height {
			best, found = r, true
		}
	}
	if !found {
		return gfx.Rect{}, fmt.Errorf("找不到行「%s」的整行外框", label)
	}
	return best, nil
}

// libraryRightClickRow 对应 React 采集器的 rightClickRow：在行上距整行左上角 (40, 16) 处点右键。
func libraryRightClickRow(label string) libraryStep {
	return func(s *headless.Session, look *kit.Look) error {
		if err := libraryReveal(s, look, label); err != nil {
			return err
		}
		r, err := libraryRowRect(s, label)
		if err != nil {
			return err
		}
		if err := s.RightClick(r.X+40, r.Y+16); err != nil {
			return err
		}
		return s.Rest()
	}
}

// libraryOpenTagColors 对应 React 采集器的 openTagColors：右键标签行，ArrowDown 到「颜色」，ArrowRight 展开并聚焦第一项。
func libraryOpenTagColors(label string) libraryStep {
	return libraryChain(libraryRightClickRow(label), libraryKeys("down"), libraryKeys("right"))
}

// libraryKeys 按一个键，等画面落定。
func libraryKeys(spec string) libraryStep {
	return func(s *headless.Session, _ *kit.Look) error {
		if err := s.Keys(spec); err != nil {
			return err
		}
		return s.Rest()
	}
}

// libraryClickMenuItem 点菜单项（名字以 prefix 开头：带说明的项可访问名是「标题 说明」）。
func libraryClickMenuItem(prefix string) libraryStep {
	return func(s *headless.Session, _ *kit.Look) error {
		if err := clickRolePrefix(s, gfx.RoleMenuItem, prefix); err != nil {
			return err
		}
		return s.Rest()
	}
}

// libraryType 向有焦点的输入框打字，等画面落定。
func libraryType(text string) libraryStep {
	return func(s *headless.Session, _ *kit.Look) error {
		if err := s.Type(text); err != nil {
			return err
		}
		return s.Rest()
	}
}

func init() {
	data := libraryOptions{variant: "data"}
	// 默认与各种数据形态
	registry["app.window.library"] = libraryScene("app.window.library", data, nil, parkAll)
	registry["app.window.library.empty"] = libraryScene("app.window.library.empty", libraryOptions{variant: "empty"}, nil, parkAll)
	registry["app.window.library.long"] = libraryScene("app.window.library.long",
		libraryOptions{variant: "long", smartViews: "long"}, nil, parkAll)
	registry["app.window.library.many"] = libraryScene("app.window.library.many",
		libraryOptions{variant: "many"}, libraryScrollSidebarToEnd, parkAll)
	// 选中态：叠加筛选、范围页签、回收站、未使用的标签、智能视图
	registry["app.window.library.facets"] = libraryScene("app.window.library.facets", data, libraryChain(
		libraryClickPrefix("收藏"), libraryClickButton("学习笔记"), libraryClickButton("哔哩哔哩"), libraryClickButton("产品设计")), parkAll)
	registry["app.window.library.scope-uncategorized"] = libraryScene("app.window.library.scope-uncategorized", data,
		libraryClickPrefix("未归合集"), parkAll)
	registry["app.window.library.scope-favorites"] = libraryScene("app.window.library.scope-favorites", data,
		libraryClickPrefix("收藏"), parkAll)
	registry["app.window.library.scope-archived"] = libraryScene("app.window.library.scope-archived", data,
		libraryClickPrefix("归档"), parkAll)
	// 回收站钉在侧栏滚动区的最底：React 的 click 自动滚进视野（libraryClickButton 里的 libraryReveal 做同一件事）。
	registry["app.window.library.trash"] = libraryScene("app.window.library.trash", data,
		libraryClickButton("回收站"), parkAll)
	// 眼睛按钮在「标签」分区标题上、没有自己的文字：先按标题把它滚进视野。
	registry["app.window.library.unused-tags"] = libraryScene("app.window.library.unused-tags", data,
		libraryChain(libraryRevealText("标签"), libraryClickPrefix("显示 ")), parkAll)
	registry["app.window.library.smart-views"] = libraryScene("app.window.library.smart-views",
		libraryOptions{variant: "data", smartViews: "basic"}, libraryClickButton("工作库的收藏"), parkAll)
	// 行的悬停与键盘聚焦
	registry["app.window.library.row-hover"] = libraryScene("app.window.library.row-hover", data, libraryHoverRow("学习笔记"), parkNone)
	registry["app.window.library.row-focus"] = libraryScene("app.window.library.row-focus", data,
		libraryPlain(focusByTab(gfx.RoleButton, "学习笔记")), parkKeepFocus)
	// 菜单：打开后第一项自动聚焦，那是用户看到的自然状态
	// 浮层（菜单、对话框、提示）里的文字：渲染层灰度抗锯齿，Viva 整窗 LCD，墨迹总量偏大（A-009）。
	registry["app.window.library.menu-collection"] = libraryScene("app.window.library.menu-collection", data,
		libraryOpenRowMenu("学习笔记"), parkKeepFocus, overlayInkWaiver)
	registry["app.window.library.menu-tag"] = libraryScene("app.window.library.menu-tag", data,
		libraryRightClickRow("产品设计"), parkKeepFocus, overlayInkWaiver)
	registry["app.window.library.menu-tag-colors"] = libraryScene("app.window.library.menu-tag-colors", data,
		libraryOpenTagColors("产品设计"), parkKeepFocus, overlayInkWaiver)
	// 对话框：名称框自动聚焦（带聚焦环），整个过程里焦点都在对话框内，所以一律保留焦点；
	// 模态，遮罩背后的页面模糊（A-007，Modal 的 backdrop-blur-md 是 12），页眉标题带字距（A-013）。
	dialog := func(id, title, reveal string, steps libraryStep) {
		registry[id] = withBackdrop(
			libraryScene(id, data, steps, parkKeepFocus, append(modalTitleWaivers(title), overlayInkWaiver)...),
			libraryModalBackdrop(title, reveal))
	}
	dialog("app.window.library.dlg-collection-new", "新建合集", "", libraryClickButton("新建合集"))
	dialog("app.window.library.dlg-collection-new-filled", "新建合集", "",
		libraryChain(libraryClickButton("新建合集"), libraryType("读书笔记"), libraryClickButton("📖")))
	dialog("app.window.library.dlg-collection-rename", "重命名", "旅行计划",
		libraryChain(libraryOpenRowMenu("旅行计划"), libraryClickMenuItem("重命名")))
	dialog("app.window.library.dlg-collection-rename-custom", "重命名", "自定义图标",
		libraryChain(libraryOpenRowMenu("自定义图标"), libraryClickMenuItem("重命名")))
	dialog("app.window.library.dlg-tag-rename", "重命名", "产品设计",
		libraryChain(libraryOpenRowMenu("产品设计"), libraryClickMenuItem("重命名")))
	dialog("app.window.library.dlg-smart-view", "保存智能视图", "", libraryClickButton("保存当前筛选为智能视图"))
	dialog("app.window.library.dlg-smart-view-filled", "保存智能视图", "",
		libraryChain(libraryClickButton("保存当前筛选为智能视图"), libraryType("工作库的收藏视频")))
	// 提示：撤销条
	registry["app.window.library.toast-collection-deleted"] = libraryScene("app.window.library.toast-collection-deleted", data,
		libraryChain(libraryOpenRowMenu("灵感"), libraryClickMenuItem("删除合集")), parkAll, overlayInkWaiver)
	registry["app.window.library.toast-tag-deleted"] = libraryScene("app.window.library.toast-tag-deleted", data,
		libraryChain(libraryOpenRowMenu("待读"), libraryClickMenuItem("删除标签")), parkAll, overlayInkWaiver)
}
