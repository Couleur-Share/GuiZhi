package settings_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	sscreen "guizhi-mygo-shell/viva/screens/settings"
)

// aiSecret 是配置里的 API Key：只能出现在密码框的内容里，界面文字、提示、详情与日志里一个字都不能有。
const aiSecret = "sk-live-9f2c-SECRET-7d41"

// aiEnv 是「AI 模型服务」页的测试环境：假的 AI 配置服务。
type aiEnv struct {
	*sysEnv
	ai *servicestest.AISettingsFake
}

// seedAIConfig 是一个 OpenAI 端点加两个模型（对话加视觉的默认模型、向量模型）。
func seedAIConfig() services.AIConfig {
	const url = "https://api.openai.com"
	return services.AIConfig{
		Providers: []services.AIProvider{{ID: "p1", Name: "Main", Provider: "openai", APIProtocol: services.AIProtocolOpenAI, APIKey: aiSecret, APIURL: url, Enabled: true}},
		Models: []services.AIModel{
			{ID: "m1", ProviderID: "p1", Provider: "openai", APIProtocol: services.AIProtocolOpenAI, APIKey: aiSecret, APIURL: url, Model: "gpt-4o", IsDefault: true, Enabled: true,
				Capabilities: services.AICapabilities{Chat: true, Vision: true}},
			{ID: "m2", ProviderID: "p1", Provider: "openai", APIProtocol: services.AIProtocolOpenAI, APIKey: aiSecret, APIURL: url, Model: "text-embedding-3-small", Enabled: true,
				Capabilities: services.AICapabilities{Embedding: true}},
		},
		Routes: map[string]string{},
	}
}

func newAIEnv() *aiEnv {
	e := &aiEnv{sysEnv: newEnv(), ai: &servicestest.AISettingsFake{}}
	e.ai.PutConfig(seedAIConfig())
	return e
}

func (e *aiEnv) open(t *testing.T) *rig {
	t.Helper()
	return e.openAt(t, 1280)
}

func (e *aiEnv) openAt(t *testing.T, width float32) *rig {
	t.Helper()
	return openPageAt(t, width, func(look *kit.Look) func(c ui.C) {
		a := &sscreen.AI{Look: look, Guard: e.guard(), Service: e.ai, Wake: func() {}}
		t.Cleanup(a.Close)
		return a.Page
	})
}

// assertNoSecret 核对提示、详情、日志与画面文字里没有 API Key。
func (e *aiEnv) assertNoSecret(t *testing.T, r *rig) {
	t.Helper()
	all := fmt.Sprint(e.notify.Queued(), e.logs.all())
	if strings.Contains(all, aiSecret) {
		t.Errorf("提示或日志里出现了 API Key：%s", all)
	}
	if r != nil && r.hasText(aiSecret) {
		t.Error("画面文字里出现了 API Key 明文")
	}
}

// lastSave 是最近一次成功写盘的配置。
func (e *aiEnv) lastSave(t *testing.T) services.AIConfig {
	t.Helper()
	saves := e.ai.Saves()
	if len(saves) == 0 {
		t.Fatal("还没有任何写盘")
	}
	return saves[len(saves)-1]
}

func (e *aiEnv) waitSaves(r *rig, n int) {
	r.t.Helper()
	waitUntil(r, fmt.Sprintf("写盘 %d 次", n), func() bool { return len(e.ai.Saves()) >= n })
}

func TestAIShowsOverviewAndProviders(t *testing.T) {
	e := newAIEnv()
	r := e.open(t)
	waitButton(r, "Main")
	// 状态总览：六条路由各一张卡；主文本与向量已解析到模型，转写与文生图没有候选。
	for _, label := range []string{
		locales.SettingsAiWorkbenchRouteMainText(), locales.SettingsAiWorkbenchRouteFastText(), locales.SettingsAiWorkbenchRouteVisionText(),
		locales.SettingsAiWorkbenchRouteEmbedding(), locales.SettingsAiWorkbenchRouteAudioText(), locales.SettingsAiWorkbenchRouteImageGen(),
	} {
		if !r.hasText(label) {
			t.Errorf("状态总览缺少「%s」", label)
		}
	}
	if !r.hasText("gpt-4o") || !r.hasText("text-embedding-3-small") {
		t.Error("路由应显示解析到的模型名")
	}
	if !r.hasText(locales.SettingsAiWorkbenchNotConfigured()) {
		t.Error("没有候选的路由应显示「未配置」")
	}
	// 用量块在没有记录时是一行说明。
	if !r.hasText(locales.SettingsAiUsageEmpty(30)) {
		t.Error("没有用量记录时应有说明")
	}
	if got := e.ai.UsageDays(); len(got) != 1 || got[0] != 30 {
		t.Errorf("用量应读取一次最近 30 天，实际 %v", got)
	}
	e.assertNoSecret(t, r)
}

func TestAILoadFailureShowsRetry(t *testing.T) {
	e := newAIEnv()
	e.ai.FailLoad(errors.New("read ai-models.json: access denied"))
	r := e.open(t)
	waitText(r, locales.SettingsAi())
	waitUntil(r, "读取失败状态", func() bool { return r.has(gfx.RoleButton, locales.CommonRetry()) })
	if r.hasText(locales.SettingsAiWorkbenchStatusOverview()) || r.has(gfx.RoleButton, "Main") {
		t.Error("读取失败不能渲染成正常页或空态")
	}
	if len(e.logs.all()) == 0 {
		t.Error("读取失败应记入日志")
	}
	e.ai.FailLoad(nil)
	r.clickNode(gfx.RoleButton, locales.CommonRetry())
	waitButton(r, "Main")
}

func TestAIRouteSwitchSavesAndRollsBackOnFailure(t *testing.T) {
	e := newAIEnv()
	e.ai.PutConfig(func() services.AIConfig {
		c := seedAIConfig()
		c.Models = append(c.Models, services.AIModel{ID: "m3", ProviderID: "p1", Provider: "openai", APIProtocol: services.AIProtocolOpenAI, APIKey: aiSecret,
			APIURL: "https://api.openai.com", Model: "gpt-4.1-mini", Enabled: true, Capabilities: services.AICapabilities{Chat: true}})
		return c
	}())
	r := e.open(t)
	waitButton(r, "Main")
	label := locales.SettingsAiWorkbenchRouteFastText()

	// 切换「快速文本」到 gpt-4.1-mini：立即落盘。
	r.pick(label, "gpt-4.1-mini (Main)")
	e.waitSaves(r, 1)
	if got := e.lastSave(t).Routes[services.AIRouteFastText]; got != "m3" {
		t.Fatalf("写入的路由 = %q", got)
	}

	// 写盘失败：回到上一次成功写入的配置，并弹出带原因的错误。
	e.ai.FailSave(errors.New("write ai-models.json: disk full"))
	r.pick(label, locales.SettingsAiWorkbenchFollowGlobalDefault())
	waitUntil(r, "错误提示", func() bool { return len(e.toasts(kit.ToastError)) == 1 })
	if !strings.Contains(e.toasts(kit.ToastError)[0].Detail, "disk full") {
		t.Errorf("错误详情应带原因：%+v", e.toasts(kit.ToastError)[0])
	}
	if got := e.ai.Current().Routes[services.AIRouteFastText]; got != "m3" {
		t.Errorf("失败后磁盘上的配置应保持原样，路由 = %q", got)
	}
	e.assertNoSecret(t, r)
}

func TestAIRouteSelectDisabledWithoutCandidates(t *testing.T) {
	e := newAIEnv()
	r := e.open(t)
	waitButton(r, "Main")
	n, ok := r.node(gfx.RoleCombobox, locales.SettingsAiWorkbenchRouteImageGen())
	if !ok {
		t.Fatal("找不到文生图路由的下拉")
	}
	if !n.Disabled {
		t.Error("没有候选模型的路由，下拉应置灰")
	}
}
