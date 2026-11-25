package main

// Viva 真实产品实例（非自动化）才接的外壳能力：登录项、启动即隐藏、窗口位置与大小记忆。
// 自动化实例（-bench / -shot）与带 -userdata / -db 的临时运行不碰系统登录项，也不读写真实窗口状态。

import (
	"path/filepath"

	vivaapp "guizhi-mygo-shell/viva/app"
)

// vivaWindowStateFile 是窗口状态文件名：userData 下，与 Electron 版、React 版同名同格式，新旧外壳互读。
const vivaWindowStateFile = "window-state.json"

// vivaLoginManaged 报告是否由本实例管理系统登录项：只有真实实例（非自动化）且没有显式 -userdata / -db 才管理——
// 带这些参数是开发者或验证脚本的临时运行，开机自启写的是整台机器的注册表，绝不能被它改写。
func vivaLoginManaged(automation, userDataGiven, dbGiven bool) bool {
	return !automation && !userDataGiven && !dbGiven
}

// applyVivaLoginPolicy 决定生命周期策略里的登录项写入函数：管理时接 Win32 注册表实现；否则置空，
// 使 app:setAutoLaunch 与设置订阅都不会触碰注册表。必须无条件调用——main() 里给的默认值是依赖 mygo 事件循环的实现。
func applyVivaLoginPolicy(lc *appLifecycle, manage bool) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.setLoginItem = nil
	if manage {
		lc.setLoginItem = setVivaLoginItem
	}
}

// vivaRealShellInput 是装配真实外壳需要的外部事实（由 runViva 从系统与设置读出；测试直接构造）。
type vivaRealShellInput struct {
	userData         string
	manageLogin      bool
	packaged         bool
	minimizeOnLaunch bool
	osArgs           []string          // 操作系统原始命令行（含登录项参数）
	launch           windowLaunchState // 已按当前显示器校正的窗口记忆
	sizeGiven        bool              // 命令行显式给了 -viva-width / -viva-height
}

// realShellInput 从系统与设置读出装配真实外壳需要的事实：原始命令行、「启动时最小化」设置、窗口记忆（按当前显示器校正）。
func (vb vivaBoot) realShellInput(nset *nativeSettings, manageLogin bool) vivaRealShellInput {
	ud := vb.lib.paths.UserData
	minimize := false
	if v, err := nset.Snapshot(); err != nil {
		logAppErrorf("shell", "读取启动可见性设置", "%s", err.Error())
	} else {
		minimize = v.App.MinimizeOnLaunch
	}
	return vivaRealShellInput{
		userData: ud, manageLogin: manageLogin, packaged: packagedBuild == "true", minimizeOnLaunch: minimize,
		osArgs:    osCommandLineArgs(),
		launch:    readWindowLaunchState(filepath.Join(ud, vivaWindowStateFile), vivaDisplayWorkAreas()),
		sizeGiven: flagGiven("viva-width") || flagGiven("viva-height"),
	}
}

// wireVivaRealShell 把真实实例的登录项、启动可见性与窗口记忆接到外壳与窗口选项上，返回窗口状态记录器（退出后 flush）。
func wireVivaRealShell(vs *vivaShell, opts *vivaapp.Options, in vivaRealShellInput) *vivaWindowStatePersister {
	vs.manageLoginItem, vs.packaged = in.manageLogin, in.packaged
	vs.startHidden = vivaLaunchVisibility(in.osArgs, in.minimizeOnLaunch, in.launch.ShouldMaximize).StartHidden

	geo := planVivaWindow(in.launch, in.sizeGiven, opts.Width, opts.Height)
	opts.Width, opts.Height, opts.Origin = geo.Width, geo.Height, geo.Origin
	var initial *windowBounds // 只有记忆里有位置时才预填：默认窗口的位置是系统摆放的，不知道，不能写 (0,0)
	if in.launch.HasPosition {
		b := in.launch.Bounds
		initial = &b
	}
	persist := newVivaWindowStatePersister(filepath.Join(in.userData, vivaWindowStateFile), initial)
	persist.bind(opts)
	vs.windowState = persist
	return persist
}
