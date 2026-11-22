package capture

import (
	"math"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// View 声明对话框（Modal，lg 档）。每一帧都要调用：Modal 在不打开的那一帧复位自己的状态。
func (d *Dialog) View(c ui.C) {
	if d.Look == nil {
		return
	}
	st := d.snapshot()
	closed := d.Look.Modal(c.Key("capture-dialog"), &d.modal, kit.ModalSpec{
		Open: st.open, Size: kit.ModalLG, Title: locales.CaptureTitle(), CloseLabel: locales.CommonClose(),
		Body: func(c ui.C) { d.body(c, st) },
	})
	if closed {
		d.Close()
	}
}

// body 是内容区（space-y-4 的四段）：草稿框、文件列表、标签行、底部操作条。
func (d *Dialog) body(c ui.C, st state) {
	l := d.Look
	draft, action := d.action()
	canSubmit := action.Kind != ActionEmpty || len(st.files) > 0
	ui.Column(c.Key("form")).FillWidth().Gap(l.Space(4)).Children(func() {
		d.draftZone(c, draft, action, canSubmit, st)
		d.fileList(c, st.files)
		d.tagRowView(c, st)
		d.actionBar(c, st, action, canSubmit)
	})
}

// draftZone 是草稿框加状态栏：rounded-xl border transition-colors，拖入文件时 border-primary bg-primary/5，平时 border-border bg-background/60。
// 草稿框 6 行、px-4 py-3；状态栏 border-t border-border/60 px-3 py-2 text-xs。Ctrl/Cmd+Enter 在草稿框里提交（快速采集由全局快捷键唤起，提交也不该被迫回到鼠标）。
func (d *Dialog) draftZone(c ui.C, draft Draft, action Action, canSubmit bool, st state) {
	l := d.Look
	t := l.T
	over := float32(0)
	if d.dragOver {
		over = 1
	}
	p := c.Canvas().Animate(ui.ID(c.Key("drag")), 0, over, l.Motion(kit.DurationDefault, kit.EaseDefault))
	zone := ui.Column(c.Key("zone")).FillWidth().Pad(1).Draw(func(dc *ui.DrawContext) {
		d.zone = dc.Rect
		l.PaintSurface(dc, kit.Surface{
			Bg:     kit.MixToken(t.Background.WithAlpha(0.6), t.Primary.WithAlpha(0.05), p),
			Border: kit.MixToken(t.Border, t.Primary, p), BorderW: 1, Radius: l.Rem(0.75),
		})
	})
	if zone.Shortcut(ui.Primary, "enter") && canSubmit {
		d.submit(action, st)
	}
	zone.Children(func() {
		// 草稿框是 inline-block 的 <textarea>：它下面留着一行盒的基线下沿（父级 16px 字号的 descent，约 6px），状态栏因此不贴着它。
		// 全局规则 `:focus-visible { ring-2 ring-primary/50 }` 对文本框任何方式的聚焦都生效：环画在草稿框外侧（盖住拖放区的 1px 边框）。
		var ring float32
		ui.Column(c.Key("draft-wrap")).FillWidth().MarginBottom(draftBaselineGap(l)).
			Draw(func(dc *ui.DrawContext) { l.PaintInputRing(dc, ring, l.Rem(0.75)) }).
			Children(func() {
				box := l.TextArea(c.Key("draft"), &d.draft, locales.CapturePlaceholder(), locales.CapturePlaceholder(), kit.TextAreaSpec{
					Height: l.Rem(9), PadX: l.Space(4), PadY: draftPadY(l),
					Color: t.Foreground, Placeholder: t.MutedForeground.WithAlpha(0.7), PlaceholderLeading: l.Rem(1.25),
				}).FillWidth().AutoFocus()
				ring = l.InputRing(c, ui.ID(c.Key("draft-ring")), box.State().Focused)
			})
		d.statusBar(c, draft, action)
	})
}

// draftPadY 是草稿框的上下内边距：py-3，再扣掉一行高度差的一半。渲染层 leading-5 的行高是 1.25rem，Viva 多行框的行高是字号加 7（更高），
// 行盒里的文字居中，所以同样的内边距下首行文字会比渲染层偏下半个差值（小档 0.875px，对拍里占位符墨迹偏下 1 像素）；上内边距扣掉它，首行就对齐了。
// 第二行起每行仍比渲染层低 1px（行高本身写死在 Viva 的多行框里，见 dialog_pitch_test.go）。
func draftPadY(l *kit.Look) float32 {
	delta := l.Theme.Metrics.FontSizeM + 7 - l.Rem(1.25)
	return l.Space(3) - max(delta, 0)/2
}

// draftBaselineGap 是草稿框与状态栏之间的间隙（inline-block 的基线下沿）：渲染层实测小 / 中 / 大三档字号分别是 6 / 6 / 7px，
// 对得上 0.375rem（5.25 / 6 / 6.75px）向上取整——Chromium 把行盒的 descent 取整到整像素。三档之外的字号没有实测，取同一规则。
func draftBaselineGap(l *kit.Look) float32 { return float32(math.Ceil(float64(l.Rem(0.375)))) }

// statusBar 是草稿框下面的状态栏：左边是识别结果（链接时 text-primary 加地球图标，文字时剪贴板图标，至多两行），
// 右边在文字夹链接（mixed）时是改判按钮。
func (d *Dialog) statusBar(c ui.C, draft Draft, action Action) {
	l := d.Look
	t := l.T
	ink := t.MutedForeground
	if action.Kind == ActionURLs {
		ink = t.Primary
	}
	ui.Row(c.Key("status")).FillWidth().Items(ui.Center).Gap(l.Space(2)).
		PadSides(1+l.Space(2), l.Space(3), l.Space(2), l.Space(3)).
		Draw(func(dc *ui.DrawContext) { l.PaintEdge(dc, kit.EdgeTop, 1, t.Border.WithAlpha(0.6)) }).
		Children(func() {
			ui.Row(c.Key("hint")).Grow(1).Shrink(1).Basis(0).MinWidth(0).Items(ui.Center).Gap(l.Space(1)).Children(func() {
				switch action.Kind {
				case ActionURLs:
					l.Icon(c.Key("icon"), icons.Globe, l.Rem(0.875), ink).Shrink(0)
				case ActionText:
					l.Icon(c.Key("icon"), icons.ClipboardPaste, l.Rem(0.875), ink).Shrink(0)
				}
				l.Text(c.Key("text"), hintText(draft, action), kit.TextXS, kit.WeightNormal, ink, kit.LineClamp(2)).
					Grow(1).Shrink(1).MinWidth(0)
			})
			if draft.Kind != DraftMixed {
				return
			}
			label := locales.CaptureSwitchToUrl()
			switch {
			case action.Kind == ActionURLs:
				label = locales.CaptureSwitchToText()
			case len(draft.URLs) > 1:
				label = locales.CaptureSwitchToUrls(len(draft.URLs))
			}
			if d.textButton(c.Key("switch"), label, textButton{
				Style: kit.TextXS, Idle: t.MutedForeground, Hover: t.Foreground, Underline: true,
				Insets: [4]float32{l.Space(0.5), l.Space(1.5), l.Space(0.5), l.Space(1.5)}, Radius: l.Rem(0.75) - 2,
			}) {
				if action.Kind == ActionURLs {
					d.override = ActionText
				} else {
					d.override = ActionURLs
				}
			}
		})
}

// hintText 是状态栏的识别结果文字（渲染层 hintText）：从文字里抠出来的链接要显示出来，用户得看得见我们挑的是哪个。
func hintText(draft Draft, action Action) string {
	switch action.Kind {
	case ActionURLs:
		count := len(action.URLs)
		if draft.Kind != DraftMixed {
			if count > 1 {
				return locales.CaptureDetectedUrls(count)
			}
			return locales.CaptureDetectedUrl()
		}
		if count > 1 {
			return locales.CaptureExtractedUrls(count)
		}
		return locales.CaptureExtractedUrl(action.URLs[0])
	case ActionText:
		return locales.CaptureDetectedText()
	}
	return locales.CaptureHint()
}

// textButton 描述一枚纯文字按钮的外观。
type textButton struct {
	Style       kit.TextStyle
	Idle, Hover tokens.Color
	Underline   bool       // 悬停时画下划线
	Insets      [4]float32 // 内边距：上、右、下、左
	Radius      float32    // 聚焦环的圆角
}

// textButton 声明一枚纯文字按钮（transition-colors，悬停换色）：命中区是整个盒子，键盘可达，聚焦时 ring-2 ring-primary/50。返回这一帧是否被点了。
func (d *Dialog) textButton(c ui.C, label string, spec textButton) bool {
	l := d.Look
	t := l.T
	id := ui.ID(c)
	box := ui.Row(c).Shrink(0).Items(ui.Center).
		PadSides(spec.Insets[0], spec.Insets[1], spec.Insets[2], spec.Insets[3]).
		HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand})
	l.TabStop(box)
	hovered := box.State().Hovered
	p := l.HoverProgress(c, id, 0, hovered, kit.DurationDefault)
	ink := kit.MixToken(spec.Idle, spec.Hover, p)
	box.Draw(func(dc *ui.DrawContext) {
		if dc.State.FocusVisible {
			l.PaintRing(dc, 2, t.Primary.WithAlpha(0.5), spec.Radius)
		}
	}).Children(func() {
		l.Text(c.Key("text"), label, spec.Style, kit.WeightNormal, ink, kit.Underline(spec.Underline && hovered))
	})
	return box.Clicked()
}

// fileList 是已选文件的列表：space-y-1 rounded-xl border border-border p-3，每行图标、路径（truncate）与「移除」。
func (d *Dialog) fileList(c ui.C, files []string) {
	if len(files) == 0 {
		return
	}
	l := d.Look
	t := l.T
	ui.Column(c.Key("files")).FillWidth().Gap(l.Space(1)).Pad(l.Space(3) + 1).
		Draw(func(dc *ui.DrawContext) {
			l.PaintSurface(dc, kit.Surface{Border: t.Border, BorderW: 1, Radius: l.Rem(0.75)})
		}).Children(func() {
		for _, path := range files {
			rc := c.Key("file-" + path)
			ui.Row(rc).FillWidth().Items(ui.Center).Gap(l.Space(2)).Children(func() {
				l.Icon(rc.Key("icon"), icons.FileUp, l.Rem(0.875), t.MutedForeground).Shrink(0)
				l.Text(rc.Key("path"), path, kit.TextXS, kit.WeightNormal, t.Foreground, kit.Ellipsis()).Grow(1).Shrink(1).MinWidth(0)
				if d.textButton(rc.Key("remove"), locales.CommonRemove(), textButton{
					Style: kit.TextXS, Idle: t.MutedForeground, Hover: t.Destructive, Radius: l.Rem(0.25),
				}) {
					d.removeFile(path)
				}
			})
		}
	})
}

// actionBar 是底部操作条（flex flex-wrap items-center gap-2）：选择文件、空白笔记、占位、合集下拉、采集。
func (d *Dialog) actionBar(c ui.C, st state, action Action, canSubmit bool) {
	l := d.Look
	t := l.T
	outline := kit.ButtonStyle{
		Height: l.Rem(2.25), PadX: l.Space(3), Radius: l.Rem(0.75), Gap: l.Space(1.5), Text: kit.TextSM, Weight: kit.WeightNormal,
		Fg: t.Foreground, Border: t.Border, BorderW: 1, BgHover: t.Muted.WithAlpha(0.6), Transition: kit.DurationDefault,
	}
	ui.Flow(c.Key("actions")).FillWidth().Items(ui.Center).Gap(l.Space(2)).LineGap(l.Space(2)).Children(func() {
		if l.Button(c.Key("pick"), kit.ButtonSpec{Label: locales.CaptureChooseFiles(), Icon: icons.FileUp, IconSize: l.Rem(1)}, outline).Clicked() {
			d.PickFiles()
		}
		if d.Hooks.BlankNote != nil {
			if l.Button(c.Key("blank"), kit.ButtonSpec{Label: locales.CaptureBlankNote(), Icon: icons.StickyNote, IconSize: l.Rem(1)}, outline).Clicked() {
				collectionID := d.collectionID
				d.Close()
				d.Hooks.BlankNote(collectionID)
			}
		}
		ui.Row(c.Key("spacer")).Grow(1).Shrink(1).Basis(0).MinWidth(0)
		options := []kit.SelectOption{{Value: "", Label: locales.LibraryNoCollection()}}
		for _, col := range st.collections {
			options = append(options, kit.SelectOption{Value: col.ID, Label: col.Name})
		}
		if picked, changed := l.Select(c.Key("collection"), &d.pick, kit.SelectSpec{
			Label: locales.LibraryCollection(), Value: d.collectionID, Options: options, Width: l.Rem(9),
		}); changed {
			d.collectionID = picked
		}
		submit := l.Button(c.Key("submit"), kit.ButtonSpec{
			Label: locales.CaptureSubmit(), Icon: icons.FilePlus, IconSize: l.Rem(1), Disabled: !canSubmit || st.busy, Busy: st.busy,
		}, kit.ButtonStyle{
			Height: l.Rem(2.25), PadX: l.Space(4), Radius: l.Rem(0.75), Gap: l.Space(1.5), Text: kit.TextSM, Weight: kit.WeightMedium,
			Fg: t.PrimaryForeground, Bg: t.Primary, BgHover: t.Primary.WithAlpha(0.9), Transition: kit.DurationDefault, Surface: t.UiPanelStrongBg,
		})
		if submit.Clicked() {
			d.submit(action, st)
		}
	})
}
