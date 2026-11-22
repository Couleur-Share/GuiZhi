package settings

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 外部链接与联系方式（与渲染层 AboutSettings 一致）。
const (
	repoURL         = "https://github.com/Couleur-Share/GuiZhi"
	userGuideURL    = repoURL + "/blob/main/docs/README.md"
	changelogURL    = repoURL + "/blob/main/CHANGELOG.md"
	issueURL        = repoURL + "/issues/new/choose"
	authorURL       = "https://github.com/Couleur-Share"
	softMemoURL     = "https://couleurapp.com/"
	authorEmail     = "couleurapp@gmail.com"
	repoDisplay     = "github.com/Couleur-Share/GuiZhi"
	authorHandle    = "@Couleur-Share"
	aboutBreakpoint = 640 // Tailwind sm 断点：不窄于它时身份区横排、概览三列并排
)

// 更新检查的结果，记在 UpdateRecord.Outcome 里（渲染层 updater.store 的 lastCheckOutcome）。
const (
	UpdateOutcomeAvailable = "available"
	UpdateOutcomeLatest    = "latest"
	UpdateOutcomeError     = "error"
)

// UpdateRecord 是最近一次更新检查的结果（渲染层 updater.store 的 lastCheck*）。
// 自动检查在没有新版本时不会改动界面，「关于」里的这一行是它唯一的可见痕迹。
// 由外壳持有（切换分区不丢），关于页在每次手动检查后写入；零值表示还没检查过。
type UpdateRecord struct {
	At      time.Time
	Outcome string
	Version string
	Error   string
}

var (
	errUpdaterUnavailable = errors.New("updater is not available")
	errNoOpener           = errors.New("external link opener is not available")
)

// updatePhase 是更新流程所处的阶段。
type updatePhase uint8

const (
	phaseIdle        updatePhase = iota // 空闲：可以检查
	phaseChecking                       // 正在检查
	phaseLatest                         // 已是最新
	phaseAvailable                      // 有新版本，等用户决定下载
	phaseDownloading                    // 下载中
	phaseDownloaded                     // 下载完成，等用户安装
)

// About 是「关于应用」页（渲染层 AboutSettings 的桌面分支；网页运行时的分支 Viva 没有）。
//
// 渲染层点「检查更新」是派发 open-update-dialog，由全局的 UpdateDialog 接管；Viva 还没有那个对话框，
// 所以检查、下载、安装直接在这一行里完成（见 台账「有意的差异」），状态来自同一条 updater:status 推送。
type About struct {
	Look  *kit.Look
	Store Store
	Guard kit.Guard
	// Updater 是更新服务；nil 表示外壳没接：检查时如实报错。
	Updater services.Updater
	// Version 是当前应用版本号；空时显示「...」（渲染层在版本读到之前的占位）。
	Version string
	// OpenExternal 用系统浏览器打开链接；nil 时点击弹出失败提示。
	OpenExternal func(url string) error
	// OpenSetupGuide 打开「设置引导」；nil 时不显示那一行（渲染层的 requestSetupChecklist）。
	OpenSetupGuide func()
	// Record 是最近一次更新检查的记录；nil 时不显示、也不记。
	Record *UpdateRecord
	// Now 给时间戳；nil 取系统时间（测试里固定）。
	Now func() time.Time
	// Wake 请求窗口重绘：后台结果回到界面线程前调用。可为 nil。
	Wake func()

	phase   updatePhase
	latest  string  // 有新版本时的版本号
	percent float64 // 下载进度（0–100）

	previewAsk     bool // 「加入预览通道」确认对话框是否打开
	previewConfirm kit.ConfirmState

	subscribed bool
	unsub      func()
	ctx        context.Context
	cancel     context.CancelFunc
	mb         mailbox
}

// Close 在分区被切走时调用：取消订阅与进行中的请求，丢弃还没送达的结果。
func (a *About) Close() {
	a.mb.close()
	if a.unsub != nil {
		a.unsub()
	}
	if a.cancel != nil {
		a.cancel()
	}
}

func (a *About) context() context.Context {
	if a.ctx == nil {
		a.ctx, a.cancel = context.WithCancel(context.Background())
	}
	return a.ctx
}

func (a *About) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

// subscribe 订阅更新状态推送（首次声明时一次）：下载进度与「已下载」来自这里，检查结果仍以调用的返回为准。
func (a *About) subscribe() {
	if a.subscribed || a.Updater == nil {
		return
	}
	a.subscribed = true
	wake := a.Wake
	a.unsub = a.Updater.OnStatus(func(st services.UpdateStatus) {
		a.mb.post(wake, func() { a.onStatus(st) })
	})
}

// onStatus 把一条状态推送并入界面状态。
func (a *About) onStatus(st services.UpdateStatus) {
	switch st.State {
	case services.UpdateStateDownloading:
		a.phase, a.percent = phaseDownloading, st.Percent
	case services.UpdateStateDownloaded:
		a.phase = phaseDownloaded
	case services.UpdateStateAvailable:
		if a.phase != phaseDownloading && a.phase != phaseDownloaded {
			a.phase = phaseAvailable
			if st.Version != "" {
				a.latest = st.Version
			}
		}
	case services.UpdateStateNotAvailable:
		if a.phase == phaseChecking {
			a.phase = phaseLatest
		}
	}
}

// options 是此刻的更新检查选项：镜像与通道取设置里的值。
func (a *About) options() services.UpdateCheckOptions {
	prefs := a.Store.Values().UI
	return services.UpdateCheckOptions{UseMirror: prefs.UseUpdateMirror, Channel: prefs.UpdateChannel}
}

// check 手动检查更新：后台请求，期间按钮禁用并转圈；失败弹出带原因的错误提示，并记入最近一次检查。
func (a *About) check() {
	if a.phase == phaseChecking || a.phase == phaseDownloading {
		return
	}
	a.phase = phaseChecking
	opts, ctx, svc, wake := a.options(), a.context(), a.Updater, a.Wake
	go func() {
		var res services.UpdateCheckResult
		var err error
		if svc == nil {
			err = errUpdaterUnavailable
		} else {
			res, err = svc.Check(ctx, opts)
		}
		if ctx.Err() != nil {
			return
		}
		a.mb.post(wake, func() { a.checked(res, err) })
	}()
}

// checked 收下一次检查的结果。
func (a *About) checked(res services.UpdateCheckResult, err error) {
	rec := UpdateRecord{At: a.now()}
	switch {
	case err != nil:
		a.phase = phaseIdle
		rec.Outcome, rec.Error = UpdateOutcomeError, err.Error()
		a.Guard.Fail(locales.SettingsUpdateCheckFailed(), locales.SettingsCheckUpdate(), err)
	case res.Available:
		a.phase, a.latest = phaseAvailable, res.Version
		rec.Outcome, rec.Version = UpdateOutcomeAvailable, res.Version
	default:
		a.phase = phaseLatest
		rec.Outcome = UpdateOutcomeLatest
	}
	if a.Record != nil {
		*a.Record = rec
	}
}

// download 下载新版本：后台进行，进度来自状态推送；失败回到「有新版本」，错误提示带原因，用户可以重试。
func (a *About) download() {
	if a.phase != phaseAvailable {
		return
	}
	a.phase, a.percent = phaseDownloading, 0
	opts, ctx, svc, wake := a.options(), a.context(), a.Updater, a.Wake
	go func() {
		var err error
		if svc == nil {
			err = errUpdaterUnavailable
		} else {
			err = svc.Download(ctx, opts)
		}
		if ctx.Err() != nil {
			return
		}
		a.mb.post(wake, func() {
			if err != nil {
				a.phase = phaseAvailable
				a.Guard.Report(locales.SettingsDownloadUpdate(), err)
				return
			}
			a.phase = phaseDownloaded
		})
	}()
}

// install 安装已下载的更新：外壳关闭应用并启动安装程序，所以不等结果、也不用随分区取消。
func (a *About) install() {
	svc := a.Updater
	a.Guard.Go(locales.SettingsInstallNow(), func() error {
		if svc == nil {
			return errUpdaterUnavailable
		}
		return svc.Install(context.Background())
	}, nil)
}

// openReleases 打开发布页（有新版本时的「查看」）。
func (a *About) openReleases() {
	svc := a.Updater
	a.Guard.Run(locales.SettingsOpenReleasesPage(), func() error {
		if svc == nil {
			return errUpdaterUnavailable
		}
		return svc.OpenReleases()
	})
}

// open 用系统浏览器打开链接；action 是链接的可见文字（进入失败提示的标题）。
func (a *About) open(action, url string) {
	a.Guard.Run(action, func() error {
		if a.OpenExternal == nil {
			return errNoOpener
		}
		return a.OpenExternal(url)
	})
}

// copyEmail 复制作者邮箱：成功弹「已复制」，失败弹带原因的错误提示。
func (a *About) copyEmail() {
	if err := a.Guard.Notify.CopyText(authorEmail); err != nil {
		a.Guard.Fail(locales.SettingsMcpCopyFailed(), locales.SettingsMcpCopy(), err)
		return
	}
	a.Guard.Succeed(locales.ToastCopied())
}

// channelLabel 是当前更新通道的显示名。
func (a *About) channelLabel() string {
	if a.Store.Values().UI.UpdateChannel == settings.ChannelPreview {
		return locales.SettingsPreviewChannel()
	}
	return locales.SettingsStableChannel()
}

// lastCheckLabel 是最近一次检查的一句话（渲染层 buildLastCheckLabel）；还没检查过返回空串。
func (a *About) lastCheckLabel() string {
	r := a.Record
	if r == nil || r.At.IsZero() || r.Outcome == "" {
		return ""
	}
	at := r.At.Format("15:04")
	switch r.Outcome {
	case UpdateOutcomeAvailable:
		return locales.SettingsLastCheckAvailable(at, r.Version)
	case UpdateOutcomeError:
		// 原因也显示出来：用户在这行只看得到「失败」两个字时没法判断下一步（截到 120 字符）
		label := locales.SettingsLastCheckFailed(at)
		if reason := truncateRunes(r.Error, 120); reason != "" {
			return locales.NativeSettingsLastCheckFailedReason(label, reason)
		}
		return label
	}
	return locales.SettingsLastCheckLatest(at)
}

// updateDescription 是「检查更新」一行的说明：流程进行中说进度，空闲时是通道加最近一次检查。
func (a *About) updateDescription() string {
	switch a.phase {
	case phaseChecking:
		return locales.SettingsChecking()
	case phaseLatest:
		return locales.SettingsNoUpdateDesc(a.Version)
	case phaseAvailable:
		return locales.SettingsUpdateAvailableDesc(a.latest)
	case phaseDownloading:
		return locales.SettingsDownloadProgress(fmt.Sprintf("%.0f", a.percent))
	case phaseDownloaded:
		return locales.SettingsDownloadComplete() + " · " + locales.SettingsInstallRestartHint()
	}
	channel := a.channelLabel()
	if label := a.lastCheckLabel(); label != "" {
		return channel + " \u00b7 " + label
	}
	return locales.SettingsUpdateCheckPending(channel)
}

func truncateRunes(s string, n int) string {
	runes := []rune(strings.TrimSpace(s))
	if len(runes) > n {
		runes = runes[:n]
	}
	return string(runes)
}
