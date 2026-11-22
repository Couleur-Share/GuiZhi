package settings

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// funasrEngine 是本地转写引擎的设置行（渲染层 FunasrEngineRow）：安装、检查更新、升级（分阶段进度）与卸载。
// 安装完成后外壳写入内置模型并接上「语音转写」路由（AI 配置在外壳那一侧改，设置页不持有它，所以不需要像渲染层那样回读设置）。
//
// Windows = Python SenseVoice（约 3GB，含说话人分离）；macOS Apple Silicon = FunASR GGUF（约 300MB，无分离）；其余平台隐藏安装入口。
type funasrEngine struct {
	look  *kit.Look
	guard kit.Guard
	svc   services.CaptureTools
	wake  func()
	// Now 给更新耗时计时；nil 取系统时间（测试里固定）。
	Now func() time.Time

	probe  probe[services.FunasrStatus]
	row    engineRowState
	check  updateCheck
	phase  funasrBusy
	prog   *services.FunasrProgress
	since  time.Time // 本次更新的开始时刻（耗时显示）
	detail bool      // 「查看运行详情」是否展开

	subscribed bool
	unsub      func()
	stopTick   chan struct{}
	w          worker
}

// funasrBusy 是此刻在做的事；同一时刻只能做一件（渲染层靠一组互斥的守卫实现）。
type funasrBusy uint8

const (
	funasrIdle funasrBusy = iota
	funasrInstalling
	funasrUpdating
	funasrUninstalling
)

func (f *funasrEngine) Close() {
	f.w.close()
	if f.unsub != nil {
		f.unsub()
	}
	f.stopTicker()
}

func (f *funasrEngine) bind(look *kit.Look, guard kit.Guard, svc services.CaptureTools, wake func(), cache *EngineCache) {
	f.look, f.guard, f.svc, f.wake = look, guard, svc, wake
	f.probe.cache, f.probe.w, f.probe.wake, f.probe.guard, f.probe.action = cache, &f.w, wake, guard, locales.SettingsFunasrEngine()
	f.probe.fetch = nil
	if svc != nil {
		f.probe.fetch = svc.FunasrStatus
	}
}

func (f *funasrEngine) now() time.Time {
	if f.Now != nil {
		return f.Now()
	}
	return time.Now()
}

func (f *funasrEngine) subscribe() {
	if f.subscribed || f.svc == nil {
		return
	}
	f.subscribed = true
	f.unsub = f.svc.OnFunasrProgress(func(p services.FunasrProgress) {
		f.w.mb.post(f.wake, func() { f.prog = &p })
	})
}

// startTicker 在更新期间每秒请求一次重绘，让「已用 m:ss」走起来；更新结束或分区切走时停掉。
func (f *funasrEngine) startTicker() {
	f.stopTicker()
	stop := make(chan struct{})
	f.stopTick = stop
	wake := f.wake
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				if wake != nil {
					wake()
				}
			}
		}
	}()
}

func (f *funasrEngine) stopTicker() {
	if f.stopTick != nil {
		close(f.stopTick)
		f.stopTick = nil
	}
}

// Row 声明这一行；c 由调用方按引擎键好。
func (f *funasrEngine) Row(c ui.C) {
	f.w.drain()
	f.subscribe()
	f.probe.ensure("funasr")
	engineRow(f.look, c, &f.row, f.view())
}

// ── 动作 ──

func (f *funasrEngine) busy() bool { return f.phase != funasrIdle || f.check.phase == checkChecking }

func (f *funasrEngine) checkUpdate() {
	if f.busy() {
		return
	}
	f.check = updateCheck{phase: checkChecking}
	svc, g, name := f.svc, f.guard, locales.SettingsFunasrEngine()
	f.w.load(f.wake, func(ctx context.Context) func() {
		var (
			res services.ToolUpdateCheck
			err = errCaptureUnavailable
		)
		if svc != nil {
			res, err = svc.CheckFunasrUpdate(ctx)
		}
		next := updateCheck{}
		switch {
		case err != nil:
			if ctx.Err() == nil {
				g.Fail(locales.SettingsCaptureCheckFailed(), name, err)
			}
		case res.Latest == "":
			g.Reject(locales.SettingsCaptureCheckFailed())
		case res.UpdateAvailable:
			next = updateCheck{phase: checkAvailable, version: res.Latest}
		default:
			next = updateCheck{phase: checkLatest, version: res.Latest}
		}
		return func() { f.check = next }
	})
}

func (f *funasrEngine) begin(phase funasrBusy) bool {
	if f.busy() {
		return false
	}
	f.phase, f.prog, f.check = phase, nil, updateCheck{}
	return true
}

func (f *funasrEngine) finish(refresh bool) {
	f.phase, f.prog = funasrIdle, nil
	f.stopTicker()
	if refresh {
		f.probe.refresh(true)
	}
}

func (f *funasrEngine) install() {
	if !f.begin(funasrInstalling) {
		return
	}
	svc, g, name := f.svc, f.guard, locales.SettingsFunasrEngine()
	f.w.act(f.wake, func() func() {
		_, err := "", errCaptureUnavailable
		if svc != nil {
			_, err = svc.InstallFunasr(context.Background())
		}
		if err != nil {
			g.Fail(locales.SettingsCaptureInstallFailed(err.Error()), name, err)
		} else {
			g.Succeed(locales.SettingsFunasrInstallDone())
		}
		return func() { f.finish(true) }
	})
}

func (f *funasrEngine) update() {
	version := f.check.version
	if version == "" || f.busy() {
		return
	}
	f.phase, f.prog, f.since = funasrUpdating, nil, f.now()
	f.startTicker()
	svc, g := f.svc, f.guard
	f.w.act(f.wake, func() func() {
		res, err := services.FunasrUpdateResult{}, errCaptureUnavailable
		if svc != nil {
			res, err = svc.UpdateFunasr(context.Background(), version)
		}
		switch {
		case err != nil:
			g.Fail(locales.SettingsFunasrUpdateFailed(), locales.SettingsFunasrEngine(), err)
		case res.Warning != "":
			if g.Notify != nil {
				g.Notify.Warning(locales.SettingsFunasrUpdateDone(res.Version), kit.WithDetail(res.Warning))
			}
		default:
			g.Succeed(locales.SettingsFunasrUpdateDone(res.Version))
		}
		return func() {
			f.check = updateCheck{} // 无论成败，之前的检查结论都作废
			f.finish(true)
		}
	})
}

func (f *funasrEngine) uninstall() {
	if !f.begin(funasrUninstalling) {
		return
	}
	svc, g, name := f.svc, f.guard, locales.SettingsFunasrEngine()
	f.w.act(f.wake, func() func() {
		err := errCaptureUnavailable
		if svc != nil {
			err = svc.UninstallFunasr(context.Background())
		}
		if err != nil {
			g.Fail(locales.SettingsFunasrUninstallFailed(err.Error()), name, err)
		} else {
			g.Succeed(locales.SettingsFunasrUninstalled())
		}
		return func() { f.finish(true) }
	})
}

// ── 视图 ──

func (f *funasrEngine) phaseLabel(st *services.FunasrStatus) string {
	if f.prog == nil {
		return ""
	}
	switch f.prog.Phase {
	case services.FunasrPhaseBackup:
		return locales.SettingsFunasrPhaseBackup()
	case services.FunasrPhaseVerify:
		return locales.SettingsFunasrPhaseVerify()
	case services.FunasrPhaseRollback:
		return locales.SettingsFunasrPhaseRollback()
	case services.FunasrPhaseRuntime:
		return locales.SettingsFunasrPhaseRuntime()
	case services.FunasrPhaseDeps:
		return locales.SettingsFunasrPhaseDeps()
	case services.FunasrPhaseModels:
		if st != nil && st.InstallFlavor == services.FunasrFlavorGguf {
			return locales.SettingsFunasrPhaseModelsGguf()
		}
		return locales.SettingsFunasrPhaseModels()
	}
	return ""
}

func (f *funasrEngine) view() engineView {
	st := f.probe.status
	installSupported := st == nil || st.InstallSupported
	state := engineMissing
	switch {
	case f.probe.err != "":
		state = engineError
	case st == nil:
		state = engineProbing
	case st.Installed:
		state = engineReady
	}
	isGguf := st != nil && st.InstallFlavor == services.FunasrFlavorGguf
	updating := f.phase == funasrUpdating
	rollingBack := updating && f.prog != nil && f.prog.Phase == services.FunasrPhaseRollback

	busyText := ""
	switch {
	case f.phase == funasrInstalling || updating:
		label := f.phaseLabel(st)
		if label == "" {
			label = locales.SettingsCaptureInstalling()
			if updating {
				label = locales.SettingsCaptureUpdating()
			}
		}
		parts := []string{label}
		if f.prog != nil && f.prog.Percent != nil {
			parts = append(parts, strconv.Itoa(*f.prog.Percent)+"%")
		}
		if f.prog != nil && f.prog.Detail != "" {
			parts = append(parts, f.prog.Detail)
		}
		busyText = joinDot(parts)
	case f.phase == funasrUninstalling:
		busyText = locales.SettingsFunasrUninstalling()
	}
	if updating {
		busyText = "" // 更新期间由下方的进度面板说明
	}

	detail := ""
	switch state {
	case engineReady:
		// 「待命」是常态，说了等于没说——徽章的「已就绪」已经覆盖；只有真的在跑（占着内存）才值得单独标一下。
		parts := []string{"v" + orQuestion(st.Version)}
		if st.Running {
			parts = append(parts, locales.SettingsFunasrRunning())
		}
		detail = joinDot(parts)
	case engineMissing:
		switch {
		case !installSupported:
			detail = locales.SettingsFunasrUnsupportedHint()
		case isGguf:
			detail = locales.SettingsFunasrDiskHintGguf()
		default:
			detail = locales.SettingsFunasrDiskHint()
		}
	case engineError:
		detail = f.probe.err
	}
	if updating && !rollingBack && st != nil && st.Version != "" && f.check.version != "" {
		detail = "v" + st.Version + " → v" + f.check.version
	}

	v := engineView{
		ID: "funasr", Name: locales.SettingsFunasrEngine(), Purpose: locales.SettingsFunasrPurpose(), State: state, Optional: true,
		Detail: detail, BusyText: busyText, Progress: -1, ActionsDisabled: f.busy(),
		Refreshing: f.probe.refreshing && !updating,
		OnRefresh: func() {
			if f.busy() {
				return
			}
			f.check = updateCheck{}
			f.probe.refresh(true)
		},
		Primary: f.primary(st, installSupported, isGguf, rollingBack),
	}
	switch {
	case rollingBack:
		v.ActivityLabel = locales.SettingsFunasrRestoringBadge()
	case updating:
		v.ActivityLabel = locales.SettingsFunasrUpdatingBadge()
	}
	if updating {
		v.Activity = f.updatePanel
	}
	if f.phase == funasrInstalling && f.prog != nil && f.prog.Percent != nil {
		v.Progress = *f.prog.Percent
	}
	if st != nil && st.Installed {
		v.ActivePath = st.Dir
	}
	if installSupported && st != nil && st.Installed {
		v.Reinstall = &engineAction{Label: locales.SettingsFunasrReinstall(), Busy: f.phase == funasrInstalling, OnClick: f.install}
		confirm := locales.SettingsFunasrUninstallConfirm()
		if isGguf {
			confirm = locales.SettingsFunasrUninstallConfirmGguf()
		}
		v.Remove = &engineAction{
			Label: locales.SettingsFunasrUninstall(), ConfirmTitle: locales.SettingsFunasrUninstall(), ConfirmText: confirm,
			Busy: f.phase == funasrUninstalling, OnClick: f.uninstall,
		}
	}
	return v
}

func (f *funasrEngine) primary(st *services.FunasrStatus, installSupported, isGguf, rollingBack bool) *enginePrimary {
	if !installSupported || st == nil {
		return nil
	}
	busy := f.busy()
	if f.phase == funasrInstalling || f.phase == funasrUpdating {
		label := locales.SettingsCaptureInstalling()
		switch {
		case rollingBack:
			label = locales.SettingsFunasrRestoring()
		case f.phase == funasrUpdating:
			label = locales.SettingsCaptureUpdating()
		}
		return &enginePrimary{Label: label, Icon: icons.Download, Busy: true, OnClick: func() {}}
	}
	if !st.Installed {
		return &enginePrimary{Label: locales.SettingsCaptureInstall(), Icon: icons.Download, Emphasized: true, Busy: busy, OnClick: f.install}
	}
	if !st.UpdateSupported {
		return &enginePrimary{Label: locales.SettingsFunasrUpdateWithApp(), Status: true}
	}
	switch f.check.phase {
	case checkAvailable:
		return &enginePrimary{Label: locales.SettingsCaptureUpdateTo("v" + f.check.version), Icon: icons.CircleArrowUp, Emphasized: true, Busy: busy, OnClick: f.update}
	case checkLatest:
		return &enginePrimary{Label: locales.SettingsCaptureUpToDate(), Status: true}
	case checkChecking:
		return &enginePrimary{Label: locales.SettingsCaptureChecking(), Icon: icons.RefreshCw, Busy: true, OnClick: f.checkUpdate}
	}
	return &enginePrimary{Label: locales.SettingsCaptureCheckUpdate(), Icon: icons.RefreshCw, Busy: busy, OnClick: f.checkUpdate}
}

func joinDot(parts []string) string {
	out := ""
	for _, p := range parts {
		if p == "" {
			continue
		}
		if out != "" {
			out += " · "
		}
		out += p
	}
	return out
}

func orQuestion(s string) string {
	if s == "" {
		return "?"
	}
	return s
}

// elapsedText 是更新已用时间的 m:ss 写法。
func elapsedText(d time.Duration) string {
	s := int(d / time.Second)
	if s < 0 {
		s = 0
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}
