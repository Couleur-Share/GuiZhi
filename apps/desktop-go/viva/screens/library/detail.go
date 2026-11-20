package library

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 详情栏（渲染层 components/library/ItemDetail 与 LibraryWorkspace 里挂它的两处）。
//
// 卡片视图里它是列表栏右边那一栏（min-w-0 flex-1 app-wallpaper-section）；专注阅读模式下列表与二级侧栏都隐去，
// 详情占满工作区、限宽居中，右下角浮一枚「退出专注」；表格视图里它在详情浮层里（detailmodal.go）。
// 三处共用同一个 itemDetail：读取失败、读取中、没有选中、有条目四种状态，有条目时是通知条、头部、面板。
//
// 本里程碑（M2-4）只做壳：阅读区（Markdown 预览、楼层卡片正文、网页快照、图库、主题阅读）、编辑器、AI 卡片、文章工具侧栏与
// 提问都不在这里，它们的位置留白或只画入口，入口的动作弹一条说明归属里程碑的失败提示（台账 B-059、B-060）。

// detailUI 是详情栏的界面状态：渲染层 ItemDetail、ItemDetailHeader、ContentPanel 的组件状态，工作区卸载即复位。
type detailUI struct {
	// title 是标题框的文字（受控：每一帧与 Store 里的标题对齐，见 syncTitle），titleItem 是它当前属于哪个条目。
	title     ui.Field
	titleItem string

	// more 与 collection 是头部的两个菜单，各自的触发钮外框（Keep 与锚点用）。
	more           anchoredMenu
	moreRect       gfx.Rect
	collection     anchoredMenu
	collectionRect gfx.Rect

	// focusReading 是专注阅读模式（渲染层 ui.store 的 isFocusReadingMode：不持久化，离开知识库即退出）。
	focusReading bool

	// panel 是内容面板的界面状态（detailpanel.go）；modal 是表格视图的详情浮层（detailmodal.go）。
	panel panelState
	// notice 是头部上方三块通知条的折叠状态（detailnotice.go）。
	notice noticeUI
	modal  detailModal
}

// FocusReading 报告是否处于专注阅读模式：外壳据此隐去二级侧栏（渲染层 App 的 `!(isFocusReadingMode && appModule === "library")`）。
func (s *Screen) FocusReading() bool { return s.detail.focusReading }

// SetFocusReading 进入或退出专注阅读（渲染层 setFocusReadingMode）。
func (s *Screen) SetFocusReading(on bool) {
	if s.detail.focusReading != on {
		s.detail.focusReading = on
		s.remountDetail()
	}
}

// toggleFocusReading 切换专注阅读（头部按钮与 Alt+Z 共用）。
func (s *Screen) toggleFocusReading() { s.SetFocusReading(!s.detail.focusReading) }

// remountDetail 重置详情栏里随挂载而生的界面状态：专注阅读与普通布局里 ItemDetail 在渲染树的不同位置，切换就是卸载重挂，
// 面板的标签（由阅读记忆恢复）、预览开关、查找条与通知条的折叠都回到初始。
func (s *Screen) remountDetail() {
	s.detail.panel = panelState{}
	s.detail.notice = noticeUI{}
	s.detail.titleItem = ""
}

// detailColumn 是卡片视图里的详情栏：min-w-0 flex-1 app-wallpaper-section。
func (s *Screen) detailColumn(c ui.C, v View) {
	l := s.Look
	t := l.T
	ui.Column(c.Key("detail")).Grow(1).MinWidth(0).FillHeight().Draw(func(d *ui.DrawContext) {
		l.PaintSurface(d, kit.Surface{Bg: t.UiSectionBg})
	}).Children(func() {
		if !s.HideDetail {
			s.itemDetail(c, v, nil)
		}
	})
}

// focusWorkspace 是专注阅读模式的工作区：relative flex h-full min-h-0 flex-1 overflow-hidden app-wallpaper-section，
// 里面一栏详情（min-w-0 flex-1），右下角绝对定位一枚「退出专注」。
func (s *Screen) focusWorkspace(c ui.C, v View) {
	l := s.Look
	t := l.T
	ui.Row(c.Key("focus")).FillWidth().Grow(1).MinHeight(0).Clip().Draw(func(d *ui.DrawContext) {
		l.PaintSurface(d, kit.Surface{Bg: t.UiSectionBg})
	}).Children(func() {
		ui.Column(c.Key("focus-detail")).Grow(1).MinWidth(0).FillHeight().Children(func() {
			s.itemDetail(c, v, nil)
		})
		s.exitFocusButton(c)
	})
}

// exitFocusButton 是专注阅读模式右下角的浮钮：absolute bottom-5 right-5 z-20 inline-flex h-10 items-center gap-2 rounded-full
// border border-border bg-background/95 px-4 text-sm font-medium text-foreground shadow-lg backdrop-blur-sm hover:bg-accent。
func (s *Screen) exitFocusButton(c ui.C) {
	l := s.Look
	t := l.T
	style := kit.ButtonStyle{
		Height: l.Rem(2.5), PadX: l.Space(4), Radius: 9999, Gap: l.Space(2), Text: kit.TextSM, Weight: kit.WeightMedium,
		Fg: t.Foreground, Bg: t.Background.WithAlpha(0.95), BgHover: t.Accent,
		Border: t.Border, BorderW: 1, Shadow: t.ShadowLg, Transition: kit.DurationDefault,
	}
	box := l.Button(c.Key("exit-focus"), kit.ButtonSpec{
		Label: locales.LibraryExitFocusReadingShort(), Icon: icons.Minimize2, IconSize: l.Rem(1),
		Name: locales.LibraryExitFocusReading(), Hint: locales.LibraryExitFocusReading(),
	}, style).Absolute().Bottom(l.Space(5)).Right(l.Space(5))
	if box.Clicked() {
		s.SetFocusReading(false)
	}
}

// itemDetail 是条目详情（渲染层 ItemDetail）：读取失败、读取中、没有选中、有条目。onClose 非空时（详情浮层）头部多一枚关闭钮。
func (s *Screen) itemDetail(c ui.C, v View, onClose func()) {
	l := s.Look
	t := l.T
	s.detailShown = true
	det := v.Detail
	switch {
	case det.LoadError != "":
		if l.LoadErrorState(c.Key("load-error"), det.LoadError) {
			s.Store.SelectItem(v.List.SelectedID)
		}
	case det.Loading:
		// role="status" p-6 text-sm text-muted-foreground
		ui.Row(c.Key("loading")).FillWidth().Pad(l.Space(6)).Draw(func(d *ui.DrawContext) {
			d.Canvas.ContainerWithOptions(d.Rect, gfx.RoleStatus, "", gfx.HitOptions{Live: gfx.LivePolite})
		}).Children(func() {
			l.Text(c.Key("loading-text"), locales.LibraryLoadingItem(), kit.TextSM, kit.WeightNormal, t.MutedForeground)
		})
	case det.Item == nil || det.Item.ID != v.List.SelectedID:
		// flex h-full items-center justify-center px-8 text-center text-sm text-muted-foreground
		ui.Row(c.Key("no-selection")).FillWidth().FillHeight().Items(ui.Center).Justify(ui.Center).PadSides(0, l.Space(8), 0, l.Space(8)).Children(func() {
			l.Text(c.Key("no-selection-text"), locales.LibraryNoSelection(), kit.TextSM, kit.WeightNormal, t.MutedForeground, kit.Wrapped(), kit.Centered()).Shrink(1).MinWidth(0)
		})
	default:
		s.itemDetailBody(c, v, *det.Item, onClose)
	}
}

// detailMaxWidth 是专注阅读模式下详情栏的上限（Tailwind max-w-4xl = 56rem）。
const detailMaxWidthRem float32 = 56

// itemDetailBody 是有条目时的详情：relative flex h-full min-h-0 flex-col（专注阅读模式再加 mx-auto w-full max-w-4xl）。
// 子元素依次是草稿冲突通知、来源版本、头部、内容面板（文章工具侧栏与提问栏不在本里程碑）。
func (s *Screen) itemDetailBody(c ui.C, v View, item services.LibraryItem, onClose func()) {
	l := s.Look
	trashed := item.DeletedAt != 0
	col := ui.Column(c.Key("article")).FillHeight().MinHeight(0)
	if s.detail.focusReading {
		col = col.FillWidth().MaxWidth(l.Rem(detailMaxWidthRem)).Self(ui.Center) // mx-auto w-full max-w-4xl
	} else {
		col = col.FillWidth()
	}
	caps := deriveCaps(item)
	s.syncNotices(item.ID)
	col.Children(func() {
		s.conflictNotice(c, v, item)
		s.revisionsNotice(c, v, item)
		s.detailHeader(c, v, item, caps, trashed, onClose)
		s.contentPanel(c, v, item, caps)
	})
}
