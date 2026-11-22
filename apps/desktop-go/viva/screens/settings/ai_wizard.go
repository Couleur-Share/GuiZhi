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

// 「AI 快速配置」向导（AIQuickSetupWizard）：目标 → 服务 → 模型 → 验证，四步，最后一次性整体保存。

type wizardTest uint8

const (
	testIdle wizardTest = iota
	testRunning
	testSuccess
	testFailed
	testSkipped
)

// wizardPreset 是第二步的服务预设。
type wizardPreset struct {
	id       string
	label    func() string
	url      string
	protocol string
}

var wizardPresets = []wizardPreset{
	{"openai", func() string { return "OpenAI" }, "https://api.openai.com/v1", services.AIProtocolOpenAI},
	{"anthropic", func() string { return "Anthropic" }, "https://api.anthropic.com", services.AIProtocolAnthropic},
	{"gemini", func() string { return "Gemini" }, "https://generativelanguage.googleapis.com/v1beta", services.AIProtocolGemini},
	{"local", locales.SettingsQuickSetupProviderLocal, "http://127.0.0.1:11434/v1", services.AIProtocolOpenAI},
	{"custom", locales.SettingsQuickSetupProviderCustom, "", services.AIProtocolOpenAI},
}

// wizardState 是向导的全部状态；关闭即丢弃。
type wizardState struct {
	step     int
	goals    map[services.QuickSetupGoal]bool
	preset   string
	protocol string
	name     ui.Field
	url      ui.Field
	key      ui.Field
	showKey  bool
	models   map[services.QuickSetupRole]*ui.Field
	modelIDs []string // 远端拉到的模型名；为空表示手填
	tests    map[services.QuickSetupRole]wizardTest
	fetching bool
	saving   bool
	err      string
	protoSel kit.SelectState
}

// openWizard 打开向导：默认只勾「基础问答」、服务取 OpenAI。
func (a *AI) openWizard() {
	w := &wizardState{
		goals:  map[services.QuickSetupGoal]bool{services.GoalBasic: true},
		models: map[services.QuickSetupRole]*ui.Field{}, tests: map[services.QuickSetupRole]wizardTest{},
	}
	for _, r := range []services.QuickSetupRole{services.RoleText, services.RoleEmbedding, services.RoleVision, services.RoleAudio, services.RoleImageGen} {
		w.models[r] = &ui.Field{}
	}
	w.selectPreset("openai")
	a.wizard = w
}

func (w *wizardState) roles() []services.QuickSetupRole { return services.RolesForGoals(w.goals) }

func (w *wizardState) selectPreset(id string) {
	for _, p := range wizardPresets {
		if p.id != id {
			continue
		}
		w.preset, w.protocol = id, p.protocol
		w.name.SetText(p.label())
		w.url.SetText(p.url)
		if id == "local" && w.key.Text() == "" {
			w.key.SetText("local") // 无需鉴权的本地服务：占位密钥
		}
	}
}

func roleLabel(r services.QuickSetupRole) string {
	switch r {
	case services.RoleEmbedding:
		return locales.SettingsQuickSetupSemantic()
	case services.RoleVision:
		return locales.SettingsAiScenarioOcr()
	case services.RoleAudio:
		return locales.SettingsAiScenarioTranscription()
	case services.RoleImageGen:
		return locales.SettingsAiScenarioIllustration()
	}
	return locales.SettingsQuickSetupBasic()
}

// wizardModelFor 是某角色用来测试的模型。
func (w *wizardState) testModel(r services.QuickSetupRole) services.AIModel {
	return services.AIModel{
		Provider: trimmed(w.name.Text()), APIProtocol: w.protocol, APIURL: services.NormalizeAPIURL(w.url.Text()),
		APIKey: trimmed(w.key.Text()), Model: trimmed(w.models[r].Text()), Capabilities: r.Capabilities(), Enabled: true,
	}
}

// wizardFetch 是第二步的「获取模型并继续」：缺地址或密钥给出提示；拉取失败也进入第三步（手填模型名）。
func (a *AI) wizardFetch(w *wizardState) {
	if trimmed(w.url.Text()) == "" || trimmed(w.key.Text()) == "" {
		w.err = locales.SettingsQuickSetupNeedEndpoint()
		return
	}
	w.fetching, w.err = true, ""
	ep := modelEndpoint{provider: trimmed(w.name.Text()), protocol: w.protocol, apiKey: trimmed(w.key.Text()), apiURL: w.url.Text()}
	a.fetchRemote(ep, func(res services.AIFetchModelsResult) {
		if a.wizard != w {
			return
		}
		w.fetching = false
		w.step = 2
		if !res.Success || len(res.Models) == 0 {
			w.err = firstNonEmptyStr(res.Error, locales.SettingsQuickSetupNoModels())
			return
		}
		w.modelIDs = w.modelIDs[:0]
		for _, m := range res.Models {
			w.modelIDs = append(w.modelIDs, m.ID)
		}
		for _, r := range w.roles() {
			w.models[r].SetText(services.RecommendModel(w.modelIDs, r))
		}
	})
}

// wizardTestRoles 依次测试各角色的模型（逐项，一项完成再测下一项）。文生图没有连通测试（真实出图会计费），直接标为未验证。
func (a *AI) wizardTestRoles(w *wizardState, roles []services.QuickSetupRole) {
	for len(roles) > 0 && (trimmed(w.models[roles[0]].Text()) == "" || roles[0] == services.RoleImageGen) {
		if roles[0] == services.RoleImageGen && trimmed(w.models[roles[0]].Text()) != "" {
			w.tests[roles[0]] = testSkipped
		}
		roles = roles[1:]
	}
	if len(roles) == 0 {
		return
	}
	role, rest := roles[0], roles[1:]
	w.tests[role] = testRunning
	m := w.testModel(role)
	a.runTest(m, func(out services.AITestOutcome) {
		if a.wizard != w {
			return
		}
		if out.Success {
			w.tests[role] = testSuccess
		} else {
			w.tests[role] = testFailed
			w.err = locales.SettingsQuickSetupTestFailed(roleLabel(role), connectionErrorMessage(redact(out.Error, m.APIKey)))
		}
		a.wizardTestRoles(w, rest)
	})
}

// wizardSave 一次性整体保存：每个角色都要有模型；失败时现有配置不变，原因显示在向导里。
func (a *AI) wizardSave(w *wizardState) {
	roles := w.roles()
	selections := map[services.QuickSetupRole]string{}
	for _, r := range roles {
		if trimmed(w.models[r].Text()) == "" {
			w.err = locales.SettingsQuickSetupModelRequired()
			return
		}
		selections[r] = trimmed(w.models[r].Text())
	}
	verified := map[services.QuickSetupRole]bool{}
	for _, r := range roles {
		verified[r] = w.tests[r] == testSuccess
	}
	name := trimmed(w.name.Text())
	in := services.BuildQuickSetupInput(services.QuickSetupProvider{
		Name: name, Provider: name, APIProtocol: w.protocol, APIURL: services.NormalizeAPIURL(w.url.Text()), APIKey: trimmed(w.key.Text()),
	}, roles, selections, verified)
	key := w.key.Text()
	w.saving, w.err = true, ""
	a.commitThen(a.cfg.ApplyQuickSetup(in, a.now()), "", func(err error) {
		if a.wizard != w {
			return
		}
		w.saving = false
		if err != nil {
			w.err = locales.SettingsQuickSetupSaveFailed(redact(err.Error(), key))
			return
		}
		a.wizard = nil
		a.notify(kit.ToastSuccess, locales.SettingsQuickSetupSaved())
	})
}

// wizardModal 声明向导弹窗；每帧都要经过。
func (a *AI) wizardModal(c ui.C) {
	l := a.Look
	w := a.wizard
	spec := kit.ModalSpec{Open: w != nil, Size: kit.ModalLG, CloseLabel: locales.CommonClose()}
	if w != nil {
		spec.Title = locales.SettingsQuickSetupTitle()
		spec.Body = func(c ui.C) { a.wizardBody(c, w) }
	}
	if l.Modal(c, &a.wzModal, spec) && (w == nil || !w.saving) {
		a.wizard = nil
	}
}

func (a *AI) wizardBody(c ui.C, w *wizardState) {
	l := a.Look
	t := l.T
	ui.Column(c).FillWidth().Gap(l.Space(5)).Children(func() {
		a.wizardProgress(c.Key("progress"), w)
		switch w.step {
		case 0:
			a.wizardGoals(c.Key("goals"), w)
		case 1:
			a.wizardService(c.Key("service"), w)
		case 2:
			a.wizardModels(c.Key("models"), w)
		default:
			a.wizardVerify(c.Key("verify"), w)
		}
		if w.err != "" {
			l.Text(c.Key("error"), w.err, kit.TextXS, kit.WeightNormal, t.Destructive, kit.Wrapped())
		}
		a.wizardFooter(c.Key("footer"), w)
	})
}

// wizardProgress 是四格进度条：当前格主色实心，已过的格 primary/10，其余 muted。
func (a *AI) wizardProgress(c ui.C, w *wizardState) {
	l := a.Look
	t := l.T
	labels := []string{locales.SettingsQuickSetupStepGoals(), locales.SettingsQuickSetupStepService(), locales.SettingsModel(), locales.SettingsFunasrStepVerify()}
	ui.Row(c).FillWidth().Gap(l.Space(2)).Children(func() {
		for i, label := range labels {
			bg, fg := t.Muted, t.MutedForeground
			switch {
			case w.step == i:
				bg, fg = t.Primary, t.PrimaryForeground
			case i < w.step:
				bg, fg = t.Primary.WithAlpha(0.1), t.Primary
			}
			ui.Row(c.Key(itoa(i))).Grow(1).Basis(0).MinWidth(0).Justify(ui.Center).PadSides(l.Space(1), l.Space(2), l.Space(1), l.Space(2)).
				Draw(func(d *ui.DrawContext) { l.PaintSurface(d, kit.Surface{Bg: bg, Radius: mdRadius(l)}) }).Children(func() {
				// JSX 里 {index + 1}. {label} 是三个相邻的文本节点（序号、「. 」、标签），各画各的，文字序列才与渲染层一致。
				ui.Row(c.Key("t")).Shrink(1).MinWidth(0).Children(func() {
					l.Text(c.Key("n"), itoa(i+1), kit.TextXS, kit.WeightNormal, fg).Shrink(0)
					l.Text(c.Key("dot"), ". ", kit.TextXS, kit.WeightNormal, fg).Shrink(0)
					l.Text(c.Key("label"), label, kit.TextXS, kit.WeightNormal, fg, kit.Ellipsis()).MinWidth(0)
				})
			})
		}
	})
}

// cellGrid 把 n 个单元按 cols 列排成等宽网格。
func cellGrid(l *kit.Look, c ui.C, cols, n int, gap float32, cell func(c ui.C, i int)) {
	ui.Column(c).FillWidth().Gap(gap).Children(func() {
		for start := 0; start < n; start += cols {
			ui.Row(c.Key("row" + itoa(start))).FillWidth().Gap(gap).Children(func() {
				for i := start; i < start+cols; i++ {
					if i >= n {
						ui.Row(c.Key("pad" + itoa(i))).Grow(1).Basis(0)
						continue
					}
					ui.Column(c.Key("cell" + itoa(i))).Grow(1).Basis(0).MinWidth(0).Children(func() { cell(c.Key("c"+itoa(i)), i) })
				}
			})
		}
	})
}

// wizardGoals 是第一步：目标卡（可多选）加「完整能力」。
func (a *AI) wizardGoals(c ui.C, w *wizardState) {
	l := a.Look
	t := l.T
	type goalDef struct {
		goal        services.QuickSetupGoal
		label, desc string
	}
	goals := []goalDef{
		{services.GoalBasic, locales.SettingsQuickSetupBasic(), locales.SettingsQuickSetupBasicDesc()},
		{services.GoalSemantic, locales.SettingsQuickSetupSemantic(), locales.SettingsQuickSetupSemanticDesc()},
		{services.GoalVision, locales.SettingsAiScenarioOcr(), locales.SettingsQuickSetupVisionDesc()},
		{services.GoalAudio, locales.SettingsAiScenarioTranscription(), locales.SettingsQuickSetupAudioDesc()},
		{services.GoalImageGen, locales.SettingsAiScenarioIllustration(), locales.SettingsQuickSetupImageGenDesc()},
	}
	cols := 1
	if l.ViewportWidth(c) >= 640 {
		cols = 2
	}
	cellGrid(l, c, cols, len(goals)+1, l.Space(2), func(c ui.C, i int) {
		if i == len(goals) {
			// 「完整能力」是 `rounded-xl border border-dashed border-border p-3 text-left text-sm` 的普通按钮：撑满网格行高，文字左对齐、纵向居中，
			// 没有悬停样式。
			name := locales.SettingsQuickSetupSelectAll()
			box := ui.Column(c).FillWidth().Grow(1).Justify(ui.Center).Pad(l.Space(3)+1).
				HitWith(ui.ID(c), name, gfx.HitOptions{Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand})
			l.TabStop(box)
			box.Draw(func(d *ui.DrawContext) {
				l.PaintDashedBorder(d, t.Border, 1, l.Rem(1))
			}).Children(func() {
				l.Text(c.Key("label"), name, kit.TextSM, kit.WeightNormal, t.Foreground)
			})
			if box.Clicked() {
				for _, g := range services.QuickSetupGoals {
					w.goals[g] = true
				}
			}
			return
		}
		g := goals[i]
		on := w.goals[g.goal]
		box := ui.Column(c).FillWidth().Grow(1).Pad(l.Space(3)+1).
			HitWith(ui.ID(c), g.label+" "+g.desc, gfx.HitOptions{Role: gfx.RoleButton, Selected: on, Cursor: gfx.PointerCursorHand})
		l.TabStop(box)
		surface := kit.Surface{Border: t.Border, BorderW: 1, Radius: l.Rem(1)}
		if on {
			surface.Border, surface.Bg = t.Primary, t.Primary.WithAlpha(0.05)
		}
		box.Draw(func(d *ui.DrawContext) { l.PaintSurface(d, surface) }).Children(func() {
			ui.Row(c.Key("title")).Items(ui.Center).Gap(l.Space(2)).Children(func() {
				ic, ink := icons.Circle, t.MutedForeground
				if on {
					ic, ink = icons.CircleCheck, t.Primary
				}
				l.Icon(c.Key("icon"), ic, l.Rem(1), ink)
				l.Text(c.Key("label"), g.label, kit.TextSM, kit.WeightMedium, t.Foreground)
			})
			l.Text(c.Key("desc"), g.desc, kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped()).MarginTop(l.Space(1))
		})
		if box.Clicked() {
			w.goals[g.goal] = !on
		}
	})
}

// wizardField 是向导里的文本框（`h-9 rounded-lg border border-border bg-background px-3 text-sm`）。
func (a *AI) wizardField(c ui.C, f *ui.Field, name, placeholder string) ui.Box {
	l := a.Look
	return aiField(l, c, f, aiFieldSpec{
		Name: name, Placeholder: placeholder, Height: l.Rem(2.25), Radius: l.Rem(0.75), Bg: l.T.Background, Border: l.T.Border, BorderW: 1,
		Left: l.Space(3), Right: l.Space(3), PlaceholderInk: l.T.MutedForeground,
	})
}

// labeled 是「标签在上、控件在下」的一项（`<label class="block text-xs">`，控件 mt-1）。
func (a *AI) labeled(c ui.C, label string, body func(c ui.C)) {
	l := a.Look
	ui.Column(c).FillWidth().Children(func() {
		l.Text(c.Key("label"), label, kit.TextXS, kit.WeightNormal, l.T.Foreground).MarginBottom(l.Space(1))
		body(c.Key("body"))
	})
}

// wizardService 是第二步：服务预设、名称、协议、地址与密钥。
func (a *AI) wizardService(c ui.C, w *wizardState) {
	l := a.Look
	t := l.T
	ui.Column(c).FillWidth().Gap(l.Space(3)).Children(func() {
		cols := 2
		if l.ViewportWidth(c) >= 640 {
			cols = 5
		}
		cellGrid(l, c.Key("presets"), cols, len(wizardPresets), l.Space(2), func(c ui.C, i int) {
			p := wizardPresets[i]
			st := kit.ButtonStyle{
				PadX: l.Space(2), PadY: l.Space(2), Radius: l.Rem(0.75), Text: kit.TextXS, Weight: kit.WeightNormal, Fg: t.Foreground,
				Border: t.Border, BorderOn: t.Primary, BgOn: t.Primary.WithAlpha(0.05), BorderW: 1, Transition: kit.DurationDefault,
				BgHover: t.Muted.WithAlpha(0.3),
			}
			if l.Button(c, kit.ButtonSpec{Label: p.label(), Toggle: true, On: w.preset == p.id, Full: true}, st).Clicked() {
				w.selectPreset(p.id)
			}
		})
		a.labeled(c.Key("name"), locales.SettingsQuickSetupServiceName(), func(c ui.C) {
			a.wizardField(c, &w.name, locales.SettingsQuickSetupServiceName(), "")
		})
		cols2 := 1
		if l.ViewportWidth(c) >= 640 {
			cols2 = 2
		}
		row := ui.Row(c.Key("endpoint"))
		if cols2 == 1 {
			row = ui.Column(c.Key("endpoint"))
		}
		row.FillWidth().Gap(l.Space(3)).Items(ui.Start).Children(func() {
			ui.Column(c.Key("protocol")).Width(l.Rem(9)).Shrink(0).Children(func() {
				a.labeled(c.Key("l"), locales.SettingsProtocol(), func(c ui.C) {
					if picked, changed := l.Select(c, &w.protoSel, kit.SelectSpec{
						Label: locales.SettingsProtocol(), Value: w.protocol, Width: -1,
						Options: []kit.SelectOption{
							{Value: services.AIProtocolOpenAI, Label: "OpenAI"},
							{Value: services.AIProtocolAnthropic, Label: "Anthropic"},
							{Value: services.AIProtocolGemini, Label: "Gemini"},
						},
					}); changed {
						w.protocol = picked
					}
				})
			})
			ui.Column(c.Key("url")).Grow(1).Basis(0).MinWidth(0).Children(func() {
				a.labeled(c.Key("l"), locales.SettingsApiUrl(), func(c ui.C) { a.wizardField(c, &w.url, locales.SettingsApiUrl(), "") })
			})
		})
		a.labeled(c.Key("key"), "API Key", func(c ui.C) {
			l.SettingsPasswordInput(c, &w.key, &w.showKey, kit.SettingsInputSpec{Name: "API Key", Placeholder: locales.SettingsApiKeyPlaceholder()},
				locales.CommonShowPassword(), locales.CommonHidePassword())
		})
	})
}

// wizardModels 是第三步：每个角色一个模型名输入框，拉到列表时给出推荐。
func (a *AI) wizardModels(c ui.C, w *wizardState) {
	l := a.Look
	t := l.T
	ui.Column(c).FillWidth().Gap(l.Space(3)).Children(func() {
		for _, r := range w.roles() {
			placeholder := locales.SettingsQuickSetupModelPlaceholder()
			if len(w.modelIDs) > 0 {
				placeholder = locales.SettingsQuickSetupRecommended(services.RecommendModel(w.modelIDs, r))
			}
			ui.Column(c.Key(string(r))).FillWidth().Children(func() {
				ui.Row(c.Key("head")).Items(ui.Center).Gap(l.Space(2)).MarginBottom(l.Space(1)).Children(func() {
					l.Text(c.Key("role"), roleLabel(r), kit.TextSM, kit.WeightMedium, t.Foreground)
					l.Text(c.Key("routes"), strings.Join(r.Routes(), " / "), kit.TextXS, kit.WeightNormal, t.MutedForeground)
				})
				a.wizardField(c.Key("field"), w.models[r], roleLabel(r), placeholder)
			})
		}
		if len(w.modelIDs) == 0 {
			amber := tokens.Tailwind("amber-700")
			if l.Dark {
				amber = tokens.Tailwind("amber-300")
			}
			l.Text(c.Key("manual"), locales.SettingsQuickSetupManualModels(), kit.TextXS, kit.WeightNormal, amber, kit.Wrapped())
		}
	})
}

// wizardVerify 是第四步：每个角色一行状态，可逐项测试或跳过。
func (a *AI) wizardVerify(c ui.C, w *wizardState) {
	l := a.Look
	t := l.T
	ui.Column(c).FillWidth().Gap(l.Space(2)).Children(func() {
		for _, r := range w.roles() {
			state := w.tests[r]
			ui.Row(c.Key(string(r))).FillWidth().Items(ui.Center).Gap(l.Space(3)).PadSides(l.Space(2)+1, l.Space(3)+1, l.Space(2)+1, l.Space(3)+1).
				Draw(func(d *ui.DrawContext) {
					l.PaintSurface(d, kit.Surface{Border: t.Border, BorderW: 1, Radius: l.Rem(0.75)})
				}).Children(func() {
				ui.Row(c.Key("text")).Grow(1).MinWidth(0).Items(ui.Center).Gap(l.Space(2)).Children(func() {
					l.Text(c.Key("role"), roleLabel(r), kit.TextSM, kit.WeightSemibold, t.Foreground).Shrink(0)
					l.Text(c.Key("model"), trimmed(w.models[r].Text()), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Ellipsis()).MinWidth(0)
				})
				if state == testRunning {
					l.Spinner(c.Key("spin"), l.Rem(1), t.Foreground)
					return
				}
				label, ink := locales.SettingsQuickSetupUntested(), t.MutedForeground
				switch state {
				case testSuccess:
					label, ink = locales.SettingsQuickSetupVerified(), tokens.Tailwind("emerald-600")
				case testFailed:
					label, ink = locales.SettingsQuickSetupFailed(), t.Destructive
				case testSkipped:
					label = locales.SettingsAiWorkbenchUnverified()
				}
				l.Text(c.Key("state"), label, kit.TextXS, kit.WeightNormal, ink).Shrink(0)
			})
		}
		l.Text(c.Key("hint"), locales.SettingsQuickSetupTestHint(), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
		ui.Row(c.Key("actions")).Gap(l.Space(2)).Children(func() {
			st := outlineBtn(l, shape(l, 0, 3, 0, kit.TextXS, kit.WeightNormal), t.Muted)
			st.Height, st.PadY = 0, l.Space(2)
			if btn(l, c.Key("each"), kit.ButtonSpec{Label: locales.SettingsQuickSetupTestEach()}, st, false) {
				w.err = ""
				a.wizardTestRoles(w, w.roles())
			}
			if btn(l, c.Key("skip"), kit.ButtonSpec{Label: locales.SettingsQuickSetupSkipTests()}, st, false) {
				for _, r := range w.roles() {
					w.tests[r] = testSkipped
				}
			}
		})
	})
}

// wizardFooter 是页脚：上一步，以及下一步 / 获取模型并继续 / 一次性保存。
func (a *AI) wizardFooter(c ui.C, w *wizardState) {
	l := a.Look
	t := l.T
	ui.Row(c).FillWidth().Items(ui.Center).Justify(ui.Between).PadSides(l.Space(4)+1, 0, 0, 0).
		Draw(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeTop, 1, t.Border) }).Children(func() {
		// py-2 + text-sm 的 1.25rem 行高：描边的「上一步」多 2px 边框（38），主色的「下一步」「保存」没有边框（36）。
		back := outlineBtn(l, shape(l, 2.25, 4, 2, kit.TextSM, kit.WeightNormal), t.Muted)
		back.Height += 2 // 2px 边框不随字号档缩放
		back.DisabledOpacity = 0.4
		if btn(l, c.Key("back"), kit.ButtonSpec{Label: locales.SettingsQuickSetupBack(), Disabled: w.step == 0 || w.saving}, back, false) {
			w.step = max(0, w.step-1)
		}
		primary := primaryBtn(l, shape(l, 2.25, 4, 2, kit.TextSM, kit.WeightNormal))
		if w.step < 3 {
			label := locales.SettingsQuickSetupNext()
			if w.step == 1 {
				label = locales.SettingsQuickSetupFetchModels()
			}
			spec := kit.ButtonSpec{Label: label, Icon: icons.WandSparkles, IconSize: l.Rem(1), BusySize: l.Rem(1), Disabled: len(w.goals) == 0 || w.fetching}
			if btn(l, c.Key("next"), spec, primary, w.fetching) {
				if w.step == 1 {
					a.wizardFetch(w)
				} else {
					w.step++
				}
			}
			return
		}
		label := locales.SettingsQuickSetupSaveAll()
		if w.saving {
			label = locales.LibrarySaving()
		}
		if btn(l, c.Key("save"), kit.ButtonSpec{Label: label, Disabled: w.saving}, primary, false) {
			a.wizardSave(w)
		}
	})
}
