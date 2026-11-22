package scenes

import (
	"slices"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/parity"
	settingsscreen "guizhi-mygo-shell/viva/screens/settings"
	shellscreen "guizhi-mygo-shell/viva/screens/shell"
)

// 设置页三个外围分区（M3-2b：采集与转写、MCP 设置、AI 模型服务）的对照场景，与 React 采集器
// （scripts/parity/scenes/react-settings-advanced-scenes.mjs、lib/react-settings-advanced.mjs）一一对应：
// 同一份夹具（viva/parity/testdata/settings-advanced-fixture.json）、同样的操作。
//
// React 一侧页内替换只读接口（yt-dlp / ffmpeg / 转写引擎探测、平台账号、MCP 配置与范围、用量），端点与模型经真实 settings.set 写进一次性 userData；
// Viva 一侧用假服务装配同样的数据。内容区场景只比设置内容区，对话框场景比整窗（遮罩与面板压在 rail 与顶栏之上）。

// settingsAdvancedFixture 取夹具；它是编译进来的常量，解码失败是程序错误。
func settingsAdvancedFixture() parity.SettingsAdvancedFixture {
	fix, err := parity.LoadSettingsAdvancedFixture()
	if err != nil {
		panic(err)
	}
	return fix
}

// advancedServices 是一次渲染里三个分区用的假服务：每次渲染各新建一份，不跨渲染共享。
// withAI 为假时模型服务是空配置（零值假服务：没有端点、没有模型、用量为零），对应 React 一侧「空配置」的会话状态。
type advancedServices struct {
	capture *servicestest.Capture
	solver  *servicestest.Solver
	mcp     *servicestest.MCPFake
	ai      *servicestest.AISettingsFake
}

func newAdvancedServices(fix parity.SettingsAdvancedFixture, withAI bool) advancedServices {
	s := advancedServices{
		capture: &servicestest.Capture{}, solver: &servicestest.Solver{}, mcp: &servicestest.MCPFake{}, ai: &servicestest.AISettingsFake{},
	}
	s.capture.SetToolStatus(services.ToolYtDlp, fix.Tools["ytdlp"])
	s.capture.SetToolStatus(services.ToolFfmpeg, fix.Tools["ffmpeg"])
	s.capture.SetFunasrStatus(fix.Funasr)
	s.capture.SetAccounts(fix.Accounts)
	s.mcp.SetConfig(fix.MCP.Config)
	s.mcp.PutScope(fix.MCP.Scope)
	if withAI {
		s.ai.PutConfig(fix.AIConfig())
		s.ai.PutUsage(fix.AI.Usage)
	}
	return s
}

// advancedShell 是整窗里设置页的壳：Active 指定分区，三个分区的服务来自 svc。
func advancedShell(look *kit.Look, active settingsscreen.SectionID, fix parity.SettingsAdvancedFixture, svc advancedServices) *shellscreen.Window {
	return &shellscreen.Window{
		Look: look, Page: shellscreen.PageSettings, OwnControls: true, SearchShortcut: "Alt+Shift+F", CaptureShortcut: "Alt+Shift+N",
		Settings: &settingsscreen.Shell{
			Look: look, Store: settingsscreen.NewMemStore(settings.Default()), Active: active,
			CaptureTools: svc.capture, Solver: svc.solver, MCP: svc.mcp, AI: svc.ai, Platform: fix.Platform,
		},
	}
}

// captureLoaded 是「采集与转写」页的后台读取（两个引擎、转写引擎、平台账号）都已上屏的判定：
// 版本号（yt-dlp 以 年-月-日 显示）、转写引擎的 v 版本、平台账号描述里的浏览器版本。
func captureLoaded(s *headless.Session, fix parity.SettingsAdvancedFixture) bool {
	ytdlp := fix.Tools["ytdlp"].Version
	return snapshotHasText(s, ytdlpDisplayVersion(ytdlp)) && snapshotHasText(s, "v"+fix.Funasr.Version) &&
		snapshotHasText(s, fix.Accounts[0].BrowserVersion)
}

// ytdlpDisplayVersion 是 yt-dlp 稳定版版本串的显示写法：2026.07.15 → 2026-07-15（渲染层 formatEngineVersion 的同一规则）。
func ytdlpDisplayVersion(v string) string {
	b := []byte(v)
	for i := range b {
		if b[i] == '.' {
			b[i] = '-'
		}
	}
	return string(b)
}

// captureScene 是「采集与转写」页的内容区场景。
func captureScene(id string, waivers ...parity.Waiver) parity.Scene {
	prepare := func(look *kit.Look) (func(c ui.C), func(s *headless.Session) error) {
		fix := settingsAdvancedFixture()
		svc := newAdvancedServices(fix, false)
		p := &settingsscreen.Capture{
			Look: look, Store: settingsscreen.NewMemStore(settings.Default()), Service: svc.capture, Solver: svc.solver, Platform: fix.Platform,
		}
		return p.Page, func(s *headless.Session) error {
			if err := waitCond(s, "采集与转写读取完成", func() bool { return captureLoaded(s, fix) }); err != nil {
				return err
			}
			if err := settleData(s); err != nil {
				return err
			}
			// 滚到底：页面的最后一块（Cloudflare 后备采集）的下拉在顶视图里正好压在 800 高的内容区下缘，只露出 8px，
			// 渲染层里它的文字落在裁剪线之外、读不到触发器的当前值（对照把按钮里可见的文字当作值），Viva 一侧的值照常给出，L1 无法对齐。
			return scrollToEnd(s)
		}
	}
	return parity.Scene{
		ID: id,
		Build: func(look *kit.Look) func(c ui.C) {
			view, _ := prepare(look)
			return view
		},
		Prepare: prepare,
		Waivers: waivers,
	}
}

// mcpScene 是「MCP 设置」页的内容区场景：范围读到「未归合集」一行、服务状态读到「组件就绪」。
func mcpScene(id string, waivers ...parity.Waiver) parity.Scene {
	prepare := func(look *kit.Look) (func(c ui.C), func(s *headless.Session) error) {
		fix := settingsAdvancedFixture()
		svc := newAdvancedServices(fix, false)
		m := &settingsscreen.MCP{Look: look, Service: svc.mcp}
		return m.Page, func(s *headless.Session) error {
			if err := waitCond(s, "MCP 设置读取完成", func() bool { return snapshotHasText(s, locales.SettingsMcpStatusReady()) }); err != nil {
				return err
			}
			return settleData(s)
		}
	}
	return parity.Scene{
		ID: id,
		Build: func(look *kit.Look) func(c ui.C) {
			view, _ := prepare(look)
			return view
		},
		Prepare: prepare,
		Waivers: waivers,
	}
}

// aiLoaded 是「AI 模型服务」页的后台读取（配置、用量）都已上屏的判定：
// 有数据时是端点名与用量块的分组钮，空配置时是「还没有添加任何模型」说明（用量块为零也有一行说明，随同一批读取到齐）。
func aiLoaded(s *headless.Session, fix parity.SettingsAdvancedFixture, withAI bool) bool {
	if !withAI {
		// 说明文字在窄侧栏里折成多行，画面上的文字是逐行的：只认开头几个字。
		return snapshotHasText(s, string([]rune(locales.SettingsAiWorkbenchNoModels())[:6])) && snapshotHasText(s, locales.SettingsAiUsageEmpty(30))
	}
	for _, p := range fix.AI.Providers {
		if !snapshotHasText(s, p.Name) {
			return false
		}
	}
	return snapshotHasText(s, locales.SettingsAiUsageGroupScenario())
}

// waitAILoaded 等 AI 页读取到齐，再多出几帧收尾。
func waitAILoaded(s *headless.Session, fix parity.SettingsAdvancedFixture, withAI bool) error {
	if err := waitCond(s, "AI 模型服务读取完成", func() bool { return aiLoaded(s, fix, withAI) }); err != nil {
		return err
	}
	return settleData(s)
}

// aiScene 是「AI 模型服务」页的内容区场景；withAI 为假是无端点空态。
func aiScene(id string, withAI bool, waivers ...parity.Waiver) parity.Scene {
	prepare := func(look *kit.Look) (func(c ui.C), func(s *headless.Session) error) {
		fix := settingsAdvancedFixture()
		svc := newAdvancedServices(fix, withAI)
		a := &settingsscreen.AI{Look: look, Service: svc.ai}
		return a.Page, func(s *headless.Session) error { return waitAILoaded(s, fix, withAI) }
	}
	return parity.Scene{
		ID: id,
		Build: func(look *kit.Look) func(c ui.C) {
			view, _ := prepare(look)
			return view
		},
		Prepare: prepare,
		Waivers: waivers,
	}
}

// aiDialogScene 是整窗（顶栏 + rail + AI 页）上弹着对话框的场景：等 AI 页读取到齐，点名为 opener 的按钮，再等标题为 title 的对话框出现。
// 打开之前的页面（遮罩背后的模糊，台账 A-007）走 Build，由 BackdropSpec 的步骤补齐「读取到齐」。
func aiDialogScene(id, opener, title string, waivers ...parity.Waiver) parity.Scene {
	prepare := func(look *kit.Look) (func(c ui.C), func(s *headless.Session) error) {
		fix := settingsAdvancedFixture()
		svc := newAdvancedServices(fix, true)
		w := advancedShell(look, settingsscreen.SectionAI, fix, svc)
		return w.View, func(s *headless.Session) error {
			if err := waitAILoaded(s, fix, true); err != nil {
				return err
			}
			if err := clickAndSettle(s, gfx.RoleButton, opener); err != nil {
				return err
			}
			if err := waitCond(s, "对话框打开："+title, func() bool { return snapshotHasText(s, title) }); err != nil {
				return err
			}
			return parity.ParkPointer(s)
		}
	}
	backdrop := modalBackdrop(title)
	backdrop.BaseSteps = func(s *headless.Session, _ *kit.Look) error {
		return waitAILoaded(s, settingsAdvancedFixture(), true)
	}
	return withBackdrop(parity.Scene{
		ID: id,
		Build: func(look *kit.Look) func(c ui.C) {
			view, _ := prepare(look)
			return view
		},
		Prepare: prepare,
		Waivers: slices.Concat(pageSettingsWaivers, []parity.Waiver{overlayInkWaiver}, modalTitleWaivers(title), waivers),
	}, backdrop)
}

func init() {
	registry["settings.capture"] = captureScene("settings.capture", captureWaivers()...)
	registry["settings.mcp"] = mcpScene("settings.mcp", mcpWaivers()...)
	registry["settings.ai"] = aiScene("settings.ai", true, aiWaivers()...)
	registry["settings.ai.empty"] = aiScene("settings.ai.empty", false, aiEmptyWaivers()...)
	registry["settings.ai.endpoint-dlg"] = aiDialogScene("settings.ai.endpoint-dlg", locales.SettingsAddProvider(), locales.SettingsAiWorkbenchAddProvider(), slices.Concat(aiWaivers(), endpointDialogWaivers())...)
	registry["settings.ai.wizard"] = aiDialogScene("settings.ai.wizard", locales.SettingsQuickSetupOpen(), locales.SettingsQuickSetupTitle(), slices.Concat(aiWaivers(), wizardWaivers())...)
}
