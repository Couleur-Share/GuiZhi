package settings

import (
	"context"
	"errors"
	"strconv"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// errDataUnavailable 是外壳没有接上备份、数据或配置迁移服务时的失败原因（进「查看详情」与日志，不进界面文案）。
var errDataUnavailable = errors.New("the data and backup service is not available in this build")

// dataBusy 是旧快照一块正在做的事（渲染层 BusyAction，加上删除）：做事期间其余按钮都禁用。
type dataBusy uint8

const (
	dataIdle dataBusy = iota
	dataCreating
	dataRestoring
	dataExporting
	dataDeleting
)

// snapshotAsk 是旧快照的确认对话框：恢复或删除哪一份由 target 给出。
type snapshotAsk uint8

const (
	askNone snapshotAsk = iota
	askRestore
	askDelete
)

// Data 是「数据与备份」页（渲染层 DataSettings）：完整备份计划、完整备份仓库、旧数据库快照、Markdown 导出与配置迁移五块。
//
// 备份、恢复、导出、配置读写都经 services 接口交给主包的处理器（备份走 VACUUM INTO 与 WAL 检查点，机密走 DPAPI 或口令加密），
// 这一层只管界面与失败提示。渲染层的页面没有「数据目录迁移」入口（它在引导流程里），这里也不放。
type Data struct {
	Look  *kit.Look
	Store Store
	// Guard 接住写入与操作失败：错误提示（原因折叠在「查看详情」里）并记日志。
	Guard kit.Guard
	// Runtime、Backup、Transfer 是本页要用的服务；nil 表示外壳没接，相应操作如实报错。
	Runtime  services.Data
	Backup   services.Backup
	Transfer services.ConfigTransfer
	// Wake 请求窗口重绘：后台结果回到界面线程前调用。可为 nil。
	Wake func()
	// Locale 是时间的区域写法（系统区域，kit.ParseDateLocale）；零值按中文。
	Locale kit.DateLocale

	intervalSel, keepSel kit.SelectState

	started   bool
	backups   []services.BackupInfo
	loading   bool  // 旧快照列表读取中
	loadErr   error // 列表读取失败：画读取失败与重试，不渲染成「还没有备份」
	paths     services.RuntimePaths
	havePaths bool
	busy      dataBusy

	ask     snapshotAsk
	target  services.BackupInfo
	confirm kit.ConfirmState

	repo     RepositoryBackup
	transfer ConfigTransferSection
	w        worker
}

// Close 在分区被切走时调用：取消读取请求，丢弃还没送达的结果；进行中的备份与恢复不被打断。
func (d *Data) Close() {
	d.w.close()
	d.repo.Close()
	d.transfer.Close()
}

// Page 声明整页：标题加五个分区，再加对话框（浮层，不占版面）。
func (d *Data) Page(c ui.C) {
	l := d.Look
	d.w.drain()
	d.start()
	d.repo.Look, d.repo.Guard, d.repo.Backup, d.repo.Wake, d.repo.Locale = l, d.Guard, d.Backup, d.Wake, d.Locale
	d.transfer.Look, d.transfer.Guard, d.transfer.Service, d.transfer.Runtime, d.transfer.Wake, d.transfer.Locale = l, d.Guard, d.Transfer, d.Runtime, d.Wake, d.Locale
	l.SettingsContent(c, locales.SettingsData(), func() {
		l.SettingSections(c, func() {
			d.plan(c)
			d.repo.Page(c.Key("repo"))
			d.snapshots(c.Key("snapshots"))
			d.export(c)
			d.transfer.Page(c.Key("transfer"))
		})
	})
	d.dialog(c)
	d.repo.Dialogs(c.Key("repo-dialogs"))
	d.transfer.Dialogs(c.Key("transfer-dialogs"))
}

// start 在页面第一次出现时读取旧快照列表与运行时路径（渲染层 useEffect(reloadBackups)）。
func (d *Data) start() {
	if d.started {
		return
	}
	d.started = true
	d.reload()
	d.loadPaths()
}

func (d *Data) save(mutate func(*settings.Values)) bool { return save(d.Guard, d.Store, mutate) }

// reload 重新读取旧快照列表：读取期间显示转圈；失败弹出带原因的错误提示，列表区改成读取失败加重试。
func (d *Data) reload() {
	d.loading = true
	svc, g := d.Backup, d.Guard
	d.w.load(d.Wake, func(ctx context.Context) func() {
		var list []services.BackupInfo
		err := errDataUnavailable
		if svc != nil {
			list, err = svc.List(ctx)
		}
		if err != nil && ctx.Err() == nil {
			g.Fail(locales.SettingsBackupListFailed(), locales.SettingsBackupListFailed(), err)
		}
		return func() {
			d.loading = false
			d.loadErr = err
			if err == nil {
				d.backups = list
			}
		}
	})
}

// loadPaths 读取备份目录与日志目录；读不到时两个「打开」按钮保持禁用（渲染层同样），原因记进日志。
func (d *Data) loadPaths() {
	rt, g := d.Runtime, d.Guard
	d.w.load(d.Wake, func(ctx context.Context) func() {
		if rt == nil {
			return func() {}
		}
		p, err := rt.RuntimePaths(ctx)
		if err != nil {
			g.Background("read runtime paths", err)
			return func() {}
		}
		return func() { d.paths, d.havePaths = p, true }
	})
}

// open 用系统文件管理器打开目录；失败弹出带原因的错误提示（action 是按钮的可见文字）。
func (d *Data) open(dir, action string) {
	rt := d.Runtime
	d.Guard.Go(action, func() error {
		if rt == nil {
			return errDataUnavailable
		}
		return rt.OpenPath(context.Background(), dir)
	}, nil)
}

// ── 完整备份计划 ──

func intervalOptions() []kit.SelectOption {
	return []kit.SelectOption{
		{Value: "12", Label: locales.SettingsBackupInterval12h()},
		{Value: "24", Label: locales.SettingsBackupInterval24h()},
		{Value: "72", Label: locales.SettingsBackupInterval72h()},
		{Value: "168", Label: locales.SettingsBackupInterval168h()},
	}
}

func keepOptions() []kit.SelectOption {
	return []kit.SelectOption{{Value: "5", Label: "5"}, {Value: "10", Label: "10"}, {Value: "20", Label: "20"}, {Value: "30", Label: "30"}}
}

func (d *Data) plan(c ui.C) {
	l := d.Look
	app := func() settings.App { return d.Store.Values().App }
	l.SettingSection(c, locales.SettingsBackupAutoSection(), []kit.SettingRow{
		{
			Label: locales.SettingsBackupAuto(), Description: locales.SettingsBackupAutoDesc(),
			Control: func() {
				on := app().BackupAutoEnabled
				if l.Switch(c, &on, locales.SettingsBackupAuto()).Clicked() {
					next := on
					d.save(func(v *settings.Values) { v.App.BackupAutoEnabled = next })
				}
			},
		},
		{
			Label: locales.SettingsBackupInterval(), Description: locales.SettingsBackupIntervalDesc(),
			Control: func() {
				picked, ok := l.Select(c, &d.intervalSel, kit.SelectSpec{
					Label: locales.SettingsBackupInterval(), Value: strconv.Itoa(app().BackupIntervalHours),
					Options: intervalOptions(), Width: l.Rem(8), // w-32
				})
				if ok {
					d.save(func(v *settings.Values) { v.App.BackupIntervalHours = parseNumber(picked) })
				}
			},
		},
		{
			Label: locales.SettingsBackupKeep(), Description: locales.SettingsBackupKeepDesc(),
			Control: func() {
				picked, ok := l.Select(c, &d.keepSel, kit.SelectSpec{
					Label: locales.SettingsBackupKeep(), Value: strconv.Itoa(app().BackupKeepCount),
					Options: keepOptions(), Width: l.Rem(8), // w-32
				})
				if ok {
					d.save(func(v *settings.Values) { v.App.BackupKeepCount = parseNumber(picked) })
				}
			},
		},
	})
}

// ── 旧数据库快照 ──

func (d *Data) snapshots(c ui.C) {
	l := d.Look
	l.SettingSectionBody(c, locales.SettingsBackupSection(), func() {
		d.toolbar(c)
		switch {
		case d.loading:
			spinnerBox(l, c.Key("loading"))
		case d.loadErr != nil:
			if l.LoadErrorState(c.Key("load-error"), d.loadErr.Error()) {
				d.reload()
			}
		case len(d.backups) == 0:
			l.Text(c.Key("empty"), locales.SettingsBackupEmpty(), kit.TextSM, kit.WeightNormal, l.T.MutedForeground, kit.Centered(), kit.Wrapped()).
				FillWidth().PadSides(l.Space(6), l.Space(4), l.Space(6), l.Space(4))
		default:
			l.ScrollArea(c.Key("list"), [4]float32{}, func() {
				for i, b := range d.backups {
					d.snapshotRow(c.Key("row-"+b.FileName), b, i == len(d.backups)-1)
				}
			}).FillWidth().MaxHeight(l.Rem(18)) // max-h-72
		}
	})
}

// toolbar 是列表上方的三枚按钮（flex items-center gap-2 px-4 py-3 border-b border-border/70）。
func (d *Data) toolbar(c ui.C) {
	l := d.Look
	t := l.T
	idle := d.busy == dataIdle
	ui.Row(c.Key("toolbar")).FillWidth().Items(ui.Center).Gap(l.Space(2)).
		PadSides(l.Space(3), l.Space(4), l.Space(3)+1, l.Space(4)).
		DrawOver(func(dc *ui.DrawContext) { l.PaintEdge(dc, kit.EdgeBottom, 1, t.Border.WithAlpha(0.7)) }).
		Children(func() {
			primary := primaryBtn(l, shape(l, 2.25, 3, 1.5, kit.TextSM, kit.WeightMedium))
			outline := outlineBtn(l, shape(l, 2.25, 3, 1.5, kit.TextSM, kit.WeightNormal), t.Muted.WithAlpha(0.6))
			create := kit.ButtonSpec{Label: locales.SettingsBackupNow(), Icon: icons.DatabaseBackup, Disabled: !idle}
			if btn(l, c.Key("create"), create, primary, d.busy == dataCreating) {
				d.create()
			}
			openDir := kit.ButtonSpec{Label: locales.SettingsOpenBackupsDir(), Icon: icons.FolderOpen, Disabled: !d.havePaths}
			if btn(l, c.Key("open-backups"), openDir, outline, false) {
				d.open(d.paths.BackupsDir, locales.SettingsOpenBackupsDir())
			}
			// 后台任务（定时备份、后台 Wiki 编译）失败时不弹窗，只记进 error.log；这个按钮是它们唯一的出口。
			logs := kit.ButtonSpec{Label: locales.SettingsOpenLogs(), Icon: icons.ScrollText, Disabled: !d.havePaths, Hint: locales.SettingsOpenLogsHint(), Description: locales.SettingsOpenLogsHint()}
			if btn(l, c.Key("open-logs"), logs, outline, false) {
				d.open(d.paths.LogsDir, locales.SettingsOpenLogs())
			}
		})
}

// snapshotRow 是一份旧快照：来源徽标、时间、文件名与大小，恢复与删除两个动作（flex items-center gap-3 px-4 py-2.5，非末行带 border-b border-border/50）。
func (d *Data) snapshotRow(c ui.C, b services.BackupInfo, last bool) {
	l := d.Look
	t := l.T
	pad := [4]float32{l.Space(2.5), l.Space(4), l.Space(2.5), l.Space(4)}
	if !last {
		pad[2]++
	}
	idle := d.busy == dataIdle
	ui.Row(c).FillWidth().Items(ui.Center).Gap(l.Space(3)).PadSides(pad[0], pad[1], pad[2], pad[3]).
		DrawOver(func(dc *ui.DrawContext) {
			if !last {
				l.PaintEdge(dc, kit.EdgeBottom, 1, t.Border.WithAlpha(0.5))
			}
		}).
		Children(func() {
			backupKindBadge(l, c.Key("badge"), b.Kind)
			ui.Column(c.Key("info")).Grow(1).Basis(0).MinWidth(0).Children(func() {
				l.Text(c.Key("time"), formatDateTime(b.CreatedAt, true, d.Locale), kit.TextSM, kit.WeightNormal, t.Foreground, kit.Ellipsis())
				backupFileMeta(l, c.Key("file"), b.FileName, formatBytes(b.SizeBytes, true))
			})
			ic, size := iconOf(l, icons.ArchiveRestore, true)
			restore := kit.ButtonSpec{Label: locales.SettingsBackupRestore(), Icon: ic, IconSize: size, Disabled: !idle}
			if btn(l, c.Key("restore"), restore, outlineBtn(l, shape(l, 2, 2.5, 1, kit.TextXS, kit.WeightNormal), t.Muted.WithAlpha(0.6)), false) {
				d.ask, d.target = askRestore, b
			}
			ic, size = iconOf(l, icons.Trash2, true)
			del := kit.ButtonSpec{Name: locales.SettingsBackupDelete(), Hint: locales.SettingsBackupDelete(), Icon: ic, IconSize: size, Center: true, Disabled: !idle}
			if btn(l, c.Key("delete"), del, iconBtn(l, l.Rem(2), t.Destructive.WithAlpha(0.1), t.Destructive), false) {
				d.ask, d.target = askDelete, b
			}
		})
}

// dialog 是恢复与删除的确认对话框（破坏性，红色确认钮）。
func (d *Data) dialog(c ui.C) {
	l := d.Look
	spec := kit.ConfirmSpec{
		Open: d.ask != askNone, ConfirmText: locales.CommonConfirm(), CancelText: locales.CommonCancel(), Destructive: true,
	}
	switch d.ask {
	case askRestore:
		spec.Title = locales.SettingsBackupRestoreConfirmTitle()
		spec.Message = locales.SettingsBackupRestoreConfirmMessage(formatDateTime(d.target.CreatedAt, true, d.Locale))
	case askDelete:
		spec.Title = locales.SettingsBackupDelete()
		spec.Message = locales.SettingsBackupDeleteConfirmMessage(d.target.FileName)
	}
	switch l.ConfirmDialog(c, &d.confirm, spec) {
	case kit.ConfirmAccepted:
		ask, target := d.ask, d.target
		d.ask = askNone
		switch ask {
		case askRestore:
			d.restore(target)
		case askDelete:
			d.remove(target)
		}
	case kit.ConfirmDismissed:
		d.ask = askNone
	}
}

// create 立即创建一份手动旧快照：后台进行，期间按钮转圈、其余禁用；失败的提示带原因，用户可以直接再点一次。
func (d *Data) create() {
	if d.busy != dataIdle {
		return
	}
	d.busy = dataCreating
	svc, g := d.Backup, d.Guard
	d.w.act(d.Wake, func() func() {
		err := errDataUnavailable
		if svc != nil {
			_, err = svc.Create(context.Background())
		}
		if err != nil {
			g.Fail(locales.SettingsBackupFailed(), locales.SettingsBackupNow(), err)
			return func() { d.busy = dataIdle }
		}
		g.Succeed(locales.SettingsBackupCreated())
		return func() { d.busy = dataIdle; d.reload() }
	})
}

// restore 用选中的旧快照替换当前数据：成功后外壳随即重启，所以界面一直保持忙碌；失败回到空闲并提示原因。
func (d *Data) restore(b services.BackupInfo) {
	if d.busy != dataIdle {
		return
	}
	d.busy = dataRestoring
	svc, g := d.Backup, d.Guard
	d.w.act(d.Wake, func() func() {
		err := errDataUnavailable
		if svc != nil {
			err = svc.Restore(context.Background(), b.FileName)
		}
		if err != nil {
			g.Fail(locales.SettingsBackupRestoreFailed(), locales.SettingsBackupRestore(), err)
			return func() { d.busy = dataIdle }
		}
		g.Succeed(locales.SettingsBackupRestoring())
		return func() {} // 主进程随后重启；保持忙碌直到重启
	})
}

// remove 删除一份旧快照：失败弹出带原因的提示；无论成败都重新读取列表。
func (d *Data) remove(b services.BackupInfo) {
	if d.busy != dataIdle {
		return
	}
	d.busy = dataDeleting
	svc, g := d.Backup, d.Guard
	d.w.act(d.Wake, func() func() {
		err := errDataUnavailable
		if svc != nil {
			err = svc.Delete(context.Background(), b.FileName)
		}
		if err != nil {
			g.Fail(locales.SettingsBackupDeleteFailed(), locales.SettingsBackupDelete(), err)
		}
		return func() { d.busy = dataIdle; d.reload() }
	})
}

// ── Markdown 导出 ──

func (d *Data) export(c ui.C) {
	l := d.Look
	l.SettingSection(c, locales.SettingsExportSection(), []kit.SettingRow{{
		Label: locales.SettingsExportMarkdown(), Description: locales.SettingsExportMarkdownDesc(),
		Control: func() {
			spec := kit.ButtonSpec{Label: locales.SettingsExportStart(), Icon: icons.FileDown, Disabled: d.busy != dataIdle}
			st := outlineBtn(l, shape(l, 2.25, 3, 1.5, kit.TextSM, kit.WeightNormal), l.T.Muted.WithAlpha(0.6))
			if btn(l, c.Key("export"), spec, st, d.busy == dataExporting) {
				d.exportMarkdown()
			}
		},
	}})
}

// exportMarkdown 选目录并导出全部条目：用户取消什么都不提示；成功提示条数并在文件管理器里打开目录。
func (d *Data) exportMarkdown() {
	if d.busy != dataIdle {
		return
	}
	d.busy = dataExporting
	svc, rt, g := d.Backup, d.Runtime, d.Guard
	d.w.act(d.Wake, func() func() {
		var res services.MarkdownExport
		err := errDataUnavailable
		if svc != nil {
			res, err = svc.ExportMarkdown(context.Background())
		}
		switch {
		case err != nil:
			g.Fail(locales.SettingsExportFailed(), locales.SettingsExportMarkdown(), err)
		case !res.Canceled:
			g.Succeed(locales.SettingsExportDone(res.Count))
			if rt != nil && res.Dir != "" {
				// 打开目录只是顺手：导出已经成功，失败只记日志，不再弹第二条提示。
				g.Background(locales.SettingsExportMarkdown(), rt.OpenPath(context.Background(), res.Dir))
			}
		}
		return func() { d.busy = dataIdle }
	})
}

// backupFileMeta 是行里的「文件名 · 大小」：渲染层的 {fileName} · {size} 是三个相邻的文本节点（truncate 的 p），
// 所以拆成三段画、对照才逐段对得上；文件名一段在放不下时带省略号收缩，分隔与大小保持完整。
func backupFileMeta(l *kit.Look, c ui.C, name, size string) {
	st := kit.TextXS
	px := l.Rem(st.Size)
	canvas := c.Canvas()
	muted := l.T.MutedForeground
	ui.Row(c).FillWidth().MinWidth(0).Items(ui.Start).Children(func() {
		l.Text(c.Key("name"), name, st, kit.WeightNormal, muted, kit.Ellipsis()).Width(canvas.TextWidthWeight(name, px, kit.WeightNormal)).Shrink(1).MinWidth(0)
		for i, run := range []string{" \u00b7 ", size} {
			l.Text(c.Key("tail"+string(rune('0'+i))), run, st, kit.WeightNormal, muted).Width(canvas.TextWidthWeight(run, px, kit.WeightNormal)).Shrink(0)
		}
	})
}
