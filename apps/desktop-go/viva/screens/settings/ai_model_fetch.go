package settings

import (
	"context"
	"sort"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// fetchState 是「拉取模型」弹窗的状态：先转圈等远端列表，成功后是可搜索、按家族分组、可批量勾选的列表。
type fetchState struct {
	modelEndpoint
	loading   bool
	models    []services.AIRemoteModel
	selected  map[string]bool
	collapsed map[string]bool
	search    ui.Field
}

// modelCategoryOrder 是分组的显示顺序（CATEGORY_ORDER）；不在表里的家族排在后面、按名称排序。
var modelCategoryOrder = []string{
	"GPT", "Claude", "Gemini", "DeepSeek", "Qwen", "StepFun", "MiniMax", "Doubao", "GLM", "Moonshot", "Baichuan",
	"Grok", "Command", "Llama", "Gemma", "Mistral", "Yi", "ERNIE", "Spark", "Hunyuan", "InternLM", "Phi", "Nova",
	"Jamba", "Sonar", "Embedding", "Audio", "Image", "Other",
}

func categoryRank(name string) int {
	for i, c := range modelCategoryOrder {
		if c == name {
			return i
		}
	}
	return -1
}

// fetchFeedback 把拉取失败按原因翻成提示的种类与文案（getFetchModelsFeedback）：
// 空列表与网络问题是警告，认证是错误，端点不支持列表是信息；其余是带原因的错误。
func fetchFeedback(r services.AIFetchModelsResult) (kit.ToastKind, string) {
	if r.Success {
		return kit.ToastWarning, locales.SettingsAiWorkbenchFetchModelsEmpty()
	}
	switch r.Reason {
	case services.AIFetchReasonAuth:
		return kit.ToastError, locales.SettingsAiWorkbenchFetchModelsAuthError()
	case services.AIFetchReasonUnsupported, services.AIFetchReasonParse:
		return kit.ToastInfo, locales.SettingsAiWorkbenchFetchModelsUnsupported()
	case services.AIFetchReasonNetwork:
		msg := connectionErrorMessage(r.Error)
		if msg == r.Error || strings.TrimSpace(msg) == "" {
			msg = locales.SettingsAiWorkbenchFetchModelsNetworkError()
		}
		return kit.ToastWarning, msg
	}
	return kit.ToastError, firstNonEmptyStr(r.Error, locales.SettingsAiWorkbenchFetchModelsFailed())
}

// fetchRemote 经 Go 壳（SSRF 防护）拉取远端模型列表，结果交给 done。密钥只用于这次请求，绝不进错误文字。
func (a *AI) fetchRemote(ep modelEndpoint, done func(services.AIFetchModelsResult)) {
	svc := a.Service
	a.w.load(a.Wake, func(ctx context.Context) func() {
		res, err := services.AIFetchModelsResult{}, errAIUnavailable
		if svc != nil {
			res, err = svc.FetchModels(ctx, ep.apiURL, ep.apiKey, ep.protocol)
		}
		if err != nil {
			res = services.AIFetchModelsResult{Error: redact(err.Error(), ep.apiKey)}
			if ctx.Err() == nil {
				a.Guard.Background(locales.SettingsFetchModels(), err)
			}
		}
		res.Error = redact(res.Error, ep.apiKey)
		return func() { done(res) }
	})
}

// openFetchModels 打开「拉取模型」弹窗并立刻开始拉取；失败时按原因提示并关掉弹窗，成功后展示列表。
func (a *AI) openFetchModels(g services.AIEndpointGroup) {
	ep := endpointOf(g)
	if trimmed(ep.apiKey) == "" || trimmed(ep.apiURL) == "" {
		a.Guard.Reject(locales.SettingsFillApiFirst())
		return
	}
	f := &fetchState{modelEndpoint: ep, loading: true, selected: map[string]bool{}, collapsed: map[string]bool{}}
	a.fetch = f
	a.fetchRemote(ep, func(res services.AIFetchModelsResult) {
		if a.fetch != f {
			return // 弹窗已被关掉
		}
		f.loading = false
		if !res.Success || len(res.Models) == 0 {
			kind, msg := fetchFeedback(res)
			a.notify(kind, msg)
			a.fetch = nil
			return
		}
		f.models = res.Models
		a.notify(kit.ToastSuccess, locales.SettingsModelsLoaded(len(res.Models)))
	})
}

// batchAddModels 把勾选的模型批量加进端点：名称留空，能力按模型名推断。
func (a *AI) batchAddModels() {
	f := a.fetch
	if f == nil || len(f.selected) == 0 {
		return
	}
	if trimmed(f.provider) == "" || trimmed(f.apiKey) == "" || trimmed(f.apiURL) == "" {
		a.Guard.Reject(locales.SettingsFillApiFirst())
		return
	}
	var ids []string
	for _, m := range f.models { // 按列表顺序加，结果稳定
		if f.selected[m.ID] {
			ids = append(ids, m.ID)
		}
	}
	next := a.cfg
	for _, id := range ids {
		next, _ = next.AddModel(services.AIModel{
			ProviderID: f.providerID, Provider: trimmed(f.provider), APIProtocol: f.protocol, APIKey: trimmed(f.apiKey),
			APIURL: services.NormalizeAPIURL(f.apiURL), Model: id, Capabilities: services.InferModelCapabilities(id), Enabled: true,
		})
	}
	a.fetch = nil
	a.commit(next, locales.SettingsModelAdded()+" ("+itoa(len(ids))+")")
}

// fetchModal 声明「拉取模型」弹窗；每帧都要经过。
func (a *AI) fetchModal(c ui.C) {
	l := a.Look
	f := a.fetch
	spec := kit.ModalSpec{Open: f != nil, Size: kit.ModalXL, CloseLabel: locales.CommonClose()}
	if f != nil {
		spec.Title = locales.SettingsFetchModels()
		spec.Body = func(c ui.C) { a.fetchBody(c, f) }
	}
	if l.Modal(c, &a.ftModal, spec) {
		a.fetch = nil
	}
}

func (a *AI) fetchBody(c ui.C, f *fetchState) {
	l := a.Look
	t := l.T
	ui.Column(c).FillWidth().Gap(l.Space(4)).Children(func() {
		if f.loading {
			ui.Row(c.Key("loading")).FillWidth().Height(l.Rem(20)).Items(ui.Center).Justify(ui.Center).Gap(l.Space(2)).
				Draw(func(d *ui.DrawContext) {
					l.PaintSurface(d, kit.Surface{Bg: t.Muted.WithAlpha(0.2), Border: t.Border.WithAlpha(0.6), BorderW: 1, Radius: l.Rem(1)})
				}).Children(func() {
				l.Spinner(c.Key("spin"), l.Rem(1), t.MutedForeground)
				l.Text(c.Key("text"), locales.CommonLoading(), kit.TextSM, kit.WeightNormal, t.MutedForeground)
			})
		} else {
			a.modelList(c.Key("list"), f)
		}
		ui.Row(c.Key("footer")).FillWidth().Items(ui.Center).Justify(ui.End).Gap(l.Space(2)).PadSides(l.Space(4)+1, 0, 0, 0).
			Draw(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeTop, 1, t.Border) }).Children(func() {
			b := shape(l, 2.25, 4, 2, kit.TextSM, kit.WeightNormal)
			if btn(l, c.Key("cancel"), kit.ButtonSpec{Label: locales.CommonCancel()}, outlineBtn(l, b, t.Muted), false) {
				a.fetch = nil
			}
			n := len(f.selected)
			label := locales.SettingsAddModel()
			if n > 1 {
				label = locales.SettingsAddNModels(n)
			}
			add := primaryBtn(l, shape(l, 2.25, 4, 2, kit.TextSM, kit.WeightMedium))
			if btn(l, c.Key("add"), kit.ButtonSpec{Label: label, Disabled: f.loading || n == 0}, add, false) {
				a.batchAddModels()
			}
		})
	})
}

// modelList 是远端模型列表：头部（标题、数量、搜索框）加可滚动的分组列表。
func (a *AI) modelList(c ui.C, f *fetchState) {
	l := a.Look
	t := l.T
	query := strings.ToLower(trimmed(f.search.Text()))
	groups := map[string][]services.AIRemoteModel{}
	shown := 0
	for _, m := range f.models {
		if query != "" && !strings.Contains(strings.ToLower(m.ID), query) && !strings.Contains(strings.ToLower(m.OwnedBy), query) {
			continue
		}
		cat := services.ModelCategory("", m.ID, m.OwnedBy)
		groups[cat] = append(groups[cat], m)
		shown++
	}
	cats := make([]string, 0, len(groups))
	for k := range groups {
		cats = append(cats, k)
	}
	sort.SliceStable(cats, func(i, j int) bool {
		ri, rj := categoryRank(cats[i]), categoryRank(cats[j])
		switch {
		case ri < 0 && rj < 0:
			return cats[i] < cats[j]
		case ri < 0:
			return false
		case rj < 0:
			return true
		}
		return ri < rj
	})

	ui.Column(c).FillWidth().Draw(func(d *ui.DrawContext) {
		l.PaintSurface(d, kit.Surface{Bg: t.Muted.WithAlpha(0.2), Border: t.Border, BorderW: 1, Radius: l.Rem(1)})
	}).Children(func() {
		// 头部
		ui.Column(c.Key("head")).FillWidth().PadSides(l.Space(2.5)+1, l.Space(3)+1, l.Space(2.5)+1, l.Space(3)+1).
			Draw(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeBottom, 1, t.Border.WithAlpha(0.6)) }).Children(func() {
			ui.Row(c.Key("title")).FillWidth().Items(ui.Center).Justify(ui.Between).MarginBottom(l.Space(2)).Children(func() {
				l.Text(c.Key("label"), locales.SettingsSelectModels(), kit.TextXS, kit.WeightMedium, t.MutedForeground)
				ui.Row(c.Key("counts")).Items(ui.Center).Gap(l.Space(1.5)).Children(func() {
					total := locales.SettingsTotalModels(len(f.models))
					if query != "" && shown != len(f.models) {
						total += " · " + itoa(shown)
					}
					l.TextPx(c.Key("total"), total, 11, 16.5, kit.WeightNormal, t.MutedForeground)
					if n := len(f.selected); n > 0 {
						ui.Row(c.Key("sel")).PadSides(l.Space(0.5), l.Space(1.5), l.Space(0.5), l.Space(1.5)).
							Draw(func(d *ui.DrawContext) {
								l.PaintSurface(d, kit.Surface{Bg: t.Primary.WithAlpha(0.1), Radius: l.Rem(0.25)})
							}).Children(func() {
							l.TextPx(c.Key("n"), locales.SettingsSelectedModels(n), 11, 16.5, kit.WeightNormal, t.Primary)
						})
					}
				})
			})
			aiField(l, c.Key("search"), &f.search, aiFieldSpec{
				Name: locales.SettingsSearchModels(), Placeholder: locales.SettingsSearchModels(), Height: l.Rem(2), Radius: l.Rem(0.75),
				Bg: t.Background, Left: l.Rem(2), Right: l.Space(3), Size: l.Rem(0.75), PlaceholderInk: t.MutedForeground,
				Overlay: func(c ui.C) {
					ui.Row(c.Key("icon")).Absolute().Left(l.Rem(0.625)).Top(0).Bottom(0).Items(ui.Center).Children(func() {
						l.Icon(c.Key("i"), icons.Search, l.Rem(0.875), t.MutedForeground)
					})
				},
			})
		})
		// 列表
		l.ScrollArea(c.Key("scroll"), [4]float32{l.Space(1.5), l.Space(1.5), l.Space(1.5), l.Space(1.5)}, func() {
			ui.Column(c.Key("cats")).FillWidth().Gap(l.Space(1)).Children(func() {
				if len(cats) == 0 {
					l.Text(c.Key("none"), locales.SettingsNoModelsMatch(), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Centered()).
						FillWidth().PadSides(l.Space(6), 0, l.Space(6), 0)
					return
				}
				for _, cat := range cats {
					a.modelCategory(c.Key("cat:"+cat), f, cat, groups[cat])
				}
			})
		}).FillWidth().MaxHeight(l.Rem(15)) // max-h-60
	})
}

// modelCategory 是一个家族分组：头部（折叠钮加全选钮）与下面缩进的模型行。
func (a *AI) modelCategory(c ui.C, f *fetchState, cat string, models []services.AIRemoteModel) {
	l := a.Look
	t := l.T
	label := cat
	if cat == "Other" {
		label = locales.SettingsOther()
	}
	collapsed := f.collapsed[cat]
	all, some := true, false
	for _, m := range models {
		if f.selected[m.ID] {
			some = true
		} else {
			all = false
		}
	}
	ui.Column(c).FillWidth().Children(func() {
		head := ui.Row(c.Key("head")).FillWidth().Items(ui.Center)
		head.Children(func() {
			// 折叠钮
			toggleName := locales.SettingsCollapseModelCategory(label)
			if collapsed {
				toggleName = locales.SettingsExpandModelCategory(label)
			}
			tk := c.Key("toggle")
			id := ui.ID(tk)
			tb := ui.Row(tk).Grow(1).MinWidth(0).Items(ui.Center).Gap(l.Space(2)).PadSides(l.Space(1.5), l.Space(2), l.Space(1.5), l.Space(2)).
				HitWith(id, toggleName, gfx.HitOptions{Role: gfx.RoleButton, Expandable: true, Expanded: !collapsed, Cursor: gfx.PointerCursorHand})
			l.TabStop(tb)
			hover := tb.State().Hovered
			tb.Draw(func(d *ui.DrawContext) {
				if hover {
					l.PaintSurface(d, kit.Surface{Bg: t.Muted.WithAlpha(0.5), Radius: l.Rem(0.75)})
				}
			}).Children(func() {
				ic := icons.ChevronDown
				if collapsed {
					ic = icons.ChevronRight
				}
				l.Icon(c.Key("chev"), ic, l.Rem(0.75), t.MutedForeground).Shrink(0)
				brandBadge(l, c.Key("brand"), cat, l.Rem(1), t.Muted, t.Primary, l.Rem(0.25), 9)
				l.Text(c.Key("name"), label, kit.TextXS, kit.WeightMedium, t.Foreground)
				ui.Row(c.Key("count")).PadSides(l.Space(0.5), l.Space(1), l.Space(0.5), l.Space(1)).
					Draw(func(d *ui.DrawContext) { l.PaintSurface(d, kit.Surface{Bg: t.Muted, Radius: l.Rem(0.25)}) }).
					Children(func() { l.TextPx(c.Key("n"), itoa(len(models)), 10, 15, kit.WeightNormal, t.MutedForeground) })
			})
			if tb.Clicked() {
				f.collapsed[cat] = !collapsed
			}
			// 全选钮
			selName, selText := locales.SettingsSelectAllModelsInCategory(label), locales.CommonSelectAll()
			if all {
				selName, selText = locales.SettingsDeselectAllModelsInCategory(label), locales.CommonDeselectAll()
			}
			st := kit.ButtonStyle{
				PadX: l.Space(2), PadY: l.Space(0.5), Radius: l.Rem(0.25), FontPx: 10, LinePx: 15, Weight: kit.WeightNormal,
				Fg: t.MutedForeground, FgHover: t.Foreground, BgHover: t.Muted, Transition: kit.DurationDefault,
			}
			switch {
			case all:
				st.Fg, st.FgHover, st.Bg, st.BgHover = t.Primary, t.Primary, t.Primary.WithAlpha(0.15), t.Primary.WithAlpha(0.15)
			case some:
				st.Fg, st.FgHover, st.Bg, st.BgHover = t.Primary.WithAlpha(0.7), t.Primary.WithAlpha(0.7), t.Primary.WithAlpha(0.08), t.Primary.WithAlpha(0.15)
			}
			if l.Button(c.Key("all"), kit.ButtonSpec{Label: selText, Name: selName}, st).Clicked() {
				for _, m := range models {
					if all {
						delete(f.selected, m.ID)
					} else {
						f.selected[m.ID] = true
					}
				}
			}
		})
		if collapsed {
			return
		}
		ui.Column(c.Key("models")).FillWidth().PadSides(0, 0, 0, l.Rem(1.75)).Gap(l.Space(0.5)).Children(func() {
			for _, m := range models {
				a.remoteModelRow(c.Key("m:"+m.ID), f, m)
			}
		})
	})
}

// remoteModelRow 是一个可勾选的远端模型。
func (a *AI) remoteModelRow(c ui.C, f *fetchState, m services.AIRemoteModel) {
	l := a.Look
	t := l.T
	on := f.selected[m.ID]
	name := locales.SettingsSelectModel(m.ID)
	if on {
		name = locales.SettingsDeselectModel(m.ID)
	}
	id := ui.ID(c)
	box := ui.Row(c).FillWidth().Items(ui.Center).Gap(l.Space(2)).PadSides(l.Space(1.5), l.Space(2.5), l.Space(1.5), l.Space(2.5)).
		HitWith(id, name, gfx.HitOptions{Role: gfx.RoleButton, Selected: on, Cursor: gfx.PointerCursorHand})
	l.TabStop(box)
	fg, bg := t.Foreground, t.Background.WithAlpha(0)
	switch {
	case on:
		fg, bg = t.Primary, t.Primary.WithAlpha(0.1)
	case box.State().Hovered:
		bg = t.Muted.WithAlpha(0.6)
	}
	box.Draw(func(d *ui.DrawContext) { l.PaintSurface(d, kit.Surface{Bg: bg, Radius: l.Rem(0.75)}) }).Children(func() {
		// 勾选指示：h-3.5 w-3.5 的小方框，选中是 primary 底加对勾。
		side := l.Rem(0.875)
		ui.Row(c.Key("box")).Size(side, side).Shrink(0).Items(ui.Center).Justify(ui.Center).
			Draw(func(d *ui.DrawContext) {
				if on {
					l.PaintSurface(d, kit.Surface{Bg: t.Primary, Border: t.Primary, BorderW: 1, Radius: l.Rem(0.25)})
					return
				}
				l.PaintSurface(d, kit.Surface{Bg: t.Background, Border: t.Border, BorderW: 1, Radius: l.Rem(0.25)})
			}).Children(func() {
			if on {
				l.Icon(c.Key("mark"), icons.Check, l.Rem(0.625), t.PrimaryForeground)
			}
		})
		l.Text(c.Key("id"), m.ID, kit.TextXS, kit.WeightNormal, fg, kit.Ellipsis()).Grow(1).MinWidth(0)
		if m.OwnedBy != "" {
			pillBg, pillFg := t.Muted, t.MutedForeground
			if on {
				pillBg, pillFg = t.Primary.WithAlpha(0.2), t.Primary
			}
			ui.Row(c.Key("owner")).Shrink(0).PadSides(l.Space(0.5), l.Space(1.5), l.Space(0.5), l.Space(1.5)).
				Draw(func(d *ui.DrawContext) { l.PaintSurface(d, kit.Surface{Bg: pillBg, Radius: l.Rem(0.25)}) }).
				Children(func() { l.TextPx(c.Key("t"), m.OwnedBy, 10, 15, kit.WeightNormal, pillFg) })
		}
	})
	if box.Clicked() {
		if on {
			delete(f.selected, m.ID)
		} else {
			f.selected[m.ID] = true
		}
	}
}
