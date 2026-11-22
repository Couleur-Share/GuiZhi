package settings

import (
	"context"
	"strings"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// copiedFor 是复制按钮显示「已复制」的时长（渲染层 setTimeout 2000）。
const copiedFor = 2 * time.Second

// clientSnippet 是一段可复制的配置（渲染层 McpClientSnippet）。
type clientSnippet struct {
	key, label, text string
}

// clientPreset 是一个 AI 客户端的接入说明（渲染层 client-presets.ts）。
type clientPreset struct {
	id                services.MCPClient
	label             string
	location, pitfall string
	oneClick          bool
	snippets          []clientSnippet
}

func buildClientPresets(cfg services.MCPConfig) []clientPreset {
	return []clientPreset{
		{
			id: services.MCPClientCursor, label: "Cursor", oneClick: true,
			location: locales.SettingsMcpCursorLocation(), pitfall: locales.SettingsMcpCursorPitfall(),
			snippets: []clientSnippet{{"config", locales.SettingsMcpSnippetConfig(), services.BuildCursorConfig(cfg)}},
		},
		{
			id: services.MCPClientCodex, label: "Codex",
			location: locales.SettingsMcpCodexLocation(), pitfall: locales.SettingsMcpCodexPitfall(),
			snippets: []clientSnippet{
				{"command", locales.SettingsMcpSnippetCommand(), services.BuildCodexAddCommand(cfg)},
				{"toml", locales.SettingsMcpSnippetToml(), services.BuildCodexToml(cfg)},
			},
		},
	}
}

// mcpClients 是「接入 AI 客户端」块（渲染层 McpClientTabs）：Cursor / Codex 两个标签，每个有配置文件位置、一键写入（Cursor）、可复制的片段与易错提醒。
// 片段里的环境变量会原样出现在配置里（用户要拿去粘贴），但从不写进日志、错误详情或提示。
type mcpClients struct {
	Look    *kit.Look
	Guard   kit.Guard
	Service services.MCP
	Wake    func()

	active     services.MCPClient
	installing bool
	copied     string // 刚复制的片段键：显示「已复制」
	copiedAt   time.Time
	timer      *time.Timer
	w          worker
}

func (k *mcpClients) close() {
	k.w.close()
	if k.timer != nil {
		k.timer.Stop()
	}
}

// Page 声明整块。
func (k *mcpClients) Page(c ui.C, cfg services.MCPConfig) {
	l := k.Look
	t := l.T
	k.w.drain()
	if k.active == "" {
		k.active = services.MCPClientCursor
	}
	presets := buildClientPresets(cfg)
	preset := presets[0]
	for _, p := range presets {
		if p.id == k.active {
			preset = p
		}
	}
	amber := tokens.Tailwind("amber-600")
	if l.Dark {
		amber = tokens.Tailwind("amber-400")
	}
	ui.Column(c).FillWidth().Children(func() {
		k.tabs(c.Key("tabs"), presets)
		l.Text(c.Key("location"), preset.location, relaxed(kit.TextXS), kit.WeightNormal, t.MutedForeground, kit.Wrapped()).MarginTop(l.Space(3))
		if preset.oneClick {
			style := primaryBtn(l, shape(l, 2.25, 3, 1.5, kit.TextSM, kit.WeightMedium))
			style.DisabledOpacity = 0.6
			ic, size := iconOf(l, icons.Zap, false)
			spec := kit.ButtonSpec{Label: locales.SettingsMcpInstallOneClick(preset.label), Icon: ic, IconSize: size}
			ui.Row(c.Key("install")).MarginTop(l.Space(3)).Children(func() {
				if btn(l, c.Key("install-button"), spec, style, k.installing) {
					k.install(preset)
				}
			})
			l.Text(c.Key("install-hint"), locales.SettingsMcpInstallHint(), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped()).MarginTop(l.Space(1.5))
		}
		for _, s := range preset.snippets {
			k.snippet(c.Key("snippet-"+s.key), s)
		}
		l.Text(c.Key("pitfall"), preset.pitfall, relaxed(kit.TextXS), kit.WeightNormal, amber, kit.Wrapped()).MarginTop(l.Space(3))
	})
}

// tabs 是客户端标签条：flex gap-1 border-b border-border/60；每个标签 -mb-px border-b-2 px-3 py-1.5 text-sm，选中 border-primary text-foreground。
func (k *mcpClients) tabs(c ui.C, presets []clientPreset) {
	l := k.Look
	t := l.T
	ui.Row(c).FillWidth().Gap(l.Space(1)).
		DrawOver(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeBottom, 1, t.Border.WithAlpha(0.6)) }).
		Children(func() {
			for _, p := range presets {
				on := p.id == k.active
				tc := c.Key(string(p.id))
				id := ui.ID(tc)
				box := ui.Row(tc).Items(ui.Center).PadSides(l.Space(1.5), l.Space(3), l.Space(1.5)+2, l.Space(3)).
					// 渲染层的标签是普通按钮（没有 role=tab / aria-selected）：当前项只靠下划线与文字色表达，可访问树里与其余按钮一样。
					HitWith(id, p.label, gfx.HitOptions{Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand})
				l.TabStop(box)
				ink, line := t.MutedForeground, t.Border.WithAlpha(0)
				switch {
				case on:
					ink, line = t.Foreground, t.Primary
				case box.State().Hovered:
					ink = t.Foreground
				}
				box.DrawOver(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeBottom, 2, line) }).Children(func() {
					l.Text(tc.Key("label"), p.label, kit.TextSM, kit.WeightNormal, ink)
				})
				if box.Clicked() {
					k.active = p.id
				}
			}
		})
}

// install 把归知合并进客户端配置文件（已有的其它 MCP 服务器与注释保留，覆盖前留备份）；成功提示带文件路径详情。
func (k *mcpClients) install(p clientPreset) {
	if k.installing {
		return
	}
	k.installing = true
	svc, g := k.Service, k.Guard
	k.w.act(k.Wake, func() func() {
		res, err := services.MCPInstallResult{}, errMCPUnavailable
		if svc != nil {
			res, err = svc.Install(context.Background(), p.id)
		}
		switch {
		case err != nil:
			g.Fail(locales.SettingsMcpInstallFailed(), p.label, err)
		case g.Notify != nil && res.Replaced:
			g.Notify.Success(locales.SettingsMcpInstallUpdated(p.label), kit.WithDetail(res.FilePath))
		case g.Notify != nil:
			g.Notify.Success(locales.SettingsMcpInstallWritten(p.label), kit.WithDetail(res.FilePath))
		}
		return func() { k.installing = false }
	})
}

func (k *mcpClients) copy(s clientSnippet) {
	if err := k.Guard.Notify.CopyText(s.text); err != nil {
		// 只报剪贴板的原因，不带片段原文。
		k.Guard.Fail(locales.SettingsMcpCopyFailed(), locales.SettingsMcpCopy(), err)
		return
	}
	k.copied, k.copiedAt = s.key, time.Now()
	if k.timer != nil {
		k.timer.Stop()
	}
	wake := k.Wake
	k.timer = time.AfterFunc(copiedFor, func() {
		if wake != nil {
			wake()
		}
	})
}

// snippet 是一段可复制的配置：标签加「复制」小钮，下面是等宽原文（max-h-56，超出裁切）。
func (k *mcpClients) snippet(c ui.C, s clientSnippet) {
	l := k.Look
	t := l.T
	done := k.copied == s.key && time.Since(k.copiedAt) < copiedFor
	ui.Column(c).FillWidth().MarginTop(l.Space(3)).Children(func() {
		ui.Row(c.Key("head")).FillWidth().Items(ui.Center).Gap(l.Space(2)).MarginBottom(l.Space(1.5)).Children(func() {
			l.Text(c.Key("label"), s.label, kit.TextXS, kit.WeightNormal, t.MutedForeground).Grow(1).MinWidth(0)
			style := kit.ButtonStyle{
				Height: l.Rem(1.5), PadX: l.Space(2), Gap: l.Space(1), Radius: l.Rem(0.375), Transition: kit.DurationDefault,
				FontPx: 11, LinePx: 11 * 1.5, Weight: kit.WeightNormal,
				Fg: t.MutedForeground, FgHover: t.Foreground, Border: t.Border.WithAlpha(0.7), BorderHover: t.Border, BorderW: 1,
				BgHover: t.Accent.WithAlpha(0.6),
			}
			label, ic := locales.SettingsMcpCopy(), icons.ClipboardCopy
			if done {
				label, ic = locales.SettingsMcpCopied(), icons.Check // 渲染层的对勾是 primary 色，按钮只有一个前景色，这里随文字
			}
			if l.Button(c.Key("copy"), kit.ButtonSpec{Label: label, Name: label, Icon: ic, IconSize: l.Rem(0.75)}, style).Clicked() {
				k.copy(s)
			}
		})
		lines := strings.Split(strings.TrimRight(s.text, " \r\n\t"), "\n")
		ui.Column(c.Key("pre")).FillWidth().MaxHeight(l.Rem(14)).Clip().Pad(l.Space(3) + 1).
			Draw(func(d *ui.DrawContext) {
				l.PaintSurface(d, kit.Surface{Bg: t.Muted.WithAlpha(0.4), Border: t.Border.WithAlpha(0.7), BorderW: 1, Radius: l.Rem(0.75)})
			}).Children(func() {
			l.MonoParagraph(c.Key("text"), lines, 11, 11*1.625, t.Foreground)
		})
	})
}
