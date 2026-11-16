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

	closeOpen    bool // 关闭行为下拉是否展开
	languageOpen bool // 语言下拉是否展开
}

// Page 声明整页：标题加五个分区。
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
}

// toggle 是绑定到设置布尔字段的开关：点击后把新值交给 Store；写入失败时设置不变，界面保持原状。
func (g *General) toggle(c ui.C, label string, get func(settings.Values) bool, set func(*settings.Values, bool)) func() {
	return func() {
		on := get(g.Store.Values())
		sw := g.Look.Switch(c, &on, label)
		if sw.Clicked() {
			next := on
			_ = g.Store.Update(func(v *settings.Values) { set(v, next) })
		}
	}
}

func (g *General) startup(c ui.C) {
	rows := []kit.SettingRow{
		{
			Label: locales.SettingsBackgroundTasks(), Description: locales.SettingsBackgroundTasksDesc(),
			Control: g.toggle(c, locales.SettingsBackgroundTasks(),
				func(v settings.Values) bool { return v.App.BackgroundTasksEnabled },
				func(v *settings.Values, on bool) { v.App.BackgroundTasksEnabled = on }),
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
				current := g.Store.Values().UI.CloseAction
				g.Look.SelectTrigger(c, locales.SettingsCloseAction(), closeActionLabel(current), g.Look.Rem(10), g.closeOpen)
			},
		},
	}
	g.Look.SettingSection(c, locales.SettingsStartup(), rows)
}

// closeActionLabel 是关闭行为取值对应的选项文案。
func closeActionLabel(a settings.CloseAction) string {
	switch a {
	case settings.CloseMinimize:
		return locales.SettingsCloseToTray()
	case settings.CloseExit:
		return locales.SettingsCloseApp()
	default:
		return locales.SettingsAskEveryTime()
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
				g.Look.SelectTrigger(c, locales.SettingsLanguage(), languageLabel(g.Store.Values().App.Language), g.Look.Rem(10), g.languageOpen)
			},
		},
	})
}

// languageLabel 是语言取值对应的选项文案：语言名不翻译，两种界面语言下都是各自的本名。
func languageLabel(lang settings.Language) string {
	if lang == settings.LangEn {
		return locales.NativeLanguageNameEn()
	}
	return locales.NativeLanguageNameZh()
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
