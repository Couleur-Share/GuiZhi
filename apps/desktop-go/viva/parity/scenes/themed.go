package scenes

import (
	"context"
	"fmt"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/parity"
	themedscreen "guizhi-mygo-shell/viva/screens/themed"
)

// 主题阅读（AI 阅读，M3-5b，台账 B-064）的整窗对照场景：知识库详情切到「AI 阅读」之后的原生部分（生成设置对话框、任务状态卡、阅读页设置菜单里的资料面板）
// 与处理中心的「主题页任务」页。阅读岛里的页面本体由 WebView2 承载、不在无头对照范围内：React 一侧用 config.detail.themed 把岛里的内容隐去，
// Viva 一侧开 themed.Screen.BlankPage，页面位置留一块纯空白。
// 与 React 采集器（scripts/parity/scenes/react-themed-scenes.mjs、lib/react-themed.mjs）一一对应：同一份夹具
// （viva/parity/testdata/themed-fixture.json）、同样的点击顺序。React 一侧在页内替换 window.api.themedReading 的读取接口，
// Viva 一侧用假服务（servicestest.ThemedFake）装配同样的读取结果。

// themedMode 是知识库场景里这篇笔记的主题阅读数据：与 React 采集器 applyThemedPatch 的 config 对应。
type themedMode string

const (
	themedEmpty   themedMode = "empty"   // 没有页面也没有任务：阅读区是空态
	themedRunning themedMode = "running" // 没有页面，最近的任务在进行中
	themedFailed  themedMode = "failed"  // 没有页面，最近的任务失败了
	themedPage    themedMode = "page"    // 有已生成的阅读页（带参考资料）
)

// themedFixture 取夹具；它是编译进来的常量，解码失败是程序错误。
func themedFixture() parity.ThemedFixture {
	f, err := parity.LoadThemedFixture()
	if err != nil {
		panic(err)
	}
	return f
}

// newThemedFake 按模式给假服务放数据：与 React 一侧页内替换的 window.api.themedReading 返回同一组。
func newThemedFake(fix parity.ThemedFixture, mode themedMode) *servicestest.ThemedFake {
	svc := &servicestest.ThemedFake{}
	svc.SetModels(fix.Models.Text, fix.Models.Image)
	svc.SetSearch(fix.Search)
	req := services.ThemedRequest{ItemID: fix.ItemID, SourceKind: fix.SourceKind}
	switch mode {
	case themedRunning:
		svc.PutTask(fix.Running)
	case themedFailed:
		svc.PutTask(fix.Failed)
	case themedPage:
		svc.PutPage(req, fix.Page.ThemedPage)
		svc.SetReferences(req, fix.Page.UsedReferences()...)
	}
	return svc
}

// newThemedScreen 装配一份主题阅读界面：后台动作同步执行（对照要确定），页面位置留空白，链接交给一个什么也不做的外部打开钩子（链接才会画成链接）。
func newThemedScreen(look *kit.Look, ctx context.Context, guard kit.Guard, svc services.ThemedReading) *themedscreen.Screen {
	s := themedscreen.New(svc, guard, nil)
	s.Look = look
	s.SetContext(ctx)
	s.SetSpawn(func(fn func()) { fn() })
	s.BlankPage = true
	fix := themedFixture()
	s.Loc, s.Locale = time.FixedZone(fix.Timezone, 8*3600), kit.ParseDateLocale(fix.Locale)
	s.Hooks = themedscreen.Hooks{
		OpenSettingsAI: func() {},
		OpenExternal:   func(string) error { return nil },
		OpenItem:       func(string, string) {},
	}
	return s
}

// libraryThemedScreen 是知识库场景的主题阅读界面（opt.themed 非空时挂到 libraryscreen.Screen.Themed）。
func libraryThemedScreen(look *kit.Look, ctx context.Context, guard kit.Guard, mode themedMode) *themedscreen.Screen {
	return newThemedScreen(look, ctx, guard, newThemedFake(themedFixture(), mode))
}

// themedOpenAI 点读者工具条里的「AI 阅读」：主题阅读把状态读进来。
func themedOpenAI(s *headless.Session, _ *kit.Look, _ *libraryEnv) error {
	if err := parity.ClickRole(s, gfx.RoleButton, locales.ThemedReadingReaderAi()); err != nil {
		return err
	}
	return wikiSettle(s)
}

// themedDetails 展开状态卡里折叠的「生成详情」（失败原因与用量在里面）。
func themedDetails(s *headless.Session, _ *kit.Look, _ *libraryEnv) error {
	if err := parity.ClickRole(s, gfx.RoleButton, locales.ThemedReadingReaderTaskDetails()); err != nil {
		return err
	}
	return wikiSettle(s)
}

// themedOpenReferences 点「阅读页设置」→ 菜单里的「参考资料」：资料面板打开。
func themedOpenReferences(s *headless.Session, _ *kit.Look, _ *libraryEnv) error {
	n, ok := s.AccessByRole(gfx.RoleCombobox, locales.ThemedReadingReaderSettings())
	if !ok {
		return fmt.Errorf("找不到 %s「%s」", gfx.RoleCombobox, locales.ThemedReadingReaderSettings())
	}
	if err := s.Click(n.Bounds.X+n.Bounds.Width/2, n.Bounds.Y+n.Bounds.Height/2); err != nil {
		return err
	}
	if err := wikiSettle(s); err != nil {
		return err
	}
	if err := parity.ClickRole(s, gfx.RoleMenuItem, locales.ThemedReadingReaderReferences()); err != nil {
		return err
	}
	return wikiSettle(s)
}

// themedOpenSetup 点空态里的「生成阅读页」：生成设置对话框打开（渲染层的自动弹出要求没有 original 内容，知识库详情里总是带着原文，所以不会自动弹）。
func themedOpenSetup(s *headless.Session, _ *kit.Look, _ *libraryEnv) error {
	if err := parity.ClickRole(s, gfx.RoleButton, locales.ThemedReadingReaderCreate()); err != nil {
		return err
	}
	return wikiSettle(s)
}

// themedBaseSteps 把页面带到「对话框打开之前」（遮罩背后的页面，见 parity.BackdropSpec.BaseSteps）：选中笔记、切到 AI 阅读。
func themedBaseSteps(s *headless.Session, look *kit.Look) error {
	if err := listClickCard(detailNote)(s, look, nil); err != nil {
		return err
	}
	if err := themedOpenAI(s, look, nil); err != nil {
		return err
	}
	return parity.ParkAndBlur(s)
}

// inboxThemedScreen 是处理中心场景的主题阅读界面：任务列表里是夹具里的任务（已按更新时间从新到旧排好，假服务的列表读取按同一规则排序）。
func inboxThemedScreen(look *kit.Look, ctx context.Context) *themedscreen.Screen {
	fix := themedFixture()
	svc := &servicestest.ThemedFake{}
	svc.SetModels(fix.Models.Text, fix.Models.Image)
	svc.SetSearch(fix.Search)
	for _, task := range fix.Inbox.Tasks {
		svc.PutTask(task)
	}
	return newThemedScreen(look, ctx, kit.Guard{Notify: kit.NewNotifier(nil)}, svc)
}

// themedOpenTasks 点工作区页眉里的「主题页任务」：工作区换成任务列表（渲染层 setShowThemeTasks）。
func themedOpenTasks(s *headless.Session, _ *inboxEnv) error {
	if err := parity.ClickRole(s, gfx.RoleButton, locales.ThemedReadingTasks()); err != nil {
		return err
	}
	return wikiSettle(s)
}

// cardTitleWaivers 登记 A-013 在状态大卡标题上的延伸：标题是 text-base font-semibold tracking-tight（-0.025em）。
// 与 modalTitleWaivers（text-xl）同一成因，只是字号档不同：text-base 在小、中、大字号档是 14、16、18px，
// 每个字符比渲染层宽 0.35、0.4、0.45px；宽度差框在 [字符数×0.35−0.6, 字符数×0.45+0.6]，更大的偏差照常判定；墨迹位移上限是宽度差的一半再加 1 像素。
// 这个标题没有被截断，不需要 ellipsis-cut 一类。
func cardTitleWaivers(title string) []parity.Waiver {
	n := float64(utf8.RuneCountInString(title))
	return []parity.Waiver{
		{
			Ledger: "A-013", Layer: parity.LayerL2, Kind: "text-width", Subject: title,
			MinDelta: max(n*0.35-0.6, 0.01), MaxDelta: n*0.45 + 0.6,
			Note: "tracking-tight 的字距：Viva 没有字距，标题绘制宽度宽 字符数 × 0.025em",
		},
		{
			Ledger: "A-013", Layer: parity.LayerL2, Kind: "ink", Subject: title, MaxDelta: n*0.45/2 + 1,
			Note: "同上：标题被拉宽，墨迹整体位移最多宽度差的一半（只管横向）",
		},
	}
}

// 英文界面里被登记的标题（写死的英文，themed_waivers_en_test.go 与 locales 目录里英文画出的字逐一核对；Waivers 在 init 时构造，那时界面语言还是中文）。
const (
	enThemedSetupTitle  = "Generate reading page"         // themedReading.readerCreate：模态标题（text-xl）
	enThemedRefsTitle   = "References"                    // themedReading.readerReferences：模态标题（text-xl）
	enThemedRunTitle    = "Designing page"                // 进行中任务状态大卡的标题（text-base）
	enThemedFailedTitle = "This generation is incomplete" // 失败任务状态大卡的标题（text-base）
)

// englishTitleWaivers 是 modalTitleWaivers / cardTitleWaivers 的英文版：同一成因（tracking-tight），只在英文变体所在的字号档放过，
// 只取 L2（宽度与墨迹）两条，不含截断一类。沿用既有口径，不新增类别；英文 Subject 的延伸与 A-013 其他英文延伸一样待追认（台账 C-010）。
func englishTitleWaivers(modal bool, title string) []parity.Waiver {
	base := cardTitleWaivers(title)
	if modal {
		base = modalTitleWaivers(title)
	}
	var out []parity.Waiver
	for _, w := range base {
		if w.Layer != parity.LayerL2 {
			continue
		}
		w.Variant = englishTier
		out = append(out, w)
	}
	return out
}

// themedRunningTitle 是进行中任务在状态大卡里的标题（阶段名；没有进度计数的夹具任务）。
func themedRunningTitle(task services.ThemedTask) string { return themedscreen.StageLabel(task.Stage) }

func init() {
	// 知识库详情的主题阅读：读者工具条要画（readerToolbar），React 一侧的 config.detail.themed 只隐去岛里的页面本体。
	base := libraryOptions{variant: "data", list: true, detail: true, readerToolbar: true}
	withMode := func(mode themedMode) libraryOptions {
		opt := base
		opt.themed = mode
		return opt
	}
	cards := semiboldKerningWaivers()
	selectNote := listClickCard(detailNote)
	reg := func(id string, opt libraryOptions, steps libraryEnvStep, mode parkMode, waivers ...parity.Waiver) parity.Scene {
		s := libraryEnvScene(id, opt, steps, mode, waivers...)
		registry[id] = s
		return s
	}
	// 生成设置对话框与资料面板是 Modal：遮罩背后的页面模糊（A-007，backdrop-blur-md 是 12），浮层文字的墨迹总量偏大（A-009）。
	modal := func(id string, name func() string) {
		spec := modalBackdrop("")
		spec.PanelNameFn = name
		spec.BaseSteps = themedBaseSteps
		registry[id] = withBackdrop(registry[id], spec)
	}
	overlay := append([]parity.Waiver{overlayInkWaiver}, cards...)

	const (
		setup   = "app.window.library.detail-themed-setup"
		running = "app.window.library.detail-themed-running"
		failed  = "app.window.library.detail-themed-failed"
		tools   = "app.window.library.detail-themed-tools"
	)
	// 标题的 tracking-tight（A-013）：模态页眉标题、状态大卡的标题都是同一成因，复用 modalTitleWaivers 的窄口径（按字符数框住宽度差）。
	withTitles := func(base []parity.Waiver, titles ...string) []parity.Waiver {
		out := slices.Clone(base)
		for _, title := range titles {
			out = append(out, modalTitleWaivers(title)...)
		}
		return out
	}
	reg(setup, withMode(themedEmpty), listChain(selectNote, themedOpenAI, themedOpenSetup), parkAll,
		append(withTitles(overlay, locales.ThemedReadingReaderCreate()), englishTitleWaivers(true, enThemedSetupTitle)...)...)
	modal(setup, locales.ThemedReadingReaderCreate)
	reg(running, withMode(themedRunning), listChain(selectNote, themedOpenAI), parkAll,
		slices.Concat(cards, cardTitleWaivers(themedRunningTitle(themedFixture().Running)), englishTitleWaivers(false, enThemedRunTitle))...)
	reg(failed, withMode(themedFailed), listChain(selectNote, themedOpenAI, themedDetails), parkAll,
		slices.Concat(cards, cardTitleWaivers(themedscreen.StateLabel(themedFixture().Failed.State)), englishTitleWaivers(false, enThemedFailedTitle))...)
	// 资料链接是 400 字重：Chromium 对 400 做字偶距、Viva 不做（A-014），含拉丁字母的链接整行 Viva 略宽（0.73px），只差在横向墨迹 1px。
	linkKern := parity.Waiver{
		Ledger: "A-014", Layer: parity.LayerL2, Kind: "ink", Subject: "Rob Pike：并发不是并行", MaxDelta: 1,
		Note: "字偶距：Chromium 对 400 字重做字偶距、Viva 不做，累积成墨迹水平位移 1px（只管横向）",
	}
	reg(tools, withMode(themedPage), listChain(selectNote, themedOpenAI, themedOpenReferences), parkAll,
		slices.Concat(withTitles(overlay, locales.ThemedReadingReaderReferences()), englishTitleWaivers(true, enThemedRefsTitle), []parity.Waiver{linkKern})...)
	modal(tools, locales.ThemedReadingReaderReferences)

	// 处理中心的「主题页任务」：处理中心整窗，点工作区页眉里的按钮，工作区换成任务列表。
	registry["app.window.inbox.themed-tasks"] = inboxScene("app.window.inbox.themed-tasks", inboxOptions{themedTasks: true}, chainInbox(themedOpenTasks),
		// 任务卡标题是 font-medium（500）：带拉丁字母的标题 Viva 做字偶距、Chromium 不做（A-026，同卡片标题）。
		parity.Waiver{Ledger: "A-026", Layer: parity.LayerL2, Kind: "text-width", Subject: "技术论坛：SQLite WAL 模式的备份坑", MaxDelta: semiboldWidthCap,
			Note: "字偶距：Viva 对 500 字重做字偶距、Chromium 不做，带拉丁字母的标题整行偏窄"},
		parity.Waiver{Ledger: "A-026", Layer: parity.LayerL2, Kind: "ink", Subject: "技术论坛：SQLite WAL 模式的备份坑", MaxDelta: semiboldInkCap,
			Note: "同上：整行宽度差累积成墨迹剖面的最佳对齐位移（只管横向）"})
}
