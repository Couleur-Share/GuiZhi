package servicestest

import (
	"context"
	"sync"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/settings"
)

// Shortcuts 是 services.Shortcuts 的假实现：内存里存组合键与模式，记录每次保存，可注入失败。零值可用，初值是默认快捷键。
type Shortcuts struct {
	mu       sync.Mutex
	started  bool
	cfg      services.ShortcutConfig
	getErr   error
	saveErr  error
	modesErr error
	saves    []map[string]string
	modeSets []map[string]string
	gets     int
}

var _ services.Shortcuts = (*Shortcuts)(nil)

func (f *Shortcuts) init() {
	if !f.started {
		f.cfg = services.ShortcutConfig{Accelerators: services.DefaultShortcuts(), Modes: services.DefaultShortcutModes()}
		f.started = true
	}
}

func cloneStrings(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// Set 直接摆好当前配置（不记为一次保存）。
func (f *Shortcuts) Set(cfg services.ShortcutConfig) {
	f.mu.Lock()
	f.cfg = services.ShortcutConfig{Accelerators: cloneStrings(cfg.Accelerators), Modes: cloneStrings(cfg.Modes)}
	f.started = true
	f.mu.Unlock()
}

// FailGet、FailSave、FailModes 让对应的调用返回 err 且不改配置；传 nil 取消。
func (f *Shortcuts) FailGet(err error)   { f.mu.Lock(); f.getErr = err; f.mu.Unlock() }
func (f *Shortcuts) FailSave(err error)  { f.mu.Lock(); f.saveErr = err; f.mu.Unlock() }
func (f *Shortcuts) FailModes(err error) { f.mu.Lock(); f.modesErr = err; f.mu.Unlock() }

// Saves 返回成功保存过的组合键表（按顺序）；ModeSets 同理；Gets 是读取次数。
func (f *Shortcuts) Saves() []map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]string(nil), f.saves...)
}
func (f *Shortcuts) ModeSets() []map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]string(nil), f.modeSets...)
}
func (f *Shortcuts) Gets() int { f.mu.Lock(); defer f.mu.Unlock(); return f.gets }

// Current 返回当前配置（副本）。
func (f *Shortcuts) Current() services.ShortcutConfig {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.init()
	return services.ShortcutConfig{Accelerators: cloneStrings(f.cfg.Accelerators), Modes: cloneStrings(f.cfg.Modes)}
}

func (f *Shortcuts) Get(context.Context) (services.ShortcutConfig, error) {
	f.mu.Lock()
	f.gets++
	err := f.getErr
	f.mu.Unlock()
	if err != nil {
		return services.ShortcutConfig{}, err
	}
	return f.Current(), nil
}

func (f *Shortcuts) Save(_ context.Context, accelerators map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.init()
	if f.saveErr != nil {
		return f.saveErr
	}
	f.cfg.Accelerators = cloneStrings(accelerators)
	f.saves = append(f.saves, cloneStrings(accelerators))
	return nil
}

func (f *Shortcuts) SetModes(_ context.Context, modes map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.init()
	if f.modesErr != nil {
		return f.modesErr
	}
	f.cfg.Modes = cloneStrings(modes)
	f.modeSets = append(f.modeSets, cloneStrings(modes))
	return nil
}

// Solver 是 services.FlareSolverr 的假实现：记录每次测试用的配置，返回注入的结果。零值可用（连通）。
type Solver struct {
	mu    sync.Mutex
	err   error
	calls []settings.FlareSolverr
	// Gate 非 nil 时 Check 先等它关闭再返回：测试用来把「测试中」状态停在界面上。
	Gate chan struct{}
}

var _ services.FlareSolverr = (*Solver)(nil)

// FailWith 让之后的 Check 返回 err；传 nil 恢复连通。
func (f *Solver) FailWith(err error) { f.mu.Lock(); f.err = err; f.mu.Unlock() }

// Calls 返回每次 Check 收到的配置。
func (f *Solver) Calls() []settings.FlareSolverr {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]settings.FlareSolverr(nil), f.calls...)
}

func (f *Solver) Check(_ context.Context, cfg settings.FlareSolverr) error {
	f.mu.Lock()
	f.calls = append(f.calls, cfg)
	err, gate := f.err, f.Gate
	f.mu.Unlock()
	if gate != nil {
		<-gate
	}
	return err
}

// Updater 是 services.Updater 的假实现：返回注入的检查结果与错误，记录每个动作，可手动推送状态。零值可用（已是最新版本）。
type Updater struct {
	mu         sync.Mutex
	result     services.UpdateCheckResult
	checkErr   error
	dlErr      error
	installErr error
	checks     []services.UpdateCheckOptions
	downloads  []services.UpdateCheckOptions
	installs   int
	releases   int
	nextSub    int
	subs       map[int]func(services.UpdateStatus)
	// Gate 非 nil 时 Check 先等它关闭再返回：测试用来把「检查中」状态停在界面上；DownloadGate 同理，停在「下载中」。
	Gate         chan struct{}
	DownloadGate chan struct{}
}

var _ services.Updater = (*Updater)(nil)

// SetResult 设置之后的 Check 返回的结果；FailCheck、FailDownload、FailInstall 注入对应动作的错误（nil 取消）。
func (f *Updater) SetResult(r services.UpdateCheckResult) { f.mu.Lock(); f.result = r; f.mu.Unlock() }
func (f *Updater) FailCheck(err error)                    { f.mu.Lock(); f.checkErr = err; f.mu.Unlock() }
func (f *Updater) FailDownload(err error)                 { f.mu.Lock(); f.dlErr = err; f.mu.Unlock() }
func (f *Updater) FailInstall(err error)                  { f.mu.Lock(); f.installErr = err; f.mu.Unlock() }

// Checks、Downloads 返回收到的参数；Installs、ReleaseOpens 是次数。
func (f *Updater) Checks() []services.UpdateCheckOptions {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]services.UpdateCheckOptions(nil), f.checks...)
}
func (f *Updater) Downloads() []services.UpdateCheckOptions {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]services.UpdateCheckOptions(nil), f.downloads...)
}
func (f *Updater) Installs() int     { f.mu.Lock(); defer f.mu.Unlock(); return f.installs }
func (f *Updater) ReleaseOpens() int { f.mu.Lock(); defer f.mu.Unlock(); return f.releases }

// Subscribers 返回当前订阅者数量。
func (f *Updater) Subscribers() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.subs) }

// Emit 向全部订阅者推送一次状态（锁外调用，订阅者里再调用本对象不会死锁）。
func (f *Updater) Emit(s services.UpdateStatus) {
	f.mu.Lock()
	fns := make([]func(services.UpdateStatus), 0, len(f.subs))
	for _, fn := range f.subs {
		fns = append(fns, fn)
	}
	f.mu.Unlock()
	for _, fn := range fns {
		fn(s)
	}
}

func (f *Updater) Check(_ context.Context, opts services.UpdateCheckOptions) (services.UpdateCheckResult, error) {
	f.mu.Lock()
	f.checks = append(f.checks, opts)
	res, err, gate := f.result, f.checkErr, f.Gate
	f.mu.Unlock()
	if gate != nil {
		<-gate
	}
	return res, err
}

func (f *Updater) Download(_ context.Context, opts services.UpdateCheckOptions) error {
	f.mu.Lock()
	f.downloads = append(f.downloads, opts)
	err, gate := f.dlErr, f.DownloadGate
	f.mu.Unlock()
	if gate != nil {
		<-gate
	}
	return err
}

func (f *Updater) Install(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.installs++
	return f.installErr
}

func (f *Updater) OpenReleases() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.releases++
	return nil
}

func (f *Updater) OnStatus(fn func(services.UpdateStatus)) (cancel func()) {
	f.mu.Lock()
	if f.subs == nil {
		f.subs = map[int]func(services.UpdateStatus){}
	}
	f.nextSub++
	id := f.nextSub
	f.subs[id] = fn
	f.mu.Unlock()
	return func() {
		f.mu.Lock()
		delete(f.subs, id)
		f.mu.Unlock()
	}
}
