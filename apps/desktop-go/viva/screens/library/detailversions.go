package library

import (
	"strings"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// 网页条目的来源版本（渲染层 WebSourceVersions）：每次重新采集到的原文都留一个版本，面板里可以和当前正文并排比较，
// 并把某个版本采用为正文。头部是一枚展开钮，下面是版本面板（展开着或读取失败才有）。
// 网页快照窗格（版本带快照时的整页预览，台账 B-066）：原生只有一座阅读岛，所以版本面板里是「查看网页快照」按钮，
// 点了把阅读区切到这个版本的快照（detailsnapshot.go）；「围绕此版本提问」属于问答（M3）。

// versionsUI 是来源版本的界面状态：展开与否、选中的版本（空串是没选）与它的下拉；
// paneW 是并排两栏里一栏上一帧的宽度（等宽原文按它折行，见 versionPane）；
// chipW 是展开钮里文字盒子上一帧的宽度（放不下时文字按它折行，见 versionsChip）。
type versionsUI struct {
	open     bool
	selected string
	sel      kit.SelectState
	paneW    float32
	chipW    float32
}

const (
	// versionPaneMaxHeightRem 是原文窗格的最高处：max-h-80。
	versionPaneMaxHeightRem float32 = 20
	// versionGridBreakpoint 是两栏并排的窗口宽度：lg:grid-cols-2（Tailwind lg = 1024px，按窗口宽而不是面板宽）。
	versionGridBreakpoint float32 = 1024
)

// toolsVersions 声明「采集的来源版本」一块：border-t border-border/60 pt-4，标题（mb-3）、展开钮、版本面板。
func (s *Screen) toolsVersions(c ui.C, v View, item services.LibraryItem) {
	l := s.Look
	t := l.T
	vu := &s.detail.tools.versions
	vv := v.Detail.Tools.Versions
	s.toolsSection(c.Key("versions")).Children(func() {
		l.Heading(c.Key("title"), 3, locales.ArticleReaderSourceVersions(), kit.TextSM, kit.WeightMedium, t.Foreground).MarginBottom(l.Space(3))
		s.versionsChip(c.Key("chip"), vv, vu)
		if vu.open || vv.Error != "" {
			s.versionsPanel(c.Key("panel"), item, vv, vu)
		}
	})
}

// versionsChip 是展开钮：动作按钮的外观（ACTION_CHIP），展开着时边框、底与字都是 primary；
// 文字是「原文版本（N）」，有待比较的新版本接「 · 有新版本可比较」，摘要过期再接「· 摘要需重新整理」（译文自带前导圆点，不加空格）；末尾 ChevronDown，展开时转 180°。
//
// 它是 inline-flex 的按钮：外框不超过容器宽度（max-w-full），里面三项（图标、文字、图标）各自 flex-shrink: 1，文字项的基准尺寸是不折行的整行宽度。
// 英文的整行（两项状态都有时约 450px）比面板宽，三项就按基准尺寸成比例收窄（两枚图标各收窄约四分之一），文字在收窄后的盒子里折成两行、
// 每行居中（button 的 text-align: center），超出 28px 的行高上下各一点（居中对齐，不裁剪）。
func (s *Screen) versionsChip(c ui.C, vv VersionsView, vu *versionsUI) {
	l := s.Look
	t := l.T
	// 渲染层是三个相邻的文本节点（计数、「 · 有新版本可比较」、「· 摘要需重新整理」），文字行也按节点各记一条，所以分三段画（kit.TextFlowCentered）。
	runs := []string{locales.WebCaptureVersionsCount(len(vv.Versions))}
	if vv.Baseline != nil && vv.Baseline.PendingVersion {
		runs = append(runs, " · "+locales.WebCaptureVersionReady())
	}
	if vv.Baseline != nil && vv.Baseline.SummaryStale {
		runs = append(runs, locales.WebCaptureSummaryStale())
	}
	label := strings.Join(runs, "")
	whole := c.Canvas().TextWidthWeight(label, l.Rem(kit.TextXS.Size), kit.WeightNormal) // 文字项的基准尺寸：不折行的整行宽度
	spec := inlineSpec{
		Name: label, Disclosure: true, Open: vu.open, Gap: l.Space(1.5), MaxFill: true,
		Height: l.Rem(1.75), PadX: l.Space(2.5) + 1, Radius: l.Rem(0.75),
		Fg: t.MutedForeground, FgHover: t.Primary, BgHover: t.Primary.WithAlpha(0.05),
		Border: t.Border.WithAlpha(0.7), BorderHover: t.Primary.WithAlpha(0.5), BorderW: 1,
	}
	if vu.open {
		spec.Fg, spec.Bg, spec.Border = t.Primary, t.Primary.WithAlpha(0.05), t.Primary.WithAlpha(0.5)
		spec.FgHover, spec.BgHover, spec.BorderHover = tokens.Color{}, tokens.Color{}, tokens.Color{}
	}
	box := s.inlineButton(c, spec, func(fg tokens.Color) {
		l.Icon(c.Key("icon"), icons.History, l.Rem(0.875), fg).Shrink(1)
		ui.Column(c.Key("label")).Width(whole).Shrink(1).MinWidth(0).Items(ui.Center).
			Draw(func(d *ui.DrawContext) {
				if w := d.Rect.Width; w > 0 && abs32(w-vu.chipW) > 0.01 {
					vu.chipW = w
					d.Canvas.RequestFrame()
				}
			}).
			Children(func() {
				// 盒子不比整行窄就不折行（避免量宽的浮点误差把最后一个词挤到下一行）；首帧还没量到宽度，也按一行排。
				wrapW := vu.chipW
				if wrapW >= whole-0.01 {
					wrapW = 0
				}
				l.TextFlowCentered(c.Key("flow"), kit.TextXS, kit.WeightNormal, fg, wrapW, runs...)
			})
		chevronID := ui.ID(c.Key("chevron-id")) // 键只作用于紧接着的一次声明：先取 ID，再声明图标
		l.RotatingChevron(c.Key("chevron"), chevronID, l.Rem(0.875), vu.open, fg, kit.DurationDefault).Shrink(1)
	}).Self(ui.Start)
	if box.Clicked() {
		vu.open = !vu.open
		if vu.open {
			s.Store.Spawn(func() { s.Store.LoadWebVersions(s.Ctx) })
		}
	}
}

// versionsPanel 是版本面板：w-full min-w-0 space-y-3 rounded-lg border border-border p-3。
// 读取失败时最上面是错误态（带重试）；展开着时依次是说明、版本下拉、（选了版本时）并排的两栏原文、提问入口与「采用所选正文版本」。
func (s *Screen) versionsPanel(c ui.C, item services.LibraryItem, vv VersionsView, vu *versionsUI) {
	l := s.Look
	t := l.T
	var selected *services.LibraryWebVersion
	for i := range vv.Versions {
		if vv.Versions[i].ID == vu.selected {
			selected = &vv.Versions[i]
			break
		}
	}
	ui.Column(c).FillWidth().MinWidth(0).Gap(l.Space(3)).Pad(1 + l.Space(3)).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Border: t.Border, BorderW: 1, Radius: l.Rem(0.75)})
		}).
		Children(func() {
			if vv.Error != "" {
				if l.LoadErrorState(c.Key("load-error"), vv.Error) {
					s.Store.Spawn(func() { s.Store.LoadWebVersions(s.Ctx) })
				}
			}
			if !vu.open {
				return
			}
			l.Text(c.Key("hint"), locales.WebCaptureAdoptHint(), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
			options := make([]kit.SelectOption, len(vv.Versions))
			for i, version := range vv.Versions {
				options[i] = kit.SelectOption{Value: version.ID, Label: s.versionLabel(version)}
			}
			if picked, changed := l.Select(c.Key("versions"), &vu.sel, kit.SelectSpec{
				Label: locales.WebCaptureVersions(), Value: vu.selected, Options: options, Width: -1, Placeholder: locales.CommonSelect(),
			}); changed {
				vu.selected = picked
			}
			if selected == nil {
				return
			}
			s.versionGrid(c.Key("grid"), vv, *selected, vu)
			if selected.HasSnapshot() && s.derivedOf(item).caps.IsWechat {
				// 渲染层把快照窗格嵌在版本面板里；原生只有一座阅读岛，所以这里是切换阅读区去看这个版本的快照（收起工具侧栏）。
				if s.versionButton(c.Key("snapshot"), locales.LibrarySnapshotViewOriginal(), false) {
					s.showVersionSnapshot(selected.ID)
				}
			}
			if selected.HasSnapshot() && item.DeletedAt == 0 {
				ask := locales.ArticleAskAskVersion()
				if s.versionButton(c.Key("ask"), ask, false) {
					s.notMigrated(ask, "M3", "B-060")
				}
			}
			if s.versionButton(c.Key("adopt"), locales.WebCaptureAdoptSelected(), !selected.Complete || item.DeletedAt != 0) {
				id := selected.ID
				s.Store.Spawn(func() { s.Store.AdoptWebVersion(s.Ctx, id) })
			}
		})
}

// versionLabel 是下拉里一个版本的文字：采集时间 · 本地快照（或引擎版本），不完整的版本再接「· 不完整」（译文自带前导圆点）。
func (s *Screen) versionLabel(version services.LibraryWebVersion) string {
	kind := version.EngineVersion
	if version.Kind == services.WebVersionLocal {
		kind = locales.WebCaptureLocalSnapshot()
	}
	label := kit.FormatDateTime(version.CapturedAt, s.loc(), s.Locale) + " · " + kind
	if !version.Complete {
		label += locales.WebCaptureIncomplete()
	}
	return label
}

// versionButton 是面板里的小按钮（采用、围绕此版本提问）：rounded-md border border-border px-3 py-1 text-sm，没有悬停样式，禁用时 opacity-50。
func (s *Screen) versionButton(c ui.C, label string, disabled bool) bool {
	l := s.Look
	t := l.T
	return l.Button(c, kit.ButtonSpec{Label: label, Disabled: disabled}, kit.ButtonStyle{
		PadX: l.Space(3), PadY: l.Space(1), Radius: l.Rem(0.75) - 2, Text: kit.TextSM, Weight: kit.WeightNormal,
		Fg: t.Foreground, Border: t.Border, BorderW: 1, Transition: kit.DurationNone, DisabledOpacity: 0.5, Surface: t.Background,
	}).Self(ui.Start).Clicked()
}

// versionGrid 是并排比较：grid gap-3 lg:grid-cols-2。窗口够宽（1024）时左右两栏，否则上下叠放；
// 左边是「当前正文」（比较时读到的正文），右边是「所选版本 ·」加版本标题与该版本的 Markdown。
func (s *Screen) versionGrid(c ui.C, vv VersionsView, version services.LibraryWebVersion, vu *versionsUI) {
	l := s.Look
	current := ""
	if vv.Baseline != nil {
		current = vv.Baseline.Content
	}
	panes := func(wide bool) {
		// 标题是两个相邻的文本节点（「所选版本 ·」与版本标题）：后一段接在前一段之后排、放不下再换行（kit.TextFlow）。
		s.versionPane(c.Key("current"), []string{locales.WebCaptureCurrentContent()}, current, wide, vu)
		s.versionPane(c.Key("selected"), []string{locales.WebCaptureSelectedVersion(), version.Title}, version.Markdown, wide, vu)
	}
	if s.detail.viewportW >= versionGridBreakpoint {
		ui.Row(c).FillWidth().Gap(l.Space(3)).Children(func() { panes(true) })
		return
	}
	ui.Column(c).FillWidth().Gap(l.Space(3)).Children(func() { panes(false) })
}

// versionPane 是一栏：标题（text-xs font-medium）与等宽原文（pre：max-h-80 overflow-auto whitespace-pre-wrap break-words text-xs）。
// 原文的折行按渲染层的规则自己做（kit.WrapMono），宽度取上一帧量到的栏宽；长到超过最高处就竖向滚动，滚动条占去的宽度要重新折。
func (s *Screen) versionPane(c ui.C, title []string, text string, wide bool, vu *versionsUI) {
	l := s.Look
	t := l.T
	col := ui.Column(c)
	if wide {
		col = col.Grow(1).Shrink(1).Basis(0).MinWidth(0)
	} else {
		col = col.FillWidth()
	}
	col.Draw(func(d *ui.DrawContext) {
		if w := d.Rect.Width; w > 0 && abs32(w-vu.paneW) > 0.01 {
			vu.paneW = w
			d.Canvas.RequestFrame()
		}
	}).Children(func() {
		l.TextFlow(c.Key("title"), kit.TextXS, kit.WeightMedium, t.Foreground, vu.paneW, title...)
		size, line := l.Rem(0.75), l.Rem(1) // text-xs：0.75rem、行高 1rem，随字号档缩放
		maxH := l.Rem(versionPaneMaxHeightRem)
		lines := kit.WrapMono(text, vu.paneW, size)
		scrolls := float32(len(lines))*line > maxH+0.01
		if scrolls {
			lines = kit.WrapMono(text, vu.paneW-8, size)
		}
		pre := func() {
			// 每行已按渲染层的度量折好，一行一个 Label 画（汉字行用界面字体，见 kit.MonoParagraph）
			l.MonoParagraph(c.Key("pre"), lines, size, line, t.Foreground)
		}
		if scrolls {
			l.ScrollArea(c.Key("pre-scroll"), [4]float32{}, pre).Height(maxH)
		} else {
			ui.Column(c.Key("pre-box")).FillWidth().Children(pre)
		}
	})
}
