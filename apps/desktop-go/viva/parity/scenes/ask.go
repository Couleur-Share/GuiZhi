package scenes

import (
	"context"
	"slices"
	"strconv"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/parity"
	askscreen "guizhi-mygo-shell/viva/screens/ask"
	shellscreen "guizhi-mygo-shell/viva/screens/shell"
)

// AI 问答（M3-4）的整窗对照场景：全局问答的空态、一轮带来源与证据的对话、未配置模型、围绕本文的会话（顶栏 + rail + 二级侧栏 + 工作区），
// 以及知识库详情右侧的「围绕本文提问」栏（台账 B-061）。
// 与 React 采集器（scripts/parity/scenes/react-ask-scenes.mjs、lib/react-ask.mjs）一一对应：同一份夹具（viva/parity/testdata/ask-fixture.json）、
// 同样的操作。React 一侧在页内替换 window.api 的会话读写接口并按配置设定模型是否已配置，Viva 一侧用假服务（servicestest.AskFake、Inbox）装配同样的读取结果。

// askOptions 是一个场景的数据与模型配置，与 React 采集器 enterAsk 的选项同名同义。
type askOptions struct {
	remembered    string // 上次活跃会话的 ID；空表示没有（进空态）
	notConfigured bool   // 没有配置文本模型（embedding 模型仍在，语义索引卡照常出现）
}

// newFakeAsk 按夹具与选项给假服务放会话与模型状态：与 React 一侧页内替换的 window.api 返回同一组数据。
func newFakeAsk(fix parity.AskFixture, opt askOptions) (*servicestest.AskFake, *servicestest.Inbox) {
	svc := &servicestest.AskFake{}
	for _, s := range fix.Sessions {
		svc.PutSession(s.Record())
	}
	// 本文标题（React 一侧 knowledge.get）：围绕本文的会话头部与「返回文章」读它。
	svc.PutItemTitle(fix.Article.ItemID, fix.Article.Title)
	svc.SetQAConfigured(!opt.notConfigured)
	inbox := &servicestest.Inbox{}
	inbox.SetSemanticStatus(services.InboxSemanticStatus{
		Configured: true, EligibleItems: fix.Semantic.EligibleItems, IndexedItems: fix.Semantic.IndexedItems,
	})
	return svc, inbox
}

// newAskScreen 装配问答界面：与根（viva/app/root_ask.go mountAsk）同一套仓库，后台动作同步执行、时钟与时区取夹具，对照才确定。
func newAskScreen(look *kit.Look, ctx context.Context, fix parity.AskFixture, opt askOptions) *askscreen.Screen {
	svc, inbox := newFakeAsk(fix, opt)
	prefs := &servicestest.Prefs{}
	if opt.remembered != "" {
		prefs.Put(askscreen.PrefActiveSession, strconv.Quote(opt.remembered))
	}
	guard := kit.Guard{}
	store := askscreen.NewStore(svc, guard, nil)
	store.Ctx, store.Prefs = ctx, prefs
	store.Spawn = func(fn func()) { fn() }
	now := func() time.Time { return time.UnixMilli(fix.Now) }
	store.Now = now
	seq := 0
	store.NewID = func(prefix string) string { seq++; return prefix + "-parity-" + strconv.Itoa(seq) }
	history := askscreen.NewHistoryStore(svc, guard, nil)
	history.Ctx = ctx
	store.OnSaved = func() { history.Load(false) }
	setup := askscreen.NewSetup(svc, inbox, guard, nil)
	setup.Ctx = ctx
	return &askscreen.Screen{
		Look: look, Store: store, History: history, Setup: setup, Ctx: ctx, Guard: guard,
		Now: now, Loc: time.FixedZone(fix.Timezone, 8*3600), Locale: kit.ParseDateLocale(fix.Locale),
		After: func(time.Duration, func()) func() { return func() {} },
	}
}

// askEnv 是一次渲染里步骤能用到的东西；每次渲染各一份（见 parity.Scene.Prepare）。
type askEnv struct {
	fix parity.AskFixture
}

// askSteps 是问答场景的操作步骤；nil 表示不操作。
type askSteps func(s *headless.Session, e *askEnv) error

// askScene 是问答整窗的场景：夹具与 React 采集器共用。页面与步骤由 Prepare 一起装配（理由同 importsScene）：仓库与步骤用的环境每次渲染各一份。
func askScene(id string, opt askOptions, steps askSteps, waivers ...parity.Waiver) parity.Scene {
	prepare := func(look *kit.Look) (func(c ui.C), func(s *headless.Session) error) {
		fix, err := parity.LoadAskFixture()
		if err != nil {
			panic(err) // 夹具是编译进来的常量，解码失败是程序错误
		}
		screen := newAskScreen(look, context.Background(), fix, opt)
		w := &shellscreen.Window{
			Look: look, Page: shellscreen.PageHome, Module: shellscreen.ModuleAsk,
			OwnControls: true, SearchShortcut: "Alt+Shift+F",
			Modules: map[shellscreen.Module]shellscreen.ModuleView{
				shellscreen.ModuleAsk: {Panel: screen.Panel, Workspace: screen.Workspace},
			},
		}
		view := func(c ui.C) {
			w.View(c)
			screen.EndFrame()
		}
		var run func(s *headless.Session) error
		if steps != nil {
			env := &askEnv{fix: fix}
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
		Waivers: slices.Concat(pageSettingsWaivers, askBaseWaivers(), waivers),
	}
}

// askBaseWaivers 是问答各场景共有的已登记偏差：二级侧栏里的「历史会话」分组标题是 text-xs font-medium tracking-wide（0.025em），
// 与处理中心侧栏标题同一类（A-013：Viva 的文字没有字距，绘制宽度窄 字符数 × 0.025em，盒宽已按字数补齐）。只复用既有登记 A-013 与既有写法，没有新增台账条目；
// 英文界面画出来的是大写的「HISTORY」，同样沿用 A-013 的英文延伸。
func askBaseWaivers() []parity.Waiver {
	// 会话行标题是 truncate 的单行，被截断处前是空格时 Chromium 留着、Viva 去掉（A-025，既有的 ellipsisSpaceWaivers，text-sm 区间 [3, 5.6]）。
	return slices.Concat(wideTrackingWaivers("历史会话"), wideLatinTrackingWaivers("HISTORY"), ellipsisSpaceWaivers)
}

// conversationWaivers 是一轮对话场景的已登记偏差，都只沿用既有条目、没有新增台账：
//   - A-014：回答第一段含「goroutine」「channel」「WaitGroup」三个拉丁词，Chromium 做字偶距、Viva 不做，整行宽度 Viva 多约 1.1px
//     （中档实测 1.09px；上限 1.6 给大字号档的放大留余量，更大的偏差照常判定）。
//   - A-016：来源 chip 的编号是 text-[10px] 等宽的「[1]」，「1」只有一道细竖笔、「]」笔画细，Viva 的墨迹总量比渲染层少
//     （中档实测「1」0.819、「]」0.789，与既有登记 smallLatinInkWaiver 同一类现象，区间 [0.1, 0.25]）。
func conversationWaivers() []parity.Waiver {
	return []parity.Waiver{
		{
			Ledger: "A-014", Layer: parity.LayerL2, Kind: "text-width", Subject: "goroutine 泄漏最常见", MaxDelta: 1.6,
			Note: "字偶距：回答第一段含三个拉丁词，Chromium 做字偶距、Viva 不做，整行宽度 Viva 多约 1.1px",
		},
		smallLatinInkWaiver("「1」"),
		smallLatinInkWaiver("「]」"),
	}
}

// libraryAskScreen 装配知识库详情右侧提问栏用的问答界面（libraryOptions.askPanel）：与全局问答同一套仓库，没有上次会话。
func libraryAskScreen(look *kit.Look, ctx context.Context) *askscreen.Screen {
	fix, err := parity.LoadAskFixture()
	if err != nil {
		panic(err)
	}
	return newAskScreen(look, ctx, fix, askOptions{})
}

// askClickFirstSource 点第一枚来源 chip：展开该来源回答时的证据摘录，chip 高亮 2 秒（React 一侧等 2.6 秒让高亮走完，这里等画面静止）。
// 第一条来源是夹具里的文章（item-001），chip 的可访问名是「[1] 标题」。
func askClickFirstSource(s *headless.Session, e *askEnv) error {
	if err := parity.ClickRole(s, gfx.RoleButton, "[1] "+e.fix.Article.Title); err != nil {
		return err
	}
	if err := s.Rest(); err != nil {
		return err
	}
	return parity.ParkAndBlur(s)
}

// askOpenArticlePanel 点详情头部的「提问」：右侧出提问栏，本文会话读进来。
func askOpenArticlePanel(s *headless.Session, _ *kit.Look, _ *libraryEnv) error {
	if err := parity.ClickRole(s, gfx.RoleButton, locales.ArticleAskTitle()); err != nil {
		return err
	}
	return s.Rest()
}

func init() {
	fix, err := parity.LoadAskFixture()
	if err != nil {
		panic(err)
	}
	registry["app.window.ask.empty"] = askScene("app.window.ask.empty", askOptions{}, nil)
	registry["app.window.ask.conversation"] = askScene("app.window.ask.conversation",
		askOptions{remembered: fix.ConversationID}, askClickFirstSource, conversationWaivers()...)
	registry["app.window.ask.not-configured"] = askScene("app.window.ask.not-configured", askOptions{notConfigured: true}, nil)
	registry["app.window.ask.article-global"] = askScene("app.window.ask.article-global", askOptions{remembered: fix.ArticleSessionID}, nil)

	// 知识库详情 + 右侧提问栏（台账 B-061）：场景名跟随既有详情场景的 detail-<名> 写法。
	detailAsk := libraryOptions{variant: "data", list: true, detail: true, askPanel: true}
	registry["app.window.library.detail-ask"] = libraryEnvScene("app.window.library.detail-ask", detailAsk,
		listChain(listClickCard(detailNote), askOpenArticlePanel), parkAll,
		slices.Concat(semiboldKerningWaivers(), chipEllipsisWaivers)...)
}

// chipEllipsisWaivers 登记 A-025 在提问栏来源 chip 上的延伸：chip 是 text-xs（小、中、大字号档 10.5、12、13.5px）的单行 truncate，
// 窄栏里第二枚 chip 的标题被省略号截断，截断点前恰是空格（「…Pipelines …」）。Chromium 把空格留着，Viva 去掉，省略号左移一个空格宽；
// 既有登记 ellipsisSpaceWaivers 的区间 [3, 5.6] 是按 text-sm 量的（空格 3.4–4.4px），text-xs 的空格更窄（2.9–3.8px，
// 再减去省略号字形量法的零点几像素，实测中档 2.85px），落在它的下沿之外。成因、主题（「 …」）与方向完全相同，只补 text-xs 的区间。
var chipEllipsisWaivers = []parity.Waiver{{
	Ledger: "A-025", Layer: parity.LayerL2, Kind: "text-width", Subject: " …」", MinDelta: 2.2, MaxDelta: 3.2,
	Note: "省略号前的空格（text-xs）：Chromium 留着，Viva 去掉，省略号左移一个空格宽（A-025 的 text-xs 延伸）",
}}
