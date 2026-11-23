package overlays

// 更新对话框（UpdateDialog.tsx）与自动检查更新（App.tsx 的 checkForUpdates、update-check.ts）的状态层。
//
// 两件事共用一份状态：对话框显示的更新状态机（检查中 / 有新版本 / 下载中 / 已下载待重启 / 已是最新 / 失败），
// 顶栏「更新」按钮的提示（Pill：有可用或已下载的新版本）。它们都来自同一条 updater:status 推送，
// 渲染层是两个监听器各记各的，这里合并成一个。

import (
	"errors"
	"strings"
	"sync"
	"time"

	"guizhi-mygo-shell/native/services"
	nsettings "guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/screens/settings"
)

// UpdatePhase 是更新对话框所处的阶段。
type UpdatePhase uint8

const (
	// UpdateIdle 还没有任何状态：对话框显示版本号与「检查更新」按钮。
	UpdateIdle UpdatePhase = iota
	UpdateChecking
	UpdateAvailable
	UpdateLatest
	UpdateDownloading
	UpdateDownloaded
	UpdateFailed
)

// 自动检查的节奏（update-check.ts）。
const (
	// AutoCheckInterval 是周期检查的间隔，也是窗口重新显示时补检的冷却时间。
	AutoCheckInterval = time.Hour
	// AutoCheckRetryInterval 是上次检查失败后允许提前补检的最小间隔。
	AutoCheckRetryInterval = 5 * time.Minute
	// autoCheckStartupDelay 是启动后首次检查的延迟（App.tsx 的 3000 ms）。
	autoCheckStartupDelay = 3 * time.Second
)

// 自动检查被跳过的原因（updater:logAutoSkip 的白名单）。
const (
	SkipDisabled = "disabled"
	SkipHidden   = "hidden"
	SkipOffline  = "offline"
	SkipInFlight = "in-flight"
	SkipCooldown = "cooldown"
)

// AutoCheckContext 是判定一次自动检查该不该发起的输入（AutoUpdateCheckContext）。
type AutoCheckContext struct {
	Trigger       string
	Enabled       bool
	WindowVisible bool
	Online        bool
	InFlight      bool
	Now           time.Time
	// LastAttemptAt 是上次实际发起检查的时间；零值表示本次会话还没检查过。
	LastAttemptAt     time.Time
	LastAttemptFailed bool
}

// ResolveAutoSkipReason 判定自动检查要不要跳过：返回跳过原因（Skip*），空串表示应当真的发起一次检查。
// 窗口每次显示都补检，会在托盘频繁切换时打成连击，所以 visibility 触发按上次结果给冷却窗口；
// 失败过就允许更早重试（否则一次网络抖动会静默吞掉整整一小时的更新）。
func ResolveAutoSkipReason(c AutoCheckContext) string {
	switch {
	case !c.Enabled:
		return SkipDisabled
	case !c.WindowVisible:
		return SkipHidden
	case !c.Online:
		return SkipOffline
	case c.InFlight:
		return SkipInFlight
	}
	if c.Trigger == services.UpdateTriggerVisibility && !c.LastAttemptAt.IsZero() {
		cooldown := AutoCheckInterval
		if c.LastAttemptFailed {
			cooldown = AutoCheckRetryInterval
		}
		if c.Now.Sub(c.LastAttemptAt) < cooldown {
			return SkipCooldown
		}
	}
	return ""
}

// UpdateView 是更新对话框与顶栏提示的快照。
type UpdateView struct {
	Open  bool
	Phase UpdatePhase
	// Version 在有新版本 / 已下载时是新版本号；可能为空（此时用 DisplayVersion）。
	Version      string
	ReleaseNotes string
	ReleaseDate  string
	// Percent 是下载进度，夹在 0–100。
	Percent float64
	// Error 在失败时是原因原文。
	Error string
	// CurrentVersion 是当前应用版本；UseMirror 与 Channel 是最近一次检查用的镜像与通道。
	CurrentVersion string
	UseMirror      bool
	Channel        nsettings.UpdateChannel
	// Refreshing 为真表示一次检查在进行：刷新按钮禁用并转圈。Installing 为真表示正在启动安装程序。
	Refreshing bool
	Installing bool

	// PillVersion 非空表示顶栏「更新」按钮要显示：有可用或已下载的新版本。PillDownloaded 为真是已下载（待重启安装）。
	PillVersion    string
	PillDownloaded bool
}

// DisplayVersion 是新版本号的显示：状态里没带版本号时退回当前版本，再没有就是省略号（渲染层 getStatusVersion）。
func (v UpdateView) DisplayVersion() string {
	switch {
	case v.Version != "":
		return v.Version
	case v.CurrentVersion != "":
		return v.CurrentVersion
	}
	return "..."
}

// Stable 报告当前显示的是「有新版本 / 已下载」：再次检查时保留它，不闪回「检查中」（渲染层 isStableUpgradeState）。
func (v UpdateView) Stable() bool { return v.Phase == UpdateAvailable || v.Phase == UpdateDownloaded }

// SHA512Failed 报告失败原因是安装包校验不通过：界面此时给「打开下载目录」按钮，让用户手动处理。
func (v UpdateView) SHA512Failed() bool {
	return v.Phase == UpdateFailed && strings.Contains(v.Error, "SHA512")
}

// Update 管理更新对话框、顶栏提示与自动检查。方法可从任意 goroutine 调用。
type Update struct {
	Env
	// Options 返回此刻的检查选项（设置里的镜像与更新通道）；每次检查现读。nil 取零值（稳定通道、不用镜像）。
	Options func() services.UpdateCheckOptions
	// CurrentVersion 返回当前应用版本；nil 或空串时对话框显示省略号。
	CurrentVersion func() string
	// AutoCheckEnabled 报告设置里的「自动检查更新」是否开启；nil 按关闭处理。
	AutoCheckEnabled func() bool
	// WindowVisible 报告窗口此刻是否可见（托盘里隐藏时跳过自动检查）；nil 按可见处理。
	WindowVisible func() bool
	// Online 报告此刻是否联网；nil 按联网处理。
	Online func() bool

	svc        services.Updater
	guard      kit.Guard
	invalidate func()

	mu         sync.Mutex
	view       UpdateView
	unsub      func()
	lastCheck  settings.UpdateRecord
	autoAt     time.Time
	autoFailed bool
	autoBusy   bool
	notified   bool   // 本次会话是否已为自动检查失败弹过提示
	sched      uint64 // 定时器的代数：Stop 与重新 Start 使旧的回调失效
}

// NewUpdate 创建更新状态层；svc 为 nil 表示外壳没接更新服务（检查时如实报错）。
func NewUpdate(svc services.Updater, guard kit.Guard, invalidate func()) *Update {
	return &Update{svc: svc, guard: guard, invalidate: invalidate}
}

// View 返回当前快照。
func (u *Update) View() UpdateView {
	u.mu.Lock()
	defer u.mu.Unlock()
	v := u.view
	if u.CurrentVersion != nil {
		v.CurrentVersion = u.CurrentVersion()
	}
	return v
}

// LastCheck 返回最近一次检查的记录（关于页「上次检查…」那一行用）：自动检查没有新版本或失败时界面不变，这是它唯一的可见痕迹。
// 零值表示本次会话还没检查过。
func (u *Update) LastCheck() settings.UpdateRecord {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.lastCheck
}

func (u *Update) wake() { wake(u.invalidate) }

var errUpdaterUnavailable = errors.New("updater is not available")

// Attach 订阅更新状态推送（只订一次）。应用启动时调用，之后对话框开不开都在记录状态与顶栏提示。
func (u *Update) Attach() {
	u.mu.Lock()
	if u.unsub != nil || u.svc == nil {
		u.mu.Unlock()
		return
	}
	u.unsub = func() {} // 占位：订阅在锁外做，避免回调里重入
	u.mu.Unlock()
	cancel := u.svc.OnStatus(u.OnStatus)
	u.mu.Lock()
	u.unsub = cancel
	u.mu.Unlock()
}

// Detach 取消订阅并停掉定时检查。
func (u *Update) Detach() {
	u.mu.Lock()
	cancel := u.unsub
	u.unsub = nil
	u.sched++
	u.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// OnStatus 并入一条更新状态推送。检查过程里的瞬时「checking」在已显示新版本时被忽略（否则界面会闪回转圈）。
func (u *Update) OnStatus(st services.UpdateStatus) {
	u.mu.Lock()
	v := &u.view
	if st.State == services.UpdateStateChecking && v.Stable() {
		u.mu.Unlock()
		return
	}
	at := u.now()
	switch st.State {
	case services.UpdateStateChecking:
		v.Phase = UpdateChecking
	case services.UpdateStateAvailable:
		v.Phase, v.Version, v.ReleaseNotes, v.ReleaseDate, v.Error = UpdateAvailable, st.Version, st.ReleaseNotes, st.ReleaseDate, ""
		v.PillVersion, v.PillDownloaded = st.Version, false
		u.lastCheck = settings.UpdateRecord{At: at, Outcome: settings.UpdateOutcomeAvailable, Version: st.Version}
	case services.UpdateStateNotAvailable:
		v.Phase, v.Error = UpdateLatest, ""
		v.PillVersion, v.PillDownloaded = "", false
		u.lastCheck = settings.UpdateRecord{At: at, Outcome: settings.UpdateOutcomeLatest}
	case services.UpdateStateDownloading:
		v.Phase, v.Percent = UpdateDownloading, clampPercent(st.Percent)
	case services.UpdateStateDownloaded:
		v.Phase, v.Version, v.ReleaseNotes, v.ReleaseDate, v.Error = UpdateDownloaded, st.Version, st.ReleaseNotes, st.ReleaseDate, ""
		v.PillVersion, v.PillDownloaded = st.Version, true
	case services.UpdateStateError:
		v.Phase, v.Error = UpdateFailed, errorOrUnknown(st.Error)
		u.lastCheck = settings.UpdateRecord{At: at, Outcome: settings.UpdateOutcomeError, Error: v.Error}
	default:
		u.mu.Unlock()
		return
	}
	if st.State != services.UpdateStateChecking {
		v.Refreshing = false
	}
	u.mu.Unlock()
	u.wake()
}

func clampPercent(p float64) float64 {
	if p != p { // NaN
		return 0
	}
	return min(max(p, 0), 100)
}

func errorOrUnknown(s string) string {
	if strings.TrimSpace(s) == "" {
		return locales.CommonUnknownError()
	}
	return s
}

// Open 打开对话框并强制重新检查一次（不用缓存）；已经显示着新版本的保留显示、后台刷新。
// 下载或安装在进行时只打开对话框，不重新检查（检查会打断进度）。
func (u *Update) Open() {
	u.mu.Lock()
	u.view.Open = true
	busy := u.view.Phase == UpdateDownloading || u.view.Installing
	u.mu.Unlock()
	u.wake()
	if !busy {
		u.Check()
	}
}

// Close 关闭对话框（进行中的检查与下载不受影响，状态照常更新）。
func (u *Update) Close() {
	u.mu.Lock()
	u.view.Open = false
	u.mu.Unlock()
	u.wake()
}

func (u *Update) options() services.UpdateCheckOptions {
	if u.Options != nil {
		return u.Options()
	}
	return services.UpdateCheckOptions{}
}

// Check 手动检查更新（打开对话框、刷新按钮、「检查更新」按钮）。检查中再点无效；
// 已显示新版本时保留显示、只让刷新按钮转圈。检查结果经状态推送并入，这里只处理没有推送的失败。
func (u *Update) Check() {
	u.mu.Lock()
	if u.view.Refreshing || u.view.Phase == UpdateDownloading || u.view.Installing {
		u.mu.Unlock()
		return
	}
	opts := u.options()
	u.view.UseMirror, u.view.Channel = opts.UseMirror, opts.Channel
	u.view.Refreshing = true
	if !u.view.Stable() {
		u.view.Phase, u.view.Error = UpdateChecking, ""
	}
	u.mu.Unlock()
	u.wake()

	u.spawn(func() {
		res, err := u.callCheck(opts)
		u.mu.Lock()
		u.view.Refreshing = false // 成功时推送通常已经清过；没有推送的成功也不能让按钮一直转圈
		switch {
		case err != nil:
			u.failLocked(err)
		case u.view.Phase == UpdateChecking:
			// 调用成功却没有等到结果推送：以返回值为准，不能让对话框停在「检查中」
			u.applyResultLocked(res)
		}
		u.mu.Unlock()
		u.wake()
	})
}

// applyResultLocked 把一次检查的返回值并入状态（推送缺失时的兜底，行为与对应的推送一致）。
func (u *Update) applyResultLocked(res services.UpdateCheckResult) {
	at := u.now()
	if res.Available {
		u.view.Phase, u.view.Version, u.view.Error = UpdateAvailable, res.Version, ""
		u.view.PillVersion, u.view.PillDownloaded = res.Version, false
		u.lastCheck = settings.UpdateRecord{At: at, Outcome: settings.UpdateOutcomeAvailable, Version: res.Version}
		return
	}
	u.view.Phase, u.view.Error = UpdateLatest, ""
	u.view.PillVersion, u.view.PillDownloaded = "", false
	u.lastCheck = settings.UpdateRecord{At: at, Outcome: settings.UpdateOutcomeLatest}
}

func (u *Update) callCheck(opts services.UpdateCheckOptions) (services.UpdateCheckResult, error) {
	if u.svc == nil {
		return services.UpdateCheckResult{}, errUpdaterUnavailable
	}
	return u.svc.Check(u.context(), opts)
}

// failLocked 把一次调用的失败记成「失败」状态；推送先到的、已经映射过的原因（更友好）保留，不用调用返回的原始错误覆盖。
func (u *Update) failLocked(err error) {
	if u.view.Phase == UpdateFailed && u.view.Error != "" {
		return
	}
	u.view.Phase, u.view.Error = UpdateFailed, errorOrUnknown(err.Error())
}

// Download 下载已检查到的新版本；只在「有新版本」时有效。进度来自状态推送。
func (u *Update) Download() {
	u.mu.Lock()
	if u.view.Phase != UpdateAvailable {
		u.mu.Unlock()
		return
	}
	u.view.Phase, u.view.Percent = UpdateDownloading, 0
	opts := u.options()
	u.mu.Unlock()
	u.wake()

	u.spawn(func() {
		var err error
		if u.svc == nil {
			err = errUpdaterUnavailable
		} else {
			err = u.svc.Download(u.context(), opts)
		}
		u.mu.Lock()
		switch {
		case err != nil:
			u.failLocked(err)
		case u.view.Phase == UpdateDownloading:
			// 下载调用成功返回就是下载完成：没等到「已下载」推送时也不能让进度条停在中途
			u.view.Phase, u.view.Percent = UpdateDownloaded, 100
			u.view.PillVersion, u.view.PillDownloaded = u.view.Version, true
		}
		u.mu.Unlock()
		u.wake()
	})
}

// Install 运行已下载的安装包并退出应用；只在「已下载」时有效。失败（安装包起不来）显示在对话框里。
func (u *Update) Install() {
	u.mu.Lock()
	if u.view.Phase != UpdateDownloaded || u.view.Installing {
		u.mu.Unlock()
		return
	}
	u.view.Installing = true
	u.mu.Unlock()
	u.wake()

	u.spawn(func() {
		var err error
		if u.svc == nil {
			err = errUpdaterUnavailable
		} else {
			err = u.svc.Install(u.context())
		}
		u.mu.Lock()
		u.view.Installing = false
		if err != nil {
			u.failLocked(err)
		}
		u.mu.Unlock()
		u.wake()
	})
}

// OpenDownloaded 在资源管理器里显示已下载的安装包（SHA512 校验失败时让用户手动处理）；失败弹出带原因的提示
// （渲染层不处理这个失败）。
func (u *Update) OpenDownloaded() {
	u.runShell(locales.SettingsOpenDownloadFolder(), func(s services.Updater) error { return s.OpenDownloaded() })
}

// OpenReleases 用系统浏览器打开发布页（手动下载）；失败弹出带原因的提示。
func (u *Update) OpenReleases() {
	u.runShell(locales.SettingsOpenReleasesPage(), func(s services.Updater) error { return s.OpenReleases() })
}

func (u *Update) runShell(action string, fn func(services.Updater) error) {
	u.spawn(func() {
		u.guard.Run(action, func() error {
			if u.svc == nil {
				return errUpdaterUnavailable
			}
			return fn(u.svc)
		})
	})
}

// AutoCheck 是一次自动检查（启动、周期、窗口重新显示）。被跳过时把原因记进日志并返回它；返回空串表示已发起。
// 失败每个会话只弹一次提示，之后只进日志与「上次检查」记录；开发模式下主进程本就不检查，不算失败。
func (u *Update) AutoCheck(trigger string) string {
	enabled := u.AutoCheckEnabled != nil && u.AutoCheckEnabled()
	visible := u.WindowVisible == nil || u.WindowVisible()
	online := u.Online == nil || u.Online()
	now := u.now()

	u.mu.Lock()
	reason := ResolveAutoSkipReason(AutoCheckContext{
		Trigger: trigger, Enabled: enabled, WindowVisible: visible, Online: online, InFlight: u.autoBusy,
		Now: now, LastAttemptAt: u.autoAt, LastAttemptFailed: u.autoFailed,
	})
	if reason == "" {
		u.autoBusy, u.autoAt = true, now
	}
	u.mu.Unlock()

	if reason != "" {
		if u.svc != nil {
			u.svc.LogAutoSkip(trigger, reason)
		}
		return reason
	}
	opts := u.options()
	opts.Trigger = trigger
	u.spawn(func() {
		_, err := u.callCheck(opts)
		var disabled *services.UpdateDisabledError
		failed := err != nil && !errors.As(err, &disabled)

		u.mu.Lock()
		u.autoBusy, u.autoFailed = false, failed
		first := failed && !u.notified
		if failed {
			u.notified = true
			u.lastCheck = settings.UpdateRecord{At: u.now(), Outcome: settings.UpdateOutcomeError, Error: errorOrUnknown(err.Error())}
		}
		u.mu.Unlock()
		switch {
		case first:
			u.guard.Fail(locales.SettingsAutoUpdateCheckFailed(), "updater.autoCheck", err)
		case failed:
			u.guard.Background("updater.autoCheck", err)
		}
		u.wake()
	})
	return ""
}

// OnVisibilityChanged 在窗口显示或隐藏时调用：重新显示时补检一次（隐藏期间的检查被跳过，含「启动即最小化到托盘」的首检）。
func (u *Update) OnVisibilityChanged(visible bool) {
	if visible {
		u.AutoCheck(services.UpdateTriggerVisibility)
	}
}

// Start 开始定时检查：启动后 3 秒检查一次，之后每小时一次。重复调用以最后一次为准。
func (u *Update) Start() {
	u.mu.Lock()
	u.sched++
	gen := u.sched
	u.mu.Unlock()
	u.after(autoCheckStartupDelay, func() {
		if u.current(gen) {
			u.AutoCheck(services.UpdateTriggerStartup)
		}
	})
	u.scheduleInterval(gen)
}

func (u *Update) scheduleInterval(gen uint64) {
	u.after(AutoCheckInterval, func() {
		if !u.current(gen) {
			return
		}
		u.AutoCheck(services.UpdateTriggerInterval)
		u.scheduleInterval(gen)
	})
}

func (u *Update) current(gen uint64) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.sched == gen
}
