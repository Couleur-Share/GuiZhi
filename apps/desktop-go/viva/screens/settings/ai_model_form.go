package settings

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// modelEndpoint 是模型表单与拉取弹窗继承自端点的连接字段：渲染层的这两个入口都锁定端点字段（lockEndpointFields），
// 用户只填模型名与能力，所以这些字段不出现在界面上，只随模型一起保存。
type modelEndpoint struct {
	providerID, provider, protocol, apiKey, apiURL string
}

func endpointOf(g services.AIEndpointGroup) modelEndpoint {
	return modelEndpoint{
		providerID: g.ProviderConfigID, provider: g.Provider, protocol: g.APIProtocol,
		apiKey: g.EffectiveAPIKey(), apiURL: g.APIURL,
	}
}

// modelForm 是「添加模型 / 编辑模型」弹窗的草稿（ModelFormState）。
type modelForm struct {
	modelEndpoint
	editingID   string // 空表示添加
	name, model ui.Field
	caps        services.AICapabilities
}

// draftKey 是草稿测试的忙碌标识：编辑中用模型 id，添加用占位。
func (f *modelForm) draftKey() string {
	if f.editingID != "" {
		return f.editingID
	}
	return "__draft__"
}

// openAddModel 打开「添加模型」，端点字段取自分组。
func (a *AI) openAddModel(g services.AIEndpointGroup) {
	a.model = &modelForm{modelEndpoint: endpointOf(g), caps: services.DefaultAICapabilities()}
}

// openEditModel 打开「编辑模型」。
func (a *AI) openEditModel(m services.AIModel) {
	f := &modelForm{
		modelEndpoint: modelEndpoint{providerID: m.ProviderID, provider: m.Provider, protocol: m.APIProtocol, apiKey: m.APIKey, apiURL: m.APIURL},
		editingID:     m.ID, caps: m.Capabilities,
	}
	f.name.SetText(m.Name)
	f.model.SetText(m.Model)
	a.model = f
}

// draftModel 把草稿拼成一个可测试的模型。
func (f *modelForm) draftModel() services.AIModel {
	return services.AIModel{
		ProviderID: f.providerID, Provider: trimmed(f.provider), APIProtocol: f.protocol,
		APIKey: trimmed(f.apiKey), APIURL: f.apiURL, Model: trimmed(f.model.Text()), Name: trimmed(f.name.Text()),
		Capabilities: f.caps, Enabled: true,
	}
}

// testDraft 测试草稿：缺密钥、地址或模型名时直接拒绝。
func (a *AI) testDraft() {
	f := a.model
	if f == nil {
		return
	}
	if trimmed(f.apiKey) == "" || trimmed(f.apiURL) == "" || trimmed(f.model.Text()) == "" {
		a.Guard.Reject(locales.SettingsFillComplete())
		return
	}
	m := f.draftModel()
	name := firstNonEmptyStr(m.Name, m.Model, "AI")
	a.testingModel = f.draftKey()
	a.runTest(m, func(out services.AITestOutcome) {
		a.testingModel = ""
		a.reportTest(name, out)
	})
}

// saveModel 保存草稿：缺任何一项都拒绝；添加与编辑分别提示。
func (a *AI) saveModel() {
	f := a.model
	if f == nil {
		return
	}
	if trimmed(f.provider) == "" || trimmed(f.apiKey) == "" || trimmed(f.apiURL) == "" || trimmed(f.model.Text()) == "" {
		a.Guard.Reject(locales.SettingsFillComplete())
		return
	}
	m := f.draftModel()
	m.APIURL = services.NormalizeAPIURL(f.apiURL)
	a.model = nil
	if f.editingID == "" {
		next, _ := a.cfg.AddModel(m)
		a.commit(next, locales.SettingsModelAdded())
		return
	}
	next := a.cfg.UpdateModel(f.editingID, func(mm *services.AIModel) {
		mm.Name, mm.ProviderID, mm.Provider, mm.APIProtocol = m.Name, m.ProviderID, m.Provider, m.APIProtocol
		mm.APIKey, mm.APIURL, mm.Model, mm.Capabilities = m.APIKey, m.APIURL, m.Model, m.Capabilities
	})
	a.commit(next, locales.SettingsModelUpdated())
}

// modelModal 声明「添加/编辑模型」弹窗；每帧都要经过。
func (a *AI) modelModal(c ui.C) {
	l := a.Look
	f := a.model
	spec := kit.ModalSpec{Open: f != nil, Size: kit.ModalXL, CloseLabel: locales.CommonClose()}
	if f != nil {
		spec.Title = locales.SettingsAddModel()
		if f.editingID != "" {
			spec.Title = locales.SettingsAiWorkbenchEditModel()
		}
		sub := locales.SettingsAiWorkbenchModelModalSubtitle()
		spec.SubtitleText = sub
		spec.Subtitle = func(c ui.C) { l.Text(c, sub, kit.TextXS, kit.WeightNormal, l.T.MutedForeground, kit.Wrapped()) }
		spec.Body = func(c ui.C) { a.modelBody(c, f) }
	}
	if l.Modal(c, &a.mdModal, spec) {
		a.model = nil
	}
}

func (a *AI) modelBody(c ui.C, f *modelForm) {
	l := a.Look
	t := l.T
	ui.Column(c).FillWidth().Gap(l.Space(4)).Children(func() {
		ui.Column(c.Key("name")).FillWidth().Children(func() {
			fieldLabel(l, c.Key("label"), locales.SettingsCustomNameOptional())
			modalField(l, c.Key("field"), &f.name, locales.SettingsCustomNameOptional(), locales.SettingsCustomNamePlaceholder())
		})
		ui.Column(c.Key("model")).FillWidth().Children(func() {
			fieldLabel(l, c.Key("label"), locales.SettingsModelName())
			modalField(l, c.Key("field"), &f.model, locales.SettingsModelName(), locales.SettingsModelNamePlaceholder())
		})
		a.capabilityPanel(c.Key("caps"), f)
		ui.Row(c.Key("footer")).FillWidth().Items(ui.Center).Justify(ui.Between).PadSides(l.Space(4)+1, 0, 0, 0).
			Draw(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeTop, 1, t.Border) }).Children(func() {
			b := shape(l, 2.25, 4, 2, kit.TextSM, kit.WeightNormal)
			test := outlineBtn(l, b, t.Muted)
			test.Bg = t.Background
			if btn(l, c.Key("test"), kit.ButtonSpec{Label: locales.SettingsAiWorkbenchTestDraft(), Icon: icons.TestTube, IconSize: l.Rem(1), BusySize: l.Rem(1)}, test, a.testingModel == f.draftKey()) {
				a.testDraft()
			}
			ui.Row(c.Key("actions")).Items(ui.Center).Gap(l.Space(2)).Children(func() {
				if btn(l, c.Key("cancel"), kit.ButtonSpec{Label: locales.CommonCancel()}, outlineBtn(l, b, t.Muted), false) {
					a.model = nil
				}
				label := locales.SettingsAddModel()
				if f.editingID != "" {
					label = locales.SettingsSaveChanges()
				}
				if btn(l, c.Key("save"), kit.ButtonSpec{Label: label}, primaryBtn(l, shape(l, 2.25, 4, 2, kit.TextSM, kit.WeightMedium)), false) {
					a.saveModel()
				}
			})
		})
	})
}

// capabilityPanel 是「模型能力」面板：说明加五张能力卡（lg 两列）。
func (a *AI) capabilityPanel(c ui.C, f *modelForm) {
	l := a.Look
	t := l.T
	wide := l.ViewportWidth(c) >= 1024
	type capDef struct {
		key, title, desc string
		icon             gfx.Icon
		on               bool
		apply            func(services.AICapabilities, bool) services.AICapabilities
	}
	defs := []capDef{
		{"vision", locales.SettingsAiWorkbenchVisionCapability(), locales.SettingsAiWorkbenchVisionCapabilityDesc(), icons.Eye, f.caps.Vision, services.AICapabilities.ToggleVision},
		{"reasoning", locales.SettingsAiWorkbenchReasoningCapability(), locales.SettingsAiWorkbenchReasoningCapabilityDesc(), icons.Brain, f.caps.Reasoning, services.AICapabilities.ToggleReasoning},
		{"embedding", locales.SettingsAiWorkbenchEmbeddingCapability(), locales.SettingsAiWorkbenchEmbeddingCapabilityDesc(), icons.Database, f.caps.Embedding, services.AICapabilities.ToggleEmbedding},
		{"transcription", locales.SettingsAiWorkbenchTranscriptionCapability(), locales.SettingsAiWorkbenchTranscriptionCapabilityDesc(), icons.AudioLines, f.caps.AudioTranscription, services.AICapabilities.ToggleAudioTranscription},
		{"imagegen", locales.SettingsAiWorkbenchImageGenCapability(), locales.SettingsAiWorkbenchImageGenCapabilityDesc(), icons.Image, f.caps.ImageGeneration, services.AICapabilities.ToggleImageGeneration},
	}
	ui.Column(c).FillWidth().Pad(l.Space(3) + 1).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.Muted.WithAlpha(0.2), Border: t.Border.WithAlpha(0.6), BorderW: 1, Radius: l.Rem(0.75)})
		}).Children(func() {
		l.Text(c.Key("title"), locales.SettingsAiWorkbenchModelCapabilities(), kit.TextXS, kit.WeightMedium, t.MutedForeground).MarginBottom(l.Space(1))
		l.Text(c.Key("hint"), locales.SettingsAiWorkbenchModelCapabilitiesHint(), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped()).MarginBottom(l.Space(3))
		cols := 1
		if wide {
			cols = 2
		}
		ui.Column(c.Key("grid")).FillWidth().Gap(l.Space(2)).Children(func() {
			for start := 0; start < len(defs); start += cols {
				ui.Row(c.Key("row" + itoa(start))).FillWidth().Gap(l.Space(2)).Children(func() {
					for i := start; i < start+cols; i++ {
						if i >= len(defs) {
							ui.Row(c.Key("pad")).Grow(1).Basis(0)
							continue
						}
						d := defs[i]
						if on := a.capabilityCard(c.Key(d.key), d.title, d.desc, d.icon, d.on); on != d.on {
							f.caps = d.apply(f.caps, on)
						}
					}
				})
			}
		})
	})
}

// capabilityCard 是一张能力卡（`rounded-md border border-border/60 bg-background px-3 py-2`）：复选框、图标、标题与说明；
// 点卡片任何位置都切换。返回新的勾选值。
func (a *AI) capabilityCard(c ui.C, title, desc string, ic gfx.Icon, checked bool) bool {
	l := a.Look
	t := l.T
	on := checked
	cardID := ui.ID(c)
	if c.Canvas().Clicked(cardID) {
		on = !on
	}
	ui.Row(c).Grow(1).Basis(0).MinWidth(0).Items(ui.Start).Gap(l.Space(3)).
		PadSides(l.Space(2)+1, l.Space(3)+1, l.Space(2)+1, l.Space(3)+1).
		HitWith(cardID, "", gfx.HitOptions{SkipFocus: true, Cursor: gfx.PointerCursorHand}).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.Background, Border: t.Border.WithAlpha(0.6), BorderW: 1, Radius: mdRadius(l)})
		}).Children(func() {
		l.Checkbox(c.Key("check"), &on, kit.CheckboxSpec{Name: title}).MarginTop(l.Space(1))
		l.Icon(c.Key("icon"), ic, l.Rem(1), t.MutedForeground).MarginTop(l.Space(0.5)).Shrink(0)
		ui.Column(c.Key("text")).Shrink(1).MinWidth(0).Children(func() {
			l.Text(c.Key("title"), title, kit.TextSM, kit.WeightMedium, t.Foreground)
			l.Text(c.Key("desc"), desc, kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped()).MarginTop(l.Space(0.5))
		})
	})
	return on
}
