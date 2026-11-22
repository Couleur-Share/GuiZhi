package settings

import (
	"context"
	"time"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// relaunchDelay 是导入成功后到重启之间留给用户看提示的时间（渲染层 setTimeout 1500）。
const relaunchDelay = 1500 * time.Millisecond

// minExportPassword 是导出密码的最短长度（渲染层 MIN_PASSWORD_LENGTH）：密码保护的是 API Key，太短的口令拦不住离线爆破。
const minExportPassword = 6

// xferBusy 是配置迁移正在做的事（渲染层 ConfigTransferBusy）：做事期间两个入口都禁用。
type xferBusy uint8

const (
	xferIdle xferBusy = iota
	xferExport
	xferRead
	xferApply
)

// pendingImport 是已读出预览、等用户确认的导入（渲染层 PendingImport）。
type pendingImport struct {
	filePath string
	preview  services.ConfigPreview
}

// ConfigTransferSection 是「配置迁移」一块（渲染层 ConfigTransferSection + use-config-transfer）：导出与导入两行，各开一个对话框。
//
// 导入分两步（读取预览、确认应用）是有意的：它会覆盖本机全部设置，用户得先看清文件里有什么。
// 所有密码都只留在文本框里：作为参数交给服务，不进日志、提示与错误文案；对话框关闭、导出或导入完成后清空。
type ConfigTransferSection struct {
	Look    *kit.Look
	Guard   kit.Guard
	Service services.ConfigTransfer
	Runtime services.Data
	Wake    func()

	busy xferBusy

	exp exportForm
	imp importForm

	w worker
}

// exportForm 是导出对话框的状态。
type exportForm struct {
	open                   bool
	modal                  kit.ModalState
	secrets                bool
	layout, shortcuts, mcp bool
	password, confirm      ui.Field
	showPassword, showConf bool
}

// importForm 是导入对话框的状态：pending 非空即对话框打开。
type importForm struct {
	modal         kit.ModalState
	pending       *pendingImport
	password      ui.Field
	showPassword  bool
	passwordError string
	sel           services.ConfigApplySelection
}

// Close 在分区被切走时调用：丢弃还没送达的结果，清空密码文本框。
func (s *ConfigTransferSection) Close() {
	s.w.close()
	for _, f := range []*ui.Field{&s.exp.password, &s.exp.confirm, &s.imp.password} {
		f.SetText("")
	}
}

// Page 声明「配置迁移」一块：两行说明加一枚按钮。
func (s *ConfigTransferSection) Page(c ui.C) {
	l := s.Look
	s.w.drain()
	idle := s.busy == xferIdle
	secondary := func() kit.ButtonStyle {
		return outlineBtn(l, shape(l, 2.25, 3, 1.5, kit.TextSM, kit.WeightMedium), l.T.Muted.WithAlpha(0.6))
	}
	l.SettingSection(c, locales.SettingsConfigTransferSection(), []kit.SettingRow{
		{
			Label: locales.SettingsConfigExport(), Description: locales.SettingsConfigExportDesc(),
			Control: func() {
				spec := kit.ButtonSpec{Label: locales.SettingsConfigExportStart(), Icon: icons.PackageOpen, Disabled: !idle}
				if btn(l, c.Key("export"), spec, secondary(), s.busy == xferExport) {
					s.openExport()
				}
			},
		},
		{
			Label: locales.SettingsConfigImport(), Description: locales.SettingsConfigImportDesc(),
			Control: func() {
				spec := kit.ButtonSpec{Label: locales.SettingsConfigImportStart(), Icon: icons.FolderInput, Disabled: !idle}
				if btn(l, c.Key("import"), spec, secondary(), s.busy == xferRead || s.busy == xferApply) {
					s.pickFile()
				}
			},
		},
	})
}

// Dialogs 声明导出与导入两个对话框（浮层，不占版面）。
func (s *ConfigTransferSection) Dialogs(c ui.C) {
	s.exportDialog(c.Key("export-dialog"))
	s.importDialog(c.Key("import-dialog"))
}

// ── 导出 ──

// openExport 打开导出对话框并复位表单：默认带上 API Key 与全部偏好，密码清空。
func (s *ConfigTransferSection) openExport() {
	e := &s.exp
	e.open, e.secrets, e.layout, e.shortcuts, e.mcp = true, true, true, true, true
	e.password.SetText("")
	e.confirm.SetText("")
	e.showPassword, e.showConf = false, false
}

func (s *ConfigTransferSection) closeExport() {
	s.exp.open = false
	s.exp.password.SetText("")
	s.exp.confirm.SetText("")
}

// exportConfig 选位置并写文件：用户取消时对话框保留；成功后提示、定位到文件并关闭对话框；失败提示原因，对话框保留可重试。
func (s *ConfigTransferSection) exportConfig() {
	if s.busy != xferIdle {
		return
	}
	e := &s.exp
	opts := services.ConfigExportOptions{
		IncludeSecrets: e.secrets, Password: e.password.Text(),
		IncludeUILayout: e.layout, IncludeShortcuts: e.shortcuts, IncludeMCPScope: e.mcp,
	}
	s.busy = xferExport
	svc, rt, g := s.Service, s.Runtime, s.Guard
	s.w.act(s.Wake, func() func() {
		res := services.ConfigExportResult{}
		err := services.ErrConfigNotWired
		if svc != nil {
			res, err = svc.Export(context.Background(), opts)
		}
		switch {
		case err != nil:
			g.Fail(locales.SettingsConfigExportFailed(), locales.SettingsConfigExport(), err)
			return func() { s.busy = xferIdle }
		case res.Canceled:
			return func() { s.busy = xferIdle }
		}
		g.Succeed(locales.SettingsConfigExportDone())
		if rt != nil && res.FilePath != "" {
			// 给文件管理器定位只是顺手：导出已经成功，失败只记日志。
			g.Background(locales.SettingsConfigExport(), rt.OpenPath(context.Background(), res.FilePath))
		}
		return func() { s.busy = xferIdle; s.closeExport() }
	})
}

// ── 导入 ──

// pickFile 选配置文件并读出预览：取消什么都不做；读不出来提示原因。
func (s *ConfigTransferSection) pickFile() {
	if s.busy != xferIdle {
		return
	}
	s.busy = xferRead
	s.imp.passwordError = ""
	svc, g := s.Service, s.Guard
	s.w.act(s.Wake, func() func() {
		var res services.ConfigRead
		err := services.ErrConfigNotWired
		if svc != nil {
			res, err = svc.Read(context.Background())
		}
		switch {
		case err != nil:
			g.Fail(locales.SettingsConfigImportUnreadable(), locales.SettingsConfigImport(), err)
			return func() { s.busy = xferIdle }
		case res.Canceled:
			return func() { s.busy = xferIdle }
		}
		return func() {
			s.busy = xferIdle
			s.openImport(&pendingImport{filePath: res.FilePath, preview: res.Preview})
		}
	})
}

// openImport 打开导入对话框并复位表单：默认应用设置、界面布局、快捷键与 MCP 范围，密码清空。
func (s *ConfigTransferSection) openImport(p *pendingImport) {
	i := &s.imp
	i.pending, i.passwordError, i.showPassword = p, "", false
	i.sel = services.ConfigApplySelection{Settings: true, UILayout: true, Shortcuts: true, MCPScope: true}
	i.password.SetText("")
}

func (s *ConfigTransferSection) cancelImport() {
	s.imp.pending, s.imp.passwordError = nil, ""
	s.imp.password.SetText("")
}

// applyImport 应用导入。密码不对：留在对话框里就地提示；其余失败：提示原因；成功：提示、关闭对话框，
// 留一会儿让提示看得见再重启（期间 busy 不放，避免再点一次）。
func (s *ConfigTransferSection) applyImport() {
	i := &s.imp
	if s.busy != xferIdle || i.pending == nil {
		return
	}
	filePath, password, sel := i.pending.filePath, i.password.Text(), i.sel
	s.busy = xferApply
	i.passwordError = ""
	svc, rt, g := s.Service, s.Runtime, s.Guard
	s.w.act(s.Wake, func() func() {
		var res services.ConfigApplyResult
		err := services.ErrConfigNotWired
		if svc != nil {
			res, err = svc.Apply(context.Background(), filePath, password, sel)
		}
		if err != nil {
			var wrong *services.ConfigWrongPasswordError
			if asWrongPassword(err, &wrong) {
				msg := wrong.Message
				if msg == "" {
					msg = locales.SettingsConfigImportWrongPassword()
				}
				return func() { s.busy = xferIdle; i.passwordError = msg }
			}
			g.Fail(locales.SettingsConfigImportFailed(), locales.SettingsConfigImport(), err)
			return func() { s.busy = xferIdle }
		}
		detail := ""
		for n, w := range res.Warnings {
			if n > 0 {
				detail += "\n"
			}
			detail += w
		}
		succeedWithDetail(g, locales.SettingsConfigImportDone(), detail)
		if rt != nil {
			time.AfterFunc(relaunchDelay, func() { g.Background(locales.SettingsConfigImportDone(), rt.Relaunch(context.Background())) })
		}
		return func() { s.cancelImport() } // busy 保持到重启
	})
}
