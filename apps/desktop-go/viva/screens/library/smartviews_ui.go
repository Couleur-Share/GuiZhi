package library

import (
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// smartViews 是智能视图分区的界面状态（渲染层 SmartViewsSection 的 views、isCreating、name）。
type smartViews struct {
	views []SavedView
	modal kit.ModalState
	open  bool
	name  ui.Field // 取消不清空：与渲染层一致，再次打开时还是上次输入的文字
}

// reset 在模块出现时重新读取（渲染层每次挂载都从存储里读）。弹窗关着，输入框清空。
func (m *smartViews) reset(views []SavedView) {
	*m = smartViews{views: views}
}

// smartViewsSection 是「智能视图」分区：本机保存的结构化筛选，不保存转瞬即逝的搜索关键词。
// 标题的按钮打开「保存智能视图」弹窗；每个视图一行，行尾的「更多」按钮直接删除（不是菜单）。
func (s *Screen) smartViewsSection(c ui.C, v View) {
	m := &s.smart
	s.sectionHeading(c, "smart", locales.LibrarySmartViews(), func(c ui.C) {
		if s.headingButton(c, icons.BookmarkPlus, locales.LibrarySmartViewSaveCurrent(), 0.8).Clicked() {
			m.open = true
		}
	})
	l := s.Look
	for _, view := range m.views {
		s.sidebarRow(c, rowSpec{
			Key:    "smart:" + view.ID,
			Icon:   func(c ui.C, fg tokens.Color) { l.Icon(c, icons.BookmarkPlus, l.Rem(1), fg) },
			Label:  view.Name,
			Active: view.Matches(v.Filters),
			More:   locales.LibrarySmartViewDelete(),
			OnClick: func() {
				s.Store.ApplyFilters(s.Ctx, view.Filters)
			},
			OnMore: func(moreEvent) { s.removeSmartView(view.ID) },
		})
	}
}

// removeSmartView 删除一个视图（渲染层 remove）：立即生效，没有确认也没有撤销。
// 写入失败时界面照常更新（渲染层把写入错误吞掉了），另外弹出提示让用户知道没存上（台账 C-005）。
func (s *Screen) removeSmartView(id string) {
	m := &s.smart
	next := make([]SavedView, 0, len(m.views))
	for _, view := range m.views {
		if view.ID != id {
			next = append(next, view)
		}
	}
	s.persistSmartViews(next)
}

// persistSmartViews 写入并更新界面上的列表。
func (s *Screen) persistSmartViews(next []SavedView) {
	if err := WriteSavedViews(s.Prefs, next); err != nil {
		s.Guard.Report(locales.LibrarySmartViewSaveTitle(), err)
	}
	s.smart.views = next
}

// saveSmartView 把当前筛选存成一个视图放在最前面（渲染层 saveCurrent）。名称为空什么都不做。
// 界面上的列表取完整的 next（可能超过 30 条），存储里只留 30 条，与渲染层一致。
func (s *Screen) saveSmartView(f Filters) {
	m := &s.smart
	view, ok := NewSavedView(m.name.Text(), f, s.nowTime(), s.newID)
	if !ok {
		return
	}
	s.persistSmartViews(append([]SavedView{view}, m.views...))
	m.name.SetText("")
	m.open = false
}

// smartDialogView 声明「保存智能视图」弹窗（渲染层 Modal size="sm"）：一个输入框，取消与保存两枚按钮。
func (s *Screen) smartDialogView(c ui.C, v View) {
	l := s.Look
	m := &s.smart
	closed := l.Modal(c.Key("smart-dialog"), &m.modal, kit.ModalSpec{
		Open: m.open, Size: kit.ModalSM, Title: locales.LibrarySmartViewSaveTitle(), CloseLabel: locales.CommonClose(),
		Body: func(c ui.C) {
			ui.Column(c.Key("form")).FillWidth().Gap(l.Space(4)).Children(func() {
				input := l.FieldInput(c.Key("name"), &m.name, kit.FieldSpec{Placeholder: locales.LibrarySmartViewNamePlaceholder(), AutoFocus: true})
				if input.Submitted() {
					s.saveSmartView(v.Filters)
				}
				cancel, save := s.formButtons(c, formButtons{
					Confirm: locales.CommonSave(), PadX: l.Space(3), Plain: true,
					ConfirmDisabled: jsTrim(m.name.Text()) == "",
				})
				if cancel {
					m.open = false
				}
				if save {
					s.saveSmartView(v.Filters)
				}
			})
		},
	})
	if closed {
		m.open = false
	}
}

// formButtons 描述弹窗底部的取消与确认按钮。
type formButtons struct {
	Confirm         string
	ConfirmDisabled bool
	PadX            float32
	// Plain 为真时是渲染层没写 transition、没写 font-medium 与悬停色的那一版（智能视图弹窗）；
	// 否则是带 transition-colors、font-medium、悬停色的那一版（新建与重命名弹窗）。
	Plain bool
}

// formButtons 声明 flex justify-end gap-2 的按钮行，返回取消与确认是否被点击。
//
// 取消：h-9 rounded-lg border border-border text-sm hover:bg-muted/60；确认：h-9 rounded-lg bg-primary text-sm text-primary-foreground
// disabled:opacity-50。完整版另有 inline-flex items-center、text-foreground、transition-colors、font-medium 与 hover:bg-primary/90。
func (s *Screen) formButtons(c ui.C, spec formButtons) (cancel, confirm bool) {
	l := s.Look
	t := l.T
	height, radius := l.Rem(2.25), l.Rem(0.75)
	transition, weight := kit.DurationDefault, kit.WeightMedium
	hoverPrimary := t.Primary.WithAlpha(0.9)
	if spec.Plain {
		transition, weight, hoverPrimary = kit.DurationNone, kit.WeightNormal, tokens.Color{}
	}
	ui.Row(c.Key("buttons")).FillWidth().Justify(ui.End).Gap(l.Space(2)).Children(func() {
		cancel = l.Button(c.Key("cancel"), kit.ButtonSpec{Label: locales.CommonCancel()}, kit.ButtonStyle{
			Height: height, PadX: spec.PadX, Radius: radius, Text: kit.TextSM, Weight: kit.WeightNormal,
			Fg: t.Foreground, Border: t.Border, BorderW: 1, BgHover: t.Muted.WithAlpha(0.6), Transition: transition,
		}).Clicked()
		confirm = l.Button(c.Key("confirm"), kit.ButtonSpec{Label: spec.Confirm, Disabled: spec.ConfirmDisabled}, kit.ButtonStyle{
			Height: height, PadX: spec.PadX, Radius: radius, Text: kit.TextSM, Weight: weight,
			Fg: t.PrimaryForeground, Bg: t.Primary, BgHover: hoverPrimary, Transition: transition,
		}).Clicked()
	})
	return cancel, confirm
}
