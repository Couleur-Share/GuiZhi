package scenes

import (
	"strings"
	"testing"

	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/parity"
)

// 守住 overlays_waivers.go 里的登记：写死的英文标题与英文目录画出的字一致；英文登记只在英文变体所在字号档、
// 只指向既有条目（A-013 / A-014）、只放过 L2 的 text-width / ink，每条有上限；中文界面里的字一条都不命中英文登记。
// 语言是进程内全局状态：测试不并行，结束时还原。

func TestEnglishOverlayTitleSubjectsMatchCatalog(t *testing.T) {
	locales.SetLang(locales.En)
	t.Cleanup(func() { locales.SetLang(locales.Zh) })
	cases := []struct{ name, got, want string }{
		{"更新对话框标题", enUpdateTitle, locales.SettingsCheckUpdate()},
		{"设置清单标题", enSetupTitle, locales.SetupTitle()},
		{"迁移确认标题", enMigrationTitle, locales.MigrationTitle()},
		{"命令面板「采集任务」分组", enImportsGroup, locales.ImportsTitle()},
		{"更新卡片标题", enUpdateAvailable, locales.SettingsUpdateAvailable()},
		{"版本前缀", enVersionLabel, locales.SettingsVersion()},
		{"更新说明小标题", enReleaseNotesTitle, strings.ToUpper(locales.SettingsReleaseNotes())},
		{"设置清单介绍", enSetupIntro, locales.SetupIntro()},
		{"设置清单转写项", enSetupTranscribe, locales.SetupItemTranscription()},
		{"迁移「暂不迁移」", enMigrationSkip, locales.MigrationSkip()},
		{"迁移「立即迁移」", enMigrationStart, locales.MigrationStart()},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s：登记写的是 %q，英文目录画出来的是 %q", c.name, c.got, c.want)
		}
	}
}

func TestEnglishOverlayWaiversAreBounded(t *testing.T) {
	for _, w := range englishTitleWaivers(true, "Any title here") {
		if w.Variant != englishTier || w.Ledger != "A-013" || w.Layer != parity.LayerL2 || w.MaxDelta <= 0 {
			t.Errorf("登记 %s %s/%s %q 的范围不对：%+v", w.Ledger, w.Layer, w.Kind, w.Subject, w)
		}
	}
	for _, subject := range []string{enUpdateTitle, enSetupTitle, enMigrationTitle} {
		for _, zh := range []string{locales.SettingsCheckUpdate(), locales.SetupTitle(), locales.MigrationTitle()} {
			if strings.Contains(zh, subject) {
				t.Errorf("中文界面的 %q 不该含英文登记 %q", zh, subject)
			}
		}
	}
}

// 命令面板片段与更新说明的登记都是只管 L2、只指向 A-014 / A-013、有上限的窄条目。
func TestOverlayNarrowWaiversAreBounded(t *testing.T) {
	check := func(name string, ws []parity.Waiver, ledgers ...string) {
		t.Helper()
		if len(ws) == 0 {
			t.Errorf("%s：没有登记", name)
		}
		for _, w := range ws {
			ok := false
			for _, l := range ledgers {
				ok = ok || w.Ledger == l
			}
			if !ok || w.Layer != parity.LayerL2 || w.MaxDelta <= 0 || w.Subject == "" {
				t.Errorf("%s：登记 %s %s/%s %q 的范围不对：%+v", name, w.Ledger, w.Layer, w.Kind, w.Subject, w)
			}
		}
	}
	check("面板片段", paletteSnippetWaivers(), kerning400Ledger)
	check("更新说明", updateNotesWaivers(), "A-013", kerning400Ledger)
	check("冒号字形", []parity.Waiver{updateColonWaiver()}, "A-009")
}

// 英文登记只在英文变体里生效、只放过 L2、只指向既有条目、每条有正的上限；中文变体里画出的字不命中任何一条。
func TestEnglishOverlayWaiverSetsAreBounded(t *testing.T) {
	ledgers := map[string]bool{"A-009": true, "A-013": true, kerning400Ledger: true, kerning600Ledger: true}
	sets := map[string][]parity.Waiver{
		"背后页面": overlaysEnglishBackdropWaivers(), "命令面板": paletteEnglishWaivers(), "更新": updateEnglishWaivers(),
		"设置清单": setupEnglishWaivers(), "迁移确认": migrationEnglishWaivers(),
	}
	for name, ws := range sets {
		for _, w := range ws {
			if w.Variant != englishTier || w.Layer != parity.LayerL2 || !ledgers[w.Ledger] || w.MaxDelta <= 0 || w.Subject == "" {
				t.Errorf("%s：登记 %s %s/%s %q 的范围不对：%+v", name, w.Ledger, w.Layer, w.Kind, w.Subject, w)
			}
		}
	}
}
