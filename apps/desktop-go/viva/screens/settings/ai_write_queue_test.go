package settings_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	sscreen "guizhi-mygo-shell/viva/screens/settings"
)

// seqAI 在假的 AI 配置服务外面包一层写盘观测：第 1 次 Save 可以挂住、可以指定第几次 Save 失败，
// 记下每次 Save 收到的配置（含失败的）与同一时刻在途的最大个数。其余方法原样交给内层的假实现。
type seqAI struct {
	*servicestest.AISettingsFake

	mu          sync.Mutex
	hold        chan struct{} // 非 nil 时第 1 次 Save 先等它关闭
	failCall    int           // 第几次 Save 返回失败；0 表示不失败
	calls       int
	inflight    int
	maxInflight int
	attempted   []services.AIConfig
}

var _ services.AISettings = (*seqAI)(nil)

func (s *seqAI) Save(ctx context.Context, cfg services.AIConfig) error {
	s.mu.Lock()
	s.calls++
	n := s.calls
	s.attempted = append(s.attempted, cfg.Clone())
	s.inflight++
	if s.inflight > s.maxInflight {
		s.maxInflight = s.inflight
	}
	hold, failCall := s.hold, s.failCall
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.inflight--
		s.mu.Unlock()
	}()
	if n == 1 && hold != nil {
		<-hold
	}
	if n == failCall {
		return errors.New("write ai-models.json: disk full")
	}
	return s.AISettingsFake.Save(ctx, cfg)
}

func (s *seqAI) callCount() (n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *seqAI) peakInflight() (n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maxInflight
}

func (s *seqAI) attempts() []services.AIConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]services.AIConfig(nil), s.attempted...)
}

// queueEnv 摆好「路由可选」的配置（多一个纯对话模型 m3），并返回挂在 seqAI 上的页面。
func queueEnv(t *testing.T, svc func(inner *servicestest.AISettingsFake) *seqAI) (*aiEnv, *seqAI, *rig) {
	t.Helper()
	e := newAIEnv()
	cfg := seedAIConfig()
	cfg.Models = append(cfg.Models, services.AIModel{ID: "m3", ProviderID: "p1", Provider: "openai", APIProtocol: services.AIProtocolOpenAI, APIKey: aiSecret,
		APIURL: "https://api.openai.com", Model: "gpt-4.1-mini", Enabled: true, Capabilities: services.AICapabilities{Chat: true}})
	e.ai.PutConfig(cfg)
	s := svc(e.ai)
	r := openPage(t, func(look *kit.Look) func(c ui.C) {
		a := &sscreen.AI{Look: look, Guard: e.guard(), Service: s, Wake: func() {}}
		t.Cleanup(a.Close)
		return a.Page
	})
	waitButton(r, "GPT Main api.openai.com 3")
	return e, s, r
}

const queueModel = "gpt-4.1-mini (Main)"

// waitRouteValue 等某条路由的下拉显示成 want（回滚是后台结果回到界面线程后才生效的，要出几帧）。
func waitRouteValue(r *rig, label, want string) {
	r.t.Helper()
	waitUntil(r, "「"+label+"」显示为「"+want+"」", func() bool {
		n, ok := r.node(gfx.RoleCombobox, label)
		return ok && n.Value == want
	})
}

// 写盘一次只在途一个，并且按提交顺序：后一次修改是在前一次之上做的，并发或乱序都会把新值写回旧值。
func TestAIWriteQueueIsSingleFlightAndOrdered(t *testing.T) {
	e, svc, r := queueEnv(t, func(in *servicestest.AISettingsFake) *seqAI {
		return &seqAI{AISettingsFake: in, hold: make(chan struct{})}
	})
	fast, main := locales.SettingsAiWorkbenchRouteFastText(), locales.SettingsAiWorkbenchRouteMainText()

	r.pick(fast, queueModel) // 第 1 次写盘挂在 hold 上
	waitUntil(r, "第 1 次写盘已发出", func() bool { return svc.callCount() == 1 })
	r.pick(main, queueModel) // 第 2 次修改只能排队
	// 第 1 次还没返回：第 2 次不能开始。守卫失效的话它会立刻冒出来，这里留足时间让它冒出来。
	r.frames(5)
	time.Sleep(150 * time.Millisecond)
	r.frames(2)
	if n := svc.callCount(); n != 1 {
		t.Fatalf("第 1 次写盘还没返回时不应开始第 2 次，实际已发出 %d 次", n)
	}

	close(svc.hold)
	waitUntil(r, "两次写盘都完成", func() bool { return len(e.ai.Saves()) == 2 })
	saves := e.ai.Saves()
	if saves[0].Routes[services.AIRouteFastText] != "m3" || saves[0].Routes[services.AIRouteMainText] != "" {
		t.Errorf("第 1 次写入应只含第一处修改：%v", saves[0].Routes)
	}
	if saves[1].Routes[services.AIRouteFastText] != "m3" || saves[1].Routes[services.AIRouteMainText] != "m3" {
		t.Errorf("第 2 次写入应含两处修改：%v", saves[1].Routes)
	}
	if got := svc.peakInflight(); got != 1 {
		t.Errorf("同一时刻在途的写盘最多 1 个，实际 %d", got)
	}
}

// 某次写盘失败：排在它后面的修改都是在失败的状态上做的，一并丢弃；之后用户再改，写出的是「上一次成功的配置 + 这一处」。
func TestAIWriteFailureDropsQueuedChanges(t *testing.T) {
	e, svc, r := queueEnv(t, func(in *servicestest.AISettingsFake) *seqAI {
		return &seqAI{AISettingsFake: in, hold: make(chan struct{}), failCall: 1}
	})
	fast, main := locales.SettingsAiWorkbenchRouteFastText(), locales.SettingsAiWorkbenchRouteMainText()

	r.pick(fast, queueModel) // 第 1 次写盘挂住，随后失败
	waitUntil(r, "第 1 次写盘已发出", func() bool { return svc.callCount() == 1 })
	r.pick(main, queueModel) // 排在失败那次后面的修改
	close(svc.hold)
	waitUntil(r, "失败提示", func() bool { return len(e.toasts(kit.ToastError)) == 1 })
	waitRouteValue(r, fast, locales.SettingsAiWorkbenchFollowGlobalDefault()) // 界面已回滚

	// 失败之后再改一处：只应写出一次，且不含被丢弃的「主文本」那处。
	r.pick(fast, queueModel)
	waitUntil(r, "之后的修改写盘", func() bool { return len(e.ai.Saves()) >= 1 })
	r.frames(5)
	time.Sleep(50 * time.Millisecond)
	r.frames(2)
	for i, cfg := range svc.attempts() {
		if i > 0 && cfg.Routes[services.AIRouteMainText] != "" {
			t.Errorf("第 %d 次写盘带着被丢弃的修改：%v", i+1, cfg.Routes)
		}
	}
	if got := e.ai.Current().Routes; got[services.AIRouteMainText] != "" || got[services.AIRouteFastText] != "m3" {
		t.Errorf("磁盘上的路由应只含失败之后的那一处修改：%v", got)
	}
}

// 回滚的落点是「最近一次确认写盘成功的配置」，不是界面此刻的配置：
// 第 1 次成功、第 2 次失败时，回到第 1 次写入的样子，而不是带着失败的第 2 处修改。
func TestAIWriteFailureRollsBackToLastConfirmedSave(t *testing.T) {
	e, svc, r := queueEnv(t, func(in *servicestest.AISettingsFake) *seqAI {
		return &seqAI{AISettingsFake: in, hold: make(chan struct{}), failCall: 2}
	})
	fast, main := locales.SettingsAiWorkbenchRouteFastText(), locales.SettingsAiWorkbenchRouteMainText()

	r.pick(fast, queueModel) // 第 1 次：挂住，随后成功
	waitUntil(r, "第 1 次写盘已发出", func() bool { return svc.callCount() == 1 })
	r.pick(main, queueModel) // 第 2 次：排队，轮到时失败
	close(svc.hold)
	waitUntil(r, "第 2 次写盘失败提示", func() bool { return len(e.toasts(kit.ToastError)) == 1 })
	waitRouteValue(r, main, locales.SettingsAiWorkbenchFollowGlobalDefault()) // 界面回到第 1 次写入的样子，不带失败的第 2 处修改
	waitRouteValue(r, fast, queueModel)
	if got := e.ai.Current().Routes; got[services.AIRouteFastText] != "m3" || got[services.AIRouteMainText] != "" {
		t.Fatalf("失败之后磁盘上应只有第 1 次的修改：%v", got)
	}

	// 回滚后的界面状态是下一次修改的基准：再把「快速文本」改回跟随全局，写出的配置里不能还带着失败的「主文本」。
	r.pick(fast, locales.SettingsAiWorkbenchFollowGlobalDefault())
	waitUntil(r, "之后的修改写盘", func() bool { return len(e.ai.Saves()) == 2 })
	if got := e.ai.Current().Routes; got[services.AIRouteMainText] != "" || got[services.AIRouteFastText] != "" {
		t.Errorf("回滚的基准应是第 1 次成功写入的配置：%v", got)
	}
}
