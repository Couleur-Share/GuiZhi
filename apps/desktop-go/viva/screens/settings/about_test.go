package settings_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	nsettings "guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	sscreen "guizhi-mygo-shell/viva/screens/settings"
)

// 测试里固定的「现在」：最近一次检查的时间戳按 15:04 显示。
var aboutNow = time.Date(2026, 11, 22, 10, 30, 0, 0, time.Local)

// aboutRig 是打开的「关于」页，连同它挂着的各个替身。
type aboutRig struct {
	*rig
	about  *sscreen.About
	store  *sscreen.MemStore
	env    *sysEnv
	upd    *servicestest.Updater
	record *sscreen.UpdateRecord
	opened []string // OpenExternal 收到的链接
}

// openAbout 在 1280 宽下打开「关于」页；tune 可在挂载前改 About 的字段。
func openAbout(t *testing.T, upd *servicestest.Updater, tune func(a *aboutRig)) *aboutRig {
	t.Helper()
	return openAboutAt(t, 1280, upd, tune)
}

func openAboutAt(t *testing.T, width float32, upd *servicestest.Updater, tune func(a *aboutRig)) *aboutRig {
	t.Helper()
	ar := &aboutRig{store: sscreen.NewMemStore(nsettings.Default()), env: newEnv(), upd: upd, record: &sscreen.UpdateRecord{}}
	ar.rig = openPageAt(t, width, func(look *kit.Look) func(c ui.C) {
		ar.about = &sscreen.About{
			Look: look, Store: ar.store, Guard: ar.env.guard(), Version: "1.2.3", Record: ar.record,
			Now: func() time.Time { return aboutNow }, Wake: func() {},
			OpenExternal: func(url string) error { ar.opened = append(ar.opened, url); return nil },
		}
		if upd != nil {
			ar.about.Updater = upd
		}
		if tune != nil {
			tune(ar)
		}
		t.Cleanup(ar.about.Close)
		return ar.about.Page
	})
	return ar
}

// 页面的静态内容：名称、版本加通道、支持政策、产品概览、开源链接、页脚；没有接设置引导时不显示那一行。
func TestAboutShowsIdentityAndStaticContent(t *testing.T) {
	r := openAbout(t, &servicestest.Updater{}, nil)
	for _, want := range []string{
		"GuiZhi",
		locales.SettingsCurrentVersion("1.2.3"), // 版本与通道是相邻的三段文字（版本、分隔、通道），分别判定
		locales.SettingsStableChannel(),
		locales.SettingsSupportPolicyDesc(),
		locales.SettingsProductOverviewLocalTitle(),
		locales.SettingsProductOverviewAiTitle(),
		locales.SettingsProductOverviewCaptureTitle(),
		locales.SettingsUpdateCheckPending(locales.SettingsStableChannel()),
	} {
		if !r.hasText(want) {
			t.Errorf("「关于」页应显示 %q", want)
		}
	}
	if r.hasText(locales.SetupOpenFromAbout()) {
		t.Error("没有接设置引导时不应显示那一行")
	}
	r.scrollToEnd()
	if !r.hasText("AGPL-3.0") {
		t.Error("页脚应显示许可证")
	}
}

// 版本号还没读到时显示占位「...」。
func TestAboutPlaceholderVersion(t *testing.T) {
	r := openAbout(t, &servicestest.Updater{}, func(a *aboutRig) { a.about.Version = "" })
	if !r.hasText(locales.SettingsCurrentVersion("...")) {
		t.Error("没有版本号时应显示占位")
	}
}

// 窄窗口（低于 640）改为竖排，页面仍完整可用。
func TestAboutNarrowLayout(t *testing.T) {
	r := openAboutAt(t, 560, &servicestest.Updater{}, nil)
	if !r.hasText(locales.SettingsCurrentVersion("1.2.3")) {
		t.Fatal("窄窗口下版本应可见")
	}
	// 窗口宽 560：滚动要落在窗口内（scrollToEnd 的落点在 x=700，窄窗口外）。
	scroll := func(dy float32) {
		if err := r.s.Scroll(gfx.ScrollEvent{X: 300, Y: 400, DeltaY: dy, Precise: true}); err != nil {
			t.Fatal(err)
		}
		r.frames(3)
	}
	scroll(300)
	if !r.has(gfx.RoleButton, locales.SettingsCheckUpdate()) {
		t.Error("窄窗口下检查更新按钮应可达")
	}
	scroll(4000)
	if !r.has(gfx.RoleLink, locales.SettingsVisitSoftMemo()) {
		t.Error("窄窗口下其他作品的链接应可达")
	}
}

// 设置引导：接了回调才显示那一行，点击触发回调。
func TestAboutSetupGuideRow(t *testing.T) {
	calls := 0
	r := openAbout(t, &servicestest.Updater{}, func(a *aboutRig) { a.about.OpenSetupGuide = func() { calls++ } })
	if !r.hasText(locales.SetupOpenFromAbout()) {
		t.Fatal("接了回调后应显示设置引导一行")
	}
	r.clickNode(gfx.RoleButton, locales.SetupOpenFromAbout())
	if calls != 1 {
		t.Errorf("点设置引导应触发一次回调，得到 %d", calls)
	}
}

// 自动检查与调试模式两个开关写入设置；写入失败时开关保持原状并给出带原因的提示。
func TestAboutSwitchesWriteSettings(t *testing.T) {
	r := openAbout(t, &servicestest.Updater{}, nil)
	auto := locales.SettingsAutoCheckUpdate()
	before := r.switchChecked(auto)
	r.clickNode(gfx.RoleSwitch, auto)
	if got := r.store.Values().UI.AutoCheckUpdate; got == before || r.switchChecked(auto) != got {
		t.Errorf("点自动检查后应写入并翻转：写入 %v，开关 %v", got, r.switchChecked(auto))
	}

	r.store.FailNext = errors.New("read-only profile")
	r.clickNode(gfx.RoleSwitch, auto)
	if r.store.Values().UI.AutoCheckUpdate == before {
		t.Fatal("写入失败时设置不应改变")
	}
	if errs := r.env.toasts(kit.ToastError); len(errs) != 1 || !strings.Contains(errs[0].Detail, "read-only profile") {
		t.Errorf("应弹一条带原因的错误提示，得到 %+v", errs)
	}

	r.scrollToEnd()
	debug := locales.SettingsDebugMode()
	r.clickNode(gfx.RoleSwitch, debug)
	if !r.store.Values().UI.DebugMode || !r.switchChecked(debug) {
		t.Error("点调试模式后应打开并写入")
	}
}

// 预览通道：打开要先确认，确认前设置不变；取消不写入；确认后切到预览并标记为显式选择；关掉直接回稳定版。
func TestAboutPreviewChannelConfirmFlow(t *testing.T) {
	r := openAbout(t, &servicestest.Updater{}, nil)
	name := locales.SettingsJoinPreviewChannel()
	title := locales.SettingsPreviewChannelConfirmTitle()

	r.clickNode(gfx.RoleSwitch, name)
	if !r.has(gfx.RoleGroup, title) {
		t.Fatal("打开预览通道应先弹确认对话框")
	}
	if prefs := r.store.Values().UI; prefs.UpdateChannel == nsettings.ChannelPreview || prefs.UpdateChannelExplicitlySet {
		t.Fatal("确认之前设置不应改变")
	}
	for _, want := range []string{
		locales.SettingsPreviewChannelWarning(), locales.SettingsPreviewChannelConfirmRisk(),
		locales.SettingsPreviewChannelConfirmBackup(), locales.SettingsPreviewChannelConfirmConsent(),
	} {
		// 段落在对话框里会折行，按开头几个字核对。
		if !r.hasText(string([]rune(want)[:8])) {
			t.Errorf("确认框应说明 %q", want)
		}
	}

	r.clickNode(gfx.RoleButton, locales.SettingsPreviewChannelConfirmCancel())
	if r.has(gfx.RoleGroup, title) || r.store.Values().UI.UpdateChannel == nsettings.ChannelPreview || r.store.Updates != 0 {
		t.Fatal("取消应关闭对话框且不写设置")
	}

	r.clickNode(gfx.RoleSwitch, name)
	r.clickNode(gfx.RoleButton, locales.SettingsPreviewChannelConfirmEnable())
	prefs := r.store.Values().UI
	if prefs.UpdateChannel != nsettings.ChannelPreview || !prefs.UpdateChannelExplicitlySet {
		t.Fatalf("确认后应切到预览通道并标记显式选择：%+v", prefs)
	}
	if !r.switchChecked(name) || !r.hasText(locales.SettingsCurrentVersion("1.2.3")) || !r.hasText(locales.SettingsPreviewChannel()) {
		t.Error("切换后开关应打开，版本行应显示预览通道")
	}

	r.clickNode(gfx.RoleSwitch, name)
	if r.has(gfx.RoleGroup, title) {
		t.Fatal("关闭预览通道不需要确认")
	}
	if prefs = r.store.Values().UI; prefs.UpdateChannel != nsettings.ChannelStable || !prefs.UpdateChannelExplicitlySet {
		t.Errorf("关闭后应回到稳定通道：%+v", prefs)
	}
}

// 预览通道写入失败：对话框关闭，设置不变，提示带原因。
func TestAboutPreviewChannelWriteFailure(t *testing.T) {
	r := openAbout(t, &servicestest.Updater{}, nil)
	r.clickNode(gfx.RoleSwitch, locales.SettingsJoinPreviewChannel())
	r.store.FailNext = errors.New("no space")
	r.clickNode(gfx.RoleButton, locales.SettingsPreviewChannelConfirmEnable())
	if r.store.Values().UI.UpdateChannel == nsettings.ChannelPreview {
		t.Error("写入失败时不应切到预览通道")
	}
	if errs := r.env.toasts(kit.ToastError); len(errs) != 1 || !strings.Contains(errs[0].Detail, "no space") {
		t.Errorf("应弹一条带原因的错误提示，得到 %+v", errs)
	}
}

// 检查更新：期间按钮禁用并转圈；有新版本时出现「查看发布页」与「下载更新」；检查带上设置里的镜像与通道；结果记入最近一次检查。
func TestAboutCheckFindsUpdate(t *testing.T) {
	upd := &servicestest.Updater{}
	upd.SetResult(services.UpdateCheckResult{Available: true, Version: "2.0.0"})
	upd.Gate = make(chan struct{})
	r := openAbout(t, upd, nil)
	if err := r.store.Update(func(v *nsettings.Values) { v.UI.UseUpdateMirror = true }); err != nil {
		t.Fatal(err)
	}

	r.clickNode(gfx.RoleButton, locales.SettingsCheckUpdate())
	if n, ok := r.node(gfx.RoleButton, locales.SettingsChecking()); !ok || !n.Disabled {
		t.Fatalf("检查期间应显示禁用的「检查中」按钮：%+v", n)
	}
	if !r.hasText(locales.SettingsChecking()) {
		t.Error("检查期间说明应写「检查中」")
	}
	close(upd.Gate)
	waitUntil(r.rig, "出现下载按钮", func() bool { return r.has(gfx.RoleButton, locales.SettingsDownloadUpdate()) })

	if !r.has(gfx.RoleButton, locales.SettingsOpenReleasesPage()) || !r.hasText(locales.SettingsUpdateAvailableDesc("2.0.0")) {
		t.Error("有新版本时应提示版本并给出「查看发布页」")
	}
	checks := upd.Checks()
	if len(checks) != 1 || !checks[0].UseMirror || checks[0].Channel != nsettings.ChannelStable {
		t.Errorf("检查应带上设置里的镜像与通道：%+v", checks)
	}
	if want := (sscreen.UpdateRecord{At: aboutNow, Outcome: sscreen.UpdateOutcomeAvailable, Version: "2.0.0"}); *r.record != want {
		t.Errorf("最近一次检查记录 = %+v，期望 %+v", *r.record, want)
	}
	if errs := r.env.toasts(kit.ToastError); len(errs) != 0 {
		t.Errorf("成功的检查不应弹错误：%+v", errs)
	}
}

// 已是最新：说明写明当前版本，记录为 latest，按钮回到「检查更新」。
func TestAboutCheckLatest(t *testing.T) {
	upd := &servicestest.Updater{}
	r := openAbout(t, upd, nil)
	r.clickNode(gfx.RoleButton, locales.SettingsCheckUpdate())
	waitUntil(r.rig, "已是最新", func() bool { return r.hasText(locales.SettingsNoUpdateDesc("1.2.3")) })
	if r.record.Outcome != sscreen.UpdateOutcomeLatest || !r.has(gfx.RoleButton, locales.SettingsCheckUpdate()) {
		t.Errorf("已是最新时应记 latest 并保留检查按钮：%+v", *r.record)
	}
}

// 检查失败：错误提示带原因，按钮恢复可重试，最近一次检查里写明失败原因。
func TestAboutCheckFailureShowsDetailAndRetries(t *testing.T) {
	upd := &servicestest.Updater{}
	upd.FailCheck(errors.New("GitHub API rate limited"))
	r := openAbout(t, upd, nil)
	r.clickNode(gfx.RoleButton, locales.SettingsCheckUpdate())
	waitUntil(r.rig, "失败提示", func() bool { return len(r.env.toasts(kit.ToastError)) == 1 })

	errs := r.env.toasts(kit.ToastError)
	if errs[0].Message != locales.SettingsUpdateCheckFailed() || !strings.Contains(errs[0].Detail, "GitHub API rate limited") {
		t.Errorf("失败提示应带原因：%+v", errs[0])
	}
	if got := r.env.logs.all(); len(got) != 1 || !strings.Contains(got[0], "GitHub API rate limited") {
		t.Errorf("失败应记一条带原因的日志，得到 %q", got)
	}
	if !r.has(gfx.RoleButton, locales.SettingsCheckUpdate()) {
		t.Fatal("失败后应恢复「检查更新」按钮")
	}
	want := locales.SettingsLastCheckFailed("10:30") + "（GitHub API rate limited）"
	if !r.hasText(want) {
		t.Errorf("最近一次检查应写明失败原因 %q", want)
	}

	upd.FailCheck(nil)
	r.clickNode(gfx.RoleButton, locales.SettingsCheckUpdate())
	waitUntil(r.rig, "重试成功", func() bool { return r.hasText(locales.SettingsNoUpdateDesc("1.2.3")) })
}

// 下载：期间显示进度，完成后出现「立即安装」；点安装调用服务。
func TestAboutDownloadAndInstall(t *testing.T) {
	upd := &servicestest.Updater{}
	upd.SetResult(services.UpdateCheckResult{Available: true, Version: "2.0.0"})
	upd.DownloadGate = make(chan struct{})
	r := openAbout(t, upd, nil)
	r.clickNode(gfx.RoleButton, locales.SettingsCheckUpdate())
	waitUntil(r.rig, "出现下载按钮", func() bool { return r.has(gfx.RoleButton, locales.SettingsDownloadUpdate()) })

	r.clickNode(gfx.RoleButton, locales.SettingsDownloadUpdate())
	if n, ok := r.node(gfx.RoleButton, locales.SettingsDownloading()); !ok || !n.Disabled {
		t.Fatalf("下载期间应显示禁用的「下载中」按钮：%+v", n)
	}
	upd.Emit(services.UpdateStatus{State: services.UpdateStateDownloading, Percent: 42})
	waitUntil(r.rig, "进度 42%", func() bool { return r.hasText(locales.SettingsDownloadProgress("42")) })

	close(upd.DownloadGate)
	waitUntil(r.rig, "出现安装按钮", func() bool { return r.has(gfx.RoleButton, locales.SettingsInstallNow()) })
	if !r.hasText(locales.SettingsDownloadComplete()) {
		t.Error("下载完成后说明应写「下载完成」")
	}
	if got := len(upd.Downloads()); got != 1 {
		t.Errorf("应调用一次下载，得到 %d", got)
	}

	r.clickNode(gfx.RoleButton, locales.SettingsInstallNow())
	waitUntil(r.rig, "调用安装", func() bool { return upd.Installs() == 1 })
}

// 下载失败：回到「有新版本」，错误提示带原因，可以再点一次重试。
func TestAboutDownloadFailureCanRetry(t *testing.T) {
	upd := &servicestest.Updater{}
	upd.SetResult(services.UpdateCheckResult{Available: true, Version: "2.0.0"})
	upd.FailDownload(errors.New("checksum mismatch"))
	r := openAbout(t, upd, nil)
	r.clickNode(gfx.RoleButton, locales.SettingsCheckUpdate())
	waitUntil(r.rig, "出现下载按钮", func() bool { return r.has(gfx.RoleButton, locales.SettingsDownloadUpdate()) })

	r.clickNode(gfx.RoleButton, locales.SettingsDownloadUpdate())
	waitUntil(r.rig, "下载失败提示", func() bool { return len(r.env.toasts(kit.ToastError)) == 1 })
	if errs := r.env.toasts(kit.ToastError); !strings.Contains(errs[0].Detail, "checksum mismatch") {
		t.Errorf("下载失败提示应带原因：%+v", errs[0])
	}
	waitUntil(r.rig, "恢复下载按钮", func() bool { return r.has(gfx.RoleButton, locales.SettingsDownloadUpdate()) })

	upd.FailDownload(nil)
	r.clickNode(gfx.RoleButton, locales.SettingsDownloadUpdate())
	waitUntil(r.rig, "重试后下载完成", func() bool { return r.has(gfx.RoleButton, locales.SettingsInstallNow()) })
}

// 安装失败：错误提示带原因（外壳没能启动安装程序）。
func TestAboutInstallFailureShowsDetail(t *testing.T) {
	upd := &servicestest.Updater{}
	upd.SetResult(services.UpdateCheckResult{Available: true, Version: "2.0.0"})
	upd.FailInstall(errors.New("installer missing"))
	r := openAbout(t, upd, nil)
	r.clickNode(gfx.RoleButton, locales.SettingsCheckUpdate())
	waitUntil(r.rig, "出现下载按钮", func() bool { return r.has(gfx.RoleButton, locales.SettingsDownloadUpdate()) })
	r.clickNode(gfx.RoleButton, locales.SettingsDownloadUpdate())
	waitUntil(r.rig, "出现安装按钮", func() bool { return r.has(gfx.RoleButton, locales.SettingsInstallNow()) })

	r.clickNode(gfx.RoleButton, locales.SettingsInstallNow())
	waitUntil(r.rig, "安装失败提示", func() bool { return len(r.env.toasts(kit.ToastError)) == 1 })
	if errs := r.env.toasts(kit.ToastError); !strings.Contains(errs[0].Detail, "installer missing") {
		t.Errorf("安装失败提示应带原因：%+v", errs[0])
	}
}

// 「查看发布页」调用服务打开发布页；失败时提示带原因。
func TestAboutOpenReleases(t *testing.T) {
	upd := &servicestest.Updater{}
	upd.SetResult(services.UpdateCheckResult{Available: true, Version: "2.0.0"})
	r := openAbout(t, upd, nil)
	r.clickNode(gfx.RoleButton, locales.SettingsCheckUpdate())
	waitUntil(r.rig, "出现查看发布页", func() bool { return r.has(gfx.RoleButton, locales.SettingsOpenReleasesPage()) })

	r.clickNode(gfx.RoleButton, locales.SettingsOpenReleasesPage())
	if upd.ReleaseOpens() != 1 {
		t.Errorf("应打开一次发布页，得到 %d", upd.ReleaseOpens())
	}
	if errs := r.env.toasts(kit.ToastError); len(errs) != 0 {
		t.Errorf("正常打开不应弹错误：%+v", errs)
	}
}

// 没有接更新服务：检查如实报错，按钮恢复。
func TestAboutWithoutUpdaterReportsFailure(t *testing.T) {
	r := openAbout(t, nil, nil)
	r.clickNode(gfx.RoleButton, locales.SettingsCheckUpdate())
	waitUntil(r.rig, "失败提示", func() bool { return len(r.env.toasts(kit.ToastError)) == 1 })
	if errs := r.env.toasts(kit.ToastError); errs[0].Detail == "" {
		t.Errorf("没有更新服务时提示应说明原因：%+v", errs[0])
	}
	if !r.has(gfx.RoleButton, locales.SettingsCheckUpdate()) {
		t.Error("失败后应恢复检查按钮")
	}
}

// 外链：每个链接按钮用系统浏览器打开确切的地址。
func TestAboutLinksOpenExternal(t *testing.T) {
	r := openAbout(t, &servicestest.Updater{}, nil)
	r.scrollToEnd()
	const repo = "https://github.com/Couleur-Share/GuiZhi"
	for _, c := range []struct{ name, url string }{
		{locales.SettingsOpenUserGuide(), repo + "/blob/main/docs/README.md"},
		{locales.SettingsOpenChangelog(), repo + "/blob/main/CHANGELOG.md"},
		{"github.com/Couleur-Share/GuiZhi", repo},
		{locales.SettingsSubmitIssue(), repo + "/issues/new/choose"},
		{"@Couleur-Share GitHub", "https://github.com/Couleur-Share"},
		{locales.SettingsVisitSoftMemo(), "https://couleurapp.com/"},
	} {
		before := len(r.opened)
		r.clickNode(gfx.RoleLink, c.name)
		if len(r.opened) != before+1 || r.opened[len(r.opened)-1] != c.url {
			t.Errorf("点「%s」应打开 %s，实际打开了 %v", c.name, c.url, r.opened[before:])
		}
	}
	if errs := r.env.toasts(kit.ToastError); len(errs) != 0 {
		t.Errorf("正常打开不应弹错误：%+v", errs)
	}
}

// 打开链接失败：提示带原因；没有接打开器时同样如实报错。
func TestAboutLinkFailures(t *testing.T) {
	r := openAbout(t, &servicestest.Updater{}, func(a *aboutRig) {
		a.about.OpenExternal = func(string) error { return errors.New("no default browser") }
	})
	r.scrollToEnd()
	r.clickNode(gfx.RoleLink, locales.SettingsOpenUserGuide())
	if errs := r.env.toasts(kit.ToastError); len(errs) != 1 || !strings.Contains(errs[0].Detail, "no default browser") {
		t.Errorf("打开失败应弹带原因的提示，得到 %+v", errs)
	}

	r = openAbout(t, &servicestest.Updater{}, func(a *aboutRig) { a.about.OpenExternal = nil })
	r.scrollToEnd()
	r.clickNode(gfx.RoleLink, locales.SettingsOpenUserGuide())
	if errs := r.env.toasts(kit.ToastError); len(errs) != 1 || errs[0].Detail == "" {
		t.Errorf("没有接打开器时应说明原因，得到 %+v", errs)
	}
}

// 作者邮箱：点一下复制到剪贴板并弹「已复制」；剪贴板失败时提示带原因。
func TestAboutCopyEmail(t *testing.T) {
	r := openAbout(t, &servicestest.Updater{}, nil)
	r.scrollToEnd()
	r.clickNode(gfx.RoleButton, "couleurapp@gmail.com Email")
	if len(r.env.copied) != 1 || r.env.copied[0] != "couleurapp@gmail.com" {
		t.Fatalf("应复制作者邮箱，得到 %v", r.env.copied)
	}
	if ok := r.env.toasts(kit.ToastSuccess); len(ok) != 1 || ok[0].Message != locales.ToastCopied() {
		t.Errorf("应弹「已复制」，得到 %+v", ok)
	}

	r.env.clipErr = errors.New("clipboard busy")
	r.clickNode(gfx.RoleButton, "couleurapp@gmail.com Email")
	if errs := r.env.toasts(kit.ToastError); len(errs) != 1 || !strings.Contains(errs[0].Detail, "clipboard busy") {
		t.Errorf("复制失败应弹带原因的提示，得到 %+v", errs)
	}
}

// 页面关闭后取消订阅，进行中的检查结果不再落到界面上。
func TestAboutCloseUnsubscribes(t *testing.T) {
	upd := &servicestest.Updater{}
	upd.Gate = make(chan struct{})
	r := openAbout(t, upd, nil)
	if upd.Subscribers() != 1 {
		t.Fatalf("打开后应订阅一次状态推送，得到 %d", upd.Subscribers())
	}
	r.clickNode(gfx.RoleButton, locales.SettingsCheckUpdate())
	r.about.Close()
	close(upd.Gate)
	time.Sleep(20 * time.Millisecond)
	if upd.Subscribers() != 0 {
		t.Errorf("关闭后应取消订阅，得到 %d", upd.Subscribers())
	}
	if r.record.Outcome != "" {
		t.Errorf("关闭后到达的检查结果不应再记录：%+v", *r.record)
	}
}

// 外壳：三个分区都已挂上，切分区即卸载重挂（订阅随之取消、再进来重新订阅）；检查记录跨分区保留。
func TestShellMountsSystemSections(t *testing.T) {
	upd := &servicestest.Updater{}
	upd.SetResult(services.UpdateCheckResult{Available: true, Version: "2.0.0"})
	env := newEnv()
	store := sscreen.NewMemStore(nsettings.Default())
	r := openPage(t, func(look *kit.Look) func(c ui.C) {
		sh := &sscreen.Shell{
			Look: look, Store: store, Guard: env.guard(), Wake: func() {}, Active: sscreen.SectionAbout,
			Solver: &servicestest.Solver{}, Shortcuts: &servicestest.Shortcuts{}, Updater: upd,
			AppVersion: "1.2.3", OpenExternal: func(string) error { return nil },
			Now: func() time.Time { return aboutNow },
		}
		return sh.Page
	})
	if !r.hasText(locales.SettingsCurrentVersion("1.2.3")) || upd.Subscribers() != 1 {
		t.Fatalf("默认应挂着「关于」并订阅更新推送（订阅 %d）", upd.Subscribers())
	}
	r.clickNode(gfx.RoleButton, locales.SettingsCheckUpdate())
	waitUntil(r, "有新版本", func() bool { return r.has(gfx.RoleButton, locales.SettingsDownloadUpdate()) })

	r.clickNode(gfx.RoleButton, locales.SettingsShortcuts())
	if upd.Subscribers() != 0 {
		t.Errorf("切走「关于」后应取消订阅，得到 %d", upd.Subscribers())
	}
	if !r.hasText(locales.SettingsShortcutShowApp()) {
		t.Error("切到「快捷键」后应显示快捷键列表")
	}
	r.clickNode(gfx.RoleButton, locales.SettingsNetwork())
	if !r.has(gfx.RoleCombobox, locales.SettingsNetworkProxyMode()) {
		t.Error("切到「网络」后应显示代理模式")
	}
	r.clickNode(gfx.RoleButton, locales.SettingsAbout())
	if upd.Subscribers() != 1 || !r.has(gfx.RoleButton, locales.SettingsCheckUpdate()) {
		t.Errorf("回到「关于」应重新订阅并回到空闲（订阅 %d）", upd.Subscribers())
	}
	if !r.hasText(locales.SettingsLastCheckAvailable("10:30", "2.0.0")) {
		t.Error("最近一次检查应跨分区保留")
	}
}
