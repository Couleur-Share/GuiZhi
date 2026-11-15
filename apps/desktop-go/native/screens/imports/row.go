package imports

import (
	"github.com/egoist/mygo/ui"

	"guizhi-mygo-shell/native/kit"
	"guizhi-mygo-shell/native/kit/icons"
	"guizhi-mygo-shell/native/services"
)

// sourceIcon 对应渲染层 sourceKindIcon：网址、文件、文本三种来源。
func sourceIcon(kind string) icons.Name {
	switch kind {
	case "url":
		return icons.Globe
	case "file":
		return icons.FileText
	}
	return icons.Type
}

// statusColor 把状态映射到主题色：降级的完成态用警告色，不能只给一枚绿色的「已完成」。
func statusColor(t *ui.Theme, r rowVM) ui.Color {
	switch {
	case r.degraded:
		return t.Warning
	case r.status == services.ImportStatusFailed:
		return t.Danger
	case r.status == services.ImportStatusCompleted:
		return t.Success
	case r.status == services.ImportStatusProcessing:
		return t.Accent
	}
	return t.TextMuted
}

// drawRow 绘制一行任务。行内只读 rowVM，不做解析与格式化。
func drawRow(c *ui.Context, t *ui.Theme, r rowVM) {
	ui.Row(c).Gap(12).Padding(10, 12).AlignItems(ui.Center).Radius(8).
		Background(t.Surface).Border(1, t.Border).Key(r.id).Children(func() {
		kit.Icon(c, sourceIcon(r.sourceKind), 14).TextColor(t.TextMuted)
		ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
			ui.Text(c, r.name).MaxLines(1).Ellipsis("…").FontWeight(500)
			sub := r.sub
			if r.running {
				if sub != "" {
					sub += "  ·  "
				}
				sub += stageLabel(r.stage)
			}
			if sub != "" {
				ui.Text(c, sub).FontSize(12).TextColor(t.TextMuted).MaxLines(1).Ellipsis("…")
			}
			switch {
			case r.errText != "":
				ui.Text(c, r.errText).FontSize(12).TextColor(t.Danger).MaxLines(2).Ellipsis("…")
			case r.warnText != "":
				ui.Text(c, r.warnText).FontSize(12).TextColor(t.Warning).MaxLines(2).Ellipsis("…")
			}
		})
		ui.Text(c, r.timeLabel).FontSize(12).TextColor(t.TextMuted)
		ui.Text(c, statusLabel(r.status, r.degraded)).FontSize(12).FontWeight(600).
			TextColor(statusColor(t, r)).MinWidth(56)
	})
}
