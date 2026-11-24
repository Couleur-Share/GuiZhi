package imports

import (
	"sort"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// 采集文档站里批次与页面的文案与动作判定（渲染层 DocumentSitePanel 的 labels 表与按钮显隐条件）。

// 表单的取值范围（渲染层输入框的 min / max）：只给界面做步进与提示用，真正的校验在主进程的 crawl:create 处理器里，
// 越界时处理器的原因原样弹出（可复制的详情），所以 Store 不重复校验。
const (
	DocSiteMinPages     = 1
	DocSiteMaxPages     = 300
	DocSiteDefaultPages = 50
	DocSiteMinDepth     = 0
	DocSiteMaxDepth     = 5
	DocSiteDefaultDepth = 2
)

// JobStatusLabel 是批次状态的文案；认不出的状态返回空串（渲染层 labels[job.status]，没有就不显示）。
func JobStatusLabel(status string) string {
	switch status {
	case services.CrawlJobPending:
		return locales.WebCapturePending()
	case services.CrawlJobRunning:
		return locales.WebCaptureCapturing()
	case services.CrawlJobPaused:
		return locales.WebCapturePaused()
	case services.CrawlJobInterrupted:
		return locales.WebCaptureInterrupted()
	case services.CrawlJobCompleted:
		return locales.WebCaptureCompleted()
	case services.CrawlJobCanceled:
		return locales.WebCaptureCanceled()
	}
	return ""
}

// PageStatusLabel 是页面状态的文案（running 与 pending 沿用批次的「采集中」「等待」）；认不出的状态返回空串。
func PageStatusLabel(status string) string {
	switch status {
	case services.CrawlPagePending:
		return locales.WebCapturePending()
	case services.CrawlPageRunning:
		return locales.WebCaptureCapturing()
	case services.CrawlPageAdded:
		return locales.WebCaptureAdded()
	case services.CrawlPageDuplicate:
		return locales.WebCaptureDuplicate()
	case services.CrawlPageUpdated:
		return locales.WebCaptureUpdated()
	case services.CrawlPagePendingVersion:
		return locales.WebCapturePendingVersion()
	case services.CrawlPageUnchanged:
		return locales.WebCaptureUnchanged()
	case services.CrawlPageFailed:
		return locales.WebCaptureFailed()
	case services.CrawlPageSkipped:
		return locales.WebCaptureSkipped()
	case services.CrawlPageCanceled:
		return locales.WebCaptureCanceled()
	}
	return ""
}

// CountEntry 是批次计数里的一项：状态、文案（认不出的状态用状态名本身，渲染层 labels[key] || key）与页数。
type CountEntry struct {
	Status string
	Label  string
	Count  int
}

// 页面状态在计数行里的固定顺序：渲染层按后端 JSON 的键序显示，Go 的 map 没有顺序，所以定一个稳定的（按处理流程）。
var pageStatusOrder = []string{
	services.CrawlPagePending, services.CrawlPageRunning, services.CrawlPageAdded, services.CrawlPageDuplicate,
	services.CrawlPageUpdated, services.CrawlPagePendingVersion, services.CrawlPageUnchanged, services.CrawlPageFailed,
	services.CrawlPageSkipped, services.CrawlPageCanceled,
}

// OrderedCounts 把批次的计数按固定顺序展开；认不出的状态排在后面、按状态名排序；页数为 0 的项照实保留（后端不给就没有）。
func OrderedCounts(job services.CrawlJob) []CountEntry {
	out := make([]CountEntry, 0, len(job.Counts))
	seen := make(map[string]bool, len(job.Counts))
	for _, status := range pageStatusOrder {
		if n, ok := job.Counts[status]; ok {
			out = append(out, CountEntry{Status: status, Label: PageStatusLabel(status), Count: n})
			seen[status] = true
		}
	}
	var extra []string
	for status := range job.Counts {
		if !seen[status] {
			extra = append(extra, status)
		}
	}
	sort.Strings(extra)
	for _, status := range extra {
		out = append(out, CountEntry{Status: status, Label: status, Count: job.Counts[status]})
	}
	return out
}

// JobActions 是一个批次上能点的按钮（渲染层的显隐条件）：
// 运行中显示「暂停」，否则只要没取消就显示「继续」；「重试失败项」总是显示、有失败页才可点；已完成与已取消的批次没有「取消」。
type JobActions struct {
	ShowPause  bool
	ShowResume bool
	ShowCancel bool
	CanRetry   bool
}

// ActionsFor 求一个批次的按钮状态。操作进行中（Busy）时按钮是否禁用由界面叠加。
func ActionsFor(job services.CrawlJob) JobActions {
	return JobActions{
		ShowPause:  job.Status == services.CrawlJobRunning,
		ShowResume: job.Status != services.CrawlJobRunning && job.Status != services.CrawlJobCanceled,
		ShowCancel: job.Status != services.CrawlJobCompleted && job.Status != services.CrawlJobCanceled,
		CanRetry:   job.Counts[services.CrawlPageFailed] > 0,
	}
}

// JobTitleURL 是批次标题里的入口网址（渲染层 job.input.seeds[0].url）；没有入口返回空串。
func JobTitleURL(job services.CrawlJob) string {
	if len(job.Input.Seeds) == 0 {
		return ""
	}
	return job.Input.Seeds[0].URL
}
