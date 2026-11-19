package library

import (
	"strconv"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// pageWindow 是页码按钮最多显示几个（渲染层 PAGE_WINDOW）。
const pageWindow = 5

// buildPageWindow 是以当前页为中心的页码窗口（渲染层 buildPageWindow）：总页数不超过窗口时全列，
// 否则当前页居中（前两页后两页），贴着两端时顶住。
func buildPageWindow(current, total int) []int {
	size := min(pageWindow, total)
	start := 1
	if total > pageWindow {
		start = min(max(current-2, 1), total-pageWindow+1)
	}
	pages := make([]int, size)
	for i := range pages {
		pages[i] = start + i
	}
	return pages
}

// totalPages 是总页数（至少 1 页）。
func totalPages(list ListView) int {
	return max(1, (list.Total+list.PageSize-1)/list.PageSize)
}

// pager 是列表底部的分页条（渲染层 ItemPagination）：
// flex shrink-0 items-center justify-between gap-3 border-t border-border px-3 py-2 text-xs text-muted-foreground。
//
// 分页在服务端做，卡片与列表两种视图共用同一份状态。这里不再显示条目总数（顶部工具条已有一份）；
// 页码按钮只在全宽表格（wide）里摆——七颗按钮要 220px，窄栏放不下，改用「‹ 1 / 2 ›」，窄栏被拖宽也保持紧凑。
// 没有条目时整条不画。
func (s *Screen) pager(c ui.C, list ListView, wide bool) {
	if list.Total == 0 {
		return
	}
	l := s.Look
	t := l.T
	pages := totalPages(list)
	ui.Row(c.Key("pager")).FillWidth().Shrink(0).Items(ui.Center).Gap(l.Space(3)).
		PadSides(l.Space(2)+1, l.Space(3), l.Space(2), l.Space(3)).
		Draw(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeTop, 1, t.Border) }).
		Children(func() {
			// min-w-0 items-center gap-2：「每页」与下拉；放不下时截断标签，而不是让整条折行。
			ui.Row(c.Key("size")).Grow(1).MinWidth(0).Items(ui.Center).Gap(l.Space(2)).Children(func() {
				l.Text(c.Key("label"), locales.LibraryPageSize(), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Ellipsis()).
					Shrink(1).MinWidth(0)
				s.pageSizeSelect(c, list)
			})
			if pages > 1 {
				s.pageButtons(c, list, pages, wide)
			}
		})
}

// pageSizeSelect 是「每页」下拉：触发钮是 flex h-7 min-w-[56px] rounded-md border border-border bg-muted px-2 text-xs
// text-foreground hover:bg-accent（Select 的 triggerClassName 覆盖），面板最小宽 88。
func (s *Screen) pageSizeSelect(c ui.C, list ListView) {
	l := s.Look
	t := l.T
	options := make([]kit.SelectOption, len(PageSizeOptions))
	for i, size := range PageSizeOptions {
		text := strconv.Itoa(size)
		options[i] = kit.SelectOption{Value: text, Label: text}
	}
	trigger := kit.SelectTriggerStyle{
		Height: l.Rem(1.75), MinWidth: 56, PadX: l.Space(2), Gap: l.Space(1), Radius: l.Rem(0.75) - 2,
		BorderW: 1, Border: t.Border, Bg: t.Muted, BgHover: t.Accent,
		FontPx: l.Rem(0.75), LinePx: l.Rem(1), Transition: kit.DurationDefault,
	}
	picked, changed := l.Select(c.Key("page-size"), &s.pane.pageSize, kit.SelectSpec{
		Label: locales.LibraryPageSize(), Value: strconv.Itoa(list.PageSize), Options: options,
		MenuMinWidth: 88, Trigger: &trigger,
	})
	if changed {
		if size, err := strconv.Atoi(picked); err == nil {
			s.Store.SetPageSize(s.Ctx, size)
		}
	}
}

// pageButtons 是右边的翻页：上一页、页码（窄栏是「当前 / 总数」，宽表格是最多五颗页码按钮）、下一页。
func (s *Screen) pageButtons(c ui.C, list ListView, pages int, wide bool) {
	l := s.Look
	t := l.T
	page := list.Page
	// rounded-md p-1.5 transition-colors hover:bg-accent disabled:cursor-not-allowed disabled:opacity-40；颜色继承 muted-foreground。
	arrow := kit.ButtonStyle{
		PadX: l.Space(1.5), PadY: l.Space(1.5), Radius: l.Rem(0.75) - 2, Fg: t.MutedForeground, BgHover: t.Accent,
		Transition: kit.DurationDefault, DisabledOpacity: 0.4,
	}
	ui.Row(c.Key("pages")).Shrink(0).Items(ui.Center).Gap(l.Space(1)).Children(func() {
		prev := l.Button(c.Key("prev"), kit.ButtonSpec{
			Name: locales.LibraryPreviousPage(), Icon: icons.ChevronLeft, IconSize: l.Rem(1), Disabled: page == 1,
		}, arrow)
		if prev.Clicked() {
			s.Store.SetPage(s.Ctx, page-1)
		}
		if wide {
			s.pageNumbers(c, list, pages)
		} else {
			// px-1 tabular-nums text-foreground：JSX 的 {page} / {totalPages} 是三个相邻的文本节点，浏览器按节点各给一个文字矩形。
			ui.Row(c.Key("current")).Items(ui.Center).PadSides(0, l.Space(1), 0, l.Space(1)).Children(func() {
				l.Text(c.Key("page"), strconv.Itoa(page), kit.TextXS, kit.WeightNormal, t.Foreground)
				l.Text(c.Key("of"), " / ", kit.TextXS, kit.WeightNormal, t.Foreground)
				l.Text(c.Key("total"), strconv.Itoa(pages), kit.TextXS, kit.WeightNormal, t.Foreground)
			})
		}
		next := l.Button(c.Key("next"), kit.ButtonSpec{
			Name: locales.LibraryNextPage(), Icon: icons.ChevronRight, IconSize: l.Rem(1), Disabled: page == pages,
		}, arrow)
		if next.Clicked() {
			s.Store.SetPage(s.Ctx, page+1)
		}
	})
}

// pageNumbers 是宽表格里的页码按钮：h-7 w-7 rounded-md transition-colors，当前页 bg-primary text-primary-foreground，其余悬停 bg-accent。
func (s *Screen) pageNumbers(c ui.C, list ListView, pages int) {
	l := s.Look
	t := l.T
	style := kit.ButtonStyle{
		Height: l.Rem(1.75), Width: l.Rem(1.75), Radius: l.Rem(0.75) - 2, Text: kit.TextXS, Weight: kit.WeightNormal,
		Fg: t.MutedForeground, BgHover: t.Accent, FgOn: t.PrimaryForeground, BgOn: t.Primary, Transition: kit.DurationDefault,
	}
	for _, candidate := range buildPageWindow(list.Page, pages) {
		text := strconv.Itoa(candidate)
		b := l.Button(c.Key("page-"+text), kit.ButtonSpec{
			Label: text, Name: locales.LibraryPageNumber(candidate), Toggle: true, On: candidate == list.Page, Center: true,
		}, style)
		if b.Clicked() {
			s.Store.SetPage(s.Ctx, candidate)
		}
	}
}
