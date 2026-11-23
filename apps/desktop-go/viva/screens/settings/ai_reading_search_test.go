package settings_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	sscreen "guizhi-mygo-shell/viva/screens/settings"
	themedscreen "guizhi-mygo-shell/viva/screens/themed"
)

// 本文件测 AI 设置页里「联网搜索」块（主题阅读的搜索服务，台账 B-074）的接线：注入了才出现、夹在场景默认模型与用量之间、
// 没注入时页面一点不变。块里的界面行为（读取、保存、清除、密钥不外泄）在 viva/screens/themed 的 L4 里测，这里只验证宿主这一侧。

const searchMarker = "reading-search-block"

// texts 是画面上全部文字（按绘制顺序）。
func (r *rig) texts() []string { return r.s.Texts() }

func indexOfText(texts []string, sub string) int {
	return slices.IndexFunc(texts, func(s string) bool { return strings.Contains(s, sub) })
}

// openWithSearch 打开 AI 设置页，search 为注入的搜索块（nil 表示没接）。
func (e *aiEnv) openWithSearch(t *testing.T, search func(look *kit.Look) func(c ui.C), after func(c ui.C)) *rig {
	t.Helper()
	return openPageAt(t, 1280, func(look *kit.Look) func(c ui.C) {
		a := &sscreen.AI{Look: look, Guard: e.guard(), Service: e.ai, Wake: func() {}}
		if search != nil {
			a.Search = search(look)
		}
		t.Cleanup(a.Close)
		return func(c ui.C) {
			a.Page(c)
			if after != nil {
				after(c)
			}
		}
	})
}

func TestAISearchBlockIsAbsentUnlessInjected(t *testing.T) {
	e := newAIEnv()
	r := e.openWithSearch(t, nil, nil)
	waitButton(r, mainProviderBtn)
	r.scrollToEnd()
	if r.hasText(locales.SettingsReadingSearchTitle()) || r.hasText(searchMarker) {
		t.Fatalf("没注入搜索块时页面不该有它；画面上有 %q", r.texts())
	}
	if !r.hasText(locales.SettingsAiUsageEmpty(30)) {
		t.Fatal("没注入时其余版面照旧：用量块仍在")
	}
	e.assertNoSecret(t, r)
}

func TestAISearchBlockSitsBetweenScenarioDefaultsAndUsage(t *testing.T) {
	e := newAIEnv()
	hook := func(look *kit.Look) func(c ui.C) {
		return func(c ui.C) { look.Text(c, searchMarker, kit.TextSM, kit.WeightNormal, look.T.Foreground) }
	}
	r := e.openWithSearch(t, hook, nil)
	waitButton(r, mainProviderBtn)
	r.scrollToEnd()
	if !r.hasText(searchMarker) {
		t.Fatalf("注入后应出现搜索块；画面上有 %q", r.texts())
	}
	texts := r.texts()
	marker, usage := indexOfText(texts, searchMarker), indexOfText(texts, locales.SettingsAiUsageEmpty(30))
	if usage < 0 || marker < 0 || marker > usage {
		t.Fatalf("搜索块应在用量块之前（与渲染层同序）：搜索块 %d，用量 %d；%q", marker, usage, texts)
	}
}

func TestAISearchBlockWorksInsideTheAIPage(t *testing.T) {
	e := newAIEnv()
	svc := &servicestest.ThemedFake{}
	svc.SetSearch(services.ThemedSearchStatus{Provider: themedscreen.ProviderTavily, Persistent: true})
	var th *themedscreen.Screen
	r := e.openWithSearch(t, func(look *kit.Look) func(c ui.C) {
		th = themedscreen.New(svc, e.guard(), nil)
		th.Look = look
		th.SetSpawn(func(fn func()) { fn() })
		return th.SearchPanel
	}, func(ui.C) { th.EndFrame() })
	waitButton(r, mainProviderBtn)
	r.scrollToEnd()
	waitUntil(r, "搜索块读到配置", func() bool { return r.hasText(locales.SettingsReadingSearchDefault()) })
	if !r.hasText(locales.SettingsReadingSearchTitle()) {
		t.Fatalf("应显示块标题；画面上有 %q", r.texts())
	}

	const key = "tvly-page-secret-42"
	r.typeInto(themedscreen.ProviderName(themedscreen.ProviderTavily)+" API Key", key)
	r.clickNode(gfx.RoleButton, locales.CommonSave())
	waitUntil(r, "保存成功提示", func() bool { return len(e.toasts(kit.ToastSuccess)) == 1 })
	if got := e.toasts(kit.ToastSuccess)[0].Message; got != locales.SettingsReadingSearchSaved() {
		t.Fatalf("提示 = %q", got)
	}
	if got := svc.Calls().SearchConfigs; len(got) != 1 {
		t.Fatalf("应保存一次搜索配置：%q", got)
	}
	if n := len(e.ai.Saves()); n != 0 {
		t.Fatalf("保存搜索配置不该改 AI 模型配置，写盘了 %d 次", n)
	}
	if strings.Contains(strings.Join(r.texts(), "")+strings.Join(svcRecords(e), ""), key) {
		t.Fatal("密钥明文不该出现在画面、提示或日志里")
	}
}

func svcRecords(e *aiEnv) []string {
	var out []string
	for _, q := range e.notify.Queued() {
		out = append(out, q.Message, q.Detail)
	}
	return append(out, e.logs.all()...)
}
