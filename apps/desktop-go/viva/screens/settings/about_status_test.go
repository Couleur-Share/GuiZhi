package settings_test

import (
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/locales"
)

// 状态推送里的「有新版本」只在空闲、检查中等阶段有意义：下载已经开始或已经完成之后再到的这类推送
// （自动检查的后到结果、重复事件）不能把界面倒退回「有新版本」，否则用户会看到进度消失、下载按钮重新出现，
// 点下去就是第二次下载。变异检查（M3-2a Step 3 收尾）发现这条分支此前没有用例覆盖。
func TestAboutLateAvailablePushDoesNotRewindDownload(t *testing.T) {
	upd := &servicestest.Updater{}
	upd.SetResult(services.UpdateCheckResult{Available: true, Version: "2.0.0"})
	upd.DownloadGate = make(chan struct{})
	r := openAbout(t, upd, nil)
	r.clickNode(gfx.RoleButton, locales.SettingsCheckUpdate())
	waitUntil(r.rig, "出现下载按钮", func() bool { return r.has(gfx.RoleButton, locales.SettingsDownloadUpdate()) })

	r.clickNode(gfx.RoleButton, locales.SettingsDownloadUpdate())
	upd.Emit(services.UpdateStatus{State: services.UpdateStateDownloading, Percent: 42})
	waitUntil(r.rig, "进度 42%", func() bool { return r.hasText(locales.SettingsDownloadProgress("42")) })

	// 下载中又到一条「有新版本」：进度与禁用的「下载中」按钮保持，版本说明不变。
	upd.Emit(services.UpdateStatus{State: services.UpdateStateAvailable, Version: "9.9.9"})
	r.frames(3)
	if !r.hasText(locales.SettingsDownloadProgress("42")) || r.hasText(locales.SettingsUpdateAvailableDesc("9.9.9")) {
		t.Error("下载中收到「有新版本」推送后，说明应仍是下载进度")
	}
	if n, ok := r.node(gfx.RoleButton, locales.SettingsDownloading()); !ok || !n.Disabled {
		t.Errorf("下载中应仍显示禁用的「下载中」按钮：%+v", n)
	}
	if r.has(gfx.RoleButton, locales.SettingsDownloadUpdate()) {
		t.Error("下载中不应重新出现「下载」按钮")
	}

	// 下载完成后再到一条：「立即安装」保持，下载按钮不回来。
	close(upd.DownloadGate)
	waitUntil(r.rig, "出现安装按钮", func() bool { return r.has(gfx.RoleButton, locales.SettingsInstallNow()) })
	upd.Emit(services.UpdateStatus{State: services.UpdateStateAvailable, Version: "9.9.9"})
	r.frames(3)
	if !r.has(gfx.RoleButton, locales.SettingsInstallNow()) || r.has(gfx.RoleButton, locales.SettingsDownloadUpdate()) {
		t.Error("下载完成后收到「有新版本」推送，应仍是「立即安装」，不能回到「下载」")
	}
	if got := len(upd.Downloads()); got != 1 {
		t.Errorf("整个过程只应下载一次，得到 %d", got)
	}
}
