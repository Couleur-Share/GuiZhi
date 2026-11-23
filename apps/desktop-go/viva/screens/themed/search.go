package themed

// 联网搜索设置（ReadingSearchSettings.tsx）：AI 阅读与「围绕本文提问」共用的搜索服务配置。

import (
	"context"
	"strings"
	"sync"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 搜索服务标识。
const (
	ProviderNative    = "native"
	ProviderTavily    = "tavily"
	ProviderAnySearch = "anysearch"
)

// SearchProviders 是下拉里的选项顺序。
var SearchProviders = []string{ProviderNative, ProviderTavily, ProviderAnySearch}

// ProviderName 是搜索服务的显示名。
func ProviderName(p string) string {
	switch p {
	case ProviderNative:
		return locales.SettingsReadingSearchNative()
	case ProviderTavily:
		return "Tavily"
	case ProviderAnySearch:
		return "AnySearch"
	}
	return p
}

// SearchView 是搜索设置的快照。密钥明文不在这里：界面通过 SetKey 写入，快照只告诉界面「有没有填」。
type SearchView struct {
	// Status 是保存着的配置；nil 表示还没读到（此时看 Error，并提供重试）。
	Status *services.ThemedSearchStatus
	// Provider 是下拉里选中的服务（可能还没保存）。
	Provider string
	// KeyFilled 为输入框里有非空白内容。
	KeyFilled bool
	// Busy 非空表示一个操作在进行（它就是状态提示文字），此时所有控件禁用。
	Busy string
	// Error 是最近一次读取 / 操作失败的原因；非空时在表单里显示，读取失败还带重试。
	Error string
}

// Native 报告选中的是模型服务原生搜索（不需要密钥）。
func (v SearchView) Native() bool { return v.Provider == ProviderNative }

// Configured 报告选中的服务是否已配置：优先看已配置服务清单，没有清单时只认当前服务。
func (v SearchView) Configured() bool {
	st := v.Status
	if st == nil {
		return false
	}
	if st.ConfiguredProviders != nil {
		for _, p := range st.ConfiguredProviders {
			if p == v.Provider {
				return true
			}
		}
		return false
	}
	return st.Provider == v.Provider && st.Configured
}

// Switching 报告选中的服务与保存着的不同（保存后才切换）。
func (v SearchView) Switching() bool { return v.Status != nil && v.Provider != v.Status.Provider }

// CanSave：有新密钥，或要切换到一个已配置的服务。
func (v SearchView) CanSave() bool {
	return v.Busy == "" && v.Status != nil && (v.KeyFilled || (v.Configured() && v.Switching()))
}

// CanTest：有新密钥，或选中的服务已配置（用已存的密钥测试）。
func (v SearchView) CanTest() bool {
	return v.Busy == "" && v.Status != nil && (v.KeyFilled || v.Configured())
}

// CanClear：只有需要密钥且已配置的服务能清除。
func (v SearchView) CanClear() bool {
	return v.Busy == "" && v.Status != nil && v.Configured() && !v.Native()
}

// NativeInfo 是原生搜索的说明：可用的主文本模型，或不可用的原因。
func (v SearchView) NativeInfo() string {
	st := v.Status
	switch {
	case st != nil && st.NativeModel != "":
		return locales.SettingsReadingSearchNativeModel(st.NativeModel)
	case st != nil && st.NativeUnavailable != "":
		return st.NativeUnavailable
	}
	return locales.SettingsReadingSearchNativeMissing()
}

// KeyPlaceholder 是密钥输入框的占位文字。
func (v SearchView) KeyPlaceholder() string {
	if v.Configured() {
		return locales.SettingsReadingSearchKeyReplace()
	}
	return locales.SettingsReadingSearchKeyPlaceholder(ProviderName(v.Provider))
}

// StatusLine 是表单下方的状态行：服务 · 是否已配置 · 当前使用哪个（含仅本次运行保存的提示）。
func (v SearchView) StatusLine() string {
	if v.Status == nil {
		return ""
	}
	parts := []string{ProviderName(v.Provider)}
	if v.Configured() {
		parts = append(parts, locales.SettingsReadingSearchConfigured())
	} else {
		parts = append(parts, locales.SettingsReadingSearchNotConfigured())
	}
	cur := ProviderName(v.Status.Provider)
	if v.Switching() {
		parts = append(parts, locales.SettingsReadingSearchCurrentPending(cur))
	} else {
		parts = append(parts, locales.SettingsReadingSearchCurrent(cur))
	}
	if !v.Status.Persistent {
		parts = append(parts, locales.SettingsReadingSearchSessionOnly())
	}
	return strings.Join(parts, " · ")
}

// SearchSettings 管理联网搜索设置。方法可从任意 goroutine 调用。
type SearchSettings struct {
	svc        services.ThemedReading
	guard      kit.Guard
	invalidate func()

	// Ctx 是发给服务的请求的根上下文；nil 取 context.Background()。
	Ctx context.Context

	mu   sync.Mutex
	view SearchView
	key  string // 输入框里的密钥明文：只存在内存，不进快照、不进日志
}

// NewSearchSettings 创建搜索设置（默认选中 tavily，读取后以保存的服务为准）。
func NewSearchSettings(svc services.ThemedReading, guard kit.Guard, invalidate func()) *SearchSettings {
	return &SearchSettings{svc: svc, guard: guard, invalidate: invalidate, view: SearchView{Provider: ProviderTavily}}
}

// View 返回当前快照。
func (s *SearchSettings) View() SearchView {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.view
}

func (s *SearchSettings) wake() {
	if s.invalidate != nil {
		s.invalidate()
	}
}

func (s *SearchSettings) ctx() context.Context {
	if s.Ctx != nil {
		return s.Ctx
	}
	return context.Background()
}

// begin 占用「忙」标记；已有操作在进行则失败。
func (s *SearchSettings) begin(msg string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.view.Busy != "" {
		return false
	}
	s.view.Busy, s.view.Error = msg, ""
	return true
}

func (s *SearchSettings) end() {
	s.mu.Lock()
	s.view.Busy = ""
	s.mu.Unlock()
	s.wake()
}

// Load 读取配置。失败只在表单里显示原因并留痕（不弹提示），界面提供重试（再次 Load）。
func (s *SearchSettings) Load() bool {
	if !s.begin(locales.SettingsReadingSearchLoading()) {
		return false
	}
	s.wake()
	defer s.end()
	st, err := s.svc.SearchStatus(s.ctx())
	s.mu.Lock()
	if err != nil {
		s.view.Error = err.Error()
		s.mu.Unlock()
		s.guard.Background("themedReading.searchStatus", err)
		return false
	}
	s.view.Status, s.view.Provider = &st, st.Provider
	s.mu.Unlock()
	return true
}

// SetProvider 切换选中的服务：清空已填的密钥与错误提示（不同服务的密钥不能混用）。
func (s *SearchSettings) SetProvider(p string) {
	s.mu.Lock()
	if s.view.Busy != "" {
		s.mu.Unlock()
		return
	}
	s.view.Provider, s.view.KeyFilled, s.view.Error = p, false, ""
	s.key = ""
	s.mu.Unlock()
	s.wake()
}

// SetKey 写入输入框里的密钥（最多 1000 个字符，与输入框上限一致）。
func (s *SearchSettings) SetKey(key string) {
	if r := []rune(key); len(r) > 1000 {
		key = string(r[:1000])
	}
	s.mu.Lock()
	s.key = key
	s.view.KeyFilled = strings.TrimSpace(key) != ""
	s.mu.Unlock()
	s.wake()
}

// apply 用一次写入操作的入参执行并处理结果；成功后清空输入框（keepKey 为 true 时保留，如只改默认开关）并弹成功提示，失败弹带原因的错误提示。
func (s *SearchSettings) apply(busy string, keepKey bool, build func(provider, key string) services.ThemedSearchConfigInput, success func(provider string) string, failTitle string) bool {
	if !s.begin(busy) {
		return false
	}
	s.wake()
	defer s.end()
	s.mu.Lock()
	provider, key := s.view.Provider, strings.TrimSpace(s.key)
	s.mu.Unlock()

	st, err := s.svc.ConfigureSearch(s.ctx(), build(provider, key))
	if err != nil {
		s.mu.Lock()
		s.view.Error = err.Error()
		s.mu.Unlock()
		s.guard.Fail(failTitle, "themedReading.searchConfig", err)
		return false
	}
	s.mu.Lock()
	s.view.Status = &st
	if !keepKey && provider == s.view.Provider { // 操作期间用户没改选服务才清空输入
		s.key, s.view.KeyFilled = "", false
	}
	s.mu.Unlock()
	s.guard.Succeed(success(provider))
	return true
}

func keyArg(native bool, key string) *string {
	if native || key == "" {
		return nil
	}
	return &key
}

// Save 保存当前服务与（新填的）密钥。
func (s *SearchSettings) Save() bool {
	return s.apply(locales.SettingsReadingSearchSaving(), false, func(p, k string) services.ThemedSearchConfigInput {
		return services.ThemedSearchConfigInput{Provider: p, APIKey: keyArg(p == ProviderNative, k)}
	}, func(string) string { return locales.SettingsReadingSearchSaved() }, locales.SettingsReadingSearchFailed())
}

// Test 先用候选配置真实搜索一次，通过才保存。
func (s *SearchSettings) Test() bool {
	return s.apply(locales.SettingsReadingSearchTesting(), false, func(p, k string) services.ThemedSearchConfigInput {
		return services.ThemedSearchConfigInput{Provider: p, APIKey: keyArg(p == ProviderNative, k), Test: true}
	}, func(p string) string { return locales.SettingsReadingSearchTestPassed(ProviderName(p)) }, locales.SettingsReadingSearchFailed())
}

// ClearKey 清除当前服务已存的密钥（原生搜索没有密钥，不可用）。
func (s *SearchSettings) ClearKey() bool {
	if !s.View().CanClear() {
		return false
	}
	empty := ""
	return s.apply(locales.SettingsReadingSearchClearing(), false, func(p, _ string) services.ThemedSearchConfigInput {
		return services.ThemedSearchConfigInput{Provider: p, APIKey: &empty}
	}, func(p string) string { return locales.SettingsReadingSearchKeyCleared(ProviderName(p)) }, locales.SettingsReadingSearchFailed())
}

// SetDefault 保存「生成阅读页时默认联网搜索」。
func (s *SearchSettings) SetDefault(on bool) bool {
	return s.apply(locales.SettingsReadingSearchSavingDefault(), true, func(string, string) services.ThemedSearchConfigInput {
		return services.ThemedSearchConfigInput{DefaultEnabled: &on}
	}, func(string) string { return locales.SettingsReadingSearchDefaultSaved() }, locales.SettingsReadingSearchDefaultFailed())
}
