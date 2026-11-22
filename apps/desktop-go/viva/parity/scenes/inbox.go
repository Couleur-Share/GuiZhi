package scenes

import (
	"context"
	"errors"
	"slices"
	"strconv"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/parity"
	inboxscreen "guizhi-mygo-shell/viva/screens/inbox"
	shellscreen "guizhi-mygo-shell/viva/screens/shell"
)

// 处理中心（M3-1）的整窗对照场景：顶栏 + rail + 二级侧栏（筛选）+ 工作区（待办列表）。
// 与 React 采集器（scripts/parity/scenes/react-inbox-scenes.mjs、lib/react-inbox.mjs）一一对应：同一份夹具
// （viva/parity/testdata/inbox-fixture.json）、同样的操作。React 一侧在页内替换 window.api 的读取接口并按配置设定模型是否已配置，
// Viva 一侧用假服务（servicestest.Inbox）装配同样的读取结果。

// inboxOptions 是一个场景的数据与模型配置，与 React 采集器 enterInbox 的选项同名同义。
type inboxOptions struct {
	variant        string // data（各分组都有条目）| empty（什么都没有）
	loadError      bool   // 主列表读取失败
	wikiNeedsModel bool   // Wiki 场景没有配置文本模型
	semanticError  bool   // 语义索引状态读取失败（局部错误条，不挡主列表）
}

// inboxEnv 是一次渲染里步骤能用到的东西，由 prepare 装配页面时填入；每次渲染各有一份（见 parity.Scene.Prepare）。
type inboxEnv struct {
	store   *inboxscreen.Store
	variant parity.InboxVariant
	// compact 表示视口处于紧凑布局（≤ 1179.98px）：二级侧栏默认收起，要先点顶栏的「展开」才点得到里面的按钮。
	compact bool
}

// inboxSteps 是处理中心场景的操作步骤；nil 表示不操作。
type inboxSteps func(s *headless.Session, e *inboxEnv) error

// newFakeInbox 按选项给假服务放读取结果：与 React 一侧页内替换的 window.api 返回同一组数据。
func newFakeInbox(fix parity.InboxFixture, v parity.InboxVariant, opt inboxOptions) *servicestest.Inbox {
	svc := &servicestest.Inbox{}
	svc.SetList(v.List)
	svc.SetWikiCandidates(v.Wiki)
	svc.SetWikiModelReady(!opt.wikiNeedsModel)
	svc.SetSemanticStatus(services.InboxSemanticStatus{
		Configured: true, EligibleItems: v.Semantic.EligibleItems, IndexedItems: v.Semantic.IndexedItems,
	})
	if opt.loadError {
		svc.FailOn("List", errors.New(fix.LoadError))
	}
	if opt.semanticError {
		svc.FailOn("SemanticStatus", errors.New(fix.SemanticError))
	}
	return svc
}

// inboxScene 是处理中心整窗的场景：夹具与 React 采集器共用。
//
// 页面与步骤由 Prepare 一起装配（理由同 importsScene）：Store 与步骤用的环境每次渲染各一份。
func inboxScene(id string, opt inboxOptions, steps inboxSteps, waivers ...parity.Waiver) parity.Scene {
	prepare := func(look *kit.Look) (func(c ui.C), func(s *headless.Session) error) {
		fix, err := parity.LoadInboxFixture()
		if err != nil {
			panic(err) // 夹具是编译进来的常量，解码失败是程序错误
		}
		name := opt.variant
		if name == "" {
			name = "data"
		}
		variant, err := fix.Variant(name)
		if err != nil {
			panic(err)
		}
		ctx := context.Background()
		store := inboxscreen.NewStore(newFakeInbox(fix, variant, opt), kit.Guard{}, nil)
		// 对照要确定：后台动作同步执行。
		store.Spawn = func(fn func()) { fn() }
		env := &inboxEnv{store: store, variant: variant, compact: look.Viewport <= compactMaxWidth}
		screen := &inboxscreen.Screen{
			Look: look, Store: store, Ctx: ctx,
			Hooks: inboxscreen.Hooks{
				Collections:     func() []services.LibraryCollection { return fix.Collections },
				LoadCollections: func(context.Context) {},
			},
		}
		w := &shellscreen.Window{
			Look: look, Page: shellscreen.PageHome, Module: shellscreen.ModuleInbox,
			OwnControls: true, SearchShortcut: "Alt+Shift+F",
			Modules: map[shellscreen.Module]shellscreen.ModuleView{
				shellscreen.ModuleInbox: {Panel: screen.Panel, Workspace: screen.Workspace},
			},
		}
		view := func(c ui.C) {
			w.InboxTotal = store.View().Total
			w.View(c)
			screen.EndFrame()
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
		Waivers: slices.Concat(pageSettingsWaivers, inboxBaseWaivers(), waivers),
	}
}

// inboxBaseWaivers 是处理中心各场景共有的已登记偏差：二级侧栏顶部的「处理中心」是 text-xs font-medium tracking-wide（0.025em），
// 与标签浮层的小标题同一类（A-013：Viva 的文字没有字距，绘制宽度窄 字符数 × 0.025em，盒宽已按字数补齐）。只复用既有登记 A-013 与既有写法，没有新增台账条目。
func inboxBaseWaivers() []parity.Waiver {
	// 英文界面画出来的是大写的「INBOX」：字距之外再加字偶距（A-013 的英文延伸，只在中档字号档放过；实测宽度差 1.94px，字距应有 5 × 0.3 = 1.5）。
	return slices.Concat(wideTrackingWaivers("处理中心"), wideLatinTrackingWaivers("INBOX"))
}

// openInboxSidebar 对应 React 采集器的 openCompactSidebar：紧凑布局下先点顶栏的「展开」让二级侧栏以浮层打开，宽屏什么也不做。
func openInboxSidebar(s *headless.Session, e *inboxEnv) error {
	if !e.compact {
		return nil
	}
	if err := parity.ClickRole(s, gfx.RoleButton, "展开"); err != nil {
		return err
	}
	return s.Rest()
}

// closeInboxSidebar 对应 React 采集器的 closeCompactSidebar：紧凑布局下按 Esc 收起浮层（浮层盖住工作区左侧，不收起就点不到列表里的勾选框）。
func closeInboxSidebar(s *headless.Session, e *inboxEnv) error {
	if !e.compact {
		return nil
	}
	if err := s.Keys("escape"); err != nil {
		return err
	}
	return s.Rest()
}

// clickFilter 点二级侧栏里的一行筛选（可访问名是「名称 计数」），紧凑布局先展开侧栏浮层、点完再按 Esc 收起。
// 收起是为了可访问树的顺序：渲染层的浮层与遮罩在 DOM 里排在工作区之前，Viva 的可访问树按绘制顺序登记、浮层画在工作区之后，浮层开着时两边的节点顺序不同，
// L1 会整段错位（画面本身一致，L3 0.0093%）；所以整个处理中心的对照都不在浮层开着的状态下取样（紧凑布局浮层的可访问树顺序，台账尚未登记，见交付报告）。
func clickFilter(name func() string, count func(*inboxEnv) int) inboxSteps {
	return func(s *headless.Session, e *inboxEnv) error {
		if err := openInboxSidebar(s, e); err != nil {
			return err
		}
		if err := parity.ClickRole(s, gfx.RoleButton, name()+" "+strconv.Itoa(count(e))); err != nil {
			return err
		}
		if err := s.Rest(); err != nil {
			return err
		}
		return closeInboxSidebar(s, e)
	}
}

// filterUnclassified 点「未归合集」（计数取夹具里该分组的条数）。
var filterUnclassified = clickFilter(locales.InboxFilterUnclassified, func(e *inboxEnv) int { return e.variant.List.Counts.Unclassified })

// checkItems 依次点标题为 titles 的条目卡片的勾选框（可访问名「选择 标题」）。
func checkItems(titles func(*inboxEnv) []string) inboxSteps {
	return func(s *headless.Session, e *inboxEnv) error {
		for _, title := range titles(e) {
			if err := parity.ClickRole(s, gfx.RoleCheckbox, locales.InboxSelectItem(title)); err != nil {
				return err
			}
			// 第一次勾选后批量条出现、列表整体下移，后面的点按要用新的位置：Access 是上一帧的树，批量条在窄窗口里折行，多一帧才稳。
			if err := s.Frame(); err != nil {
				return err
			}
			if err := s.Rest(); err != nil {
				return err
			}
		}
		return nil
	}
}

// chainInbox 依次执行几步，最后把指针移开并清掉焦点（点过的控件持有焦点；渲染层采集前同样 blur）。
func chainInbox(steps ...inboxSteps) inboxSteps {
	return func(s *headless.Session, e *inboxEnv) error {
		for _, step := range steps {
			if err := step(s, e); err != nil {
				return err
			}
		}
		return parity.ParkAndBlur(s)
	}
}

// pickUnclassified 是「选中的两条未归合集」：列表里的第 1 条与第 3 条（半选状态，两条都是未归合集所以 AI 智能归类钮出现）。
func pickUnclassified(e *inboxEnv) []string {
	titles := e.variant.UnclassifiedTitles()
	return []string{titles[0], titles[2]}
}

func init() {
	// 全部待处理：复核项、采集问题、Wiki 候选摘要横幅、语义索引与 Wiki 聚合大卡片。
	registry["app.window.inbox.all"] = inboxScene("app.window.inbox.all", inboxOptions{}, nil)
	// 未归合集分组里勾选两条：半选的「全选当前列表」、批量整理条（合集下拉、标签输入、批量整理、AI 智能归类、取消）。
	registry["app.window.inbox.selected"] = inboxScene("app.window.inbox.selected", inboxOptions{},
		chainInbox(filterUnclassified, checkItems(pickUnclassified)))
	registry["app.window.inbox.unclassified"] = inboxScene("app.window.inbox.unclassified", inboxOptions{}, chainInbox(filterUnclassified))
	// 没有配置文本模型：黄框横幅与「去配置模型」；语义索引状态读取失败：局部错误条（role=alert），不挡主列表。
	registry["app.window.inbox.wiki-needs-model"] = inboxScene("app.window.inbox.wiki-needs-model",
		inboxOptions{wikiNeedsModel: true, semanticError: true}, nil)
	registry["app.window.inbox.empty"] = inboxScene("app.window.inbox.empty", inboxOptions{variant: "empty"}, nil)
	registry["app.window.inbox.error"] = inboxScene("app.window.inbox.error", inboxOptions{loadError: true}, nil)
}
