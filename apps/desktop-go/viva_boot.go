package main

// Viva 原生界面启动路径（-viva，长期分支 feat/viva-ui）。
//
// 与 React 版、-native 共用 main() 里到处理器登记为止的全部初始化（数据库、Library、IPC 处理器、后台服务），
// 之后不建 WebView、不绑定 Bridge、不起 MyGo 窗口，而是开 viva/app 的 Viva 窗口。
// 界面经 native/services 的窄接口取数（适配器见 native_adapters.go），空闲时进程里没有任何 WebView；
// 阅读视图、Wiki 图谱、mermaid、媒体按需开「岛」（viva/island，W1）。
//
// 切换到产品默认之前，-viva 只允许与 -bench 同用，且必须显式给 -db 与 -userdata：
// 它还不是完整的产品界面，不应在真实数据目录上运行（见 docs/agents/viva-ui-development.md §7）。

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

// vivaWindowClass 是 Viva 窗口的类名。Viva 把 WebView2 的 profile 放在 os.UserCacheDir()\<Class>\WebView2，
// 所以必须是专用名：用 "GuiZhi" 会指向用户真实安装的 WebView2 配置。切换为产品默认时再决定正式名称。
const vivaWindowClass = "guizhi-viva"

// vivaFlags 是 -viva 的命令行开关；必须在 flag.Parse 之前声明。
type vivaFlags struct {
	on     *bool
	width  *int
	height *int
	module *string
	theme  *string
	hue    *string
	font   *string
}

func declareVivaFlags() vivaFlags {
	return vivaFlags{
		on:     flag.Bool("viva", false, "Viva 原生界面（feat/viva-ui 开发中）：不加载 WebView 与渲染进程；必须与 -bench、-db、-userdata 同用"),
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

// checkLaunch 检查 -viva 的启动条件。它必须在解析数据目录、初始化数据库之前调用：
// 不带 -db 时 main() 会按正式安装的规则推断出真实的数据目录并对它做初始化（租约、迁移、升级前备份），
// 所以条件不满足时要在碰任何数据之前就退出，而不是等到 runViva 里才拒绝。
func checkVivaLaunch(nativeOn, bench, dbGiven, userDataGiven bool) error {
	switch {
	case nativeOn:
		return errors.New("-native 与 -viva 不能同时使用")
	case !bench || !dbGiven || !userDataGiven:
		return errors.New("-viva 目前只能与 -bench、-db、-userdata 同用：它还不是完整的产品界面，不应在真实数据目录上运行")
	}
	return nil
}

// checkLaunch 用命令行的实际给定情况调用 checkVivaLaunch。
func (f vivaFlags) checkLaunch(nativeOn, bench bool) error {
	return checkVivaLaunch(nativeOn, bench, flagGiven("db"), flagGiven("userdata"))
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
	automation bool // 截图/基准实例：不占单实例锁、不建托盘、不抢系统快捷键
	bench      bool // 基准实例：首屏声明后在标准输出打印 READY，之后一直运行直到被终止
}

// runViva 打开 Viva 窗口并阻塞到退出。
func runViva(vb vivaBoot) error {
	if err := vb.flags.checkLaunch(false, vb.bench); err != nil { // main() 已检查过一次；这里再守一次，防止别的调用方绕过
		return err
	}
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
	hub := newPushHub()
	vb.lib.setPush(hub.Dispatch) // 单监听推送 → 订阅者
	svc := newNativeServices(vb.bridge, hub)
	svc.Settings, svc.Prefs = nset, store

	var ready atomic.Bool
	if vb.bench {
		go announceReady(ready.Load)
	}
	err = vivaapp.Run(vivaapp.Options{
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
	})
	if err != nil {
		return fmt.Errorf("Viva 窗口退出：%w", err)
	}
	return nil
}
