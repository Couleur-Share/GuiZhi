package scenes

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"

	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/parity"
)

// 知识库列表栏（M2-2）的整窗场景：rail + 顶栏 + 侧栏 + 工作区里的列表栏（工具条、卡片、分页）。
// 右侧详情栏在 React 一侧被隐去（scripts/parity/lib/react-library.mjs 的 hideDetail），Viva 一侧是空白底色。
// 条目来自夹具里 data 变体的 entries，两侧共用；操作的名字与位置与 React 采集器（scenes/react-library-list-scenes.mjs）一一对应。

// listCardAnchor 是卡片里的点击位置偏移：落在左内边距里，不压在任何控件（置顶图标、星标、类型图标）上。
const listCardAnchor float32 = 24

// listCard 找可访问名以 title 开头的卡片（按钮）；侧栏行的名字不会以条目标题开头。
func listCard(s *headless.Session, title string) (gfx.Rect, error) {
	for _, n := range s.Access() {
		if n.Role == gfx.RoleButton && strings.HasPrefix(n.Label, title) {
			return n.Bounds, nil
		}
	}
	return gfx.Rect{}, fmt.Errorf("找不到标题以「%s」开头的卡片", title)
}

// listClickCard 点一张卡片（对应 React 采集器的 clickCard：点卡片左内边距处）。
func listClickCard(title string) libraryEnvStep {
	return func(s *headless.Session, _ *kit.Look, _ *libraryEnv) error {
		b, err := listCard(s, title)
		if err != nil {
			return err
		}
		if err := s.Click(b.X+listCardAnchor, b.Y+b.Height/2); err != nil {
			return err
		}
		return s.Rest()
	}
}

// listHoverCard 把指针停在卡片左内边距处（对应 hoverCard）。
func listHoverCard(title string) libraryEnvStep {
	return func(s *headless.Session, _ *kit.Look, _ *libraryEnv) error {
		b, err := listCard(s, title)
		if err != nil {
			return err
		}
		if err := s.Move(b.X+listCardAnchor, b.Y+b.Height/2); err != nil {
			return err
		}
		return s.Rest()
	}
}

// listFocusCard 用 Tab 把键盘焦点送到卡片上（对应 focusCard：先按一次 Tab 进入键盘模态，再程序聚焦）。
//
// Tab 路上要经过侧栏的各个停靠点，最后一个（回收站）取得焦点时侧栏滚到底，而 React 一侧是程序聚焦，侧栏从没滚过：
// 到达卡片之后把侧栏滚回顶部，终态才一致（走法不同，状态相同——键盘模态下卡片带聚焦环、侧栏在顶部）。
func listFocusCard(title string) libraryEnvStep {
	return func(s *headless.Session, _ *kit.Look, _ *libraryEnv) error {
		for range 120 {
			for _, n := range s.Access() {
				if n.Role == gfx.RoleButton && strings.HasPrefix(n.Label, title) && n.Focused {
					if err := parity.ScrollDown(s, listSidebarX, listSidebarY, -listSidebarScroll); err != nil {
						return err
					}
					if err := s.Rest(); err != nil {
						return err
					}
					return parity.ParkPointer(s)
				}
			}
			if err := s.Keys("tab"); err != nil {
				return err
			}
		}
		return fmt.Errorf("按了 120 次 Tab 仍没有走到标题以「%s」开头的卡片", title)
	}
}

// 侧栏滚回顶部用的点位与滚动量：点位在侧栏的空白行之间（只要落在侧栏的滚动区里），滚动量远大于侧栏的内容高度。
const (
	listSidebarX, listSidebarY float32 = 150, 300
	listSidebarScroll          float32 = 4000
)

// listWaitSpinner 让读取中的转圈走过 250 ms 的延迟与淡入（React 一侧等 600 ms 才取图）。
func listWaitSpinner(s *headless.Session, _ *kit.Look, _ *libraryEnv) error {
	return s.Advance(600 * time.Millisecond)
}

// listSearch 对应 React 采集器的 searchList：点顶栏搜索框、打字，等 300 ms 防抖到点（这里由步骤直接让 Store 读一次），
// 最后点 rail 中段的空白处让输入框失焦。
func listSearch(text string) libraryEnvStep {
	return func(s *headless.Session, _ *kit.Look, e *libraryEnv) error {
		if err := clickRolePrefix(s, gfx.RoleTextField, "搜索"); err != nil {
			return err
		}
		if err := s.Type(text); err != nil {
			return err
		}
		if err := s.Rest(); err != nil {
			return err
		}
		e.store.SetSearch(e.ctx, text) // 防抖到点之后的那次读取
		if err := s.Rest(); err != nil {
			return err
		}
		if err := s.Click(listBlurX, listBlurY); err != nil {
			return err
		}
		return s.Rest()
	}
}

// listBlurX、listBlurY 是顶栏里侧栏开关与搜索框之间的空白处：点它让搜索框失焦（与 React 一侧的 BLUR_X、BLUR_Y 一致）。
// 不用 imports 场景的 rail 中段（blurX、blurY）：知识库窗口的 rail 上有五个模块按钮，那个位置落在「采集」上，点下去会换页。
const listBlurX, listBlurY = 200, 20

// listClickButton 点名为 name 的按钮，等画面落定。
func listClickButton(name string) libraryEnvStep {
	return func(s *headless.Session, _ *kit.Look, _ *libraryEnv) error {
		if err := parity.ClickRole(s, gfx.RoleButton, name); err != nil {
			return err
		}
		return s.Rest()
	}
}

// listClickSort 点工具条上的排序按钮：可访问名是「排序: 当前排序」，带弹出菜单的按钮在可访问树里是 combobox。
func listClickSort(s *headless.Session, _ *kit.Look, _ *libraryEnv) error {
	if err := clickRolePrefix(s, gfx.RoleCombobox, "排序"); err != nil {
		return err
	}
	return s.Rest()
}

// listPickMenuItem 点菜单里名为 name 的项。
func listPickMenuItem(name string) libraryEnvStep {
	return func(s *headless.Session, _ *kit.Look, _ *libraryEnv) error {
		if err := parity.ClickRole(s, gfx.RoleMenuItem, name); err != nil {
			return err
		}
		return s.Rest()
	}
}

// listPickPageSize 打开「每页」下拉并选一项。
func listPickPageSize(option string) libraryEnvStep {
	return func(s *headless.Session, _ *kit.Look, _ *libraryEnv) error {
		if err := clickRolePrefix(s, gfx.RoleCombobox, "每页"); err != nil {
			return err
		}
		if err := s.Rest(); err != nil {
			return err
		}
		if err := parity.ClickRole(s, gfx.RoleOption, option); err != nil {
			return err
		}
		return s.Rest()
	}
}

// listApplyFacets 在侧栏里依次点几行（按可访问名，名字里没有计数），把筛选叠上去。
// 紧凑布局下侧栏是浮层：先点「展开」，叠完按 Esc 收起（对应 React 一侧的 openCompactSidebar、closeCompactSidebar）。
func listApplyFacets(rows ...string) libraryEnvStep {
	return func(s *headless.Session, look *kit.Look, _ *libraryEnv) error {
		compact := look.Viewport <= compactMaxWidth
		if err := openLibraryCompactSidebar(s, compact); err != nil {
			return err
		}
		for _, name := range rows {
			// 渲染层的 click 会先把目标滚进视野（侧栏里行多时，标签与回收站在折叠线以下），这里对应地先滚过去。
			if err := libraryReveal(s, look, name); err != nil {
				return err
			}
			if err := parity.ClickRole(s, gfx.RoleButton, name); err != nil {
				return err
			}
			if err := s.Rest(); err != nil {
				return err
			}
		}
		if compact {
			if err := s.Keys("Escape"); err != nil {
				return err
			}
			return s.Rest()
		}
		return nil
	}
}

// listOpenFilterSummary 点工具条上的「组合筛选 · N」，让弹层展开。
func listOpenFilterSummary(s *headless.Session, _ *kit.Look, _ *libraryEnv) error {
	if err := clickRolePrefix(s, gfx.RoleButton, "组合筛选"); err != nil {
		return err
	}
	return s.Rest()
}

// 叠加的分面：合集「学习笔记」、标签「AI」、平台「网页」，条目里恰有一条同时满足（夹具 col-study/web/tag-ai）。
var (
	facetsTwo   = []string{"学习笔记", "AI"}
	facetsThree = []string{"学习笔记", "AI", "网页"}
)

// listChain 依次执行几步。
func listChain(steps ...libraryEnvStep) libraryEnvStep {
	return func(s *headless.Session, look *kit.Look, e *libraryEnv) error {
		for _, step := range steps {
			if err := step(s, look, e); err != nil {
				return err
			}
		}
		return nil
	}
}

// 第一页上、不带搜索时排在前面的几张卡片的标题。
const (
	cardPinned = "学习笔记：Go 并发模式"
	cardPlain  = "如何构建本地优先的个人知识库"
)

// semiboldKerningWaivers 登记 A-026：卡片标题是 font-medium（500），带拉丁字母的标题在两个引擎里宽度不同。
// Viva 对 500 / 600 字重走 DirectWrite 路径，做字偶距（14px 时 Yo −1.50、To −1.55、AV −1.29、We −0.88）；
// Chromium 对这两档字重不做字偶距（font-kerning:auto 与 none 的宽度逐位相同，取证见台账 A-026），与 400、700 字重（A-014）相反：
// 那两档 Chromium 做、Viva 不做。逐字符的字宽两边一致，差的只是字偶对的总和。
//
// 后果：整行偏窄（text-width，实测最大 5.4px，是「YouTube: The Future of Local-First Software」）；
// 同一行里拉丁字母之后的汉字整体左移（ink，≤ 4px）。这里只登记这两种字形层面的差：
// 标题的占位与折行（钮宽、折行点、行数）早已改按渲染层不做字偶距的宽度自己排（kit.TextNoKern，台账 B-044），
// 不再有「整行宽度恰在折行边界上、Viva 多放下一个词 / 一个汉字」的翻转，对应的放过项随之删除（2026-11-19）。
// 各档按「应有的大小」框住：字号档放大时字偶距按字号放大（15.75px 时约 1.125 倍）。
func semiboldKerningWaivers() []parity.Waiver {
	fix, err := parity.LoadLibraryFixture()
	if err != nil {
		panic(err) // 夹具是编译进来的常量，解码失败是程序错误
	}
	titles := map[string]bool{}
	for _, variant := range fix.Variants {
		for _, e := range variant.Entries {
			if hasLatinLetter(e.Title) {
				titles[e.Title] = true
			}
		}
	}
	var out []parity.Waiver
	for _, title := range slices.Sorted(maps.Keys(titles)) {
		out = append(out,
			parity.Waiver{
				Ledger: "A-026", Layer: parity.LayerL2, Kind: "text-width", Subject: title, MaxDelta: semiboldWidthCap,
				Note: "字偶距：Viva 对 500 字重做字偶距、Chromium 不做，带拉丁字母的标题整行偏窄（≤ 字偶对之和）",
			},
			parity.Waiver{
				Ledger: "A-026", Layer: parity.LayerL2, Kind: "ink", Subject: title, MaxDelta: semiboldInkCap,
				Note: "同上：整行宽度差累积成墨迹剖面的最佳对齐位移（只管横向，纵向错位照常判定）",
			})
	}
	return out
}

const (
	semiboldWidthCap float64 = 7 // 字偶对之和的上限：实测最大 5.4px（14px），字号档放大后约 6.1px
	semiboldInkCap   float64 = 5 // 墨迹剖面最佳位移的上限：实测最大 4px
)

// hasLatinLetter 报告 s 里有没有 ASCII 字母。
func hasLatinLetter(s string) bool {
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return true
		}
	}
	return false
}

func init() {
	list := libraryOptions{variant: "data", list: true}
	reg := func(id string, opt libraryOptions, steps libraryEnvStep, mode parkMode, waivers ...parity.Waiver) {
		registry[id] = libraryEnvScene(id, opt, steps, mode, waivers...)
	}
	cards := semiboldKerningWaivers()
	reg("app.window.library.list", list, nil, parkAll, cards...)
	reg("app.window.library.list-selected", list, listClickCard(cardPinned), parkAll, cards...)
	reg("app.window.library.list-hover", list, listHoverCard(cardPlain), parkNone, cards...)
	reg("app.window.library.list-focus", list, listFocusCard(cardPlain), parkKeepFocus, cards...)
	reg("app.window.library.list-empty", libraryOptions{variant: "empty", list: true}, nil, parkAll)
	reg("app.window.library.list-loading", libraryOptions{variant: "data", list: true, fault: faultHold}, listWaitSpinner, parkAll)
	reg("app.window.library.list-error", libraryOptions{variant: "data", list: true, fault: faultFail}, nil, parkAll)
	// 搜索中排序按钮禁用（disabled:opacity-50），「相关度」是整组带透明度的文字（A-009 的延伸）。
	reg("app.window.library.list-search-empty", list, listSearch("没有任何条目含有这个词"), parkAll, opacityInkWaiver("「相关度」"))
	reg("app.window.library.list-sort-menu", list, listClickSort, parkKeepFocus, append([]parity.Waiver{overlayInkWaiver}, cards...)...)
	reg("app.window.library.list-sort-title", list, listChain(listClickSort, listPickMenuItem("标题 A→Z")), parkAll, cards...)
	reg("app.window.library.list-trash", list, listApplyFacets("回收站"), parkAll, cards...)
	reg("app.window.library.list-filters", list, listApplyFacets(facetsTwo...), parkAll, cards...)
	reg("app.window.library.list-filters-open", list, listChain(listApplyFacets(facetsThree...), listOpenFilterSummary), parkKeepFocus,
		append([]parity.Waiver{overlayInkWaiver}, cards...)...)
	reg("app.window.library.list-page2", list, listChain(listPickPageSize("10"), listClickButton("下一页")), parkAll, cards...)
}
