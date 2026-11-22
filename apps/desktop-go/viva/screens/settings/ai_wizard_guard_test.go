package settings_test

import (
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 拉取模型：服务返回的「失败结果」（不是调用本身出错）里的原因文字同样可能带着请求的密钥，提示里必须是脱敏后的。
func TestAIFetchModelsResultErrorIsRedacted(t *testing.T) {
	e := newAIEnv()
	e.ai.PutFetchResult(services.AIFetchModelsResult{Reason: services.AIFetchReasonHTTP, Error: "upstream rejected key " + aiSecret + " (HTTP 500)"})
	r := e.openProvider(t)
	r.clickNode(gfx.RoleButton, locales.SettingsFetchModels())
	waitUntil(r, "提示", func() bool { return len(e.toasts(kit.ToastError)) == 1 })
	if got := e.toasts(kit.ToastError)[0].Message; got != "upstream rejected key *** (HTTP 500)" {
		t.Errorf("失败原因应是脱敏后的原文，得到 %q", got)
	}
	e.assertNoSecret(t, r)
}

// openWizardAtService 打开向导并停在第二步（服务），默认只勾「基础问答」。
func (e *aiEnv) openWizardAtService(t *testing.T) *rig {
	t.Helper()
	r := e.open(t)
	waitText(r, locales.SettingsAiWorkbenchStatusOverview())
	r.clickNode(gfx.RoleButton, locales.SettingsQuickSetupOpen())
	waitUntil(r, "向导", func() bool { return r.dialogOpen(locales.SettingsQuickSetupTitle()) })
	r.clickNode(gfx.RoleButton, locales.SettingsQuickSetupNext())
	waitUntil(r, "服务步骤", func() bool { return r.has(gfx.RoleTextField, "API Key") })
	return r
}

// 向导验证失败：原因里带着密钥时，向导里显示的与记录的都是脱敏后的文字。
func TestAIQuickSetupTestFailureKeepsSecretOut(t *testing.T) {
	e := newAIEnv()
	e.ai.PutConfig(services.AIConfig{Routes: map[string]string{}})
	e.ai.PutFetchResult(services.AIFetchModelsResult{Success: true, Models: []services.AIRemoteModel{{ID: "gpt-4o-mini"}}})
	e.ai.PutTestOutcome(services.AITestOutcome{Success: false, Error: "gateway said bad key " + aiSecret + " (HTTP 500)"})
	r := e.openWizardAtService(t)
	r.typeInto("API Key", aiSecret)
	r.clickNode(gfx.RoleButton, locales.SettingsQuickSetupFetchModels())
	waitUntil(r, "模型步骤", func() bool { return r.has(gfx.RoleTextField, locales.SettingsQuickSetupBasic()) })
	r.clickNode(gfx.RoleButton, locales.SettingsQuickSetupNext())
	waitUntil(r, "验证步骤", func() bool { return r.has(gfx.RoleButton, locales.SettingsQuickSetupTestEach()) })
	r.clickNode(gfx.RoleButton, locales.SettingsQuickSetupTestEach())
	want := locales.SettingsQuickSetupTestFailed(locales.SettingsQuickSetupBasic(), "gateway said bad key *** (HTTP 500)")
	waitText(r, want)
	e.assertNoSecret(t, r)
}

// 向导保存：每个角色都必须选了模型，缺了就停在原步骤给出原因，什么都不写；补上之后能正常保存。
func TestAIQuickSetupSaveRequiresModelForEveryRole(t *testing.T) {
	e := newAIEnv()
	e.ai.PutConfig(services.AIConfig{Routes: map[string]string{}})
	e.ai.PutFetchResult(services.AIFetchModelsResult{Success: true}) // 拉取成功但列表为空：模型名要手填
	r := e.openWizardAtService(t)
	r.typeInto("API Key", aiSecret)
	r.clickNode(gfx.RoleButton, locales.SettingsQuickSetupFetchModels())
	waitUntil(r, "模型步骤", func() bool { return r.has(gfx.RoleTextField, locales.SettingsQuickSetupBasic()) })
	r.clickNode(gfx.RoleButton, locales.SettingsQuickSetupNext())
	waitUntil(r, "验证步骤", func() bool { return r.has(gfx.RoleButton, locales.SettingsQuickSetupSaveAll()) })

	r.clickNode(gfx.RoleButton, locales.SettingsQuickSetupSaveAll())
	waitText(r, locales.SettingsQuickSetupModelRequired())
	r.frames(5)
	if n := len(e.ai.Saves()); n != 0 {
		t.Fatalf("没选模型不应写盘，实际写了 %d 次", n)
	}
	if !r.dialogOpen(locales.SettingsQuickSetupTitle()) {
		t.Fatal("缺模型时向导应留着")
	}

	// 回到上一步手填模型名，再保存。
	r.clickNode(gfx.RoleButton, locales.SettingsQuickSetupBack())
	waitUntil(r, "模型步骤", func() bool { return r.has(gfx.RoleTextField, locales.SettingsQuickSetupBasic()) })
	r.typeInto(locales.SettingsQuickSetupBasic(), "gpt-4o-mini")
	r.clickNode(gfx.RoleButton, locales.SettingsQuickSetupNext())
	waitUntil(r, "验证步骤", func() bool { return r.has(gfx.RoleButton, locales.SettingsQuickSetupSaveAll()) })
	r.clickNode(gfx.RoleButton, locales.SettingsQuickSetupSaveAll())
	e.waitSaves(r, 1)
	if cfg := e.lastSave(t); len(cfg.Models) != 1 || cfg.Models[0].Model != "gpt-4o-mini" {
		t.Errorf("应写入手填的模型：%+v", cfg.Models)
	}
}

// 清空密钥框后显示的占位符是掩码形式；明文永远不能出现在画面上（占位符也是画面文字）。
func TestAIKeyPlaceholderIsMaskedWhenFieldCleared(t *testing.T) {
	e := newAIEnv()
	r := e.openProvider(t)
	name := locales.SettingsAiWorkbenchEndpointApiKey()
	r.clickNode(gfx.RoleTextField, name)
	r.keys("cmd+a")
	r.keys("backspace")
	r.frames(3)
	if got := r.fieldValue(name); got != "" {
		t.Fatalf("密钥框应已清空，得到 %q", got)
	}
	if r.hasText(aiSecret) {
		t.Error("清空后的占位符不应显示明文密钥")
	}
	if !r.hasText(services.MaskAPIKey(aiSecret)) {
		t.Errorf("清空后应显示掩码占位符 %q", services.MaskAPIKey(aiSecret))
	}
}

// 向导目标步：取消勾选全部目标后，「下一步」按钮应禁用且无法推进；重新勾选后可正常进入服务步骤。
func TestAIQuickSetupUncheckingAllGoalsDisablesNext(t *testing.T) {
	e := newAIEnv()
	e.ai.PutConfig(services.AIConfig{Routes: map[string]string{}})
	r := e.open(t)
	waitText(r, locales.SettingsAiWorkbenchStatusOverview())
	r.clickNode(gfx.RoleButton, locales.SettingsQuickSetupOpen())
	waitUntil(r, "向导", func() bool { return r.dialogOpen(locales.SettingsQuickSetupTitle()) })

	goalBtn := locales.SettingsQuickSetupBasic() + " " + locales.SettingsQuickSetupBasicDesc()
	r.clickNode(gfx.RoleButton, goalBtn)
	r.frames(2)
	r.clickNode(gfx.RoleButton, locales.SettingsQuickSetupNext())
	r.frames(3)
	if r.has(gfx.RoleTextField, "API Key") {
		t.Fatal("取消全部目标后，下一步不应推进到服务步骤")
	}

	r.clickNode(gfx.RoleButton, goalBtn)
	r.frames(2)
	r.clickNode(gfx.RoleButton, locales.SettingsQuickSetupNext())
	waitUntil(r, "服务步骤", func() bool { return r.has(gfx.RoleTextField, "API Key") })
}
