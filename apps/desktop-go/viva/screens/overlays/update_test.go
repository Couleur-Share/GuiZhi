package overlays

import (
	"errors"
	"strings"
	"testing"
	"time"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	nsettings "guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/screens/settings"
)

type updateRig struct {
	*rig
	svc *servicestest.Updater
	u   *Update

	enabled, visible, online bool
	opts                     services.UpdateCheckOptions
}

func newUpdateRig(t *testing.T) *updateRig {
	t.Helper()
	ur := &updateRig{rig: newRig(t), svc: &servicestest.Updater{}, enabled: true, visible: true, online: true}
	u := NewUpdate(ur.svc, ur.guard, ur.invalidate)
	u.Env = ur.env()
	u.Options = func() services.UpdateCheckOptions { return ur.opts }
	u.CurrentVersion = func() string { return "1.0.0" }
	u.AutoCheckEnabled = func() bool { return ur.enabled }
	u.WindowVisible = func() bool { return ur.visible }
	u.Online = func() bool { return ur.online }
	ur.u = u
	u.Attach()
	t.Cleanup(u.Detach)
	return ur
}

func TestResolveAutoSkipReason(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000)
	base := AutoCheckContext{Trigger: services.UpdateTriggerInterval, Enabled: true, WindowVisible: true, Online: true, Now: now}
	cases := []struct {
		name string
		mod  func(*AutoCheckContext)
		want string
	}{
		{"可以检查", func(*AutoCheckContext) {}, ""},
		{"自动检查关闭", func(c *AutoCheckContext) { c.Enabled = false }, SkipDisabled},
		{"窗口隐藏", func(c *AutoCheckContext) { c.WindowVisible = false }, SkipHidden},
		{"离线", func(c *AutoCheckContext) { c.Online = false }, SkipOffline},
		{"上一次还在进行", func(c *AutoCheckContext) { c.InFlight = true }, SkipInFlight},
		{"关闭优先于隐藏", func(c *AutoCheckContext) { c.Enabled, c.WindowVisible = false, false }, SkipDisabled},
		{"周期触发不受冷却限制", func(c *AutoCheckContext) { c.LastAttemptAt = now.Add(-time.Minute) }, ""},
		{"显示触发在一小时内冷却", func(c *AutoCheckContext) {
			c.Trigger, c.LastAttemptAt = services.UpdateTriggerVisibility, now.Add(-59*time.Minute)
		}, SkipCooldown},
		{"显示触发满一小时放行", func(c *AutoCheckContext) {
			c.Trigger, c.LastAttemptAt = services.UpdateTriggerVisibility, now.Add(-time.Hour)
		}, ""},
		{"失败后 5 分钟内冷却", func(c *AutoCheckContext) {
			c.Trigger, c.LastAttemptAt, c.LastAttemptFailed = services.UpdateTriggerVisibility, now.Add(-4*time.Minute), true
		}, SkipCooldown},
		{"失败后满 5 分钟放行", func(c *AutoCheckContext) {
			c.Trigger, c.LastAttemptAt, c.LastAttemptFailed = services.UpdateTriggerVisibility, now.Add(-5*time.Minute), true
		}, ""},
		{"会话里第一次显示触发不冷却", func(c *AutoCheckContext) { c.Trigger = services.UpdateTriggerVisibility }, ""},
	}
	for _, c := range cases {
		ctx := base
		c.mod(&ctx)
		if got := ResolveAutoSkipReason(ctx); got != c.want {
			t.Errorf("%s：得到 %q，期望 %q", c.name, got, c.want)
		}
	}
}

func TestUpdateStatusStateMachine(t *testing.T) {
	ur := newUpdateRig(t)
	u, svc := ur.u, ur.svc
	if v := u.View(); v.Phase != UpdateIdle || v.PillVersion != "" || v.CurrentVersion != "1.0.0" {
		t.Fatalf("初始状态不对：%+v", v)
	}

	svc.Emit(services.UpdateStatus{State: services.UpdateStateChecking})
	if u.View().Phase != UpdateChecking {
		t.Error("应进入检查中")
	}
	svc.Emit(services.UpdateStatus{State: services.UpdateStateAvailable, Version: "2.0.0", ReleaseNotes: "notes", ReleaseDate: "2026-11-23"})
	v := u.View()
	if v.Phase != UpdateAvailable || v.Version != "2.0.0" || v.ReleaseNotes != "notes" || v.ReleaseDate != "2026-11-23" || v.PillVersion != "2.0.0" || v.PillDownloaded {
		t.Fatalf("有新版本的状态不对：%+v", v)
	}
	if rec := u.LastCheck(); rec.Outcome != settings.UpdateOutcomeAvailable || rec.Version != "2.0.0" || rec.At.IsZero() {
		t.Errorf("应记下有新版本：%+v", rec)
	}

	svc.Emit(services.UpdateStatus{State: services.UpdateStateChecking}) // 稳定的新版本状态不被瞬时的检查中冲掉
	if u.View().Phase != UpdateAvailable {
		t.Error("已有新版本时 checking 推送应被忽略")
	}

	svc.Emit(services.UpdateStatus{State: services.UpdateStateDownloading, Percent: 137})
	if v := u.View(); v.Phase != UpdateDownloading || v.Percent != 100 {
		t.Errorf("进度应夹在 0–100：%+v", v)
	}
	svc.Emit(services.UpdateStatus{State: services.UpdateStateDownloading, Percent: -5})
	if u.View().Percent != 0 {
		t.Error("负进度应夹到 0")
	}
	svc.Emit(services.UpdateStatus{State: services.UpdateStateDownloaded, Version: "2.0.0"})
	if v := u.View(); v.Phase != UpdateDownloaded || !v.PillDownloaded || v.PillVersion != "2.0.0" {
		t.Errorf("已下载状态不对：%+v", v)
	}

	svc.Emit(services.UpdateStatus{State: services.UpdateStateNotAvailable})
	if v := u.View(); v.Phase != UpdateLatest || v.PillVersion != "" || v.PillDownloaded {
		t.Errorf("已是最新应清掉顶栏提示：%+v", v)
	}
	if u.LastCheck().Outcome != settings.UpdateOutcomeLatest {
		t.Error("应记下已是最新")
	}

	svc.Emit(services.UpdateStatus{State: services.UpdateStateError, Error: ""})
	if v := u.View(); v.Phase != UpdateFailed || v.Error != locales.CommonUnknownError() {
		t.Errorf("空原因应显示未知错误：%+v", v)
	}
	svc.Emit(services.UpdateStatus{State: services.UpdateStateError, Error: "SHA512 mismatch"})
	if v := u.View(); !v.SHA512Failed() {
		t.Errorf("校验失败应被识别：%+v", v)
	}
	if rec := u.LastCheck(); rec.Outcome != settings.UpdateOutcomeError || rec.Error != "SHA512 mismatch" {
		t.Errorf("应记下失败原因：%+v", rec)
	}
	svc.Emit(services.UpdateStatus{State: "bogus"})
	if u.View().Phase != UpdateFailed {
		t.Error("未知状态应忽略")
	}
}

func TestUpdateDisplayVersionFallbacks(t *testing.T) {
	if got := (UpdateView{Version: "2", CurrentVersion: "1"}).DisplayVersion(); got != "2" {
		t.Error(got)
	}
	if got := (UpdateView{CurrentVersion: "1"}).DisplayVersion(); got != "1" {
		t.Error(got)
	}
	if got := (UpdateView{}).DisplayVersion(); got != "..." {
		t.Error(got)
	}
}

func TestUpdatePillSurvivesWithoutDialog(t *testing.T) {
	ur := newUpdateRig(t)
	ur.svc.Emit(services.UpdateStatus{State: services.UpdateStateAvailable, Version: "3.0.0"})
	v := ur.u.View()
	if v.Open || v.PillVersion != "3.0.0" {
		t.Errorf("对话框没开时顶栏提示也应记录：%+v", v)
	}
}

func TestUpdateOpenForcesCheckWithOptions(t *testing.T) {
	ur := newUpdateRig(t)
	ur.opts = services.UpdateCheckOptions{UseMirror: true, Channel: nsettings.UpdateChannel("beta")}
	ur.svc.SetResult(services.UpdateCheckResult{Available: true, Version: "2.0.0"})
	ur.u.Open()
	checks := ur.svc.Checks()
	if len(checks) != 1 || !checks[0].UseMirror || checks[0].Channel != "beta" || checks[0].Trigger != "" {
		t.Fatalf("打开应带当前选项做一次手动检查：%+v", checks)
	}
	v := ur.u.View()
	if !v.Open || v.Refreshing || !v.UseMirror || v.Channel != "beta" {
		t.Errorf("状态不对：%+v", v)
	}
	if v.Phase != UpdateAvailable || v.Version != "2.0.0" || v.PillVersion != "2.0.0" {
		t.Errorf("没有推送时应以返回值为准（不能停在检查中）：%+v", v)
	}
}

func TestUpdateCheckWithoutPushReportsLatest(t *testing.T) {
	ur := newUpdateRig(t)
	ur.u.Check()
	if v := ur.u.View(); v.Phase != UpdateLatest || v.Refreshing {
		t.Errorf("没有新版本且没有推送：%+v", v)
	}
	if ur.u.LastCheck().Outcome != settings.UpdateOutcomeLatest {
		t.Error("应记下已是最新")
	}
}

func TestUpdateCheckFailureShowsOriginalErrorWithoutPush(t *testing.T) {
	ur := newUpdateRig(t)
	ur.svc.FailCheck(errors.New("mirror unreachable"))
	ur.u.Check()
	if v := ur.u.View(); v.Phase != UpdateFailed || v.Error != "mirror unreachable" || v.Refreshing {
		t.Errorf("没有推送时应显示调用返回的错误：%+v", v)
	}
}

func TestUpdatePushedErrorWinsOverRawError(t *testing.T) {
	ur := newUpdateRig(t)
	ur.u.Env = ur.asyncEnv()
	ur.svc.Gate = make(chan struct{})
	ur.svc.FailCheck(errors.New("raw network failure"))
	ur.u.Check()
	// 主进程先推送映射过的错误，调用随后才返回原始错误
	ur.svc.Emit(services.UpdateStatus{State: services.UpdateStateError, Error: "friendly mapped reason"})
	close(ur.svc.Gate)
	ur.wait()
	if v := ur.u.View(); v.Error != "friendly mapped reason" {
		t.Errorf("推送的映射原因应保留：%q", v.Error)
	}
}

func TestUpdateCheckWhileStableKeepsDisplayAndSpinsRefresh(t *testing.T) {
	ur := newUpdateRig(t)
	ur.svc.Emit(services.UpdateStatus{State: services.UpdateStateAvailable, Version: "2.0.0"})
	ur.u.Env = ur.asyncEnv()
	ur.svc.Gate = make(chan struct{})
	ur.svc.SetResult(services.UpdateCheckResult{Available: true, Version: "2.0.0"})
	ur.u.Check()
	v := ur.u.View()
	if v.Phase != UpdateAvailable || !v.Refreshing {
		t.Errorf("已有新版本时应保留显示、只让刷新转圈：%+v", v)
	}
	ur.u.Check() // 检查中再点无效
	close(ur.svc.Gate)
	ur.wait()
	if n := len(ur.svc.Checks()); n != 1 {
		t.Errorf("检查中重复点击不应再发请求：%d", n)
	}
	if ur.u.View().Refreshing {
		t.Error("结束后应取消转圈")
	}
}

func TestUpdateOpenDoesNotRecheckDuringDownload(t *testing.T) {
	ur := newUpdateRig(t)
	ur.svc.Emit(services.UpdateStatus{State: services.UpdateStateDownloading, Percent: 40})
	ur.u.Open()
	if n := len(ur.svc.Checks()); n != 0 {
		t.Errorf("下载中打开对话框不应重新检查：%d", n)
	}
	if !ur.u.View().Open {
		t.Error("应打开对话框")
	}
	ur.u.Close()
	if ur.u.View().Open {
		t.Error("应关闭对话框")
	}
}

func TestUpdateDownloadOnlyWhenAvailable(t *testing.T) {
	ur := newUpdateRig(t)
	ur.u.Download()
	if len(ur.svc.Downloads()) != 0 {
		t.Fatal("没有新版本时不应下载")
	}
	ur.opts = services.UpdateCheckOptions{UseMirror: true}
	ur.svc.Emit(services.UpdateStatus{State: services.UpdateStateAvailable, Version: "2.0.0"})
	ur.u.Download()
	if d := ur.svc.Downloads(); len(d) != 1 || !d[0].UseMirror {
		t.Fatalf("应带选项下载一次：%+v", d)
	}
	if v := ur.u.View(); v.Phase != UpdateDownloaded || v.Percent != 100 || !v.PillDownloaded {
		t.Errorf("没有推送时下载成功返回应进入已下载：%+v", v)
	}
}

func TestUpdateDownloadFailureShowsReason(t *testing.T) {
	ur := newUpdateRig(t)
	ur.svc.Emit(services.UpdateStatus{State: services.UpdateStateAvailable, Version: "2.0.0"})
	ur.svc.FailDownload(errors.New("connection reset"))
	ur.u.Download()
	if v := ur.u.View(); v.Phase != UpdateFailed || v.Error != "connection reset" {
		t.Errorf("下载失败应显示原因：%+v", v)
	}
}

func TestUpdateInstall(t *testing.T) {
	ur := newUpdateRig(t)
	ur.u.Install()
	if ur.svc.Installs() != 0 {
		t.Fatal("没下载完不应安装")
	}
	ur.svc.Emit(services.UpdateStatus{State: services.UpdateStateDownloaded, Version: "2.0.0"})
	ur.u.Install()
	if ur.svc.Installs() != 1 || ur.u.View().Installing {
		t.Errorf("应安装一次并结束安装中：%d", ur.svc.Installs())
	}
	ur.svc.FailInstall(errors.New("installer missing"))
	ur.u.Install()
	if v := ur.u.View(); v.Phase != UpdateFailed || v.Error != "installer missing" || v.Installing {
		t.Errorf("安装失败应显示原因：%+v", v)
	}
}

func TestUpdateShellActionsReportFailures(t *testing.T) {
	ur := newUpdateRig(t)
	ur.u.OpenDownloaded()
	ur.u.OpenReleases()
	if ur.svc.DownloadOpens() != 1 || ur.svc.ReleaseOpens() != 1 || len(ur.toasts()) != 0 {
		t.Fatal("成功时应各调用一次且不弹提示")
	}
	ur.svc.FailOpenDownloaded(errors.New("folder missing"))
	ur.u.OpenDownloaded()
	if tt := ur.lastToast(); tt.Kind != kit.ToastError || !strings.Contains(tt.Detail, "folder missing") {
		t.Errorf("失败应弹出带原因的提示：%+v", tt)
	}
}

func TestUpdateNilServiceReportsUnavailable(t *testing.T) {
	r := newRig(t)
	u := NewUpdate(nil, r.guard, r.invalidate)
	u.Env = r.env()
	u.Attach()
	u.Check()
	if v := u.View(); v.Phase != UpdateFailed || v.Error == "" {
		t.Errorf("没接更新服务应如实报错：%+v", v)
	}
	if reason := u.AutoCheck(services.UpdateTriggerStartup); reason != SkipDisabled {
		t.Errorf("没开自动检查应跳过：%q", reason)
	}
}

func TestAutoCheckSkipsAreLoggedWithReason(t *testing.T) {
	ur := newUpdateRig(t)
	ur.enabled = false
	if got := ur.u.AutoCheck(services.UpdateTriggerStartup); got != SkipDisabled {
		t.Errorf("得到 %q", got)
	}
	ur.enabled, ur.visible = true, false
	if got := ur.u.AutoCheck(services.UpdateTriggerInterval); got != SkipHidden {
		t.Errorf("得到 %q", got)
	}
	ur.visible, ur.online = true, false
	if got := ur.u.AutoCheck(services.UpdateTriggerVisibility); got != SkipOffline {
		t.Errorf("得到 %q", got)
	}
	want := []servicestest.AutoSkip{
		{Trigger: "startup", Reason: "disabled"}, {Trigger: "interval", Reason: "hidden"}, {Trigger: "visibility", Reason: "offline"},
	}
	got := ur.svc.AutoSkips()
	if len(got) != len(want) {
		t.Fatalf("跳过记录 %v，期望 %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 条 %v，期望 %v", i, got[i], want[i])
		}
	}
	if len(ur.svc.Checks()) != 0 {
		t.Error("跳过时不应发请求")
	}
}

func TestAutoCheckPassesTriggerAndRecordsOutcome(t *testing.T) {
	ur := newUpdateRig(t)
	ur.opts = services.UpdateCheckOptions{UseMirror: true}
	ur.svc.SetResult(services.UpdateCheckResult{Available: false})
	if got := ur.u.AutoCheck(services.UpdateTriggerStartup); got != "" {
		t.Fatalf("应发起检查：%q", got)
	}
	checks := ur.svc.Checks()
	if len(checks) != 1 || checks[0].Trigger != "startup" || !checks[0].UseMirror {
		t.Errorf("应带触发来源与当前选项：%+v", checks)
	}
	if len(ur.toasts()) != 0 {
		t.Error("成功的自动检查不应弹提示")
	}
}

func TestAutoCheckInFlightIsSkipped(t *testing.T) {
	ur := newUpdateRig(t)
	ur.u.Env = ur.asyncEnv()
	ur.svc.Gate = make(chan struct{})
	if got := ur.u.AutoCheck(services.UpdateTriggerInterval); got != "" {
		t.Fatalf("第一次应发起：%q", got)
	}
	if got := ur.u.AutoCheck(services.UpdateTriggerInterval); got != SkipInFlight {
		t.Errorf("第二次应因在途跳过：%q", got)
	}
	close(ur.svc.Gate)
	ur.wait()
	if got := ur.u.AutoCheck(services.UpdateTriggerInterval); got != "" {
		t.Errorf("结束后应能再发起：%q", got)
	}
}

func TestAutoCheckVisibilityCooldownAndFailureRetry(t *testing.T) {
	ur := newUpdateRig(t)
	ur.u.OnVisibilityChanged(true)
	if len(ur.svc.Checks()) != 1 {
		t.Fatal("首次显示应检查")
	}
	ur.advance(30 * time.Minute)
	ur.u.OnVisibilityChanged(true)
	if len(ur.svc.Checks()) != 1 {
		t.Error("一小时内再次显示应冷却")
	}
	if sk := ur.svc.AutoSkips(); len(sk) != 1 || sk[0].Reason != SkipCooldown || sk[0].Trigger != "visibility" {
		t.Errorf("应记冷却跳过：%v", sk)
	}
	ur.u.OnVisibilityChanged(false)
	if len(ur.svc.AutoSkips()) != 1 {
		t.Error("隐藏窗口不触发自动检查")
	}

	ur.advance(31 * time.Minute)
	ur.svc.FailCheck(errors.New("dns lookup failed"))
	ur.u.OnVisibilityChanged(true) // 满一小时：放行，但这次失败
	if len(ur.svc.Checks()) != 2 {
		t.Fatal("满一小时应放行")
	}
	ur.advance(6 * time.Minute)
	ur.u.OnVisibilityChanged(true) // 失败后 5 分钟即可重试
	if len(ur.svc.Checks()) != 3 {
		t.Error("失败后 5 分钟应允许重试")
	}
}

func TestAutoCheckFailureToastsOncePerSession(t *testing.T) {
	ur := newUpdateRig(t)
	ur.svc.FailCheck(errors.New("tls handshake timeout"))
	ur.u.AutoCheck(services.UpdateTriggerStartup)
	ur.u.AutoCheck(services.UpdateTriggerInterval)
	toasts := ur.toasts()
	if len(toasts) != 1 || toasts[0].Kind != kit.ToastError || toasts[0].Message != locales.SettingsAutoUpdateCheckFailed() ||
		!strings.Contains(toasts[0].Detail, "tls handshake timeout") {
		t.Fatalf("失败只应弹一次带原因的提示：%+v", toasts)
	}
	if n := len(ur.log.entries()); n != 2 {
		t.Errorf("每次失败都应留日志：%v", ur.log.entries())
	}
	if rec := ur.u.LastCheck(); rec.Outcome != settings.UpdateOutcomeError || rec.Error != "tls handshake timeout" {
		t.Errorf("应记下失败：%+v", rec)
	}
}

func TestAutoCheckDevDisabledIsNotAFailure(t *testing.T) {
	ur := newUpdateRig(t)
	ur.svc.FailCheck(&services.UpdateDisabledError{Reason: "updater is disabled in development"})
	ur.u.AutoCheck(services.UpdateTriggerStartup)
	if len(ur.toasts()) != 0 || len(ur.log.entries()) != 0 {
		t.Errorf("开发模式禁用不是失败：toasts=%v logs=%v", ur.toasts(), ur.log.entries())
	}
	if ur.u.LastCheck().Outcome != "" {
		t.Error("不应记成失败")
	}
	// 也不应让随后的显示触发进入「失败后重试」的短冷却
	ur.advance(10 * time.Minute)
	ur.u.OnVisibilityChanged(true)
	if len(ur.svc.Checks()) != 1 {
		t.Error("禁用不是失败，10 分钟内的显示触发应仍在一小时冷却里")
	}
}

func TestUpdateStartSchedulesStartupAndInterval(t *testing.T) {
	ur := newUpdateRig(t)
	ur.u.Start()
	if ur.timers.count() != 2 || ur.timers.delay(0) != 3*time.Second || ur.timers.delay(1) != time.Hour {
		t.Fatalf("应排启动检查（3 秒）与周期检查（1 小时）：%d", ur.timers.count())
	}
	ur.timers.fire(0)
	if c := ur.svc.Checks(); len(c) != 1 || c[0].Trigger != "startup" {
		t.Errorf("启动检查：%+v", c)
	}
	ur.timers.fire(1)
	if c := ur.svc.Checks(); len(c) != 2 || c[1].Trigger != "interval" {
		t.Errorf("周期检查：%+v", c)
	}
	if ur.timers.count() != 3 || ur.timers.delay(2) != time.Hour {
		t.Errorf("周期检查后应排下一次：%d", ur.timers.count())
	}
}

func TestUpdateStopInvalidatesScheduledChecks(t *testing.T) {
	ur := newUpdateRig(t)
	ur.u.Start()
	ur.u.Detach()
	ur.timers.fire(0)
	ur.timers.fire(1)
	if len(ur.svc.Checks()) != 0 || len(ur.svc.AutoSkips()) != 0 || ur.timers.count() != 2 {
		t.Error("停止后到期的回调不应再检查或再排期")
	}
	ur.u.Start() // 重新开始：旧回调仍然无效
	ur.timers.fire(0)
	if len(ur.svc.Checks()) != 0 {
		t.Error("旧的回调不应因重新开始而复活")
	}
	ur.timers.fire(2)
	if len(ur.svc.Checks()) != 1 {
		t.Error("新一轮的启动检查应生效")
	}
}

func TestUpdateAttachIsIdempotentAndDetachUnsubscribes(t *testing.T) {
	ur := newUpdateRig(t)
	ur.u.Attach()
	if ur.svc.Subscribers() != 1 {
		t.Errorf("只应订阅一次：%d", ur.svc.Subscribers())
	}
	ur.u.Detach()
	if ur.svc.Subscribers() != 0 {
		t.Error("应取消订阅")
	}
	ur.u.Attach()
	if ur.svc.Subscribers() != 1 {
		t.Error("取消后应能重新订阅")
	}
}
