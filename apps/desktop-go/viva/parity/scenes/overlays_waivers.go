package scenes

import (
	"slices"
	"strings"

	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/parity"
)

// 全局浮层（M3-6a）场景的登记：只复用 A-013（模态页眉标题的 tracking-tight）与它的英文延伸，不新增类别。
// 英文界面里被登记的标题写死英文（overlays_waivers_test.go 与 locales 目录里英文画出的字逐一核对；Waivers 在 init 时构造，那时界面语言还是中文）。
const (
	enUpdateTitle    = "Check for Updates"        // settings.checkUpdate：更新对话框的模态标题（text-xl）
	enSetupTitle     = "Get started with GuiZhi"  // setup.title：设置清单的模态标题
	enMigrationTitle = "Legacy GuiZhi data found" // migration.title：迁移确认的模态标题
)

// overlayTitleWaivers 登记模态页眉标题的 tracking-tight（A-013）：中文沿用 modalTitleWaivers 的窄口径，
// 英文取 englishTitleWaivers（同一成因，只在英文变体所在字号档放过 L2 的 text-width 与 ink；英文 Subject 的延伸待追认，台账 C-010）。
func overlayTitleWaivers(zh, en string) []parity.Waiver {
	return slices.Concat(modalTitleWaivers(zh), englishTitleWaivers(true, en))
}

// updateTitleWaivers、setupTitleWaivers、migrationTitleWaivers 是三个对话框标题的登记。
func updateTitleWaivers() []parity.Waiver {
	return overlayTitleWaivers(locales.SettingsCheckUpdate(), enUpdateTitle)
}

func setupTitleWaivers() []parity.Waiver {
	return overlayTitleWaivers(locales.SetupTitle(), enSetupTitle)
}

func migrationTitleWaivers() []parity.Waiver {
	return overlayTitleWaivers(locales.MigrationTitle(), enMigrationTitle)
}

// updateNotesWaivers 是更新说明在中文界面里的登记，都是已有条目的同源延伸：
//   - 页头「更新内容」是 uppercase tracking-wide（0.025em）的小标题：Viva 没有字距（A-013，同 wideTrackingWaivers 的口径）；
//   - 说明里带拉丁字母的一行（夹具里的「WAL」）是 400 字重：Chromium 对 400 字重做字偶距、Viva 不做（A-014），整行 Viva 略宽（实测 0.63px），只差在横向墨迹 1px。
func updateNotesWaivers() []parity.Waiver {
	return slices.Concat(wideTrackingWaivers(locales.SettingsReleaseNotes()), []parity.Waiver{{
		Ledger: kerning400Ledger, Layer: parity.LayerL2, Kind: "ink", Subject: updateNoteLine("WAL"), MaxDelta: kerningInkCapLine,
		Note: "字偶距：Chromium 对 400 字重做字偶距、Viva 不做，带拉丁字母的整行 Viva 略宽，累积成墨迹水平位移 1px（只管横向）",
	}})
}

// updateNoteLine 取夹具更新说明里含 sub 的那一条（去掉列表符号）：登记的 Subject 跟夹具走，改了夹具文字不必同步改登记。
func updateNoteLine(sub string) string {
	for _, line := range strings.Split(overlaysFixture().Update.ReleaseNotes, "\n") {
		if strings.Contains(line, sub) {
			return strings.TrimPrefix(line, "- ")
		}
	}
	panic("更新说明夹具里没有含 " + sub + " 的一条")
}

// paletteSnippetWaivers 是命令面板搜索命中态里知识条目片段的登记（A-014）：片段（夹具第一条）夹着拉丁字母「-wal」「VACUUM INTO」，
// 400 字重的整段 Chromium 做字偶距、Viva 不做，整行 Viva 略宽（实测 1.47px，上限取 kerningWidthCap）；同一条目的标题也带拉丁字母，墨迹位移 1px。只圈这两行，不放过别的文字。
func paletteSnippetWaivers() []parity.Waiver {
	first := overlaysFixture().Palette.Knowledge[0]
	subject := "「" + string([]rune(first.Snippet)[:10])
	return []parity.Waiver{
		{Ledger: kerning400Ledger, Layer: parity.LayerL2, Kind: "text-width", Subject: subject, MaxDelta: kerningWidthCap,
			Note: "字偶距：Chromium 对 400 字重做字偶距、Viva 不做，夹带拉丁字母的整段片段 Viva 略宽"},
		{Ledger: kerning400Ledger, Layer: parity.LayerL2, Kind: "ink", Subject: subject, MaxDelta: kerningInkCapLine,
			Note: "字偶距：同上，累积成片段墨迹水平位移 1px（只管横向，T1 大档 1920x1080 实测 +1）"},
		{Ledger: kerning400Ledger, Layer: parity.LayerL2, Kind: "ink", Subject: "「" + first.Title + "」", MaxDelta: kerningInkCapLine,
			Note: "字偶距：标题「SQLite WAL …」夹着拉丁字母，Chromium 对 400 字重做字偶距、Viva 不做，墨迹水平位移 1px（T2 大档实测 +1，只管横向）"},
	}
}

// updateColonWaiver 是更新卡片「版本: x.y.z」里冒号单字形的登记（A-009）：Viva 整窗 LCD、渲染层灰度抗锯齿，墨迹总量偏大；
// 单个冒号字形的墨迹量只有几十个像素，比值被放大——实测中文深色小档 1.501、英文中档 1.916，都超出通用上限 1.5（overlayInkWaiver 量得住整行文字）。
// 只放过这一个字形、浮层里、Viva 偏多的方向，上限 1.0（向外取整）。
func updateColonWaiver() parity.Waiver {
	return parity.Waiver{
		Ledger: "A-009", Layer: parity.LayerL2, Kind: "ink-mass", Subject: "「:」", Overlay: true, Dir: 1, MaxDelta: 1.0,
		Note: "浮层文字：渲染层灰度抗锯齿、Viva 整窗 LCD，墨迹总量偏大；单个冒号字形的墨迹量很小，比值被放大（通用上限 1.5 只量得住整行文字）",
	}
}
