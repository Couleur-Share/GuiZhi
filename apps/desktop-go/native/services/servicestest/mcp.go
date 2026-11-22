package servicestest

import (
	"context"
	"sync"

	"guizhi-mygo-shell/native/services"
)

// MCPFake 是 services.MCP 的假实现：内存里存启动信息与可访问范围，记录每次动作，可注入失败。
// 零值可用：组件就绪、启动命令是 C:\fake\GuiZhi.exe --mcp、范围是「全部可见」。
type MCPFake struct {
	mu      sync.Mutex
	started bool
	cfg     services.MCPConfig
	scope   services.MCPScope

	cfgErr     error
	scopeErr   error
	setErr     error
	installErr error
	install    services.MCPInstallResult

	configs  int
	scopeSet []services.MCPScope
	installs []services.MCPClient

	// ConfigGate 非 nil 时 Config 先等它关闭再返回：测试用来把「读取中」状态停在界面上。要在开始使用之前设置。
	ConfigGate chan struct{}
}

var _ services.MCP = (*MCPFake)(nil)

func (f *MCPFake) init() {
	if f.started {
		return
	}
	f.started = true
	f.cfg = services.MCPConfig{
		Available: true, ServerPath: `C:\fake\GuiZhi.exe`, Command: `C:\fake\GuiZhi.exe`,
		Args: []string{"--mcp"}, Env: map[string]string{},
	}
	f.scope = services.DefaultMCPScope()
	f.install = services.MCPInstallResult{FilePath: `C:\fake\home\.cursor\mcp.json`}
}

func (f *MCPFake) with(fn func()) {
	f.mu.Lock()
	f.init()
	fn()
	f.mu.Unlock()
}

// SetConfig 摆好启动信息；PutScope 摆好当前范围（不记为一次保存）；SetInstallResult 摆好一键写入成功时的结果。
func (f *MCPFake) SetConfig(c services.MCPConfig) { f.with(func() { f.cfg = c }) }
func (f *MCPFake) PutScope(s services.MCPScope)   { f.with(func() { f.scope = s.Clone() }) }
func (f *MCPFake) SetInstallResult(r services.MCPInstallResult) {
	f.with(func() { f.install = r })
}

// FailConfig、FailScope、FailSetScope、FailInstall 注入对应动作的失败（nil 取消）；失败时不改任何状态。
func (f *MCPFake) FailConfig(err error)   { f.with(func() { f.cfgErr = err }) }
func (f *MCPFake) FailScope(err error)    { f.with(func() { f.scopeErr = err }) }
func (f *MCPFake) FailSetScope(err error) { f.with(func() { f.setErr = err }) }
func (f *MCPFake) FailInstall(err error)  { f.with(func() { f.installErr = err }) }

// Configs 是读取启动信息的次数；ScopeSets 是成功保存过的范围（按顺序）；Installs 是一键写入过的客户端。
func (f *MCPFake) Configs() (n int) { f.with(func() { n = f.configs }); return n }
func (f *MCPFake) ScopeSets() (out []services.MCPScope) {
	f.with(func() {
		for _, s := range f.scopeSet {
			out = append(out, s.Clone())
		}
	})
	return out
}
func (f *MCPFake) Installs() (out []services.MCPClient) {
	f.with(func() { out = append(out, f.installs...) })
	return out
}

// CurrentScope 返回当前保存的范围（副本）。
func (f *MCPFake) CurrentScope() (s services.MCPScope) {
	f.with(func() { s = f.scope.Clone() })
	return s
}

func (f *MCPFake) Config(context.Context) (services.MCPConfig, error) {
	var c services.MCPConfig
	var err error
	var gate chan struct{}
	f.with(func() {
		f.configs++
		c, err, gate = f.cfg, f.cfgErr, f.ConfigGate
	})
	if gate != nil {
		<-gate
	}
	return c, err
}

func (f *MCPFake) Scope(context.Context) (services.MCPScope, error) {
	var s services.MCPScope
	var err error
	f.with(func() { s, err = f.scope.Clone(), f.scopeErr })
	return s, err
}

func (f *MCPFake) SetScope(_ context.Context, scope services.MCPScope) (services.MCPScope, error) {
	var out services.MCPScope
	var err error
	f.with(func() {
		if err = f.setErr; err != nil {
			return
		}
		f.scope = scope.Clone()
		f.scopeSet = append(f.scopeSet, scope.Clone())
		out = f.scope.Clone()
	})
	return out, err
}

func (f *MCPFake) Install(_ context.Context, client services.MCPClient) (services.MCPInstallResult, error) {
	var res services.MCPInstallResult
	var err error
	f.with(func() {
		f.installs = append(f.installs, client)
		res, err = f.install, f.installErr
	})
	return res, err
}