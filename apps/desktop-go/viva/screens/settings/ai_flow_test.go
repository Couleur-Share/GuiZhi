package settings_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	sscreen "guizhi-mygo-shell/viva/screens/settings"
)

// openProvider 打开页面并选中左侧栏的「Main」端点。
func (e *aiEnv) openProvider(t *testing.T) *rig {
	t.Helper()
	r := e.open(t)
	waitButton(r, "Main")
	r.clickNode(gfx.RoleButton, "Main")
	waitUntil(r, "端点卡", func() bool { return r.hasText("gpt-4o") && r.has(gfx.RoleButton, locales.SettingsTestConnection()) })
	return r
}

// ── 端点 ──

func TestAIAddEndpointSavesCredentials(t *testing.T) {
	e := newAIEnv()
	r := e.open(t)
	waitButton(r, "Main")
	r.clickNode(gfx.RoleButton, locales.SettingsAddProvider())
	waitUntil(r, "添加弹窗", func() bool { return r.dialogOpen(locales.SettingsAiWorkbenchAddProvider()) })

	// 默认是 OpenAI：名称与地址取预设；密钥是密码框，内容不进可访问树。
	if got := r.fieldValue(locales.SettingsAiWorkbenchProviderDisplayName()); got != "OpenAI" {
		t.Errorf("默认名称 = %q", got)
	}
	r.typeInto(locales.SettingsApiKey(), "sk-new-key-0001")
	if n, ok := r.node(gfx.RoleTextField, locales.SettingsApiKey()); !ok || strings.Contains(n.Value, "sk-new-key-0001") {
		t.Errorf("密钥框不应把明文暴露给可访问树：%+v", n)
	}
	r.press(gfx.RoleButton, locales.SettingsSaveChanges())
	e.waitSaves(r, 1)

	cfg := e.lastSave(t)
	if len(cfg.Providers) != 2 {
		t.Fatalf("应新增一个供应商，实际 %d 个", len(cfg.Providers))
	}
	p := cfg.Providers[1]
	if p.Name != "OpenAI" || p.Provider != "openai" || p.APIKey != "sk-new-key-0001" || !p.Enabled {
		t.Errorf("新供应商 = %v（key 长度 %d）", p, len(p.APIKey))
	}
	waitUntil(r, "成功提示", func() bool { return len(e.toasts(kit.ToastSuccess)) == 1 })
	if got := e.toasts(kit.ToastSuccess)[0].Message; got != locales.SettingsAiWorkbenchProviderAdded() {
		t.Errorf("提示 = %q", got)
	}
	if r.dialogOpen(locales.SettingsAiWorkbenchAddProvider()) {
		t.Error("保存后弹窗应关闭")
	}
	e.assertNoSecret(t, r)
}

func TestAIEditEndpointRenamesAndClearsVerification(t *testing.T) {
	e := newAIEnv()
	c := seedAIConfig()
	c.Models[0].LastVerifiedAt = "2026-11-01T00:00:00.000Z"
	e.ai.PutConfig(c)
	r := e.openProvider(t)

	r.clickNth(gfx.RoleButton, locales.CommonEdit(), 0)
	waitUntil(r, "编辑弹窗", func() bool { return r.dialogOpen(locales.SettingsAiWorkbenchEditEndpoint()) })
	r.typeInto(locales.SettingsAiWorkbenchProviderDisplayName(), "Team OpenAI")
	r.press(gfx.RoleButton, locales.SettingsSaveChanges())
	e.waitSaves(r, 1)

	cfg := e.lastSave(t)
	if cfg.Providers[0].Name != "Team OpenAI" {
		t.Errorf("名称 = %q", cfg.Providers[0].Name)
	}
	for _, m := range cfg.Models {
		if m.LastVerifiedAt != "" {
			t.Errorf("凭据改动后验证时间应清空：%s %q", m.ID, m.LastVerifiedAt)
		}
	}
	waitButton(r, "Team OpenAI")
	e.assertNoSecret(t, r)
}

func TestAIDeleteEndpointNeedsConfirmAndRemovesModels(t *testing.T) {
	e := newAIEnv()
	r := e.openProvider(t)

	r.clickNth(gfx.RoleButton, locales.CommonDelete(), 0)
	waitUntil(r, "删除供应商确认框", func() bool { return r.dialogOpen(locales.SettingsAiWorkbenchDeleteProvider()) })
	if got, want := r.dialogDescription(locales.SettingsAiWorkbenchDeleteProvider()), locales.SettingsAiWorkbenchConfirmDeleteProviderModels("Main", 2); got != want {
		t.Fatalf("确认框说明 = %q，应为 %q", got, want)
	}
	if len(e.ai.Saves()) != 0 {
		t.Fatal("确认前不应写盘")
	}
	r.clickLast(gfx.RoleButton, locales.CommonDelete()) // 对话框里的确认钮（页面上的删除钮此刻在遮罩后面）
	e.waitSaves(r, 1)
	cfg := e.lastSave(t)
	if len(cfg.Providers) != 0 || len(cfg.Models) != 0 {
		t.Errorf("删除供应商应连同旗下模型一起删：%d 个供应商、%d 个模型", len(cfg.Providers), len(cfg.Models))
	}
}

func TestAIToggleEndpointDisablesRouting(t *testing.T) {
	e := newAIEnv()
	r := e.openProvider(t)
	r.press(gfx.RoleSwitch, locales.SettingsAiWorkbenchDisableProvider())
	e.waitSaves(r, 1)
	if e.lastSave(t).Providers[0].Enabled {
		t.Error("停用后供应商应写成未启用")
	}
	waitUntil(r, "开关文字翻转", func() bool { return r.has(gfx.RoleSwitch, locales.SettingsAiWorkbenchEnableProvider()) })
	// 停用的端点不再参与路由：它的模型不是任何路由的候选。
	if cfg := e.ai.Current(); len(cfg.RouteCandidates(services.AIRouteMainText)) != 0 {
		t.Error("停用后不应再有候选模型")
	}
}

// ── 连通性测试 ──

func TestAITestEndpointSuccessStampsVerification(t *testing.T) {
	e := newAIEnv()
	e.ai.PutTestOutcome(services.AITestOutcome{Success: true, LatencyMs: 321})
	r := e.openProvider(t)
	r.clickNode(gfx.RoleButton, locales.SettingsTestConnection())
	e.waitSaves(r, 1)

	for _, m := range e.lastSave(t).Models {
		if m.LastVerifiedAt == "" {
			t.Errorf("模型 %s 应带验证时间", m.ID)
		}
	}
	waitUntil(r, "成功提示", func() bool { return len(e.toasts(kit.ToastSuccess)) == 1 })
	if got := e.toasts(kit.ToastSuccess)[0].Message; got != locales.SettingsAiWorkbenchEndpointConnected(321) {
		t.Errorf("提示 = %q", got)
	}
	waitText(r, "gpt-4o · 321ms")
	// 测试走的是对话模型，不是向量模型。
	if tests := e.ai.Tests(); len(tests) != 1 || tests[0].ID != "m1" {
		t.Errorf("端点测试应选对话模型：%v", tests)
	}
	e.assertNoSecret(t, r)
}

func TestAITestEndpointFailureKeepsSecretOutOfToast(t *testing.T) {
	e := newAIEnv()
	e.ai.PutTestOutcome(services.AITestOutcome{Success: false, Error: "upstream said: 400 bad request for key " + aiSecret})
	r := e.openProvider(t)
	r.clickNode(gfx.RoleButton, locales.SettingsTestConnection())
	waitUntil(r, "错误提示", func() bool { return len(e.toasts(kit.ToastError)) == 1 })

	toast := e.toasts(kit.ToastError)[0]
	if toast.Message != locales.ToastConnectionFailed() || !strings.Contains(toast.Detail, "bad request") {
		t.Errorf("失败提示应是「连接失败」加可复制的原因：%+v", toast)
	}
	if len(e.ai.Saves()) != 0 {
		t.Error("测试失败不应改配置")
	}
	waitText(r, locales.ToastConnectionFailed())
	e.assertNoSecret(t, r)
}

func TestAITestModelRowReportsOutcome(t *testing.T) {
	e := newAIEnv()
	e.ai.PutTestOutcome(services.AITestOutcome{Success: false, Error: "HTTP 401 unauthorized"})
	r := e.openProvider(t)
	r.clickNth(gfx.RoleButton, locales.SettingsAiWorkbenchTestAction(), 0)
	waitUntil(r, "错误提示", func() bool { return len(e.toasts(kit.ToastError)) == 1 })
	toast := e.toasts(kit.ToastError)[0]
	if !strings.Contains(toast.Message, locales.SettingsAiWorkbenchModelTestFailed()) || toast.Detail != locales.SettingsAiWorkbenchConnectionAuthError() {
		t.Errorf("鉴权失败应翻成指引：%+v", toast)
	}

	e.ai.PutTestOutcome(services.AITestOutcome{Success: true, LatencyMs: 88})
	r.clickNth(gfx.RoleButton, locales.SettingsAiWorkbenchTestAction(), 0)
	waitUntil(r, "成功提示", func() bool { return len(e.toasts(kit.ToastSuccess)) == 1 })
	if got := e.toasts(kit.ToastSuccess)[0].Message; !strings.Contains(got, "(88ms)") {
		t.Errorf("成功提示应带耗时：%q", got)
	}
}

func TestAITestDefaultRejectsWithoutModel(t *testing.T) {
	e := newAIEnv()
	e.ai.PutConfig(services.AIConfig{Routes: map[string]string{}})
	r := e.open(t)
	waitText(r, locales.SettingsAiWorkbenchStatusOverview())
	r.clickNode(gfx.RoleButton, locales.SettingsAiWorkbenchTestDefault())
	waitUntil(r, "错误提示", func() bool { return len(e.toasts(kit.ToastError)) == 1 })
	if got := e.toasts(kit.ToastError)[0].Message; got != locales.SettingsAiWorkbenchNoDefaultModel() {
		t.Errorf("提示 = %q", got)
	}
	if len(e.ai.Tests()) != 0 {
		t.Error("没有默认模型不应发起测试")
	}
}

// ── 模型 ──

func TestAIFetchModelsBatchAddInfersCapabilities(t *testing.T) {
	e := newAIEnv()
	e.ai.PutFetchResult(services.AIFetchModelsResult{Success: true, Models: []services.AIRemoteModel{
		{ID: "gpt-4.1", OwnedBy: "openai"}, {ID: "text-embedding-3-large", OwnedBy: "openai"}, {ID: "whisper-1", OwnedBy: "openai"},
	}})
	r := e.openProvider(t)
	r.clickNode(gfx.RoleButton, locales.SettingsFetchModels())
	waitUntil(r, "模型列表", func() bool { return r.has(gfx.RoleButton, locales.SettingsSelectModel("gpt-4.1")) })

	if f := e.ai.Fetches(); len(f) != 1 || f[0].APIKey != aiSecret || f[0].APIURL != "https://api.openai.com" {
		t.Fatalf("拉取应带端点的地址与密钥：%v", f)
	}
	if len(e.toasts(kit.ToastSuccess)) == 0 {
		waitUntil(r, "已加载提示", func() bool { return len(e.toasts(kit.ToastSuccess)) == 1 })
	}
	// 搜索过滤、选两项、批量添加。
	r.typeInto(locales.SettingsSearchModels(), "e")
	waitUntil(r, "过滤后仍有向量模型", func() bool { return r.has(gfx.RoleButton, locales.SettingsSelectModel("text-embedding-3-large")) })
	r.clickNode(gfx.RoleButton, locales.SettingsSelectModel("text-embedding-3-large"))
	r.typeInto(locales.SettingsSearchModels(), "")
	r.clickNode(gfx.RoleButton, locales.SettingsSelectModel("whisper-1"))
	waitUntil(r, "批量按钮", func() bool { return r.has(gfx.RoleButton, locales.SettingsAddNModels(2)) })
	r.clickNode(gfx.RoleButton, locales.SettingsAddNModels(2))
	e.waitSaves(r, 1)

	cfg := e.lastSave(t)
	if len(cfg.Models) != 4 {
		t.Fatalf("应新增 2 个模型，共 %d 个", len(cfg.Models))
	}
	byName := map[string]services.AIModel{}
	for _, m := range cfg.Models {
		byName[m.Model] = m
	}
	if m := byName["text-embedding-3-large"]; !m.Capabilities.Embedding || m.Capabilities.Chat || m.ProviderID != "p1" || !m.Enabled {
		t.Errorf("向量模型 = %+v", m.Capabilities)
	}
	if m := byName["whisper-1"]; !m.Capabilities.AudioTranscription {
		t.Errorf("转写模型 = %+v", m.Capabilities)
	}
	if _, ok := byName["gpt-4.1"]; ok {
		t.Error("没勾选的模型不应加入")
	}
	e.assertNoSecret(t, r)
}

func TestAIFetchModelsFailureFeedback(t *testing.T) {
	cases := []struct {
		name   string
		result services.AIFetchModelsResult
		kind   kit.ToastKind
		want   string
	}{
		{"鉴权", services.AIFetchModelsResult{Reason: services.AIFetchReasonAuth, Error: "401"}, kit.ToastError, locales.SettingsAiWorkbenchFetchModelsAuthError()},
		{"不支持列表", services.AIFetchModelsResult{Reason: services.AIFetchReasonUnsupported}, kit.ToastInfo, locales.SettingsAiWorkbenchFetchModelsUnsupported()},
		{"空列表", services.AIFetchModelsResult{Success: true}, kit.ToastWarning, locales.SettingsAiWorkbenchFetchModelsEmpty()},
		{"其他", services.AIFetchModelsResult{Reason: services.AIFetchReasonHTTP, Error: "HTTP 500 upstream"}, kit.ToastError, "HTTP 500 upstream"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newAIEnv()
			e.ai.PutFetchResult(tc.result)
			r := e.openProvider(t)
			r.clickNode(gfx.RoleButton, locales.SettingsFetchModels())
			waitUntil(r, "提示", func() bool { return len(e.toasts(tc.kind)) == 1 })
			if got := e.toasts(tc.kind)[0].Message; got != tc.want {
				t.Errorf("提示 = %q，期望 %q", got, tc.want)
			}
			waitUntil(r, "弹窗关闭", func() bool { return !r.dialogOpen(locales.SettingsFetchModels()) })
		})
	}
}

func TestAIAddModelWithCapabilities(t *testing.T) {
	e := newAIEnv()
	r := e.openProvider(t)
	r.clickNode(gfx.RoleButton, locales.SettingsAddModel())
	waitUntil(r, "添加模型弹窗", func() bool { return r.has(gfx.RoleTextField, locales.SettingsModelName()) })

	// 缺模型名：拒绝并提示。
	r.clickNode(gfx.RoleButton, locales.SettingsAddModel()) // 弹窗打开后背景里的同名按钮不在可访问树里
	waitUntil(r, "拒绝提示", func() bool { return len(e.toasts(kit.ToastError)) == 1 })
	if len(e.ai.Saves()) != 0 {
		t.Fatal("缺模型名不应写盘")
	}

	r.typeInto(locales.SettingsModelName(), "text-embedding-3-large")
	r.clickNode(gfx.RoleCheckbox, locales.SettingsAiWorkbenchEmbeddingCapability())
	r.clickNode(gfx.RoleButton, locales.SettingsAddModel()) // 弹窗打开后背景里的同名按钮不在可访问树里
	e.waitSaves(r, 1)
	cfg := e.lastSave(t)
	m := cfg.Models[len(cfg.Models)-1]
	if m.Model != "text-embedding-3-large" || !m.Capabilities.Embedding || m.Capabilities.Chat || m.ProviderID != "p1" || m.APIKey != aiSecret {
		t.Errorf("新模型 = %+v", m.Capabilities)
	}
}

func TestAIEditModelAndSetDefaultAndDelete(t *testing.T) {
	e := newAIEnv()
	r := e.openProvider(t)

	// 设为默认：向量模型行有「设为默认」，默认模型行没有。
	r.clickNode(gfx.RoleButton, locales.SettingsSetDefault())
	e.waitSaves(r, 1)
	if cfg := e.lastSave(t); cfg.Models[0].IsDefault || !cfg.Models[1].IsDefault {
		t.Errorf("默认模型应换成第二个：%v %v", cfg.Models[0].IsDefault, cfg.Models[1].IsDefault)
	}

	// 编辑第一个模型：加上推理能力。
	r.clickNth(gfx.RoleButton, locales.CommonEdit(), 1)
	waitUntil(r, "编辑模型弹窗", func() bool { return r.dialogOpen(locales.SettingsAiWorkbenchEditModel()) })
	if got := r.fieldValue(locales.SettingsModelName()); got != "gpt-4o" {
		t.Errorf("模型名 = %q", got)
	}
	r.clickNode(gfx.RoleCheckbox, locales.SettingsAiWorkbenchReasoningCapability())
	r.press(gfx.RoleButton, locales.SettingsSaveChanges())
	e.waitSaves(r, 2)
	if m := e.lastSave(t).Models[0]; !m.Capabilities.Reasoning || !m.Capabilities.Chat {
		t.Errorf("编辑后能力 = %+v", m.Capabilities)
	}

	// 删除第二个模型：先确认再写盘。
	r.clickNth(gfx.RoleButton, locales.CommonDelete(), 2)
	waitUntil(r, "删除模型确认框", func() bool { return r.dialogOpen(locales.CommonDelete()) })
	if len(e.ai.Saves()) != 2 {
		t.Fatal("确认前不应写盘")
	}
	r.clickLast(gfx.RoleButton, locales.CommonDelete())
	e.waitSaves(r, 3)
	if cfg := e.lastSave(t); len(cfg.Models) != 1 || cfg.Models[0].ID != "m1" || len(cfg.Providers) != 1 {
		t.Errorf("删除后应只剩第一个模型：%d 个模型", len(cfg.Models))
	}
}

func TestAITestDraftNeedsCompleteFields(t *testing.T) {
	e := newAIEnv()
	e.ai.PutTestOutcome(services.AITestOutcome{Success: true, LatencyMs: 12})
	r := e.openProvider(t)
	r.clickNode(gfx.RoleButton, locales.SettingsAddModel())
	waitUntil(r, "添加模型弹窗", func() bool { return r.has(gfx.RoleTextField, locales.SettingsModelName()) })
	r.clickNode(gfx.RoleButton, locales.SettingsAiWorkbenchTestDraft())
	waitUntil(r, "拒绝提示", func() bool { return len(e.toasts(kit.ToastError)) == 1 })
	if len(e.ai.Tests()) != 0 {
		t.Fatal("缺模型名不应发起测试")
	}
	r.typeInto(locales.SettingsModelName(), "gpt-4.1")
	r.clickNode(gfx.RoleButton, locales.SettingsAiWorkbenchTestDraft())
	waitUntil(r, "成功提示", func() bool { return len(e.toasts(kit.ToastSuccess)) == 1 })
	if tests := e.ai.Tests(); len(tests) != 1 || tests[0].Model != "gpt-4.1" {
		t.Errorf("测试的草稿 = %v", tests)
	}
	if len(e.ai.Saves()) != 0 {
		t.Error("测试草稿不应写盘")
	}
}

// ── 用量 ──

func TestAIUsageShowsTotalsAndGroups(t *testing.T) {
	e := newAIEnv()
	e.ai.PutUsage(services.AIUsageSummary{
		Days: 30, Calls: 12, PromptTokens: 1500, CompletionTokens: 700,
		ByScenario: []services.AIUsageRow{{Scenario: "qa", Calls: 9, PromptTokens: 1200, CompletionTokens: 500}, {Scenario: "wiki", Calls: 3, PromptTokens: 300, CompletionTokens: 200}},
		ByModel:    []services.AIUsageRow{{Model: "gpt-4o", Calls: 12, PromptTokens: 1500, CompletionTokens: 700}},
	})
	r := e.open(t)
	waitText(r, locales.SettingsAiUsageTitle(30))
	if !r.hasText(locales.SettingsAiUsageTotal(12, "2.2k")) {
		t.Error("应显示合计（调用数与简写的 token 数）")
	}
	if !r.hasText(locales.SettingsAiUsageRow(9, "1.7k")) {
		t.Error("默认按场景汇总")
	}
	r.press(gfx.RoleButton, locales.SettingsAiUsageGroupModel())
	waitUntil(r, "按模型", func() bool { return r.hasText(locales.SettingsAiUsageRow(12, "2.2k")) })
	if r.hasText(locales.SettingsAiUsageRow(9, "1.7k")) {
		t.Error("按模型时不应再列场景")
	}
	// 刷新会再读一次。
	r.clickNode(gfx.RoleButton, locales.CommonRefresh())
	waitUntil(r, "再次读取用量", func() bool { return len(e.ai.UsageDays()) == 2 })
}

func TestAIUsageLoadFailureToasts(t *testing.T) {
	e := newAIEnv()
	e.ai.FailUsage(errors.New("read usage: database is locked"))
	r := e.open(t)
	waitButton(r, "Main")
	waitUntil(r, "错误提示", func() bool { return len(e.toasts(kit.ToastError)) == 1 })
	toast := e.toasts(kit.ToastError)[0]
	if toast.Message != locales.SettingsAiUsageLoadFailed() || !strings.Contains(toast.Detail, "database is locked") {
		t.Errorf("提示 = %+v", toast)
	}
	if !r.hasText(locales.SettingsAiUsageEmpty(30)) {
		t.Error("读不出用量时，面板退回说明行，页面其余部分照常")
	}
}

// ── 快速配置向导 ──

func TestAIQuickSetupWizardSavesWholeSetup(t *testing.T) {
	e := newAIEnv()
	e.ai.PutConfig(services.AIConfig{Routes: map[string]string{}})
	e.ai.PutFetchResult(services.AIFetchModelsResult{Success: true, Models: []services.AIRemoteModel{{ID: "gpt-4o-mini"}, {ID: "text-embedding-3-small"}}})
	r := e.open(t)
	waitText(r, locales.SettingsAiWorkbenchStatusOverview())
	r.clickNode(gfx.RoleButton, locales.SettingsQuickSetupOpen())
	waitUntil(r, "向导", func() bool { return r.dialogOpen(locales.SettingsQuickSetupTitle()) })

	// 第一步：默认只勾基础问答；再勾语义检索。
	if !r.selected(gfx.RoleButton, locales.SettingsQuickSetupBasic()) || r.selected(gfx.RoleButton, locales.SettingsQuickSetupSemantic()) {
		t.Fatal("默认应只勾「基础问答」")
	}
	r.clickNode(gfx.RoleButton, locales.SettingsQuickSetupSemantic())
	waitUntil(r, "勾选", func() bool { return r.selected(gfx.RoleButton, locales.SettingsQuickSetupSemantic()) })
	r.clickNode(gfx.RoleButton, locales.SettingsQuickSetupNext())

	// 第二步：缺密钥，提示并停在原步骤。
	waitUntil(r, "服务步骤", func() bool { return r.has(gfx.RoleTextField, locales.SettingsQuickSetupServiceName()) })
	r.clickNode(gfx.RoleButton, locales.SettingsQuickSetupFetchModels())
	waitText(r, locales.SettingsQuickSetupNeedEndpoint())
	if len(e.ai.Fetches()) != 0 {
		t.Fatal("缺密钥不应发起拉取")
	}
	r.typeInto("API Key", aiSecret)
	r.clickNode(gfx.RoleButton, locales.SettingsQuickSetupFetchModels())

	// 第三步：按模型名推荐。
	waitUntil(r, "模型步骤", func() bool { return r.has(gfx.RoleTextField, locales.SettingsQuickSetupSemantic()) })
	if got := r.fieldValue(locales.SettingsQuickSetupBasic()); got != "gpt-4o-mini" {
		t.Errorf("基础问答的推荐 = %q", got)
	}
	if got := r.fieldValue(locales.SettingsQuickSetupSemantic()); got != "text-embedding-3-small" {
		t.Errorf("语义检索的推荐 = %q", got)
	}
	r.clickNode(gfx.RoleButton, locales.SettingsQuickSetupNext())

	// 第四步：跳过测试，一次性保存。
	waitUntil(r, "验证步骤", func() bool { return r.has(gfx.RoleButton, locales.SettingsQuickSetupSkipTests()) })
	r.clickNode(gfx.RoleButton, locales.SettingsQuickSetupSkipTests())
	waitText(r, locales.SettingsAiWorkbenchUnverified())
	r.clickNode(gfx.RoleButton, locales.SettingsQuickSetupSaveAll())
	e.waitSaves(r, 1)

	cfg := e.lastSave(t)
	if len(cfg.Providers) != 1 || len(cfg.Models) != 2 {
		t.Fatalf("应整体写入一个服务、两个模型：%d、%d", len(cfg.Providers), len(cfg.Models))
	}
	if cfg.Providers[0].APIKey != aiSecret {
		t.Error("服务的密钥应原样保存")
	}
	text, ok := cfg.ResolveRoute(services.AIRouteMainText)
	if !ok || text.Model != "gpt-4o-mini" {
		t.Errorf("主文本路由 = %+v", text)
	}
	if emb, ok := cfg.ResolveRoute(services.AIRouteEmbedding); !ok || emb.Model != "text-embedding-3-small" {
		t.Errorf("向量路由 = %+v", emb)
	}
	for _, m := range cfg.Models {
		if m.LastVerifiedAt != "" {
			t.Errorf("跳过测试的模型不应带验证时间：%s", m.Model)
		}
	}
	waitUntil(r, "向导关闭", func() bool { return !r.dialogOpen(locales.SettingsQuickSetupTitle()) })
	waitUntil(r, "成功提示", func() bool { return len(e.toasts(kit.ToastSuccess)) == 1 })
	e.assertNoSecret(t, r)
}

func TestAIQuickSetupSaveFailureStaysInWizard(t *testing.T) {
	e := newAIEnv()
	e.ai.PutConfig(services.AIConfig{Routes: map[string]string{}})
	e.ai.PutFetchResult(services.AIFetchModelsResult{Success: true, Models: []services.AIRemoteModel{{ID: "gpt-4o-mini"}}})
	r := e.open(t)
	waitText(r, locales.SettingsAiWorkbenchStatusOverview())
	r.clickNode(gfx.RoleButton, locales.SettingsQuickSetupOpen())
	waitUntil(r, "向导", func() bool { return r.dialogOpen(locales.SettingsQuickSetupTitle()) })
	r.clickNode(gfx.RoleButton, locales.SettingsQuickSetupNext())
	waitUntil(r, "服务步骤", func() bool { return r.has(gfx.RoleTextField, "API Key") })
	r.typeInto("API Key", aiSecret)
	r.clickNode(gfx.RoleButton, locales.SettingsQuickSetupFetchModels())
	waitUntil(r, "模型步骤", func() bool { return r.has(gfx.RoleTextField, locales.SettingsQuickSetupBasic()) })
	r.clickNode(gfx.RoleButton, locales.SettingsQuickSetupNext())
	waitUntil(r, "验证步骤", func() bool { return r.has(gfx.RoleButton, locales.SettingsQuickSetupSaveAll()) })

	e.ai.FailSave(errors.New("write ai-models.json: disk full, key " + aiSecret))
	r.clickNode(gfx.RoleButton, locales.SettingsQuickSetupSaveAll())
	waitUntil(r, "失败原因", func() bool { return r.hasText("disk full") })
	if !r.dialogOpen(locales.SettingsQuickSetupTitle()) {
		t.Error("保存失败时向导应留着，让用户重试")
	}
	if got := e.ai.Current(); len(got.Providers) != 0 {
		t.Error("失败时配置不应改变")
	}
	if len(e.toasts(kit.ToastError)) != 0 {
		t.Error("向导自己显示失败原因，不再另弹通用错误提示")
	}
	e.assertNoSecret(t, r)

	// 修好之后可以直接重试。
	e.ai.FailSave(nil)
	r.clickNode(gfx.RoleButton, locales.SettingsQuickSetupSaveAll())
	e.waitSaves(r, 1)
}

func TestAIQuickSetupVerifyStepRunsTests(t *testing.T) {
	e := newAIEnv()
	e.ai.PutConfig(services.AIConfig{Routes: map[string]string{}})
	e.ai.PutFetchResult(services.AIFetchModelsResult{Success: true, Models: []services.AIRemoteModel{{ID: "gpt-4o-mini"}}})
	e.ai.PutTestOutcome(services.AITestOutcome{Success: true, LatencyMs: 10})
	r := e.open(t)
	waitText(r, locales.SettingsAiWorkbenchStatusOverview())
	r.clickNode(gfx.RoleButton, locales.SettingsQuickSetupOpen())
	waitUntil(r, "向导", func() bool { return r.dialogOpen(locales.SettingsQuickSetupTitle()) })
	r.clickNode(gfx.RoleButton, locales.SettingsQuickSetupNext())
	waitUntil(r, "服务步骤", func() bool { return r.has(gfx.RoleTextField, "API Key") })
	r.typeInto("API Key", aiSecret)
	r.clickNode(gfx.RoleButton, locales.SettingsQuickSetupFetchModels())
	waitUntil(r, "模型步骤", func() bool { return r.has(gfx.RoleTextField, locales.SettingsQuickSetupBasic()) })
	r.clickNode(gfx.RoleButton, locales.SettingsQuickSetupNext())
	waitUntil(r, "验证步骤", func() bool { return r.has(gfx.RoleButton, locales.SettingsQuickSetupTestEach()) })
	r.clickNode(gfx.RoleButton, locales.SettingsQuickSetupTestEach())
	waitText(r, locales.SettingsQuickSetupVerified())
	r.clickNode(gfx.RoleButton, locales.SettingsQuickSetupSaveAll())
	e.waitSaves(r, 1)
	for _, m := range e.lastSave(t).Models {
		if m.LastVerifiedAt == "" {
			t.Errorf("测试通过的模型应带验证时间：%s", m.Model)
		}
	}
}

// ── 收尾与机密 ──

func TestAIAPIKeyIsMaskedInCredentials(t *testing.T) {
	e := newAIEnv()
	r := e.openProvider(t)
	n, ok := r.node(gfx.RoleTextField, locales.SettingsAiWorkbenchEndpointApiKey())
	if !ok {
		t.Fatal("端点卡应有密钥框")
	}
	if strings.Contains(n.Value, aiSecret) {
		t.Error("密钥框的可访问值不应含明文")
	}
	if !n.Secure {
		t.Error("密钥框应是口令框")
	}
	if r.hasText(aiSecret) {
		t.Error("画面文字里不应有明文")
	}
	// 显示按钮只切换圆点与明文，不会把明文写进日志或提示。
	r.clickNode(gfx.RoleButton, locales.CommonShow())
	r.frames(2)
	e.assertNoSecret(t, nil)
}

func TestAISwitchingSectionClosesModalsAndBackgroundWork(t *testing.T) {
	e := newAIEnv()
	e.ai.TestGate = make(chan struct{})
	cap := newCaptureEnv()
	r := openPage(t, func(look *kit.Look) func(c ui.C) {
		sh := &sscreen.Shell{
			Look: look, Store: cap.store, Guard: e.guard(), Wake: func() {}, Active: sscreen.SectionAI, AI: e.ai, Platform: "win32",
		}
		return sh.Page
	})
	waitButton(r, "Main")
	r.clickNode(gfx.RoleButton, "Main")
	waitButton(r, locales.SettingsTestConnection())
	r.clickNode(gfx.RoleButton, locales.SettingsTestConnection()) // 测试挂起在 TestGate 上
	waitUntil(r, "测试已发出", func() bool { return len(e.ai.Tests()) == 1 })

	// 切到「通用」：测试请求被取消；放行之后结果被丢弃，没有任何提示。
	r.clickNode(gfx.RoleButton, locales.SettingsGeneral())
	close(e.ai.TestGate)
	r.frames(10)
	if n := len(e.notify.Queued()); n != 0 {
		t.Errorf("切走分区后不应再弹提示，实际 %d 条", n)
	}
	if len(e.ai.Saves()) != 0 {
		t.Error("丢弃的测试结果不应写盘")
	}

	// 切回来：重新读取，弹窗与选中状态不残留。
	r.clickNode(gfx.RoleButton, locales.SettingsAi())
	waitUntil(r, "重新读取", func() bool { return e.ai.Loads() == 2 })
	waitButton(r, "Main")
	if r.dialogOpen(locales.SettingsQuickSetupTitle()) {
		t.Error("回来时不应残留弹窗")
	}
}
