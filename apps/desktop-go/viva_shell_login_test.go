package main

import (
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/settings"
)

// loginRecorder 记下登录项写入调用（只记录，不碰真实注册表）。
type loginRecorder struct {
	mu    sync.Mutex
	calls []bool
	err   error
}

func (l *loginRecorder) set(open bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, open)
	return l.err
}

func (l *loginRecorder) got() []bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]bool(nil), l.calls...)
}

func setLaunchAtStartup(t *testing.T, r *shellRig, on bool) {
	t.Helper()
	if _, err := r.set.Update(func(v *settings.Values) { v.App.LaunchAtStartup = on }); err != nil {
		t.Fatal(err)
	}
}

// 启动即隐藏：窗口建好后直接驻留托盘并通知可见性；点托盘（切换）再显示。
func TestVivaShellStartHiddenResidesInTray(t *testing.T) {
	r := newShellRigWith(t, settings.CloseAsk, false, func(r *shellRig) { r.shell.startHidden = true })
	if r.host.count("hide") != 1 || r.host.Visible() {
		t.Fatalf("启动即隐藏应隐藏窗口：%v", r.host.calls)
	}
	r.host.drain()
	if r.host.count("tray-add") != 1 {
		t.Errorf("驻留托盘必须把托盘图标建好，否则没有地方点回来：%v", r.host.calls)
	}
	if got := r.pushed(); !reflect.DeepEqual(got, []string{"window:visibility-changed=false"}) {
		t.Errorf("应通知界面窗口不可见：%v", got)
	}
	r.lc.toggleWindow() // 托盘左键 / 第二个实例拉起
	if r.host.count("show") != 1 || !r.host.Visible() {
		t.Errorf("应能从托盘显示窗口：%v", r.host.calls)
	}
}

func TestVivaShellNormalLaunchStaysVisible(t *testing.T) {
	r := newShellRig(t, settings.CloseAsk, false)
	if r.host.count("hide") != 0 || !r.host.Visible() {
		t.Fatalf("默认不隐藏：%v", r.host.calls)
	}
	r.host.drain()
	if r.host.count("tray-add") != 0 {
		t.Error("关闭行为是 ask 且不隐藏启动时不该建托盘")
	}
	if len(r.pushed()) != 0 {
		t.Errorf("没有可见性变化不该推送：%v", r.pushed())
	}
}

// B-024：安装包构建且启动时设置已开，刷新一次旧版登记到当前安装路径。
func TestVivaShellRefreshesLegacyLoginItemWhenPackaged(t *testing.T) {
	rec := &loginRecorder{}
	r := newShellRigWith(t, settings.CloseAsk, false, func(r *shellRig) {
		r.shell.manageLoginItem, r.shell.packaged = true, true
		r.lc.setLoginItem = rec.set
		setLaunchAtStartup(t, r, true)
	})
	if got := rec.got(); !reflect.DeepEqual(got, []bool{true}) {
		t.Fatalf("启动期应刷新一次登记：%v", got)
	}
	r.shell.Shutdown()
	if r.set.Subscribers() != 0 {
		t.Error("Shutdown 应当撤销设置订阅")
	}
}

// 启动时设置为关：不动注册表（用户可能在任务管理器里自己管理）；之后随设置变化写入，无关设置的变化不写。
func TestVivaShellLoginItemFollowsSetting(t *testing.T) {
	rec := &loginRecorder{}
	r := newShellRigWith(t, settings.CloseAsk, false, func(r *shellRig) {
		r.shell.manageLoginItem, r.shell.packaged = true, true
		r.lc.setLoginItem = rec.set
	})
	if got := rec.got(); len(got) != 0 {
		t.Fatalf("设置为关时启动期不该动注册表：%v", got)
	}
	if _, err := r.set.Update(func(v *settings.Values) { v.UI.CloseAction = settings.CloseExit }); err != nil {
		t.Fatal(err)
	}
	if got := rec.got(); len(got) != 0 {
		t.Fatalf("无关设置的变化不该写登录项：%v", got)
	}
	setLaunchAtStartup(t, r, true)
	setLaunchAtStartup(t, r, false)
	if got := rec.got(); !reflect.DeepEqual(got, []bool{true, false}) {
		t.Fatalf("设置变化应写登录项：%v", got)
	}
}

// 非安装包构建（开发、自行构建）启动时不刷新登记；用户之后在设置里切换仍然写。
func TestVivaShellNoStartupRefreshWhenNotPackaged(t *testing.T) {
	rec := &loginRecorder{}
	r := newShellRigWith(t, settings.CloseAsk, false, func(r *shellRig) {
		r.shell.manageLoginItem = true
		r.lc.setLoginItem = rec.set
		setLaunchAtStartup(t, r, true)
	})
	if got := rec.got(); len(got) != 0 {
		t.Fatalf("非安装包构建启动期不该写：%v", got)
	}
	setLaunchAtStartup(t, r, false)
	if got := rec.got(); !reflect.DeepEqual(got, []bool{false}) {
		t.Fatalf("got %v", got)
	}
}

// 自动化隔离护栏（外壳一侧）：manageLoginItem 为假时，无论设置怎么变、安装包与否，都绝不调用登录项写入。
func TestVivaShellUnmanagedNeverTouchesLoginItem(t *testing.T) {
	rec := &loginRecorder{}
	r := newShellRigWith(t, settings.CloseAsk, false, func(r *shellRig) {
		r.shell.packaged = true // manageLoginItem 缺省为假
		r.lc.setLoginItem = rec.set
		setLaunchAtStartup(t, r, true)
	})
	setLaunchAtStartup(t, r, false)
	setLaunchAtStartup(t, r, true)
	if got := rec.got(); len(got) != 0 {
		t.Fatalf("不管理登录项时不该有任何写入：%v", got)
	}
	if r.set.Subscribers() != 1 { // 只有关闭行为的订阅
		t.Errorf("不管理登录项时不该订阅登录项变化：订阅数 %d", r.set.Subscribers())
	}
}

// 写登录项失败只记日志，不阻断设置、不影响后续变化；登录项函数为空（隔离实例）时静默跳过。
func TestVivaShellLoginItemFailureAndNilAreSafe(t *testing.T) {
	rec := &loginRecorder{err: errors.New("注册表拒绝访问")}
	r := newShellRigWith(t, settings.CloseAsk, false, func(r *shellRig) {
		r.shell.manageLoginItem = true
		r.lc.setLoginItem = rec.set
	})
	setLaunchAtStartup(t, r, true)
	setLaunchAtStartup(t, r, false)
	if got := rec.got(); !reflect.DeepEqual(got, []bool{true, false}) {
		t.Fatalf("失败后仍应继续响应后续变化：%v", got)
	}

	r2 := newShellRigWith(t, settings.CloseAsk, false, func(r *shellRig) {
		r.shell.manageLoginItem = true
		r.lc.setLoginItem = nil
	})
	setLaunchAtStartup(t, r2, true) // 不 panic
}

// B-027：关闭请求时先落盘窗口状态，即使这次关闭被拦下（隐藏到托盘）。
func TestVivaShellCloseRequestFlushesWindowState(t *testing.T) {
	file := filepath.Join(t.TempDir(), "window-state.json")
	p := newVivaWindowStatePersister(file, nil)
	p.debounce = time.Hour // 只有 flush 才会写
	p.onFrame(gfx.Rect{X: 60, Y: 70, Width: 1100, Height: 760})

	r := newShellRigWith(t, settings.CloseMinimize, false, func(r *shellRig) { r.shell.windowState = p })
	if !r.shell.OnCloseRequest() {
		t.Fatal("关闭行为是最小化：这次关闭应被接管（隐藏到托盘）")
	}
	if st := readStateFile(t, file); st.Bounds != (windowBounds{X: 60, Y: 70, Width: 1100, Height: 760}) {
		t.Fatalf("被拦下的关闭也应先保存窗口状态：%+v", st)
	}
}

// 没有窗口状态记录器（自动化实例）时关闭请求照常工作。
func TestVivaShellCloseRequestWithoutWindowState(t *testing.T) {
	r := newShellRig(t, settings.CloseExit, false)
	if r.shell.OnCloseRequest() {
		t.Fatal("关闭行为是退出：应放行")
	}
}
