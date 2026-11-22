package settings

import (
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// scopePicker 是「MCP 能看见哪些知识库」（渲染层 McpScopePicker）：两张模式卡（全部合集 / 指定合集）；
// 指定时下面是一列可勾选的行（「未归合集」加各合集），选中态直接显示在原位。
// 没做成左右搬运的双栏：知识库通常十来个，双栏只是把「点一下」变成「找到它再点一下」。
func (m *MCP) scopePicker(c ui.C) {
	l := m.Look
	selected := m.scope.Mode == services.MCPScopeSelected
	ui.Column(c).FillWidth().Gap(l.Space(3)).Children(func() {
		ui.Row(c.Key("modes")).FillWidth().Gap(l.Space(2)).Children(func() {
			if m.modeCard(c.Key("all"), locales.SettingsMcpScopeAll(), locales.SettingsMcpScopeAllDesc(), !selected) {
				next := m.scope.Clone()
				next.Mode = services.MCPScopeAll
				m.applyScope(next)
			}
			if m.modeCard(c.Key("selected"), locales.SettingsMcpScopeSelected(), locales.SettingsMcpScopeSelectedDesc(), selected) {
				next := m.scope.Clone()
				next.Mode = services.MCPScopeSelected
				m.applyScope(next)
			}
		})
		if !selected {
			return
		}
		ui.Column(c.Key("rows")).FillWidth().Gap(l.Space(1.5)).Children(func() {
			if m.scopeRow(c.Key("uncategorized"), nil, locales.SettingsMcpScopeUncategorized(), locales.SettingsMcpScopeUncategorizedHint(), m.scope.AllowUncategorized) {
				next := m.scope.Clone()
				next.AllowUncategorized = !next.AllowUncategorized
				m.applyScope(next)
			}
			for _, col := range m.collection {
				on := m.scope.HasCollection(col.ID)
				if m.scopeRow(c.Key("collection-"+col.ID), &col, col.Name, "", on) {
					m.applyScope(m.scope.ToggleCollection(col.ID))
				}
			}
			if m.scope.IsEmpty() {
				amber := tokens.Tailwind("amber-600")
				if l.Dark {
					amber = tokens.Tailwind("amber-400")
				}
				l.Text(c.Key("empty"), locales.SettingsMcpScopeEmptyWarning(), kit.TextXS, kit.WeightNormal, amber, kit.Wrapped()).MarginTop(l.Space(1))
			}
		})
	})
}

// modeCard 是一张模式卡：flex-1 rounded-xl border px-4 py-3；选中 border-primary/60 bg-primary/[0.07]，否则 border-border/70 悬停 bg-accent/40。
func (m *MCP) modeCard(c ui.C, label, desc string, active bool) bool {
	l := m.Look
	t := l.T
	id := ui.ID(c)
	box := ui.Column(c).Grow(1).Basis(0).MinWidth(0).Pad(l.Space(4)+1).
		PadSides(l.Space(3)+1, l.Space(4)+1, l.Space(3)+1, l.Space(4)+1).
		// 渲染层的模式卡是普通按钮（没有 aria-pressed）：名字是两行文字拼起来（label + 说明），选中只靠边框与底色表达。
		HitWith(id, label+" "+desc, gfx.HitOptions{Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand})
	l.TabStop(box)
	hovered := box.State().Hovered
	surface := kit.Surface{Border: t.Border.WithAlpha(0.7), BorderW: 1, Radius: l.Rem(0.75)}
	switch {
	case active:
		surface.Bg, surface.Border = t.Primary.WithAlpha(0.07), t.Primary.WithAlpha(0.6)
	case hovered:
		surface.Bg = t.Accent.WithAlpha(0.4)
	}
	box.Draw(func(d *ui.DrawContext) { l.PaintSurface(d, surface) }).Children(func() {
		l.Text(c.Key("label"), label, kit.TextSM, kit.WeightMedium, t.Foreground)
		l.Text(c.Key("desc"), desc, kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped()).MarginTop(l.Space(0.5))
	})
	return box.Clicked()
}

// scopeRow 是一行可勾选的合集：w-full gap-2.5 rounded-lg border px-3 py-2 text-sm；选中 border-primary/50 bg-primary/[0.07] text-foreground，
// 否则 border-border/60 text-muted-foreground，悬停 border-border bg-accent/40。col 非空时图标取合集的 emoji（空则文件夹图标），nil 是「未归合集」的收件箱图标。
func (m *MCP) scopeRow(c ui.C, col *services.LibraryCollection, label, hint string, on bool) bool {
	l := m.Look
	t := l.T
	id := ui.ID(c)
	box := ui.Row(c).FillWidth().Items(ui.Center).Gap(l.Space(2.5)).
		PadSides(l.Space(2)+1, l.Space(3)+1, l.Space(2)+1, l.Space(3)+1).
		HitWith(id, strings.TrimSpace(label+" "+hint), gfx.HitOptions{Role: gfx.RoleButton, Selected: on, Cursor: gfx.PointerCursorHand})
	l.TabStop(box)
	hovered := box.State().Hovered
	surface := kit.Surface{Border: t.Border.WithAlpha(0.6), BorderW: 1, Radius: l.Rem(0.75)}
	ink := t.MutedForeground
	switch {
	case on:
		surface.Bg, surface.Border, ink = t.Primary.WithAlpha(0.07), t.Primary.WithAlpha(0.5), t.Foreground
	case hovered:
		surface.Bg, surface.Border = t.Accent.WithAlpha(0.4), t.Border
	}
	box.Draw(func(d *ui.DrawContext) { l.PaintSurface(d, surface) }).Children(func() {
		// 勾选框 h-4 w-4 rounded border：选中是 primary 底加对勾。
		ui.Row(c.Key("check")).Width(l.Rem(1)).Height(l.Rem(1)).Shrink(0).Items(ui.Center).Justify(ui.Center).
			Draw(func(d *ui.DrawContext) {
				s := kit.Surface{Border: t.Border, BorderW: 1, Radius: l.Rem(0.25)}
				if on {
					s = kit.Surface{Bg: t.Primary, Border: t.Primary, BorderW: 1, Radius: l.Rem(0.25)}
				}
				l.PaintSurface(d, s)
			}).Children(func() {
			if on {
				l.Icon(c.Key("tick"), icons.Check, l.Rem(0.75), t.PrimaryForeground)
			}
		})
		switch {
		case col == nil:
			l.Icon(c.Key("icon"), icons.Inbox, l.Rem(0.875), t.MutedForeground).Shrink(0)
		case strings.TrimSpace(col.Icon) != "":
			l.Text(c.Key("emoji"), strings.TrimSpace(col.Icon), kit.TextSM, kit.WeightNormal, t.MutedForeground).Shrink(0)
		default:
			l.Icon(c.Key("icon"), icons.Folder, l.Rem(0.875), t.MutedForeground).Shrink(0)
		}
		l.Text(c.Key("label"), label, kit.TextSM, kit.WeightNormal, ink, kit.Ellipsis()).Grow(1).MinWidth(0)
		if hint != "" {
			l.Text(c.Key("hint"), hint, kit.TextXS, kit.WeightNormal, t.MutedForeground).Shrink(0)
		}
	})
	return box.Clicked()
}
