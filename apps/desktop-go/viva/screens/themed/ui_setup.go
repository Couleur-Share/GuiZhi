package themed

import (
	"strconv"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// 生成设置对话框（ThemedReadingSetup.tsx）：生成 / 重新生成 / 调整已有页面的选项。表单的值与判断在 SetupForm（setup.go），这里只画。

// setupUI 是对话框的界面状态。表单在对话框打开的那一帧按当前读取结果重建，关闭即丢弃。
type setupUI struct {
	modal      kit.ModalState
	opened     bool
	form       SetupForm
	style      ui.Field
	styleFocus bool
	modelsOpen bool
	action     kit.SelectState
	depth      kit.SelectState
	limit      kit.SelectState
}

// setupDialog 每一帧都要经过（Modal 靠它复位）：Setup 为 SetupNone 时什么也不画。
func (s *Screen) setupDialog(c ui.C) {
	l := s.Look
	u := &s.setup
	mode := s.view.Setup
	open := s.attached && mode != SetupNone
	if open && !u.opened {
		u.form = s.Reading.NewSetupForm(mode)
		u.style.SetText(u.form.Style)
		u.modelsOpen = false
	}
	u.opened = open

	title := locales.ThemedReadingReaderCreate()
	if mode == SetupAdjust {
		title = locales.ThemedReadingReaderAdjust()
	}
	closed := l.Modal(c, &u.modal, kit.ModalSpec{
		Open: open, Size: kit.ModalLG, Title: title, CloseLabel: locales.CommonClose(),
		SubtitleText: s.subjectLine(),
		Subtitle:     s.subjectView,
		Body:         s.setupBody,
	})
	if closed {
		s.Reading.CloseSetup()
	}
}

// subjectLine 是对话框副标题：文章标题 · 正文 / 讨论总结。
func (s *Screen) subjectLine() string {
	kind := locales.LibraryBodySection()
	if s.view.SourceKind == "summary" {
		kind = locales.LibraryForumSummarySection()
	}
	if s.sub.Title == "" {
		return kind
	}
	return s.sub.Title + " · " + kind
}

// subjectView 画副标题：渲染层是 {title} ·{" "}{kind} 三个相邻的文字节点（title、" ·"、kind，中间一个空格），
// 对照按文字节点比较，所以这里也分成三段，段间的空格用一个空格的宽度补。
func (s *Screen) subjectView(c ui.C) {
	l := s.Look
	kind := locales.LibraryBodySection()
	if s.view.SourceKind == "summary" {
		kind = locales.LibraryForumSummarySection()
	}
	if s.sub.Title == "" {
		l.Text(c, kind, kit.TextXS, kit.WeightNormal, l.T.MutedForeground, kit.Wrapped())
		return
	}
	size := l.Rem(kit.TextXS.Size)
	space := c.Canvas().TextWidthWeight(" ", size, kit.WeightNormal)
	part := func(key, text string) {
		l.Text(c.Key(key), text, kit.TextXS, kit.WeightNormal, l.T.MutedForeground, kit.Wrapped()).Shrink(0)
	}
	ui.Flow(c).FillWidth().Children(func() {
		part("title", s.sub.Title)
		part("dot", " ·")
		ui.Row(c.Key("gap")).Width(space).Shrink(0).Children(func() {})
		part("kind", kind)
	})
}

func (s *Screen) setupBody(c ui.C) {
	l := s.Look
	t := l.T
	u := &s.setup
	f := u.form
	ui.Column(c).FillWidth().Gap(l.Space(5)).Children(func() {
		s.relaxedText(c.Key("hint"), locales.ThemedReadingReaderSetupHint(), kit.TextSM, t.MutedForeground)

		if f.Adjusting {
			if v, ok := l.Select(c.Key("action"), &u.action, kit.SelectSpec{
				Label: locales.ThemedReadingReaderAdjustMode(), Value: f.Action, Width: -1,
				Options: []kit.SelectOption{
					{Value: services.ThemedActionRedesign, Label: locales.ThemedReadingReaderAdjustDesign()},
					{Value: services.ThemedActionRevise, Label: locales.ThemedReadingReaderAdjustContent()},
					{Value: services.ThemedActionRefresh, Label: locales.ThemedReadingReaderAdjustRefresh()},
				},
			}); ok {
				f = f.WithAction(v)
			}
		}
		if f.ShowsResearch() {
			f = s.researchBlock(c.Key("research"), f)
		}

		on := f.EnhancedInteraction
		l.Checkbox(c.Key("enhanced"), &on, kit.CheckboxSpec{Label: locales.ThemedReadingReaderEnhancedInteraction()})
		f = f.WithEnhancedInteraction(on)

		f = s.styleField(c.Key("style"), f)
		f = s.imageBlock(c.Key("images"), f)
		s.modelsDisclosure(c.Key("models"), f)
		if f.NeedsConfigHint() {
			s.configHint(c.Key("config"), f)
		}
		s.setupFooter(c.Key("footer"), f)
	})
	u.form = f
}

// researchBlock 是联网补充资料：勾选、说明、搜索服务缺失的提醒（带「前往配置」）、查证深度。
func (s *Screen) researchBlock(c ui.C, f SetupForm) SetupForm {
	l := s.Look
	t := l.T
	u := &s.setup
	ui.Column(c).FillWidth().Gap(l.Space(2)).Children(func() {
		on := f.Research
		l.Checkbox(c.Key("check"), &on, kit.CheckboxSpec{Label: locales.ThemedReadingReaderResearchOption()})
		f = f.WithResearch(on)
		hint := locales.ThemedReadingReaderOfflineHint()
		if f.Research {
			hint = locales.ThemedReadingReaderResearchHint()
		}
		s.relaxedText(c.Key("hint"), hint, kit.TextXS, t.MutedForeground)
		if f.ResearchMissingSearch() {
			ui.Flow(c.Key("missing")).FillWidth().Items(ui.Center).Gap(l.Space(2)).Children(func() {
				l.Text(c.Key("text"), locales.ThemedReadingReaderSearchMissing(), kit.TextSM, kit.WeightNormal,
					tokens.Tailwind("amber-600"), kit.Wrapped())
				if s.textLink(c.Key("link"), locales.ArticleAskConfigure(), kit.TextSM) {
					s.openSettingsAI()
				}
			})
		}
		if f.Research {
			if v, ok := l.Select(c.Key("depth"), &u.depth, kit.SelectSpec{
				Label: locales.ThemedReadingReaderResearchDepth(), Value: f.ResearchDepth, Width: -1,
				Options: []kit.SelectOption{
					{Value: services.ThemedDepthStandard, Label: locales.ThemedReadingReaderResearchStandard()},
					{Value: services.ThemedDepthDeep, Label: locales.ThemedReadingReaderResearchDeep()},
				},
			}); ok {
				f = f.WithResearchDepth(v)
			}
		}
	})
	return f
}

// styleField 是设计要求：多行输入，超出上限的部分截掉。
func (s *Screen) styleField(c ui.C, f SetupForm) SetupForm {
	l := s.Look
	t := l.T
	u := &s.setup
	// <label className="block space-y-2 text-sm">：textarea 是 inline-block，沿基线放在行盒里，行盒底部多出一段行内下沿（小、中、大字号档实测 4.5、5、5.5px，即 1px + 0.25rem）。
	ui.Column(c).FillWidth().Gap(l.Space(2)).PadSides(0, 0, l.Space(1)+1, 0).Children(func() {
		l.Text(c.Key("label"), locales.ThemedReadingStyle(), kit.TextSM, kit.WeightNormal, t.Foreground)
		height := 3*l.Rem(1.25) + 2*l.Space(2) + 2 // rows=3，text-sm 行高 20px
		ui.Stack(c.Key("box")).FillWidth().Height(height).Children(func() {
			sid := ui.ID(c.Key("surface"))
			p := l.HoverProgress(c, sid, 0, u.styleFocus, kit.DurationDefault)
			border := kit.MixToken(t.Border, t.Primary, p)
			ui.Row(c.Key("surface-row")).Absolute().Inset(0).Draw(func(d *ui.DrawContext) {
				l.PaintSurface(d, kit.Surface{Bg: t.Background, Border: border, BorderW: 1, Radius: l.Rem(0.75)})
			})
			box := l.TextArea(c.Key("input"), &u.style, locales.ThemedReadingStyle(), locales.ThemedReadingStylePlaceholder(), kit.TextAreaSpec{
				Height: height, PadX: l.Space(3) + 1, PadY: l.Space(2) + 1, Color: t.Foreground,
				Placeholder: uaPlaceholder, PlaceholderLeading: l.Rem(1.25),
			}).Absolute().Inset(0)
			u.styleFocus = box.State().Focused
			if box.Changed() {
				f = f.WithStyle(u.style.Text())
				if f.Style != u.style.Text() {
					u.style.SetText(f.Style)
				}
			}
		})
	})
	return f
}

// imageBlock 是主题图片：开关（没有生图模型时不可选）、数量上限、费用说明。
func (s *Screen) imageBlock(c ui.C, f SetupForm) SetupForm {
	l := s.Look
	t := l.T
	u := &s.setup
	// rounded-xl border border-border p-4，内部 space-y-3。
	s.cardWith(c, l.Space(4)+1, l.Space(3), func() {
		on := f.GenerateImages
		l.Checkbox(c.Key("check"), &on, kit.CheckboxSpec{
			Label: locales.ThemedReadingGenerateImages(), Disabled: f.Models.Image == nil && !f.GenerateImages,
		})
		f = f.WithGenerateImages(on)
		if f.GenerateImages {
			ui.Row(c.Key("limit")).FillWidth().Items(ui.Center).Justify(ui.Between).Gap(l.Space(3)).Children(func() {
				l.Text(c.Key("label"), locales.ThemedReadingImageLimit(), kit.TextXS, kit.WeightNormal, t.MutedForeground)
				opts := make([]kit.SelectOption, 0, services.ThemedMaxImages)
				for n := 1; n <= services.ThemedMaxImages; n++ {
					opts = append(opts, kit.SelectOption{Value: strconv.Itoa(n), Label: locales.ThemedReadingImageCount(n)})
				}
				if v, ok := l.Select(c.Key("select"), &u.limit, kit.SelectSpec{
					Label: locales.ThemedReadingImageLimit(), Value: strconv.Itoa(f.MaxImages), Options: opts, Width: l.Rem(6),
					MenuMinWidth: 96, AlignEnd: true,
				}); ok {
					if n, err := strconv.Atoi(v); err == nil {
						f = f.WithMaxImages(n)
					}
				}
			})
		}
		cost := locales.ThemedReadingNoImageCost()
		if f.GenerateImages {
			cost = locales.ThemedReadingImageCost()
		}
		s.relaxedText(c.Key("cost"), cost, kit.TextXS, t.MutedForeground)
	})
	return f
}

// modelsDisclosure 是折叠的「查看使用的模型」。
func (s *Screen) modelsDisclosure(c ui.C, f SetupForm) {
	l := s.Look
	t := l.T
	u := &s.setup
	ui.Column(c).FillWidth().Gap(l.Space(1)).Children(func() {
		id := ui.ID(c.Key("toggle-id"))
		// <summary className="cursor-pointer py-1 hover:text-foreground">：标记是浏览器自带的 disclosure 三角（行内，不是图标），文字让出它的宽度。
		marker := kit.ChromeDisclosureMarker(l.Rem(kit.TextXS.Size), l.Rem(kit.TextXS.Line), l.Scale)
		padY := l.Space(1)
		row := ui.Row(c.Key("toggle")).FillWidth().Items(ui.Center).PadSides(padY, 0, padY, marker.Advance)
		box, ph := s.hitDisclosure(c, id, row, locales.ThemedReadingReaderModels(), u.modelsOpen)
		ink := kit.MixToken(t.MutedForeground, t.Foreground, ph)
		box.Draw(func(d *ui.DrawContext) {
			if d.State.FocusVisible {
				l.PaintRing(d, 2, t.Primary.WithAlpha(0.5), l.Rem(0.25))
			}
			line := d.Rect
			line.Y += padY
			line.Height -= 2 * padY
			kit.PaintDisclosure(d, line, marker, u.modelsOpen, ink)
		}).Children(func() {
			l.Text(c.Key("label"), locales.ThemedReadingReaderModels(), kit.TextXS, kit.WeightNormal, ink)
		})
		if box.Clicked() {
			u.modelsOpen = !u.modelsOpen
		}
		if !u.modelsOpen {
			return
		}
		name := func(m *string) string {
			if m == nil {
				return locales.ThemedReadingNotConfigured()
			}
			return *m
		}
		s.smallText(c.Key("text"), locales.ThemedReadingTextModel()+": "+name(f.Models.Text))
		s.smallText(c.Key("image"), locales.ThemedReadingImageModel()+": "+name(f.Models.Image))
	})
}

// configHint 是缺模型或缺搜索服务时的提示条：已能提交（只缺生图模型）说「仍可仅排版」，否则说「请先配置」。
func (s *Screen) configHint(c ui.C, f SetupForm) {
	l := s.Look
	t := l.T
	msg := locales.ThemedReadingConfigureHint()
	if f.ConfigHintImageOptional() {
		msg = locales.ThemedReadingImageOptionalHint()
	}
	ui.Row(c).FillWidth().Items(ui.Center).Gap(l.Space(3)).Pad(l.Space(3)).Draw(func(d *ui.DrawContext) {
		l.PaintSurface(d, kit.Surface{Bg: t.Muted, Radius: l.Rem(0.5)})
	}).Children(func() {
		ui.Row(c.Key("text")).Grow(1).MinWidth(0).Children(func() {
			l.Text(c.Key("msg"), msg, kit.TextSM, kit.WeightNormal, t.Foreground, kit.Wrapped())
		})
		if l.Button(c.Key("configure"), kit.ButtonSpec{Label: locales.ThemedReadingConfigure()}, s.compactButton()).Clicked() {
			s.Reading.CloseSetup()
			s.openSettingsAI()
		}
	})
}

// setupFooter 是取消与生成。生成在后台启动（要先保存未落盘的编辑），启动成功对话框才关闭。
func (s *Screen) setupFooter(c ui.C, f SetupForm) {
	l := s.Look
	busy := s.view.Busy
	ui.Row(c).FillWidth().Justify(ui.End).Gap(l.Space(2)).Children(func() {
		if l.Button(c.Key("cancel"), kit.ButtonSpec{Label: locales.CommonCancel()}, s.compactButton()).Clicked() {
			s.Reading.CloseSetup()
		}
		label := locales.ThemedReadingGenerate()
		if busy {
			label = locales.ThemedReadingStarting()
		}
		spec := kit.ButtonSpec{Label: label, Disabled: busy || !f.Configured(), Busy: busy}
		if l.Button(c.Key("generate"), spec, s.compactPrimary()).Clicked() {
			form := f
			s.spawn(func() { s.Reading.SubmitSetup(form) })
		}
	})
}

// openSettingsAI 请外壳去设置页的 AI 分区。
func (s *Screen) openSettingsAI() {
	if s.Hooks.OpenSettingsAI != nil {
		s.Hooks.OpenSettingsAI()
	}
}

// uaPlaceholder 是Tailwind 预检样式给 ::placeholder 的颜色（gray-400，#9CA3AF）：设计要求的 <textarea> 没有 placeholder: 类，用的就是这个，深浅主题下都不变。
var uaPlaceholder = tokens.RGBA(156, 163, 175, 1)
