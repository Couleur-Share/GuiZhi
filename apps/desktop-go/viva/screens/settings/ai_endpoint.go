package settings

import (
	"strconv"
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

func itoa(n int) string { return strconv.Itoa(n) }

// mdRadius 是 Tailwind 的 rounded-md：本应用 --radius（0.75rem）减 2px。
func mdRadius(l *kit.Look) float32 { return l.Rem(0.75) - 2 }

// ── 状态 ──

// endpointStatus 是端点的状态（EndpointsSection.getEndpointStatus）：已停用 → 本次会话测试结果 → 已验证过 → 已配置未验证 → 未配置。
func (a *AI) endpointStatus(g services.AIEndpointGroup) endpointStatus {
	if !g.Enabled {
		return endpointStatus{toneWarning, locales.SettingsAiWorkbenchDisabled(), locales.SettingsAiWorkbenchProviderDisabledDetail(len(g.Models))}
	}
	if s, ok := a.statuses[g.Key]; ok {
		return s
	}
	if s, ok := a.verifiedStatus(g); ok {
		return s
	}
	for _, m := range g.Models {
		if services.IsConfiguredModel(m) {
			return endpointStatus{toneWarning, locales.SettingsAiWorkbenchUnverified(), locales.SettingsAiWorkbenchModelCount(len(g.Models))}
		}
	}
	return endpointStatus{toneWarning, locales.SettingsAiWorkbenchNotConfigured(), locales.SettingsAiWorkbenchMissingModelConfig()}
}

// verifiedStatus 是持久化的验证结果（buildVerifiedEndpointStatus）：旗下有模型带验证时间就算已连接，
// 详情是「首个已验证模型 · 最近验证时间」，时间解析不了时退回模型数。
func (a *AI) verifiedStatus(g services.AIEndpointGroup) (endpointStatus, bool) {
	var first *services.AIModel
	var latest time.Time
	for i, m := range g.Models {
		if strings.TrimSpace(m.LastVerifiedAt) == "" {
			continue
		}
		if first == nil {
			first = &g.Models[i]
		}
		if at, err := time.Parse(time.RFC3339Nano, m.LastVerifiedAt); err == nil && at.After(latest) {
			latest = at
		}
	}
	if first == nil {
		return endpointStatus{}, false
	}
	prefix := ""
	if strings.TrimSpace(first.Model) != "" {
		prefix = first.Model + " · "
	}
	suffix := locales.SettingsAiWorkbenchModelCount(len(g.Models))
	if !latest.IsZero() {
		suffix = formatDateTime(latest.UnixMilli(), false, a.Locale)
	}
	return endpointStatus{toneReady, locales.SettingsAiWorkbenchConnected(), prefix + suffix}, true
}

// ── 端点卡 ──

// endpointCard 是选中供应商的主面板：头部（名称、状态、操作）、凭据块、模型列表。
func (a *AI) endpointCard(c ui.C, g services.AIEndpointGroup) {
	l := a.Look
	t := l.T
	wide := l.ViewportWidth(c) >= 768 // md
	engine := g.ProviderConfigID == services.AIFunasrProviderID
	ui.Column(c).FillWidth().Pad(1).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.Card, Border: t.Border, BorderW: 1, Radius: l.Rem(0.75)})
		}).Children(func() {
		a.endpointHeader(c.Key("header"), g, engine, wide)
		ui.Column(c.Key("creds-wrap")).FillWidth().PadSides(l.Space(3), l.Space(4), l.Space(3), l.Space(4)).Children(func() {
			a.credentials(c.Key("creds"), g, engine)
		})
		ui.Row(c.Key("models-head")).FillWidth().Items(ui.Center).Justify(ui.Between).PadSides(l.Space(2.5), l.Space(4), l.Space(2.5), l.Space(4)).Children(func() {
			l.Text(c.Key("title"), locales.SettingsModel(), kit.TextSM, kit.WeightSemibold, t.Foreground)
			if engine {
				return
			}
			ui.Row(c.Key("actions")).Items(ui.Center).Gap(l.Space(1.5)).Children(func() {
				st := kit.ButtonStyle{Radius: mdRadius(l), Fg: t.MutedForeground, BgHover: t.Muted, Border: t.Border, BorderW: 1}
				fetch, add := locales.SettingsFetchModels(), locales.SettingsAddModel()
				if smallIconBtnPx(l, c.Key("fetch"), icons.ListPlus, fetch, fetch, l.Rem(2), l.Rem(1), false, st) {
					a.openFetchModels(g)
				}
				if smallIconBtnPx(l, c.Key("add"), icons.Plus, add, add, l.Rem(2), l.Rem(1), false, st) {
					a.openAddModel(g)
				}
			})
		})
		badges := a.routeBadges()
		for i, m := range g.Models {
			a.modelRow(c.Key(m.ID), m, badges[m.ID], i == len(g.Models)-1, wide, engine)
		}
	})
}

// smallIconBtnPx 同 smallIconBtn，图标边长可调（模型区头部的钮图标是 1rem，行内的是 0.875rem）。
func smallIconBtnPx(l *kit.Look, c ui.C, ic gfx.Icon, name, hint string, size, iconSize float32, disabled bool, st kit.ButtonStyle) bool {
	st.Height, st.Width = size, size
	st.Transition = kit.DurationDefault
	st.DisabledOpacity = 0.4
	return l.Button(c, kit.ButtonSpec{Name: name, Hint: hint, Icon: ic, IconSize: iconSize, Center: true, Disabled: disabled}, st).Clicked()
}

// endpointHeader 是端点卡头部：border-b px-4 py-3.5；md 以上左右并排，否则上下叠放。
func (a *AI) endpointHeader(c ui.C, g services.AIEndpointGroup, engine, wide bool) {
	l := a.Look
	t := l.T
	status := a.endpointStatus(g)
	detail := status.detail
	if !g.Enabled {
		detail = locales.SettingsAiWorkbenchModelCount(len(g.Models))
	}
	name := g.DisplayName(providerLabel)
	host := services.EndpointHost(g.APIURL, protocolLabel(g.APIProtocol))
	box := ui.Row(c)
	if !wide {
		box = ui.Column(c).Gap(l.Space(3))
	}
	box.FillWidth().Items(ui.Center).Justify(ui.Between).Gap(l.Space(3)).
		PadSides(l.Space(3.5), l.Space(4), l.Space(3.5)+1, l.Space(4)).
		DrawOver(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeBottom, 1, t.Border) }).
		Children(func() {
			ui.Row(c.Key("who")).Grow(1).MinWidth(0).Items(ui.Center).Gap(l.Space(3)).Children(func() {
				brandBadge(l, c.Key("brand"), services.EndpointCategory(g.Provider, g.Models), l.Rem(2.5), 20, t.Primary.WithAlpha(0.1), l.Rem(0.75)) // h-10 w-10 rounded-xl bg-primary/10，图标 20px
				ui.Column(c.Key("text")).Grow(1).MinWidth(0).Children(func() {
					ui.Row(c.Key("title")).FillWidth().Items(ui.Center).Gap(l.Space(2.5)).Children(func() {
						l.Heading(c.Key("name"), 4, name, kit.TextStyle{Size: 1, Line: 1.25}, kit.WeightSemibold, t.Foreground, kit.Ellipsis()).MinWidth(0)
						if engine {
							a.engineBadge(c.Key("builtin"))
						} else {
							a.protocolBadge(c.Key("protocol"), protocolLabel(g.APIProtocol))
						}
					})
					ui.Row(c.Key("status")).FillWidth().Items(ui.Center).Gap(l.Space(2)).Children(func() {
						statusDot(l, c.Key("dot"), l.Rem(0.375), toneColor(status.tone), 3, 0.15)
						l.Text(c.Key("label"), status.label, kit.TextXS, kit.WeightNormal, t.MutedForeground).Shrink(0)
						l.Text(c.Key("sep1"), "·", kit.TextXS, kit.WeightNormal, t.Border).Shrink(0)
						l.Text(c.Key("detail"), detail, kit.TextXS, kit.WeightNormal, t.MutedForeground).Shrink(0)
						l.Text(c.Key("sep2"), "·", kit.TextXS, kit.WeightNormal, t.Border).Shrink(0)
						l.Text(c.Key("host"), host, kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Ellipsis()).MinWidth(0)
					}).MarginTop(l.Space(1.5))
				})
			})
			a.endpointActions(c.Key("actions"), g, engine)
		})
}

// engineBadge 是内置引擎的徽标（`rounded-md bg-primary/10 px-2 py-0.5 text-[10px] font-medium text-primary`，带包图标）。
func (a *AI) engineBadge(c ui.C) {
	l := a.Look
	t := l.T
	ui.Row(c).Shrink(0).Items(ui.Center).Gap(l.Space(1)).PadSides(l.Space(0.5), l.Space(2), l.Space(0.5), l.Space(2)).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.Primary.WithAlpha(0.1), Radius: mdRadius(l)})
		}).
		Children(func() {
			l.Icon(c.Key("icon"), icons.Package, l.Rem(0.75), t.Primary)
			l.TextPx(c.Key("label"), locales.SettingsAiWorkbenchBuiltin(), 10, 15, kit.WeightMedium, t.Primary)
		})
}

// protocolBadge 是协议徽标（`rounded-md border border-border/70 bg-muted/30 px-2 py-0.5 text-[10px] font-medium text-muted-foreground`）。
func (a *AI) protocolBadge(c ui.C, label string) {
	l := a.Look
	t := l.T
	ui.Row(c).Shrink(0).Items(ui.Center).PadSides(l.Space(0.5)+1, l.Space(2)+1, l.Space(0.5)+1, l.Space(2)+1).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.Muted.WithAlpha(0.3), Border: t.Border.WithAlpha(0.7), BorderW: 1, Radius: mdRadius(l)})
		}).
		Children(func() { l.TextPx(c.Key("label"), label, 10, 15, kit.WeightMedium, t.MutedForeground) })
}

// endpointActions 是头部右侧的操作：参与路由开关、测试连接、编辑、删除。内置引擎的编辑与删除置灰。
func (a *AI) endpointActions(c ui.C, g services.AIEndpointGroup, engine bool) {
	l := a.Look
	t := l.T
	ui.Row(c).Shrink(0).Items(ui.Center).Gap(l.Space(1.5)).Children(func() {
		// 参与路由：h-8 的描边小块，里面是文字加紧凑开关。
		ui.Row(c.Key("route")).Height(l.Rem(2)).Items(ui.Center).Gap(l.Space(2)).PadSides(0, l.Space(2.5)+1, 0, l.Space(2.5)+1).MarginRight(l.Space(0.5)).
			Draw(func(d *ui.DrawContext) {
				l.PaintSurface(d, kit.Surface{Bg: t.Muted.WithAlpha(0.25), Border: t.Border.WithAlpha(0.7), BorderW: 1, Radius: l.Rem(0.75)})
			}).Children(func() {
			l.TextPx(c.Key("label"), locales.SettingsAiWorkbenchRoutingParticipation(), 11, 16.5, kit.WeightMedium, t.MutedForeground)
			label := locales.SettingsAiWorkbenchEnableProvider()
			if g.Enabled {
				label = locales.SettingsAiWorkbenchDisableProvider()
			}
			on := g.Enabled
			if l.SwitchWith(c.Key("switch"), &on, kit.SwitchSpec{Label: label, Compact: true}).Clicked() {
				a.toggleEndpoint(g, on)
			}
		})
		testing := a.testingEndpoint == g.Key
		test := outlineBtn(l, shape(l, 2, 2.5, 1.5, kit.TextXS, kit.WeightMedium), t.Muted)
		test.Border, test.Bg, test.Fg, test.FgHover = t.Border.WithAlpha(0.7), t.Background.WithAlpha(0.4), t.MutedForeground, t.Foreground
		test.DisabledOpacity = 0.4
		spec := kit.ButtonSpec{Label: locales.SettingsTestConnection(), Icon: icons.TestTube, IconSize: l.Rem(0.875)}
		if testing {
			spec.Busy, spec.Icon = true, gfx.Icon{}
		}
		spec.Disabled = testing || !g.Enabled
		spec.BusySize = l.Rem(0.875)
		if l.Button(c.Key("test"), spec, test).Clicked() {
			a.testEndpoint(g)
		}
		edit, del := locales.CommonEdit(), locales.CommonDelete()
		editHint, delHint := edit, del
		if engine {
			editHint, delHint = locales.SettingsAiWorkbenchLocalEngineReadOnly(), locales.SettingsAiWorkbenchLocalEngineLocked()
		}
		sq := kit.ButtonStyle{Radius: l.Rem(0.75), Fg: t.MutedForeground, FgHover: t.Foreground, BgHover: t.Muted, Border: t.Border.WithAlpha(0.7), BorderW: 1, Bg: t.Background.WithAlpha(0.4)}
		if smallIconBtn(l, c.Key("edit"), icons.Pencil, edit, editHint, l.Rem(2), engine, sq) {
			a.openEditEndpoint(g)
		}
		sq.FgHover, sq.BgHover, sq.BorderHover = t.Destructive, t.Destructive.WithAlpha(0.1), t.Destructive.WithAlpha(0.3)
		if smallIconBtn(l, c.Key("delete"), icons.Trash2, del, delHint, l.Rem(2), engine, sq) {
			e := g
			a.delEndpoint = &e
		}
	})
}

// ── 凭据 ──

// aiCredentials 是凭据块的草稿：地址与密钥两个文本框，随选中的端点与已保存值同步。
// 密钥只存在于输入框里（默认圆点），草稿之外不留副本。
type aiCredentials struct {
	groupKey string
	baseURL  string
	baseKey  string
	url, key ui.Field
	show     bool
}

// sync 在选中的端点或其已保存值变化时重置草稿（EndpointsSection 里依赖 [apiKey, apiUrl, key] 的 effect）。
func (cr *aiCredentials) sync(groupKey, apiURL, apiKey string) {
	if cr.groupKey == groupKey && cr.baseURL == apiURL && cr.baseKey == apiKey {
		return
	}
	cr.groupKey, cr.baseURL, cr.baseKey = groupKey, apiURL, apiKey
	cr.url.SetText(apiURL)
	cr.key.SetText(apiKey)
	cr.show = false
}

func (cr *aiCredentials) dirty() bool {
	return cr.url.Text() != cr.baseURL || cr.key.Text() != cr.baseKey
}

func (cr *aiCredentials) reset() {
	cr.url.SetText(cr.baseURL)
	cr.key.SetText(cr.baseKey)
}

// credentials 是凭据块（`rounded-lg border border-border/70 bg-muted/20 p-3`）。
func (a *AI) credentials(c ui.C, g services.AIEndpointGroup, engine bool) {
	l := a.Look
	t := l.T
	cr := &a.cred
	cr.sync(g.Key, g.APIURL, g.EffectiveAPIKey())
	dirty := cr.dirty()
	canSave := dirty && trimmed(cr.url.Text()) != ""
	title := locales.SettingsAiWorkbenchEndpointCredentials()
	if engine {
		title = locales.SettingsAiWorkbenchLocalEngineSection()
	}
	ui.Column(c).FillWidth().Pad(l.Space(3) + 1).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.Muted.WithAlpha(0.2), Border: t.Border.WithAlpha(0.7), BorderW: 1, Radius: l.Rem(0.75)})
		}).Children(func() {
		ui.Row(c.Key("head")).FillWidth().Items(ui.Start).Justify(ui.Between).Gap(l.Space(3)).MarginBottom(l.Space(3)).Children(func() {
			l.Text(c.Key("title"), title, kit.TextXS, kit.WeightMedium, t.MutedForeground)
			if !dirty {
				return
			}
			ui.Row(c.Key("buttons")).Shrink(0).Items(ui.Center).Gap(l.Space(1)).Children(func() {
				st := kit.ButtonStyle{Radius: mdRadius(l), Fg: t.MutedForeground, FgHover: t.Foreground, Bg: t.Background, BgHover: t.Muted, Border: t.Border, BorderW: 1}
				save, cancel := locales.CommonSave(), locales.CommonCancel()
				if smallIconBtnPx(l, c.Key("save"), icons.Check, save, save, l.Rem(2), l.Rem(1), !canSave, st) {
					a.saveCredentials(g)
				}
				if smallIconBtnPx(l, c.Key("cancel"), icons.X, cancel, cancel, l.Rem(2), l.Rem(1), false, st) {
					cr.reset()
				}
			})
		})
		ui.Column(c.Key("body")).FillWidth().Gap(l.Space(2)).Children(func() {
			if engine {
				l.Text(c.Key("note"), locales.SettingsAiWorkbenchLocalEngineManaged(), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
			}
			a.credentialURL(c.Key("url"), g, engine)
			if engine {
				a.manageEngineButton(c.Key("manage"))
				return
			}
			a.credentialKey(c.Key("key"), g)
		})
	})
}

// credentialRow 是凭据块里的一行：图标、小标签加输入。
func (a *AI) credentialRow(c ui.C, ic gfx.Icon, label string, input func(c ui.C)) {
	l := a.Look
	t := l.T
	ui.Row(c).FillWidth().Items(ui.Center).Gap(l.Space(3)).Children(func() {
		l.Icon(c.Key("icon"), ic, l.Rem(1), t.MutedForeground).Shrink(0)
		ui.Column(c.Key("col")).Grow(1).MinWidth(0).Children(func() {
			l.Text(c.Key("label"), label, kit.TextXS, kit.WeightNormal, t.MutedForeground).MarginBottom(l.Space(1))
			input(c.Key("input"))
		})
	})
}

// credField 是凭据块里的文本框（`h-9 rounded-md border border-border bg-background px-3 font-mono text-sm`）。
func (a *AI) credField(c ui.C, f *ui.Field, name, placeholder string, right float32, overlay func(ui.C)) ui.Box {
	l := a.Look
	t := l.T
	return aiField(l, c, f, aiFieldSpec{
		Name: name, Placeholder: placeholder, Height: l.Rem(2.25), Radius: mdRadius(l), Bg: t.Background, Border: t.Border, BorderW: 1,
		Left: l.Space(3), Right: right, Mono: true, PlaceholderInk: t.MutedForeground, Overlay: overlay,
	})
}

func (a *AI) credentialURL(c ui.C, g services.AIEndpointGroup, engine bool) {
	l := a.Look
	t := l.T
	a.credentialRow(c, icons.Link, locales.SettingsApiUrl(), func(c ui.C) {
		if engine {
			// 不带边框：同样的高度加一圈边框就是一个 disabled 的输入框，而这里要说的恰恰是「这不是让你填的东西」。
			ui.Row(c).FillWidth().Height(l.Rem(2.25)).Items(ui.Center).PadSides(0, l.Space(3), 0, l.Space(3)).
				Draw(func(d *ui.DrawContext) {
					l.PaintSurface(d, kit.Surface{Bg: t.Muted.WithAlpha(0.5), Radius: mdRadius(l)})
				}).
				Children(func() {
					l.Text(c.Key("value"), g.APIURL, kit.TextSM, kit.WeightNormal, t.MutedForeground, kit.Ellipsis()).MinWidth(0)
				})
			return
		}
		box := a.credField(c, &a.cred.url, locales.SettingsAiWorkbenchEndpointApiUrl(), "", l.Space(3), nil)
		a.credentialKeys(box, g)
	})
}

func (a *AI) credentialKey(c ui.C, g services.AIEndpointGroup) {
	l := a.Look
	cr := &a.cred
	a.credentialRow(c, icons.KeyRound, locales.SettingsApiKey(), func(c ui.C) {
		cr.key.Secure = !cr.show
		box := a.credField(c, &cr.key, locales.SettingsAiWorkbenchEndpointApiKey(), services.MaskAPIKey(g.EffectiveAPIKey()), l.Space(9), func(c ui.C) {
			size := l.Rem(1.75)
			eyeToggle(l, c.Key("eye"), &cr.show, locales.CommonShow(), locales.CommonHide(), size).
				Absolute().Right(l.Space(1)).Top((l.Rem(2.25) - size) / 2)
		})
		a.credentialKeys(box, g)
	})
}

// credentialKeys 给凭据文本框接键盘：回车保存，Esc 还原（渲染层 onKeyDown）。
func (a *AI) credentialKeys(box ui.Box, g services.AIEndpointGroup) {
	box.OnKey(func(e gfx.KeyEvent) gfx.EventResult {
		if e.Release {
			return gfx.EventResult{}
		}
		switch e.Key {
		case gfx.KeyEscape:
			a.cred.reset()
			return gfx.EventResult{Handled: true, Repaint: true}
		}
		return gfx.EventResult{}
	})
	if box.Submitted() {
		a.saveCredentials(g)
	}
}

// manageEngineButton 是内置引擎卡上的「管理引擎」：跳到「采集与转写」。
func (a *AI) manageEngineButton(c ui.C) {
	l := a.Look
	t := l.T
	st := outlineBtn(l, shape(l, 2, 2.5, 1.5, kit.TextXS, kit.WeightNormal), t.Muted)
	st.Fg, st.FgHover, st.Bg, st.Radius = t.MutedForeground, t.Foreground, t.Background, mdRadius(l)
	if l.Button(c, kit.ButtonSpec{Label: locales.SettingsAiWorkbenchLocalEngineManage(), Icon: icons.Settings, IconSize: l.Rem(0.875)}, st).Self(ui.Start).Clicked() && a.OnManageEngine != nil {
		a.OnManageEngine()
	}
}

// ── 模型行 ──

// typeBadge 是模型类型徽标：嵌入、转写、文生图是专用模型，其余按对话模型展示。
func typeBadge(m services.AIModel) (gfx.Icon, string) {
	switch {
	case m.Capabilities.Embedding:
		return icons.Database, locales.SettingsEmbeddingModel()
	case m.Capabilities.AudioTranscription:
		return icons.AudioLines, locales.SettingsTranscriptionModel()
	case m.Capabilities.ImageGeneration:
		return icons.Image, locales.SettingsImageGenModel()
	}
	return icons.Type, locales.SettingsChatModel()
}

// modelRow 是模型列表的一行：品牌徽标、名称（带别名时下面是模型名）、能力与路由徽标、右侧操作。
func (a *AI) modelRow(c ui.C, m services.AIModel, routes []string, last, wide, engineGroup bool) {
	l := a.Look
	t := l.T
	builtin := m.ID == services.AIFunasrModelID
	l.SettingBlock(c, last, func() {
		row := ui.Row(c.Key("row"))
		if !wide {
			row = ui.Column(c.Key("row")).Gap(l.Space(3))
		}
		row.FillWidth().Items(ui.Center).Justify(ui.Between).Gap(l.Space(3)).PadSides(l.Space(3), l.Space(4), l.Space(3), l.Space(4)).Children(func() {
			ui.Row(c.Key("who")).MinWidth(0).Items(ui.Center).Gap(l.Space(3)).Children(func() {
				brandBadge(l, c.Key("brand"), services.ModelCategory(m.Provider, firstNonEmptyStr(m.Model, m.ID), ""), l.Rem(2.25), 20, t.Muted, mdRadius(l)) // h-9 w-9 rounded-md bg-muted，图标 20px
				ui.Column(c.Key("name")).MinWidth(0).Children(func() {
					l.Text(c.Key("title"), firstNonEmptyStr(m.Name, m.Model), kit.TextSM, kit.WeightMedium, t.Foreground, kit.Ellipsis()).MinWidth(0)
					if m.Name != "" {
						l.Text(c.Key("sub"), m.Model, kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Ellipsis()).MinWidth(0)
					}
				})
				ui.Row(c.Key("badges")).MinWidth(0).Items(ui.Center).Gap(l.Space(1.5)).Children(func() {
					ic, label := typeBadge(m)
					iconBadge(l, c.Key("type"), ic, label, false)
					if m.IsDefault {
						iconBadge(l, c.Key("default"), icons.Star, locales.SettingsAiWorkbenchTypeDefault(), true)
					}
					if services.ModelHasCapability(m, services.CapVision) {
						iconBadge(l, c.Key("vision"), icons.Eye, locales.SettingsAiWorkbenchVisionCapability(), false)
					}
					if m.Capabilities.Reasoning {
						iconBadge(l, c.Key("reasoning"), icons.Brain, locales.SettingsAiWorkbenchReasoningCapability(), false)
					}
					for i, label := range routes {
						routeBadge(l, c.Key("route"+itoa(i)), label)
					}
				})
			})
			a.modelActions(c.Key("actions"), m, builtin)
		})
	})
}

func firstNonEmptyStr(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// modelActions 是模型行右侧的操作：测试、设为默认、编辑、删除。内置引擎的模型只能测试。
func (a *AI) modelActions(c ui.C, m services.AIModel, builtin bool) {
	l := a.Look
	t := l.T
	ui.Row(c).Shrink(0).Items(ui.Center).Gap(l.Space(1.5)).Children(func() {
		plain := kit.ButtonStyle{Radius: mdRadius(l), Fg: t.MutedForeground, BgHover: t.Muted}
		test := locales.SettingsAiWorkbenchTestAction()
		testing := a.testingModel == m.ID
		spec := kit.ButtonSpec{Name: test, Hint: test, Icon: icons.TestTube, IconSize: l.Rem(0.875), Center: true, Disabled: testing, Busy: testing}
		if testing {
			spec.Icon, spec.BusySize = gfx.Icon{}, l.Rem(0.875)
		}
		sz := l.Rem(2)
		st := plain
		st.Height, st.Width, st.Transition, st.DisabledOpacity = sz, sz, kit.DurationDefault, 0.5
		if l.Button(c.Key("test"), spec, st).Clicked() {
			a.testModelRow(m)
		}
		if !m.IsDefault {
			def := locales.SettingsSetDefault()
			if smallIconBtn(l, c.Key("default"), icons.Star, def, def, sz, false, plain) {
				a.setDefaultModel(m.ID)
			}
		}
		edit, del := locales.CommonEdit(), locales.CommonDelete()
		editHint, delHint := edit, del
		if builtin {
			editHint, delHint = locales.SettingsAiWorkbenchLocalEngineReadOnly(), locales.SettingsAiWorkbenchLocalEngineLocked()
		}
		if smallIconBtn(l, c.Key("edit"), icons.Pencil, edit, editHint, sz, builtin, plain) {
			a.openEditModel(m)
		}
		red := tokens.Tailwind("red-500")
		danger := plain
		danger.Fg, danger.BgHover = red, red.WithAlpha(0.05)
		if builtin {
			danger.Fg = t.MutedForeground
		}
		if smallIconBtn(l, c.Key("delete"), icons.Trash2, del, delHint, sz, builtin, danger) {
			mm := m
			a.delModel = &mm
		}
	})
}
