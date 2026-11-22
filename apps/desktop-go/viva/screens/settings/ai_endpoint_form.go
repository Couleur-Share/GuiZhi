package settings

import (
	"strings"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// endpointForm 是「添加供应商 / 编辑端点」弹窗的草稿（EndpointDraft）。
// 文本框的内容放在 ui.Field 里；只在保存时才写回配置，取消丢弃。
type endpointForm struct {
	adding             bool
	group              services.AIEndpointGroup // 编辑时是原分组；添加时为零值
	name, key, url     ui.Field
	provider, protocol string
	enabled            bool
	showKey            bool
	urlFocused         bool // 上一帧地址框是否有焦点：失焦的那一帧把地址规范化（onBlur）
	provSel, protoSel  kit.SelectState
}

// providerOptionLabel 是供应商预设在下拉里的显示名。
func providerOptionLabel(p services.AIProviderPreset) string { return providerLabel(p.ID) }

// openAddEndpoint 打开「添加供应商」：默认 OpenAI，名称、协议、地址取预设。
func (a *AI) openAddEndpoint() {
	preset, _ := services.AIProviderPresetByID("openai")
	f := &endpointForm{adding: true, provider: preset.ID, protocol: preset.RecommendedProtocol, enabled: true}
	f.name.SetText(providerOptionLabel(preset))
	f.url.SetText(preset.DefaultURL)
	a.endpoint = f
}

// openEditEndpoint 打开「编辑端点」，草稿取自分组当前值。
func (a *AI) openEditEndpoint(g services.AIEndpointGroup) {
	f := &endpointForm{group: g, provider: g.Provider, protocol: g.APIProtocol, enabled: g.Enabled}
	name := g.Name
	if name == "" {
		name = g.DisplayName(providerLabel)
	}
	f.name.SetText(name)
	f.key.SetText(g.EffectiveAPIKey())
	f.url.SetText(g.APIURL)
	a.endpoint = f
}

// saveEndpoint 保存草稿：名称缺省取供应商类型名；地址规范化；凭据变了，验证时间清空。
func (a *AI) saveEndpoint() {
	f := a.endpoint
	if f == nil {
		return
	}
	name := trimmed(f.name.Text())
	if name == "" {
		name = providerLabel(f.provider)
	}
	patch := endpointPatch{
		name: name, provider: trimmed(f.provider), protocol: f.protocol,
		apiKey: trimmed(f.key.Text()), apiURL: services.NormalizeAPIURL(f.url.Text()), enabled: &f.enabled,
	}
	a.endpoint = nil
	if f.adding {
		next, _ := a.cfg.AddProvider(services.AIProvider{
			Name: patch.name, Provider: patch.provider, APIProtocol: patch.protocol,
			APIKey: patch.apiKey, APIURL: patch.apiURL, Enabled: f.enabled,
		})
		a.commit(next, locales.SettingsAiWorkbenchProviderAdded())
		return
	}
	delete(a.statuses, f.group.Key)
	a.commit(applyEndpoint(a.cfg, f.group, patch), locales.SettingsAiWorkbenchEndpointUpdated())
}

// changeProvider 切换供应商类型：添加时名称若还是上一个类型名（或为空）就跟着换；协议与地址取新预设。
func (f *endpointForm) changeProvider(id string) {
	next, _ := services.AIProviderPresetByID(id)
	if f.adding {
		prev := f.provider
		if p, ok := services.AIProviderPresetByID(f.provider); ok {
			prev = providerOptionLabel(p)
		}
		if cur := trimmed(f.name.Text()); cur == "" || cur == prev {
			name := id
			if next.ID != "" {
				name = providerOptionLabel(next)
			}
			f.name.SetText(name)
		}
	}
	f.provider = id
	if next.RecommendedProtocol != "" {
		f.protocol = next.RecommendedProtocol
	}
	if next.DefaultURL != "" {
		f.url.SetText(next.DefaultURL)
	}
}

// endpointModal 声明「添加供应商 / 编辑端点」弹窗；每帧都要经过（关闭帧负责复位）。
func (a *AI) endpointModal(c ui.C) {
	l := a.Look
	f := a.endpoint
	spec := kit.ModalSpec{Open: f != nil, Size: kit.ModalLG, CloseLabel: locales.CommonClose()}
	if f != nil {
		spec.Title, spec.SubtitleText = locales.SettingsAiWorkbenchEditEndpoint(), locales.SettingsAiWorkbenchEditEndpointSubtitle()
		if f.adding {
			spec.Title, spec.SubtitleText = locales.SettingsAiWorkbenchAddProvider(), locales.SettingsAiWorkbenchAddProviderSubtitle()
		}
		sub := spec.SubtitleText
		spec.Subtitle = func(c ui.C) {
			l.Text(c, sub, kit.TextXS, kit.WeightNormal, l.T.MutedForeground, kit.Wrapped())
		}
		spec.Body = func(c ui.C) { a.endpointBody(c, f) }
	}
	if l.Modal(c, &a.epModal, spec) {
		a.endpoint = nil
	}
}

func (a *AI) endpointBody(c ui.C, f *endpointForm) {
	l := a.Look
	t := l.T
	preset, hasPreset := services.AIProviderPresetByID(f.provider)
	typeLabel := f.provider
	if hasPreset {
		typeLabel = providerOptionLabel(preset)
	}
	ui.Column(c).FillWidth().Gap(l.Space(4)).Children(func() {
		// 名称
		ui.Column(c.Key("name")).FillWidth().Children(func() {
			fieldLabel(l, c.Key("label"), locales.SettingsAiWorkbenchProviderDisplayName())
			modalField(l, c.Key("field"), &f.name, locales.SettingsAiWorkbenchProviderDisplayName(), locales.SettingsAiWorkbenchProviderDisplayNamePlaceholder())
		})
		// 供应商类型
		ui.Column(c.Key("type")).FillWidth().Children(func() {
			fieldLabel(l, c.Key("label"), locales.SettingsAiWorkbenchProviderType())
			options := make([]kit.SelectOption, 0, len(services.AIProviderPresets()))
			for _, p := range services.AIProviderPresets() {
				options = append(options, kit.SelectOption{Value: p.ID, Label: providerOptionLabel(p)})
			}
			if picked, changed := l.Select(c.Key("select"), &f.provSel, kit.SelectSpec{
				Label: locales.SettingsAiWorkbenchProviderType(), Value: f.provider, Options: options, Width: -1,
			}); changed {
				f.changeProvider(picked)
			}
			l.TextPx(c.Key("hint"), locales.SettingsAiWorkbenchProviderTypeHint(typeLabel), 11, 16.5, kit.WeightNormal, t.MutedForeground, kit.Wrapped()).MarginTop(l.Space(1))
		})
		// 协议（仅预设允许自定义协议时）
		if hasPreset && preset.AllowsCustomProtocol {
			ui.Column(c.Key("protocol")).FillWidth().Children(func() {
				fieldLabel(l, c.Key("label"), locales.SettingsProtocol())
				if picked, changed := l.Select(c.Key("select"), &f.protoSel, kit.SelectSpec{
					Label: locales.SettingsProtocol(), Value: f.protocol, Width: -1,
					Options: []kit.SelectOption{
						{Value: services.AIProtocolOpenAI, Label: locales.SettingsProtocolOpenAICompatible()},
						{Value: services.AIProtocolGemini, Label: locales.SettingsProtocolGeminiCompatible()},
						{Value: services.AIProtocolAnthropic, Label: locales.SettingsProtocolAnthropicCompatible()},
					},
				}); changed {
					f.protocol = picked
				}
			})
		}
		// API Key：口令框，默认圆点
		ui.Column(c.Key("key")).FillWidth().Children(func() {
			fieldLabel(l, c.Key("label"), locales.SettingsApiKey())
			l.SettingsPasswordInput(c.Key("field"), &f.key, &f.showKey, kit.SettingsInputSpec{
				Name: locales.SettingsApiKey(), Placeholder: locales.SettingsApiKeyPlaceholder(),
			}, locales.CommonShowPassword(), locales.CommonHidePassword())
		})
		// API 地址与说明卡
		ui.Column(c.Key("url")).FillWidth().Children(func() {
			fieldLabel(l, c.Key("label"), locales.SettingsApiUrl())
			box := modalField(l, c.Key("field"), &f.url, locales.SettingsApiUrl(), "")
			focused := box.State().Focused
			if f.urlFocused && !focused {
				// 失焦：把用户粘贴的完整端点规范化成基础地址（onBlur）。
				if norm := services.NormalizeAPIURL(f.url.Text()); norm != f.url.Text() {
					f.url.SetText(norm)
				}
			}
			f.urlFocused = focused
			a.urlGuide(c.Key("guide"), f, preset)
		})
		// 启用开关
		a.enabledRow(c.Key("enabled"), f)
		// 页脚
		ui.Row(c.Key("footer")).FillWidth().Justify(ui.End).Gap(l.Space(2)).PadSides(l.Space(4)+1, 0, 0, 0).
			Draw(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeTop, 1, t.Border) }).Children(func() {
			b := shape(l, 2.25, 4, 2, kit.TextSM, kit.WeightNormal)
			cancel := outlineBtn(l, b, t.Muted)
			if btn(l, c.Key("cancel"), kit.ButtonSpec{Label: locales.CommonCancel()}, cancel, false) {
				a.endpoint = nil
			}
			save := primaryBtn(l, shape(l, 2.25, 4, 2, kit.TextSM, kit.WeightMedium))
			if btn(l, c.Key("save"), kit.ButtonSpec{Label: locales.SettingsSaveChanges()}, save, false) {
				a.saveEndpoint()
			}
		})
	})
}

// enabledRow 是「供应商状态」行：`rounded-lg border border-border/70 bg-muted/20 px-3 py-2.5`，右侧开关。
func (a *AI) enabledRow(c ui.C, f *endpointForm) {
	l := a.Look
	t := l.T
	state := locales.SettingsAiWorkbenchEnabled()
	if !f.enabled {
		state = locales.SettingsAiWorkbenchDisabled()
	}
	ui.Row(c).FillWidth().Items(ui.Center).Justify(ui.Between).Gap(l.Space(3)).
		PadSides(l.Space(2.5)+1, l.Space(3)+1, l.Space(2.5)+1, l.Space(3)+1).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.Muted.WithAlpha(0.2), Border: t.Border.WithAlpha(0.7), BorderW: 1, Radius: l.Rem(0.75)})
		}).Children(func() {
		ui.Column(c.Key("text")).Shrink(1).MinWidth(0).Children(func() {
			l.Text(c.Key("title"), locales.SettingsAiWorkbenchProviderStatus(), kit.TextSM, kit.WeightMedium, t.Foreground)
			l.Text(c.Key("state"), state, kit.TextXS, kit.WeightNormal, t.MutedForeground)
		})
		l.Switch(c.Key("switch"), &f.enabled, locales.SettingsAiWorkbenchProviderStatus())
	})
}

// urlGuide 是地址框下面的说明卡：示例、识别到的基础地址与实际请求地址、完整端点提示。
func (a *AI) urlGuide(c ui.C, f *endpointForm, preset services.AIProviderPreset) {
	l := a.Look
	t := l.T
	raw := f.url.Text()
	trim := trimmed(raw)
	base := services.BaseURL(raw)
	request := services.APIEndpointPreview(raw, f.protocol)
	var examples []string
	switch f.protocol {
	case services.AIProtocolGemini:
		examples = []string{"https://generativelanguage.googleapis.com", "https://generativelanguage.googleapis.com/v1beta"}
	case services.AIProtocolAnthropic:
		examples = []string{"https://api.anthropic.com", "https://api.anthropic.com/v1"}
	default:
		first := preset.DefaultURL
		if first == "" {
			first = "https://api.openai.com"
		}
		examples = []string{first, "https://api.example.com/v1"}
	}
	fullEndpoint := trim != "" && !strings.HasSuffix(trim, "#") && base != "" && base != strings.TrimSuffix(trim, "/")
	amber := tokens.Tailwind("amber-600")
	if l.Dark {
		amber = tokens.Tailwind("amber-400")
	}

	ui.Column(c).FillWidth().Gap(l.Space(2)).MarginTop(l.Space(2)).PadSides(l.Space(3)+1, l.Space(3)+1, l.Space(3)+1, l.Space(3)+1).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.Muted.WithAlpha(0.2), Border: t.Border.WithAlpha(0.6), BorderW: 1, Radius: l.Rem(0.75)})
		}).Children(func() {
		l.Text(c.Key("guide"), locales.SettingsAiWorkbenchApiUrlGuide(), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
		a.guideLine(c.Key("examples"), locales.SettingsAiWorkbenchApiUrlExamplesLabel()+":", strings.Join(examples, "  ·  "), t.MutedForeground, true)
		if base != "" {
			a.guideLine(c.Key("base"), locales.SettingsAiWorkbenchApiUrlBaseLabel()+":", base, t.Primary, false)
		}
		if request != "" {
			a.guideLine(c.Key("request"), locales.SettingsAiWorkbenchApiUrlRequestLabel()+":", request, t.Primary, false)
		}
		if strings.HasSuffix(trim, "#") {
			ui.Row(c.Key("hash")).PadSides(l.Space(1), l.Space(2), l.Space(1), l.Space(2)).
				Draw(func(d *ui.DrawContext) {
					l.PaintSurface(d, kit.Surface{Bg: amber.WithAlpha(0.1), Border: amber.WithAlpha(0.2), BorderW: 1, Radius: mdRadius(l)})
				}).Children(func() {
				l.TextPx(c.Key("text"), locales.SettingsAutoFillDisabled(), 11, 16.5, kit.WeightNormal, amber, kit.Wrapped())
			})
		}
		if fullEndpoint || services.NormalizeAPIURL(raw) != trim {
			l.TextPx(c.Key("full"), locales.SettingsAiWorkbenchApiUrlDetectedFullEndpoint(), 11, 16.5, kit.WeightNormal, amber, kit.Wrapped())
		}
	})
}

// guideLine 是说明卡里的一行：加粗的标签加等宽的值。inline 为真时同行（示例），否则值在标签下面（flex-col）。
func (a *AI) guideLine(c ui.C, label, value string, valueInk tokens.Color, inline bool) {
	l := a.Look
	t := l.T
	box := ui.Column(c).FillWidth().Gap(l.Space(1))
	if inline {
		box = ui.Row(c).FillWidth().Gap(l.Space(1))
	}
	box.Children(func() {
		l.Text(c.Key("label"), label, kit.TextXS, kit.WeightMedium, t.Foreground).Shrink(0)
		ui.Text(c.Key("value"), value).Mono().MaxLines(0).FontSize(l.Rem(0.75)).LineHeight(l.Rem(1)).Color(l.Col(valueInk)).MinWidth(0).Shrink(1)
	})
}
