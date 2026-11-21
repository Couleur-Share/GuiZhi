package library

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 详情标签行（渲染层 TagEditor）：只摆条目已有的标签，末尾留一枚「+」；新建标签、选已有标签、AI 建议都收在「+」弹出的浮层里
// （TagPickerPopover，与列表右键菜单里的「编辑标签」是同一块面板，见 tagPickerPanel）。每个改动都经 updateSelected 写进详情的草稿。

// tagEditorUI 是标签行的界面状态：浮层每次打开都重新挂载（草稿清空）；add 是「+」钮上一帧的外框（浮层的锚点，点它只收起）。
type tagEditorUI struct {
	pop   kit.PopoverState
	draft ui.Field
	add   gfx.Rect
}

// tagEditor 声明标签行：mt-2 flex flex-wrap items-center gap-1.5，条目的标签（带移除钮）依次排开，最后是「+」。
func (s *Screen) tagEditor(c ui.C, v View, item services.LibraryItem) {
	l := s.Look
	ed := &s.detail.tools.tags
	ui.Flow(c.Key("tag-row")).FillWidth().MarginTop(l.Space(2)).Items(ui.Center).Gap(l.Space(1.5)).LineGap(l.Space(1.5)).Children(func() {
		for _, tag := range item.Tags {
			if s.pickerChip(c.Key("tag-"+tag.ID), tag) {
				s.updateItemTags(withoutTagID(item.Tags, tag.ID))
			}
		}
		s.tagAddButton(c.Key("add"), item, ed)
	})
	if ed.pop.Open {
		s.tagPickerPanel(c, "tag-picker", &ed.pop, &ed.draft, tagPickerData{
			tags: item.Tags, all: v.Tags, apply: s.updateItemTags, trigger: ed.add,
		})
	}
}

// tagAddButton 是「+」钮：inline-flex h-6 items-center gap-1 rounded-full border border-dashed border-border px-2 text-xs
// text-muted-foreground，悬停边框 muted-foreground/60、字 foreground；没有标签时带「添加标签」文字，有标签时是纯图标、悬停出气泡。
// 浮层开着再点它是收起；展开时在钮的左下方 6px 处打开。
func (s *Screen) tagAddButton(c ui.C, item services.LibraryItem, ed *tagEditorUI) {
	l := s.Look
	t := l.T
	label := locales.LibraryAddTag()
	spec := kit.ButtonSpec{Icon: icons.Plus, IconSize: l.Rem(0.75), Name: label, Disclosure: true, On: ed.pop.Open}
	if len(item.Tags) == 0 {
		spec.Label = label
	} else {
		spec.Hint = label
	}
	box := l.Button(c, spec, kit.ButtonStyle{
		Height: l.Rem(1.5), PadX: l.Space(2), Radius: 9999, Gap: l.Space(1), Text: kit.TextXS, Weight: kit.WeightNormal,
		Fg: t.MutedForeground, FgHover: t.Foreground,
		Border: t.Border, BorderHover: t.MutedForeground.WithAlpha(0.6), BorderW: 1, Dashed: true,
		Transition: kit.DurationDefault,
	}).DrawOver(func(d *ui.DrawContext) { ed.add = d.Rect })
	if box.Clicked() {
		if ed.pop.Open {
			ed.pop.Close()
			return
		}
		ed.draft.SetText("")
		r := ed.add
		ed.pop.Show(gfx.Point{X: r.X, Y: r.Y + r.Height + 6})
	}
}

// updateItemTags 把改后的标签名写进详情的草稿（渲染层 updateSelected({ tagNames })）：即时更新，防抖落盘。
func (s *Screen) updateItemTags(names []string) {
	s.Store.UpdateSelected(s.Ctx, services.LibraryDraftFields{TagNames: append([]string{}, names...)})
}

// withoutTagID 返回去掉某个 ID 的标签之后的标签名（渲染层 removeTag：按 ID 过滤，保持原顺序）。
func withoutTagID(tags []services.LibraryTag, id string) []string {
	names := make([]string, 0, len(tags))
	for _, tag := range tags {
		if tag.ID != id {
			names = append(names, tag.Name)
		}
	}
	return names
}
