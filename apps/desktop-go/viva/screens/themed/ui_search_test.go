package themed

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

const secretKey = "sk-live-secret-0123456789"

func searchRig(t *testing.T, st services.ThemedSearchStatus) *uiRig {
	t.Helper()
	r := newUIRig(t, withHost(hostSearch), func(r *uiRig) {
		if st.Provider != "" {
			r.svc.SetSearch(st)
		}
	})
	r.settle()
	return r
}

func keyFieldName() string { return ProviderName(ProviderTavily) + " API Key" }

// typeKey 点进密钥框输入 secretKey。
func (r *uiRig) typeKey() {
	r.t.Helper()
	r.click(gfx.RoleTextField, keyFieldName())
	r.typeText(secretKey)
}

// wantNoSecret 断言密钥明文没有出现在画面、提示、日志里。
func (r *uiRig) wantNoSecret() {
	r.t.Helper()
	if r.textHas(secretKey) {
		r.t.Fatalf("画面上不该出现密钥明文：%q", r.session.Texts())
	}
	for _, tm := range r.toasts() {
		if strings.Contains(tm.Message+tm.Detail, secretKey) {
			r.t.Fatalf("提示里不该出现密钥明文：%+v", tm)
		}
	}
	for _, e := range r.log.entries() {
		if strings.Contains(e, secretKey) {
			r.t.Fatalf("日志里不该出现密钥明文：%q", e)
		}
	}
	for _, c := range r.svc.Calls().SearchConfigs {
		if strings.Contains(c, secretKey) {
			r.t.Fatalf("调用记录里不该出现密钥明文：%q", c)
		}
	}
}

func TestSearchShowsSavedConfiguration(t *testing.T) {
	r := searchRig(t, services.ThemedSearchStatus{Provider: ProviderTavily, Persistent: true, ConfiguredProviders: []string{ProviderTavily}, Configured: true})
	for _, want := range []string{locales.SettingsReadingSearchTitle(), locales.SettingsReadingSearchDefault(), locales.SettingsReadingSearchProvider()} {
		if !r.textHas(want) {
			t.Fatalf("应显示 %q；画面上有 %q", want, r.session.Texts())
		}
	}
	if line := r.screen.Search.View().StatusLine(); line == "" || !r.textHas(line) {
		t.Fatalf("应显示状态行 %q；画面上有 %q", line, r.session.Texts())
	}
	if !r.has(gfx.RoleTextField, keyFieldName()) {
		t.Fatal("tavily 需要密钥输入框")
	}
	if n, ok := r.node(gfx.RoleButton, locales.CommonSave()); !ok || !n.Disabled {
		t.Fatalf("没填新密钥也没换服务，「保存」应禁用：%+v ok=%v", n, ok)
	}
	if n, ok := r.node(gfx.RoleButton, locales.SettingsReadingSearchClearKey()); !ok || n.Disabled {
		t.Fatalf("已配置的服务应能清除密钥：%+v ok=%v", n, ok)
	}
}

func TestSearchLoadFailureOffersRetry(t *testing.T) {
	r := newUIRig(t, withHost(hostSearch), func(r *uiRig) { r.svc.Fail("searchStatus", errTest("config file is locked")) })
	r.settle()
	if !r.textHas("config file is locked") {
		t.Fatalf("读取失败应显示原因；画面上有 %q", r.session.Texts())
	}
	if r.textHas(locales.SettingsReadingSearchDefault()) {
		t.Fatal("读取失败不该画出可编辑的表单")
	}
	r.svc.Fail("searchStatus", nil)
	r.clickButton(locales.LibrarySnapshotRetryLoad())
	r.settle()
	if !r.textHas(locales.SettingsReadingSearchDefault()) {
		t.Fatalf("重试成功后应出现表单；画面上有 %q", r.session.Texts())
	}
}

func TestSearchSaveKeyConfiguresProviderAndClearsField(t *testing.T) {
	r := searchRig(t, services.ThemedSearchStatus{Provider: ProviderTavily, Persistent: true})
	if n, ok := r.node(gfx.RoleButton, locales.CommonSave()); !ok || !n.Disabled {
		t.Fatalf("没填密钥时「保存」应禁用：%+v ok=%v", n, ok)
	}
	r.typeKey()
	if !r.screen.Search.View().KeyFilled {
		t.Fatal("输入后 Store 应知道密钥已填")
	}
	r.clickButton(locales.CommonSave())
	r.settle()
	if got := r.svc.Calls().SearchConfigs; len(got) != 1 {
		t.Fatalf("应保存一次：%q", got)
	}
	v := r.screen.Search.View()
	if v.KeyFilled || v.Status == nil || !v.Configured() {
		t.Fatalf("保存后应已配置且密钥框清空：%+v", v)
	}
	if got := r.screen.search.key.Text(); got != "" {
		t.Fatalf("保存成功后输入框应清空，实际 %d 个字符", len(got))
	}
	if tm := r.lastToast(); tm.Message != locales.SettingsReadingSearchSaved() {
		t.Fatalf("应提示已保存：%+v", tm)
	}
	r.wantNoSecret()
}

func TestSearchSaveFailureKeepsKeyAndShowsReason(t *testing.T) {
	r := searchRig(t, services.ThemedSearchStatus{Provider: ProviderTavily, Persistent: true})
	r.svc.Fail("configureSearch", errTest("provider rejected the request"))
	r.typeKey()
	r.clickButton(locales.CommonSave())
	r.settle()
	if !r.textHas("provider rejected the request") {
		t.Fatalf("失败应在表单里显示原因；画面上有 %q", r.session.Texts())
	}
	if tm := r.lastToast(); tm.Detail == "" {
		t.Fatalf("失败提示应带可复制的原因：%+v", tm)
	}
	if !r.screen.Search.View().KeyFilled || r.screen.search.key.Text() != secretKey {
		t.Fatal("保存失败应保留已输入的密钥，便于重试")
	}
	r.wantNoSecret()
	r.svc.Fail("configureSearch", nil)
	r.clickButton(locales.CommonSave())
	r.settle()
	if !r.screen.Search.View().Configured() {
		t.Fatal("重试应成功")
	}
}

func TestSearchTestConnectionRunsWithTestFlag(t *testing.T) {
	r := searchRig(t, services.ThemedSearchStatus{Provider: ProviderTavily, Persistent: true})
	r.typeKey()
	r.clickButton(locales.SettingsTestConnection())
	r.settle()
	if got := r.svc.Calls().SearchConfigs; len(got) != 1 {
		t.Fatalf("应调用一次：%q", got)
	}
	// 记录里隐去了标记，用提示区分「测试通过」与「已保存」。
	if tm := r.lastToast(); tm.Message != locales.SettingsReadingSearchTestPassed(ProviderName(ProviderTavily)) {
		t.Fatalf("应提示连接测试通过：%+v", tm)
	}
	r.wantNoSecret()
}

func TestSearchClearKeyRemovesConfiguration(t *testing.T) {
	r := searchRig(t, services.ThemedSearchStatus{Provider: ProviderTavily, Persistent: true, Configured: true, ConfiguredProviders: []string{ProviderTavily}})
	r.clickButton(locales.SettingsReadingSearchClearKey())
	r.settle()
	if r.screen.Search.View().Configured() {
		t.Fatalf("清除后不该再是已配置：%+v", r.screen.Search.View())
	}
	if tm := r.lastToast(); tm.Message != locales.SettingsReadingSearchKeyCleared(ProviderName(ProviderTavily)) {
		t.Fatalf("应提示已清除：%+v", tm)
	}
}

func TestSearchDefaultSwitchSavesImmediately(t *testing.T) {
	r := searchRig(t, services.ThemedSearchStatus{Provider: ProviderTavily, Persistent: true})
	r.click(gfx.RoleSwitch, locales.SettingsReadingSearchDefault())
	r.settle()
	if got := r.svc.Calls().SearchConfigs; len(got) != 1 {
		t.Fatalf("拨动默认开关应立即保存一次：%q", got)
	}
	if tm := r.lastToast(); tm.Message != locales.SettingsReadingSearchDefaultSaved() {
		t.Fatalf("应提示默认设置已保存：%+v", tm)
	}
	if st := r.screen.Search.View().Status; st == nil || !st.DefaultEnabled {
		t.Fatalf("默认开关应已开启：%+v", st)
	}
	if n, ok := r.node(gfx.RoleSwitch, locales.SettingsReadingSearchDefault()); !ok || !n.Checked {
		t.Fatalf("开关应显示为开：%+v ok=%v", n, ok)
	}
}

func TestSearchNativeProviderHasNoKeyInput(t *testing.T) {
	r := searchRig(t, services.ThemedSearchStatus{Provider: ProviderTavily, Persistent: true, NativeModel: "gpt-test"})
	r.click(gfx.RoleCombobox, locales.SettingsReadingSearchProvider())
	r.click(gfx.RoleOption, ProviderName(ProviderNative))
	r.settle()
	if r.has(gfx.RoleTextField, keyFieldName()) {
		t.Fatal("原生搜索不需要密钥")
	}
	if r.hasButton(locales.SettingsReadingSearchClearKey()) {
		t.Fatal("原生搜索没有密钥可清除")
	}
	if !r.textHas(locales.SettingsReadingSearchNativeModel("gpt-test")) {
		t.Fatalf("应说明将使用的主文本模型；画面上有 %q", r.session.Texts())
	}
	if !r.textHas(locales.SettingsReadingSearchNativeHint()) {
		t.Fatal("应提示原生搜索的前提")
	}
}

func TestSearchLeavingBlockClearsTypedKey(t *testing.T) {
	r := searchRig(t, services.ThemedSearchStatus{Provider: ProviderTavily, Persistent: true})
	r.typeKey()
	if !r.screen.Search.View().KeyFilled {
		t.Fatal("前置：密钥应已填")
	}
	r.host = hostNone
	r.settle()
	if r.screen.Search.View().KeyFilled {
		t.Fatal("离开这一块后 Store 里的密钥应清空")
	}
	r.host = hostSearch
	r.settle()
	if got := r.screen.search.key.Text(); got != "" {
		t.Fatalf("回来后输入框应是空的，实际 %d 个字符", len(got))
	}
	r.wantNoSecret()
}
