package settings

import (
	"context"
	"errors"
	"strings"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// minRecoveryPassword 是恢复口令的最短长度（UTF-16 码元数，渲染层 `password.length < 12`）。
const minRecoveryPassword = 12

// repoBusy 是完整备份仓库一块正在做的事（渲染层 Busy）：做事期间其余按钮都禁用。
type repoBusy uint8

const (
	repoIdle repoBusy = iota
	repoInit
	repoCreate
	repoPreview
	repoRestore
	repoExport
	repoImport
	repoPassword
)

// RepositoryBackup 是「自动完整备份」一块（渲染层 RepositoryBackupSection）：未启用时是设置恢复口令与换机导入，
// 已启用时是密钥环状态、立即备份、快照列表、便携备份与改口令。
//
// 口令只存在文本框里：作为参数交给服务，不进日志、提示与错误文案；启用、改口令成功与分区被切走时清空文本框。
type RepositoryBackup struct {
	Look    *kit.Look
	Guard   kit.Guard
	Backup  services.Backup
	Wake    func()
	started bool

	status    *services.RepositoryStatus
	snapshots []services.BackupInfo
	loadErr   error // 首次读取失败：画读取失败与重试，不能一直转圈
	busy      repoBusy

	// password、confirm 是启用时的口令与确认；unlock 是备份、恢复、导出、导入时给的恢复口令（可空）；current、next 是改口令。
	password, confirm, unlock, current, next ui.Field
	changeOpen                               bool

	preview        *services.RestorePreview
	previewConfirm kit.ConfirmState
	deleteTarget   *services.BackupInfo
	deleteConfirm  kit.ConfirmState
	deleting       bool

	w worker
}

// Close 在分区被切走时调用：取消读取，丢弃还没送达的结果，清空口令文本框。
func (r *RepositoryBackup) Close() {
	r.w.close()
	for _, f := range []*ui.Field{&r.password, &r.confirm, &r.unlock, &r.current, &r.next} {
		f.SetText("")
	}
}

// reload 读取仓库状态与完整快照（渲染层 reload）。失败弹出带原因的提示；首次读取就失败时整块改画读取失败与重试。
func (r *RepositoryBackup) reload() {
	svc, g := r.Backup, r.Guard
	r.w.load(r.Wake, func(ctx context.Context) func() {
		var (
			st    services.RepositoryStatus
			snaps []services.BackupInfo
		)
		err := errDataUnavailable
		if svc != nil {
			if st, err = svc.RepositoryStatus(ctx); err == nil {
				snaps, err = svc.ListSnapshots(ctx)
			}
		}
		if err != nil && ctx.Err() == nil {
			g.Fail(locales.SettingsFullBackupLoadFailed(), locales.SettingsFullBackupTitle(), err)
		}
		return func() {
			r.loadErr = err
			if err == nil {
				r.status, r.snapshots = &st, snaps
			}
		}
	})
}

// Page 声明这一块（SettingSection：标题加卡片）。
func (r *RepositoryBackup) Page(c ui.C) {
	l := r.Look
	r.w.drain()
	if !r.started {
		r.started = true
		r.reload()
	}
	l.SettingSectionBody(c, locales.SettingsFullBackupTitle(), func() {
		switch {
		case r.status == nil && r.loadErr != nil:
			if l.LoadErrorState(c.Key("load-error"), r.loadErr.Error()) {
				r.loadErr = nil
				r.reload()
			}
		case r.status == nil:
			spinnerBox(l, c.Key("loading"))
		case !r.status.Initialized:
			r.setup(c.Key("setup"))
		default:
			r.ready(c.Key("ready"))
		}
	})
}

// wide 报告窗口是否不窄于 sm 断点（640）：口令两栏并排（grid sm:grid-cols-2）。
func (r *RepositoryBackup) wide(c ui.C) bool { return r.Look.ViewportWidth(c) >= 640 }

// ── 未启用 ──

func (r *RepositoryBackup) setup(c ui.C) {
	l := r.Look
	t := l.T
	idle := r.busy == repoIdle
	mismatch := r.confirm.Text() != "" && r.password.Text() != r.confirm.Text()
	ui.Column(c).FillWidth().Pad(l.Space(4)).Gap(l.Space(4)).Children(func() {
		r.intro(c.Key("intro"))
		mismatchText := ""
		if mismatch {
			mismatchText = locales.SettingsFullBackupPasswordMismatch()
		}
		if r.wide(c) {
			ui.Row(c.Key("pair")).FillWidth().Gap(l.Space(3)).Children(func() {
				r.pairField(c.Key("p1"), &r.password, locales.SettingsFullBackupSetPassword(), "")
				r.pairField(c.Key("p2"), &r.confirm, locales.SettingsFullBackupConfirmPassword(), mismatchText)
			})
		} else {
			ui.Column(c.Key("pair")).FillWidth().Gap(l.Space(3)).Children(func() {
				r.pairField(c.Key("p1"), &r.password, locales.SettingsFullBackupSetPassword(), "")
				r.pairField(c.Key("p2"), &r.confirm, locales.SettingsFullBackupConfirmPassword(), mismatchText)
			})
		}
		l.Text(c.Key("hint"), locales.SettingsFullBackupPasswordHint(), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
		enable := kit.ButtonSpec{
			Label: locales.SettingsFullBackupEnable(), Icon: icons.KeyRound,
			Disabled: !idle || jsLen(r.password.Text()) < minRecoveryPassword || r.password.Text() != r.confirm.Text(),
		}
		if btn(l, c.Key("enable"), enable, primaryBtn(l, shape(l, 2.25, 3, 2, kit.TextSM, kit.WeightMedium)), r.busy == repoInit) {
			r.initialize()
		}
		r.importBlock(c.Key("import"))
	})
}

// pairField 是启用表单里的一栏（grid 的一个单元：占满单元宽度）。
func (r *RepositoryBackup) pairField(c ui.C, f *ui.Field, label, errText string) {
	col := ui.Column(c).Grow(1).Shrink(1).Basis(0).MinWidth(0)
	col.Children(func() { labeledField(r.Look, c.Key("field"), f, label, "", errText, 0, true) })
}

// intro 是顶部的说明框：rounded-xl border border-primary/20 bg-primary/5 p-4，盾牌图标加两段文字。
func (r *RepositoryBackup) intro(c ui.C) {
	l := r.Look
	t := l.T
	surface := kit.Surface{Bg: t.Primary.WithAlpha(0.05), Border: t.Primary.WithAlpha(0.2), BorderW: 1, Radius: l.Rem(0.75)}
	ui.Row(c).FillWidth().Gap(l.Space(3)).Pad(l.Space(4) + 1).
		Draw(func(d *ui.DrawContext) { l.PaintSurface(d, surface) }).
		Children(func() {
			l.Icon(c.Key("icon"), icons.ShieldCheck, l.Rem(1.25), t.Primary).Shrink(0).MarginTop(l.Space(0.5))
			ui.Column(c.Key("text")).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				l.Text(c.Key("title"), locales.SettingsFullBackupIntroTitle(), kit.TextSM, kit.WeightMedium, t.Foreground, kit.Wrapped())
				l.Text(c.Key("desc"), locales.SettingsFullBackupIntroDesc(), kit.TextSM, kit.WeightNormal, t.MutedForeground, kit.Wrapped()).MarginTop(l.Space(1))
			})
		})
}

// importBlock 是未启用时的「已有便携备份（换机恢复）」：border-t border-border/60 pt-4。
func (r *RepositoryBackup) importBlock(c ui.C) {
	l := r.Look
	t := l.T
	ui.Column(c).FillWidth().PadSides(l.Space(4)+1, 0, 0, 0).
		Draw(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeTop, 1, t.Border.WithAlpha(0.6)) }).
		Children(func() {
			l.Text(c.Key("title"), locales.SettingsFullBackupImportTitle(), kit.TextSM, kit.WeightMedium, t.Foreground, kit.Wrapped())
			l.Text(c.Key("desc"), locales.SettingsFullBackupImportDesc(), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped()).MarginTop(l.Space(1))
			ui.Flow(c.Key("row")).FillWidth().MarginTop(l.Space(3)).Gap(l.Space(2)).LineGap(l.Space(2)).Items(ui.End).Children(func() {
				labeledField(l, c.Key("unlock"), &r.unlock, "", locales.SettingsFullBackupImportPasswordPlaceholder(), "", l.Rem(14), true)
				r.importButton(c.Key("import-button"))
			})
		})
}

func (r *RepositoryBackup) importButton(c ui.C) {
	l := r.Look
	spec := kit.ButtonSpec{Label: locales.SettingsFullBackupImportAction(), Icon: icons.FileInput, Disabled: r.busy != repoIdle}
	if btn(l, c, spec, outlineBtn(l, shape(l, 2.5, 3, 2, kit.TextSM, kit.WeightNormal), l.T.Muted), r.busy == repoImport) {
		r.importPortable()
	}
}

// ── 已启用 ──

func (r *RepositoryBackup) ready(c ui.C) {
	l := r.Look
	idle := r.busy == repoIdle
	ui.Column(c).FillWidth().Children(func() {
		ui.Column(c.Key("top")).FillWidth().Pad(l.Space(4)).Gap(l.Space(3)).Children(func() {
			r.keyring(c.Key("keyring"))
			ui.Flow(c.Key("actions")).FillWidth().Gap(l.Space(2)).LineGap(l.Space(2)).Items(ui.End).Children(func() {
				labeledField(l, c.Key("unlock"), &r.unlock, "", locales.SettingsFullBackupUnlockPlaceholder(), "", l.Rem(14), true)
				now := kit.ButtonSpec{Label: locales.SettingsFullBackupNow(), Icon: icons.DatabaseBackup, Disabled: !idle}
				if btn(l, c.Key("now"), now, primaryBtn(l, shape(l, 2.5, 3, 2, kit.TextSM, kit.WeightMedium)), r.busy == repoCreate) {
					r.createSnapshot()
				}
				r.importButton(c.Key("import-button"))
			})
		})
		r.snapshotList(c.Key("snapshots"))
		r.changePassword(c.Key("password"))
	})
}

// keyring 是密钥环状态条：可无人值守备份时是绿色，否则是琥珀色并说明原因（rounded-lg border px-3 py-2 text-sm）。
func (r *RepositoryBackup) keyring(c ui.C) {
	l := r.Look
	st := r.status
	pick := func(light, dark string) tokens.Color {
		if l.Dark {
			return tokens.Tailwind(dark)
		}
		return tokens.Tailwind(light)
	}
	var text string
	var bg, border, fg tokens.Color
	if st.AutomaticAccessAvailable {
		backend := "secure storage"
		if st.KeyStorageBackend != nil && *st.KeyStorageBackend != "" {
			backend = *st.KeyStorageBackend
		}
		text = locales.SettingsFullBackupKeyringReady(backend)
		bg, border, fg = tokens.Tailwind("emerald-500").WithAlpha(0.05), tokens.Tailwind("emerald-500").WithAlpha(0.2), pick("emerald-700", "emerald-300")
	} else {
		text = st.Warning
		bg, border, fg = tokens.Tailwind("amber-500").WithAlpha(0.1), tokens.Tailwind("amber-500").WithAlpha(0.3), pick("amber-700", "amber-300")
	}
	if text == "" {
		return
	}
	ui.Column(c).FillWidth().PadSides(l.Space(2)+1, l.Space(3)+1, l.Space(2)+1, l.Space(3)+1).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: bg, Border: border, BorderW: 1, Radius: l.Rem(0.75)})
		}).
		Children(func() { l.Text(c.Key("text"), text, kit.TextSM, kit.WeightNormal, fg, kit.Wrapped()) })
}

// snapshotList 是完整快照列表（border-t border-border/60，最高 max-h-80 内滚动）；没有快照时是说明与换机提示。
func (r *RepositoryBackup) snapshotList(c ui.C) {
	l := r.Look
	t := l.T
	top := func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeTop, 1, t.Border.WithAlpha(0.6)) }
	if len(r.snapshots) == 0 {
		ui.Column(c).FillWidth().Items(ui.Center).PadSides(l.Space(6)+1, l.Space(4), l.Space(6), l.Space(4)).Draw(top).Children(func() {
			l.Text(c.Key("none"), locales.SettingsFullBackupNoSnapshots(), kit.TextSM, kit.WeightNormal, t.MutedForeground, kit.Centered(), kit.Wrapped())
			l.Text(c.Key("hint"), locales.SettingsFullBackupNoSnapshotsImportHint(), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Centered(), kit.Wrapped()).MarginTop(l.Space(1))
		})
		return
	}
	ui.Column(c).FillWidth().PadSides(1, 0, 0, 0).Draw(top).Children(func() {
		l.ScrollArea(c.Key("list"), [4]float32{}, func() {
			for i, s := range r.snapshots {
				r.snapshotRow(c.Key("row-"+s.FileName), s, i == len(r.snapshots)-1)
			}
		}).FillWidth().MaxHeight(l.Rem(20)) // max-h-80
	})
}

func (r *RepositoryBackup) snapshotRow(c ui.C, s services.BackupInfo, last bool) {
	l := r.Look
	t := l.T
	pad := [4]float32{l.Space(3), l.Space(4), l.Space(3), l.Space(4)}
	if !last {
		pad[2]++
	}
	idle := r.busy == repoIdle
	ui.Row(c).FillWidth().Items(ui.Center).Gap(l.Space(3)).PadSides(pad[0], pad[1], pad[2], pad[3]).
		DrawOver(func(d *ui.DrawContext) {
			if !last {
				l.PaintEdge(d, kit.EdgeBottom, 1, t.Border.WithAlpha(0.5))
			}
		}).
		Children(func() {
			kind := locales.SettingsBackupKindManual()
			if s.Kind == services.BackupKindAuto {
				kind = locales.SettingsBackupKindAuto()
			}
			pill(l, c.Key("kind"), kind, t.Primary.WithAlpha(0.1), t.Primary, 0, false)
			ui.Column(c.Key("info")).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				l.Text(c.Key("time"), formatDateTime(s.CreatedAt, false), kit.TextSM, kit.WeightNormal, t.Foreground)
				var items, assets, schema int
				if s.Summary != nil {
					items, assets, schema = s.Summary.ItemCount, s.Summary.AssetCount, s.Summary.SchemaVersion
				}
				l.Text(c.Key("summary"), locales.SettingsFullBackupSnapshotSummary(items, assets, formatBytes(s.SizeBytes, false), schema),
					kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Ellipsis())
			})
			ic, size := iconOf(l, icons.Download, false)
			export := kit.ButtonSpec{Name: locales.SettingsFullBackupExportPortable(), Hint: locales.SettingsFullBackupExportPortable(), Icon: ic, IconSize: size, Center: true, Disabled: !idle}
			if btn(l, c.Key("export"), export, iconBtn(l, l.Rem(2), t.Muted, t.Foreground), false) {
				r.exportPortable(s)
			}
			ic, size = iconOf(l, icons.ArchiveRestore, true)
			preview := kit.ButtonSpec{Label: locales.SettingsFullBackupRestoreAction(), Icon: ic, IconSize: size, Disabled: !idle}
			if btn(l, c.Key("preview"), preview, outlineBtn(l, shape(l, 2, 2.5, 1, kit.TextXS, kit.WeightNormal), t.Muted), r.busy == repoPreview && false) {
				r.openPreview(s)
			}
			ic, size = iconOf(l, icons.Trash2, false)
			del := kit.ButtonSpec{Name: locales.SettingsFullBackupDeleteSnapshot(), Hint: locales.SettingsFullBackupDeleteSnapshot(), Icon: ic, IconSize: size, Center: true, Disabled: !idle}
			if btn(l, c.Key("delete"), del, iconBtn(l, l.Rem(2), t.Destructive.WithAlpha(0.1), t.Destructive), false) {
				target := s
				r.deleteTarget = &target
			}
		})
}

// changePassword 是列表下面的折叠「修改恢复口令」（<details>，默认收起）。
func (r *RepositoryBackup) changePassword(c ui.C) {
	l := r.Look
	t := l.T
	ui.Column(c).FillWidth().Pad(l.Space(4) + 1).
		Draw(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeTop, 1, t.Border.WithAlpha(0.6)) }).
		Children(func() {
			if summaryRow(l, c.Key("details"), locales.SettingsFullBackupChangePassword(), r.changeOpen, t.Foreground) {
				r.changeOpen = !r.changeOpen
			}
			if !r.changeOpen {
				return
			}
			ui.Flow(c.Key("form")).FillWidth().MarginTop(l.Space(3)).Gap(l.Space(2)).LineGap(l.Space(2)).Items(ui.End).Children(func() {
				labeledField(l, c.Key("current"), &r.current, locales.SettingsFullBackupCurrentPassword(), "", "", l.Rem(13), true)
				labeledField(l, c.Key("next"), &r.next, locales.SettingsFullBackupNewPassword(), "", "", l.Rem(13), true)
				spec := kit.ButtonSpec{Label: locales.SettingsFullBackupConfirmChange(), Disabled: r.busy != repoIdle}
				if btn(l, c.Key("confirm"), spec, outlineBtn(l, shape(l, 2.5, 3, 0, kit.TextSM, kit.WeightNormal), t.Muted), r.busy == repoPassword) {
					r.changeRecoveryPassword()
				}
			})
		})
}

// Dialogs 声明恢复预检通过后的确认与删除确认（浮层，不占版面）。
func (r *RepositoryBackup) Dialogs(c ui.C) {
	l := r.Look
	var spec kit.ConfirmSpec
	spec = kit.ConfirmSpec{
		Open: r.preview != nil, Title: locales.SettingsFullBackupRestoreConfirmTitle(),
		ConfirmText: locales.SettingsFullBackupRestoreAndRestart(), CancelText: locales.CommonCancel(),
		Destructive: true, Loading: r.busy == repoRestore,
	}
	if r.preview != nil {
		s := r.preview.Snapshot.Summary
		var items, assets, domains int
		if s != nil {
			items, assets, domains = s.ItemCount, s.AssetCount, len(s.ConfigDomains)
		}
		spec.Message = locales.SettingsFullBackupRestoreConfirmMessage(items, assets, domains)
	}
	switch l.ConfirmDialog(c.Key("restore"), &r.previewConfirm, spec) {
	case kit.ConfirmAccepted:
		r.restore()
	case kit.ConfirmDismissed:
		r.preview = nil
	}
	spec = kit.ConfirmSpec{
		Open: r.deleteTarget != nil, Title: locales.SettingsFullBackupDeleteTitle(), Message: locales.SettingsFullBackupDeleteMessage(),
		ConfirmText: locales.CommonDelete(), CancelText: locales.CommonCancel(), Destructive: true, Loading: r.deleting,
	}
	switch l.ConfirmDialog(c.Key("delete"), &r.deleteConfirm, spec) {
	case kit.ConfirmAccepted:
		r.removeSnapshot()
	case kit.ConfirmDismissed:
		r.deleteTarget = nil
	}
}

// ── 操作 ──

// initialize 启用完整备份：两次口令不一致时只提示、不调用服务；成功清空口令框并重新读取。
func (r *RepositoryBackup) initialize() {
	if r.busy != repoIdle {
		return
	}
	password := r.password.Text()
	if password != r.confirm.Text() {
		r.Guard.Reject(locales.SettingsFullBackupPasswordMismatch())
		return
	}
	r.busy = repoInit
	svc, g := r.Backup, r.Guard
	r.w.act(r.Wake, func() func() {
		err := errDataUnavailable
		if svc != nil {
			err = svc.InitRepository(context.Background(), password)
		}
		if err != nil {
			g.Fail(locales.SettingsFullBackupInitFailed(), locales.SettingsFullBackupEnable(), err)
			return func() { r.busy = repoIdle }
		}
		g.Succeed(locales.SettingsFullBackupEnabled())
		return func() {
			r.busy = repoIdle
			r.password.SetText("")
			r.confirm.SetText("")
			r.reload()
		}
	})
}

// createSnapshot 立即做一次完整备份：恢复口令可空（系统密钥环可用时不需要）。
func (r *RepositoryBackup) createSnapshot() {
	if r.busy != repoIdle {
		return
	}
	r.busy = repoCreate
	unlock := r.unlock.Text()
	svc, g := r.Backup, r.Guard
	r.w.act(r.Wake, func() func() {
		var res services.SnapshotCreated
		err := errDataUnavailable
		if svc != nil {
			res, err = svc.CreateSnapshot(context.Background(), unlock)
		}
		if err != nil {
			g.Fail(locales.SettingsFullBackupFailed(), locales.SettingsFullBackupNow(), err)
			return func() { r.busy = repoIdle }
		}
		g.Succeed(locales.SettingsFullBackupCreated(res.CreatedObjects, res.ReusedObjects))
		return func() { r.busy = repoIdle; r.reload() }
	})
}

// openPreview 恢复预检：通过才弹确认；不通过时把缺失与损坏的文件逐行列在提示详情里。
func (r *RepositoryBackup) openPreview(s services.BackupInfo) {
	if r.busy != repoIdle {
		return
	}
	r.busy = repoPreview
	unlock := r.unlock.Text()
	svc, g := r.Backup, r.Guard
	r.w.act(r.Wake, func() func() {
		var res services.RestorePreview
		err := errDataUnavailable
		if svc != nil {
			res, err = svc.PreviewRestore(context.Background(), s.FileName, unlock)
		}
		if err != nil {
			title, detail := locales.SettingsFullBackupPreviewFailed(), err
			var pe *services.RestorePreviewError
			if errors.As(err, &pe) {
				if strings.TrimSpace(pe.Message) != "" {
					title = pe.Message
				}
				var lines []string
				for _, name := range pe.MissingFiles {
					lines = append(lines, locales.SettingsFullBackupMissingFile(name))
				}
				for _, name := range pe.DamagedFiles {
					lines = append(lines, locales.SettingsFullBackupDamagedFile(name))
				}
				if len(lines) > 0 {
					detail = errors.New(strings.Join(lines, "\n"))
				}
			}
			g.Fail(title, locales.SettingsFullBackupRestoreAction(), detail)
			return func() { r.busy = repoIdle }
		}
		return func() { r.busy = repoIdle; r.preview = &res }
	})
}

// restore 用预检通过的快照替换当前数据：成功后外壳随即重启，对话框一直转圈；失败回到可操作并提示原因（对话框保留，可重试）。
func (r *RepositoryBackup) restore() {
	if r.busy != repoIdle || r.preview == nil {
		return
	}
	r.busy = repoRestore
	id, unlock := r.preview.Snapshot.FileName, r.unlock.Text()
	svc, g := r.Backup, r.Guard
	r.w.act(r.Wake, func() func() {
		err := errDataUnavailable
		if svc != nil {
			err = svc.RestoreSnapshot(context.Background(), id, unlock)
		}
		if err != nil {
			g.Fail(locales.SettingsBackupRestoreFailed(), locales.SettingsFullBackupRestoreAndRestart(), err)
			return func() { r.busy = repoIdle }
		}
		g.Succeed(locales.SettingsFullBackupRestored())
		return func() {} // 主进程随后重启
	})
}

// exportPortable 选位置导出便携备份：用户取消什么都不提示。
func (r *RepositoryBackup) exportPortable(s services.BackupInfo) {
	if r.busy != repoIdle {
		return
	}
	r.busy = repoExport
	unlock := r.unlock.Text()
	svc, g := r.Backup, r.Guard
	r.w.act(r.Wake, func() func() {
		var res services.PortableExport
		err := errDataUnavailable
		if svc != nil {
			res, err = svc.ExportPortable(context.Background(), s.FileName, unlock)
		}
		switch {
		case err != nil:
			g.Fail(locales.SettingsFullBackupExportFailed(), locales.SettingsFullBackupExportPortable(), err)
		case !res.Canceled:
			g.Succeed(locales.SettingsFullBackupExported())
		}
		return func() { r.busy = repoIdle }
	})
}

// importPortable 换机恢复：先要恢复口令；导入后快照并入本机仓库，是否替换当前数据仍由「预检并恢复」决定。
func (r *RepositoryBackup) importPortable() {
	if r.busy != repoIdle {
		return
	}
	unlock := r.unlock.Text()
	if unlock == "" {
		r.Guard.Reject(locales.SettingsFullBackupImportNeedPassword())
		return
	}
	r.busy = repoImport
	svc, g := r.Backup, r.Guard
	r.w.act(r.Wake, func() func() {
		var res services.PortableImport
		err := errDataUnavailable
		if svc != nil {
			res, err = svc.ImportPortable(context.Background(), unlock)
		}
		switch {
		case err != nil:
			g.Fail(locales.SettingsFullBackupImportFailed(), locales.SettingsFullBackupImportAction(), err)
			return func() { r.busy = repoIdle }
		case res.Canceled:
			return func() { r.busy = repoIdle }
		case res.AlreadyExisted:
			g.Succeed(locales.SettingsFullBackupImportExisted())
		default:
			detail := ""
			if res.ReplacedRepository {
				detail = locales.SettingsFullBackupImportReplacedDetail()
			}
			succeedWithDetail(g, locales.SettingsFullBackupImported(), detail)
		}
		return func() { r.busy = repoIdle; r.reload() }
	})
}

// changeRecoveryPassword 修改恢复口令：成功清空两个口令框；失败提示原因。
func (r *RepositoryBackup) changeRecoveryPassword() {
	if r.busy != repoIdle {
		return
	}
	r.busy = repoPassword
	current, next := r.current.Text(), r.next.Text()
	svc, g := r.Backup, r.Guard
	r.w.act(r.Wake, func() func() {
		err := errDataUnavailable
		if svc != nil {
			err = svc.ChangePassword(context.Background(), current, next)
		}
		if err != nil {
			g.Fail(locales.SettingsFullBackupPasswordChangeFailed(), locales.SettingsFullBackupChangePassword(), err)
			return func() { r.busy = repoIdle }
		}
		g.Succeed(locales.SettingsFullBackupPasswordChanged())
		return func() {
			r.busy = repoIdle
			r.current.SetText("")
			r.next.SetText("")
		}
	})
}

// removeSnapshot 删除确认框里选中的快照：对话框在删除期间转圈；失败提示原因；无论成败都重新读取。
func (r *RepositoryBackup) removeSnapshot() {
	if r.deleting || r.deleteTarget == nil {
		return
	}
	r.deleting = true
	id := r.deleteTarget.FileName
	svc, g := r.Backup, r.Guard
	r.w.act(r.Wake, func() func() {
		err := errDataUnavailable
		if svc != nil {
			err = svc.DeleteSnapshot(context.Background(), id)
		}
		if err != nil {
			g.Fail(locales.SettingsBackupDeleteFailed(), locales.SettingsFullBackupDeleteSnapshot(), err)
		}
		return func() { r.deleting = false; r.deleteTarget = nil; r.reload() }
	})
}
