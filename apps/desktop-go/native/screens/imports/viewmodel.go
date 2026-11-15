package imports

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"guizhi-mygo-shell/native/kit"
	"guizhi-mygo-shell/native/locales"
	"guizhi-mygo-shell/native/services"
)

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// rowVM 是一行的展示模型：与语言无关的文本在构建快照时一次算好，状态/阶段等随语言变化的标签在绘制时取。
type rowVM struct {
	id         string
	name       string // 显示名；采集前是原始输入
	sub        string // 副标题：网址取域名（去 www.），文件取所在目录
	sourceKind string
	status     string
	stage      string
	running    bool // pending 或 processing
	degraded   bool // 已入库但内容有缺失：不能只给一枚绿色的「已完成」
	errText    string
	warnText   string
	timeLabel  string
}

// snapshot 是页面某一时刻的只读数据。
type snapshot struct {
	loaded  bool // 首次加载是否已结束：队列为空既可能是「真没有任务」也可能是「还没读出来」，必须能区分
	loading bool
	err     string
	rows    []rowVM
	total   int64
	active  int64
	counts  map[string]int64 // 键是过滤 id：all/active/completed/degraded/duplicate/failed/canceled
}

func isActive(t services.ImportTask) bool {
	return t.Status == services.ImportStatusPending || t.Status == services.ImportStatusProcessing
}

// isDegraded：入库了但内容有缺失（转写失败等）。
func isDegraded(t services.ImportTask) bool {
	return t.Status == services.ImportStatusCompleted && t.Warning != ""
}

func matchesFilter(t services.ImportTask, filter string) bool {
	switch filter {
	case services.ImportFilterAll, "":
		return true
	case services.ImportFilterActive:
		return isActive(t)
	case services.ImportFilterDegraded:
		return isDegraded(t)
	}
	return t.Status == filter
}

// matchesQuery 搜索同时匹配显示名、原始输入、错误与警告——采集成功后显示名会换成真实标题，
// 只匹配显示名的话，用手里的链接反而搜不到那条任务。
func matchesQuery(t services.ImportTask, keyword string) bool {
	if keyword == "" {
		return true
	}
	for _, s := range []string{t.DisplayName, t.SourceInput, t.Error, t.Warning} {
		if strings.Contains(strings.ToLower(s), keyword) {
			return true
		}
	}
	return false
}

// mergeActiveFirst 进行中的任务排在前面，并按 id 去重。
func mergeActiveFirst(active, entries []services.ImportTask) []services.ImportTask {
	seen := make(map[string]struct{}, len(active)+len(entries))
	out := make([]services.ImportTask, 0, len(active)+len(entries))
	for _, list := range [][]services.ImportTask{active, entries} {
		for _, t := range list {
			if _, dup := seen[t.ID]; dup {
				continue
			}
			seen[t.ID] = struct{}{}
			out = append(out, t)
		}
	}
	return out
}

// countsFromPage 把服务端的「状态 → 数量」换成筛选组的各档计数（口径同渲染层 countsFromResult）。
func countsFromPage(p services.ImportPage) map[string]int64 {
	var all int64
	for _, n := range p.Counts {
		all += n
	}
	return map[string]int64{
		services.ImportFilterAll:       all,
		services.ImportFilterActive:    p.Counts[services.ImportStatusPending] + p.Counts[services.ImportStatusProcessing],
		services.ImportStatusCompleted: p.Counts[services.ImportStatusCompleted],
		services.ImportFilterDegraded:  p.DegradedCount,
		services.ImportStatusDuplicate: p.Counts[services.ImportStatusDuplicate],
		services.ImportStatusFailed:    p.Counts[services.ImportStatusFailed],
		services.ImportStatusCanceled:  p.Counts[services.ImportStatusCanceled],
	}
}

// buildSnapshot 把一页结果整理成快照：进行中的任务在前，再按当前筛选与搜索收窄。
func buildSnapshot(p services.ImportPage, q query, now time.Time, locale string) snapshot {
	keyword := strings.ToLower(strings.TrimSpace(q.text))
	merged := mergeActiveFirst(p.Active, p.Entries)
	rows := make([]rowVM, 0, len(merged))
	for _, t := range merged {
		if matchesFilter(t, q.filter) && matchesQuery(t, keyword) {
			rows = append(rows, toRow(t, now, locale))
		}
	}
	counts := countsFromPage(p)
	return snapshot{loaded: true, rows: rows, total: p.Total, active: counts[services.ImportFilterActive], counts: counts}
}

func toRow(t services.ImportTask, now time.Time, locale string) rowVM {
	running := isActive(t)
	stamp := t.UpdatedAt
	if running {
		stamp = t.CreatedAt
	}
	name := t.DisplayName
	if name == "" {
		name = t.SourceInput
	}
	return rowVM{
		id: t.ID, name: name, sub: resolveSub(t), sourceKind: t.SourceKind, status: t.Status, stage: t.Stage,
		running: running, degraded: isDegraded(t), errText: t.Error, warnText: t.Warning,
		timeLabel: kit.FormatItemTime(stamp, now, locale),
	}
}

// resolveSub 网址任务取域名（去掉 www.，一列链接混在一起时最省版面的辨识标记），文件任务取所在目录。
func resolveSub(t services.ImportTask) string {
	switch t.SourceKind {
	case "url":
		if u, err := url.Parse(t.SourceInput); err == nil {
			return strings.TrimPrefix(u.Hostname(), "www.")
		}
	case "file":
		segs := strings.FieldsFunc(t.SourceInput, func(r rune) bool { return r == '/' || r == '\\' })
		if len(segs) > 1 {
			return strings.Join(segs[:len(segs)-1], "/")
		}
	}
	return ""
}

// statusLabel 取状态文案；降级的完成态单独成档。
func statusLabel(status string, degraded bool) string {
	if degraded {
		return locales.ImportsStatusDegraded()
	}
	switch status {
	case services.ImportStatusPending:
		return locales.ImportsStatusPending()
	case services.ImportStatusProcessing:
		return locales.ImportsStatusProcessing()
	case services.ImportStatusCompleted:
		return locales.ImportsStatusCompleted()
	case services.ImportStatusFailed:
		return locales.ImportsStatusFailed()
	case services.ImportStatusCanceled:
		return locales.ImportsStatusCanceled()
	case services.ImportStatusDuplicate:
		return locales.ImportsStatusDuplicate()
	}
	return status
}

// stageLabel 取阶段文案；未知阶段与渲染层一致，回落到「抓取中」。
func stageLabel(stage string) string {
	switch stage {
	case "web-preparing":
		return locales.ImportsStageWebPreparing()
	case "web-verifying":
		return locales.ImportsStageWebVerifying()
	case "extracting":
		return locales.ImportsStageExtracting()
	case "saving":
		return locales.ImportsStageSaving()
	case "video-metadata":
		return locales.ImportsStageVideoMetadata()
	case "video-captions":
		return locales.ImportsStageVideoCaptions()
	case "video-audio":
		return locales.ImportsStageVideoAudio()
	case "transcoding":
		return locales.ImportsStageTranscoding()
	case "transcribing":
		return locales.ImportsStageTranscribing()
	case "formatting":
		return locales.ImportsStageFormatting()
	case "summarizing":
		return locales.ImportsStageSummarizing()
	case "image-download":
		return locales.ImportsStageImageDownload()
	case "image-ocr":
		return locales.ImportsStageImageOcr()
	case "forum-replies":
		return locales.ImportsStageForumReplies()
	case "browser-capture":
		return locales.ImportsStageBrowserCapture()
	case "comments":
		return locales.ImportsStageComments()
	}
	return locales.ImportsStageFetching()
}
