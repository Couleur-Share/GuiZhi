package imports

import (
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// 详情弹窗底部的动作与它们背后的异步流程（渲染层 ImportTaskDetailModal 的动作区、WebVerificationRetryButton、NodeseekRetryButton）。
// 都在后台执行，状态写回 detailState 后请求重绘；成功后要关弹窗的由 requestCloseDetail 交给界面线程。

// amber500 是琥珀色 500 档（不是主题令牌，浅色与深色同一档）带透明度。
func amber500(alpha float32) tokens.Color { return tokens.Tailwind("amber-500").WithAlpha(alpha) }

// inlineFlexExtra 是「text-xs 的一行里放一个行内弹性盒（图标加文字）」比一行文字多出的高度（图标边长 icon）：
// 行内弹性盒的基线取第一项（图标）的下沿，图标在行高里居中，下沿比文字基线低，行盒因此向上多出一截，见 kit.InlineFlexExtra。
func (s *Screen) inlineFlexExtra(icon float32) float32 {
	l := s.Look
	return kit.InlineFlexExtra(kit.WeightNormal, l.Rem(0.75), l.Rem(1), icon, l.Scale)
}

// detailActions 是动作区：flex items-center gap-2 border-t border-border pt-4。
// 顺序：打开条目 / 打开已有条目、打开原条目、验证类或普通重试、登录态重试。
func (s *Screen) detailActions(c ui.C, task services.ImportTask, openable string, pair refreshPair, hasPair bool) {
	l := s.Look
	t := l.T
	canRetry := task.Status == services.ImportStatusFailed || task.Status == services.ImportStatusCanceled
	authPlatform := AuthenticatedRetryPlatform(task)
	snap := s.detailSnapshot()

	ui.Row(c).FillWidth().Items(ui.Center).Gap(l.Space(2)).PadSides(l.Space(4)+1, 0, 0, 0).
		Draw(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeTop, 1, t.Border) }).
		Children(func() {
			if openable != "" {
				label := locales.ImportsOpenItem()
				if task.Status == services.ImportStatusDuplicate {
					label = locales.ImportsOpenExisting()
				}
				b := l.Button(c.Key("open"), kit.ButtonSpec{Label: label, Icon: icons.ExternalLink, IconSize: l.Rem(0.875)},
					s.actionStyle(actionPrimary))
				if b.Clicked() {
					s.openItemAndClose(openable)
				}
			}
			if hasPair {
				b := l.Button(c.Key("open-original"), kit.ButtonSpec{Label: locales.ImportsOpenOriginal()}, s.actionStyle(actionOutline))
				if b.Clicked() {
					s.openItemAndClose(pair.original.ID)
				}
			}
			switch {
			case NeedsWebVerification(task):
				label := locales.ImportsWebVerifyRetry()
				if s.webSolverEnabled() {
					label = locales.ImportsWebSolverRetry()
				}
				b := l.Button(c.Key("verify"), kit.ButtonSpec{Label: label, Icon: icons.ShieldCheck, IconSize: l.Rem(0.875), Disabled: snap.webBusy},
					s.verifyStyle())
				if b.Clicked() {
					s.verifyWebRetry(task)
				}
			case canRetry:
				b := l.Button(c.Key("retry"), kit.ButtonSpec{Label: locales.ImportsRetry(), Icon: icons.RotateCcw, IconSize: l.Rem(0.875)},
					s.actionStyle(actionOutline))
				if b.Clicked() {
					s.retryAndClose(task)
				}
			}
			switch {
			case authPlatform == "nodeseek":
				s.nodeseekButtons(c, task, snap.nodePending)
			case authPlatform != "":
				b := l.Button(c.Key("auth-retry"), kit.ButtonSpec{Label: locales.ImportsAuthenticatedRetry(), Icon: icons.LogIn, IconSize: l.Rem(0.875)},
					s.actionStyle(actionPrimary))
				if b.Clicked() {
					s.retryAuthenticated(task, authPlatform, true)
				}
			}
		})
}

// retryAndClose 重试并关闭弹窗（渲染层 void retryTask(task.id); onClose()）：不等重试的结果，失败由 Guard 提示。
func (s *Screen) retryAndClose(task services.ImportTask) {
	s.Store.Spawn(func() { s.Store.RetryTask(s.Ctx, task.ID, services.ImportRetryOptions{}) })
	s.closeDetail()
}

// verifyWebRetry 是「验证并自动采集」：带 verifyWeb 重试，成功后关闭弹窗；进行中按钮禁用。
func (s *Screen) verifyWebRetry(task services.ImportTask) {
	s.mu.Lock()
	if s.detail.webBusy {
		s.mu.Unlock()
		return
	}
	s.detail.webBusy = true
	s.mu.Unlock()
	s.Store.Spawn(func() {
		ok := s.Store.RetryTask(s.Ctx, task.ID, services.ImportRetryOptions{VerifyWeb: true})
		s.mu.Lock()
		s.detail.webBusy = false
		s.mu.Unlock()
		if ok {
			s.requestCloseDetail(task.ID)
			return
		}
		s.Store.Wake()
	})
}

// retryAuthenticated 用内置登录窗口的登录态重试：钩子可能要等用户登录，所以放在后台；
// 它返回「已发起」时（fromDetail 为真）关闭详情弹窗，失败由钩子自己带详情提示。
func (s *Screen) retryAuthenticated(task services.ImportTask, platform string, fromDetail bool) {
	if s.Hooks.RetryAuthenticated == nil {
		return
	}
	s.Store.Spawn(func() {
		if s.Hooks.RetryAuthenticated(task, platform) && fromDetail {
			s.requestCloseDetail(task.ID)
		}
	})
}

// nodeseekButtons 是 NodeSeek 的验证重试：「验证并自动采集」，进行中变成转圈的「等待网页验证…」并多出「取消」。
// 两颗按钮在渲染层里包在 flex items-center gap-2 里，间距与动作区相同，这里直接并入动作区。
func (s *Screen) nodeseekButtons(c ui.C, task services.ImportTask, pending bool) {
	l := s.Look
	t := l.T
	spec := kit.ButtonSpec{Label: locales.ImportsNodeseekVerifyRetry(), Icon: icons.ShieldCheck, IconSize: l.Rem(0.875), Disabled: pending}
	if pending {
		spec = kit.ButtonSpec{Label: locales.ImportsNodeseekVerifying(), Busy: true, BusySize: l.Rem(0.875), Disabled: true}
	}
	if b := l.Button(c.Key("nodeseek"), spec, s.verifyStyle()); b.Clicked() {
		s.nodeseekVerify(task)
	}
	if !pending {
		return
	}
	// h-8 rounded-lg border border-border px-3 text-xs hover:bg-accent：文字沿用正文色，没有 transition。
	cancel := s.actionStyle(actionOutline)
	cancel.Fg, cancel.FgHover, cancel.Transition = t.Foreground, t.Foreground, kit.DurationNone
	if b := l.Button(c.Key("nodeseek-cancel"), kit.ButtonSpec{Label: locales.CommonCancel()}, cancel); b.Clicked() {
		s.cancelNodeseek(false)
	}
}

// nodeseekVerify 打开 NodeSeek 的验证窗口，通过后用登录态重试任务，成功则关闭弹窗。
// 旧的「已验证」标记可能已过期，每次都以目标帖子实际可读为准（由钩子实现）。取消后不重试也不弹失败。
func (s *Screen) nodeseekVerify(task services.ImportTask) {
	if s.Hooks.VerifyNodeseek == nil {
		return
	}
	s.mu.Lock()
	if s.detail.nodePending {
		s.mu.Unlock()
		return
	}
	s.detail.nodePending, s.detail.nodeCanceled = true, false
	s.mu.Unlock()
	canceled := func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.detail.nodeCanceled
	}
	s.Store.Spawn(func() {
		defer func() {
			s.mu.Lock()
			s.detail.nodePending = false
			s.mu.Unlock()
			s.Store.Wake()
		}()
		if err := s.Hooks.VerifyNodeseek(s.Ctx, task); err != nil {
			if !canceled() && s.Guard.Notify != nil {
				s.Guard.Notify.Error(locales.ImportsNodeseekVerificationFailed(), err.Error())
			}
			return
		}
		if canceled() {
			return
		}
		if s.Store.RetryTask(s.Ctx, task.ID, services.ImportRetryOptions{CaptureStrategy: "authenticated"}) {
			s.requestCloseDetail(task.ID)
		}
	})
}

// cancelNodeseek 取消正在等待的 NodeSeek 验证。quiet 为真是弹窗关闭时的清理：失败不提示（渲染层卸载时 .catch(() => undefined)）。
func (s *Screen) cancelNodeseek(quiet bool) {
	s.mu.Lock()
	s.detail.nodeCanceled = true
	s.mu.Unlock()
	if s.Hooks.CancelNodeseek == nil {
		return
	}
	s.Store.Spawn(func() {
		if err := s.Hooks.CancelNodeseek(); err != nil && !quiet && s.Guard.Notify != nil {
			s.Guard.Notify.Error(locales.ImportsNodeseekCancelFailed(), err.Error())
		}
	})
}

// copyReport 复制诊断信息：版本与平台取不到不算失败（少一行环境信息的诊断文本仍然有用，为它把整次复制搞失败是本末倒置）。
// 进行中按钮禁用；成功与失败都弹提示，失败带可复制的原因。
func (s *Screen) copyReport(task services.ImportTask) {
	s.mu.Lock()
	if s.detail.copying {
		s.mu.Unlock()
		return
	}
	s.detail.copying = true
	s.mu.Unlock()
	now, loc, locale := s.nowTime().UnixMilli(), s.loc(), s.Locale
	s.Store.Spawn(func() {
		defer func() {
			s.mu.Lock()
			s.detail.copying = false
			s.mu.Unlock()
			s.Store.Wake()
		}()
		rc := ReportContext{Now: now, FormatTime: func(ms int64) string { return kit.FormatDateTime(ms, loc, locale) }}
		if s.Hooks.AppInfo != nil {
			rc.AppVersion, rc.Platform = s.Hooks.AppInfo()
		}
		if task.CaptureStrategy == "authenticated" && s.Hooks.CaptureBrowser != nil {
			rc.CaptureBrowser = s.Hooks.CaptureBrowser(DetectPlatformCapturePlatform(task.SourceInput))
		}
		if err := s.Guard.Notify.CopyText(BuildTaskReport(task, rc)); err != nil {
			if s.Guard.Notify != nil {
				s.Guard.Notify.Error(locales.ImportsReportCopyFailed(), err.Error())
			}
			return
		}
		if s.Guard.Notify != nil {
			s.Guard.Notify.Success(locales.ImportsReportCopied())
		}
	})
}
