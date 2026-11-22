package scenes

import (
	"fmt"
	"slices"
	"strings"
	"time"

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

// 设置页四个核心分区（M3-2a：网络、快捷键、数据与备份、关于）的对照场景，与 React 采集器
// （scripts/parity/scenes/react-settings-core-scenes.mjs、lib/react-settings-core.mjs）一一对应：
// 同一份夹具（viva/parity/testdata/settings-core-fixture.json）、同样的操作。
//
// React 一侧在页内替换几个 IPC 频道的回复（app:getRuntimePaths、updater:version、shortcuts:get、backup:list、
// backupRepository:*），Viva 一侧用假服务装配同样的数据；内容区场景只比设置页的滚动区（与 settings.general 同口径），
// 对话框场景比整窗（模态的遮罩与面板压在 rail 与顶栏之上）。

// settingsCoreFixture 取夹具；它是编译进来的常量，解码失败是程序错误。
func settingsCoreFixture() parity.SettingsCoreFixture {
	fix, err := parity.LoadSettingsCoreFixture()
	if err != nil {
		panic(err)
	}
	return fix
}

// coreServices 是一次渲染里数据与备份、快捷键两个分区用的假服务：每次渲染各新建一份，不跨渲染共享。
type coreServices struct {
	data      *servicestest.Data
	backup    *servicestest.Backup
	transfer  *servicestest.ConfigTransfer
	shortcuts *servicestest.Shortcuts
}

func newCoreServices(fix parity.SettingsCoreFixture) coreServices {
	s := coreServices{
		data: &servicestest.Data{}, backup: &servicestest.Backup{}, transfer: &servicestest.ConfigTransfer{},
		shortcuts: &servicestest.Shortcuts{},
	}
	s.data.SetPaths(fix.RuntimePaths)
	s.backup.SetLegacy(fix.LegacyBackups)
	s.backup.SetSnapshots(fix.Repository.Snapshots)
	s.backup.SetStatus(fix.Repository.Status)
	s.shortcuts.Set(services.ShortcutConfig{Accelerators: fix.Shortcuts, Modes: services.DefaultShortcutModes()})
	return s
}

// snapshotHasText 报告画面上有没有含 sub 的文字（含裁剪线外的：后台读取的列表行在滚动区下面也算「已到」）。
func snapshotHasText(s *headless.Session, sub string) bool {
	for _, t := range s.Snapshot().Texts {
		if strings.Contains(t.Value, sub) {
			return true
		}
	}
	return false
}

// waitCond 逐帧等到条件成立：后台读取经邮箱回到界面线程，场景里没人替它请求出帧，所以每轮要自己出帧
// （理由同 waitTextPrefix）。最多约 2 秒，等不到是装配错误，如实报错而不是照着加载中的画面去对照。
func waitCond(s *headless.Session, what string, cond func() bool) error {
	for range 400 {
		if err := s.Frame(); err != nil {
			return err
		}
		if err := s.Rest(); err != nil {
			return err
		}
		if cond() {
			return nil
		}
		time.Sleep(5 * time.Millisecond)
	}
	return fmt.Errorf("等待超时：%s", what)
}

// clickAndSettle 点角色加名称对应的控件，再出几帧让点击的结果（下拉展开、对话框打开）上屏。
func clickAndSettle(s *headless.Session, role gfx.Role, name string) error {
	if err := parity.ClickRole(s, role, name); err != nil {
		return err
	}
	for range 3 {
		if err := s.Frame(); err != nil {
			return err
		}
	}
	return s.Rest()
}

// pickOption 是「点开名为 name 的下拉，再点名为 option 的选项」，对应渲染层的 pickOption。
func pickOption(s *headless.Session, name, option string) error {
	if err := clickAndSettle(s, gfx.RoleCombobox, name); err != nil {
		return err
	}
	return clickAndSettle(s, gfx.RoleOption, option)
}

// fillField 是「点进名为 name 的文本框、全选后输入 text」，对应渲染层的 fill（整体替换）。
func fillField(s *headless.Session, name, text string) error {
	if err := clickAndSettle(s, gfx.RoleTextField, name); err != nil {
		return err
	}
	if err := s.Keys("cmd+a"); err != nil {
		return err
	}
	if err := s.Type(text); err != nil {
		return err
	}
	for range 3 {
		if err := s.Frame(); err != nil {
			return err
		}
	}
	return nil
}

// networkScene 是「网络」页的内容区场景。
func networkScene(id string, steps func(s *headless.Session) error, waivers ...parity.Waiver) parity.Scene {
	return parity.Scene{
		ID: id,
		Build: func(look *kit.Look) func(c ui.C) {
			n := &settingsscreen.Network{Look: look, Store: settingsscreen.NewMemStore(settings.Default())}
			return n.Page
		},
		Steps:   steps,
		Waivers: waivers,
	}
}

// openManualProxy 是「选手动代理，再逐项填协议、主机、端口、账号、绕过列表」，最后把指针移开并清焦点。
// 对应渲染层的 openManualProxy；填的值取自夹具。口令框不填：原因见 SettingsCoreProxy。
func openManualProxy(s *headless.Session) error {
	p := settingsCoreFixture().Proxy
	if err := pickOption(s, locales.SettingsNetworkProxyMode(), locales.SettingsNetworkProxyModeManual()); err != nil {
		return err
	}
	if err := pickOption(s, locales.SettingsNetworkProxyProtocol(), strings.ToUpper(p.Protocol)); err != nil {
		return err
	}
	for _, f := range []struct{ name, text string }{
		{locales.SettingsNetworkProxyHost(), p.Host},
		{locales.SettingsNetworkProxyPort(), fmt.Sprint(p.Port)},
		{locales.SettingsNetworkProxyUsername(), p.Username},
		{locales.SettingsNetworkProxyBypass(), p.Bypass},
	} {
		if err := fillField(s, f.name, f.text); err != nil {
			return err
		}
	}
	return parity.ParkAndBlur(s)
}

// proxyHostKerningWaivers 登记 A-014（字偶距）在手动代理主机名上的延伸：Chromium 对拉丁字符做字偶距、Viva 不做，
// 输入框里的 proxy.example.com 比渲染层宽约 1.3px（中档，React 128.66、Viva 129.98），累积成墨迹的水平位移 +1。
// 只放过这一段文字的宽度差（≤2px）与横向墨迹错位（≤1px）；位置纵向、字重、颜色照常判定。
var proxyHostKerningWaivers = []parity.Waiver{
	{
		Ledger: "A-014", Layer: parity.LayerL2, Kind: "text-width", Subject: "proxy.example.com", MaxDelta: 2,
		Note: "字偶距：Chromium 对拉丁字符做字偶距、Viva 不做，输入框里的主机名整行偏宽约 1.3px",
	},
	{
		Ledger: "A-014", Layer: parity.LayerL2, Kind: "ink", Subject: "proxy.example.com", MaxDelta: 2,
		Note: "同上：整行宽度差（≤2px）累积成墨迹剖面的最佳对齐位移（中档 1 像素、大档 2 像素；只管横向）",
	},
}

// keyringKerningWaivers 登记 A-014（字偶距）在密钥环状态条上的延伸：条里「Windows DPAPI」是拉丁字符，
// Chromium 做字偶距、Viva 不做，整行比渲染层宽约 1.2px（中档，React 372.73、Viva 373.89）。只放过这一行的宽度差（≤2px）。
var keyringKerningWaivers = []parity.Waiver{
	{
		Ledger: "A-014", Layer: parity.LayerL2, Kind: "text-width", Subject: "系统密钥环已就绪", MaxDelta: 2,
		Note: "字偶距：状态条里的 Windows DPAPI 为拉丁字符，整行偏宽约 1.2px",
	},
}

// englishSettingsWaivers 是设置核心场景在英文变体（`settings.network.manual`、`settings.data`）里的全部登记：均为字偶距造成的次像素偏差，
// 不涉及版面与文字内容（英文变体的版面、文字、L3 全部对上，L3 最大 0.21%）。依据是 2026-11-22 M3-2a 的英文试点读数（浅色中档 1280×800）：
//   - A-014（400 字重 Chromium 做字偶距、Viva 不做，Viva 偏宽）：状态条「System keyring ready…」宽 +2.44、墨迹 +2；
//     「Older automatic backups…」宽 +1.09；「Choose HTTP, HTTPS…」宽 +1.60、墨迹 +2；镜像说明段落宽 +2.55、墨迹 +1；
//   - A-026（500 / 600 字重 Chromium 不做、Viva 做，Viva 偏窄）：「Network Proxy」宽 −1.43，「Proxy protocol」宽 −1.52，
//     「Change recovery password」宽 −2.00，「Network Settings」「Proxy Mode」「Proxy protocol」「Proxy host」「Proxy authentication」墨迹 −1。
//
// 登记只在英文变体（中档）里生效，写死的英文 Subject 不会命中中文变体；纵向偏差不放过（englishKerning 的 MaxDY 缺省 0）。
func englishSettingsWaivers() []parity.Waiver {
	const (
		w400, w600       = kerning400Ledger, kerning600Ledger
		longWidth        = 2.6 // 长段英文正文的宽度差（实测最大 2.55）
		note400, note600 = "字偶距（A-014）：400 字重 Chromium 做、Viva 不做，英文正文偏宽", "字偶距（A-026）：500 / 600 字重 Chromium 不做、Viva 做，英文标题偏窄"
	)
	return []parity.Waiver{
		englishKerning(w400, "text-width", "「System keyring ready (Windows DPAPI)", longWidth, note400),
		englishKerning(w400, "ink", "「System keyring ready (Windows DPAPI)", kerningInkCap, note400),
		englishKerning(w400, "text-width", "「Older automatic backups beyond this count", kerningWidthCap, note400),
		englishKerning(w400, "text-width", "「Choose HTTP, HTTPS, or SOCKS5", kerningWidthCap, note400),
		englishKerning(w400, "ink", "「Choose HTTP, HTTPS, or SOCKS5", kerningInkCap, note400),
		englishKerning(w400, "text-width", "「When enabled, update checks and installer downloads", longWidth, note400),
		englishKerning(w400, "ink", "「When enabled, update checks and installer downloads", kerningInkCapLine, note400),
		englishKerning(w600, "text-width", "「Network Proxy」", kerningWidthCap, note600),
		englishKerning(w600, "text-width", "「Proxy protocol」", kerningWidthCap, note600),
		englishKerning(w600, "text-width", "「Change recovery password」", kerningWidthCap, note600),
		englishKerning(w600, "ink", "「Network Settings」", kerningInkCapLine, note600),
		englishKerning(w600, "ink", "「Proxy Mode」", kerningInkCapLine, note600),
		englishKerning(w600, "ink", "「Proxy protocol」", kerningInkCapLine, note600),
		englishKerning(w600, "ink", "「Proxy host」", kerningInkCapLine, note600),
		englishKerning(w600, "ink", "「Proxy authentication」", kerningInkCapLine, note600),
	}
}

// aboutRepoKerningWaivers 登记 A-014（字偶距）在「关于」页项目仓库链接上的延伸：右对齐的链接文字
// 「github.com/Couleur-Share/GuiZhi」是纯拉丁字符，Chromium 做字偶距、Viva 不做，整行窄 2.27px（中档 1920×1080，React 220.89、Viva 218.62），
// 右对齐使链接外框与行内图标的左缘一起右移同样的距离（1280×800 里只有小档进可见区，1920×1080 的中档也进）。
// 只放过这一行的宽度差、外框差与图标外框差（≤2.6px），位置、字重、颜色与其余图标照常判定。
var aboutRepoKerningWaivers = []parity.Waiver{
	{
		Ledger: "A-014", Layer: parity.LayerL2, Kind: "text-width", Subject: "github.com/Couleur-Share/GuiZhi", MaxDelta: 2.6,
		Note: "字偶距：仓库链接的拉丁文字整行偏窄约 2.3px",
	},
	{
		Ledger: "A-014", Layer: parity.LayerL2, Kind: "box", Subject: "github.com/Couleur-Share/GuiZhi", MaxDelta: 2.6,
		Note: "同上：链接外框随文字宽度右对齐，左缘偏右约 2.3px",
	},
	{
		Ledger: "A-014", Layer: parity.LayerL2, Kind: "icon-box", Subject: " 16.00 16.00]", MaxDelta: 2.6,
		Note: "同上：链接左侧的 GitHub 图标随文字宽度偏右约 2.3px",
	},
	{
		Ledger: "A-014", Layer: parity.LayerL2, Kind: "icon-box", Subject: " 14.00 14.00]", MaxDelta: 2.6,
		Note: "同上：链接左侧的 GitHub 图标随文字宽度偏右约 2.3px",
	},
}

// recorderKerningWaivers 登记 A-014 在快捷键录制框文字上的延伸：居中的组合键文字（如「Ctrl + Alt + G」）含拉丁字偶，
// Viva 比渲染层窄约 0.5px，居中后起点右移 0.24px，把墨迹剖面推过一个像素边界，横向错位 1 像素。只放过横向 ≤1px。
var recorderKerningWaivers = []parity.Waiver{
	{
		Ledger: "A-014", Layer: parity.LayerL2, Kind: "ink", Subject: "Ctrl + Alt + G", MaxDelta: 1,
		Note: "字偶距：居中的组合键文字比渲染层窄约 0.5px，起点右移 0.24px，墨迹横向错位 1 像素",
	},
}

// settingsDotWaivers 登记 G-016 在设置页里孤立小圆点上的延伸：「·」（备份行与「关于」页版本行里的间隔点）与「•」（快捷键页使用提示的行首符）。
// 它们的笔画只有一个小圆点，墨迹总量由亚像素相位与两个引擎的灰度校正决定；位置、宽度、颜色与字号一致。
// 方向随字号档与主题而变（相位不同），读数全是实测（T0 / T1 / T2 / 英文，明暗 × 6 色相各份读数逐个相同），范围按档与方向各框各的：
//   - 「·」中档 Viva 偏少 0.175–0.245（「关于」页版本行）、0.201（备份行，1920×1080）；偏多 0.166（备份行，1920×1080）；
//   - 「·」大档偏多 0.189（「关于」页，深色）；小档偏多 0.280（备份行，深色）；
//   - 「•」中档深色偏多 0.161–0.174（快捷键页）。
//
// 范围向外取整到 0.01，方向固定，上限不超过 0.30：不会放过「少了一半」或「多了一倍」的缺陷。
func settingsDotWaivers() []parity.Waiver {
	rows := []struct {
		subject, variant string
		dir              int8
		lo, hi           float64
	}{
		{"「·」", "-medium-", -1, 0.17, 0.25},
		{"「·」", "-medium-", 1, 0.16, 0.17},
		{"「·」", "-large-", 1, 0.18, 0.20},
		{"「·」", "-small-", 1, 0.27, 0.29},
		{"「•」", "-medium-", 1, 0.16, 0.18},
	}
	out := make([]parity.Waiver, 0, len(rows))
	for _, r := range rows {
		out = append(out, parity.Waiver{
			Ledger: "G-016", Layer: parity.LayerL2, Kind: "ink-mass", Subject: r.subject, Dir: r.dir, Variant: r.variant, MinDelta: r.lo, MaxDelta: r.hi,
			Note: "孤立的小圆点：墨迹总量由亚像素相位与灰度校正决定（位置、宽度、颜色、字号均一致）",
		})
	}
	return out
}

// shortcutsScene 是「快捷键」页的内容区场景：组合键取夹具（读取是后台的，场景等它到齐再对照）。
func shortcutsScene(id string, waivers ...parity.Waiver) parity.Scene {
	prepare := func(look *kit.Look) (func(c ui.C), func(s *headless.Session) error) {
		fix := settingsCoreFixture()
		svc := newCoreServices(fix)
		k := &settingsscreen.Shortcuts{Look: look, Service: svc.shortcuts}
		return k.Page, func(s *headless.Session) error {
			return waitCond(s, "快捷键读取完成", func() bool {
				return svc.shortcuts.Gets() > 0 && snapshotHasText(s, strings.ReplaceAll(fix.Shortcuts["showApp"], "+", " + "))
			})
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

// dataLoaded 是「数据与备份」页的后台读取（旧快照列表、运行时路径、仓库状态与完整快照）都已上屏的判定。
// 页面比窗口高时（800×600），下面的列表行落在裁剪线外、不被绘制，不能拿它们当「到齐」的信号；
// 只看最上面的仓库状态条（密钥环那行）与「打开备份目录」按钮（可见时必须已可用）。四个读取由假服务同时即时返回，
// 状态条到了，其余的就在同一两帧里到了；调用方在条件成立后另出几帧收尾（settleData）。
func dataLoaded(s *headless.Session, fix parity.SettingsCoreFixture) bool {
	if open, ok := s.AccessByRole(gfx.RoleButton, locales.SettingsOpenBackupsDir()); ok && open.Disabled {
		return false
	}
	backend := fix.Repository.Status.KeyStorageBackend
	return backend != nil && snapshotHasText(s, *backend)
}

// settleData 在 dataLoaded 成立后多出几帧：让同一批读取里还在路上的列表与路径也上屏。
func settleData(s *headless.Session) error {
	for range 20 {
		if err := s.Frame(); err != nil {
			return err
		}
		if err := s.Rest(); err != nil {
			return err
		}
		time.Sleep(5 * time.Millisecond)
	}
	return nil
}

// dataScene 是「数据与备份」页的内容区场景。
func dataScene(id string, waivers ...parity.Waiver) parity.Scene {
	prepare := func(look *kit.Look) (func(c ui.C), func(s *headless.Session) error) {
		fix := settingsCoreFixture()
		svc := newCoreServices(fix)
		d := &settingsscreen.Data{
			Look: look, Store: settingsscreen.NewMemStore(settings.Default()),
			Runtime: svc.data, Backup: svc.backup, Transfer: svc.transfer,
		}
		return d.Page, func(s *headless.Session) error {
			if err := waitCond(s, "数据与备份读取完成", func() bool { return dataLoaded(s, fix) }); err != nil {
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

// exportDialogScene 是整窗（顶栏 + rail + 数据与备份页）上弹着「导出配置」对话框的场景：先把内容滚到底（配置迁移在最下面），
// 再点「导出到文件」。打开之前的页面（模糊背景的预测，台账 A-007）走 Build——同样滚到底之前的数据页，由 BackdropSpec 的步骤补齐。
func exportDialogScene(id string) parity.Scene {
	prepare := func(look *kit.Look) (func(c ui.C), func(s *headless.Session) error) {
		fix := settingsCoreFixture()
		svc := newCoreServices(fix)
		w := &shellscreen.Window{
			Look: look, Page: shellscreen.PageSettings, OwnControls: true, SearchShortcut: "Alt+Shift+F", CaptureShortcut: "Alt+Shift+N",
			Settings: &settingsscreen.Shell{
				Look: look, Store: settingsscreen.NewMemStore(settings.Default()), Active: settingsscreen.SectionData,
				Data: svc.data, Backup: svc.backup, ConfigTransfer: svc.transfer,
			},
		}
		return w.View, func(s *headless.Session) error {
			if err := waitCond(s, "数据与备份读取完成", func() bool { return dataLoaded(s, fix) }); err != nil {
				return err
			}
			if err := settleData(s); err != nil {
				return err
			}
			if err := scrollToEnd(s); err != nil {
				return err
			}
			if err := clickAndSettle(s, gfx.RoleButton, locales.SettingsConfigExportStart()); err != nil {
				return err
			}
			return parity.ParkPointer(s)
		}
	}
	// 遮罩背后的页面：数据到齐后滚到底，与打开对话框之前的步骤一致（背景要与 React 里同样滚到底的页面逐节点对照）。
	backdrop := modalBackdrop(locales.SettingsConfigExportTitle())
	backdrop.BaseSteps = func(s *headless.Session, _ *kit.Look) error {
		fix := settingsCoreFixture()
		if err := waitCond(s, "数据与备份读取完成", func() bool { return dataLoaded(s, fix) }); err != nil {
			return err
		}
		if err := settleData(s); err != nil {
			return err
		}
		return scrollToEnd(s)
	}
	return withBackdrop(parity.Scene{
		ID: id,
		Build: func(look *kit.Look) func(c ui.C) {
			view, _ := prepare(look)
			return view
		},
		Prepare: prepare,
		Waivers: slices.Concat(pageSettingsWaivers, keyringKerningWaivers, []parity.Waiver{overlayInkWaiver}, modalTitleWaivers(locales.SettingsConfigExportTitle())),
	}, backdrop)
}

// aboutScene 是「关于」页的内容区场景：版本号取夹具，更新检查行停在「尚未检查」。
func aboutScene(id string, waivers ...parity.Waiver) parity.Scene {
	return parity.Scene{
		ID: id,
		Build: func(look *kit.Look) func(c ui.C) {
			a := &settingsscreen.About{
				Look: look, Store: settingsscreen.NewMemStore(settings.Default()), Version: settingsCoreFixture().Version,
				Updater: &servicestest.Updater{}, OpenExternal: func(string) error { return nil }, OpenSetupGuide: func() {},
				Record: &settingsscreen.UpdateRecord{},
			}
			return a.Page
		},
		Waivers: waivers,
	}
}

func init() {
	registry["settings.network"] = networkScene("settings.network", nil)
	registry["settings.network.manual"] = networkScene("settings.network.manual", openManualProxy, slices.Concat(proxyHostKerningWaivers, englishSettingsWaivers())...)
	registry["settings.shortcuts"] = shortcutsScene("settings.shortcuts", slices.Concat(recorderKerningWaivers, settingsDotWaivers())...)
	registry["settings.data"] = dataScene("settings.data", slices.Concat(keyringKerningWaivers, settingsDotWaivers(), englishSettingsWaivers())...)
	registry["settings.data.export-dlg"] = exportDialogScene("settings.data.export-dlg")
	registry["settings.about"] = aboutScene("settings.about", slices.Concat(aboutRepoKerningWaivers, settingsDotWaivers())...)
}
