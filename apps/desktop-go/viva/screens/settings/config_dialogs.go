package settings

import (
	"errors"
	"time"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// 配置迁移的两个对话框（渲染层 ConfigExportDialog、ConfigImportDialog）：页眉由 Modal 画，内容区自己排——
// 可滚动的正文（px-6 py-4，space-y-4）加固定在底部的页脚（border-t，px-6 py-3，按钮右对齐）。

// asWrongPassword 判断错误链里有没有「密码不对」。
func asWrongPassword(err error, target **services.ConfigWrongPasswordError) bool {
	return errors.As(err, target)
}

// succeedWithDetail 弹一条带详情的成功提示（详情为空时就是普通成功提示）；没有提示通道时什么也不做。
func succeedWithDetail(g kit.Guard, message, detail string) {
	if g.Notify == nil {
		return
	}
	if detail == "" {
		g.Notify.Success(message)
		return
	}
	g.Notify.Success(message, kit.WithDetail(detail))
}

// dialogBody 排对话框内容区：正文在可滚动区里，页脚固定在底部。
// 滚动容器自己不报内容高度，要设上限：面板最高 85vh，扣掉页眉与页脚（约 10rem）剩下的给正文，内容矮时就是内容的高度。
func dialogBody(l *kit.Look, c ui.C, body, footer func()) {
	maxBody := max(c.Bounds().Height*0.85-l.Rem(10), l.Rem(8))
	ui.Column(c).FillWidth().Grow(1).Shrink(1).MinHeight(0).Children(func() {
		l.ScrollArea(c.Key("body"), [4]float32{l.Space(4), l.Space(6), l.Space(4), l.Space(6)}, func() {
			ui.Column(c.Key("stack")).FillWidth().Gap(l.Space(4)).Children(body)
		}).FillWidth().Grow(1).Shrink(1).MinHeight(0).MaxHeight(maxBody)
		ui.Row(c.Key("footer")).FillWidth().Shrink(0).Items(ui.Center).Justify(ui.End).Gap(l.Space(2)).
			PadSides(l.Space(3)+1, l.Space(6), l.Space(3), l.Space(6)).
			Draw(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeTop, 1, l.T.Border) }).
			Children(footer)
	})
}

// boxed 声明一个圆角边框框：rounded-lg border border-border/60，内边距由调用方给。
func boxed(l *kit.Look, c ui.C, bg, border tokens.Color, pad [4]float32) ui.Box {
	return ui.Column(c).FillWidth().PadSides(pad[0], pad[1], pad[2], pad[3]).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: bg, Border: border, BorderW: 1, Radius: l.Rem(0.75)})
		})
}

// toggleRow 是「文字加开关」一行：flex items-center justify-between gap-3 py-1，文字 text-xs muted。
func toggleRow(l *kit.Look, c ui.C, on *bool, label string) {
	ui.Row(c).FillWidth().Items(ui.Center).Justify(ui.Between).Gap(l.Space(3)).PadSides(l.Space(1), 0, l.Space(1), 0).Children(func() {
		l.Text(c.Key("label"), label, kit.TextXS, kit.WeightNormal, l.T.MutedForeground, kit.Wrapped()).Grow(1).Shrink(1)
		l.Switch(c.Key("switch"), on, label)
	})
}

// ── 导出 ──

func (s *ConfigTransferSection) exportDialog(c ui.C) {
	l := s.Look
	e := &s.exp
	closed := l.Modal(c, &e.modal, kit.ModalSpec{
		Open: e.open, Size: kit.ModalLG, CloseLabel: locales.CommonClose(), RawBody: true,
		Title:        locales.SettingsConfigExportTitle(),
		SubtitleText: locales.SettingsConfigExportSubtitle(),
		Subtitle: func(c ui.C) {
			l.Text(c, locales.SettingsConfigExportSubtitle(), kit.TextXS, kit.WeightNormal, l.T.MutedForeground, kit.Wrapped())
		},
		Body: func(c ui.C) { dialogBody(l, c, func() { s.exportFields(c) }, func() { s.exportFooter(c) }) },
	})
	if closed && s.busy != xferExport {
		s.closeExport()
	}
}

func (s *ConfigTransferSection) exportFields(c ui.C) {
	l := s.Look
	t := l.T
	e := &s.exp
	pw, conf := e.password.Text(), e.confirm.Text()
	n := jsLen(pw)

	// 包含 API Key
	boxed(l, c.Key("keys"), t.Muted.WithAlpha(0.2), t.Border.WithAlpha(0.6), [4]float32{l.Space(3), l.Space(4), l.Space(3), l.Space(4)}).Children(func() {
		ui.Row(c.Key("keys-row")).FillWidth().Gap(l.Space(4)).Justify(ui.Between).Children(func() {
			ui.Column(c.Key("keys-text")).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				ui.Row(c.Key("keys-title")).Items(ui.Center).Gap(l.Space(1.5)).Children(func() {
					l.Icon(c.Key("icon"), icons.KeyRound, l.Rem(1), t.Primary)
					l.Text(c.Key("label"), locales.SettingsConfigExportIncludeKeys(), kit.TextSM, kit.WeightMedium, t.Foreground)
				})
				l.Text(c.Key("desc"), locales.SettingsConfigExportIncludeKeysDesc(), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped()).MarginTop(l.Space(1))
			})
			l.Switch(c.Key("switch"), &e.secrets, locales.SettingsConfigExportIncludeKeys())
		})
	})

	if e.secrets {
		ui.Column(c.Key("secrets")).FillWidth().Gap(l.Space(3)).Children(func() {
			l.Text(c.Key("hint"), locales.SettingsConfigExportPasswordHint(), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
			l.SettingsPasswordInput(c.Key("password"), &e.password, &e.showPassword, kit.SettingsInputSpec{
				Name: locales.SettingsConfigExportPassword(), Placeholder: locales.SettingsConfigExportPasswordPlaceholder(minExportPassword),
			}, locales.CommonShowPassword(), locales.CommonHidePassword())
			l.SettingsPasswordInput(c.Key("confirm"), &e.confirm, &e.showConf, kit.SettingsInputSpec{
				Name: locales.SettingsConfigExportPasswordConfirm(), Placeholder: locales.SettingsConfigExportPasswordConfirm(),
			}, locales.CommonShowPassword(), locales.CommonHidePassword())
			if n > 0 && n < minExportPassword {
				l.Text(c.Key("short"), locales.SettingsConfigExportPasswordTooShort(minExportPassword), kit.TextXS, kit.WeightNormal, t.Destructive, kit.Wrapped())
			}
			if conf != "" && pw != conf {
				l.Text(c.Key("mismatch"), locales.SettingsConfigExportPasswordMismatch(), kit.TextXS, kit.WeightNormal, t.Destructive, kit.Wrapped())
			}
		})
	}

	// 选择要同步的偏好
	boxed(l, c.Key("optional"), tokens.Color{}, t.Border.WithAlpha(0.6), [4]float32{l.Space(3), l.Space(4), l.Space(3), l.Space(4)}).Children(func() {
		ui.Column(c.Key("optional-col")).FillWidth().Gap(l.Space(2)).Children(func() {
			l.Text(c.Key("title"), locales.SettingsConfigExportOptional(), kit.TextSM, kit.WeightMedium, t.Foreground)
			toggleRow(l, c.Key("layout"), &e.layout, locales.SettingsConfigExportLayout())
			toggleRow(l, c.Key("shortcuts"), &e.shortcuts, locales.SettingsConfigExportShortcuts())
			toggleRow(l, c.Key("mcp"), &e.mcp, locales.SettingsConfigExportMcpScope())
		})
	})
	l.Text(c.Key("scope"), locales.SettingsConfigExportScopeNote(), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
}

func (s *ConfigTransferSection) exportFooter(c ui.C) {
	l := s.Look
	e := &s.exp
	n := jsLen(e.password.Text())
	canExport := !e.secrets || (n >= minExportPassword && e.password.Text() == e.confirm.Text())
	cancel := kit.ButtonSpec{Label: locales.CommonCancel()}
	if btn(l, c.Key("cancel"), cancel, dialogGhostBtn(l, shape(l, 2.25, 3, 1.5, kit.TextSM, kit.WeightNormal)), false) && s.busy != xferExport {
		s.closeExport()
	}
	ok := kit.ButtonSpec{Label: locales.SettingsConfigExportConfirm(), Disabled: !canExport || s.busy != xferIdle}
	st := primaryBtn(l, shape(l, 2.25, 3, 1.5, kit.TextSM, kit.WeightMedium))
	st.DisabledOpacity = 0.6
	if btn(l, c.Key("confirm"), ok, st, s.busy == xferExport) {
		s.exportConfig()
	}
}

// ── 导入 ──

func (s *ConfigTransferSection) importDialog(c ui.C) {
	l := s.Look
	i := &s.imp
	spec := kit.ModalSpec{
		Open: i.pending != nil, Size: kit.ModalLG, CloseLabel: locales.CommonClose(), RawBody: true,
		Title: locales.SettingsConfigImportTitle(),
	}
	if i.pending != nil {
		p := i.pending.preview
		sub := locales.SettingsConfigImportSubtitle(formatExportedAt(p.ExportedAt), versionText(p.AppVersion))
		spec.SubtitleText = sub
		spec.Subtitle = func(c ui.C) { l.Text(c, sub, kit.TextXS, kit.WeightNormal, l.T.MutedForeground, kit.Wrapped()) }
	}
	spec.Body = func(c ui.C) { dialogBody(l, c, func() { s.importFields(c) }, func() { s.importFooter(c) }) }
	if l.Modal(c, &i.modal, spec) && s.busy != xferApply {
		s.cancelImport()
	}
}

// formatExportedAt 是文件里的导出时间；缺失或解析不了时是「未知时间」。
func formatExportedAt(value string) string {
	if at, err := time.Parse(time.RFC3339, value); err == nil {
		return formatDateTime(at.UnixMilli(), false)
	}
	return locales.SettingsConfigImportUnknownTime()
}

func versionText(v string) string {
	if v == "" {
		return "—"
	}
	return v
}

// describeMCPScope 是 MCP 范围三态：「文件未包含」必须单独说——它与「全部可见」的结果天差地别（一个不动本机设置、一个把范围放开）。
func describeMCPScope(scope *services.ConfigMCPScope) string {
	switch {
	case scope == nil:
		return locales.SettingsConfigImportRowMcpAbsent()
	case scope.Mode == "all":
		return locales.SettingsConfigImportRowMcpAll()
	}
	base := locales.SettingsConfigImportRowMcpSelected(scope.CollectionCount)
	if scope.AllowUncategorized {
		return locales.SettingsConfigImportRowMcpWithUncategorized(base)
	}
	return base
}

func (s *ConfigTransferSection) importFields(c ui.C) {
	l := s.Look
	t := l.T
	i := &s.imp
	p := i.pending.preview

	keys := locales.SettingsConfigImportRowKeysAbsent()
	if p.Encrypted {
		keys = locales.SettingsConfigImportRowKeysEncrypted()
	}
	rows := [][2]string{
		{locales.SettingsConfigImportRowEndpoints(), locales.SettingsConfigImportRowEndpointsValue(p.ProviderCount, p.ModelCount)},
		{locales.SettingsConfigImportRowRoutes(), locales.SettingsConfigImportRowCount(p.RouteCount)},
		{locales.SettingsConfigImportRowShortcuts(), locales.SettingsConfigImportRowCount(p.ShortcutCount)},
		{locales.SettingsConfigImportRowMcp(), describeMCPScope(p.MCPScope)},
		{locales.SettingsConfigImportRowKeys(), keys},
	}

	// 文件内容摘要
	boxed(l, c.Key("summary"), tokens.Color{}, t.Border.WithAlpha(0.6), [4]float32{}).Children(func() {
		for n, row := range rows {
			last := n == len(rows)-1
			bottom := l.Space(2.5)
			if !last {
				bottom++
			}
			ui.Row(c.Key("row-"+row[0])).FillWidth().Items(ui.Center).Justify(ui.Between).Gap(l.Space(3)).
				PadSides(l.Space(2.5), l.Space(4), bottom, l.Space(4)).
				DrawOver(func(d *ui.DrawContext) {
					if !last {
						l.PaintEdge(d, kit.EdgeBottom, 1, t.Border.WithAlpha(0.5))
					}
				}).
				Children(func() {
					l.Text(c.Key("label"), row[0], kit.TextSM, kit.WeightNormal, t.MutedForeground)
					l.Text(c.Key("value"), row[1], kit.TextSM, kit.WeightMedium, t.Foreground, kit.Wrapped())
				})
		}
	})

	// 加密密码
	if p.Encrypted {
		ui.Column(c.Key("password")).FillWidth().Gap(l.Space(2)).Children(func() {
			ui.Row(c.Key("password-title")).Items(ui.Center).Gap(l.Space(1.5)).Children(func() {
				l.Icon(c.Key("icon"), icons.Lock, l.Rem(1), t.Primary)
				l.Text(c.Key("label"), locales.SettingsConfigImportPassword(), kit.TextSM, kit.WeightMedium, t.Foreground)
			})
			l.SettingsPasswordInput(c.Key("input"), &i.password, &i.showPassword, kit.SettingsInputSpec{
				Name: locales.SettingsConfigImportPassword(), Placeholder: locales.SettingsConfigImportPasswordPlaceholder(),
			}, locales.CommonShowPassword(), locales.CommonHidePassword())
			if i.passwordError != "" {
				l.Text(c.Key("error"), i.passwordError, kit.TextXS, kit.WeightNormal, t.Destructive, kit.Wrapped())
			}
		})
	}

	// 选择要应用的内容
	boxed(l, c.Key("selection"), tokens.Color{}, t.Border.WithAlpha(0.6), [4]float32{l.Space(3), l.Space(4), l.Space(3), l.Space(4)}).Children(func() {
		ui.Column(c.Key("selection-col")).FillWidth().Gap(l.Space(1)).Children(func() {
			l.Text(c.Key("title"), locales.SettingsConfigImportSelection(), kit.TextSM, kit.WeightMedium, t.Foreground).MarginTop(0)
			toggleRow(l, c.Key("settings"), &i.sel.Settings, locales.SettingsConfigImportSelectionSettings())
			toggleRow(l, c.Key("layout"), &i.sel.UILayout, locales.SettingsConfigImportSelectionLayout())
			toggleRow(l, c.Key("shortcuts"), &i.sel.Shortcuts, locales.SettingsConfigImportSelectionShortcuts())
			toggleRow(l, c.Key("mcp"), &i.sel.MCPScope, locales.SettingsConfigImportSelectionMcp())
		})
	})

	// 覆盖警告
	boxed(l, c.Key("warning"), t.Destructive.WithAlpha(0.05), t.Destructive.WithAlpha(0.4), [4]float32{l.Space(3), l.Space(4), l.Space(3), l.Space(4)}).Children(func() {
		ui.Row(c.Key("warning-row")).FillWidth().Gap(l.Space(2)).Children(func() {
			l.Icon(c.Key("icon"), icons.TriangleAlert, l.Rem(1), t.Destructive).Shrink(0).MarginTop(l.Space(0.5))
			l.Text(c.Key("text"), locales.SettingsConfigImportWarning(), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped()).Grow(1).Shrink(1)
		})
	})
}

func (s *ConfigTransferSection) importFooter(c ui.C) {
	l := s.Look
	i := &s.imp
	cancel := kit.ButtonSpec{Label: locales.CommonCancel()}
	if btn(l, c.Key("cancel"), cancel, dialogGhostBtn(l, shape(l, 2.25, 3, 1.5, kit.TextSM, kit.WeightNormal)), false) && s.busy != xferApply {
		s.cancelImport()
		return
	}
	canConfirm := s.busy == xferIdle && (!i.pending.preview.Encrypted || i.password.Text() != "")
	ok := kit.ButtonSpec{Label: locales.SettingsConfigImportConfirm(), Disabled: !canConfirm}
	st := destructiveBtn(l, shape(l, 2.25, 3, 1.5, kit.TextSM, kit.WeightMedium))
	st.DisabledOpacity = 0.6
	if btn(l, c.Key("confirm"), ok, st, s.busy == xferApply) {
		s.applyImport()
	}
}
