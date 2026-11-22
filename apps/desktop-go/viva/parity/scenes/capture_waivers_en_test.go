package scenes

import (
	"testing"

	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/parity"
)

// 守住 capture_waivers_en.go：写死的英文 Subject 与英文目录画出的字一致；字偶距登记只在英文变体所在字号档、只指向 A-014、
// 只放过 L2 的 text-width / ink，每条有上限；中文界面里的同类文字一条都不命中。语言是进程内全局状态：测试不并行，结束时还原。

func TestEnglishCaptureWaiverSubjectsMatchCatalog(t *testing.T) {
	locales.SetLang(locales.En)
	t.Cleanup(func() { locales.SetLang(locales.Zh) })
	cases := []struct{ name, got, want string }{
		{"对话框标题", enCaptureTitle, locales.CaptureTitle()},
		{"设置 rail 分区名", enCaptureNav, locales.SettingsCaptureAndTranscription()},
		{"自动保存说明", enAutoSaveDesc, locales.SettingsAutoSaveDesc()},
		{"采集说明", enCaptureHint, locales.CaptureHint()},
		{"标签占位符", enCaptureTagsHint, locales.CaptureTagsPlaceholder()},
		{"草稿占位符", enCapturePlaceholder, locales.CapturePlaceholder()},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s：登记写的是 %q，英文目录画出来的是 %q", c.name, c.got, c.want)
		}
	}
}

func TestEnglishCaptureWaiversAreBounded(t *testing.T) {
	for _, w := range captureEnglishWaivers() {
		if w.Variant != englishTier || w.Ledger != kerning400Ledger || w.Layer != parity.LayerL2 || w.MaxDelta <= 0 {
			t.Errorf("登记 %s %s/%s %q 的范围不对：%+v", w.Ledger, w.Layer, w.Kind, w.Subject, w)
		}
		if w.Kind != "text-width" && w.Kind != "ink" {
			t.Errorf("登记 %q 的类别 %q 超出字偶距延伸的范围", w.Subject, w.Kind)
		}
	}
	// 中文界面画出的字不含任何一条登记的 Subject。
	zh := []string{locales.CaptureHint(), locales.CaptureTagsPlaceholder(), locales.CapturePlaceholder(), locales.SettingsCaptureAndTranscription(), locales.SettingsAutoSaveDesc()}
	for _, w := range captureEnglishWaivers() {
		for _, text := range zh {
			if w.Subject == "「"+text+"」" {
				t.Errorf("登记 %q 命中了中文界面的文字", w.Subject)
			}
		}
	}
}
