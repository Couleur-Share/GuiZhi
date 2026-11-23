package scenes

import (
	"strings"
	"testing"

	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/parity"
	themedscreen "guizhi-mygo-shell/viva/screens/themed"
)

// 守住 themed.go 里的英文标题登记：写死的英文 Subject 与英文目录画出的字一致；每条只在英文变体所在字号档、只指向 A-013、
// 只放过 L2 的 text-width / ink，每条有上限；中文界面里的字一条都不命中。语言是进程内全局状态：测试不并行，结束时还原。

func TestEnglishThemedTitleSubjectsMatchCatalog(t *testing.T) {
	locales.SetLang(locales.En)
	t.Cleanup(func() { locales.SetLang(locales.Zh) })
	fix := themedFixture()
	cases := []struct{ name, got, want string }{
		{"生成设置标题", enThemedSetupTitle, locales.ThemedReadingReaderCreate()},
		{"资料面板标题", enThemedRefsTitle, locales.ThemedReadingReaderReferences()},
		{"进行中任务的标题", enThemedRunTitle, themedRunningTitle(fix.Running)},
		{"失败任务的标题", enThemedFailedTitle, themedscreen.StateLabel(fix.Failed.State)},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s：登记写的是 %q，英文目录画出来的是 %q", c.name, c.got, c.want)
		}
	}
}

func TestEnglishThemedTitleWaiversAreBounded(t *testing.T) {
	for _, modal := range []bool{true, false} {
		for _, w := range englishTitleWaivers(modal, "Any title here") {
			if w.Variant != englishTier || w.Ledger != "A-013" || w.Layer != parity.LayerL2 || w.MaxDelta <= 0 {
				t.Errorf("登记 %s %s/%s %q 的范围不对：%+v", w.Ledger, w.Layer, w.Kind, w.Subject, w)
			}
			if w.Kind != "text-width" && w.Kind != "ink" {
				t.Errorf("登记 %q 的类别 %q 超出 tracking-tight 延伸的范围", w.Subject, w.Kind)
			}
		}
	}
	// 中文界面画出的字不含任何一条英文登记的 Subject。
	for _, subject := range []string{enThemedSetupTitle, enThemedRefsTitle, enThemedRunTitle, enThemedFailedTitle} {
		for _, zh := range []string{
			locales.ThemedReadingReaderCreate(), locales.ThemedReadingReaderReferences(),
			themedRunningTitle(themedFixture().Running), themedscreen.StateLabel(themedFixture().Failed.State),
		} {
			if strings.Contains(zh, subject) {
				t.Errorf("中文界面的 %q 不该含英文登记 %q", zh, subject)
			}
		}
	}
}
