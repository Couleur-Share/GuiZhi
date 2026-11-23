package scenes

import (
	"slices"

	"guizhi-mygo-shell/viva/parity"
)

// 全局浮层（M3-6a）在英文变体里的登记。口径同 capture_waivers_en.go / wiki_waivers_en.go：全部带 Variant: englishTier，
// 写死的英文 Subject 与 locales 目录里英文界面画出的字一致（overlays_waivers_test.go 逐一核对，Waivers 在 init 时构造，那时界面语言还是中文，取不到英文），
// 中文变体不会命中。没有新增登记类别：只是 A-009 / A-013 / A-014 / A-026 的英文 Subject 延伸，同样是待追认的新登记（台账 C-010）——
// 删掉对应一行，那个英文场景就回到未通过。读数取自 vivaparity-m36a 的英文变体（React 参照 react-m36a-en，浅色皇家蓝中档 1280x800）。
const (
	enImportsGroup      = "Capture Tasks"                                                                                                      // imports.title：命令面板里「采集任务」分组标题（500 字重）
	enUpdateAvailable   = "Update Available"                                                                                                   // settings.updateAvailable：更新卡片的标题（text-lg，600 字重）
	enVersionLabel      = "Version"                                                                                                            // settings.version：「版本: x.y.z」的前缀（含单独的「Version」与整行）
	enReleaseNotesTitle = "RELEASE NOTES"                                                                                                      // settings.releaseNotes 经 uppercase 之后
	enSetupIntro        = "You can capture and run full-text search without any model. Set up a text model to unlock summaries, Q&A and Wiki." // setup.intro
	enSetupTranscribe   = "Turn video and audio into text"                                                                                     // setup.itemTranscription
	enMigrationSkip     = "Not now"                                                                                                            // migration.skip
	enMigrationStart    = "Migrate now"                                                                                                        // migration.start
)

// 读数里超出通用上限（kerningWidthCap 2.0）的两处，按实测向外取整到 0.5：命令面板「Capture Tasks」窄 2.19px、更新卡片「Update Available」窄 2.01px。
const kerning500Cap = 2.5

const (
	noteKern500Overlay = "字偶距（A-026 的英文延伸）：Viva 对 500 / 600 字重做字偶距、Chromium 不做，含 Ta、Up 这类字偶对的英文整行偏窄 2.0–2.2px"
	noteKern400Overlay = "字偶距（A-014 的英文延伸）：Chromium 对 400 字重做字偶距、Viva 不做，英文整行宽 1.0–1.6px，沿行方向累积成墨迹的水平位移（只管横向）"
)

// overlaysEnglishBackdropWaivers 是遮罩背后设置页（默认分区）的三处 400 字重字偶距（rail 分区名的宽度与墨迹、自动保存说明的宽度）：与快速采集对话框背后是同一个页面、同一组读数，直接复用那里的登记。
func overlaysEnglishBackdropWaivers() []parity.Waiver {
	return []parity.Waiver{
		englishKerning(kerning400Ledger, "text-width", "「"+enCaptureNav+"」", kerningWidthCap, noteKern400Capture),
		englishKerning(kerning400Ledger, "ink", "「"+enCaptureNav+"」", kerningInkCapLine, noteKern400Capture),
		englishKerning(kerning400Ledger, "text-width", "「"+enAutoSaveDesc+"」", kerningWidthCap, noteKern400Capture),
	}
}

// paletteEnglishWaivers 是命令面板搜索命中态：「Capture Tasks」分组标题（text-[11px] font-medium）Viva 窄 2.19px。
func paletteEnglishWaivers() []parity.Waiver {
	return slices.Concat(overlaysEnglishBackdropWaivers(), []parity.Waiver{
		englishKerning(kerning600Ledger, "text-width", "「"+enImportsGroup+"」", kerning500Cap, noteKern500Overlay),
	})
}

// updateEnglishWaivers 是更新对话框：卡片标题 Viva 窄 2.01px（宽度、外框、墨迹左移 1）；「Version」两处墨迹右移 1；
// 页头小标题 RELEASE NOTES 的字距加字偶距。
func updateEnglishWaivers() []parity.Waiver {
	return slices.Concat(overlaysEnglishBackdropWaivers(), wideLatinTrackingWaivers(enReleaseNotesTitle), []parity.Waiver{
		englishKerning(kerning600Ledger, "text-width", "「"+enUpdateAvailable+"」", kerning500Cap, noteKern500Overlay),
		englishKerning(kerning600Ledger, "box", "heading 「"+enUpdateAvailable+"」", kerning500Cap, noteKern500Overlay),
		englishKerning(kerning600Ledger, "ink", "「"+enUpdateAvailable+"」", kerningInkCapLine, noteKern500Overlay),
		englishKerning(kerning400Ledger, "ink", "「"+enVersionLabel, kerningInkCapLine, noteKern400Overlay),
	})
}

// setupEnglishWaivers 是设置清单：介绍正文墨迹右移 2（整段 400 字重正文累积），「Turn video and audio into text」宽差 1.56px、墨迹右移 1。
func setupEnglishWaivers() []parity.Waiver {
	return slices.Concat(overlaysEnglishBackdropWaivers(), []parity.Waiver{
		englishKerning(kerning400Ledger, "ink", "「"+enSetupIntro+"」", kerningInkCap, noteKern400Overlay),
		englishKerning(kerning400Ledger, "text-width", "「"+enSetupTranscribe+"」", kerningWidthCap, noteKern400Overlay),
		englishKerning(kerning400Ledger, "ink", "「"+enSetupTranscribe+"」", kerningInkCapLine, noteKern400Overlay),
	})
}

// migrationEnglishWaivers 是迁移确认：右对齐的钮组里「Migrate now」宽窄 1.01px（500 字重，Viva 偏窄，A-026），
// 右缘固定，左边的「Not now」与钮内图标累积出约 1.0px 的外框偏移，墨迹右移 1。
func migrationEnglishWaivers() []parity.Waiver {
	return slices.Concat(overlaysEnglishBackdropWaivers(), []parity.Waiver{
		englishKerning(kerning600Ledger, "text-width", "「"+enMigrationStart+"」", kerningWidthCap, noteKern500Overlay),
		englishKerning(kerning600Ledger, "ink", "「"+enMigrationStart+"」", kerningInkCapLine, noteKern500Overlay),
		englishKerning(kerning600Ledger, "box", "button 「"+enMigrationSkip+"」", kerningBoxMax, noteKern500Overlay+"；右对齐钮组，右缘固定，左边的钮累积外框偏移"),
		englishKerning(kerning600Ledger, "box", "button 「"+enMigrationStart+"」", kerningBoxMax, noteKern500Overlay+"；右对齐钮组，右缘固定"),
		{
			Ledger: kerning600Ledger, Layer: parity.LayerL2, Kind: "icon-box", Subject: "图标", Variant: englishTier, MaxDelta: kerningBoxMax,
			Note: noteKern500Overlay + "；钮内图标紧跟在文字之前，随钮宽偏 ≤ 1.1px（外框）",
		},
	})
}
