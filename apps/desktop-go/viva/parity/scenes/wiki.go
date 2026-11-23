package scenes

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/parity"
	shellscreen "guizhi-mygo-shell/viva/screens/shell"
	wikiscreen "guizhi-mygo-shell/viva/screens/wiki"
)

// Wiki（M3-5a）的整窗对照场景：空态、页面详情、编辑态、更新建议、编译对话框（rail + 顶栏 + 二级侧栏筛选 + 工作区）。
// 与 React 采集器（scripts/parity/scenes/react-wiki-scenes.mjs、lib/react-wiki.mjs）一一对应：同一份夹具（viva/parity/testdata/wiki-fixture.json）、同样的操作。
// React 一侧在页内替换 window.api 的 wiki 读取接口并补一个已配置的对话模型，Viva 一侧用假服务（servicestest.WikiFake）装配同样的读取结果。
//
// 关系图谱不在这里：渲染层用 react-force-graph-2d 做物理力导向布局（节点位置每次不同），不适合像素对拍；
// 原生 Canvas 图谱的取色、缩放平移与命中由 viva/screens/wiki 的 ui_graph_test.go 覆盖。

// wikiOptions 是一个场景的数据配置，与 React 采集器 enterWiki 的选项同名同义。
type wikiOptions struct {
	empty bool // 没有任何页面：用夹具 empty 一组的状态行与预览
	plain bool // 页面都没有手动编辑标记：目录行里没有铅笔图标（Chromium 里它是有名字的 image，L1 没有映射规则，台账 G-027）
}

// newFakeWiki 按夹具与选项给假服务放页面、链接、来源、历史版本、更新建议、状态与编译预览：与 React 一侧页内替换的 window.api 返回同一组数据。
func newFakeWiki(fix parity.WikiFixture, opt wikiOptions) *servicestest.WikiFake {
	svc := &servicestest.WikiFake{}
	status, preview := fix.Status, fix.Compile.Preview
	if opt.empty {
		status, preview = fix.Empty.Status, fix.Empty.Preview
	} else {
		for _, p := range fix.Pages {
			if opt.plain {
				p.ManualEditedAt = nil
			}
			svc.PutPage(p)
		}
		for _, l := range fix.Links {
			svc.PutLink(l.From, l.To)
		}
		for id, sources := range fix.Sources {
			svc.PutSources(id, sources...)
		}
		for _, revs := range fix.Revisions {
			// PutRevision 后摆的排在前面；夹具里是新的在前，倒着摆。
			for i := len(revs) - 1; i >= 0; i-- {
				svc.PutRevision(revs[i])
			}
		}
		for id, sg := range fix.Suggestions {
			sg := sg
			svc.PutSuggestion(id, &sg)
		}
	}
	svc.SetStatusCounts(status.CompiledItemCount, status.EligibleItemCount)
	svc.SetPreview(preview)
	return svc
}

// newWikiScreen 装配 Wiki 界面：与根（viva/app/root_wiki.go mountWiki）同一套 Store 与 Screen，后台动作同步执行、时钟与时区取夹具，对照才确定。
func newWikiScreen(look *kit.Look, ctx context.Context, fix parity.WikiFixture, opt wikiOptions) *wikiscreen.Screen {
	guard := kit.Guard{}
	store := wikiscreen.NewStore(newFakeWiki(fix, opt), guard, nil)
	store.Ctx = ctx
	store.Spawn = func(fn func()) { fn() }
	return &wikiscreen.Screen{
		Look: look, Store: store, Ctx: ctx, Guard: guard,
		Now: func() time.Time { return time.UnixMilli(fix.Now) }, Loc: time.FixedZone(fix.Timezone, 8*3600), Locale: kit.ParseDateLocale(fix.Locale),
		After: func(time.Duration, func()) func() { return func() {} },
	}
}

// wikiEnv 是一次渲染里步骤能用到的东西；每次渲染各一份（见 parity.Scene.Prepare）。
type wikiEnv struct {
	fix parity.WikiFixture
}

// wikiSteps 是 Wiki 场景的操作步骤；nil 表示不操作。
type wikiSteps func(s *headless.Session, e *wikiEnv) error

// wikiScene 是 Wiki 整窗的场景：夹具与 React 采集器共用。页面与步骤由 Prepare 一起装配（理由同 askScene）：仓库与步骤用的环境每次渲染各一份。
func wikiScene(id string, opt wikiOptions, steps wikiSteps, waivers ...parity.Waiver) parity.Scene {
	prepare := func(look *kit.Look) (func(c ui.C), func(s *headless.Session) error) {
		fix, err := parity.LoadWikiFixture()
		if err != nil {
			panic(err) // 夹具是编译进来的常量，解码失败是程序错误
		}
		screen := newWikiScreen(look, context.Background(), fix, opt)
		w := &shellscreen.Window{
			Look: look, Page: shellscreen.PageHome, Module: shellscreen.ModuleWiki,
			OwnControls: true, SearchShortcut: "Alt+Shift+F",
			Modules: map[shellscreen.Module]shellscreen.ModuleView{
				shellscreen.ModuleWiki: {Panel: screen.Panel, Workspace: screen.Workspace},
			},
		}
		view := func(c ui.C) {
			w.View(c)
			screen.EndFrame()
		}
		var run func(s *headless.Session) error
		if steps != nil {
			env := &wikiEnv{fix: fix}
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
		Waivers: slices.Concat(pageSettingsWaivers, wikiBaseWaivers(), waivers),
	}
}

// wikiBaseWaivers 是 Wiki 各场景共有的已登记偏差：二级侧栏里的小标题「需要留意」「按类型」是 text-xs tracking-wide（0.025em），
// 与标签浮层的小标题同一类（A-013：Viva 的文字没有字距，绘制宽度窄 字符数 × 0.025em，盒宽已按字数补齐）。只复用既有登记 A-013 与既有写法，没有新增台账条目。
func wikiBaseWaivers() []parity.Waiver {
	return slices.Concat(wideTrackingWaivers(locales.WikiPanelNeedsAttention()), wideTrackingWaivers(locales.WikiPanelByKind()), wikiEnglishBaseWaivers())
}

// tocHeading 是页内目录收录的标题行（二、三级，渲染层 extractWikiToc）。
var tocHeading = regexp.MustCompile(`(?m)^#{2,3}\s`)

// wikiSettle 在点击之后补几帧再等动画停：滚动区靠上一帧布局判断是否溢出，内容变长的那一帧之后还要再画两三帧，滚动条槽与拇指才稳定（kit.ScrollArea 的说明）。
func wikiSettle(s *headless.Session) error {
	for range 3 {
		if err := s.Frame(); err != nil {
			return err
		}
	}
	return s.Rest()
}

// wikiSelectPage 点目录里标题为 title 的页面行：详情读进来，更新建议随后读一次。
// 页面行的可访问名是整行内容（标题 类型 摘要 引用数），以「标题 」开头；反向链接芯片的名字恰好是标题，不能误点，所以只认前缀加空格。
func wikiSelectPage(s *headless.Session, title string) error {
	for _, n := range s.Access() {
		if n.Role == gfx.RoleButton && strings.HasPrefix(n.Label, title+" ") {
			if err := s.Click(n.Bounds.X+n.Bounds.Width/2, n.Bounds.Y+n.Bounds.Height/2); err != nil {
				return err
			}
			return wikiSettle(s)
		}
	}
	return fmt.Errorf("找不到页面行「%s」", title)
}

// wikiOpenDetail 选典型详情页并展开页内目录（目录按钮的名字是「本页目录（N 节）」）。
func wikiOpenDetail(s *headless.Session, e *wikiEnv) error {
	page, err := e.fix.Page(e.fix.Scene.DetailPageID)
	if err != nil {
		return err
	}
	if err := wikiSelectPage(s, page.Title); err != nil {
		return err
	}
	if err := parity.ClickRole(s, gfx.RoleButton, locales.WikiToc(len(tocHeading.FindAllString(page.Body, -1)))); err != nil {
		return err
	}
	if err := wikiSettle(s); err != nil {
		return err
	}
	return parity.ParkAndBlur(s)
}

// wikiOpenEdit 选一页后点「编辑」：正文换成编辑框。
func wikiOpenEdit(s *headless.Session, e *wikiEnv) error {
	page, err := e.fix.Page(e.fix.Scene.EditPageID)
	if err != nil {
		return err
	}
	if err := wikiSelectPage(s, page.Title); err != nil {
		return err
	}
	if err := parity.ClickRole(s, gfx.RoleButton, locales.WikiEditPage()); err != nil {
		return err
	}
	if err := wikiSettle(s); err != nil {
		return err
	}
	return parity.ParkAndBlur(s)
}

// wikiOpenSuggestion 选手动维护的那一页，展开建议面板（折叠面板的摘要按钮名是建议的原因）。
func wikiOpenSuggestion(s *headless.Session, e *wikiEnv) error {
	page, err := e.fix.Page(e.fix.Scene.SuggestionPageID)
	if err != nil {
		return err
	}
	if err := wikiSelectPage(s, page.Title); err != nil {
		return err
	}
	if err := parity.ClickRole(s, gfx.RoleButton, e.fix.Suggestions[page.ID].Reason); err != nil {
		return err
	}
	if err := wikiSettle(s); err != nil {
		return err
	}
	return parity.ParkAndBlur(s)
}

// wikiOpenCompile 点顶栏的「编译 N 条新内容」：编译对话框带着预览候选打开。
func wikiOpenCompile(s *headless.Session, e *wikiEnv) error {
	if err := parity.ClickRole(s, gfx.RoleButton, locales.WikiCompilePending(e.fix.Compile.Preview.Counts.Ready)); err != nil {
		return err
	}
	if err := wikiSettle(s); err != nil {
		return err
	}
	return parity.ParkAndBlur(s)
}

// enWikiCompileTitle 是编译对话框标题的英文：场景在 init 里登记，那时界面语言还是中文，英文变体的标题放过条款要预先备好。
const enWikiCompileTitle = "Full-text compile & upgrades"

func init() {
	registry["app.window.wiki.empty"] = wikiScene("app.window.wiki.empty", wikiOptions{empty: true}, nil, wikiEnglishEmptyWaivers()...)
	registry["app.window.wiki.detail"] = wikiScene("app.window.wiki.detail", wikiOptions{plain: true}, wikiOpenDetail, wikiEnglishDetailWaivers("SQLite 预写日志")...)
	registry["app.window.wiki.edit"] = wikiScene("app.window.wiki.edit", wikiOptions{}, wikiOpenEdit, wikiEnglishEditWaivers()...)
	registry["app.window.wiki.suggestion"] = wikiScene("app.window.wiki.suggestion", wikiOptions{plain: true}, wikiOpenSuggestion,
		slices.Concat(wikiEnglishDetailWaivers("本地优先架构"), wikiEnglishSuggestionWaivers())...)
	// 编译对话框是 Modal：遮罩背后的页面模糊（A-007，backdrop-blur-md 是 12），浮层文字的墨迹总量偏大（A-009），标题带 tracking-tight 的字距（A-013）。
	compile := wikiScene("app.window.wiki.compile", wikiOptions{plain: true}, wikiOpenCompile,
		slices.Concat([]parity.Waiver{overlayInkWaiver}, modalTitleWaivers(locales.WikiCompileDialog()), modalTitleWaivers(enWikiCompileTitle), wikiEnglishCompileWaivers())...)
	registry["app.window.wiki.compile"] = withBackdrop(compile,
		&parity.BackdropSpec{Sigma: 12, ScrimAlpha: 0.6, PanelRole: "group", PanelNameFn: locales.WikiCompileDialog})
}
