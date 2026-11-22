package settings

import (
	"context"
	"fmt"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// usageWindowDays 是用量统计的窗口（USAGE_WINDOW_DAYS）。
const usageWindowDays = 30

// aiUsage 是用量块的界面状态。
type aiUsage struct {
	summary    *services.AIUsageSummary
	refreshing bool
	byModel    bool // 分组：false 按场景，true 按模型
}

// loadUsage 读取最近 30 天的用量。interactive 表示用户点了刷新：此时才有「刷新中」状态。
// 失败一律弹错误提示：否则会伪装成「还没有调用记录」。
func (a *AI) loadUsage(interactive bool) {
	svc := a.Service
	if interactive {
		a.usage.refreshing = true
	}
	a.w.load(a.Wake, func(ctx context.Context) func() {
		sum, err := services.AIUsageSummary{}, errAIUnavailable
		if svc != nil {
			sum, err = svc.UsageSummary(ctx, usageWindowDays)
		}
		if err != nil && ctx.Err() == nil {
			a.Guard.Fail(locales.SettingsAiUsageLoadFailed(), locales.SettingsAi(), err)
		}
		return func() {
			a.usage.refreshing = false
			if err == nil {
				a.usage.summary = &sum
			}
		}
	})
}

// formatCount 是 token 数的简写：千以上带 k，百万以上带 M（保留一位小数）。
func formatCount(v int64) string {
	switch {
	case v >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(v)/1_000_000)
	case v >= 1_000:
		return fmt.Sprintf("%.1fk", float64(v)/1_000)
	}
	return fmt.Sprint(v)
}

// usageScenarioLabel 是用量场景的显示名；认不得的场景原样显示。
func usageScenarioLabel(id string) string {
	switch id {
	case "qa":
		return locales.SettingsAiScenarioQa()
	case "wiki":
		return locales.SettingsAiScenarioWiki()
	case "research":
		return locales.SettingsAiScenarioResearch()
	case "summary":
		return locales.SettingsAiScenarioSummary()
	case "tagging":
		return locales.SettingsAiScenarioTagging()
	case "ocr":
		return locales.SettingsAiScenarioOcr()
	case "transcription":
		return locales.SettingsAiScenarioTranscription()
	case "formatting":
		return locales.SettingsAiScenarioFormatting()
	case "embedding":
		return locales.SettingsAiScenarioEmbedding()
	case "illustration":
		return locales.SettingsAiScenarioIllustration()
	case "themedReading":
		return locales.SettingsAiScenarioThemedReading()
	}
	return id
}

// usageSection 是用量块：没有调用记录时是一行说明，否则是标题、分组切换、刷新与逐行份额条。
// 失败次数故意不展示：面板只回答「用了多少」。
func (a *AI) usageSection(c ui.C) {
	l := a.Look
	t := l.T
	sum := a.usage.summary
	if sum == nil || sum.Calls == 0 {
		ui.Row(c).FillWidth().Pad(l.Space(4)).
			Draw(func(d *ui.DrawContext) {
				l.PaintSurface(d, kit.Surface{Border: t.Border, BorderW: 1, Radius: l.Rem(0.75)})
			}).Children(func() {
			l.Text(c.Key("empty"), locales.SettingsAiUsageEmpty(usageWindowDays), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
		})
		return
	}
	tokens := sum.PromptTokens + sum.CompletionTokens
	type usageRow struct {
		key, label    string
		calls, tokens int64
	}
	var rows []usageRow
	if a.usage.byModel {
		for _, r := range sum.ByModel {
			rows = append(rows, usageRow{r.Model, r.Model, r.Calls, r.PromptTokens + r.CompletionTokens})
		}
	} else {
		for _, r := range sum.ByScenario {
			rows = append(rows, usageRow{r.Scenario, usageScenarioLabel(r.Scenario), r.Calls, r.PromptTokens + r.CompletionTokens})
		}
	}

	ui.Column(c).FillWidth().Gap(l.Space(3)).Pad(l.Space(4)).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Border: t.Border, BorderW: 1, Radius: l.Rem(0.75)})
		}).Children(func() {
		// 标题行：标题与合计靠左，分组切换与刷新靠右。
		ui.Row(c.Key("head")).FillWidth().Items(ui.Center).Gap(l.Space(4)).Children(func() {
			l.Text(c.Key("title"), locales.SettingsAiUsageTitle(usageWindowDays), kit.TextSM, kit.WeightMedium, t.Foreground)
			l.Text(c.Key("total"), locales.SettingsAiUsageTotal(sum.Calls, formatCount(tokens)), kit.TextXS, kit.WeightNormal, t.MutedForeground)
			ui.Row(c.Key("fill")).Grow(1).MinWidth(0)
			a.usageGroupToggle(c.Key("group"))
			label := locales.CommonRefresh()
			if a.usage.refreshing {
				label = locales.CommonRefreshing()
			}
			st := kit.ButtonStyle{Text: kit.TextXS, Weight: kit.WeightNormal, Fg: t.MutedForeground, FgHover: t.Foreground, Transition: kit.DurationDefault}
			if btn(l, c.Key("refresh"), kit.ButtonSpec{Label: label, Disabled: a.usage.refreshing}, st, false) {
				a.loadUsage(true)
			}
		})
		if tokens == 0 {
			l.TextPx(c.Key("notokens"), locales.SettingsAiUsageNoTokens(), 11, 16.5, kit.WeightNormal, t.MutedForeground.WithAlpha(0.8), kit.Wrapped())
		}
		ui.Column(c.Key("rows")).FillWidth().Gap(l.Space(1.5)).Children(func() {
			for _, r := range rows {
				labelW := l.Rem(5) // w-20
				if a.usage.byModel {
					labelW = l.Rem(9) // w-36
				}
				share := float32(0)
				if sum.Calls > 0 {
					share = float32(r.calls) / float32(sum.Calls)
				}
				ui.Row(c.Key("row:" + r.key)).FillWidth().Items(ui.Center).Gap(l.Space(3)).Children(func() {
					l.Text(c.Key("label"), r.label, kit.TextXS, kit.WeightNormal, t.Foreground, kit.Ellipsis()).Width(labelW).Shrink(0)
					// 份额条：h-1.5 的 muted 轨道，primary/70 的填充。
					ui.Row(c.Key("track")).Grow(1).MinWidth(0).Height(l.Rem(0.375)).
						Draw(func(d *ui.DrawContext) { l.PaintSurface(d, kit.Surface{Bg: t.Muted, Radius: d.Rect.Height / 2}) }).
						Children(func() {
							ui.Row(c.Key("fill")).WidthPercent(min(share, 1) * 100).Height(l.Rem(0.375)).
								Draw(func(d *ui.DrawContext) {
									l.PaintSurface(d, kit.Surface{Bg: t.Primary.WithAlpha(0.7), Radius: d.Rect.Height / 2})
								})
						})
					l.Text(c.Key("count"), locales.SettingsAiUsageRow(r.calls, formatCount(r.tokens)), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Right()).
						Width(l.Rem(7)).Shrink(0)
				})
			}
		})
	})
}

// usageGroupToggle 是「按场景 / 按模型」的分段切换（`rounded-lg border border-border p-0.5`，选中是 bg-accent）。
func (a *AI) usageGroupToggle(c ui.C) {
	l := a.Look
	t := l.T
	ui.Row(c).Shrink(0).Items(ui.Center).Pad(l.Space(0.5)).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Border: t.Border, BorderW: 1, Radius: l.Rem(0.75)})
		}).Children(func() {
		seg := func(key, label string, on bool) bool {
			st := kit.ButtonStyle{
				PadX: l.Space(2), PadY: l.Space(0.5), Radius: mdRadius(l), FontPx: 11, LinePx: 16.5, Weight: kit.WeightNormal,
				Fg: t.MutedForeground, FgHover: t.Foreground, FgOn: t.Foreground, BgOn: t.Accent, Transition: kit.DurationDefault,
			}
			return l.Button(c.Key(key), kit.ButtonSpec{Label: label, Toggle: true, On: on}, st).Clicked()
		}
		if seg("scenario", locales.SettingsAiUsageGroupScenario(), !a.usage.byModel) {
			a.usage.byModel = false
		}
		if seg("model", locales.SettingsAiUsageGroupModel(), a.usage.byModel) {
			a.usage.byModel = true
		}
	})
}
