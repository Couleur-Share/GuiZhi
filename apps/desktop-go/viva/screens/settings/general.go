// Package settings 是设置页的 Viva 界面。每个分区一个文件，对应渲染层 components/settings 里的同名组件。
//
// Package settings holds the Viva settings screens, one file per section of the renderer's settings page.
package settings

import (
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// Store 是设置页对设置存取的依赖：读一份快照，原子地改。Go 壳里由 package main 的适配器实现，
// 测试与校准用内存实现。
type Store interface {
	// Values 返回当前设置的快照。
	Values() settings.Values
	// Update 应用一次修改；返回错误时设置保持不变，界面据此提示（runGuardedMutation 的语义）。
	Update(mutate func(*settings.Values)) error
}

// General 是「应用设置」页（渲染层 GeneralSettings）。
type General struct {
	Look  *kit.Look
	Store Store
	// Guard 接住写入失败：弹出带原因的错误提示并记日志（B-002）；零值时失败只会静默回到原值。
	Guard kit.Guard

	closeSel    kit.SelectState // 关闭行为下拉
	languageSel kit.SelectState // 语言下拉

	backgroundAsk     bool             // 「后台任务」确认对话框是否打开（渲染层的 confirmBackground）
	backgroundConfirm kit.ConfirmState // 该对话框的界面状态
}

// Page 声明整页：标题加五个分区，再加「后台任务」确认对话框（浮层，不占版面）。
func (g *General) Page(c ui.C) {
	l := g.Look
	l.SettingsContent(c, locales.SettingsGeneral(), func() {
		l.SettingSections(c, func() {
			g.startup(c)
			g.editor(c)
			g.wiki(c)
			g.language(c)
			g.notifications(c)
		})
	})
	g.backgroundDialog(c)
}

// save 把一次修改交给 Store，返回是否写入成功（runGuardedMutation 的语义）：
// 失败时设置保持原值、界面随之回到原状，同时弹出带原因的错误提示并记日志，用户再点一次即是重试。
func (g *General) save(mutate func(*settings.Values)) bool {
	return save(g.Guard, g.Store, mutate)
}

// save 是各设置页共用的写入入口（runGuardedMutation 的对应物）：失败时设置保持原值并弹出带原因的错误提示。
func save(guard kit.Guard, store Store, mutate func(*settings.Values)) bool {
	return guard.Run(locales.NativeSettingsSaveAction(), func() error { return store.Update(mutate) })
}

// toggle 是绑定到设置布尔字段的开关：点击后把新值交给 Store；写入失败时设置不变，界面保持原状并提示。
func (g *General) toggle(c ui.C, label string, get func(settings.Values) bool, set func(*settings.Values, bool)) func() {
	return func() {
		on := get(g.Store.Values())
		sw := g.Look.Switch(c, &on, label)
		if sw.Clicked() {
			next := on
			g.save(func(v *settings.Values) { set(v, next) })
		}
	}
}

// backgroundTasks 是「后台任务」开关。关→开先弹确认（它会同时改开机启动、隐藏启动与关闭行为），开→关直接写入；
// 开关本身由设置值决定：点开之后在确认之前它仍然是关的（渲染层的受控开关）。
func (g *General) backgroundTasks(c ui.C) func() {
	label := locales.SettingsBackgroundTasks()
	return func() {
		on := g.Store.Values().App.BackgroundTasksEnabled
		sw := g.Look.Switch(c, &on, label)
		if !sw.Clicked() {
			return
		}
		if on {
			g.backgroundAsk = true
			return
		}
		g.save(func(v *settings.Values) { v.App.BackgroundTasksEnabled = false })
	}
}

// backgroundDialog 是「启用系统后台运行」确认对话框。确认后一次写入四项：后台任务、开机启动、隐藏启动、关闭按钮最小化到托盘
// （渲染层 setBackgroundTasksEnabled(true) 的产品承诺：之后用户仍可分别修改这些偏好）。
func (g *General) backgroundDialog(c ui.C) {
	switch g.Look.ConfirmDialog(c, &g.backgroundConfirm, kit.ConfirmSpec{
		Open:        g.backgroundAsk,
		Title:       locales.SettingsBackgroundTasksConfirmTitle(),
		Message:     locales.SettingsBackgroundTasksConfirmMessage(),
		ConfirmText: locales.CommonEnable(),
		CancelText:  locales.CommonCancel(),
	}) {
	case kit.ConfirmAccepted:
		g.backgroundAsk = false
		// 写入失败时对话框照常关闭，开关仍是关的（值取自设置），错误提示说明原因，用户可以再打开一次重试
		g.save(func(v *settings.Values) {
			v.App.BackgroundTasksEnabled = true
			v.App.LaunchAtStartup = true
			v.App.MinimizeOnLaunch = true
			v.UI.CloseAction = settings.CloseMinimize
		})
	case kit.ConfirmDismissed:
		g.backgroundAsk = false
	}
}

func (g *General) startup(c ui.C) {
	rows := []kit.SettingRow{
		{
			Label: locales.SettingsBackgroundTasks(), Description: locales.SettingsBackgroundTasksDesc(),
			Control: g.backgroundTasks(c),
		},
		{
			Label: locales.SettingsLaunchAtStartup(), Description: locales.SettingsLaunchAtStartupDesc(),
			Control: g.toggle(c, locales.SettingsLaunchAtStartup(),
				func(v settings.Values) bool { return v.App.LaunchAtStartup },
				func(v *settings.Values, on bool) { v.App.LaunchAtStartup = on }),
		},
		{
			Label: locales.SettingsMinimizeOnLaunch(), Description: locales.SettingsMinimizeOnLaunchDesc(),
			Control: g.toggle(c, locales.SettingsMinimizeOnLaunch(),
				func(v settings.Values) bool { return v.App.MinimizeOnLaunch },
				func(v *settings.Values, on bool) { v.App.MinimizeOnLaunch = on }),
		},
		{
			// 渲染层仅在 Windows 上显示；归知只面向 Windows，所以恒显示。
			Label: locales.SettingsCloseAction(), Description: locales.SettingsCloseActionDesc(),
			Control: func() {
				picked, ok := g.Look.Select(c, &g.closeSel, kit.SelectSpec{
					Label: locales.SettingsCloseAction(), Value: string(g.Store.Values().UI.CloseAction),
					Options: closeActionOptions(), Width: g.Look.Rem(10), // w-40
				})
				if ok {
					g.save(func(v *settings.Values) { v.UI.CloseAction = settings.CloseAction(picked) })
				}
			},
		},
	}
	g.Look.SettingSection(c, locales.SettingsStartup(), rows)
}

// closeActionOptions 是关闭行为的选项（渲染层 GeneralSettings 里的 ask / minimize / exit）。
func closeActionOptions() []kit.SelectOption {
	return []kit.SelectOption{
		{Value: string(settings.CloseAsk), Label: locales.SettingsAskEveryTime()},
		{Value: string(settings.CloseMinimize), Label: locales.SettingsCloseToTray()},
		{Value: string(settings.CloseExit), Label: locales.SettingsCloseApp()},
	}
}

func (g *General) editor(c ui.C) {
	g.Look.SettingSection(c, locales.SettingsEditor(), []kit.SettingRow{
		{
			Label: locales.SettingsAutoSave(), Description: locales.SettingsAutoSaveDesc(),
			Control: g.toggle(c, locales.SettingsAutoSave(),
				func(v settings.Values) bool { return v.UI.AutoSave },
				func(v *settings.Values, on bool) { v.UI.AutoSave = on }),
		},
		{
			Label: locales.SettingsShowLineNumbers(), Description: locales.SettingsShowLineNumbersDesc(),
			Control: g.toggle(c, locales.SettingsShowLineNumbers(),
				func(v settings.Values) bool { return v.UI.ShowLineNumbers },
				func(v *settings.Values, on bool) { v.UI.ShowLineNumbers = on }),
		},
	})
}

func (g *General) wiki(c ui.C) {
	g.Look.SettingSection(c, locales.SettingsWikiSection(), []kit.SettingRow{
		{
			Label: locales.SettingsWikiAutoCompile(), Description: locales.SettingsWikiAutoCompileDesc(),
			Control: g.toggle(c, locales.SettingsWikiAutoCompile(),
				func(v settings.Values) bool { return v.UI.WikiCompileEnabled },
				func(v *settings.Values, on bool) { v.UI.WikiCompileEnabled = on }),
		},
	})
}

func (g *General) language(c ui.C) {
	g.Look.SettingSection(c, locales.SettingsLanguageAndRegion(), []kit.SettingRow{
		{
			Label: locales.SettingsLanguage(), Description: locales.SettingsSelectLanguage(),
			Control: func() {
				picked, ok := g.Look.Select(c, &g.languageSel, kit.SelectSpec{
					Label: locales.SettingsLanguage(), Value: string(g.Store.Values().App.Language),
					Options: languageOptions(), Width: g.Look.Rem(10), // w-40
				})
				if ok {
					g.save(func(v *settings.Values) { v.App.Language = settings.Language(picked) })
				}
			},
		},
	})
}

// languageOptions 是语言的选项：语言名不翻译，两种界面语言下都是各自的本名。
func languageOptions() []kit.SelectOption {
	return []kit.SelectOption{
		{Value: string(settings.LangZh), Label: locales.NativeLanguageNameZh()},
		{Value: string(settings.LangEn), Label: locales.NativeLanguageNameEn()},
	}
}

func (g *General) notifications(c ui.C) {
	g.Look.SettingSection(c, locales.SettingsNotifications(), []kit.SettingRow{
		{
			Label: locales.SettingsEnableNotifications(), Description: locales.SettingsEnableNotificationsDesc(),
			Control: g.toggle(c, locales.SettingsEnableNotifications(),
				func(v settings.Values) bool { return v.App.EnableNotifications },
				func(v *settings.Values, on bool) { v.App.EnableNotifications = on }),
		},
	})
}
