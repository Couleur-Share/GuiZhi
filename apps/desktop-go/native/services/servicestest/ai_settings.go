package servicestest

import (
	"context"
	"sync"

	"guizhi-mygo-shell/native/services"
)

// AIFetchCall 是一次拉取模型列表的调用记录。APIKey 保存原值供测试断言「传对了密钥」，但 String / GoString 脱敏，打印不会泄露。
type AIFetchCall struct {
	APIURL, APIKey, Protocol string
}

// String 给日志与 %v 用：密钥脱敏。
func (c AIFetchCall) String() string {
	return "{url=" + c.APIURL + " protocol=" + c.Protocol + " key=***}"
}

// GoString 让 %#v 同样脱敏。
func (c AIFetchCall) GoString() string { return "servicestest.AIFetchCall" + c.String() }

// AISettingsFake 是 services.AISettings 的假实现：内存里存整份配置，记录每次调用，可注入失败与延迟。
// 零值可用：配置为空、测试与拉取都成功（延迟 42 ms、返回空模型列表）、用量为空。
type AISettingsFake struct {
	mu  sync.Mutex
	cfg services.AIConfig

	loadErr, saveErr, testErr, fetchErr, usageErr, clearErr error
	outcome                                                 *services.AITestOutcome
	fetchResult                                             *services.AIFetchModelsResult
	usage                                                   services.AIUsageSummary

	loads    int
	saves    []services.AIConfig
	tests    []services.AIModel
	fetches  []AIFetchCall
	usageReq []int
	clears   int

	// LoadGate 非 nil 时 Load 先等它关闭再返回：测试用来把「读取中」状态停在界面上。要在开始使用之前设置。
	LoadGate chan struct{}
	// TestGate 非 nil 时 TestModel 先等它关闭再返回：用来把「测试中」状态停在界面上。要在开始使用之前设置。
	TestGate chan struct{}
	// FetchGate 非 nil 时 FetchModels 先等它关闭再返回：用来把「拉取中」状态停在界面上。要在开始使用之前设置。
	FetchGate chan struct{}
}

var _ services.AISettings = (*AISettingsFake)(nil)

func (f *AISettingsFake) with(fn func()) {
	f.mu.Lock()
	fn()
	f.mu.Unlock()
}

// PutConfig 摆好当前配置（不记为一次保存）。
func (f *AISettingsFake) PutConfig(c services.AIConfig) { f.with(func() { f.cfg = c.Clone() }) }

// PutTestOutcome 摆好 TestModel 的结果（默认成功、延迟 42 ms）。
func (f *AISettingsFake) PutTestOutcome(o services.AITestOutcome) { f.with(func() { f.outcome = &o }) }

// PutFetchResult 摆好 FetchModels 的结果（默认成功、空列表）。
func (f *AISettingsFake) PutFetchResult(r services.AIFetchModelsResult) {
	f.with(func() { f.fetchResult = &r })
}

// PutUsage 摆好用量汇总。
func (f *AISettingsFake) PutUsage(u services.AIUsageSummary) { f.with(func() { f.usage = u }) }

// FailLoad、FailSave、FailTest、FailFetch、FailUsage、FailClear 注入对应动作的失败（nil 取消）；失败时不改任何状态。
func (f *AISettingsFake) FailLoad(err error)  { f.with(func() { f.loadErr = err }) }
func (f *AISettingsFake) FailSave(err error)  { f.with(func() { f.saveErr = err }) }
func (f *AISettingsFake) FailTest(err error)  { f.with(func() { f.testErr = err }) }
func (f *AISettingsFake) FailFetch(err error) { f.with(func() { f.fetchErr = err }) }
func (f *AISettingsFake) FailUsage(err error) { f.with(func() { f.usageErr = err }) }
func (f *AISettingsFake) FailClear(err error) { f.with(func() { f.clearErr = err }) }

// Loads 是读取配置的次数；Saves 是成功保存过的配置（按顺序）；Tests 是测试过的模型；Fetches 是拉取调用；
// UsageDays 是读用量时传的天数；Clears 是清空用量的次数。
func (f *AISettingsFake) Loads() (n int) { f.with(func() { n = f.loads }); return n }
func (f *AISettingsFake) Saves() (out []services.AIConfig) {
	f.with(func() {
		for _, c := range f.saves {
			out = append(out, c.Clone())
		}
	})
	return out
}
func (f *AISettingsFake) Tests() (out []services.AIModel) {
	f.with(func() { out = append(out, f.tests...) })
	return out
}
func (f *AISettingsFake) Fetches() (out []AIFetchCall) {
	f.with(func() { out = append(out, f.fetches...) })
	return out
}
func (f *AISettingsFake) UsageDays() (out []int) {
	f.with(func() { out = append(out, f.usageReq...) })
	return out
}
func (f *AISettingsFake) Clears() (n int) { f.with(func() { n = f.clears }); return n }

// Current 返回当前保存的配置（副本）。
func (f *AISettingsFake) Current() (c services.AIConfig) {
	f.with(func() { c = f.cfg.Clone() })
	return c
}

// Load 返回当前配置的副本。
func (f *AISettingsFake) Load(context.Context) (services.AIConfig, error) {
	var c services.AIConfig
	var err error
	var gate chan struct{}
	f.with(func() {
		f.loads++
		c, err, gate = f.cfg.Clone(), f.loadErr, f.LoadGate
	})
	if gate != nil {
		<-gate
	}
	return c, err
}

// Save 整体替换配置并记录。
func (f *AISettingsFake) Save(_ context.Context, cfg services.AIConfig) error {
	var err error
	f.with(func() {
		if err = f.saveErr; err != nil {
			return
		}
		f.cfg = cfg.Clone()
		f.saves = append(f.saves, cfg.Clone())
	})
	return err
}

// TestModel 记录被测模型，返回摆好的结果。
func (f *AISettingsFake) TestModel(_ context.Context, m services.AIModel) (services.AITestOutcome, error) {
	var out services.AITestOutcome
	var err error
	var gate chan struct{}
	f.with(func() {
		f.tests = append(f.tests, m)
		out, err, gate = services.AITestOutcome{Success: true, LatencyMs: 42}, f.testErr, f.TestGate
		if f.outcome != nil {
			out = *f.outcome
		}
	})
	if gate != nil {
		<-gate
	}
	return out, err
}

// FetchModels 记录调用，返回摆好的结果。
func (f *AISettingsFake) FetchModels(_ context.Context, apiURL, apiKey, protocol string) (services.AIFetchModelsResult, error) {
	var out services.AIFetchModelsResult
	var err error
	var gate chan struct{}
	f.with(func() {
		f.fetches = append(f.fetches, AIFetchCall{APIURL: apiURL, APIKey: apiKey, Protocol: protocol})
		out, err, gate = services.AIFetchModelsResult{Success: true}, f.fetchErr, f.FetchGate
		if f.fetchResult != nil {
			out = *f.fetchResult
		}
	})
	if gate != nil {
		<-gate
	}
	return out, err
}

// UsageSummary 记录天数，返回摆好的用量。
func (f *AISettingsFake) UsageSummary(_ context.Context, days int) (services.AIUsageSummary, error) {
	var out services.AIUsageSummary
	var err error
	f.with(func() {
		f.usageReq = append(f.usageReq, days)
		out, err = f.usage, f.usageErr
	})
	return out, err
}

// ClearUsage 清空摆好的用量并记数。
func (f *AISettingsFake) ClearUsage(context.Context) error {
	var err error
	f.with(func() {
		if err = f.clearErr; err != nil {
			return
		}
		f.usage = services.AIUsageSummary{}
		f.clears++
	})
	return err
}
