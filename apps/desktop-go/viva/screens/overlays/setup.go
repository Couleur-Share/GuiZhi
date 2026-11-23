package overlays

// 首次使用设置清单（SetupChecklistDialog.tsx、setup-readiness.ts 与 App.tsx 里的自动弹出）的状态层。
//
// 清单只有一个必做项（配置文本模型），转写与语义检索是折叠起来的可选项。
// 「稍后再说」「开始采集」与关闭对话框都会永久记下「已处理」；「去配置」只关本会话，下次启动还会提醒。

import (
	"bytes"
	"errors"
	"strings"
	"sync"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// SetupItemID 是清单项的标识。
type SetupItemID string

const (
	SetupTextModel     SetupItemID = "textModel"
	SetupTranscription SetupItemID = "transcription"
	SetupEmbedding     SetupItemID = "embedding"
)

// 设置页的分区（SetupItemSection 的取值，与设置分区 ID 一致）。
const (
	SetupSectionAI      = "ai"
	SetupSectionCapture = "capture"
)

// SetupItem 是清单里的一项。
type SetupItem struct {
	ID    SetupItemID
	Ready bool
	// Required 为真表示没完成就「还不能完整用起来」。
	Required bool
}

// isLegacyTextModelReady 判断旧版单模型字段是否填全（供应商、密钥、地址、模型名都非空）。
func isLegacyTextModelReady(l services.AILegacy) bool {
	return strings.TrimSpace(l.Provider) != "" && strings.TrimSpace(l.APIKey) != "" &&
		strings.TrimSpace(l.APIURL) != "" && strings.TrimSpace(l.Model) != ""
}

func routeConfigured(cfg services.AIConfig, route string) bool {
	m, ok := cfg.ResolveRoute(route)
	return ok && services.IsConfiguredModel(m)
}

// IsCoreTextModelReady 判断核心文本模型是否就绪：主文本或快速文本任一可用（含旧版单模型字段）。
func IsCoreTextModelReady(cfg services.AIConfig) bool {
	return routeConfigured(cfg, services.AIRouteMainText) || routeConfigured(cfg, services.AIRouteFastText) ||
		isLegacyTextModelReady(cfg.AILegacy)
}

// BuildSetupChecklist 按当前 AI 配置与引擎探测结果给出清单：文本模型（必做）、转写、语义检索。
// 本地转写引擎已装，或转写路由已显式配置，才算转写就绪；yt-dlp 不进清单（只有采集 B 站 / YouTube 才需要，失败时任务行自带安装入口）。
func BuildSetupChecklist(cfg services.AIConfig, funasrInstalled bool) []SetupItem {
	return []SetupItem{
		{ID: SetupTextModel, Ready: IsCoreTextModelReady(cfg), Required: true},
		{ID: SetupTranscription, Ready: funasrInstalled || routeConfigured(cfg, services.AIRouteAudioText)},
		{ID: SetupEmbedding, Ready: routeConfigured(cfg, services.AIRouteEmbedding)},
	}
}

// SetupItemSection 是清单项「去配置」要打开的设置分区。
func SetupItemSection(id SetupItemID) string {
	if id == SetupTextModel || id == SetupEmbedding {
		return SetupSectionAI
	}
	return SetupSectionCapture
}

// SetupUsesQuickSetup 报告「去配置」是否同时请求打开 AI 快速配置（文本模型与语义检索走它）。
func SetupUsesQuickSetup(id SetupItemID) bool { return id == SetupTextModel || id == SetupEmbedding }

// SetupView 是设置清单对话框的快照。
type SetupView struct {
	Open bool
	// Items 是全部清单项；TextModelReady 与 Optional 是界面直接要的两块：文本模型主卡与折叠区里的可选项。
	Items          []SetupItem
	TextModelReady bool
	Optional       []SetupItem
	// OptionalOpen 为真表示「更多可选设置」展开着。
	OptionalOpen bool
	// Probing 为真表示正在探测本地转写引擎：转写项显示转圈而不是「去配置」。
	Probing bool
	// LoadError 非空表示 AI 配置读不出来：清单按「都没配」显示，界面应同时说明原因，不能把它当成用户真的没配。
	LoadError string
}

// SetupHooks 是设置清单触发的外部动作；为 nil 的钩子什么也不做。
type SetupHooks struct {
	// StartCapture 打开快速采集（「开始采集」）。
	StartCapture func()
	// OpenSettingsSection 打开设置页的某一节。
	OpenSettingsSection func(section string)
	// RequestAIQuickSetup 请求 AI 设置页打开快速配置。
	RequestAIQuickSetup func()
}

// Setup 管理首次使用设置清单。方法可从任意 goroutine 调用。
type Setup struct {
	Env
	AI           services.AISettings
	Prefs        services.Prefs
	CaptureTools services.CaptureTools
	Guard        kit.Guard
	Hooks        SetupHooks
	// Blocked 报告此刻是否有更要紧的对话框占着（旧版数据迁移）；为真时自动弹出推迟，直到 Unblocked 被调用。nil 视为不占。
	Blocked func() bool

	invalidate func()

	mu            sync.Mutex
	open          bool
	shouldOffer   bool // 启动评估后确认需要提醒
	sessionClosed bool // 本会话里用户已经关过：不再自动弹出
	cfg           services.AIConfig
	loadErr       string
	funasr        bool
	probing       bool
	optionalOpen  bool
	gen           uint64 // 每次打开加一：晚到的刷新结果据此判断自己是不是过期
}

// NewSetup 创建设置清单状态层。
func NewSetup(guard kit.Guard, invalidate func()) *Setup {
	return &Setup{Guard: guard, invalidate: invalidate}
}

func (s *Setup) wake() { wake(s.invalidate) }

// View 返回当前快照。
func (s *Setup) View() SetupView {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := SetupView{Open: s.open, OptionalOpen: s.optionalOpen, Probing: s.probing, LoadError: s.loadErr}
	v.Items = BuildSetupChecklist(s.cfg, s.funasr)
	for _, it := range v.Items {
		switch it.ID {
		case SetupTextModel:
			v.TextModelReady = it.Ready
		case SetupTranscription, SetupEmbedding:
			v.Optional = append(v.Optional, it)
		}
	}
	return v
}

// Evaluate 在启动时评估是否要自动弹出清单：已处理过就跳过；核心文本模型已经配好（老用户）就静默记为已处理，不打扰；
// 否则标记为待提醒，在没有更要紧的对话框占着时弹出。
func (s *Setup) Evaluate() {
	s.spawn(func() {
		if prefTrue(s.Prefs, settings.PrefSetupDismissed) {
			return
		}
		cfg, err := s.loadConfig()
		if err != nil {
			// 读不出配置时无法判断用户是不是老用户：不弹、不记已处理，留给下次启动，只留日志
			s.Guard.Background("setup.evaluate", err)
			return
		}
		if IsCoreTextModelReady(cfg) {
			if err := setPrefTrue(s.Prefs, settings.PrefSetupDismissed); err != nil {
				s.Guard.Background("setup.autoDismiss", err)
			}
			return
		}
		s.mu.Lock()
		s.shouldOffer = true
		s.cfg = cfg
		s.mu.Unlock()
		s.maybeOffer()
	})
}

// Unblocked 在占着的对话框（旧版数据迁移）关闭后调用：补上被推迟的自动弹出。
func (s *Setup) Unblocked() { s.maybeOffer() }

func (s *Setup) maybeOffer() {
	s.mu.Lock()
	if !s.shouldOffer || s.sessionClosed || s.open || (s.Blocked != nil && s.Blocked()) {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	s.openAndRefresh()
}

// RequestOpen 是用户主动要看清单（设置页的入口）：本会话关过也重新打开。
func (s *Setup) RequestOpen() {
	s.mu.Lock()
	s.sessionClosed = false
	s.mu.Unlock()
	s.openAndRefresh()
}

func (s *Setup) loadConfig() (services.AIConfig, error) {
	if s.AI == nil {
		return services.AIConfig{}, errors.New("AI settings service is not available")
	}
	return s.AI.Load(s.context())
}

// openAndRefresh 打开对话框，同时在后台重新读 AI 配置并探测本地转写引擎（每次打开都重来：用户可能刚在设置里配好）。
func (s *Setup) openAndRefresh() {
	s.mu.Lock()
	s.open = true
	s.optionalOpen = false
	s.probing = s.CaptureTools != nil
	s.gen++
	gen := s.gen
	s.mu.Unlock()
	s.wake()

	s.spawn(func() {
		cfg, err := s.loadConfig()
		installed := false
		var probeErr error
		if s.CaptureTools != nil {
			var st services.FunasrStatus
			st, probeErr = s.CaptureTools.FunasrStatus(s.context(), false)
			installed = probeErr == nil && st.Installed
		}
		s.mu.Lock()
		if gen != s.gen || !s.open {
			s.mu.Unlock()
			return
		}
		if err == nil {
			s.cfg, s.loadErr = cfg, ""
		} else {
			s.loadErr = err.Error()
		}
		s.funasr, s.probing = installed, false
		s.mu.Unlock()
		if err != nil {
			s.Guard.Background("setup.loadConfig", err)
		}
		if probeErr != nil {
			s.Guard.Background("setup.probeFunasr", probeErr) // 探测失败按「未安装」显示（渲染层同样），只留日志
		}
		s.wake()
	})
}

// ToggleOptional 展开或收起「更多可选设置」。
func (s *Setup) ToggleOptional() {
	s.mu.Lock()
	s.optionalOpen = !s.optionalOpen
	s.mu.Unlock()
	s.wake()
}

// closeSession 只关本会话的对话框；不写已处理。
func (s *Setup) closeSession() {
	s.mu.Lock()
	s.gen++
	s.open, s.sessionClosed, s.probing = false, true, false
	s.mu.Unlock()
	s.wake()
}

// GoConfigure 是某一项的「去配置」：请求 AI 快速配置（文本模型与语义检索）、打开对应设置分区，并只关本会话——
// 配没配好未知，所以不写已处理。
func (s *Setup) GoConfigure(id SetupItemID) {
	if SetupUsesQuickSetup(id) {
		call(s.Hooks.RequestAIQuickSetup)
	}
	call1(s.Hooks.OpenSettingsSection, SetupItemSection(id))
	s.closeSession()
}

// DismissLater 是「稍后再说」，也是关闭对话框（Esc、点遮罩）：永久记下已处理并关闭。
// 记录写不进去时仍然关闭（不让用户被一个关不掉的对话框困住），但如实提示，下次启动会再次提醒。
func (s *Setup) DismissLater() {
	s.mu.Lock()
	s.shouldOffer = false
	s.mu.Unlock()
	s.closeSession()
	s.spawn(func() {
		if err := setPrefTrue(s.Prefs, settings.PrefSetupDismissed); err != nil {
			s.Guard.Report(locales.SetupLater(), err)
		}
	})
}

// StartCapture 是「开始采集」（文本模型就绪后才有此按钮）：先同「稍后再说」，再打开快速采集。
func (s *Setup) StartCapture() {
	s.DismissLater()
	call(s.Hooks.StartCapture)
}

// prefTrue 读一个布尔偏好；没有或读不出都按 false。
func prefTrue(p services.Prefs, key string) bool {
	if p == nil {
		return false
	}
	raw, ok := p.Get(key)
	return ok && bytes.Equal(bytes.TrimSpace(raw), []byte("true"))
}

// setPrefTrue 把一个布尔偏好写成 true；偏好存储没接（nil）时无事可做。
func setPrefTrue(p services.Prefs, key string) error {
	if p == nil {
		return nil
	}
	return p.Set(key, true)
}
