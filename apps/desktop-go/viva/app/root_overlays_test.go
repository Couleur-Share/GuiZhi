package app

import (
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/locales"
	settingsscreen "guizhi-mygo-shell/viva/screens/settings"
	"guizhi-mygo-shell/viva/screens/shell"
)

// 全局浮层接进 Root 的接线（root_overlays.go，台账 B-008 / B-016 / B-026）：命令面板、更新对话框、设置清单、
// 旧库迁移与窗口内快捷键。各浮层自己的行为在 viva/screens/overlays 里测，这里只测接线：入口、跳转与联动。

func (f *rootFixture) paletteOpen() bool {
	_, ok := f.session.AccessByRole(gfx.RoleGroup, locales.CommandPaletteTitle())
	return ok
}

func (f *rootFixture) updateDialogOpen() bool {
	_, ok := f.session.AccessByRole(gfx.RoleGroup, locales.SettingsCheckUpdate())
	return ok
}

func (f *rootFixture) setupOpen() bool {
	_, ok := f.session.AccessByRole(gfx.RoleGroup, locales.SetupTitle())
	return ok
}

func (f *rootFixture) migrationOpen() bool {
	_, ok := f.session.AccessByRole(gfx.RoleGroup, locales.MigrationTitle())
	return ok
}

// press 按一次组合键并出帧。
func (f *rootFixture) press(t *testing.T, spec string) {
	t.Helper()
	if err := f.session.Keys(spec); err != nil {
		t.Fatal(err)
	}
	f.frames(t, 4)
}

// pressNoop 按一次预期不触发任何动作的组合键：没有任何处理者时 headless 的 Alt 组合会报「需要终端」，这里视为正常。
func (f *rootFixture) pressNoop(t *testing.T, spec string) {
	t.Helper()
	if err := f.session.Keys(spec); err != nil && !strings.Contains(err.Error(), "needs a focused terminal") {
		t.Fatal(err)
	}
	f.frames(t, 4)
}

// withSearchChip 让顶栏显示搜索入口上的快捷键提示（它是命令面板的另一个点击入口）。
func withSearchChip(o *Options) { o.SearchShortcut = "Alt+Shift+F" }

// B-008：顶栏搜索入口打开命令面板，Esc 关闭；知识库与采集模块的顶栏都一样。
func TestRootTopBarSearchOpensThePalette(t *testing.T) {
	for _, module := range []shell.Module{shell.ModuleLibrary, shell.ModuleImports} {
		f, _, _ := openCaptureRoot(t, module, withSearchChip)
		if f.paletteOpen() {
			t.Fatal("没点之前不该有面板")
		}
		f.clickRole(t, gfx.RoleButton, locales.HeaderOpenCommandPalette())
		if !f.paletteOpen() {
			t.Fatalf("点搜索入口应打开命令面板；画面上有 %q", f.session.Texts())
		}
		if err := f.session.Keys("escape"); err != nil {
			t.Fatal(err)
		}
		f.frames(t, 3)
		if f.paletteOpen() {
			t.Fatal("Esc 应关闭面板")
		}
	}
}

// B-008：命令面板搜索结果里的知识条目，点击后切到知识库并选中它。
func TestRootPaletteResultOpensTheItemInTheLibrary(t *testing.T) {
	f, _, _ := openCaptureRoot(t, shell.ModuleImports, withSearchChip)
	f.clickRole(t, gfx.RoleButton, locales.HeaderOpenCommandPalette())
	if err := f.session.Type("一条"); err != nil {
		t.Fatal(err)
	}
	f.waitFrames(t, "搜到条目", func() bool {
		_, ok := f.session.AccessByRole(gfx.RoleButton, "一条笔记")
		return ok
	})
	f.clickRole(t, gfx.RoleButton, "一条笔记")
	store := f.root.library.Store
	f.waitFrames(t, "知识库里选中条目", func() bool {
		d := store.View().Detail
		return f.root.Window().Module == shell.ModuleLibrary && d.Item != nil && d.Item.ID == "n1"
	})
	if f.paletteOpen() {
		t.Error("执行后应关闭面板")
	}
}

// B-008：命令面板的静态动作联动各模块：打开处理中心、设置分区、快速采集。
func TestRootPaletteActionsDriveModulesSettingsAndCapture(t *testing.T) {
	f, _, _ := openCaptureRoot(t, shell.ModuleLibrary, withSearchChip)

	f.root.overlay.palette.Open()
	f.frames(t, 3)
	f.clickRole(t, gfx.RoleButton, locales.CaptureTitle())
	if !f.captureOpen() || f.paletteOpen() {
		t.Fatalf("「快速采集」应关闭面板并打开采集对话框：capture=%v palette=%v", f.captureOpen(), f.paletteOpen())
	}
	if err := f.session.Keys("escape"); err != nil {
		t.Fatal(err)
	}
	f.frames(t, 3)

	f.root.overlay.palette.Open()
	f.frames(t, 3)
	f.clickRole(t, gfx.RoleButton, locales.CommandPaletteSettingsAi())
	if f.root.Window().Page != shell.PageSettings || f.root.settings.Active != settingsscreen.SectionAI {
		t.Fatalf("「AI 模型服务」应切到设置页的 AI 分区：page=%v section=%v", f.root.Window().Page, f.root.settings.Active)
	}
}

// B-008：没有处理中心服务的实例，「立即建立语义索引」如实提示，不假装成功。
func TestRootPaletteBuildSemanticIndexWithoutInboxReportsFailure(t *testing.T) {
	f, _, _ := openCaptureRoot(t, shell.ModuleLibrary)
	f.root.buildSemanticIndex()
	if got := f.root.notifier.Queued(); len(got) == 0 {
		t.Fatal("没有处理中心服务时应给出失败提示")
	}
}

// B-008：更新按钮跟着更新服务的状态出现，点它打开更新对话框并检查；托盘「检查更新」命令也打开同一个对话框。
func TestRootUpdatePillFollowsUpdaterAndOpensTheDialog(t *testing.T) {
	updater := &servicestest.Updater{}
	f, _, _ := openCaptureRoot(t, shell.ModuleLibrary, func(o *Options) { o.Services.Updater = updater })
	f.waitFrames(t, "订阅了更新推送", func() bool { return updater.Subscribers() > 0 })
	if _, ok := f.session.AccessByRole(gfx.RoleButton, locales.SettingsUpdateAvailable()); ok {
		t.Fatal("没有新版本时不该有更新按钮")
	}
	updater.Emit(services.UpdateStatus{State: services.UpdateStateAvailable, Version: "9.9.9"})
	f.waitFrames(t, "更新按钮出现", func() bool {
		_, ok := f.session.AccessByRole(gfx.RoleButton, locales.SettingsUpdateAvailable())
		return ok
	})
	if !f.session.HasText(locales.SettingsNewVersion("9.9.9")) {
		t.Errorf("更新按钮应带版本号；画面上有 %q", f.session.Texts())
	}
	f.clickRole(t, gfx.RoleButton, locales.SettingsUpdateAvailable())
	if !f.updateDialogOpen() {
		t.Fatalf("点更新按钮应打开更新对话框；画面上有 %q", f.session.Texts())
	}
	waitUntil(t, "打开时检查了一次", func() bool { return len(updater.Checks()) > 0 })
	f.clickRole(t, gfx.RoleButton, locales.CommonClose())
	if f.updateDialogOpen() {
		t.Fatal("关闭钮应关闭对话框")
	}

	f.root.dispatch(CommandOpenUpdater)
	f.frames(t, 4)
	if !f.updateDialogOpen() {
		t.Fatal("托盘的检查更新命令应打开更新对话框")
	}
}

// 没有外壳自动流程时（截图、对照、无头测试：AutoOverlays 关），启动不弹任何对话框，也不探测旧库、不检查更新。
func TestRootStartsWithoutAutoPopupsByDefault(t *testing.T) {
	updater, migrate, ai := &servicestest.Updater{}, &servicestest.Migration{}, &servicestest.AISettingsFake{}
	migrate.SetDetection(services.LegacyDetection{Path: `C:\legacy\knowledge.db`, ItemCount: 3}, true)
	f, _, _ := openCaptureRoot(t, shell.ModuleLibrary, func(o *Options) {
		o.Services.Updater, o.Services.Migration, o.Services.AI = updater, migrate, ai
	})
	f.frames(t, 20)
	time.Sleep(50 * time.Millisecond)
	f.frames(t, 4)
	if f.migrationOpen() || f.setupOpen() || f.updateDialogOpen() {
		t.Fatalf("默认不应自动弹窗：迁移=%v 清单=%v 更新=%v", f.migrationOpen(), f.setupOpen(), f.updateDialogOpen())
	}
	if migrate.Detects() != 0 || len(updater.Checks()) != 0 || ai.Loads() != 0 {
		t.Errorf("默认不应启动自动流程：探测=%d 检查=%d 读AI=%d", migrate.Detects(), len(updater.Checks()), ai.Loads())
	}
}

// 旧库迁移与首次使用清单的先后：真实外壳实例启动时先探测旧库、弹迁移确认；清单被推迟，确认页关闭后才补上。
func TestRootLegacyMigrationConfirmUnblocksTheFirstRunSetup(t *testing.T) {
	migrate, ai := &servicestest.Migration{}, &servicestest.AISettingsFake{}
	migrate.SetDetection(services.LegacyDetection{Path: `C:\legacy\knowledge.db`, ItemCount: 3}, true)
	f, _, _ := openCaptureRoot(t, shell.ModuleLibrary, func(o *Options) {
		o.AutoOverlays = true
		o.Services.Migration, o.Services.AI = migrate, ai
	})
	f.waitFrames(t, "弹出迁移确认", f.migrationOpen)
	waitUntil(t, "清单评估过了", func() bool { return ai.Loads() > 0 })
	f.frames(t, 6)
	if f.setupOpen() {
		t.Fatal("迁移确认开着时，设置清单应被推迟")
	}
	f.clickRole(t, gfx.RoleButton, locales.MigrationSkip())
	f.waitFrames(t, "确认页关闭后补上设置清单", f.setupOpen)
	if f.migrationOpen() {
		t.Error("跳过后迁移确认应关闭")
	}
}

// 没有旧库时，真实外壳实例启动直接评估设置清单：没配置文本模型就弹出，稍后再说关闭。
func TestRootFirstRunSetupOpensWithoutLegacyDatabase(t *testing.T) {
	migrate, ai := &servicestest.Migration{}, &servicestest.AISettingsFake{}
	f, _, _ := openCaptureRoot(t, shell.ModuleLibrary, func(o *Options) {
		o.AutoOverlays = true
		o.Services.Migration, o.Services.AI = migrate, ai
	})
	f.waitFrames(t, "弹出设置清单", f.setupOpen)
	if f.migrationOpen() {
		t.Error("没有旧库时不该弹迁移确认")
	}
	f.clickRole(t, gfx.RoleButton, locales.CommonClose())
	if f.setupOpen() {
		t.Fatal("关闭钮应关闭清单")
	}
}

// 设置页的「设置引导」入口随时重新打开清单。
func TestRootSettingsSetupGuideOpensTheChecklist(t *testing.T) {
	f, _, _ := openCaptureRoot(t, shell.ModuleLibrary, func(o *Options) {
		o.Services.AI = &servicestest.AISettingsFake{}
	})
	f.root.settings.OpenSetupGuide()
	f.waitFrames(t, "打开设置清单", f.setupOpen)
}

// B-026：默认的局部快捷键——新建条目开采集对话框、搜索开命令面板、设置切到设置页。
func TestRootLocalShortcutsTriggerTheirActions(t *testing.T) {
	f, _, _ := openCaptureRoot(t, shell.ModuleLibrary)

	f.press(t, "alt+shift+n")
	if !f.captureOpen() {
		t.Fatalf("Alt+Shift+N 应打开快速采集；画面上有 %q", f.session.Texts())
	}
	f.press(t, "escape")

	f.press(t, "alt+shift+f")
	if !f.paletteOpen() {
		t.Fatalf("Alt+Shift+F 应打开命令面板；画面上有 %q", f.session.Texts())
	}
	f.press(t, "escape")
	if f.paletteOpen() {
		t.Fatal("Esc 应关闭面板")
	}

	f.press(t, "alt+shift+s")
	if f.root.Window().Page != shell.PageSettings {
		t.Fatalf("Alt+Shift+S 应切到设置页：%v", f.root.Window().Page)
	}
}

// B-026：全局模式的动作不在窗口内重复触发（系统已经注册它）；换了组合键之后旧组合不再触发、新组合触发。
func TestRootLocalShortcutsFollowSavedConfigAndModes(t *testing.T) {
	svc := &servicestest.Shortcuts{}
	cfg := services.ShortcutConfig{Accelerators: services.DefaultShortcuts(), Modes: services.DefaultShortcutModes()}
	cfg.Accelerators[services.ShortcutSearch] = "Alt+Shift+K"
	cfg.Modes[services.ShortcutNewItem] = services.ShortcutModeGlobal
	svc.Set(cfg)
	f, _, _ := openCaptureRoot(t, shell.ModuleLibrary, func(o *Options) { o.Services.Shortcuts = svc })
	waitUntil(t, "读到已保存的快捷键", func() bool { return f.root.overlay.shortcuts.Accelerator(services.ShortcutSearch) == "Alt+Shift+K" })

	f.pressNoop(t, "alt+shift+f")
	if f.paletteOpen() {
		t.Fatal("搜索改成 Alt+Shift+K 之后，旧组合不该再触发")
	}
	f.pressNoop(t, "alt+shift+n")
	if f.captureOpen() {
		t.Fatal("全局模式的新建不该在窗口内再触发一次")
	}
	f.press(t, "alt+shift+k")
	if !f.paletteOpen() {
		t.Fatalf("新组合 Alt+Shift+K 应打开命令面板；画面上有 %q", f.session.Texts())
	}
}

// B-026：全局快捷键到达（shortcut:triggered → 命令）时，搜索打开命令面板；新建仍是采集对话框。
func TestRootGlobalSearchShortcutOpensThePalette(t *testing.T) {
	f, _, _ := openCaptureRoot(t, shell.ModuleLibrary)
	f.root.dispatch(CommandFocusSearch)
	f.frames(t, 4)
	if !f.paletteOpen() {
		t.Fatal("全局搜索快捷键应打开命令面板")
	}
}

// B-026：焦点在录制框时局部快捷键不抢——录制框要收下正好等于已有快捷键的组合。
func TestRootLocalShortcutsYieldToTheRecorder(t *testing.T) {
	f, _, _ := openCaptureRoot(t, shell.ModuleLibrary, func(o *Options) {
		o.Page = shell.PageSettings
		o.Services.Shortcuts = &servicestest.Shortcuts{}
	})
	f.clickRole(t, gfx.RoleButton, locales.SettingsShortcuts())
	f.clickRole(t, gfx.RoleGroup, locales.SettingsShortcutInputLabel(locales.SettingsShortcutNewItem()))
	if !f.root.settings.ShortcutRecording() || !f.session.HasText(locales.SettingsPressKeys()) {
		t.Fatalf("点录制框应进入录制；画面上有 %q", f.session.Texts())
	}
	f.press(t, "alt+shift+n") // 正好是新建条目的现有组合
	if f.captureOpen() {
		t.Fatal("录制中按下已有的快捷键不该触发动作")
	}

	// 没在录制时同一个组合照常触发。
	if err := f.session.Keys("escape"); err != nil {
		t.Fatal(err)
	}
	f.frames(t, 3)
	if f.root.settings.ShortcutRecording() {
		t.Fatal("Esc 应取消录制")
	}
	f.press(t, "alt+shift+n")
	if !f.captureOpen() {
		t.Fatal("不在录制时应触发新建")
	}
}

// B-026：焦点在文字输入框时，不带 Ctrl / Alt 的组合是在打字，放行给输入框；没有焦点时同一个组合照常触发。
func TestRootLocalShortcutsLetTextInputsTypeFreely(t *testing.T) {
	svc := &servicestest.Shortcuts{}
	cfg := services.ShortcutConfig{Accelerators: services.DefaultShortcuts(), Modes: services.DefaultShortcutModes()}
	cfg.Accelerators[services.ShortcutSearch] = "Shift+K"
	svc.Set(cfg)
	f, _, _ := openCaptureRoot(t, shell.ModuleLibrary, func(o *Options) { o.Services.Shortcuts = svc })
	waitUntil(t, "读到已保存的快捷键", func() bool { return f.root.overlay.shortcuts.Accelerator(services.ShortcutSearch) == "Shift+K" })

	field, ok := f.session.AccessByRole(gfx.RoleTextField, "")
	if !ok {
		for _, n := range f.session.Access() {
			if n.Role == gfx.RoleTextField {
				field, ok = n, true
				break
			}
		}
	}
	if !ok {
		t.Fatal("知识库顶栏应有搜索输入框")
	}
	if err := f.session.Click(field.Bounds.X+field.Bounds.Width/2, field.Bounds.Y+field.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	f.frames(t, 3)
	f.press(t, "shift+k")
	if f.paletteOpen() {
		t.Fatal("在输入框里按 Shift+K 是打字，不该触发搜索快捷键")
	}

	f.press(t, "escape")
	if err := f.session.Click(640, 600); err != nil { // 点空白处，焦点离开输入框
		t.Fatal(err)
	}
	f.frames(t, 3)
	f.press(t, "shift+k")
	if !f.paletteOpen() {
		t.Fatalf("焦点不在输入框时 Shift+K 应触发搜索；画面上有 %q", f.session.Texts())
	}
}

// B-016：采集页的入口接到 Root：快速采集开对话框，「打开条目」切到知识库并选中条目；点结果行的动作按钮同样如此。
func TestRootImportsHooksJumpToCaptureAndLibrary(t *testing.T) {
	task := importTask("t1", "示例页面")
	task.ResultItemID = "n1"
	imports := &servicestest.Imports{}
	imports.Set(services.ImportPage{Total: 1, Counts: map[string]int64{"all": 1, "completed": 1}, Entries: []services.ImportTask{task}}, nil)
	f, _, _ := openCaptureRoot(t, shell.ModuleImports, func(o *Options) { o.Services.Imports = imports })
	f.waitFrames(t, "任务列表出现", func() bool { return f.session.HasText("示例页面") })

	// 「就此提问」要装配问答服务才有，它的跳转在 TestRootAskAboutItemJumpsWithDraft 里测。
	h := f.root.imports.Hooks
	if h.QuickCapture == nil || h.OpenItem == nil {
		t.Fatal("采集页的快速采集与打开条目钩子应接线")
	}
	h.QuickCapture()
	f.frames(t, 4)
	if !f.captureOpen() {
		t.Fatal("QuickCapture 应打开快速采集")
	}
	f.press(t, "escape")

	f.clickRole(t, gfx.RoleButton, locales.ImportsOpenItem())
	store := f.root.library.Store
	f.waitFrames(t, "知识库里选中条目", func() bool {
		d := store.View().Detail
		return f.root.Window().Module == shell.ModuleLibrary && d.Item != nil && d.Item.ID == "n1"
	})
}
