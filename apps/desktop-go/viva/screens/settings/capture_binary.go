package settings

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// binaryTexts 是一个二进制引擎的专属文案（渲染层 BinaryEngineTexts）。
type binaryTexts struct {
	Name, Purpose, MissingHint string
	InstalledToast             func(version string) string
	RemovedToast               string
	RemoveConfirm              string
}

// checkPhase 是引擎「检查更新」的四态机（与「关于」里应用自身的更新入口同构）。
type checkPhase uint8

const (
	checkIdle checkPhase = iota
	checkChecking
	checkLatest
	checkAvailable
)

// updateCheck 刻意不进 EngineCache：检查结果有时效性，每次进设置页回到 idle，用户想知道就再点一次。
type updateCheck struct {
	phase   checkPhase
	version string
}

// binaryEngine 是 yt-dlp 或 ffmpeg 的设置行（渲染层 BinaryEngineRow）：状态探测、一键安装与更新内置版、移除内置版、自定义路径。
// 两者除文案与服务命名空间外完全同构。平台不支持应用内安装时，主操作改为复制推荐命令或不给安装按钮，避免点了必失败。
type binaryEngine struct {
	Tool     services.ManagedTool
	Optional bool
	Texts    func() binaryTexts
	// PathOf 取设置里的自定义路径；SetPath 写回（只有文件选择器返回过的路径才会被设置层接受，见 settings.go）。
	PathOf  func(settings.App) string
	SetPath func(*settings.Values, string)

	look  *kit.Look
	store Store
	guard kit.Guard
	svc   services.CaptureTools
	wake  func()

	probe                probe[services.ToolStatus]
	row                  engineRowState
	check                updateCheck
	installing, removing bool
	progress             *services.ToolProgress

	subscribed bool
	unsub      func()
	w          worker
}

func (b *binaryEngine) Close() {
	b.w.close()
	if b.unsub != nil {
		b.unsub()
	}
}

func (b *binaryEngine) bind(look *kit.Look, store Store, guard kit.Guard, svc services.CaptureTools, wake func(), cache *EngineCache) {
	b.look, b.store, b.guard, b.svc, b.wake = look, store, guard, svc, wake
	b.probe.cache, b.probe.w, b.probe.wake, b.probe.guard, b.probe.action = cache, &b.w, wake, guard, b.Texts().Name
	b.probe.fetch = nil
	if svc != nil {
		tool := b.Tool
		b.probe.fetch = func(ctx context.Context, force bool) (services.ToolStatus, error) {
			return svc.ToolStatus(ctx, tool, force)
		}
	}
}

// subscribe 订阅下载进度推送（首次声明时一次）；推送来自别的 goroutine，经邮箱回到界面线程。
func (b *binaryEngine) subscribe() {
	if b.subscribed || b.svc == nil {
		return
	}
	b.subscribed = true
	b.unsub = b.svc.OnToolProgress(b.Tool, func(p services.ToolProgress) {
		b.w.mb.post(b.wake, func() { b.progress = &p })
	})
}

func (b *binaryEngine) customPath() string { return b.PathOf(b.store.Values().App) }

// Row 声明这一行；c 由调用方按引擎键好。
func (b *binaryEngine) Row(c ui.C) {
	b.w.drain()
	b.subscribe()
	path := b.customPath()
	b.probe.ensure(string(b.Tool) + ":" + path)
	engineRow(b.look, c, &b.row, b.view(path))
}

// ── 动作 ──

func (b *binaryEngine) checkUpdate() {
	if b.check.phase == checkChecking {
		return
	}
	b.check = updateCheck{phase: checkChecking}
	svc, g, tool, name := b.svc, b.guard, b.Tool, b.Texts().Name
	b.w.load(b.wake, func(ctx context.Context) func() {
		var (
			res services.ToolUpdateCheck
			err = errCaptureUnavailable
		)
		if svc != nil {
			res, err = svc.CheckToolUpdate(ctx, tool)
		}
		next := updateCheck{}
		switch {
		case err != nil:
			// 这里几乎总是网络或代理问题，原因放进详情，用户才知道该去配代理。
			if ctx.Err() == nil {
				g.Fail(locales.SettingsCaptureCheckFailed(), name, err)
			}
		case res.Latest == "":
			g.Reject(locales.SettingsCaptureCheckFailed())
		case res.UpdateAvailable:
			next = updateCheck{phase: checkAvailable, version: res.Latest}
		default:
			next = updateCheck{phase: checkLatest}
		}
		return func() { b.check = next }
	})
}

func (b *binaryEngine) install() {
	if b.installing {
		return
	}
	b.installing, b.progress = true, nil
	svc, g, tool, name, texts := b.svc, b.guard, b.Tool, b.Texts().Name, b.Texts()
	// 安装不随分区切走而取消（下载做到一半不该被打断）；切走之后只是界面不再收到收尾，结果提示照常弹出。
	b.w.act(b.wake, func() func() {
		version, err := "", errCaptureUnavailable
		if svc != nil {
			version, err = svc.InstallTool(context.Background(), tool)
		}
		if err != nil {
			g.Fail(locales.SettingsCaptureInstallFailed(err.Error()), name, err)
		} else {
			g.Succeed(texts.InstalledToast(version))
		}
		return func() {
			b.installing, b.progress = false, nil
			if err == nil {
				b.check = updateCheck{} // 版本变了，之前的检查结论作废
			}
			b.probe.refresh(true)
		}
	})
}

func (b *binaryEngine) remove() {
	if b.removing {
		return
	}
	b.removing = true
	svc, g, tool, name, texts := b.svc, b.guard, b.Tool, b.Texts().Name, b.Texts()
	b.w.act(b.wake, func() func() {
		err := errCaptureUnavailable
		if svc != nil {
			err = svc.RemoveTool(context.Background(), tool)
		}
		if err != nil {
			g.Fail(locales.SettingsCaptureRemoveFailed(err.Error()), name, err)
		} else {
			g.Succeed(texts.RemovedToast)
		}
		return func() {
			b.removing = false
			b.probe.refresh(true)
		}
	})
}

func (b *binaryEngine) pick() {
	svc, g, tool, name := b.svc, b.guard, b.Tool, b.Texts().Name
	b.w.act(b.wake, func() func() {
		path, err := "", errCaptureUnavailable
		if svc != nil {
			path, err = svc.PickToolBinary(context.Background(), tool)
		}
		if err != nil {
			g.Report(name, err)
			return func() {}
		}
		if path == "" {
			return func() {} // 取消选择
		}
		return func() { b.setPath(path) }
	})
}

func (b *binaryEngine) setPath(path string) {
	save(b.guard, b.store, func(v *settings.Values) { b.SetPath(v, path) })
}

func (b *binaryEngine) copyHint(command string) {
	if err := b.guard.Notify.CopyText(command); err != nil {
		b.guard.Fail(locales.SettingsCaptureCopyFailed(), b.Texts().Name, err)
		return
	}
	b.guard.Succeed(locales.SettingsCaptureCommandCopied(command))
}

// ── 视图 ──

func engineSourceLabel(source string) string {
	switch source {
	case services.ToolSourceManaged:
		return locales.SettingsCaptureSourceManaged()
	case services.ToolSourcePath:
		return locales.SettingsCaptureSourcePath()
	case services.ToolSourceCustom:
		return locales.SettingsCaptureSourceCustom()
	}
	return ""
}

func megabytes(n int64) string { return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024)) }

func (b *binaryEngine) view(customPath string) engineView {
	st, tx := b.probe.status, b.Texts()
	hasCustom := strings.TrimSpace(customPath) != ""
	state := engineMissing
	switch {
	case b.probe.err != "":
		state = engineError
	case st == nil:
		state = engineProbing
	case st.Installed:
		state = engineReady
	case hasCustom:
		state = engineInvalid
	}
	unsupported := st != nil && !st.InstallSupported
	hint := ""
	if st != nil && unsupported && state == engineMissing {
		if st.InstallHintCommand != "" {
			hint = locales.SettingsFfmpegBrewHint(st.InstallHintCommand)
		} else {
			hint = locales.SettingsFfmpegPackageManagerHint()
		}
	}
	detail := ""
	switch state {
	case engineReady:
		parts := []string{}
		if st.Version != "" {
			parts = append(parts, services.FormatEngineVersion(st.Version))
		}
		if s := engineSourceLabel(st.Source); s != "" {
			parts = append(parts, s)
		}
		detail = strings.Join(parts, " · ")
	case engineInvalid:
		detail = locales.SettingsCaptureCustomInvalid()
	case engineError:
		detail = b.probe.err
	case engineMissing:
		detail = hint
		if detail == "" {
			detail = tx.MissingHint
		}
	}
	percent := -1
	if p := b.progress; p != nil && p.Total > 0 {
		percent = int(math.Min(100, math.Round(float64(p.Transferred)/float64(p.Total)*100)))
	}
	managed := st != nil && st.Source == services.ToolSourceManaged
	busyText := ""
	switch {
	case b.installing && percent >= 0:
		busyText = locales.SettingsCaptureDownloading(percent)
	case b.installing && b.progress != nil:
		busyText = locales.SettingsCaptureDownloadingBytes(megabytes(b.progress.Transferred))
	case b.installing:
		busyText = locales.SettingsCaptureInstalling()
	case b.removing:
		busyText = locales.SettingsCaptureRemoving()
	case b.check.phase == checkChecking:
		busyText = locales.SettingsCaptureChecking()
	}
	v := engineView{
		ID: string(b.Tool), Name: tx.Name, Purpose: tx.Purpose, State: state, Optional: b.Optional,
		Detail: detail, BusyText: busyText, Progress: -1, Primary: b.primary(st, managed),
		Refreshing: b.probe.refreshing,
		OnRefresh: func() {
			b.check = updateCheck{} // 重新探测本地状态时，之前那次远端检查的结论也一并作废
			b.probe.refresh(true)
		},
		Custom: &engineCustom{
			Value: customPath, Placeholder: locales.SettingsCaptureCustomPathPlaceholder(), Hint: locales.SettingsCaptureCustomPathHint(),
			OnPick: b.pick, OnClear: func() { b.setPath("") },
		},
	}
	if b.installing {
		v.Progress = max(percent, -1)
		if percent < 0 {
			v.Progress = -1
		}
	}
	if st != nil && (st.Source == services.ToolSourceManaged || st.Source == services.ToolSourceCustom) {
		v.ActivePath = st.Path // 系统 PATH 来源时 path 只是命令名，展示成「生效路径」反而误导
	}
	if managed {
		v.Remove = &engineAction{
			Label: locales.SettingsCaptureRemoveManaged(), ConfirmTitle: locales.SettingsCaptureRemoveManaged(), ConfirmText: tx.RemoveConfirm,
			Busy: b.removing, OnClick: b.remove,
		}
	}
	return v
}

// primary 是主操作：内置版遵循「先检查、有更新才给更新按钮」。
func (b *binaryEngine) primary(st *services.ToolStatus, managed bool) *enginePrimary {
	busy := b.installing || b.removing
	if b.installing {
		label := locales.SettingsCaptureInstalling()
		if managed {
			label = locales.SettingsCaptureUpdating()
		}
		return &enginePrimary{Label: label, Icon: icons.Download, Busy: true, OnClick: func() {}}
	}
	// 还没探测出结果（或探测失败）时不知道装没装，任何动作都是瞎猜；状态徽章已经在说「检测中 / 检测失败」。
	if st == nil {
		return nil
	}
	if !st.InstallSupported {
		if st.Installed || st.InstallHintCommand == "" {
			return nil
		}
		cmd := st.InstallHintCommand
		return &enginePrimary{Label: locales.SettingsCaptureCopyBrewCommand(), Icon: icons.ClipboardCopy, Emphasized: true, OnClick: func() { b.copyHint(cmd) }}
	}
	if !st.Installed {
		return &enginePrimary{Label: locales.SettingsCaptureInstall(), Icon: icons.Download, Emphasized: true, Busy: busy, OnClick: b.install}
	}
	if !managed {
		return &enginePrimary{Label: locales.SettingsCaptureInstallManaged(), Icon: icons.Download, Busy: busy, OnClick: b.install}
	}
	switch b.check.phase {
	case checkAvailable:
		return &enginePrimary{
			Label: locales.SettingsCaptureUpdateTo(services.FormatEngineVersion(b.check.version)), Icon: icons.CircleArrowUp,
			Emphasized: true, Busy: busy, OnClick: b.install,
		}
	case checkLatest:
		return &enginePrimary{Label: locales.SettingsCaptureUpToDate(), Status: true}
	case checkChecking:
		return &enginePrimary{Label: locales.SettingsCaptureChecking(), Icon: icons.RefreshCw, Busy: true, OnClick: b.checkUpdate}
	}
	return &enginePrimary{Label: locales.SettingsCaptureCheckUpdate(), Icon: icons.RefreshCw, Busy: busy, OnClick: b.checkUpdate}
}

// binaryTextsFor 是两个引擎的专属文案。
func ytDlpTexts() binaryTexts {
	return binaryTexts{
		Name: locales.SettingsYtDlpEngine(), Purpose: locales.SettingsYtDlpPurpose(), MissingHint: locales.SettingsYtDlpMissingHint(),
		InstalledToast: func(v string) string { return locales.SettingsYtDlpInstallDone(services.FormatEngineVersion(v)) },
		RemovedToast:   locales.SettingsYtDlpRemoved(), RemoveConfirm: locales.SettingsYtDlpRemoveConfirm(),
	}
}

func ffmpegTexts() binaryTexts {
	return binaryTexts{
		Name: locales.SettingsFfmpegEngine(), Purpose: locales.SettingsFfmpegPurpose(), MissingHint: locales.SettingsFfmpegMissingHint(),
		InstalledToast: func(v string) string { return locales.SettingsFfmpegInstallDone(services.FormatEngineVersion(v)) },
		RemovedToast:   locales.SettingsFfmpegRemoved(), RemoveConfirm: locales.SettingsFfmpegRemoveConfirm(),
	}
}
