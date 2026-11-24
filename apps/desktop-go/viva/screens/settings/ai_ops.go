package settings

import (
	"context"
	"fmt"
	"strings"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 「AI 模型服务」分区的操作：改配置（端点、模型、路由）、连通性测试、删除确认。
// 修改一律在配置副本上做（services.AIConfig 的不可变方法），再经 commit 乐观生效并排队写盘。

// connectionErrorMessage 把连通性错误翻成用户能处理的话（getConnectionErrorMessage）：
// 网络类与鉴权类给出固定的指引，其余保持原文。原文已由适配层去掉密钥。
func connectionErrorMessage(message string) string {
	n := strings.ToLower(message)
	switch {
	case strings.Contains(n, "failed to fetch") || strings.Contains(n, "networkerror"):
		return locales.SettingsAiWorkbenchConnectionNetworkError()
	case strings.Contains(n, "401") || strings.Contains(n, "403") || strings.Contains(n, "unauthorized") || strings.Contains(n, "invalid api key"):
		return locales.SettingsAiWorkbenchConnectionAuthError()
	}
	return message
}

// redact 抹掉文字里出现的密钥：错误原文在到达界面之前已由适配层去过一次，这里是展示前的最后一道。
func redact(text string, secrets ...string) string {
	for _, s := range secrets {
		if s = strings.TrimSpace(s); len([]rune(s)) >= 4 {
			text = strings.ReplaceAll(text, s, "***")
		}
	}
	return text
}

// ── 端点 ──

// endpointPatch 是对一个端点的修改；enabled 为 nil 表示不动启用状态。
type endpointPatch struct {
	name, provider, protocol, apiKey, apiURL string
	enabled                                  *bool
}

// applyEndpoint 把修改落到端点记录与旗下所有模型上，并清掉验证时间（凭据变了，上一次的验证结果不再作数）。
// 只把端点字段落到模型上：整份展开会把供应商的名称一路灌进模型名，模型路由的下拉里每一项都变成供应商名。
func applyEndpoint(cfg services.AIConfig, g services.AIEndpointGroup, p endpointPatch) services.AIConfig {
	if g.ProviderConfigID != "" {
		cfg = cfg.UpdateProvider(g.ProviderConfigID, func(pr *services.AIProvider) {
			pr.Name, pr.Provider, pr.APIProtocol, pr.APIKey, pr.APIURL = p.name, p.provider, p.protocol, p.apiKey, p.apiURL
			if p.enabled != nil {
				pr.Enabled = *p.enabled
			}
		})
	}
	for _, m := range g.Models {
		cfg = cfg.UpdateModel(m.ID, func(mm *services.AIModel) {
			mm.ProviderID, mm.Provider, mm.APIProtocol, mm.APIKey, mm.APIURL = g.ProviderConfigID, p.provider, p.protocol, p.apiKey, p.apiURL
			if p.enabled != nil {
				mm.Enabled = *p.enabled
			}
			mm.LastVerifiedAt = ""
		})
	}
	return cfg
}

// saveCredentials 保存凭据块里改过的地址与密钥（回车或点勾）。
func (a *AI) saveCredentials(g services.AIEndpointGroup) {
	cr := &a.cred
	if !cr.dirty() || trimmed(cr.url.Text()) == "" {
		return
	}
	name := g.Name
	if name == "" {
		name = g.DisplayName(providerLabel)
	}
	next := applyEndpoint(a.cfg, g, endpointPatch{
		name: name, provider: g.Provider, protocol: g.APIProtocol,
		apiKey: trimmed(cr.key.Text()), apiURL: services.NormalizeAPIURL(cr.url.Text()),
	})
	delete(a.statuses, g.Key)
	a.commit(next, locales.SettingsAiWorkbenchEndpointUpdated())
}

// toggleEndpoint 切换端点是否参与路由。有供应商记录的只改记录；合成分组没有记录，逐个改模型。
func (a *AI) toggleEndpoint(g services.AIEndpointGroup, on bool) {
	name := g.Name
	if name == "" {
		name = g.DisplayName(providerLabel)
	}
	next := a.cfg
	if g.ProviderConfigID != "" {
		next = next.UpdateProvider(g.ProviderConfigID, func(p *services.AIProvider) { p.Enabled = on })
	} else {
		for _, m := range g.Models {
			next = next.UpdateModel(m.ID, func(mm *services.AIModel) { mm.Enabled = on })
		}
	}
	toast := locales.SettingsAiWorkbenchProviderDisabledToast(name)
	if on {
		toast = locales.SettingsAiWorkbenchProviderEnabledToast(name)
	}
	a.commit(next, toast)
}

// confirmDeleteEndpoint 删除供应商连同旗下模型。
func (a *AI) confirmDeleteEndpoint(g services.AIEndpointGroup) {
	ids := make([]string, len(g.Models))
	for i, m := range g.Models {
		ids[i] = m.ID
	}
	a.commit(a.cfg.DeleteProvider(g.ProviderConfigID, ids), locales.SettingsAiWorkbenchProviderDeleted())
}

// ── 模型 ──

// setDefaultModel 设默认模型；渲染层没有成功提示。
func (a *AI) setDefaultModel(id string) { a.commit(a.cfg.SetDefaultModel(id), "") }

// setRoute 改一条路由的模型（空表示跟随全局默认）。
func (a *AI) setRoute(route, modelID string) { a.commit(a.cfg.SetRoute(route, modelID), "") }

// confirmDeleteModel 删除模型。没有供应商记录的合成分组只剩这一个模型时，先把它的端点留成正式的供应商记录，否则端点会随之消失。
func (a *AI) confirmDeleteModel(m services.AIModel) {
	next := a.cfg
	for _, g := range a.cfg.EndpointGroups() {
		if g.ProviderConfigID == "" && len(g.Models) == 1 && g.Models[0].ID == m.ID {
			name := g.Name
			if name == "" {
				name = g.DisplayName(providerLabel)
			}
			next, _ = next.AddProvider(services.SyntheticProviderFor(g, name))
		}
	}
	a.commit(next.DeleteModel(m.ID), locales.SettingsAiWorkbenchModelDeleted())
}

// deleteDialogs 是两个删除确认框；确认后才真正修改。
func (a *AI) deleteDialogs(c ui.C) {
	l := a.Look
	m := a.delModel
	res := l.ConfirmDialog(c.Key("model"), &a.delModelDlg, kit.ConfirmSpec{
		Open: m != nil, Title: locales.CommonDelete(), Message: locales.SettingsConfirmDelete(),
		ConfirmText: locales.CommonDelete(), CancelText: locales.CommonCancel(), Destructive: true,
	})
	switch res {
	case kit.ConfirmAccepted:
		if m != nil {
			a.delModel = nil
			a.confirmDeleteModel(*m)
		}
	case kit.ConfirmDismissed:
		a.delModel = nil
	}

	g := a.delEndpoint
	message := ""
	if g != nil {
		// 旗下模型会一并删除，确认框必须把这个数字说出来。
		name := g.DisplayName(providerLabel)
		message = locales.SettingsAiWorkbenchConfirmDeleteProvider(name)
		if len(g.Models) > 0 {
			message = locales.SettingsAiWorkbenchConfirmDeleteProviderModels(name, len(g.Models))
		}
	}
	res = l.ConfirmDialog(c.Key("endpoint"), &a.delEndpointDl, kit.ConfirmSpec{
		Open: g != nil, Title: locales.SettingsAiWorkbenchDeleteProvider(), Message: message,
		ConfirmText: locales.CommonDelete(), CancelText: locales.CommonCancel(), Destructive: true,
	})
	switch res {
	case kit.ConfirmAccepted:
		if g != nil {
			a.delEndpoint = nil
			a.confirmDeleteEndpoint(*g)
		}
	case kit.ConfirmDismissed:
		a.delEndpoint = nil
	}
}

// ── 连通性测试 ──

// runTest 在后台对模型做一次连通性测试（走 Go 壳代理）；切走分区时请求被取消、结果丢弃。
// 没能发起测试（服务未接）也当作失败结果交给 done，原因进日志。
func (a *AI) runTest(m services.AIModel, done func(services.AITestOutcome)) {
	svc, g := a.Service, a.Guard // 后台闭包只用界面线程上拷好的局部变量（见 reload）
	a.w.load(a.Wake, func(ctx context.Context) func() {
		out, err := services.AITestOutcome{}, errAIUnavailable
		if svc != nil {
			out, err = svc.TestModel(ctx, m)
		}
		if err != nil {
			out = services.AITestOutcome{Error: redact(err.Error(), m.APIKey)}
			if ctx.Err() == nil {
				g.Background(locales.SettingsTestConnection(), err)
			}
		}
		out.Error = redact(out.Error, m.APIKey)
		return func() { done(out) }
	})
}

// reportTest 把测试结果变成提示：成功带耗时；失败是「<模型> 测试失败」，原因放进可复制的详情。
func (a *AI) reportTest(name string, out services.AITestOutcome) {
	if out.Success {
		a.notify(kit.ToastSuccess, fmt.Sprintf("%s %s (%dms)", name, locales.SettingsAiWorkbenchModelTestSuccess(), out.LatencyMs))
		return
	}
	reason := out.Error
	if strings.TrimSpace(reason) == "" {
		reason = locales.ToastConnectionFailed()
	}
	if a.Guard.Notify != nil {
		a.Guard.Notify.Error(name+" "+locales.SettingsAiWorkbenchModelTestFailed(), connectionErrorMessage(reason))
	}
}

// testModelRow 测试模型行上的一个模型；配置不完整直接拒绝。
func (a *AI) testModelRow(m services.AIModel) {
	if !services.IsConfiguredModel(m) {
		a.Guard.Reject(locales.SettingsAiWorkbenchIncompleteModel())
		return
	}
	a.testingModel = m.ID
	a.runTest(m, func(out services.AITestOutcome) {
		a.testingModel = ""
		a.reportTest(services.ModelDisplayName(m, nil, providerLabel), out)
	})
}

// testDefault 测试当前默认的对话模型（主文本，没有就用快速文本）。
func (a *AI) testDefault() {
	m, ok := a.cfg.ResolveRoute(services.AIRouteMainText)
	if !ok {
		m, ok = a.cfg.ResolveRoute(services.AIRouteFastText)
	}
	if !ok || !services.IsConfiguredModel(m) {
		a.Guard.Reject(locales.SettingsAiWorkbenchNoDefaultModel())
		return
	}
	a.testingDefault, a.testingModel = true, m.ID
	a.runTest(m, func(out services.AITestOutcome) {
		a.testingDefault, a.testingModel = false, ""
		a.reportTest(services.ModelDisplayName(m, nil, providerLabel), out)
	})
}

// testEndpoint 测试端点连通性：优先用对话模型，只有专用模型时按其能力测。成功后把旗下模型都标成已验证。
func (a *AI) testEndpoint(g services.AIEndpointGroup) {
	var target *services.AIModel
	for i, m := range g.Models {
		if services.IsConfiguredModel(m) && m.Capabilities.Chat {
			target = &g.Models[i]
			break
		}
	}
	if target == nil {
		for i, m := range g.Models {
			if services.IsConfiguredModel(m) {
				target = &g.Models[i]
				break
			}
		}
	}
	if target == nil {
		a.Guard.Reject(locales.SettingsAiWorkbenchEndpointNotTestable())
		return
	}
	m := *target
	a.testingEndpoint = g.Key
	a.runTest(m, func(out services.AITestOutcome) {
		a.testingEndpoint = ""
		if a.statuses == nil {
			a.statuses = map[string]endpointStatus{}
		}
		if !out.Success {
			reason := connectionErrorMessage(firstNonEmptyStr(out.Error, locales.ToastConnectionFailed()))
			a.statuses[g.Key] = endpointStatus{toneError, locales.ToastConnectionFailed(), reason}
			if a.Guard.Notify != nil {
				a.Guard.Notify.Error(locales.ToastConnectionFailed(), reason)
			}
			return
		}
		a.statuses[g.Key] = endpointStatus{toneReady, locales.SettingsAiWorkbenchConnected(), fmt.Sprintf("%s · %dms", m.Model, out.LatencyMs)}
		stamp := a.now().UTC().Format("2006-01-02T15:04:05.000Z")
		next := a.cfg
		for _, gm := range g.Models {
			next = next.UpdateModel(gm.ID, func(mm *services.AIModel) { mm.LastVerifiedAt = stamp })
		}
		a.commit(next, locales.SettingsAiWorkbenchEndpointConnected(out.LatencyMs))
	})
}

// importLegacy 把旧版单模型字段导入成一个模型。
func (a *AI) importLegacy() {
	lg := a.cfg.AILegacy
	next, _ := a.cfg.AddModel(services.AIModel{
		Name: lg.Model, Provider: lg.Provider, APIProtocol: services.NormalizeAIProtocol(lg.APIProtocol, lg.Provider, lg.APIURL),
		APIKey: lg.APIKey, APIURL: lg.APIURL, Model: lg.Model, Enabled: true, Capabilities: services.DefaultAICapabilities(),
	})
	a.commit(next, locales.SettingsAiWorkbenchLegacyImported())
}

// hasLegacyOnlyConfig 报告是否只有旧版配置：没有任何模型，旧版字段四项齐全。
func (a *AI) hasLegacyOnlyConfig() bool {
	lg := a.cfg.AILegacy
	return len(a.cfg.Models) == 0 && trimmed(lg.Provider) != "" && trimmed(lg.APIKey) != "" && trimmed(lg.APIURL) != "" && trimmed(lg.Model) != ""
}
