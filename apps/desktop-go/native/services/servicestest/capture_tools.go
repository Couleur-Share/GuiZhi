package servicestest

import (
	"context"
	"errors"
	"sync"

	"guizhi-mygo-shell/native/services"
)

// Capture 是 services.CaptureTools 的假实现：内存里存各引擎与平台账号的状态，记录每个动作，可注入失败与阻塞，可手动推送进度。
// 零值可用：两个二进制引擎与本地转写引擎都是「已装好、内置版、支持应用内安装」，三个平台都可用但没有登录。
type Capture struct {
	mu sync.Mutex

	started  bool
	tools    map[services.ManagedTool]*toolFake
	funasr   funasrFake
	accounts []services.PlatformStatus
	login    loginFake
	nextSub  int
	toolSubs map[services.ManagedTool]map[int]func(services.ToolProgress)
	fnSubs   map[int]func(services.FunasrProgress)

	// 下列通道非 nil 时对应动作先等它关闭再返回：测试用来把「安装中」「登录中」之类的状态停在界面上。
	// 要在 Capture 开始被使用之前设置。
	InstallGate map[services.ManagedTool]chan struct{} // 二进制引擎的安装
	FunasrGate  chan struct{}                          // 本地转写引擎的安装、更新与卸载
	LoginGate   chan struct{}                          // 平台登录
	CheckGate   chan struct{}                          // 二进制引擎与本地转写引擎的检查更新
}

type toolFake struct {
	status      services.ToolStatus
	statusErr   error
	check       services.ToolUpdateCheck
	checkErr    error
	installVer  string
	installErr  error
	removeErr   error
	pickPath    string
	pickErr     error
	statusCalls []bool // 每次探测的 force
	checks      int
	installs    int
	removes     int
	picks       int
}

type funasrFake struct {
	status      services.FunasrStatus
	statusErr   error
	check       services.ToolUpdateCheck
	checkErr    error
	installVer  string
	installErr  error
	update      services.FunasrUpdateResult
	updateErr   error
	uninstall   error
	statusCalls []bool
	checks      int
	installs    int
	updates     []string
	uninstalls  int
}

type loginFake struct {
	err       error
	logoutErr error
	clearErr  error
	statusErr error
	noCancel  bool // 为真时 PlatformCancelLogin 返回 false
	logins    []LoginCall
	cancels   []string
	logouts   []string
	clears    int
	statuses  int
}

// LoginCall 是一次平台登录调用。
type LoginCall struct {
	Platform string
	Force    bool
}

var _ services.CaptureTools = (*Capture)(nil)

// init 摆好默认状态；调用方必须持有 f.mu。
func (f *Capture) init() {
	if f.started {
		return
	}
	f.started = true
	f.tools = map[services.ManagedTool]*toolFake{}
	for _, tool := range []services.ManagedTool{services.ToolYtDlp, services.ToolFfmpeg} {
		path := `C:\fake\tools\` + string(tool) + ".exe"
		f.tools[tool] = &toolFake{
			status: services.ToolStatus{
				Installed: true, Source: services.ToolSourceManaged, Version: "2026.07.04",
				Path: path, ManagedPath: path, InstallSupported: true,
			},
			installVer: "2026.08.01",
		}
	}
	f.funasr = funasrFake{
		status:     services.FunasrStatus{Installed: true, Port: 10095, Dir: `C:\fake\funasr`, Version: "1.2.0", InstallSupported: true, UpdateSupported: true},
		installVer: "1.2.0",
	}
	for _, p := range []string{services.PlatformXiaohongshu, services.PlatformDouyin, services.PlatformLinuxdo} {
		f.accounts = append(f.accounts, services.PlatformStatus{Platform: p, Browser: "embedded", BrowserVersion: "126.0", Available: true})
	}
	f.toolSubs = map[services.ManagedTool]map[int]func(services.ToolProgress){}
	f.fnSubs = map[int]func(services.FunasrProgress){}
}

func (f *Capture) tool(t services.ManagedTool) *toolFake {
	f.init()
	return f.tools[t]
}

// with 在锁内执行 fn（先 init），给「摆状态」类方法省去重复的加解锁。
func (f *Capture) with(fn func()) {
	f.mu.Lock()
	f.init()
	fn()
	f.mu.Unlock()
}

// ── 二进制引擎：摆状态与注入失败 ──

// SetToolStatus 摆好引擎的探测结果；FailToolStatus 让之后的探测返回 err（nil 取消）。
func (f *Capture) SetToolStatus(t services.ManagedTool, s services.ToolStatus) {
	f.with(func() { f.tool(t).status = s })
}
func (f *Capture) FailToolStatus(t services.ManagedTool, err error) {
	f.with(func() { f.tool(t).statusErr = err })
}

// SetToolCheck 摆好之后的检查更新结果；FailToolCheck 注入失败。
func (f *Capture) SetToolCheck(t services.ManagedTool, c services.ToolUpdateCheck) {
	f.with(func() { f.tool(t).check = c })
}
func (f *Capture) FailToolCheck(t services.ManagedTool, err error) {
	f.with(func() { f.tool(t).checkErr = err })
}

// SetInstallVersion 摆好安装成功时返回的版本；FailInstall、FailRemove 注入失败。
// 安装成功后引擎状态变成「已装好的内置版」，移除成功后变成「未安装」。
func (f *Capture) SetInstallVersion(t services.ManagedTool, v string) {
	f.with(func() { f.tool(t).installVer = v })
}
func (f *Capture) FailInstall(t services.ManagedTool, err error) {
	f.with(func() { f.tool(t).installErr = err })
}
func (f *Capture) FailRemove(t services.ManagedTool, err error) {
	f.with(func() { f.tool(t).removeErr = err })
}

// SetPick 摆好文件选择器返回的路径（空串表示用户取消）；FailPick 注入失败。
func (f *Capture) SetPick(t services.ManagedTool, path string) {
	f.with(func() { f.tool(t).pickPath = path })
}
func (f *Capture) FailPick(t services.ManagedTool, err error) {
	f.with(func() { f.tool(t).pickErr = err })
}

// StatusCalls 返回每次探测的 force；Checks、Installs、Removes、Picks 是次数。
func (f *Capture) StatusCalls(t services.ManagedTool) (out []bool) {
	f.with(func() { out = append(out, f.tool(t).statusCalls...) })
	return out
}
func (f *Capture) Checks(t services.ManagedTool) (n int) {
	f.with(func() { n = f.tool(t).checks })
	return n
}
func (f *Capture) Installs(t services.ManagedTool) (n int) {
	f.with(func() { n = f.tool(t).installs })
	return n
}
func (f *Capture) Removes(t services.ManagedTool) (n int) {
	f.with(func() { n = f.tool(t).removes })
	return n
}
func (f *Capture) Picks(t services.ManagedTool) (n int) {
	f.with(func() { n = f.tool(t).picks })
	return n
}

// ToolSubscribers 返回某个引擎的进度订阅者数量。
func (f *Capture) ToolSubscribers(t services.ManagedTool) (n int) {
	f.with(func() { n = len(f.toolSubs[t]) })
	return n
}

// EmitToolProgress 向某个引擎的全部订阅者推送一次下载进度（锁外调用，订阅者里再调用本对象不会死锁）。
func (f *Capture) EmitToolProgress(t services.ManagedTool, p services.ToolProgress) {
	var fns []func(services.ToolProgress)
	f.with(func() {
		for _, fn := range f.toolSubs[t] {
			fns = append(fns, fn)
		}
	})
	for _, fn := range fns {
		fn(p)
	}
}

// ── 本地转写引擎：摆状态与注入失败 ──

// SetFunasrStatus 摆好本地转写引擎的状态；FailFunasrStatus 注入探测失败。
func (f *Capture) SetFunasrStatus(s services.FunasrStatus) { f.with(func() { f.funasr.status = s }) }
func (f *Capture) FailFunasrStatus(err error)              { f.with(func() { f.funasr.statusErr = err }) }

// SetFunasrCheck 摆好检查更新的结果；FailFunasrCheck 注入失败。
func (f *Capture) SetFunasrCheck(c services.ToolUpdateCheck) { f.with(func() { f.funasr.check = c }) }
func (f *Capture) FailFunasrCheck(err error)                 { f.with(func() { f.funasr.checkErr = err }) }

// SetFunasrUpdateResult 摆好更新成功的结果；FailFunasrInstall、FailFunasrUpdate、FailFunasrUninstall 注入失败。
func (f *Capture) SetFunasrUpdateResult(r services.FunasrUpdateResult) {
	f.with(func() { f.funasr.update = r })
}
func (f *Capture) FailFunasrInstall(err error)   { f.with(func() { f.funasr.installErr = err }) }
func (f *Capture) FailFunasrUpdate(err error)    { f.with(func() { f.funasr.updateErr = err }) }
func (f *Capture) FailFunasrUninstall(err error) { f.with(func() { f.funasr.uninstall = err }) }

// FunasrStatusCalls 返回每次探测的 force；FunasrChecks、FunasrInstalls、FunasrUninstalls 是次数；FunasrUpdates 是更新的目标版本。
func (f *Capture) FunasrStatusCalls() (out []bool) {
	f.with(func() { out = append(out, f.funasr.statusCalls...) })
	return out
}
func (f *Capture) FunasrChecks() (n int)     { f.with(func() { n = f.funasr.checks }); return n }
func (f *Capture) FunasrInstalls() (n int)   { f.with(func() { n = f.funasr.installs }); return n }
func (f *Capture) FunasrUninstalls() (n int) { f.with(func() { n = f.funasr.uninstalls }); return n }
func (f *Capture) FunasrUpdates() (out []string) {
	f.with(func() { out = append(out, f.funasr.updates...) })
	return out
}

// FunasrSubscribers 返回本地转写引擎的进度订阅者数量。
func (f *Capture) FunasrSubscribers() (n int) { f.with(func() { n = len(f.fnSubs) }); return n }

// EmitFunasrProgress 向全部订阅者推送一次安装 / 更新进度。
func (f *Capture) EmitFunasrProgress(p services.FunasrProgress) {
	var fns []func(services.FunasrProgress)
	f.with(func() {
		for _, fn := range f.fnSubs {
			fns = append(fns, fn)
		}
	})
	for _, fn := range fns {
		fn(p)
	}
}

// ── 平台账号：摆状态与注入失败 ──

// SetAccounts 摆好各平台账号的状态（整张表替换）。
func (f *Capture) SetAccounts(list []services.PlatformStatus) {
	f.with(func() { f.accounts = append([]services.PlatformStatus(nil), list...) })
}

// FailAccountStatus、FailLogin、FailLogout、FailClear 注入对应动作的失败（nil 取消）。
func (f *Capture) FailAccountStatus(err error) { f.with(func() { f.login.statusErr = err }) }
func (f *Capture) FailLogin(err error)         { f.with(func() { f.login.err = err }) }
func (f *Capture) FailLogout(err error)        { f.with(func() { f.login.logoutErr = err }) }
func (f *Capture) FailClear(err error)         { f.with(func() { f.login.clearErr = err }) }

// RefuseCancel 让 PlatformCancelLogin 返回 false（没有可取消的登录）。
func (f *Capture) RefuseCancel(on bool) { f.with(func() { f.login.noCancel = on }) }

// Logins、Cancels、Logouts 返回收到的调用；Clears 与 AccountStatusCalls 是次数。
func (f *Capture) Logins() (out []LoginCall) {
	f.with(func() { out = append(out, f.login.logins...) })
	return out
}
func (f *Capture) Cancels() (out []string) {
	f.with(func() { out = append(out, f.login.cancels...) })
	return out
}
func (f *Capture) Logouts() (out []string) {
	f.with(func() { out = append(out, f.login.logouts...) })
	return out
}
func (f *Capture) Clears() (n int)             { f.with(func() { n = f.login.clears }); return n }
func (f *Capture) AccountStatusCalls() (n int) { f.with(func() { n = f.login.statuses }); return n }

// ── services.CaptureTools：二进制引擎 ──

func (f *Capture) ToolStatus(_ context.Context, t services.ManagedTool, force bool) (services.ToolStatus, error) {
	var st services.ToolStatus
	var err error
	f.with(func() {
		tf := f.tool(t)
		tf.statusCalls = append(tf.statusCalls, force)
		st, err = tf.status, tf.statusErr
	})
	return st, err
}

func (f *Capture) CheckToolUpdate(_ context.Context, t services.ManagedTool) (services.ToolUpdateCheck, error) {
	var c services.ToolUpdateCheck
	var err error
	var gate chan struct{}
	f.with(func() {
		tf := f.tool(t)
		tf.checks++
		c, err, gate = tf.check, tf.checkErr, f.CheckGate
	})
	if gate != nil {
		<-gate
	}
	return c, err
}

func (f *Capture) InstallTool(_ context.Context, t services.ManagedTool) (string, error) {
	var gate chan struct{}
	f.with(func() {
		f.tool(t).installs++
		gate = f.InstallGate[t]
	})
	if gate != nil {
		<-gate
	}
	var ver string
	var err error
	f.with(func() {
		tf := f.tool(t)
		ver, err = tf.installVer, tf.installErr
		if err == nil {
			tf.status.Installed, tf.status.Source, tf.status.Version = true, services.ToolSourceManaged, ver
			tf.status.Path = tf.status.ManagedPath
		}
	})
	return ver, err
}

func (f *Capture) RemoveTool(_ context.Context, t services.ManagedTool) error {
	var err error
	f.with(func() {
		tf := f.tool(t)
		tf.removes++
		err = tf.removeErr
		if err == nil {
			tf.status.Installed, tf.status.Source, tf.status.Version, tf.status.Path = false, "", "", ""
		}
	})
	return err
}

func (f *Capture) PickToolBinary(_ context.Context, t services.ManagedTool) (string, error) {
	var path string
	var err error
	f.with(func() {
		tf := f.tool(t)
		tf.picks++
		path, err = tf.pickPath, tf.pickErr
	})
	return path, err
}

func (f *Capture) OnToolProgress(t services.ManagedTool, fn func(services.ToolProgress)) (cancel func()) {
	var id int
	f.with(func() {
		if f.toolSubs[t] == nil {
			f.toolSubs[t] = map[int]func(services.ToolProgress){}
		}
		f.nextSub++
		id = f.nextSub
		f.toolSubs[t][id] = fn
	})
	return func() { f.with(func() { delete(f.toolSubs[t], id) }) }
}

// ── services.CaptureTools：本地转写引擎 ──

func (f *Capture) FunasrStatus(_ context.Context, force bool) (services.FunasrStatus, error) {
	var st services.FunasrStatus
	var err error
	f.with(func() {
		f.funasr.statusCalls = append(f.funasr.statusCalls, force)
		st, err = f.funasr.status, f.funasr.statusErr
	})
	return st, err
}

func (f *Capture) CheckFunasrUpdate(context.Context) (services.ToolUpdateCheck, error) {
	var c services.ToolUpdateCheck
	var err error
	var gate chan struct{}
	f.with(func() {
		f.funasr.checks++
		c, err, gate = f.funasr.check, f.funasr.checkErr, f.CheckGate
	})
	if gate != nil {
		<-gate
	}
	return c, err
}

func (f *Capture) InstallFunasr(context.Context) (string, error) {
	var gate chan struct{}
	f.with(func() {
		f.funasr.installs++
		gate = f.FunasrGate
	})
	if gate != nil {
		<-gate
	}
	var ver string
	var err error
	f.with(func() {
		ver, err = f.funasr.installVer, f.funasr.installErr
		if err == nil {
			f.funasr.status.Installed, f.funasr.status.Version = true, ver
		}
	})
	return ver, err
}

func (f *Capture) UpdateFunasr(_ context.Context, version string) (services.FunasrUpdateResult, error) {
	var gate chan struct{}
	f.with(func() {
		f.funasr.updates = append(f.funasr.updates, version)
		gate = f.FunasrGate
	})
	if gate != nil {
		<-gate
	}
	var res services.FunasrUpdateResult
	var err error
	f.with(func() {
		res, err = f.funasr.update, f.funasr.updateErr
		if err == nil {
			f.funasr.status.Version = version
		}
	})
	return res, err
}

func (f *Capture) UninstallFunasr(context.Context) error {
	var gate chan struct{}
	f.with(func() {
		f.funasr.uninstalls++
		gate = f.FunasrGate
	})
	if gate != nil {
		<-gate
	}
	var err error
	f.with(func() {
		err = f.funasr.uninstall
		if err == nil {
			f.funasr.status.Installed, f.funasr.status.Running, f.funasr.status.Version = false, false, ""
		}
	})
	return err
}

func (f *Capture) OnFunasrProgress(fn func(services.FunasrProgress)) (cancel func()) {
	var id int
	f.with(func() {
		f.nextSub++
		id = f.nextSub
		f.fnSubs[id] = fn
	})
	return func() { f.with(func() { delete(f.fnSubs, id) }) }
}

// ── services.CaptureTools：平台账号 ──

func (f *Capture) account(platform string) *services.PlatformStatus {
	for i := range f.accounts {
		if f.accounts[i].Platform == platform {
			return &f.accounts[i]
		}
	}
	return nil
}

var errUnknownPlatform = errors.New("unknown platform")

func (f *Capture) PlatformStatuses(context.Context) ([]services.PlatformStatus, error) {
	var out []services.PlatformStatus
	var err error
	f.with(func() {
		f.login.statuses++
		out, err = append(out, f.accounts...), f.login.statusErr
	})
	return out, err
}

// PlatformLogin 登录期间账号是「忙」的（statuses 里 busy 为真），等 LoginGate 放行后按 FailLogin 的设置成功或失败；成功后账号已登录。
func (f *Capture) PlatformLogin(_ context.Context, platform string, force bool) (services.PlatformStatus, error) {
	var gate chan struct{}
	var unknown bool
	f.with(func() {
		f.login.logins = append(f.login.logins, LoginCall{Platform: platform, Force: force})
		if a := f.account(platform); a != nil {
			a.Busy = true
		} else {
			unknown = true
		}
		gate = f.LoginGate
	})
	if unknown {
		return services.PlatformStatus{}, errUnknownPlatform
	}
	if gate != nil {
		<-gate
	}
	var st services.PlatformStatus
	var err error
	f.with(func() {
		a := f.account(platform)
		a.Busy = false
		if f.login.err != nil {
			err = f.login.err
			return
		}
		a.LoggedIn = true
		st = *a
	})
	return st, err
}

func (f *Capture) PlatformCancelLogin(_ context.Context, platform string) (bool, error) {
	var ok bool
	f.with(func() {
		f.login.cancels = append(f.login.cancels, platform)
		ok = !f.login.noCancel
	})
	return ok, nil
}

func (f *Capture) PlatformLogout(_ context.Context, platform string) error {
	var err error
	f.with(func() {
		f.login.logouts = append(f.login.logouts, platform)
		if err = f.login.logoutErr; err != nil {
			return
		}
		if a := f.account(platform); a != nil {
			a.LoggedIn = false
		}
	})
	return err
}

func (f *Capture) PlatformClearAll(context.Context) error {
	var err error
	f.with(func() {
		f.login.clears++
		if err = f.login.clearErr; err != nil {
			return
		}
		for i := range f.accounts {
			f.accounts[i].LoggedIn = false
		}
	})
	return err
}
