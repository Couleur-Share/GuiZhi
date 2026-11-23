package overlays

import (
	"slices"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// 更新对话框界面（L4）：检查中 / 有新版本 / 下载进度 / 已下载 / 已是最新 / 失败各阶段的画面与按钮。

func newUpdateUI(t *testing.T) *uiRig {
	t.Helper()
	r := newUIRig(t, withUpdate)
	r.screen.Update.Open()
	r.frames(3)
	return r
}

// emit 推一次更新状态并出帧。
func (r *uiRig) emit(s services.UpdateStatus) {
	r.t.Helper()
	r.updater.Emit(s)
	r.frames(3)
}

func (r *uiRig) updateOpen() bool { return r.has(gfx.RoleGroup, locales.SettingsCheckUpdate()) }

func TestUIUpdateClosedDrawsNothing(t *testing.T) {
	r := newUIRig(t, withUpdate)
	if r.updateOpen() {
		t.Fatal("没打开时不应画对话框")
	}
}

// 打开：标题、副标题里的当前版本、通道标签、页眉的刷新钮与关闭钮；打开时自动检查一次。
func TestUIUpdateOpenShowsHeaderAndChecks(t *testing.T) {
	r := newUpdateUI(t)
	if !r.updateOpen() || !r.has(gfx.RoleHeading, locales.SettingsCheckUpdate()) {
		t.Fatalf("应有标题：%q", r.session.Texts())
	}
	for _, want := range []string{locales.SettingsVersion() + ": 1.0.0", locales.SettingsStableChannel()} {
		if !r.hasText(want) {
			t.Errorf("缺少 %q；画面上有 %q", want, r.session.Texts())
		}
	}
	if !r.hasButton(locales.SettingsCheckUpdate()) || !r.hasButton(locales.CommonClose()) {
		t.Error("页眉应有刷新钮与关闭钮")
	}
	if got := len(r.updater.Checks()); got != 1 {
		t.Errorf("打开时应自动检查一次：%d", got)
	}
}

// 检查中：显示「检查中」；此时再点刷新钮不重复检查（检查调用卡住，用异步环境观察）。
func TestUIUpdateCheckingIgnoresRefreshClick(t *testing.T) {
	r := newUpdateUI(t)
	gate := make(chan struct{})
	r.updater.Gate = gate
	r.screen.Update.Env = r.asyncEnv()
	before := len(r.updater.Checks())
	r.emit(services.UpdateStatus{State: services.UpdateStateChecking})
	r.screen.Update.Check()
	r.frames(3)
	if !r.hasText(locales.SettingsChecking()) {
		t.Fatalf("应显示检查中：%q", r.session.Texts())
	}
	r.clickButton(locales.SettingsCheckUpdate())
	close(gate)
	r.wait()
	if got := len(r.updater.Checks()); got != before+1 {
		t.Errorf("检查中再点不应重复检查：%d → %d", before, got)
	}
}

// 有新版本：版本、发布日期、更新内容（Markdown）、下载与稍后。
func TestUIUpdateAvailableShowsVersionNotesAndDownloads(t *testing.T) {
	r := newUpdateUI(t)
	r.emit(services.UpdateStatus{State: services.UpdateStateAvailable, Version: "2.0.0", ReleaseDate: "2026-11-23",
		ReleaseNotes: "# Highlights\n- first change\n- second change"})
	for _, want := range []string{locales.SettingsUpdateAvailable(), locales.SettingsVersion(), "2.0.0", locales.NativeUpdateReleasedOn("2026-11-23"),
		locales.SettingsReleaseNotes(), "first change", "second change"} {
		if !r.hasText(want) {
			t.Errorf("缺少 %q；画面上有 %q", want, r.session.Texts())
		}
	}
	if !r.has(gfx.RoleHeading, "Highlights") {
		t.Error("更新说明里的标题应按 Markdown 渲染")
	}
	r.clickButton(locales.SettingsDownloadUpdate())
	if got := len(r.updater.Downloads()); got != 1 {
		t.Fatalf("应开始下载：%d", got)
	}
	if got := r.screen.Update.View().Phase; got != UpdateDownloaded || !r.hasText(locales.SettingsDownloadComplete()) {
		t.Errorf("下载调用成功返回就是下载完成（同步替身）：%v", got)
	}
}

// 「稍后安装」关闭对话框。
func TestUIUpdateLaterClosesDialog(t *testing.T) {
	r := newUpdateUI(t)
	r.emit(services.UpdateStatus{State: services.UpdateStateAvailable, Version: "2.0.0"})
	r.clickButton(locales.SettingsInstallLater())
	if r.updateOpen() || r.screen.Update.View().Open {
		t.Fatal("应关闭对话框")
	}
}

// 更新说明里的链接：http(s) 经系统浏览器打开；其他协议不打开。
func TestUIUpdateNotesLinksOnlyOpenHTTP(t *testing.T) {
	r := newUpdateUI(t)
	r.emit(services.UpdateStatus{State: services.UpdateStateAvailable, Version: "2.0.0",
		ReleaseNotes: "[safe](https://example.test/notes) and [local](file:///c:/windows/system32/calc.exe)"})
	r.click(gfx.RoleLink, "safe")
	if got := r.links(); !slices.Equal(got, []string{"https://example.test/notes"}) {
		t.Fatalf("应打开 https 链接：%v", got)
	}
	if n, ok := r.node(gfx.RoleLink, "local"); ok {
		r.clickAt(n.Bounds.X+n.Bounds.Width/2, n.Bounds.Y+n.Bounds.Height/2)
	}
	if got := r.links(); len(got) != 1 {
		t.Errorf("file: 协议链接不应被打开：%v", got)
	}
}

// 下载中：进度条与百分比，没有下载 / 安装按钮。
func TestUIUpdateDownloadingShowsProgress(t *testing.T) {
	r := newUpdateUI(t)
	r.emit(services.UpdateStatus{State: services.UpdateStateAvailable, Version: "2.0.0"})
	r.emit(services.UpdateStatus{State: services.UpdateStateDownloading, Version: "2.0.0", Percent: 42.5})
	for _, want := range []string{locales.SettingsDownloading(), "42.5%"} {
		if !r.hasText(want) {
			t.Errorf("缺少 %q；画面上有 %q", want, r.session.Texts())
		}
	}
	if r.hasButton(locales.SettingsDownloadUpdate()) || r.hasButton(locales.SettingsInstallNow()) {
		t.Error("下载中不应有下载或安装按钮")
	}
	r.emit(services.UpdateStatus{State: services.UpdateStateDownloading, Version: "2.0.0", Percent: 80})
	if !r.hasText("80.0%") {
		t.Errorf("进度应跟着推送更新：%q", r.session.Texts())
	}
}

// 已下载：立即安装（调安装服务）与打开下载文件夹。
func TestUIUpdateDownloadedInstallAndOpenFolder(t *testing.T) {
	r := newUpdateUI(t)
	r.emit(services.UpdateStatus{State: services.UpdateStateDownloaded, Version: "2.0.0"})
	if !r.hasText(locales.SettingsDownloadComplete()) || !r.hasText(locales.SettingsInstallRestartHint()) {
		t.Fatalf("应显示下载完成与重启提示：%q", r.session.Texts())
	}
	r.clickButton(locales.SettingsOpenDownloadFolder())
	if got := r.updater.DownloadOpens(); got != 1 {
		t.Errorf("应打开下载文件夹：%d", got)
	}
	r.clickButton(locales.SettingsInstallNow())
	if got := r.updater.Installs(); got != 1 {
		t.Errorf("应开始安装：%d", got)
	}
}

// 已是最新：说明当前版本。
func TestUIUpdateLatestShowsCurrentVersion(t *testing.T) {
	r := newUpdateUI(t)
	r.emit(services.UpdateStatus{State: services.UpdateStateNotAvailable})
	if !r.hasText(locales.SettingsNoUpdate()) || !r.hasText(locales.SettingsNoUpdateDesc("1.0.0")) {
		t.Errorf("应说明已是最新版本：%q", r.session.Texts())
	}
}

// 失败：原因原文、重试再检查一次、手动下载打开发布页；SHA512 校验失败时另有打开下载目录。
func TestUIUpdateFailedRetryAndManualDownload(t *testing.T) {
	r := newUpdateUI(t)
	r.emit(services.UpdateStatus{State: services.UpdateStateError, Error: "network is unreachable"})
	if !r.hasText("network is unreachable") || !r.hasText(locales.CommonError()) {
		t.Fatalf("应显示失败原因：%q", r.session.Texts())
	}
	if r.hasButton(locales.SettingsOpenDownloadFolder()) {
		t.Error("普通失败不应有打开下载目录")
	}
	before := len(r.updater.Checks())
	r.clickButton(locales.CommonRetry())
	if got := len(r.updater.Checks()); got != before+1 {
		t.Errorf("重试应再检查一次：%d → %d", before, got)
	}
	r.emit(services.UpdateStatus{State: services.UpdateStateError, Error: "network is unreachable"})
	r.clickButton(locales.SettingsManualDownload())
	if got := r.updater.ReleaseOpens(); got != 1 {
		t.Errorf("手动下载应打开发布页：%d", got)
	}
}

func TestUIUpdateSHA512FailureOffersDownloadFolder(t *testing.T) {
	r := newUpdateUI(t)
	r.emit(services.UpdateStatus{State: services.UpdateStateError, Error: "SHA512 checksum mismatch"})
	if first, _, _ := strings.Cut(locales.ErrorSha512Desc(), "\n"); !r.hasText(first) {
		t.Errorf("应换成校验失败的说明：%q", r.session.Texts())
	}
	r.clickButton(locales.SettingsOpenDownloadFolder())
	if got := r.updater.DownloadOpens(); got != 1 {
		t.Errorf("应打开下载目录：%d", got)
	}
}

// Esc 与关闭钮、点遮罩都关闭对话框。
func TestUIUpdateCloseWays(t *testing.T) {
	r := newUpdateUI(t)
	r.keys("Escape")
	if r.updateOpen() {
		t.Fatal("Esc 应关闭")
	}
	r.screen.Update.Open()
	r.frames(3)
	r.clickButton(locales.CommonClose())
	if r.updateOpen() {
		t.Fatal("关闭钮应关闭")
	}
	r.screen.Update.Open()
	r.frames(3)
	r.clickAt(10, 10)
	if r.updateOpen() {
		t.Fatal("点遮罩应关闭")
	}
}
