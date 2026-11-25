package main

// Viva 原生界面启动路径（-viva，长期分支 feat/viva-ui）。
//
// 与 React 版、-native 共用 main() 里到处理器登记为止的全部初始化（数据库、Library、IPC 处理器、后台服务），
// 之后不建 WebView、不绑定 Bridge、不起 MyGo 窗口，而是开 viva/app 的 Viva 窗口。
// 界面经 native/services 的窄接口取数（适配器见 native_adapters.go），空闲时进程里没有任何 WebView；
// 阅读视图、Wiki 图谱、mermaid、媒体按需开「岛」（viva/island，W1）。
//
// Viva 是产品默认外壳（-viva 缺省为开）：不带 -viva 的正常启动就是它；传 -viva=false 回退旧 WebView2 渲染层（历史对照、排障）。
// 显式 -native（原生界面开发路径）与显式 -shot（网页截图）仍走各自的旧路径，见 vivaFlags.active。
//
// 外壳生命周期（viva_shell.go：关闭行为、托盘、按数据目录隔离的单实例、系统级快捷键、登录项、窗口状态记忆）：
// 真实产品启动默认接上；自动化实例（-bench / -shot）默认不接——不带 -viva-shell 时窗口一关就是退出，不占单实例锁、不抢快捷键，
// 截图、对照与基准实例都是这样；显式传 -viva-shell 才在自动化实例里验证外壳（smoke-viva-shell.ps1）。
// 登录项与窗口状态文件只在真实实例里读写，带 -userdata / -db 的临时运行不碰系统注册表。

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"

	vivaapp "guizhi-mygo-shell/viva/app"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/screens/shell"
	"guizhi-mygo-shell/viva/tokens"
)

// vivaWindowClass 是 Viva 窗口的类名，也是正式产品名。Viva 的阅读岛、媒体岛把临时 WebView2 profile 放在
// %LOCALAPPDATA%\guizhi-viva\WebView2（os.UserCacheDir()\<Class>\WebView2），与旧版 React 壳的 %LOCALAPPDATA%\GuiZhi\WebView2 物理隔离：
// 既保护首次启动只读导入旧 localStorage（viva_legacy_import.go），也支持安全回退到旧版。
// 绝不能改成 "GuiZhi"：那会指向旧版渲染层的 WebView2 配置，岛的临时目录清理会碰到它。
const vivaWindowClass = "guizhi-viva"

// vivaFlags 是 -viva 的命令行开关；必须在 flag.Parse 之前声明。
type vivaFlags struct {
	on     *bool
	shell  *bool
	width  *int
	height *int
	module *string
	theme  *string
	hue    *string
	font   *string
}

func declareVivaFlags() vivaFlags {
	return vivaFlags{
		on:     flag.Bool("viva", true, "Viva 原生界面（产品默认）：不加载 WebView 与渲染进程；传 -viva=false 回退旧 WebView2 渲染层（历史对照、排障）"),
		shell:  flag.Bool("viva-shell", false, "Viva 外壳生命周期（关闭行为、托盘、按数据目录隔离的单实例）：真实启动默认开启（-viva-shell=false 关闭）；自动化实例（-bench/-shot）默认不接，传它才验证外壳（不抢用户的组合键、不改登录项）"),
		width:  flag.Int("viva-width", 1280, "Viva 窗口宽度（逻辑像素）"),
		height: flag.Int("viva-height", 800, "Viva 窗口高度（逻辑像素）"),
		module: flag.String("viva-module", "library", "Viva 界面的起始页：library|inbox|ask|wiki|imports|settings"),
		theme:  flag.String("viva-theme", "", "覆盖外观的明暗：system|light|dark（只供开发与截图；缺省取设置）"),
		hue:    flag.String("viva-hue", "", "覆盖外观的主题色：预设 ID（royal-blue、blue、purple、green、orange、teal）或「色相,饱和度」"),
		font:   flag.String("viva-font", "", "覆盖外观的字号档：small|medium|large"),
	}
}

// enabled 报告是否以 Viva 原生界面启动。
func (f vivaFlags) enabled() bool { return f.on != nil && *f.on }

// shellEnabled 报告是否显式给了 -viva-shell：自动化实例里只有显式给了才接外壳生命周期。
func (f vivaFlags) shellEnabled() bool { return f.shell != nil && *f.shell }

// active 报告本次启动是否走 Viva 路径。Viva 是产品默认（-viva 缺省为开），两条旧路径的显式开关优先：
// 显式 -native 走原生界面开发路径（pnpm shot:native），显式 -shot <文件> 走网页截图（旧 WebView2 渲染层）；
// 其余情况取 -viva 的值（显式 -viva=false 回退旧渲染层）。
func (f vivaFlags) active(nativeOn, hasShot bool) bool {
	if nativeOn || hasShot {
		return false
	}
	return f.enabled()
}

// checkVivaLaunch 检查命令行开关的组合：显式 -viva 与另外两条路径互斥。它必须在解析数据目录、初始化数据库之前调用，
// 冲突时在碰任何数据之前就退出。vivaOn 是 -viva 的取值，vivaExplicit 表示命令行是否显式给了 -viva。
func checkVivaLaunch(vivaOn, vivaExplicit, nativeOn, hasShot bool) error {
	switch {
	case nativeOn && vivaExplicit && vivaOn:
		return errors.New("-native 与 -viva 不能同时使用")
	case hasShot && vivaExplicit && vivaOn:
		return errors.New("-viva 不使用 -shot 网页截图参数（无头截图请用 vivaparity）")
	}
	return nil
}

// shellActive 报告是否接外壳生命周期（关闭行为、托盘、按数据目录隔离的单实例、系统级快捷键、登录项、窗口状态）。
// 自动化实例（-bench / -shot）只有显式 -viva-shell 才接，保持 measure-idle.ps1 与 smoke-viva-shell.ps1 的语义不变；
// 真实产品启动默认接，仅显式 -viva-shell=false 时关闭。
func (f vivaFlags) shellActive(automation bool) bool {
	return f.shellActiveGiven(automation, flagGiven("viva-shell"))
}

// shellActiveGiven 是 shellActive 的纯逻辑：shellGiven 表示命令行是否显式给了 -viva-shell。
func (f vivaFlags) shellActiveGiven(automation, shellGiven bool) bool {
	if automation {
		return f.shellEnabled()
	}
	return !shellGiven || f.shellEnabled()
}

// flagGiven 报告命令行是否显式给了某个开关。
func flagGiven(name string) bool {
	given := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			given = true
		}
	})
	return given
}

// searchShortcutLabel 是顶栏搜索框右侧的快捷键提示：读主进程实际生效的「搜索」组合键，
// 按渲染层 useShortcutLabel 在 Windows 上的规则格式化（CommandOrControl、Control 都显示 Ctrl）；读不到取默认值。
func searchShortcutLabel(lib *Library) string { return shortcutLabel(lib, "search") }

// shortcutLabel 是某个快捷键动作（showApp、newItem、search、settings）当前生效组合键的显示文案。
func shortcutLabel(lib *Library, action string) string {
	combo := defaultShortcutAccelerators[action]
	if m, ok := lib.currentShortcuts().(*omap); ok {
		if v, ok := m.vals[action].(string); ok && v != "" {
			combo = v
		}
	}
	return formatShortcutLabel(combo)
}

// formatShortcutLabel 按渲染层 useShortcutLabel 在 Windows 上的规则显示组合键：CommandOrControl、Control 都写成 Ctrl。
func formatShortcutLabel(combo string) string {
	combo = strings.Replace(combo, "CommandOrControl", "Ctrl", 1)
	return strings.Replace(combo, "Control", "Ctrl", 1)
}

// startPage 把 -viva-module 换成起始模块与页面；写错时返回带可选值的错误，而不是静默落回默认。
func (f vivaFlags) startPage() (shell.Module, shell.Page, error) {
	name := strings.TrimSpace(*f.module)
	if name == "settings" {
		return shell.ModuleLibrary, shell.PageSettings, nil
	}
	for _, m := range []shell.Module{shell.ModuleLibrary, shell.ModuleInbox, shell.ModuleAsk, shell.ModuleWiki, shell.ModuleImports} {
		if string(m) == name {
			return m, shell.PageHome, nil
		}
	}
	return "", shell.PageHome, fmt.Errorf("未知的 -viva-module %q（可选：library|inbox|ask|wiki|imports|settings）", name)
}

// appearanceOverride 把外观开关做成对 kit.Spec 的覆盖；都没给返回 nil。写错时返回带可选值的错误。
func (f vivaFlags) appearanceOverride() (func(kit.Spec) kit.Spec, error) {
	if *f.theme == "" && *f.hue == "" && *f.font == "" {
		return nil, nil
	}
	var (
		mode     tokens.Mode
		hue, sat float64
		setHue   bool
		font     string
	)
	switch tokens.Mode(*f.theme) {
	case "":
	case tokens.ModeLight, tokens.ModeDark, tokens.ModeSystem:
		mode = tokens.Mode(*f.theme)
	default:
		return nil, fmt.Errorf("未知的 -viva-theme %q（可选：system|light|dark）", *f.theme)
	}
	if v := *f.hue; v != "" {
		matched := false
		for _, p := range tokens.HuePresets {
			if p.ID == v {
				hue, sat, setHue, matched = p.Hue, p.Saturation, true, true
			}
		}
		if !matched {
			h, s, ok := strings.Cut(v, ",")
			hv, errH := strconv.ParseFloat(strings.TrimSpace(h), 64)
			sv, errS := strconv.ParseFloat(strings.TrimSpace(s), 64)
			if !ok || errH != nil || errS != nil {
				return nil, fmt.Errorf("未知的 -viva-hue %q（预设 ID 或「色相,饱和度」）", v)
			}
			hue, sat, setHue = hv, sv, true
		}
	}
	if v := *f.font; v != "" {
		matched := false
		for _, p := range tokens.FontSizes {
			matched = matched || p.ID == v
		}
		if !matched {
			return nil, fmt.Errorf("未知的 -viva-font %q（可选：small|medium|large）", v)
		}
		font = v
	}
	return func(spec kit.Spec) kit.Spec {
		if mode != "" {
			spec.Mode = mode
		}
		if setHue {
			spec.Hue, spec.Saturation = hue, sat
		}
		if font != "" {
			spec.FontSize = font
		}
		return spec
	}, nil
}

// vivaBoot 是 runViva 需要的运行期依赖。
type vivaBoot struct {
	flags      vivaFlags
	lib        *Library
	bridge     *Bridge
	lc         *appLifecycle // 生命周期策略（main 里已登记 IPC）；外壳接线用
	instance   *vivaInstance // 单实例锁（-viva-shell；可为空）
	automation bool          // 截图/基准实例：不占 mygo 的单实例锁、不抢系统快捷键
	bench      bool          // 基准实例：首屏声明后在标准输出打印 READY，之后一直运行直到被终止
}

// runViva 打开 Viva 窗口并阻塞到退出。
func runViva(vb vivaBoot) error {
	// 平台能力换成 Viva 实现：这条路径下 mygo 的事件循环不运行，对话框、Shell、登录项用 mygo 会 panic 或永久等待。
	// 必须在任何对话框、链接请求之前，所以放在最前面。
	wireVivaPlatform(vb.lib)
	module, page, err := vb.flags.startPage()
	if err != nil {
		return err
	}
	appearance, err := vb.flags.appearanceOverride()
	if err != nil {
		return err
	}

	// 设置与偏好：界面设置在 config/ui-prefs.json，Go 侧设置在 settings 表（见 native_settings.go）。
	// 自动化实例的 userdata 是一次性目录，不读用户的旧 WebView2 配置，也不会碰真实数据。
	store := openUIPrefsStore(filepath.Join(vb.lib.paths.ConfigDir(), uiPrefsFileName), log.Printf)
	nset := newNativeSettings(vb.lib, store, log.Printf)
	if !vb.automation { // 真实实例：首次启动导入旧版 React 设置（只读复制，已初始化时幂等跳过，失败只记日志）
		vb.importLegacySettings(nset)
	}
	hub := newPushHub()
	vb.lib.setPush(hub.Dispatch) // 单监听推送 → 订阅者
	svc := newNativeServices(vb.bridge, hub)
	svc.Settings, svc.Prefs = nset, store
	svc.ConfigTransfer = newConfigAdapter(vb.bridge, vb.lib, nset, store)
	// 自动完整备份的调度跟着设置走（渲染层靠 syncRendererSettings 推送，原生界面靠设置订阅）。
	stopBackupWatch := watchBackupSchedule(nset, vb.lib)
	defer stopBackupWatch()

	var ready atomic.Bool
	if vb.bench {
		go announceReady(ready.Load)
	}
	// 登录项写入：main() 给的默认实现依赖 mygo 事件循环，这里无条件换掉；只有真实实例且没有显式 -userdata/-db 才接注册表（B-024）。
	manageLogin := vivaLoginManaged(vb.automation, flagGiven("userdata"), flagGiven("db"))
	applyVivaLoginPolicy(vb.lc, manageLogin)

	// 外壳生命周期：真实启动默认接；自动化实例只在显式 -viva-shell 时接（没有外壳时窗口一关就是退出，不能被关闭对话框拦住）。
	opts := vivaapp.Options{
		Class: vivaWindowClass, Title: "归知 GuiZhi",
		Width: *vb.flags.width, Height: *vb.flags.height,
		MinWidth: minWindowWidth, MinHeight: minWindowHeight,
		OnReady:  func() { ready.Store(true) },
		Services: svc, Module: module, Page: page,
		SearchShortcut: searchShortcutLabel(vb.lib),
		// 采集空队列引导的提示文案取「新建」快捷键当前的组合键；取消应用生命周期时各模块的后台请求随之停止。
		CaptureShortcut: shortcutLabel(vb.lib, "newItem"),
		AppVersion:      appVersion,
		Platform:        nodePlatform(),
		Appearance:      appearance,
		ReaderMedia:     readerMedia(vb.lib.paths),
	}
	var vs *vivaShell
	var persist *vivaWindowStatePersister
	if vb.flags.shellActive(vb.automation) {
		vs = &vivaShell{lc: vb.lc, lib: vb.lib, hub: hub, settings: nset, systemShortcuts: !vb.automation, instance: vb.instance}
		opts.Shell = vs
		// 自动检查更新、旧库迁移确认与设置清单只在用户真正在用的外壳实例里弹出：自动化、截图与基准实例用真实服务，但不能被对话框盖住。
		opts.AutoOverlays = !vb.automation
		if !vb.automation {
			persist = wireVivaRealShell(vs, &opts, vb.realShellInput(nset, manageLogin))
		}
	}
	err = vivaapp.Run(opts)
	persist.flush() // 事件循环已停：窗口位置与大小同步落盘（关闭请求时已落过一次，这里兜底系统关机等无关闭请求的退出）
	if vs != nil {
		vs.Shutdown() // 摘托盘图标、撤销快捷键与订阅、释放单实例锁
	}
	if err != nil {
		return fmt.Errorf("Viva 窗口退出：%w", err)
	}
	return nil
}
