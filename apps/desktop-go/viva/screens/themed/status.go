package themed

// 任务状态卡（ThemedReadingStatus.tsx）的内容判断：标题、阶段步骤、可用按钮与「生成详情」各行。
// 全部是纯函数：界面只负责把 StatusModel 画出来。

import (
	"fmt"
	"sort"
	"strings"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// StatusOptions 是状态卡的显示参数。
type StatusOptions struct {
	// ReadOnly：条目只读（回收站），不出现停止 / 继续按钮。
	ReadOnly bool
	// Expanded：首次生成时居中的大卡片（突出阶段进展）；false 是页面上方的紧凑条。
	Expanded bool
	// HasPage：已有成品页面（决定说明文字）。
	HasPage bool
	// OfflineAvailable：调用方能处理「不联网继续」（任务列表里没有这个入口）。
	OfflineAvailable bool
}

// StatusStep 是进度条里的一个阶段。
type StatusStep struct {
	Stage         string
	Label         string
	Done, Current bool
}

// StatusModel 是状态卡的全部内容。
type StatusModel struct {
	Active bool
	// Title 是主标题；Headline 是展开样式下标题上方的小字（仅活动中）；Detail 是设计阶段的实时输出进度；Context 是说明。
	Title, Headline, Detail, Context string
	// Steps 只在活动中给出；StageCount 只在展开样式、活动且已开始执行时非空（「阶段 3 / 8」）。
	Steps      []StatusStep
	StageCount string
	// 按钮。ResumeLabel 在查证阶段暂停时是「补查资料并继续」。
	ShowStop, ShowResume, ShowOffline bool
	ResumeLabel                       string
	// Error 是失败原因原文（可复制）；Lines 是「生成详情」里的其余各行，按显示顺序。
	Error string
	Lines []string
	// BackgroundHint 是「可以切换文章」的提示：展开样式放在卡片底部，紧凑样式放在详情里（仅活动中）。
	BackgroundHint string
}

var stepOrder = []string{
	services.ThemedStagePrepare, services.ThemedStageUnderstand, services.ThemedStageResearch, services.ThemedStageWrite,
	services.ThemedStageDesign, services.ThemedStageImages, services.ThemedStageAssemble, services.ThemedStageValidate,
}

// StageLabel 是阶段的完整名称（标题里用）；未知阶段原样返回。
func StageLabel(stage string) string {
	switch stage {
	case services.ThemedStagePrepare:
		return locales.ThemedReadingStagePrepare()
	case services.ThemedStageUnderstand:
		return locales.ThemedReadingStageUnderstand()
	case services.ThemedStageResearch:
		return locales.ThemedReadingStageResearch()
	case services.ThemedStageWrite:
		return locales.ThemedReadingStageWrite()
	case services.ThemedStageDesign:
		return locales.ThemedReadingStageDesign()
	case services.ThemedStageImages:
		return locales.ThemedReadingStageImages()
	case services.ThemedStageAssemble:
		return locales.ThemedReadingStageAssemble()
	case services.ThemedStageValidate:
		return locales.ThemedReadingStageValidate()
	case services.ThemedStageDone:
		return locales.ThemedReadingStageDone()
	}
	return stage
}

func stepLabel(stage string) string {
	switch stage {
	case services.ThemedStagePrepare:
		return locales.ThemedReadingStepPrepare()
	case services.ThemedStageUnderstand:
		return locales.ThemedReadingStepUnderstand()
	case services.ThemedStageResearch:
		return locales.ThemedReadingStepResearch()
	case services.ThemedStageWrite:
		return locales.ThemedReadingStepWrite()
	case services.ThemedStageDesign:
		return locales.ThemedReadingStepDesign()
	case services.ThemedStageImages:
		return locales.ThemedReadingStepImages()
	case services.ThemedStageAssemble:
		return locales.ThemedReadingStepAssemble()
	case services.ThemedStageValidate:
		return locales.ThemedReadingStepValidate()
	}
	return stage
}

// StateLabel 是任务状态的名称。
func StateLabel(state string) string {
	switch state {
	case services.ThemedTaskQueued:
		return locales.ThemedReadingReaderStateQueued()
	case services.ThemedTaskRunning:
		return locales.ThemedReadingReaderStateRunning()
	case services.ThemedTaskCompleted:
		return locales.ThemedReadingReaderStateCompleted()
	case services.ThemedTaskPartial:
		return locales.ThemedReadingReaderStatePartial()
	case services.ThemedTaskFailed:
		return locales.ThemedReadingReaderStateFailed()
	case services.ThemedTaskCancelled:
		return locales.ThemedReadingReaderStateCancelled()
	case services.ThemedTaskInterrupted:
		return locales.ThemedReadingReaderStateInterrupted()
	}
	return state
}

// seconds 把毫秒格式化成一位小数的秒数（与渲染层的 toFixed(1) 一致）。
func seconds(ms int64) string { return fmt.Sprintf("%.1f", float64(ms)/1000) }

// BuildStatus 计算任务状态卡的内容。
func BuildStatus(task services.ThemedTask, o StatusOptions) StatusModel {
	active := task.Active()
	research := task.Stage == services.ThemedStageResearch
	resumable := task.Resumable()

	// 阶段步骤：已确定不生图的任务不显示「图片」步（除非正处在这一阶段）。
	var stages []string
	for _, st := range stepOrder {
		if st == services.ThemedStageImages && task.PlannedImages != nil && *task.PlannedImages == 0 && task.Stage != st {
			continue
		}
		stages = append(stages, st)
	}
	current := len(stages)
	if task.Stage != services.ThemedStageDone {
		current = -1
		for i, st := range stages {
			if st == task.Stage {
				current = i
			}
		}
	}

	stageLabel := StageLabel(task.Stage)
	if task.Stage == services.ThemedStageAssemble && task.Total > 0 {
		stageLabel = locales.ThemedReadingCompilingGraphics()
	}
	m := StatusModel{Active: active}
	if task.Stage == services.ThemedStageDesign && task.DesignProgress != nil {
		if task.DesignProgress.ReceivedChars > 0 {
			m.Detail = locales.ThemedReadingDesignReceiving(task.DesignProgress.ReceivedChars)
		} else {
			m.Detail = locales.ThemedReadingDesignThinking()
		}
	}
	if !active {
		m.Detail = "" // 实时输出进度只在执行中有意义
	}

	switch {
	case active && task.State == services.ThemedTaskQueued:
		m.Title = locales.ThemedReadingReaderQueued()
	case active:
		label := stageLabel
		if task.DesignProgress != nil && task.DesignProgress.Attempt > 1 && task.Stage == services.ThemedStageDesign {
			label = locales.ThemedReadingDesignRepairing()
		}
		m.Title = label
		if task.Total > 0 {
			m.Title += fmt.Sprintf(" · %d/%d", task.Completed, task.Total)
		}
	case research:
		m.Title = locales.ThemedReadingReaderResearchPaused()
	default:
		m.Title = StateLabel(task.State)
	}

	switch {
	case task.State == services.ThemedTaskCompleted:
	case task.LegacyFormat:
		m.Context = locales.ThemedReadingReaderLegacyTask()
	case o.HasPage && active:
		m.Context = locales.ThemedReadingReaderUpdating()
	case o.HasPage:
		m.Context = locales.ThemedReadingReaderPreviousAvailable()
	default:
		m.Context = locales.ThemedReadingReaderSourceSafe()
	}

	if active {
		m.Headline = ""
		if o.Expanded {
			m.Headline = locales.ThemedReadingReaderGenerating()
		}
		for i, st := range stages {
			started := task.State != services.ThemedTaskQueued
			m.Steps = append(m.Steps, StatusStep{
				Stage: st, Label: stepLabel(st),
				Done: started && i < current, Current: started && i == current,
			})
		}
		if o.Expanded && task.State != services.ThemedTaskQueued {
			m.StageCount = locales.ThemedReadingReaderStageCount(min(current+1, len(stages)), len(stages))
		}
		m.BackgroundHint = locales.ThemedReadingBackgroundHint()
	}

	m.ShowStop = !o.ReadOnly && active
	m.ShowResume = !o.ReadOnly && resumable
	m.ResumeLabel = locales.ThemedReadingReaderResume()
	if research {
		m.ResumeLabel = locales.ThemedReadingReaderRetryResearch()
	}
	m.ShowOffline = !o.ReadOnly && research && resumable && o.OfflineAvailable

	m.Error = task.Error
	if n := task.PlannedImages; n != nil && *n >= 0 && *n <= services.ThemedMaxImages {
		m.Lines = append(m.Lines, locales.ThemedReadingPlannedImages(*n))
	}
	if u := task.Usage; u != nil && u.SearchCalls != nil {
		pages := 0
		if u.PagesRead != nil {
			pages = *u.PagesRead
		}
		m.Lines = append(m.Lines, locales.ThemedReadingReaderResearchUsage(*u.SearchCalls, pages))
	}
	if task.FirstContentAt != 0 {
		m.Lines = append(m.Lines, locales.ThemedReadingReaderFirstContent(seconds(task.FirstContentAt-task.CreatedAt)))
	}
	if r := task.Reused; r != nil && (r.Chapters > 0 || r.Notes > 0 || r.References > 0 || r.Assets > 0) {
		m.Lines = append(m.Lines, locales.ThemedReadingReaderReused(r.Chapters, r.Notes, r.References, r.Assets))
	}
	if len(task.Timings) > 0 {
		m.Lines = append(m.Lines, timingLine(task.Timings))
	}
	if u := task.Usage; u != nil {
		m.Lines = append(m.Lines, strings.Join([]string{
			locales.ThemedReadingTextCalls(u.TextCalls),
			locales.ThemedReadingImageCalls(u.ImageCalls),
			locales.ThemedReadingImagesSaved(u.ImagesSaved),
		}, "   "))
	} else {
		m.Lines = append(m.Lines, locales.ThemedReadingUsageMissing())
	}
	return m
}

// timingLine 把各阶段耗时连成一行：按阶段先后排，未知阶段排在最后按名称排。
func timingLine(timings map[string]int64) string {
	rank := func(stage string) int {
		for i, st := range stepOrder {
			if st == stage {
				return i
			}
		}
		return len(stepOrder)
	}
	keys := make([]string, 0, len(timings))
	for k := range timings {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if ri, rj := rank(keys[i]), rank(keys[j]); ri != rj {
			return ri < rj
		}
		return keys[i] < keys[j]
	})
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, locales.ThemedReadingReaderStageTiming(StageLabel(k), seconds(timings[k])))
	}
	return strings.Join(parts, " · ")
}
