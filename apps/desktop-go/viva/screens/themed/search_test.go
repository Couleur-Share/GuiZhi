package themed

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

func newSearchRig(t *testing.T, st services.ThemedSearchStatus) (*rig, *SearchSettings) {
	r := newRig(t)
	r.svc.SetSearch(st)
	s := NewSearchSettings(r.svc, r.guard, func() {})
	return r, s
}

func TestSearchLoadSelectsSavedProvider(t *testing.T) {
	_, s := newSearchRig(t, services.ThemedSearchStatus{Provider: ProviderAnySearch, ConfiguredProviders: []string{ProviderAnySearch}, Configured: true, Persistent: true})
	if v := s.View(); v.Status != nil || v.Provider != ProviderTavily {
		t.Fatalf("读取前默认选中 tavily: %+v", v)
	}
	if !s.Load() {
		t.Fatal("读取应成功")
	}
	v := s.View()
	if v.Provider != ProviderAnySearch || !v.Configured() || v.Switching() || v.Busy != "" {
		t.Fatalf("%+v", v)
	}
}

func TestSearchLoadFailureIsInlineRetryNoToast(t *testing.T) {
	r, s := newSearchRig(t, services.ThemedSearchStatus{})
	r.svc.Fail(servicestest.ThemedOpSearchStatus, errors.New("config unreadable"))
	if s.Load() {
		t.Fatal("读取失败")
	}
	v := s.View()
	if v.Status != nil || v.Error != "config unreadable" || v.Busy != "" {
		t.Fatalf("%+v", v)
	}
	if len(r.toasts()) != 0 {
		t.Fatal("读取失败在表单里显示，不弹提示")
	}
	if len(r.log.entries()) != 1 {
		t.Fatal("应留痕")
	}
	r.svc.Fail(servicestest.ThemedOpSearchStatus, nil)
	if !s.Load() || s.View().Error != "" {
		t.Fatal("重试应恢复")
	}
}

func TestSearchButtonRules(t *testing.T) {
	_, s := newSearchRig(t, services.ThemedSearchStatus{Provider: ProviderTavily, ConfiguredProviders: []string{ProviderTavily}, Configured: true, NativeModel: "gpt"})
	s.Load()
	v := s.View()
	if v.CanSave() || !v.CanTest() || !v.CanClear() {
		t.Fatalf("已配置且没填新密钥：不能保存，能测试能清除: save=%v test=%v clear=%v", v.CanSave(), v.CanTest(), v.CanClear())
	}
	s.SetKey("  tvly-new  ")
	if v := s.View(); !v.CanSave() || !v.KeyFilled {
		t.Fatal("填了新密钥可保存")
	}
	s.SetKey("   ")
	if s.View().KeyFilled {
		t.Fatal("纯空白不算填了")
	}
	// 切到未配置的服务：没密钥不能保存也不能测试；清除不可用
	s.SetProvider(ProviderAnySearch)
	v = s.View()
	if v.CanSave() || v.CanTest() || v.CanClear() || !v.Switching() {
		t.Fatalf("未配置的服务: %+v", v)
	}
	// 切到另一个已配置的服务：可直接保存（切换）
	s.SetProvider(ProviderTavily)
	s.SetProvider(ProviderNative)
	v = s.View()
	if !v.Native() || v.CanClear() || v.NativeInfo() == "" {
		t.Fatalf("原生搜索没有密钥可清除: %+v", v)
	}
}

func TestSearchSwitchToConfiguredProviderCanSave(t *testing.T) {
	r, s := newSearchRig(t, services.ThemedSearchStatus{Provider: ProviderTavily, ConfiguredProviders: []string{ProviderTavily, ProviderAnySearch}, Configured: true})
	s.Load()
	s.SetProvider(ProviderAnySearch)
	if !s.View().CanSave() {
		t.Fatal("切换到已配置的服务可以直接保存")
	}
	if !s.Save() || s.View().Status.Provider != ProviderAnySearch {
		t.Fatalf("保存后切换: %+v", s.View().Status)
	}
	if cfg := r.svc.Calls().SearchConfigs; len(cfg) != 1 || !strings.Contains(cfg[0], "apiKey=unchanged") {
		t.Fatalf("没填密钥不能改密钥: %v", cfg)
	}
	if q := r.lastToast(); q.Kind != kit.ToastSuccess || q.Message != locales.SettingsReadingSearchSaved() {
		t.Fatalf("%+v", q)
	}
}

func TestSearchProviderChangeClearsKeyAndError(t *testing.T) {
	_, s := newSearchRig(t, services.ThemedSearchStatus{Provider: ProviderTavily})
	s.Load()
	s.SetKey("secret-key-123")
	s.SetProvider(ProviderAnySearch)
	v := s.View()
	if v.KeyFilled || v.Error != "" {
		t.Fatalf("换服务要清空密钥: %+v", v)
	}
	s.SetProvider(ProviderTavily)
	if s.View().CanSave() {
		t.Fatal("清空后的密钥不能再被保存")
	}
}

func TestSearchSaveTestClearWithKey(t *testing.T) {
	r, s := newSearchRig(t, services.ThemedSearchStatus{Provider: ProviderTavily, Persistent: true})
	s.Load()
	s.SetKey("tvly-secret-0001")
	if !s.Test() {
		t.Fatal("测试应成功")
	}
	v := s.View()
	if v.KeyFilled || !v.Configured() {
		t.Fatalf("成功后清空输入并显示已配置: %+v", v)
	}
	if q := r.lastToast(); q.Message != locales.SettingsReadingSearchTestPassed("Tavily") {
		t.Fatalf("%+v", q)
	}
	if !s.ClearKey() {
		t.Fatal("清除应成功")
	}
	if s.View().Configured() {
		t.Fatal("清除后未配置")
	}
	if q := r.lastToast(); q.Message != locales.SettingsReadingSearchKeyCleared("Tavily") {
		t.Fatalf("%+v", q)
	}
	if s.ClearKey() {
		t.Fatal("未配置时不能清除")
	}
	cfg := r.svc.Calls().SearchConfigs
	if len(cfg) != 2 || !strings.Contains(cfg[0], "apiKey=***") || !strings.Contains(cfg[1], "apiKey=cleared") {
		t.Fatalf("记录里密钥已隐去: %v", cfg)
	}
}

func TestSearchKeyNeverLeaksIntoViewLogsOrToasts(t *testing.T) {
	r, s := newSearchRig(t, services.ThemedSearchStatus{Provider: ProviderTavily})
	s.Load()
	const secret = "tvly-VERY-SECRET-key-98765"
	s.SetKey(secret)
	if strings.Contains(fmt.Sprintf("%+v %#v", s.View(), s.View()), secret) {
		t.Fatal("快照里不能有密钥明文")
	}
	r.svc.Fail(servicestest.ThemedOpConfigureSearch, errors.New("search rejected the credential"))
	if s.Save() {
		t.Fatal("失败")
	}
	for _, e := range r.log.entries() {
		if strings.Contains(e, secret) {
			t.Fatalf("日志里不能有密钥: %s", e)
		}
	}
	for _, q := range r.toasts() {
		if strings.Contains(q.Message+q.Detail, secret) {
			t.Fatal("提示里不能有密钥")
		}
	}
	// 失败保留输入，用户可以修正后重试
	if !s.View().KeyFilled {
		t.Fatal("失败后保留已填的密钥")
	}
	for _, c := range r.svc.Calls().SearchConfigs {
		if strings.Contains(c, secret) {
			t.Fatal("调用记录里不能有密钥")
		}
	}
}

func TestSearchFailureToastsReasonAndAllowsRetry(t *testing.T) {
	r, s := newSearchRig(t, services.ThemedSearchStatus{Provider: ProviderTavily})
	s.Load()
	s.SetKey("k1-k1-k1")
	r.svc.Fail(servicestest.ThemedOpConfigureSearch, errors.New("401 unauthorized"))
	if s.Test() {
		t.Fatal("失败不能报成功")
	}
	q := r.lastToast()
	if q.Kind != kit.ToastError || q.Message != locales.SettingsReadingSearchFailed() || q.Detail != "401 unauthorized" {
		t.Fatalf("%+v", q)
	}
	if v := s.View(); v.Error != "401 unauthorized" || v.Busy != "" || v.Configured() {
		t.Fatalf("失败不改状态，错误显示在表单里: %+v", v)
	}
	r.svc.Fail(servicestest.ThemedOpConfigureSearch, nil)
	if !s.Test() || s.View().Error != "" {
		t.Fatal("重试应成功并清掉错误")
	}
}

func TestSearchDefaultToggleKeepsTypedKey(t *testing.T) {
	r, s := newSearchRig(t, services.ThemedSearchStatus{Provider: ProviderTavily})
	s.Load()
	s.SetKey("typed-but-unsaved")
	if !s.SetDefault(true) {
		t.Fatal("应成功")
	}
	if v := s.View(); !v.Status.DefaultEnabled || !v.KeyFilled {
		t.Fatalf("改默认开关不应吃掉还没保存的密钥: %+v", v)
	}
	if q := r.lastToast(); q.Message != locales.SettingsReadingSearchDefaultSaved() {
		t.Fatalf("%+v", q)
	}
	r.svc.Fail(servicestest.ThemedOpConfigureSearch, errors.New("disk full"))
	if s.SetDefault(false) {
		t.Fatal("失败")
	}
	if q := r.lastToast(); q.Message != locales.SettingsReadingSearchDefaultFailed() {
		t.Fatalf("%+v", q)
	}
}

func TestSearchOneOperationAtATime(t *testing.T) {
	r, s := newSearchRig(t, services.ThemedSearchStatus{Provider: ProviderTavily, ConfiguredProviders: []string{ProviderTavily}, Configured: true})
	s.Load()
	release, arrived := r.svc.Hold(servicestest.ThemedOpConfigureSearch)
	done := make(chan bool, 1)
	go func() { done <- s.Test() }()
	<-arrived
	v := s.View()
	if v.Busy != locales.SettingsReadingSearchTesting() || v.CanSave() || v.CanTest() || v.CanClear() {
		t.Fatalf("进行中禁用全部控件并显示进度文字: %+v", v)
	}
	if s.Save() || s.SetDefault(true) || s.Load() {
		t.Fatal("进行中不能发起别的操作")
	}
	s.SetProvider(ProviderAnySearch) // 进行中不能换服务
	if s.View().Provider != ProviderTavily {
		t.Fatal("进行中不能换服务")
	}
	release()
	if !<-done {
		t.Fatal("应成功")
	}
}

func TestSearchStatusLineAndPlaceholders(t *testing.T) {
	_, s := newSearchRig(t, services.ThemedSearchStatus{Provider: ProviderTavily, ConfiguredProviders: []string{ProviderTavily}, Configured: true, Persistent: false})
	s.Load()
	line := s.View().StatusLine()
	for _, want := range []string{"Tavily", locales.SettingsReadingSearchConfigured(), locales.SettingsReadingSearchCurrent("Tavily"), locales.SettingsReadingSearchSessionOnly()} {
		if !strings.Contains(line, want) {
			t.Errorf("状态行缺 %q: %s", want, line)
		}
	}
	if s.View().KeyPlaceholder() != locales.SettingsReadingSearchKeyReplace() {
		t.Fatal("已配置时提示可替换")
	}
	s.SetProvider(ProviderAnySearch)
	v := s.View()
	if v.KeyPlaceholder() != locales.SettingsReadingSearchKeyPlaceholder("AnySearch") || !strings.Contains(v.StatusLine(), locales.SettingsReadingSearchCurrentPending("Tavily")) {
		t.Fatalf("未保存的切换要标明当前仍用旧服务: %s", v.StatusLine())
	}
	if (SearchView{}).StatusLine() != "" || (SearchView{}).Configured() {
		t.Fatal("没读到状态时没有状态行")
	}
	if (SearchView{Status: &services.ThemedSearchStatus{NativeUnavailable: "no tools"}}).NativeInfo() != "no tools" {
		t.Fatal("原生不可用时显示原因")
	}
}

func TestSearchConfiguredFallsBackWhenNoProviderList(t *testing.T) {
	v := SearchView{Provider: ProviderTavily, Status: &services.ThemedSearchStatus{Provider: ProviderTavily, Configured: true}}
	if !v.Configured() {
		t.Fatal("没有已配置清单时只认当前服务")
	}
	v.Provider = ProviderAnySearch
	if v.Configured() {
		t.Fatal("其他服务按未配置")
	}
	v.Status.ConfiguredProviders = []string{}
	v.Provider = ProviderTavily
	if v.Configured() {
		t.Fatal("有清单（哪怕为空）时以清单为准")
	}
}

func TestSetKeyCapsLength(t *testing.T) {
	_, s := newSearchRig(t, services.ThemedSearchStatus{})
	s.SetKey(strings.Repeat("k", 1500))
	s.mu.Lock()
	n := len(s.key)
	s.mu.Unlock()
	if n != 1000 {
		t.Fatalf("密钥最多 1000 个字符: %d", n)
	}
}
