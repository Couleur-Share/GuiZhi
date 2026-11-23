package overlays

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

func chatModel(id string) services.AIModel {
	return services.AIModel{
		ID: id, Provider: "openai", APIKey: "key", APIURL: "https://api.example.test", Model: "gpt-test", Enabled: true,
		Capabilities: services.AICapabilities{Chat: true}, IsDefault: true,
	}
}

func embeddingModel(id string) services.AIModel {
	m := chatModel(id)
	m.IsDefault, m.Capabilities = false, services.AICapabilities{Embedding: true}
	return m
}

func readyOf(items []SetupItem, id SetupItemID) bool {
	for _, it := range items {
		if it.ID == id {
			return it.Ready
		}
	}
	panic("missing item " + id)
}

func TestBuildSetupChecklist(t *testing.T) {
	empty := BuildSetupChecklist(services.AIConfig{}, false)
	if len(empty) != 3 || readyOf(empty, SetupTextModel) || readyOf(empty, SetupTranscription) || readyOf(empty, SetupEmbedding) {
		t.Fatalf("什么都没配时都不应就绪：%+v", empty)
	}
	if !empty[0].Required || empty[1].Required || empty[2].Required {
		t.Errorf("只有文本模型是必做项：%+v", empty)
	}

	chat := services.AIConfig{Models: []services.AIModel{chatModel("m1")}}
	items := BuildSetupChecklist(chat, false)
	if !readyOf(items, SetupTextModel) {
		t.Error("有可用对话模型应算文本模型就绪")
	}
	if readyOf(items, SetupTranscription) {
		t.Error("转写路由不回退：没显式配置时不应因为有对话模型就算转写就绪")
	}
	chat.Routes = map[string]string{services.AIRouteAudioText: "m1"}
	if !readyOf(BuildSetupChecklist(chat, false), SetupTranscription) {
		t.Error("显式配置了转写路由应算就绪")
	}
	if !readyOf(BuildSetupChecklist(services.AIConfig{}, true), SetupTranscription) {
		t.Error("本地转写引擎已装应算转写就绪")
	}

	emb := services.AIConfig{Models: []services.AIModel{embeddingModel("e1")}}
	if got := BuildSetupChecklist(emb, false); !readyOf(got, SetupEmbedding) || readyOf(got, SetupTextModel) {
		t.Errorf("向量模型只让语义检索就绪：%+v", got)
	}
}

func TestIsCoreTextModelReadyVariants(t *testing.T) {
	disabled := chatModel("m1")
	disabled.Enabled = false
	incomplete := chatModel("m2")
	incomplete.APIKey = "  "
	legacy := services.AIConfig{AILegacy: services.AILegacy{Provider: "p", APIKey: "k", APIURL: "u", Model: "m"}}
	halfLegacy := services.AIConfig{AILegacy: services.AILegacy{Provider: "p", APIKey: "k"}}
	fast := chatModel("f1")
	fast.IsDefault = false

	cases := []struct {
		name string
		cfg  services.AIConfig
		want bool
	}{
		{"空配置", services.AIConfig{}, false},
		{"停用的模型", services.AIConfig{Models: []services.AIModel{disabled}}, false},
		{"缺密钥的模型", services.AIConfig{Models: []services.AIModel{incomplete}}, false},
		{"旧版字段填全", legacy, true},
		{"旧版字段不全", halfLegacy, false},
		{"快速文本路由可用", services.AIConfig{Models: []services.AIModel{fast}, Routes: map[string]string{services.AIRouteFastText: "f1"}}, true},
	}
	for _, c := range cases {
		if got := IsCoreTextModelReady(c.cfg); got != c.want {
			t.Errorf("%s：得到 %v，期望 %v", c.name, got, c.want)
		}
	}
}

func TestSetupItemSectionAndQuickSetup(t *testing.T) {
	if SetupItemSection(SetupTextModel) != SetupSectionAI || SetupItemSection(SetupEmbedding) != SetupSectionAI || SetupItemSection(SetupTranscription) != SetupSectionCapture {
		t.Error("分区映射不对")
	}
	if !SetupUsesQuickSetup(SetupTextModel) || !SetupUsesQuickSetup(SetupEmbedding) || SetupUsesQuickSetup(SetupTranscription) {
		t.Error("只有文本模型与语义检索走快速配置")
	}
}

type setupRig struct {
	*rig
	s       *Setup
	ai      *servicestest.AISettingsFake
	prefs   *servicestest.Prefs
	capture *servicestest.Capture
	blocked bool
	calls   []string
}

func newSetupRig(t *testing.T) *setupRig {
	t.Helper()
	sr := &setupRig{rig: newRig(t), ai: &servicestest.AISettingsFake{}, prefs: &servicestest.Prefs{}, capture: &servicestest.Capture{}}
	s := NewSetup(sr.guard, sr.invalidate)
	s.Env = sr.env()
	s.AI, s.Prefs, s.CaptureTools = sr.ai, sr.prefs, sr.capture
	s.Blocked = func() bool { return sr.blocked }
	s.Hooks = SetupHooks{
		StartCapture:        func() { sr.calls = append(sr.calls, "capture") },
		OpenSettingsSection: func(section string) { sr.calls = append(sr.calls, "section:"+section) },
		RequestAIQuickSetup: func() { sr.calls = append(sr.calls, "quick") },
	}
	sr.s = s
	return sr
}

func (sr *setupRig) dismissed() bool { return prefTrue(sr.prefs, settings.PrefSetupDismissed) }

func TestSetupEvaluateSkipsWhenAlreadyDismissed(t *testing.T) {
	sr := newSetupRig(t)
	sr.prefs.Put(settings.PrefSetupDismissed, "true")
	sr.s.Evaluate()
	if sr.s.View().Open || sr.ai.Loads() != 0 {
		t.Errorf("已处理过不应弹出也不必读配置：open=%v loads=%d", sr.s.View().Open, sr.ai.Loads())
	}
}

func TestSetupEvaluateSilentlyDismissesForConfiguredUsers(t *testing.T) {
	sr := newSetupRig(t)
	sr.ai.PutConfig(services.AIConfig{Models: []services.AIModel{chatModel("m1")}})
	sr.s.Evaluate()
	if sr.s.View().Open {
		t.Error("核心模型已配好的老用户不应被打扰")
	}
	if !sr.dismissed() {
		t.Error("应静默记为已处理")
	}
	if len(sr.toasts()) != 0 {
		t.Error("静默记录不应弹提示")
	}
}

func TestSetupEvaluateSilentDismissFailureIsOnlyLogged(t *testing.T) {
	sr := newSetupRig(t)
	sr.ai.PutConfig(services.AIConfig{Models: []services.AIModel{chatModel("m1")}})
	sr.prefs.FailSetWith(errors.New("prefs file is read-only"))
	sr.s.Evaluate()
	if len(sr.toasts()) != 0 {
		t.Error("用户没有操作，静默记录失败不应弹提示")
	}
	if logs := sr.log.entries(); len(logs) != 1 || !strings.HasPrefix(logs[0], "setup.autoDismiss|") {
		t.Errorf("应留日志：%v", logs)
	}
}

func TestSetupEvaluateOffersWhenNotReady(t *testing.T) {
	sr := newSetupRig(t)
	sr.capture.SetFunasrStatus(services.FunasrStatus{Installed: true})
	sr.s.Evaluate()
	v := sr.s.View()
	if !v.Open || v.TextModelReady || v.LoadError != "" || v.Probing {
		t.Fatalf("应弹出清单：%+v", v)
	}
	if !readyOf(v.Items, SetupTranscription) {
		t.Error("打开时应探测本地转写引擎")
	}
	if len(v.Optional) != 2 || v.Optional[0].ID != SetupTranscription || v.Optional[1].ID != SetupEmbedding {
		t.Errorf("可选项应是转写与语义检索：%+v", v.Optional)
	}
	if sr.dismissed() {
		t.Error("弹出本身不应记为已处理")
	}
}

func TestSetupEvaluateLoadFailureDoesNotOfferOrDismiss(t *testing.T) {
	sr := newSetupRig(t)
	sr.ai.FailLoad(errors.New("config file unreadable"))
	sr.s.Evaluate()
	if sr.s.View().Open || sr.dismissed() {
		t.Error("读不出配置时既不弹出也不记为已处理，留给下次启动")
	}
	if logs := sr.log.entries(); len(logs) != 1 || !strings.Contains(logs[0], "config file unreadable") {
		t.Errorf("应留日志：%v", logs)
	}
}

func TestSetupOfferWaitsForBlockingDialog(t *testing.T) {
	sr := newSetupRig(t)
	sr.blocked = true
	sr.s.Evaluate()
	if sr.s.View().Open {
		t.Fatal("迁移对话框占着时不应弹出")
	}
	sr.blocked = false
	sr.s.Unblocked()
	if !sr.s.View().Open {
		t.Error("迁移对话框关闭后应补上弹出")
	}
	sr.s.Unblocked() // 已打开：不重复
	if !sr.s.View().Open {
		t.Error("重复通知不应关闭")
	}
}

func TestSetupUnblockedWithoutPendingOfferDoesNothing(t *testing.T) {
	sr := newSetupRig(t)
	sr.s.Unblocked()
	if sr.s.View().Open {
		t.Error("没有待提醒时不应弹出")
	}
}

func TestSetupDismissLaterPersistsAndStopsOffering(t *testing.T) {
	sr := newSetupRig(t)
	sr.s.Evaluate()
	sr.s.DismissLater()
	if sr.s.View().Open || !sr.dismissed() {
		t.Fatalf("应关闭并记下已处理：open=%v dismissed=%v", sr.s.View().Open, sr.dismissed())
	}
	sr.s.Unblocked()
	if sr.s.View().Open {
		t.Error("已处理后不应再自动弹出")
	}
	sr.s.RequestOpen() // 用户主动要看时仍可打开
	if !sr.s.View().Open {
		t.Error("主动请求应能再次打开")
	}
}

func TestSetupDismissFailureStillClosesAndReports(t *testing.T) {
	sr := newSetupRig(t)
	sr.s.Evaluate()
	sr.prefs.FailSetWith(errors.New("prefs file is read-only"))
	sr.s.DismissLater()
	if sr.s.View().Open {
		t.Error("记录写不进去也要关闭，不能困住用户")
	}
	if tt := sr.lastToast(); tt.Kind != kit.ToastError || !strings.Contains(tt.Message, locales.SetupLater()) || !strings.Contains(tt.Detail, "read-only") {
		t.Errorf("应如实提示：%+v", tt)
	}
}

func TestSetupGoConfigureClosesSessionOnly(t *testing.T) {
	sr := newSetupRig(t)
	sr.s.Evaluate()
	sr.s.GoConfigure(SetupTextModel)
	if !slices.Equal(sr.calls, []string{"quick", "section:ai"}) {
		t.Errorf("文本模型：先请求快速配置再打开 AI 分区：%v", sr.calls)
	}
	if sr.s.View().Open || sr.dismissed() {
		t.Error("去配置只关本会话，不写已处理")
	}
	sr.s.Unblocked()
	if sr.s.View().Open {
		t.Error("本会话关过后不再自动弹出")
	}

	sr.calls = nil
	sr.s.RequestOpen()
	sr.s.GoConfigure(SetupTranscription)
	if !slices.Equal(sr.calls, []string{"section:capture"}) {
		t.Errorf("转写不走快速配置：%v", sr.calls)
	}
}

func TestSetupStartCapturePersistsThenOpensCapture(t *testing.T) {
	sr := newSetupRig(t)
	sr.ai.PutConfig(services.AIConfig{Models: []services.AIModel{chatModel("m1")}})
	sr.s.RequestOpen()
	if !sr.s.View().TextModelReady {
		t.Fatal("测试前提：文本模型已就绪")
	}
	sr.s.StartCapture()
	if sr.s.View().Open || !sr.dismissed() || !slices.Equal(sr.calls, []string{"capture"}) {
		t.Errorf("开始采集应记下已处理、关闭并打开采集：open=%v dismissed=%v calls=%v", sr.s.View().Open, sr.dismissed(), sr.calls)
	}
}

func TestSetupOpenSurfacesLoadErrorAndProbeFailure(t *testing.T) {
	sr := newSetupRig(t)
	sr.ai.FailLoad(errors.New("config file unreadable"))
	sr.capture.FailFunasrStatus(errors.New("probe crashed"))
	sr.s.RequestOpen()
	v := sr.s.View()
	if !v.Open || v.LoadError != "config file unreadable" {
		t.Errorf("读不出配置时应带原因（不能当成用户真的没配）：%+v", v)
	}
	if readyOf(v.Items, SetupTranscription) {
		t.Error("探测失败按未安装显示")
	}
	if n := len(sr.log.entries()); n != 2 {
		t.Errorf("两个失败都应留日志：%v", sr.log.entries())
	}
	if len(sr.toasts()) != 0 {
		t.Error("读取失败不应弹提示：对话框里已有说明")
	}
}

func TestSetupRefreshesEveryTimeItOpens(t *testing.T) {
	sr := newSetupRig(t)
	sr.s.RequestOpen()
	if sr.s.View().TextModelReady {
		t.Fatal("测试前提：还没配")
	}
	sr.s.GoConfigure(SetupTextModel)
	sr.ai.PutConfig(services.AIConfig{Models: []services.AIModel{chatModel("m1")}}) // 用户在设置里配好了
	sr.s.RequestOpen()
	if !sr.s.View().TextModelReady {
		t.Error("重新打开应读到最新配置")
	}
}

// gatedCapture 让引擎探测卡住，直到 gate 关闭。
type gatedCapture struct {
	services.CaptureTools
	gate chan struct{}
}

func (g gatedCapture) FunasrStatus(ctx context.Context, force bool) (services.FunasrStatus, error) {
	<-g.gate
	return g.CaptureTools.FunasrStatus(ctx, force)
}

func TestSetupStaleRefreshIsDiscarded(t *testing.T) {
	sr := newSetupRig(t)
	sr.s.Env = sr.asyncEnv()
	gate := make(chan struct{})
	sr.capture.SetFunasrStatus(services.FunasrStatus{Installed: true})
	sr.s.CaptureTools = gatedCapture{CaptureTools: sr.capture, gate: gate}
	sr.s.RequestOpen()
	if !sr.s.View().Probing {
		t.Fatal("探测在途时应标记探测中")
	}
	sr.s.GoConfigure(SetupTextModel) // 关闭
	close(gate)
	sr.wait()
	v := sr.s.View()
	if v.Open || v.Probing || readyOf(v.Items, SetupTranscription) {
		t.Errorf("晚到的刷新不应落入已关闭的会话：%+v", v)
	}
}

func TestSetupToggleOptionalAndResetOnOpen(t *testing.T) {
	sr := newSetupRig(t)
	sr.s.RequestOpen()
	sr.s.ToggleOptional()
	if !sr.s.View().OptionalOpen {
		t.Fatal("应展开")
	}
	sr.s.ToggleOptional()
	if sr.s.View().OptionalOpen {
		t.Fatal("应收起")
	}
	sr.s.ToggleOptional()
	sr.s.GoConfigure(SetupEmbedding)
	sr.s.RequestOpen()
	if sr.s.View().OptionalOpen {
		t.Error("重新打开时可选区应默认折叠")
	}
}

func TestSetupWithoutServicesIsTolerated(t *testing.T) {
	r := newRig(t)
	s := NewSetup(r.guard, r.invalidate)
	s.Env = r.env()
	s.Evaluate() // 没有 AI 服务：读不出配置，不弹出
	if s.View().Open {
		t.Error("没接服务不应弹出")
	}
	s.RequestOpen()
	if v := s.View(); !v.Open || v.LoadError == "" {
		t.Errorf("主动打开应说明读不出配置：%+v", v)
	}
	s.DismissLater() // Prefs 为 nil：无事可做，不崩溃
	if len(r.toasts()) != 0 {
		t.Error("没接偏好存储不应弹提示")
	}
}
