package main

import (
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/gfx"

	vivaapp "guizhi-mygo-shell/viva/app"
)

// 自动化隔离护栏：只有真实实例（非自动化）且没有显式 -userdata / -db 才管理系统登录项。
// -bench 或带 -userdata/-db 的临时测试运行绝不能改写 HKCU 的 Run 键。
func TestVivaLoginManagedGuard(t *testing.T) {
	cases := []struct {
		name                          string
		automation, userData, dbGiven bool
		want                          bool
	}{
		{"真实产品启动", false, false, false, true},
		{"自动化（-bench / -shot）", true, false, false, false},
		{"自动化 + 隔离目录（对照、基准）", true, true, true, false},
		{"手工试跑：显式 -userdata", false, true, false, false},
		{"手工试跑：显式 -db", false, false, true, false},
	}
	for _, c := range cases {
		if got := vivaLoginManaged(c.automation, c.userData, c.dbGiven); got != c.want {
			t.Errorf("%s：got %v want %v", c.name, got, c.want)
		}
	}
}

// 无论是否管理，main() 里给的 mygo 默认实现都必须被换掉：不管理时置空（不写注册表），管理时换成 Win32 实现。
func TestApplyVivaLoginPolicyAlwaysReplacesMygoDefault(t *testing.T) {
	sentinel := func(bool) error { t.Error("mygo 默认实现不该再被调用"); return nil }

	lc := newAppLifecycle(true)
	lc.setLoginItem = sentinel
	applyVivaLoginPolicy(lc, false)
	if lc.setLoginItem != nil {
		t.Fatal("不管理时登录项写入必须置空（app:setAutoLaunch 与设置订阅都不碰注册表）")
	}

	lc.setLoginItem = sentinel
	applyVivaLoginPolicy(lc, true)
	if lc.setLoginItem == nil {
		t.Fatal("管理时应接 Viva 实现")
	}
	// 不调用 setLoginItem：它指向真实的 HKCU Run 键，测试里只验证接线
}

func TestWireVivaRealShell(t *testing.T) {
	ud := t.TempDir()
	login := []string{"--mygo-opened-at-login"}
	remembered := windowLaunchState{Bounds: windowBounds{X: 50, Y: 40, Width: 1300, Height: 850}, HasPosition: true}

	newShell := func() *vivaShell { return &vivaShell{} }
	opts := func() vivaapp.Options { return vivaapp.Options{Width: 1280, Height: 800} }

	// 登录项拉起 + 设置了启动时隐藏 + 有窗口记忆
	vs, o := newShell(), opts()
	p := wireVivaRealShell(vs, &o, vivaRealShellInput{userData: ud, manageLogin: true, packaged: true, minimizeOnLaunch: true, osArgs: login, launch: remembered})
	if !vs.startHidden || !vs.manageLoginItem || !vs.packaged || vs.windowState != p || p == nil {
		t.Errorf("外壳装配：%+v", vs)
	}
	if o.Width != 1300 || o.Height != 850 || o.Origin == nil || *o.Origin != (gfx.Point{X: 50, Y: 40}) {
		t.Errorf("窗口几何应取记忆：%+v", o)
	}
	if o.OnFrameChange == nil || o.OnMaximize == nil || o.OnFullscreen == nil {
		t.Error("窗口状态回调应接到选项上")
	}
	if want := filepath.Join(ud, "window-state.json"); p.file != want {
		t.Errorf("状态文件 = %s，期望 %s", p.file, want)
	}
	if !p.haveNormal || p.lastNormal != remembered.Bounds {
		t.Errorf("有记忆位置时应预填普通边界：%+v", p)
	}

	// 手动启动（没有登录项参数）：不隐藏；不管理登录项；显式大小按命令行；默认窗口不预填
	vs, o = newShell(), opts()
	p = wireVivaRealShell(vs, &o, vivaRealShellInput{userData: ud, minimizeOnLaunch: true, launch: windowLaunchState{Bounds: windowBounds{Width: 1200, Height: 800}}, sizeGiven: true})
	if vs.startHidden || vs.manageLoginItem {
		t.Errorf("手动启动不该隐藏、不该管理登录项：%+v", vs)
	}
	if o.Width != 1280 || o.Height != 800 || o.Origin != nil {
		t.Errorf("显式大小应按命令行：%+v", o)
	}
	if p.haveNormal {
		t.Error("默认窗口位置未知，不该预填普通边界")
	}

	// 登录项拉起但没设置「启动时最小化」：正常显示
	vs, o = newShell(), opts()
	wireVivaRealShell(vs, &o, vivaRealShellInput{userData: ud, osArgs: login, launch: remembered})
	if vs.startHidden {
		t.Error("没设置启动时最小化，登录项拉起也应显示窗口")
	}
}
