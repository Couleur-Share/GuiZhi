package servicestest

import (
	"context"
	"errors"
	"sync"

	"guizhi-mygo-shell/native/services"
)

// PlatformAuth 是 services.PlatformAuth 的假实现：平台状态放在内存里，登录默认立刻成功，
// 也可以用 HoldLogin 让登录停在「等用户操作」，之后由 finish 结束、CancelLogin 取消或 ctx 取消。
// 零值可用（没有任何平台）；所有方法并发安全。
type PlatformAuth struct {
	mu        sync.Mutex
	statuses  []services.PlatformStatus
	statusErr error
	loginErr  map[string]error
	calls     []string
	opts      []services.PlatformLoginOptions
	pending   map[string]*pendingLogin
	holds     map[string]*pendingLogin
	cancelErr error
}

// pendingLogin 是一次停住的登录：done 由 finish 或取消写入一次结果。
type pendingLogin struct {
	done     chan error
	finished bool
}

var _ services.PlatformAuth = (*PlatformAuth)(nil)

// SetStatuses 整体替换平台状态列表。
func (f *PlatformAuth) SetStatuses(list ...services.PlatformStatus) {
	f.mu.Lock()
	f.statuses = append([]services.PlatformStatus(nil), list...)
	f.mu.Unlock()
}

// FailStatuses 让之后的 Statuses 失败；err 为 nil 取消注入。
func (f *PlatformAuth) FailStatuses(err error) {
	f.mu.Lock()
	f.statusErr = err
	f.mu.Unlock()
}

// FailLogin 让之后针对该平台的 Login 失败；err 为 nil 取消注入。
func (f *PlatformAuth) FailLogin(platform string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.loginErr == nil {
		f.loginErr = map[string]error{}
	}
	if err == nil {
		delete(f.loginErr, platform)
		return
	}
	f.loginErr[platform] = err
}

// FailCancel 让之后的 CancelLogin 失败；err 为 nil 取消注入。
func (f *PlatformAuth) FailCancel(err error) {
	f.mu.Lock()
	f.cancelErr = err
	f.mu.Unlock()
}

// HoldLogin 让之后针对该平台的 Login 停住，直到返回的 finish 被调用（err 为 nil 表示登录成功并把状态改成已登录）、
// 或 CancelLogin、或调用方的 ctx 取消。只对紧接着的那一次 Login 生效。finish 可重复调用，只有第一次算数。
func (f *PlatformAuth) HoldLogin(platform string) (finish func(err error)) {
	hold := &pendingLogin{done: make(chan error, 1)}
	f.mu.Lock()
	if f.holds == nil {
		f.holds = map[string]*pendingLogin{}
	}
	f.holds[platform] = hold
	f.mu.Unlock()
	return func(err error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if hold.finished {
			return
		}
		hold.finished = true
		hold.done <- err
	}
}

// Pending 报告该平台是否有一次登录正停在等待里（测试用来等「验证窗口已打开」）。
func (f *PlatformAuth) Pending(platform string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pending[platform] != nil
}

// Calls 返回调用记录，每项形如 "status"、"login:nodeseek"、"cancel:nodeseek"。
func (f *PlatformAuth) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// LoginOptions 返回迄今每次登录收到的参数，按调用顺序。
func (f *PlatformAuth) LoginOptions() []services.PlatformLoginOptions {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]services.PlatformLoginOptions(nil), f.opts...)
}

func (f *PlatformAuth) Statuses(context.Context) ([]services.PlatformStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "status")
	if f.statusErr != nil {
		return nil, f.statusErr
	}
	return append([]services.PlatformStatus{}, f.statuses...), nil
}

func (f *PlatformAuth) Login(ctx context.Context, platform string, opts services.PlatformLoginOptions) (services.PlatformStatus, error) {
	f.mu.Lock()
	f.calls = append(f.calls, "login:"+platform)
	f.opts = append(f.opts, opts)
	if err := f.loginErr[platform]; err != nil {
		f.mu.Unlock()
		return services.PlatformStatus{}, err
	}
	hold := f.holds[platform]
	delete(f.holds, platform)
	if hold != nil {
		if f.pending == nil {
			f.pending = map[string]*pendingLogin{}
		}
		f.pending[platform] = hold
	}
	f.mu.Unlock()

	if hold != nil {
		var err error
		select {
		case err = <-hold.done:
		case <-ctx.Done():
			err = ctx.Err()
		}
		f.mu.Lock()
		if f.pending[platform] == hold {
			delete(f.pending, platform)
		}
		f.mu.Unlock()
		if err != nil {
			return services.PlatformStatus{}, err
		}
	}
	return f.markLoggedIn(platform), nil
}

// markLoggedIn 把该平台的状态改成已登录；列表里没有这个平台时补一项，免得返回的状态与列表对不上。
func (f *PlatformAuth) markLoggedIn(platform string) services.PlatformStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.statuses {
		if f.statuses[i].Platform == platform {
			f.statuses[i].LoggedIn = true
			return f.statuses[i]
		}
	}
	st := services.PlatformStatus{Platform: platform, Browser: "embedded", Available: true, LoggedIn: true}
	f.statuses = append(f.statuses, st)
	return st
}

func (f *PlatformAuth) CancelLogin(_ context.Context, platform string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "cancel:"+platform)
	if f.cancelErr != nil {
		return false, f.cancelErr
	}
	hold := f.pending[platform]
	if hold == nil || hold.finished {
		return false, nil
	}
	hold.finished = true
	hold.done <- errors.New("login canceled")
	return true, nil
}
