package overlays

import (
	"errors"
	"slices"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/locales"
)

// 首次使用设置清单界面（L4）：文本模型主卡、折叠的可选项、跳转配置、稍后再说与开始采集。

func newSetupUI(t *testing.T, mutate ...func(*uiRig)) *uiRig {
	t.Helper()
	r := newUIRig(t, append([]func(*uiRig){withSetup}, mutate...)...)
	r.screen.Setup.RequestOpen()
	r.frames(3)
	return r
}

func (r *uiRig) setupOpen() bool { return r.has(gfx.RoleGroup, locales.SetupTitle()) }

func (r *uiRig) setupDismissed() bool { return prefTrue(r.prefs, settings.PrefSetupDismissed) }

func withChatModel(r *uiRig) {
	r.ai.PutConfig(services.AIConfig{Models: []services.AIModel{chatModel("m1")}})
}

func TestUISetupClosedDrawsNothing(t *testing.T) {
	r := newUIRig(t, withSetup)
	if r.setupOpen() {
		t.Fatal("没打开时不应画对话框")
	}
}

// 什么都没配：标题、说明、文本模型卡与「快速配置」，没有「开始采集」，可选项折叠。
func TestUISetupUnconfiguredShowsMainCardAndCollapsedOptional(t *testing.T) {
	r := newSetupUI(t)
	if !r.setupOpen() || !r.has(gfx.RoleHeading, locales.SetupTitle()) {
		t.Fatalf("应显示标题：%q", r.session.Texts())
	}
	for _, want := range []string{locales.SetupIntro(), locales.SetupItemTextModel(), locales.SetupItemTextModelDesc()} {
		if !r.hasWrapped(want) {
			t.Errorf("缺少 %q；画面上有 %q", want, r.session.Texts())
		}
	}
	if !r.hasButton(locales.SetupQuickSetup()) || !r.hasButton(locales.SetupLater()) {
		t.Error("应有快速配置与稍后再说")
	}
	if r.hasButton(locales.SetupStartCapture()) {
		t.Error("文本模型没就绪时不应有「开始采集」")
	}
	if !r.hasButton(locales.SetupMoreOptional(2)) {
		t.Errorf("应有折叠钮：%q", r.session.Texts())
	}
	if r.hasText(locales.SetupItemTranscription()) || r.hasText(locales.SetupItemEmbedding()) {
		t.Error("可选项默认折叠")
	}
}

// 展开与再折叠。
func TestUISetupOptionalToggle(t *testing.T) {
	r := newSetupUI(t)
	r.clickButton(locales.SetupMoreOptional(2))
	for _, want := range []string{locales.SetupItemTranscription(), locales.SetupItemTranscriptionDesc(), locales.SetupItemEmbedding(), locales.SetupItemEmbeddingDesc()} {
		if !r.hasWrapped(want) {
			t.Errorf("展开后缺少 %q；画面上有 %q", want, r.session.Texts())
		}
	}
	r.clickButton(locales.SetupMoreOptional(2))
	if r.hasText(locales.SetupItemEmbedding()) {
		t.Error("再点应折叠")
	}
}

// 「快速配置」：走快速配置并跳到 AI 设置分区，对话框收起但不永久关闭（下次启动仍提示）。
func TestUISetupQuickSetupJumpsToAISettings(t *testing.T) {
	r := newSetupUI(t)
	r.clickButton(locales.SetupQuickSetup())
	if got := r.called(); !slices.Equal(got, []string{"quick", "section:" + string(SetupSectionAI)}) {
		t.Fatalf("应先发快速配置请求再跳 AI 分区：%v", got)
	}
	if r.setupOpen() {
		t.Error("跳转后应收起对话框")
	}
	if r.setupDismissed() {
		t.Error("去配置不应永久标记为已处理")
	}
}

// 可选项的「去配置」：转写进采集分区（无快速配置），语义检索进 AI 分区（有快速配置）。
func TestUISetupOptionalConfigureLinks(t *testing.T) {
	r := newSetupUI(t)
	r.clickButton(locales.SetupMoreOptional(2))
	if n := countNodes(r, gfx.RoleButton, locales.SetupConfigure()); n != 2 {
		t.Fatalf("两个可选项都应有「去配置」：%d；画面上有 %q", n, r.session.Texts())
	}
	nodes := nodesOf(r, gfx.RoleButton, locales.SetupConfigure())
	r.clickAt(nodes[0].Bounds.X+nodes[0].Bounds.Width/2, nodes[0].Bounds.Y+nodes[0].Bounds.Height/2)
	if got := r.called(); !slices.Equal(got, []string{"section:" + string(SetupSectionCapture)}) {
		t.Errorf("转写应跳采集分区：%v", got)
	}

	r2 := newSetupUI(t)
	r2.clickButton(locales.SetupMoreOptional(2))
	nodes = nodesOf(r2, gfx.RoleButton, locales.SetupConfigure())
	r2.clickAt(nodes[1].Bounds.X+nodes[1].Bounds.Width/2, nodes[1].Bounds.Y+nodes[1].Bounds.Height/2)
	if got := r2.called(); !slices.Equal(got, []string{"quick", "section:" + string(SetupSectionAI)}) {
		t.Errorf("语义检索应走快速配置并跳 AI 分区：%v", got)
	}
}

// 文本模型就绪：主卡显示「已完成」、没有快速配置，出现「开始采集」；已配好的可选项显示「已完成」。
func TestUISetupReadyShowsDoneAndStartCapture(t *testing.T) {
	r := newSetupUI(t, withChatModel)
	if !r.hasText(locales.SetupDone()) {
		t.Fatalf("主卡应显示已完成：%q", r.session.Texts())
	}
	if r.hasButton(locales.SetupQuickSetup()) {
		t.Error("就绪后不应再有快速配置")
	}
	if !r.hasButton(locales.SetupStartCapture()) {
		t.Fatal("文本模型就绪后应有「开始采集」")
	}
	r.clickButton(locales.SetupStartCapture())
	if got := r.called(); !slices.Equal(got, []string{"capture"}) {
		t.Errorf("应打开快速采集：%v", got)
	}
	if r.setupOpen() || !r.setupDismissed() {
		t.Error("开始采集应关闭对话框并记为已处理")
	}
}

// 本地转写引擎装了：展开后转写项显示「已完成」，只剩语义检索有「去配置」。
func TestUISetupInstalledTranscriptionShowsDone(t *testing.T) {
	r := newSetupUI(t, func(r *uiRig) { r.capture.SetFunasrStatus(services.FunasrStatus{Installed: true}) })
	r.clickButton(locales.SetupMoreOptional(2))
	if !r.hasText(locales.SetupDone()) || countNodes(r, gfx.RoleButton, locales.SetupConfigure()) != 1 {
		t.Errorf("转写应显示已完成、只剩一个去配置：%q", r.session.Texts())
	}
}

// 「稍后再说」与 Esc、点遮罩、关闭钮：都关闭并永久记为已处理。
func TestUISetupCloseWaysDismiss(t *testing.T) {
	cases := []struct {
		name string
		do   func(r *uiRig)
	}{
		{"稍后再说", func(r *uiRig) { r.clickButton(locales.SetupLater()) }},
		{"Esc", func(r *uiRig) { r.keys("Escape") }},
		{"关闭钮", func(r *uiRig) { r.clickButton(locales.CommonClose()) }},
		{"点遮罩", func(r *uiRig) { r.clickAt(10, 10) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newSetupUI(t)
			c.do(r)
			if r.setupOpen() || r.screen.Setup.View().Open {
				t.Error("应关闭")
			}
			if !r.setupDismissed() {
				t.Error("应永久记为已处理")
			}
		})
	}
}

// AI 配置读不出来：如实说明，不能让用户以为自己真的没配。
func TestUISetupLoadErrorIsShown(t *testing.T) {
	r := newSetupUI(t, func(r *uiRig) { r.ai.FailLoad(errors.New("config file is corrupt")) })
	if !r.hasText(locales.NativeSetupLoadError("config file is corrupt")) {
		t.Errorf("应显示读取失败的原因：%q", r.session.Texts())
	}
}
