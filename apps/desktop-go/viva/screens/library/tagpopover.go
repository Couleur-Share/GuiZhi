package library

import (
	"errors"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// 标签浮层：批量打标签（渲染层 BulkTagPopover）与单条的标签选择（TagPickerPopover）。
// 两者都是 portal 到 body、fixed 定位的 role=dialog 面板：只在点面板外与 Esc 时收起，没有 onBlur 与 resize 监听，
// 位置夹进视口（kit.PopoverSpec.StayOnBlur）；输入框一出现就取得焦点（autoFocus）。

// tagPopup 是批量打标签浮层的界面状态：草稿是组件自己的状态，每次打开都重新挂载，所以每次打开清空。
type tagPopup struct {
	pop   kit.PopoverState
	draft ui.Field
}

func (t *tagPopup) show(at gfx.Point) {
	t.pop.Show(at)
	t.draft.SetText("")
}

// tagPicker 是单条标签选择浮层的界面状态；id 是正在编辑的条目——渲染层每次渲染都从当前页的 entries 里按 id 取，
// 条目不在本页了，浮层随之不画。
type tagPicker struct {
	pop   kit.PopoverState
	id    string
	draft ui.Field
}

// openTagPicker 在条目菜单的位置打开标签选择浮层（渲染层 onEditTags 取菜单的 x、y）。
func (s *Screen) openTagPicker(e services.LibraryEntry) {
	p := &s.bulk.picker
	p.id = e.ID
	p.draft.SetText("")
	p.pop.Show(s.bulk.entry.at)
}

// errAiTagsPending 是「AI 建议标签」在 AI 功能迁入之前的失败原因（台账 B-053）：用户点了就要有交代，不能一声不响。
var errAiTagsPending = errors.New("AI 标签建议尚未迁入原生界面，将随 AI 功能一起迁入；这里仍可手动输入或选择已有标签")

const (
	tagPanelWidth  = 260 // BulkTagPopover 的 PANEL_WIDTH（px 字面量）
	tagPickerWidth = 272 // TagPickerPopover 的 PANEL_WIDTH
	tagViewportGap = 8   // VIEWPORT_MARGIN
)

// bulkTagView 声明批量打标签浮层：fixed rounded-xl border border-border bg-popover p-3 shadow-xl，宽 260。
// 提示、输入加「添加」、选择已有标签（全部标签，不排除任何一个）。输入框回车、点「添加」、点某个已有标签都是提交：
// 去掉首尾空白，空的不提交；提交就给选中的每一条追加这个标签并关闭浮层。
func (s *Screen) bulkTagView(c ui.C, v View) {
	l := s.Look
	tp := &s.bulk.bulkTag
	if !tp.pop.Open {
		return
	}
	count := len(v.List.Selection)
	commit := func(name string) {
		name = jsTrim(name)
		if name == "" {
			return
		}
		s.bulkUpdate(selection(v), services.LibraryEntryPatch{AddTagNames: []string{name}})
		tp.pop.Close()
	}
	l.Popover(c.Key("bulk-tag"), &tp.pop, kit.PopoverSpec{
		Name: locales.LibraryBulkAddTag(), Width: tagPanelWidth, Margin: tagViewportGap, Pad: l.Space(3),
		Radius: l.Rem(0.75), Shadows: kit.ShadowXL, StayOnBlur: true,
		Content: func() {
			// mb-2 text-[11px] text-muted-foreground：px 字面量，行高继承根的 1.5。
			l.TextPx(c.Key("hint"), locales.LibraryBulkAddTagHint(count), 11, 16.5, kit.WeightNormal, l.T.MutedForeground, kit.Wrapped()).
				MarginBottom(l.Space(2))
			if s.tagInputRow(c, &tp.draft) {
				commit(tp.draft.Text())
			}
			s.tagChoices(c, v.Tags, func(tag services.LibraryTag) { commit(tag.Name) })
		},
	})
}

// tagPickerView 声明单条标签选择浮层：宽 272，当前标签、输入加「添加」、选择已有标签、AI 建议四段。
// 每个改动都立刻写回条目（onChange），浮层不关；下一帧条目带着新标签回来，浮层里的内容随之更新。
func (s *Screen) tagPickerView(c ui.C, v View) {
	l := s.Look
	p := &s.bulk.picker
	if !p.pop.Open {
		return
	}
	var entry *services.LibraryEntry
	for i := range v.List.Entries {
		if v.List.Entries[i].ID == p.id {
			entry = &v.List.Entries[i]
			break
		}
	}
	if entry == nil {
		return
	}
	id := entry.ID
	names := make([]string, 0, len(entry.Tags))
	for _, tag := range entry.Tags {
		names = append(names, tag.Name)
	}
	// 已有标签里去掉这条已经带着的（按名称）。
	var unused []services.LibraryTag
	for _, tag := range v.Tags {
		if !containsString(names, tag.Name) {
			unused = append(unused, tag)
		}
	}
	// 追加一个标签：空的、与已有的（不分大小写）重名的忽略；成功不清草稿——清草稿是 commitDraft 的事。
	addTag := func(name string) {
		name = jsTrim(name)
		if name == "" {
			return
		}
		for _, existing := range names {
			if strings.EqualFold(existing, name) {
				return
			}
		}
		s.setItemTags(id, append(append([]string(nil), names...), name))
	}
	l.Popover(c.Key("tag-picker"), &p.pop, kit.PopoverSpec{
		Name: locales.LibraryEditTags(), Width: tagPickerWidth, Margin: tagViewportGap, Pad: l.Space(3),
		Radius: l.Rem(0.75), Shadows: kit.ShadowXL, StayOnBlur: true,
		Content: func() {
			if len(entry.Tags) > 0 {
				// mb-2.5 flex flex-wrap gap-1
				ui.Flow(c.Key("current")).FillWidth().Gap(l.Space(1)).LineGap(l.Space(1)).MarginBottom(l.Space(2.5)).Children(func() {
					for _, tag := range entry.Tags {
						if s.pickerChip(c.Key("chip-"+tag.ID), tag) {
							s.setItemTags(id, without(names, tag.Name))
						}
					}
				})
			}
			if s.tagInputRow(c, &p.draft) {
				addTag(p.draft.Text())
				p.draft.SetText("")
			}
			s.tagChoices(c, unused, func(tag services.LibraryTag) { addTag(tag.Name) })
			s.aiTagSuggest(c)
		},
	})
}

// tagInputRow 是「输入框加添加钮」一行（flex items-center gap-1.5）：输入框 h-7 min-w-0 flex-1 rounded-md border border-border
// bg-background px-2 text-xs，聚焦时边框 border-primary/60；添加钮 h-7 bg-primary px-2 text-xs font-medium，草稿去掉首尾空白
// 为空时禁用（opacity-40）。返回这一帧是否要提交草稿（输入框回车，或点了添加）。
func (s *Screen) tagInputRow(c ui.C, f *ui.Field) (commit bool) {
	l := s.Look
	t := l.T
	height := l.Rem(1.75)
	empty := jsTrim(f.Text()) == ""
	ui.Row(c.Key("input-row")).FillWidth().Items(ui.Center).Gap(l.Space(1.5)).Children(func() {
		placeholder := locales.LibraryTagsNewPlaceholder()
		var input ui.Box
		focused := false // 底板先声明、输入后声明：绘制闭包在绘制时读
		ui.Stack(c.Key("field")).Grow(1).Shrink(1).Basis(0).MinWidth(0).Height(height).Children(func() {
			ui.Row(c.Key("surface")).Absolute().Inset(0).Draw(func(d *ui.DrawContext) {
				border := t.Border
				if focused {
					border = t.Primary.WithAlpha(0.6)
				}
				l.PaintSurface(d, kit.Surface{Bg: t.Background, Border: border, BorderW: 1, Radius: l.Rem(0.75) - 2})
			})
			input = l.Input(c.Key("input"), f, placeholder, placeholder, kit.InputSpec{
				Size: l.Rem(0.75), Weight: kit.WeightNormal, Color: t.Foreground, Placeholder: t.MutedForeground.WithAlpha(0.6),
				Height: height, Left: l.Space(2) + 1, Right: l.Space(2) + 1,
			}).Absolute().Inset(0).AutoFocus()
			focused = input.State().Focused
		})
		if input.Submitted() {
			commit = true
		}
		add := l.Button(c.Key("add"), kit.ButtonSpec{Label: locales.CommonAdd(), Icon: icons.Plus, IconSize: l.Rem(0.75), Disabled: empty}, kit.ButtonStyle{
			Height: height, PadX: l.Space(2), Radius: l.Rem(0.75) - 2, Gap: l.Space(1), Text: kit.TextXS, Weight: kit.WeightMedium,
			Fg: t.PrimaryForeground, Bg: t.Primary, BgHover: t.Primary.WithAlpha(0.9), Transition: kit.DurationDefault, DisabledOpacity: 0.4,
		})
		if add.Clicked() {
			commit = true
		}
	})
	return commit
}

// tagChoices 是「选择已有标签」一段：mt-3 border-t border-border/60 pt-2.5，标题（text-xs font-medium uppercase tracking-wide）
// 在没有可选标签时跟一句淡色提示，否则是一排可点的小块（mt-1.5 flex max-h-28 flex-wrap gap-1 overflow-y-auto）。
func (s *Screen) tagChoices(c ui.C, tags []services.LibraryTag, pick func(services.LibraryTag)) {
	l := s.Look
	t := l.T
	ui.Column(c.Key("existing")).FillWidth().MarginTop(l.Space(3)).PadSides(l.Space(2.5)+1, 0, 0, 0).
		Draw(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeTop, 1, t.Border.WithAlpha(0.6)) }).
		Children(func() {
			// 标题是行内的 span，落在继承根字号（1rem / 1.5rem 行高）的块里：文字盒上沿离行顶 0.5rem−3px，下沿离行底 3px
			// （与列设置菜单的标题同一口径，见 tablecolmenu.go）。
			ui.Column(c.Key("title-line")).FillWidth().PadSides(l.Rem(0.5)-3, 0, 3, 0).Children(func() {
				l.TextTracked(c.Key("title"), strings.ToUpper(locales.LibraryTagsExisting()), kit.TextXS, kit.WeightMedium, t.MutedForeground, 0.025)
			})
			if len(tags) == 0 {
				l.Text(c.Key("empty"), locales.LibraryTagsExistingEmpty(), kit.TextXS, kit.WeightNormal, t.MutedForeground.WithAlpha(0.6), kit.Wrapped()).
					MarginTop(l.Space(1.5))
				return
			}
			l.ScrollAreaView(c.Key("chips"), [4]float32{}, func(kit.ScrollView) {
				ui.Flow(c.Key("flow")).FillWidth().Gap(l.Space(1)).LineGap(l.Space(1)).Children(func() {
					for _, tag := range tags {
						if s.tagChoice(c.Key("tag-"+tag.ID), tag) {
							pick(tag)
						}
					}
				})
			}).FillWidth().MaxHeight(l.Rem(7)).MarginTop(l.Space(1.5))
		})
}

// tagChoice 是一个可选的已有标签：inline-flex items-center gap-1 rounded-full border border-border px-2 py-0.5 text-xs
// text-muted-foreground transition-colors hover:border-primary/50 hover:text-foreground，前面一枚 6px 的色点。返回是否被点击。
func (s *Screen) tagChoice(c ui.C, tag services.LibraryTag) bool {
	l := s.Look
	t := l.T
	id := ui.ID(c)
	box := ui.Row(c).Shrink(0).Items(ui.Center).Gap(l.Space(1)).
		PadSides(l.Space(0.5)+1, l.Space(2)+1, l.Space(0.5)+1, l.Space(2)+1).
		HitWith(id, tag.Name, gfx.HitOptions{Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand})
	l.TabStop(box)
	p := l.HoverProgress(c, id, 0, box.State().Hovered, kit.DurationDefault)
	fg := kit.MixToken(t.MutedForeground, t.Foreground, p)
	border := kit.MixToken(t.Border, t.Primary.WithAlpha(0.5), p)
	box.Draw(func(d *ui.DrawContext) {
		if d.State.FocusVisible {
			l.PaintRingBand(d, 2, t.Primary.WithAlpha(0.5), 9999)
		}
		l.PaintSurface(d, kit.Surface{Border: border, BorderW: 1, Radius: 9999})
	}).Children(func() {
		dot := l.Rem(0.375)
		ui.Row(c.Key("dot")).Size(dot, dot).Shrink(0).Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: tagDotColor(tag.ColorKey), Radius: 9999})
		})
		l.Text(c.Key("name"), tag.Name, kit.TextXS, kit.WeightNormal, fg)
	})
	return box.Clicked()
}

// pickerChip 是当前标签的小块：inline-flex h-6 items-center gap-1 rounded-full px-2.5 text-xs（底色与文字色同表格的标签小块），
// 后面一枚移除钮。返回是否点了移除。
func (s *Screen) pickerChip(c ui.C, tag services.LibraryTag) (remove bool) {
	l := s.Look
	bg, fg := s.tagChipColors(tag.ColorKey)
	ui.Row(c).Shrink(0).Height(l.Rem(1.5)).Items(ui.Center).Gap(l.Space(1)).PadSides(0, l.Space(2.5), 0, l.Space(2.5)).
		Draw(func(d *ui.DrawContext) { l.PaintSurface(d, kit.Surface{Bg: bg, Radius: 9999}) }).
		Children(func() {
			l.Text(c.Key("name"), tag.Name, kit.TextXS, kit.WeightNormal, fg)
			remove = s.chipRemove(c.Key("remove"), tag.Name, fg)
		})
	return remove
}

// chipRemove 是小块里的移除钮：rounded-full opacity-60 transition-opacity hover:opacity-100，里面一枚 h-3 w-3 的 X。
// 渲染层里 svg 是块级（Tailwind 预检样式 svg { display: block }），按钮就是 12×12，在 h-6 的小块里被 items-center 居中
// （实测按钮外框 12×12；早先按「行内 svg 按基线对齐」算成 12×16，台账 L2 外框差 4px）。
func (s *Screen) chipRemove(c ui.C, name string, fg tokens.Color) bool {
	l := s.Look
	t := l.T
	size := l.Rem(0.75)
	id := ui.ID(c)
	label := locales.LibraryRemoveTag(name)
	box := ui.Column(c).Size(size, size).Shrink(0).
		HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand})
	l.TabStop(box)
	p := l.HoverProgress(c, id, 0, box.State().Hovered, kit.DurationDefault)
	ink := fg.WithAlpha(fg.A * (0.6 + 0.4*p))
	box.Draw(func(d *ui.DrawContext) {
		if d.State.FocusVisible {
			l.PaintRingBand(d, 2, t.Primary.WithAlpha(0.5), 9999)
		}
	}).Children(func() {
		l.Icon(c.Key("icon"), icons.X, size, ink)
	})
	return box.Clicked()
}

// aiTagSuggest 是「AI 建议标签」一段：mt-3 border-t border-border/60 pt-2.5，标题与「生成」钮一行（flex items-center
// justify-between gap-2）。生成要调 AI 请求，随 AI 功能一起迁入（台账 B-053）：在那之前点击给出可理解的说明，不假装成功。
func (s *Screen) aiTagSuggest(c ui.C) {
	l := s.Look
	t := l.T
	ui.Column(c.Key("ai")).FillWidth().MarginTop(l.Space(3)).PadSides(l.Space(2.5)+1, 0, 0, 0).
		Draw(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeTop, 1, t.Border.WithAlpha(0.6)) }).
		Children(func() {
			ui.Row(c.Key("head")).FillWidth().Items(ui.Center).Justify(ui.Between).Gap(l.Space(2)).Children(func() {
				l.TextTracked(c.Key("title"), strings.ToUpper(locales.LibraryAiTagsSuggest()), kit.TextXS, kit.WeightMedium, t.MutedForeground, 0.025)
				ui.Row(c.Key("actions")).Items(ui.Center).Gap(l.Space(0.5)).Children(func() {
					// inline-flex h-6 items-center gap-1 rounded-md px-1.5 text-xs text-primary transition-colors hover:bg-primary/10
					gen := l.Button(c.Key("generate"), kit.ButtonSpec{Label: locales.LibraryTagsGenerate(), Icon: icons.Sparkles, IconSize: l.Rem(0.875)}, kit.ButtonStyle{
						Height: l.Rem(1.5), PadX: l.Space(1.5), Radius: l.Rem(0.75) - 2, Gap: l.Space(1), Text: kit.TextXS, Weight: kit.WeightNormal,
						Fg: t.Primary, BgHover: t.Primary.WithAlpha(0.1), Transition: kit.DurationDefault, DisabledOpacity: 0.6,
					})
					if gen.Clicked() {
						s.Guard.Report(locales.LibraryAiTagsSuggest(), errAiTagsPending)
					}
				})
			})
		})
}

// containsString 报告 list 里有没有 s。
func containsString(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

// without 是去掉 name 之后的副本（按名称全等，渲染层 names.filter((n) => n !== name)）。
func without(names []string, name string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		if n != name {
			out = append(out, n)
		}
	}
	return out
}
