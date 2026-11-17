package imports

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 采集任务的诊断文本（渲染层 components/imports/import-task-report.ts）：把一条任务序列化成可直接粘进 issue 或丢给 AI 的 Markdown。
//
// 与界面显示有一处故意的不同：这里列出全部阶段，不折叠亚秒的那几个——折叠是为了扫视时的信噪比，
// 而诊断文本的读者是排查的人（或模型），少一行就是少一条线索。

// ReportContext 是生成诊断文本时的外部信息。
type ReportContext struct {
	// Now 是诊断生成时刻（毫秒）：处理中的任务要用它补算当前阶段的耗时。
	Now int64
	// AppVersion 与 Platform 取不到就不写「应用」那一行。
	AppVersion, Platform string
	// CaptureBrowser 是登录态采集用的浏览器（名称加版本），空则不写。
	CaptureBrowser string
	// FormatTime 格式化绝对时间；空时取本地时区的 toLocaleString 写法。
	FormatTime func(ms int64) string
}

// markdownPipe 与换行都会把表格单元格切断。
var (
	markdownPipe = regexp.MustCompile(`\|`)
	newlineRuns  = regexp.MustCompile(`\n+`)
	// ^\[([a-z_]+)\]：登录态采集的报错只保留稳定错误码，原文可能带页面内容。
	errorCodePrefix = regexp.MustCompile(`^\[([a-z_]+)\]`)
)

func escapeCell(value string) string {
	return newlineRuns.ReplaceAllString(markdownPipe.ReplaceAllString(value, `\|`), " ")
}

// FormatShare 是阶段占比的文案。四舍五入到 0 但确实花了时间的阶段写「<1%」而不是「0%」：
// 后者与「一点没花」撞脸，而它旁边那一列明明写着 0:01。
func FormatShare(ms, totalMs int64) string {
	share := 0.0
	if totalMs > 0 {
		share = float64(ms) / float64(totalMs) * 100
	}
	rounded := int64(share + 0.5) // Math.round：占比非负，四舍五入取 floor(x+0.5)
	if rounded == 0 && ms > 0 {
		return "<1%"
	}
	return strconv.FormatInt(rounded, 10) + "%"
}

// AIDetail 是阶段的 AI 开销说明「N 次调用 · M 次失败 · K token · 模型」；没有调用时为空。
func AIDetail(stat services.ImportStageStat) string {
	if stat.Calls == 0 {
		return ""
	}
	parts := []string{locales.ImportsStageCalls(stat.Calls)}
	if stat.FailedCalls > 0 {
		parts = append(parts, locales.ImportsStageFailedCalls(stat.FailedCalls))
	}
	if tokens := stat.PromptTokens + stat.CompletionTokens; tokens > 0 {
		parts = append(parts, locales.ImportsStageTokens(tokens))
	}
	if len(stat.Models) > 0 {
		parts = append(parts, strings.Join(stat.Models, "\u3001")) // 顿号
	}
	return strings.Join(parts, " \u00b7 ")
}

// statusLabelOrFailed 取状态文案；不认识的状态按「失败」处理（渲染层 STATUS_LABELS[status] ?? STATUS_LABELS.failed）。
func statusLabelOrFailed(status string) string {
	switch status {
	case services.ImportStatusPending, services.ImportStatusProcessing, services.ImportStatusCompleted,
		services.ImportStatusDuplicate, services.ImportStatusCanceled, services.ImportStatusFailed:
		return StatusLabel(status)
	}
	return locales.ImportsStatusFailed()
}

// BuildTaskReport 生成一条任务的诊断文本（Markdown）。
func BuildTaskReport(task services.ImportTask, ctx ReportContext) string {
	formatTime := ctx.FormatTime
	if formatTime == nil {
		formatTime = func(ms int64) string { return kit.FormatDateTime(ms, time.Local, kit.DateLocaleZH) }
	}
	capturePlatform := ""
	if task.SourceKind == "url" {
		capturePlatform = DetectPlatformCapturePlatform(task.SourceInput)
	}
	authenticated := task.CaptureStrategy == "authenticated"
	safeSource := task.SourceInput
	if authenticated {
		safeSource = capturePlatform
		if safeSource == "" {
			safeSource = "authenticated-platform"
		}
	}

	lines := []string{
		"# " + locales.ImportsReportTitle(),
		"",
		"- " + locales.ImportsReportStatus() + "\uff1a" + statusLabelOrFailed(task.Status),
	}
	add := func(label, value string) { lines = append(lines, "- "+label+"\uff1a"+value) }
	if !authenticated {
		name := task.DisplayName
		if name == "" {
			name = safeSource
		}
		add(locales.ImportsReportTask(), name)
	}
	if task.ItemType != "" {
		add(locales.ImportsReportType(), task.ItemType)
	}
	add(locales.ImportsReportSource(), safeSource)
	if authenticated {
		add(locales.ImportsReportCaptureStrategy(), "authenticated")
	}
	if !authenticated {
		add(locales.ImportsReportQueuedAt(), formatTime(task.CreatedAt))
		add(locales.ImportsReportUpdatedAt(), formatTime(task.UpdatedAt))
		add(locales.ImportsReportTaskId(), task.ID)
	}
	if ctx.AppVersion != "" {
		app := "GuiZhi " + ctx.AppVersion
		if ctx.Platform != "" {
			app += " \u00b7 " + ctx.Platform
		}
		add(locales.ImportsReportApp(), app)
	}

	if stats := ResolveStageStats(task, ctx.Now); len(stats) > 0 {
		var total int64
		for _, e := range stats {
			total += e.Ms
		}
		lines = append(lines,
			"",
			"## "+locales.ImportsReportStages()+"\uff08"+locales.ImportsTotalElapsed(FormatDuration(total))+"\uff09",
			"",
			"| "+locales.ImportsReportStageColumn()+" | "+locales.ImportsReportDurationColumn()+" | "+
				locales.ImportsReportShareColumn()+" | "+locales.ImportsReportAiColumn()+" |",
			"| --- | ---: | ---: | --- |",
		)
		for _, stat := range stats {
			name, ok := knownStageLabel(stat.Stage)
			if !ok {
				name = stat.Stage
			}
			ai := ""
			if !authenticated {
				ai = escapeCell(AIDetail(stat))
			}
			lines = append(lines, "| "+escapeCell(name)+" | "+FormatDuration(stat.Ms)+" | "+FormatShare(stat.Ms, total)+" | "+ai+" |")
		}
	}

	if task.Error != "" {
		safeError := ""
		if authenticated {
			safeError = "unknown"
			if m := errorCodePrefix.FindStringSubmatch(task.Error); m != nil {
				safeError = m[1]
			}
		} else {
			safeError = FormatTaskErrorForReport(task.Error)
		}
		lines = append(lines, "", "## "+locales.ImportsReportError(), "", safeError)
	}
	if task.Warning != "" {
		warning := FormatTaskWarning(task.Warning)
		if authenticated {
			warning = locales.ImportsAuthenticatedWarningPresent()
		}
		lines = append(lines, "", "## "+locales.ImportsReportWarning(), "", warning)
	}
	if ctx.CaptureBrowser != "" {
		add(locales.ImportsReportBrowser(), ctx.CaptureBrowser)
	}
	return strings.Join(lines, "\n") + "\n"
}
