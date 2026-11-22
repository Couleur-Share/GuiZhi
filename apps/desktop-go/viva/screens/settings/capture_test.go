package settings_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	nsettings "guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	sscreen "guizhi-mygo-shell/viva/screens/settings"
)

// captureEnv 是「采集与转写」页的测试环境：假的采集工具与 FlareSolverr 服务，默认 yt-dlp 已装内置版、ffmpeg 与本地转写引擎未装、三个平台都可登录。
type captureEnv struct {
	*sysEnv
	store    *sscreen.MemStore
	cap      *servicestest.Capture
	solver   *servicestest.Solver
	platform string
}

func managedStatus(version string) services.ToolStatus {
	return services.ToolStatus{Installed: true, Source: services.ToolSourceManaged, Version: version, Path: `C:\gz\bin\tool.exe`, InstallSupported: true}
}

func newCaptureEnv() *captureEnv {
	e := &captureEnv{sysEnv: newEnv(), store: newStore(), cap: &servicestest.Capture{}, solver: &servicestest.Solver{}, platform: "win32"}
	e.cap.SetToolStatus(services.ToolYtDlp, managedStatus("2026.03.17"))
	e.cap.SetToolStatus(services.ToolFfmpeg, services.ToolStatus{InstallSupported: true})
	e.cap.SetFunasrStatus(services.FunasrStatus{InstallSupported: true, UpdateSupported: true})
	e.cap.SetAccounts([]services.PlatformStatus{
		{Platform: services.PlatformXiaohongshu, Available: true},
		{Platform: services.PlatformDouyin, Available: true},
		{Platform: services.PlatformLinuxdo, Available: true},
	})
	return e
}

// open 把「采集与转写」页单独打开（关闭时收尾后台工作）。
func (e *captureEnv) open(t *testing.T) *rig {
	t.Helper()
	return openPage(t, func(look *kit.Look) func(c ui.C) {
		p := &sscreen.Capture{
			Look: look, Store: e.store, Guard: e.guard(), Service: e.cap, Solver: e.solver, Platform: e.platform, Wake: func() {},
		}
		t.Cleanup(p.Close)
		return p.Page
	})
}

func (e *captureEnv) lastError(t *testing.T) kit.QueuedToast {
	t.Helper()
	errs := e.toasts(kit.ToastError)
	if len(errs) == 0 {
		t.Fatal("应当弹出一条错误提示")
	}
	return errs[len(errs)-1]
}

func waitText(r *rig, text string) {
	r.t.Helper()
	waitUntil(r, "画面出现「"+text+"」", func() bool { return r.hasText(text) })
}

func waitButton(r *rig, name string) {
	r.t.Helper()
	waitUntil(r, "出现按钮「"+name+"」", func() bool { return r.has(gfx.RoleButton, name) })
}

// ── 引擎状态 ──

func TestCaptureEngineRowsShowStatus(t *testing.T) {
	e := newCaptureEnv()
	r := e.open(t)

	// yt-dlp：内置版已就绪，状态行是「版本 · 来源」。
	waitText(r, locales.SettingsCaptureStateReady())
	if !r.hasText(services.FormatEngineVersion("2026.03.17") + " · " + locales.SettingsCaptureSourceManaged()) {
		t.Error("已就绪的引擎应显示「版本 · 来源」")
	}
	// ffmpeg 是可选引擎：缺失只给中性提示与用途说明，不报警。
	if !r.hasText(locales.SettingsCaptureStateOptional()) || !r.hasText(locales.SettingsFfmpegMissingHint()) {
		t.Error("未装的 ffmpeg 应显示「未安装（可选）」与为什么值得装")
	}
	// 本地转写引擎未装：磁盘占用提示。
	if !r.hasText(locales.SettingsFunasrDiskHint()) {
		t.Error("未装的本地转写引擎应显示磁盘占用提示")
	}
	// 三个引擎各探测一次，且首次探测不强制。
	for _, tool := range []services.ManagedTool{services.ToolYtDlp, services.ToolFfmpeg} {
		if calls := e.cap.StatusCalls(tool); len(calls) != 1 || calls[0] {
			t.Errorf("%s 的首次探测应是一次不强制的探测，得到 %v", tool, calls)
		}
	}
}

func TestCaptureEngineProbeFailureShowsErrorState(t *testing.T) {
	e := newCaptureEnv()
	e.cap.FailToolStatus(services.ToolYtDlp, errors.New("spawn yt-dlp: access denied"))
	r := e.open(t)
	waitText(r, locales.SettingsCaptureStateError())
	if !r.hasText("spawn yt-dlp: access denied") {
		t.Error("探测失败应在行上显示原因，不能当成「未安装」")
	}
	if len(e.toasts(kit.ToastError)) != 0 {
		t.Error("后台探测失败只进日志，不应弹提示打扰用户")
	}
	if len(e.logs.all()) == 0 {
		t.Error("探测失败应记入日志")
	}
	// 失败时没有安装类主操作：不知道装没装，任何动作都是瞎猜。
	if r.has(gfx.RoleButton, locales.SettingsCaptureCheckUpdate()) {
		t.Error("探测失败时不应出现「检查更新」")
	}
	// 修好之后点「重新检测」恢复。
	e.cap.FailToolStatus(services.ToolYtDlp, nil)
	r.pressNth(gfx.RoleButton, locales.SettingsCaptureAdvanced(), 0)
	r.pressNth(gfx.RoleButton, locales.SettingsCaptureRecheck(), 0)
	waitText(r, locales.SettingsCaptureStateReady())
}

func TestCaptureUnsupportedPlatformOffersCopyCommand(t *testing.T) {
	e := newCaptureEnv()
	e.cap.SetToolStatus(services.ToolFfmpeg, services.ToolStatus{InstallSupported: false, InstallHintCommand: "brew install ffmpeg"})
	r := e.open(t)
	waitText(r, locales.SettingsFfmpegBrewHint("brew install ffmpeg"))
	if r.has(gfx.RoleButton, locales.SettingsCaptureInstall()) && len(r.named(gfx.RoleButton, locales.SettingsCaptureInstall())) > 1 {
		t.Error("不支持应用内安装的平台不应给 ffmpeg「一键安装」")
	}
	r.press(gfx.RoleButton, locales.SettingsCaptureCopyBrewCommand())
	if len(e.copied) != 1 || e.copied[0] != "brew install ffmpeg" {
		t.Errorf("应复制推荐命令，剪贴板 = %v", e.copied)
	}
	if got := e.toasts(kit.ToastSuccess); len(got) != 1 || got[0].Message != locales.SettingsCaptureCommandCopied("brew install ffmpeg") {
		t.Errorf("复制成功应提示已复制的命令，得到 %+v", got)
	}
}

// ── 安装进度与失败重试 ──

func TestCaptureInstallShowsProgressThenSucceeds(t *testing.T) {
	e := newCaptureEnv()
	gate := make(chan struct{})
	e.cap.InstallGate = map[services.ManagedTool]chan struct{}{services.ToolFfmpeg: gate}
	e.cap.SetInstallVersion(services.ToolFfmpeg, "2026-03-01-build")
	r := e.open(t)
	waitText(r, locales.SettingsCaptureStateOptional())

	r.pressNth(gfx.RoleButton, locales.SettingsCaptureInstall(), 0) // 第一个是 ffmpeg 行（yt-dlp 已装）
	waitText(r, locales.SettingsCaptureInstalling())
	if e.cap.ToolSubscribers(services.ToolFfmpeg) != 1 {
		t.Fatalf("应订阅下载进度，订阅数 %d", e.cap.ToolSubscribers(services.ToolFfmpeg))
	}

	// 有总量：百分比。
	e.cap.EmitToolProgress(services.ToolFfmpeg, services.ToolProgress{Transferred: 5 << 20, Total: 10 << 20})
	waitText(r, locales.SettingsCaptureDownloading(50))
	// 没有总量：已下载字节。
	e.cap.EmitToolProgress(services.ToolFfmpeg, services.ToolProgress{Transferred: 3 << 20})
	waitText(r, locales.SettingsCaptureDownloadingBytes("3.0 MB"))

	// 安装进行时，同一引擎的主按钮禁用（不能重复安装）。
	if !r.disabled(gfx.RoleButton, locales.SettingsCaptureInstalling()) {
		t.Error("安装中的主按钮应禁用")
	}

	close(gate)
	waitUntil(r, "安装成功提示", func() bool { return len(e.toasts(kit.ToastSuccess)) > 0 })
	if got := e.toasts(kit.ToastSuccess)[0].Message; got != locales.SettingsFfmpegInstallDone(services.FormatEngineVersion("2026-03-01-build")) {
		t.Errorf("成功提示 = %q", got)
	}
	// 安装结束后强制重新探测，行变成已就绪。
	waitUntil(r, "强制探测", func() bool {
		calls := e.cap.StatusCalls(services.ToolFfmpeg)
		return len(calls) >= 2 && calls[len(calls)-1]
	})
}

func TestCaptureInstallFailureShowsReasonAndCanRetry(t *testing.T) {
	e := newCaptureEnv()
	e.cap.FailInstall(services.ToolFfmpeg, errors.New("download failed: connection reset by peer"))
	r := e.open(t)
	waitText(r, locales.SettingsCaptureStateOptional())

	r.pressNth(gfx.RoleButton, locales.SettingsCaptureInstall(), 0)
	waitUntil(r, "错误提示", func() bool { return len(e.toasts(kit.ToastError)) > 0 })
	toast := e.lastError(t)
	if !strings.Contains(toast.Detail, "connection reset by peer") || !strings.Contains(toast.Message, "connection reset by peer") {
		t.Errorf("失败应说明原因（标题与详情都带原文），得到 %+v", toast)
	}
	if len(e.toasts(kit.ToastSuccess)) != 0 {
		t.Error("失败后不能报成功")
	}
	// 失败后按钮恢复可用，修好之后可以重试。
	waitUntil(r, "按钮恢复", func() bool { return len(r.named(gfx.RoleButton, locales.SettingsCaptureInstall())) == 2 })
	e.cap.FailInstall(services.ToolFfmpeg, nil)
	r.pressNth(gfx.RoleButton, locales.SettingsCaptureInstall(), 0)
	waitUntil(r, "重试成功", func() bool { return len(e.toasts(kit.ToastSuccess)) == 1 })
	if e.cap.Installs(services.ToolFfmpeg) != 2 {
		t.Errorf("应安装了两次（一次失败一次重试），得到 %d", e.cap.Installs(services.ToolFfmpeg))
	}
}

// ── 检查更新、移除与自定义路径 ──

func TestCaptureUpdateCheckStateMachine(t *testing.T) {
	e := newCaptureEnv()
	e.cap.SetToolCheck(services.ToolYtDlp, services.ToolUpdateCheck{Current: "2026.03.17", Latest: "2026.04.02", UpdateAvailable: true})
	e.cap.SetInstallVersion(services.ToolYtDlp, "2026.04.02")
	r := e.open(t)
	waitButton(r, locales.SettingsCaptureCheckUpdate())

	r.press(gfx.RoleButton, locales.SettingsCaptureCheckUpdate())
	update := locales.SettingsCaptureUpdateTo(services.FormatEngineVersion("2026.04.02"))
	waitButton(r, update)
	r.press(gfx.RoleButton, update)
	waitUntil(r, "更新完成提示", func() bool { return len(e.toasts(kit.ToastSuccess)) == 1 })
	// 版本变了，之前的检查结论作废：回到「检查更新」。
	waitButton(r, locales.SettingsCaptureCheckUpdate())

	// 已是最新：不再给按钮，显示静态状态。
	e.cap.SetToolCheck(services.ToolYtDlp, services.ToolUpdateCheck{Current: "2026.04.02", Latest: "2026.04.02"})
	r.press(gfx.RoleButton, locales.SettingsCaptureCheckUpdate())
	waitText(r, locales.SettingsCaptureUpToDate())
	if r.has(gfx.RoleButton, locales.SettingsCaptureCheckUpdate()) {
		t.Error("已是最新之后不应再有「检查更新」按钮")
	}
	// 「重新检测」让之前那次远端检查的结论作废。
	r.pressNth(gfx.RoleButton, locales.SettingsCaptureAdvanced(), 0)
	r.pressNth(gfx.RoleButton, locales.SettingsCaptureRecheck(), 0)
	waitButton(r, locales.SettingsCaptureCheckUpdate())
}

func TestCaptureUpdateCheckFailureKeepsCheckButton(t *testing.T) {
	e := newCaptureEnv()
	e.cap.FailToolCheck(services.ToolYtDlp, errors.New("proxyconnect tcp: connection refused"))
	r := e.open(t)
	waitButton(r, locales.SettingsCaptureCheckUpdate())
	r.press(gfx.RoleButton, locales.SettingsCaptureCheckUpdate())
	waitUntil(r, "错误提示", func() bool { return len(e.toasts(kit.ToastError)) > 0 })
	toast := e.lastError(t)
	if toast.Message != locales.SettingsCaptureCheckFailed() || !strings.Contains(toast.Detail, "connection refused") {
		t.Errorf("检查失败应提示「检查更新失败」并在详情里给原因，得到 %+v", toast)
	}
	waitButton(r, locales.SettingsCaptureCheckUpdate())

	// 服务端没有给出最新版本（Latest 为空）也算失败，不能显示「已是最新」。
	e.cap.FailToolCheck(services.ToolYtDlp, nil)
	e.cap.SetToolCheck(services.ToolYtDlp, services.ToolUpdateCheck{Current: "2026.03.17"})
	r.press(gfx.RoleButton, locales.SettingsCaptureCheckUpdate())
	waitUntil(r, "第二条错误提示", func() bool { return len(e.toasts(kit.ToastError)) == 2 })
	if r.hasText(locales.SettingsCaptureUpToDate()) {
		t.Error("没有最新版本信息时不能显示「已是最新」")
	}
}

func TestCaptureRemoveManagedAsksFirst(t *testing.T) {
	e := newCaptureEnv()
	r := e.open(t)
	waitButton(r, locales.SettingsCaptureAdvanced())
	r.pressNth(gfx.RoleButton, locales.SettingsCaptureAdvanced(), 0)
	r.press(gfx.RoleButton, locales.SettingsCaptureRemoveManaged())
	if !r.dialogOpen(locales.SettingsCaptureRemoveManaged()) {
		t.Fatal("移除内置版应当先弹确认对话框")
	}
	if e.cap.Removes(services.ToolYtDlp) != 0 {
		t.Error("确认之前不能移除")
	}
	// 取消：什么都不做。
	r.clickNode(gfx.RoleButton, locales.CommonCancel())
	if e.cap.Removes(services.ToolYtDlp) != 0 || r.dialogOpen(locales.SettingsCaptureRemoveManaged()) {
		t.Fatal("取消应当只关闭对话框")
	}
	// 确认：移除并提示。
	r.press(gfx.RoleButton, locales.SettingsCaptureRemoveManaged())
	r.clickLast(gfx.RoleButton, locales.SettingsCaptureRemoveManaged())
	waitUntil(r, "移除提示", func() bool { return len(e.toasts(kit.ToastSuccess)) == 1 })
	if e.cap.Removes(services.ToolYtDlp) != 1 || e.toasts(kit.ToastSuccess)[0].Message != locales.SettingsYtDlpRemoved() {
		t.Errorf("移除次数 %d，提示 %+v", e.cap.Removes(services.ToolYtDlp), e.toasts(kit.ToastSuccess))
	}
}

func TestCaptureRemoveFailureReportsReason(t *testing.T) {
	e := newCaptureEnv()
	e.cap.FailRemove(services.ToolYtDlp, errors.New("the file is in use"))
	r := e.open(t)
	waitButton(r, locales.SettingsCaptureAdvanced())
	r.pressNth(gfx.RoleButton, locales.SettingsCaptureAdvanced(), 0)
	r.press(gfx.RoleButton, locales.SettingsCaptureRemoveManaged())
	r.clickLast(gfx.RoleButton, locales.SettingsCaptureRemoveManaged())
	waitUntil(r, "错误提示", func() bool { return len(e.toasts(kit.ToastError)) == 1 })
	if !strings.Contains(e.lastError(t).Detail, "the file is in use") || len(e.toasts(kit.ToastSuccess)) != 0 {
		t.Errorf("移除失败应带原因且不报成功：%+v", e.notify.Queued())
	}
}

func TestCaptureCustomPathOnlyThroughPicker(t *testing.T) {
	e := newCaptureEnv()
	e.cap.SetPick(services.ToolYtDlp, `D:\tools\yt-dlp.exe`)
	r := e.open(t)
	waitButton(r, locales.SettingsCaptureAdvanced())
	r.pressNth(gfx.RoleButton, locales.SettingsCaptureAdvanced(), 0)

	// 路径框是只读的：没有文本框，只能点「选择文件」。
	if r.has(gfx.RoleTextField, locales.SettingsCaptureCustomPath()) {
		t.Fatal("自定义路径不应是可输入的文本框（只接受文件选择器返回的值）")
	}
	r.press(gfx.RoleButton, locales.SettingsCaptureBrowse())
	waitUntil(r, "路径写入设置", func() bool { return e.store.Values().App.YtDlpPath == `D:\tools\yt-dlp.exe` })
	if e.cap.Picks(services.ToolYtDlp) != 1 {
		t.Errorf("应只选择一次，得到 %d", e.cap.Picks(services.ToolYtDlp))
	}
	// 换了路径就换了探测目标：重新探测。
	waitUntil(r, "换路径后重新探测", func() bool { return len(e.cap.StatusCalls(services.ToolYtDlp)) >= 2 })
	if n, ok := r.node(gfx.RoleGroup, locales.SettingsCaptureCustomPath()); !ok || n.Description != `D:\tools\yt-dlp.exe` {
		t.Errorf("路径框应显示选中的路径，得到 %+v", n)
	}
	// 清除。
	r.press(gfx.RoleButton, locales.SettingsCaptureClearPath())
	waitUntil(r, "路径清空", func() bool { return e.store.Values().App.YtDlpPath == "" })

	// 取消选择（返回空路径）不改设置；选择器出错弹带原因的错误提示。
	before := e.store.Updates
	e.cap.SetPick(services.ToolYtDlp, "")
	r.press(gfx.RoleButton, locales.SettingsCaptureBrowse())
	waitUntil(r, "选择结束", func() bool { return e.cap.Picks(services.ToolYtDlp) == 2 })
	if e.store.Updates != before {
		t.Error("取消选择不应写入设置")
	}
	e.cap.FailPick(services.ToolYtDlp, errors.New("file dialog failed"))
	r.press(gfx.RoleButton, locales.SettingsCaptureBrowse())
	waitUntil(r, "选择器错误提示", func() bool { return len(e.toasts(kit.ToastError)) == 1 })
	if !strings.Contains(e.lastError(t).Detail, "file dialog failed") {
		t.Errorf("选择器失败应带原因：%+v", e.lastError(t))
	}
}

// ── 平台账号 ──

func TestCapturePlatformLoginLogoutAndCancel(t *testing.T) {
	e := newCaptureEnv()
	gate := make(chan struct{})
	e.cap.LoginGate = gate
	r := e.open(t)
	waitText(r, locales.SettingsPlatformLoggedOut())
	// LINUX DO 的主按钮是「验证」，另两个是「登录」。
	if len(r.named(gfx.RoleButton, locales.SettingsPlatformLogin())) != 2 || !r.has(gfx.RoleButton, locales.SettingsPlatformVerify()) {
		t.Fatal("小红书与抖音各一个「登录」，LINUX DO 是「验证」")
	}

	r.clickNth(gfx.RoleButton, locales.SettingsPlatformLogin(), 0)
	waitButton(r, locales.CommonCancel())
	if logins := e.cap.Logins(); len(logins) != 1 || logins[0].Platform != services.PlatformXiaohongshu || logins[0].Force {
		t.Fatalf("登录调用 = %+v", logins)
	}
	// 登录窗口开着时只给「取消」。
	r.clickNode(gfx.RoleButton, locales.CommonCancel())
	waitUntil(r, "取消已送出", func() bool { return len(e.cap.Cancels()) == 1 })
	if e.cap.Cancels()[0] != services.PlatformXiaohongshu {
		t.Errorf("取消的平台 = %v", e.cap.Cancels())
	}

	close(gate)
	waitUntil(r, "登录成功", func() bool { return r.has(gfx.RoleButton, locales.SettingsPlatformLogout()) })
	if got := e.toasts(kit.ToastSuccess); len(got) != 1 || got[0].Message != locales.SettingsPlatformLoginSuccess(locales.SettingsPlatformXiaohongshu()) {
		t.Errorf("登录成功提示 = %+v", got)
	}
	// 已登录：显示内置登录窗口，而不是任何账号凭证。
	if !r.hasText(locales.SettingsPlatformLoggedIn(locales.NativeSettingsPlatformBrowser())) {
		t.Error("已登录应显示「已登录 · 归知内置登录窗口」")
	}

	// 重新登录带 force；退出账号回到未登录。
	r.press(gfx.RoleButton, locales.SettingsPlatformRelogin())
	waitUntil(r, "重新登录送出", func() bool { return len(e.cap.Logins()) == 2 })
	if !e.cap.Logins()[1].Force {
		t.Error("重新登录应带 forceRelogin")
	}
	waitUntil(r, "重新登录完成", func() bool { return r.has(gfx.RoleButton, locales.SettingsPlatformLogout()) })
	r.press(gfx.RoleButton, locales.SettingsPlatformLogout())
	waitUntil(r, "退出", func() bool { return len(e.cap.Logouts()) == 1 })
	waitUntil(r, "回到未登录", func() bool { return !r.has(gfx.RoleButton, locales.SettingsPlatformLogout()) })
}

func TestCapturePlatformFailuresAreReported(t *testing.T) {
	e := newCaptureEnv()
	e.cap.FailLogin(errors.New("browser launch failed"))
	r := e.open(t)
	waitText(r, locales.SettingsPlatformLoggedOut())
	r.clickNth(gfx.RoleButton, locales.SettingsPlatformLogin(), 0)
	waitUntil(r, "登录失败提示", func() bool { return len(e.toasts(kit.ToastError)) == 1 })
	if !strings.Contains(e.lastError(t).Detail, "browser launch failed") || len(e.toasts(kit.ToastSuccess)) != 0 {
		t.Errorf("登录失败应带原因且不报成功：%+v", e.notify.Queued())
	}
	// 失败之后行恢复，可以再试。
	waitUntil(r, "行恢复", func() bool { return len(r.named(gfx.RoleButton, locales.SettingsPlatformLogin())) == 2 })
}

func TestCapturePlatformStatusFailureIsReported(t *testing.T) {
	e := newCaptureEnv()
	e.cap.FailAccountStatus(errors.New("session store unreadable"))
	r := e.open(t)
	waitUntil(r, "状态读取失败提示", func() bool { return len(e.toasts(kit.ToastError)) == 1 })
	toast := e.lastError(t)
	if toast.Message != locales.CommonOperationFailed(locales.SettingsPlatformCaptureStatus()) || !strings.Contains(toast.Detail, "session store unreadable") {
		t.Errorf("读取状态失败应带原因：%+v", toast)
	}
	// 状态读不到时按钮保持禁用（不知道能不能登录，渲染层同样），不能当成「未登录」让人点。
	if !r.disabled(gfx.RoleButton, locales.SettingsPlatformVerify()) {
		t.Error("读不到状态时登录按钮应禁用")
	}
}

func TestCapturePlatformUnavailableDisablesLogin(t *testing.T) {
	e := newCaptureEnv()
	e.cap.SetAccounts([]services.PlatformStatus{{Platform: services.PlatformXiaohongshu}, {Platform: services.PlatformDouyin}, {Platform: services.PlatformLinuxdo}})
	r := e.open(t)
	waitText(r, locales.SettingsPlatformBrowserUnavailable())
	if !r.disabled(gfx.RoleButton, locales.SettingsPlatformVerify()) {
		t.Error("内置登录窗口不可用时按钮应禁用")
	}
}

func TestCaptureClearPlatformDataAsksFirst(t *testing.T) {
	e := newCaptureEnv()
	r := e.open(t)
	waitText(r, locales.SettingsPlatformLoggedOut())
	r.press(gfx.RoleButton, locales.CommonClear())
	if !r.dialogOpen(locales.SettingsPlatformClearData()) || e.cap.Clears() != 0 {
		t.Fatal("清除登录数据应当先弹确认，确认之前不清除")
	}
	r.clickNode(gfx.RoleButton, locales.CommonCancel())
	if e.cap.Clears() != 0 {
		t.Fatal("取消不应清除")
	}
	r.press(gfx.RoleButton, locales.CommonClear())
	r.clickLast(gfx.RoleButton, locales.CommonClear())
	waitUntil(r, "清除提示", func() bool { return len(e.toasts(kit.ToastSuccess)) == 1 })
	if e.cap.Clears() != 1 || e.toasts(kit.ToastSuccess)[0].Message != locales.SettingsPlatformClearSuccess() {
		t.Errorf("清除次数 %d，提示 %+v", e.cap.Clears(), e.toasts(kit.ToastSuccess))
	}
	// 失败：带原因，不报成功。
	e.cap.FailClear(errors.New("profile directory is locked"))
	r.press(gfx.RoleButton, locales.CommonClear())
	r.clickLast(gfx.RoleButton, locales.CommonClear())
	waitUntil(r, "清除失败提示", func() bool { return len(e.toasts(kit.ToastError)) == 1 })
	if !strings.Contains(e.lastError(t).Detail, "profile directory is locked") || len(e.toasts(kit.ToastSuccess)) != 1 {
		t.Errorf("清除失败应带原因且不再报成功：%+v", e.notify.Queued())
	}
}

// ── 本地转写引擎与说话人分离 ──

func installedFunasr() services.FunasrStatus {
	return services.FunasrStatus{Installed: true, Version: "1.2.0", Dir: `C:\gz\funasr`, InstallSupported: true, UpdateSupported: true}
}

func TestCaptureFunasrInstallShowsPhases(t *testing.T) {
	e := newCaptureEnv()
	gate := make(chan struct{})
	e.cap.FunasrGate = gate
	r := e.open(t)
	waitText(r, locales.SettingsFunasrDiskHint())
	r.pressNth(gfx.RoleButton, locales.SettingsCaptureInstall(), 1) // 第二个是本地转写引擎
	waitUntil(r, "订阅进度", func() bool { return e.cap.FunasrSubscribers() == 1 })

	pct := 30
	e.cap.EmitFunasrProgress(services.FunasrProgress{Phase: services.FunasrPhaseModels, Percent: &pct, Detail: "model.bin"})
	waitText(r, locales.SettingsFunasrPhaseModels()+" · 30% · model.bin")
	close(gate)
	waitUntil(r, "安装成功", func() bool { return len(e.toasts(kit.ToastSuccess)) == 1 })
	if e.toasts(kit.ToastSuccess)[0].Message != locales.SettingsFunasrInstallDone() {
		t.Errorf("成功提示 = %+v", e.toasts(kit.ToastSuccess))
	}
}

func TestCaptureFunasrUpdateFlow(t *testing.T) {
	e := newCaptureEnv()
	e.cap.SetFunasrStatus(installedFunasr())
	e.cap.SetFunasrCheck(services.ToolUpdateCheck{Current: "1.2.0", Latest: "1.3.0", UpdateAvailable: true})
	e.cap.SetFunasrUpdateResult(services.FunasrUpdateResult{Version: "1.3.0", Warning: "old backup could not be removed"})
	gate := make(chan struct{})
	e.cap.FunasrGate = gate
	r := e.open(t)
	waitText(r, "v1.2.0")

	// 先检查、有更新才给更新按钮（第一个「检查更新」是 yt-dlp 行的，本地转写引擎是第二个）。
	r.pressNth(gfx.RoleButton, locales.SettingsCaptureCheckUpdate(), 1)
	update := locales.SettingsCaptureUpdateTo("v1.3.0")
	waitButton(r, update)
	r.press(gfx.RoleButton, update)

	// 更新期间：进度面板、阶段标题、百分比与「更新中」徽章；版本显示 旧 → 新。
	waitText(r, locales.SettingsFunasrUpdatingBadge())
	waitText(r, "v1.2.0 → v1.3.0")
	e.cap.EmitFunasrProgress(services.FunasrProgress{Phase: services.FunasrPhaseBackup})
	waitText(r, locales.SettingsFunasrPhaseBackup())
	pct := 40
	e.cap.EmitFunasrProgress(services.FunasrProgress{Phase: services.FunasrPhaseDeps, Percent: &pct, Detail: "pip install"})
	waitText(r, locales.SettingsFunasrUpdateDependencies())
	waitText(r, "40%")
	// 回滚阶段换标题与徽章。
	e.cap.EmitFunasrProgress(services.FunasrProgress{Phase: services.FunasrPhaseRollback})
	waitText(r, locales.SettingsFunasrRestoringBadge())

	close(gate)
	waitUntil(r, "更新完成的警告提示", func() bool { return len(e.toasts(kit.ToastWarning)) == 1 })
	w := e.toasts(kit.ToastWarning)[0]
	if w.Message != locales.SettingsFunasrUpdateDone("1.3.0") || w.Detail != "old backup could not be removed" {
		t.Errorf("带警告的更新完成提示 = %+v", w)
	}
	if len(e.cap.FunasrUpdates()) != 1 || e.cap.FunasrUpdates()[0] != "1.3.0" {
		t.Errorf("更新调用 = %v", e.cap.FunasrUpdates())
	}
	// 更新结束：面板消失，检查结论作废回到「检查更新」。
	waitButton(r, locales.SettingsCaptureCheckUpdate())
	if r.hasText(locales.SettingsFunasrUpdatingBadge()) {
		t.Error("更新结束后「更新中」徽章应消失")
	}
}

func TestCaptureFunasrUpdateFailure(t *testing.T) {
	e := newCaptureEnv()
	e.cap.SetFunasrStatus(installedFunasr())
	e.cap.SetFunasrCheck(services.ToolUpdateCheck{Latest: "1.3.0", UpdateAvailable: true})
	e.cap.FailFunasrUpdate(errors.New("verify failed, rolled back to 1.2.0"))
	r := e.open(t)
	waitUntil(r, "两个检查更新按钮", func() bool { return len(r.named(gfx.RoleButton, locales.SettingsCaptureCheckUpdate())) == 2 })
	r.pressNth(gfx.RoleButton, locales.SettingsCaptureCheckUpdate(), 1)
	waitButton(r, locales.SettingsCaptureUpdateTo("v1.3.0"))
	r.press(gfx.RoleButton, locales.SettingsCaptureUpdateTo("v1.3.0"))
	waitUntil(r, "错误提示", func() bool { return len(e.toasts(kit.ToastError)) == 1 })
	toast := e.lastError(t)
	if toast.Message != locales.SettingsFunasrUpdateFailed() || !strings.Contains(toast.Detail, "rolled back") {
		t.Errorf("更新失败应带原因：%+v", toast)
	}
	if len(e.toasts(kit.ToastSuccess)) != 0 || len(e.toasts(kit.ToastWarning)) != 0 {
		t.Error("更新失败不能报成功")
	}
}

func TestCaptureFunasrUninstallAsksFirst(t *testing.T) {
	e := newCaptureEnv()
	e.cap.SetFunasrStatus(installedFunasr())
	r := e.open(t)
	waitText(r, "v1.2.0")
	r.pressNth(gfx.RoleButton, locales.SettingsCaptureAdvanced(), 2) // 第三行
	r.press(gfx.RoleButton, locales.SettingsFunasrUninstall())
	if !r.dialogOpen(locales.SettingsFunasrUninstall()) || e.cap.FunasrUninstalls() != 0 {
		t.Fatal("卸载应当先弹确认，确认之前不卸载")
	}
	r.clickLast(gfx.RoleButton, locales.SettingsFunasrUninstall())
	waitUntil(r, "卸载提示", func() bool { return len(e.toasts(kit.ToastSuccess)) == 1 })
	if e.cap.FunasrUninstalls() != 1 {
		t.Errorf("卸载次数 %d", e.cap.FunasrUninstalls())
	}
}

func TestCaptureDiarizeSwitchFollowsPlatform(t *testing.T) {
	name := locales.SettingsTranscribeDiarize()
	e := newCaptureEnv()
	r := e.open(t)
	r.reveal(gfx.RoleSwitch, name)
	r.clickNode(gfx.RoleSwitch, name)
	if !e.store.Values().App.TranscribeDiarize || !r.switchChecked(name) {
		t.Fatal("Windows 上开关应能打开并写入设置")
	}
	r.clickNode(gfx.RoleSwitch, name)
	if e.store.Values().App.TranscribeDiarize {
		t.Fatal("再点一次应关闭")
	}

	// 其他平台：开关禁用且显示为关（即使设置里是开的），说明文字改成不支持。
	e2 := newCaptureEnv()
	e2.platform = "darwin"
	v := nsettings.Default()
	v.App.TranscribeDiarize = true
	e2.store = sscreen.NewMemStore(v)
	r2 := e2.open(t)
	r2.reveal(gfx.RoleSwitch, name)
	before := e2.store.Updates
	r2.clickNode(gfx.RoleSwitch, name)
	if n, _ := r2.node(gfx.RoleSwitch, name); n.Checked {
		t.Errorf("非 Windows 的开关应显示为关（即使设置里是开的），得到 %+v", n)
	}
	if e2.store.Updates != before || !e2.store.Values().App.TranscribeDiarize {
		t.Error("非 Windows 点击开关不应写入设置")
	}
	if !r2.hasText(locales.SettingsTranscribeDiarizeUnavailable()) {
		t.Error("应说明本平台不支持说话人分离")
	}
}

// 「采集与转写」页挂载了 FlareSolverr 块：同一份草稿与测试连接行为（块本身的细节见 flaresolverr_test.go）。
func TestCaptureMountsFlareSolverr(t *testing.T) {
	e := newCaptureEnv()
	r := e.open(t)
	r.press(gfx.RoleSwitch, locales.WebCaptureSolverEnable())
	r.typeInto(locales.WebCaptureSolverPort(), "9100")
	r.press(gfx.RoleButton, locales.WebCaptureSolverCheck())
	waitUntil(r, "连接成功", func() bool { r.scrollToEnd(); return r.hasText(locales.WebCaptureSolverConnected()) })
	calls := e.solver.Calls()
	if len(calls) != 1 || calls[0].Port != 9100 || !calls[0].Enabled {
		t.Errorf("挂载的块应用草稿测试连接，得到 %+v", calls)
	}
	if e.store.Updates != 0 {
		t.Error("测试连接不应改动设置")
	}
	r.press(gfx.RoleButton, locales.WebCaptureSolverSave())
	if got := e.store.Values().App.FlareSolverr; !got.Enabled || got.Port != 9100 {
		t.Errorf("保存后设置 = %+v", got)
	}
}

// ── 外壳挂载与收尾 ──

func TestShellCaptureAndMCPSectionsCleanUp(t *testing.T) {
	e := newCaptureEnv()
	mcp := &servicestest.MCPFake{}
	mcp.SetConfig(services.MCPConfig{Available: true, ServerPath: `C:\gz\mcp\server.js`, Command: "node", Args: []string{`C:\gz\mcp\server.js`}})
	r := openPage(t, func(look *kit.Look) func(c ui.C) {
		sh := &sscreen.Shell{
			Look: look, Store: e.store, Guard: e.guard(), Wake: func() {}, Active: sscreen.SectionCapture,
			Solver: e.solver, CaptureTools: e.cap, MCP: mcp, Platform: "win32",
		}
		return sh.Page
	})
	waitUntil(r, "订阅下载与转写进度", func() bool {
		return e.cap.ToolSubscribers(services.ToolYtDlp) == 1 && e.cap.ToolSubscribers(services.ToolFfmpeg) == 1 && e.cap.FunasrSubscribers() == 1
	})

	// 切到 MCP：采集页的订阅全部取消；MCP 页开始读取。
	r.clickNode(gfx.RoleButton, locales.SettingsMcp())
	waitUntil(r, "采集页订阅取消", func() bool {
		return e.cap.ToolSubscribers(services.ToolYtDlp) == 0 && e.cap.ToolSubscribers(services.ToolFfmpeg) == 0 && e.cap.FunasrSubscribers() == 0
	})
	waitText(r, locales.SettingsMcpStatusReady())

	// 切回采集页：重新订阅，并先用缓存的旧值（陈旧优先）再刷新，不是每次从「检测中」起步。
	r.clickNode(gfx.RoleButton, locales.SettingsCaptureAndTranscription())
	waitUntil(r, "重新订阅", func() bool { return e.cap.ToolSubscribers(services.ToolYtDlp) == 1 })
	waitText(r, locales.SettingsCaptureStateReady())
}
