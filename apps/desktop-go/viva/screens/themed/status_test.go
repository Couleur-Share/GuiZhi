package themed

import (
	"strings"
	"testing"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

func stepStages(m StatusModel) (out []string) {
	for _, s := range m.Steps {
		out = append(out, s.Stage)
	}
	return out
}

func TestStatusQueuedAndRunning(t *testing.T) {
	q := BuildStatus(services.ThemedTask{State: services.ThemedTaskQueued, Stage: services.ThemedStagePrepare}, StatusOptions{Expanded: true})
	if !q.Active || q.Title != locales.ThemedReadingReaderQueued() || q.StageCount != "" || !q.ShowStop || q.ShowResume {
		t.Fatalf("排队中: %+v", q)
	}
	for _, s := range q.Steps {
		if s.Done || s.Current {
			t.Fatalf("排队中不应标记任何步骤: %+v", s)
		}
	}
	run := BuildStatus(services.ThemedTask{State: services.ThemedTaskRunning, Stage: services.ThemedStageWrite, Completed: 2, Total: 5}, StatusOptions{Expanded: true})
	if !strings.HasSuffix(run.Title, " · 2/5") || !strings.HasPrefix(run.Title, locales.ThemedReadingStageWrite()) {
		t.Fatalf("标题带阶段与进度: %q", run.Title)
	}
	cur := -1
	for i, s := range run.Steps {
		if s.Current {
			cur = i
		}
		if s.Done && i >= 3 {
			t.Fatalf("后面的步骤不能算完成: %+v", run.Steps)
		}
	}
	if cur != 3 || run.StageCount != locales.ThemedReadingReaderStageCount(4, 8) || run.Headline == "" {
		t.Fatalf("写作是第 4 步: cur=%d count=%q", cur, run.StageCount)
	}
	compact := BuildStatus(services.ThemedTask{State: services.ThemedTaskRunning, Stage: services.ThemedStageWrite}, StatusOptions{HasPage: true})
	if compact.StageCount != "" || compact.Headline != "" || compact.Context != locales.ThemedReadingReaderUpdating() {
		t.Fatalf("紧凑样式没有阶段计数和小标题，已有页面时说明仍可阅读: %+v", compact)
	}
}

func TestStatusDesignProgressAndRepair(t *testing.T) {
	m := BuildStatus(services.ThemedTask{State: services.ThemedTaskRunning, Stage: services.ThemedStageDesign,
		DesignProgress: &services.ThemedDesignProgress{Attempt: 1, ReceivedChars: 1200}}, StatusOptions{})
	if m.Detail != locales.ThemedReadingDesignReceiving(1200) {
		t.Fatalf("%q", m.Detail)
	}
	m = BuildStatus(services.ThemedTask{State: services.ThemedTaskRunning, Stage: services.ThemedStageDesign,
		DesignProgress: &services.ThemedDesignProgress{Attempt: 2}}, StatusOptions{})
	if m.Detail != locales.ThemedReadingDesignThinking() || m.Title != locales.ThemedReadingDesignRepairing() {
		t.Fatalf("第二次尝试显示「调整页面设计」: %q / %q", m.Title, m.Detail)
	}
	m = BuildStatus(services.ThemedTask{State: services.ThemedTaskRunning, Stage: services.ThemedStageAssemble, Total: 3, Completed: 1}, StatusOptions{})
	if !strings.HasPrefix(m.Title, locales.ThemedReadingCompilingGraphics()) {
		t.Fatalf("组装阶段有分项时显示「处理图形」: %q", m.Title)
	}
	done := BuildStatus(services.ThemedTask{State: services.ThemedTaskFailed, Stage: services.ThemedStageDesign,
		DesignProgress: &services.ThemedDesignProgress{Attempt: 1, ReceivedChars: 5}}, StatusOptions{})
	if done.Detail != "" {
		t.Fatal("非活动任务不显示实时输出进度")
	}
}

func TestStatusStepsHideImagesWhenNoneArePlanned(t *testing.T) {
	zero := 0
	m := BuildStatus(services.ThemedTask{State: services.ThemedTaskRunning, Stage: services.ThemedStageDesign, PlannedImages: &zero}, StatusOptions{})
	if len(m.Steps) != 7 {
		t.Fatalf("不生图时少「图片」一步: %v", stepStages(m))
	}
	m = BuildStatus(services.ThemedTask{State: services.ThemedTaskRunning, Stage: services.ThemedStageImages, PlannedImages: &zero}, StatusOptions{})
	if len(m.Steps) != 8 {
		t.Fatalf("正处于图片阶段时保留: %v", stepStages(m))
	}
	three := 3
	m = BuildStatus(services.ThemedTask{State: services.ThemedTaskRunning, Stage: services.ThemedStageDesign, PlannedImages: &three}, StatusOptions{})
	if len(m.Steps) != 8 || m.Lines[0] != locales.ThemedReadingPlannedImages(3) {
		t.Fatalf("计划图片数显示在详情里: %v %v", stepStages(m), m.Lines)
	}
	bad := 9
	m = BuildStatus(services.ThemedTask{State: services.ThemedTaskCompleted, Stage: services.ThemedStageDone, PlannedImages: &bad}, StatusOptions{})
	for _, l := range m.Lines {
		if l == locales.ThemedReadingPlannedImages(9) {
			t.Fatal("超出范围的计划数不显示")
		}
	}
}

func TestStatusResearchPausedAndButtons(t *testing.T) {
	task := services.ThemedTask{State: services.ThemedTaskPartial, Stage: services.ThemedStageResearch, Error: "search quota exceeded"}
	m := BuildStatus(task, StatusOptions{OfflineAvailable: true})
	if m.Title != locales.ThemedReadingReaderResearchPaused() || !m.ShowResume || m.ResumeLabel != locales.ThemedReadingReaderRetryResearch() || !m.ShowOffline || m.ShowStop {
		t.Fatalf("查证暂停: %+v", m)
	}
	if m.Error != "search quota exceeded" {
		t.Fatal("失败原因原文可复制")
	}
	if BuildStatus(task, StatusOptions{}).ShowOffline {
		t.Fatal("调用方不支持时没有「不联网继续」")
	}
	ro := BuildStatus(task, StatusOptions{ReadOnly: true, OfflineAvailable: true})
	if ro.ShowResume || ro.ShowStop || ro.ShowOffline {
		t.Fatalf("只读时没有任何操作按钮: %+v", ro)
	}
	plain := BuildStatus(services.ThemedTask{State: services.ThemedTaskFailed, Stage: services.ThemedStageDesign}, StatusOptions{})
	if plain.ResumeLabel != locales.ThemedReadingReaderResume() || plain.Title != locales.ThemedReadingReaderStateFailed() {
		t.Fatalf("%+v", plain)
	}
}

func TestStatusContextText(t *testing.T) {
	cases := []struct {
		name string
		task services.ThemedTask
		opts StatusOptions
		want string
	}{
		{"已完成无说明", services.ThemedTask{State: services.ThemedTaskCompleted}, StatusOptions{HasPage: true}, ""},
		{"旧格式任务", services.ThemedTask{State: services.ThemedTaskFailed, LegacyFormat: true}, StatusOptions{}, locales.ThemedReadingReaderLegacyTask()},
		{"失败但有页面", services.ThemedTask{State: services.ThemedTaskFailed}, StatusOptions{HasPage: true}, locales.ThemedReadingReaderPreviousAvailable()},
		{"失败无页面", services.ThemedTask{State: services.ThemedTaskFailed}, StatusOptions{}, locales.ThemedReadingReaderSourceSafe()},
	}
	for _, c := range cases {
		if got := BuildStatus(c.task, c.opts).Context; got != c.want {
			t.Errorf("%s: %q，期望 %q", c.name, got, c.want)
		}
	}
	legacy := BuildStatus(services.ThemedTask{State: services.ThemedTaskFailed, LegacyFormat: true}, StatusOptions{})
	if legacy.ShowResume {
		t.Fatal("旧格式任务不能继续")
	}
}

func TestStatusDetailLines(t *testing.T) {
	search, pages := 4, 7
	task := services.ThemedTask{
		State: services.ThemedTaskCompleted, Stage: services.ThemedStageDone, CreatedAt: 1000, FirstContentAt: 3500,
		Usage:  &services.ThemedUsage{TextCalls: 3, ImageCalls: 1, ImagesSaved: 1, SearchCalls: &search, PagesRead: &pages},
		Reused: &services.ThemedReused{Chapters: 2, Assets: 1},
		Timings: map[string]int64{
			services.ThemedStageDesign: 12_300, services.ThemedStagePrepare: 500, "custom": 1_000, services.ThemedStageWrite: 2_000,
		},
	}
	m := BuildStatus(task, StatusOptions{})
	want := []string{
		locales.ThemedReadingReaderResearchUsage(4, 7),
		locales.ThemedReadingReaderFirstContent("2.5"),
		locales.ThemedReadingReaderReused(2, 0, 0, 1),
		locales.ThemedReadingReaderStageTiming(locales.ThemedReadingStagePrepare(), "0.5") + " · " +
			locales.ThemedReadingReaderStageTiming(locales.ThemedReadingStageWrite(), "2.0") + " · " +
			locales.ThemedReadingReaderStageTiming(locales.ThemedReadingStageDesign(), "12.3") + " · " +
			locales.ThemedReadingReaderStageTiming("custom", "1.0"),
	}
	if len(m.Lines) != 4 {
		t.Fatalf("应有 4 行（用量另放在 Usage 里）: %q", m.Lines)
	}
	for i, w := range want {
		if m.Lines[i] != w {
			t.Errorf("第 %d 行: %q，期望 %q", i, m.Lines[i], w)
		}
	}
	if len(m.Usage) != 3 || !strings.Contains(m.Usage[0], locales.ThemedReadingTextCalls(3)) || !strings.Contains(m.Usage[2], locales.ThemedReadingImagesSaved(1)) {
		t.Fatalf("用量（文本请求 / 生图请求 / 新图保存）: %q", m.Usage)
	}
	if m.BackgroundHint != "" || m.Active {
		t.Fatal("非活动任务没有「可以切换文章」的提示")
	}
	// 没有用量记录
	none := BuildStatus(services.ThemedTask{State: services.ThemedTaskCancelled}, StatusOptions{})
	if last := none.Lines[len(none.Lines)-1]; last != locales.ThemedReadingUsageMissing() {
		t.Fatalf("旧任务没有用量: %q", last)
	}
	// 没有复用 / 联网用量：对应行不出现
	for _, l := range none.Lines {
		if strings.Contains(l, locales.ThemedReadingReaderReused(0, 0, 0, 0)) {
			t.Fatal("没有复用不显示复用行")
		}
	}
	if act := BuildStatus(services.ThemedTask{State: services.ThemedTaskRunning, Stage: services.ThemedStageDesign}, StatusOptions{}); act.BackgroundHint == "" {
		t.Fatal("活动任务提示可以切换文章")
	}
}
