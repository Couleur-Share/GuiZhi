package imports

import (
	"regexp"
	"slices"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件对应渲染层 tests/unit/components/import-task-row.test.tsx：任务行的呈现与行内动作。
// 渲染层那边只渲染一行；这里整个工作区里只放一个任务，断言的文字与按钮名与原用例逐条一致。

// elapsedSummary 匹配行上的耗时摘要「共 10:19」（页头的「共 N 条」不带冒号，不算）。
var elapsedSummary = regexp.MustCompile(`^共 \d+(:\d\d)+$`)

func (r *rig) hasElapsedSummary() bool {
	for _, text := range r.session.Texts() {
		if elapsedSummary.MatchString(text) {
			return true
		}
	}
	return false
}

// one 打开只有一个任务的工作区（读取已完成）。
func one(t *testing.T, task services.ImportTask, mutate ...func(*rig)) *rig {
	t.Helper()
	return loaded(t, pageOf(task), mutate...)
}

func completed(patch func(*services.ImportTask)) services.ImportTask {
	return mkTask("task-1", func(x *services.ImportTask) {
		x.Status, x.Stage, x.ResultItemID = services.ImportStatusCompleted, "", "item-9"
		if patch != nil {
			patch(x)
		}
	})
}

func failedTask(patch func(*services.ImportTask)) services.ImportTask {
	return mkTask("task-1", func(x *services.ImportTask) {
		x.Status, x.Stage = services.ImportStatusFailed, ""
		if patch != nil {
			patch(x)
		}
	})
}

func TestRowMobileOriginCoexistsWithItemType(t *testing.T) {
	r := one(t, mkTask("task-1", func(x *services.ImportTask) { x.Origin = OriginMobile }))
	for _, want := range []string{locales.ImportsOriginMobileLabel(), "视频"} {
		if !r.hasText(want) {
			t.Errorf("缺少文字 %q", want)
		}
	}
	if r.hasText(locales.ImportsOriginDesktopLabel()) {
		t.Error("手机提交的任务不应同时标成桌面提交")
	}
}

func TestRowProcessingShowsStageHostAndElapsed(t *testing.T) {
	r := one(t, mkTask("task-1", nil))
	// 10 分钟：转写这类长任务只有一个转圈的话，用户无从判断是不是卡死了
	for _, want := range []string{"语音转写", "bilibili.com", "视频", "已用 10:00"} {
		if !r.hasText(want) {
			t.Errorf("缺少文字 %q；画面上有 %q", want, r.session.Texts())
		}
	}
}

func TestRowStageElapsedOnlyAfterStallThreshold(t *testing.T) {
	stalled := one(t, mkTask("task-1", func(x *services.ImportTask) { x.UpdatedAt = testNow - StallThresholdMs - 30_000 }))
	if !stalled.hasText("本阶段 2:00") {
		t.Errorf("单个阶段迟迟没动静时应补一条本阶段耗时；画面上有 %q", stalled.session.Texts())
	}
	calm := one(t, mkTask("task-1", func(x *services.ImportTask) { x.UpdatedAt = testNow - 5_000 }))
	if calm.hasTextLike("本阶段") {
		t.Error("阶段推进中不打扰：未超阈值不应显示本阶段耗时")
	}
}

// 排队中报的是等待而不是「已用」：并发只有 2，后面的任务能等一个钟头，此时它一个字节都没抓。
func TestRowPendingReportsWaitingNotElapsed(t *testing.T) {
	r := one(t, mkTask("task-1", func(x *services.ImportTask) {
		x.Status, x.Stage = services.ImportStatusPending, ""
		x.CreatedAt, x.UpdatedAt = testNow-3_600_000, testNow-3_600_000
	}))
	if !r.hasText("已排队 1:00:00") {
		t.Errorf("应写已排队 1:00:00；画面上有 %q", r.session.Texts())
	}
	if r.hasTextLike("已用") {
		t.Error("排队中的任务不应写「已用」")
	}
}

// 处理中的「已用」不含排队等待，与完成后的「共 X」同一口径：入队一小时前，真正开跑只有 2 分 10 秒。
func TestRowElapsedExcludesQueueWait(t *testing.T) {
	r := one(t, mkTask("task-1", func(x *services.ImportTask) {
		x.CreatedAt, x.UpdatedAt = testNow-3_600_000, testNow-10_000
		x.StageStats = []services.ImportStageStat{
			{Stage: "fetching", Ms: 5_000}, {Stage: "video-metadata", Ms: 115_000}, {Stage: "transcribing", Ms: 0},
		}
	}))
	if !r.hasText("已用 2:10") {
		t.Errorf("已结算 2:00 + 当前阶段 10 秒应写已用 2:10；画面上有 %q", r.session.Texts())
	}
}

func TestRowCompletedTitleOpensItem(t *testing.T) {
	r := one(t, completed(nil))
	r.click(gfx.RoleButton, "手写一个混合检索 RAG 系统")
	if len(r.opened) != 1 || r.opened[0] != "item-9" {
		t.Errorf("点标题应打开条目 item-9，实际 %v", r.opened)
	}
}

// 入库但缺了文字稿：徽标不能是绿色的「已完成」，原因要写在行里。
func TestRowCompletedWithWarningIsNotPlainCompleted(t *testing.T) {
	r := one(t, completed(func(x *services.ImportTask) { x.Warning = "文字稿生成失败：本地转写服务启动失败" }))
	if !r.hasText(locales.ImportsStatusDegraded()) {
		t.Errorf("应写「%s」；画面上有 %q", locales.ImportsStatusDegraded(), r.session.Texts())
	}
	// 「已完成」在侧栏的筛选行里本来就有一处；行上的徽标若还写它，就是两处。
	if got := r.countText(locales.ImportsStatusCompleted()); got != 1 {
		t.Errorf("带缺失的完成不能再写成「已完成」：该文字出现 %d 处，期望只有侧栏那 1 处", got)
	}
	if plain := one(t, completed(nil)); plain.countText(locales.ImportsStatusCompleted()) != 2 {
		t.Errorf("没有缺失的完成应在行上写「已完成」：该文字出现 %d 处，期望侧栏加行上共 2 处", plain.countText(locales.ImportsStatusCompleted()))
	}
	if !r.hasText("文字稿生成失败：本地转写服务启动失败") {
		t.Error("缺失的原因应当写在行里")
	}
}

func TestRowDuplicateOffersOpenExistingAndCreateCopy(t *testing.T) {
	r := one(t, mkTask("task-1", func(x *services.ImportTask) {
		x.Status, x.Stage, x.DuplicateItemID = services.ImportStatusDuplicate, "", "item-3"
	}))
	for _, name := range []string{locales.ImportsOpenExisting(), locales.ImportsCreateCopy()} {
		if !r.has(gfx.RoleButton, name) {
			t.Errorf("重复内容应给出出口「%s」", name)
		}
	}
	r.click(gfx.RoleButton, locales.ImportsOpenExisting())
	if len(r.opened) != 1 || r.opened[0] != "item-3" {
		t.Errorf("打开已有条目应打开 item-3，实际 %v", r.opened)
	}
	r.click(gfx.RoleButton, locales.ImportsCreateCopy())
	if got := r.calls(); !slices.Equal(got, []string{"retry:task-1"}) || !r.svc.RetryOptions().ForceDuplicate {
		t.Errorf("仍要创建副本应以 ForceDuplicate 重试：调用 %v，选项 %+v", got, r.svc.RetryOptions())
	}
}

func TestRowToolMissingOffersSettingsEntry(t *testing.T) {
	r := one(t, failedTask(func(x *services.ImportTask) {
		x.Error = "检测到B站视频链接，但尚未安装 yt-dlp，无法解析视频信息。"
	}))
	if !r.has(gfx.RoleButton, locales.ImportsRetry()) {
		t.Error("外部工具缺失的失败仍应能重试")
	}
	if !r.has(gfx.RoleButton, locales.ImportsGoToCaptureSettings()) {
		t.Fatal("外部工具缺失的失败应给出前往设置的入口")
	}
	r.click(gfx.RoleButton, locales.ImportsGoToCaptureSettings())
	if len(r.sections) != 1 || r.sections[0] != "general" {
		t.Errorf("应打开设置的 general 节，实际 %v", r.sections)
	}
}

func TestRowPlainFailureOnlyOffersRetry(t *testing.T) {
	r := one(t, failedTask(func(x *services.ImportTask) { x.Error = "网页抓取失败：HTTP 522" }))
	if !r.has(gfx.RoleButton, locales.ImportsRetry()) {
		t.Error("普通失败应能重试")
	}
	if r.has(gfx.RoleButton, locales.ImportsGoToCaptureSettings()) {
		t.Error("普通失败不应误导用户去装工具")
	}
	r.click(gfx.RoleButton, locales.ImportsRetry())
	if got := r.calls(); !slices.Equal(got, []string{"retry:task-1"}) {
		t.Errorf("点重试应调用服务重试，实际 %v", got)
	}
}

// 只有匿名平台解析失败才显示登录态重试；作品已删除与已用登录态的任务都不显示。
func TestRowAuthenticatedRetryOnlyForAnonymousParseFailure(t *testing.T) {
	anonymous := failedTask(func(x *services.ImportTask) {
		x.SourceInput, x.Error = "https://www.xiaohongshu.com/explore/abc", "[structure_missing] 笔记页未返回数据"
	})
	r := one(t, anonymous)
	if !r.has(gfx.RoleButton, locales.ImportsAuthenticatedRetry()) {
		t.Fatal("匿名平台解析失败应给出登录态重试")
	}
	r.click(gfx.RoleButton, locales.ImportsAuthenticatedRetry())
	if want := "task-1:" + AuthenticatedRetryPlatform(anonymous); len(r.authRetried) != 1 || r.authRetried[0] != want {
		t.Errorf("登录态重试应交给外壳：期望 [%s]，实际 %v", want, r.authRetried)
	}

	for name, task := range map[string]services.ImportTask{
		"作品已删除": failedTask(func(x *services.ImportTask) {
			x.SourceInput, x.Error = "https://www.douyin.com/video/123", "[note_unavailable] 作品已删除"
		}),
		"已用登录态": failedTask(func(x *services.ImportTask) {
			x.SourceInput, x.Error = "https://www.douyin.com/video/123", "[structure_missing] 页面变化"
			x.CaptureStrategy = "authenticated"
		}),
	} {
		if one(t, task).has(gfx.RoleButton, locales.ImportsAuthenticatedRetry()) {
			t.Errorf("%s不应显示登录态重试", name)
		}
	}
}

func TestRowWebVerificationRetryOpensDetailInsteadOfRetrying(t *testing.T) {
	task := failedTask(func(x *services.ImportTask) {
		x.SourceInput, x.Error = "https://example.com/post", "web_verification_required: 需要网页验证"
	})
	r := one(t, task)
	name := locales.ImportsWebVerifyRetry()
	if !r.has(gfx.RoleButton, name) {
		t.Fatalf("需要网页验证的失败应写「%s」", name)
	}
	r.click(gfx.RoleButton, name)
	if r.screen.detailTaskID != "task-1" {
		t.Errorf("网页验证的重试应打开详情，detailTaskID=%q", r.screen.detailTaskID)
	}
	if len(r.calls()) != 0 {
		t.Errorf("打开详情前不应调用服务：%v", r.calls())
	}
	// 开启了自动求解后，按钮改写为「自动验证并采集」。
	solver := one(t, task, func(r *rig) { r.solver = true })
	if !solver.has(gfx.RoleButton, locales.ImportsWebSolverRetry()) {
		t.Errorf("开启自动求解后应写「%s」", locales.ImportsWebSolverRetry())
	}
}

// ---- 终态任务的阶段耗时 ----

// finished 是带阶段统计的已完成任务：转写 2:06，排版 8:13（7 次调用、2 次失败）。
func finished(patch func(*services.ImportTask)) services.ImportTask {
	return completed(func(x *services.ImportTask) {
		x.StageStats = []services.ImportStageStat{
			{Stage: "transcribing", Ms: 126_000},
			{Stage: "formatting", Ms: 493_000, Calls: 7, FailedCalls: 2, PromptTokens: 9_500, CompletionTokens: 57_900, Models: []string{"qwen3.5-flash"}},
		}
		if patch != nil {
			patch(x)
		}
	})
}

func TestRowStageSummaryShowsTotalAndSlowest(t *testing.T) {
	r := one(t, finished(nil))
	for _, want := range []string{"共 10:19", "· 最慢 文字稿排版 8:13"} {
		if !r.hasText(want) {
			t.Errorf("行上应常驻 %q；画面上有 %q", want, r.session.Texts())
		}
	}
	if r.hasText("2:06") {
		t.Error("明细不摊在行上：一屏几十条会被撑成一片")
	}
}

func TestRowStageSummaryOpensDetail(t *testing.T) {
	r := one(t, finished(nil))
	r.click(gfx.RoleButton, "共 10:19 · 最慢 文字稿排版 8:13")
	if r.screen.detailTaskID != "task-1" {
		t.Errorf("点摘要应打开详情，detailTaskID=%q", r.screen.detailTaskID)
	}
}

// 失败任务没有耗时摘要可点，而它恰恰是最需要看清楚的那种，所以详情入口常驻在动作条上。
func TestRowFailedTaskKeepsDetailEntry(t *testing.T) {
	r := one(t, finished(func(x *services.ImportTask) {
		x.Status, x.StageStats, x.Error = services.ImportStatusFailed, nil, "HTTP 522"
	}))
	if r.hasElapsedSummary() {
		t.Error("失败任务没有统计时不应显示耗时摘要")
	}
	r.click(gfx.RoleButton, locales.ImportsTaskDetail())
	if r.screen.detailTaskID != "task-1" {
		t.Errorf("详情入口应打开详情，detailTaskID=%q", r.screen.detailTaskID)
	}
}

func TestRowSingleStageDoesNotSaySlowest(t *testing.T) {
	r := one(t, finished(func(x *services.ImportTask) {
		x.StageStats = []services.ImportStageStat{{Stage: "fetching", Ms: 3_000}}
	}))
	if !r.hasText("共 0:03") {
		t.Errorf("只有一个阶段也应写总耗时；画面上有 %q", r.session.Texts())
	}
	if r.hasTextLike("最慢") {
		t.Error("只有一个阶段时不说「最慢」：那等于把总耗时念第二遍")
	}
}

// 运行中的任务不显示耗时摘要：那时该回答的是「还活着吗」，不是「花在哪了」。
func TestRowRunningTaskHasNoSummaryButElapsed(t *testing.T) {
	r := one(t, finished(func(x *services.ImportTask) {
		x.Status, x.Stage, x.ResultItemID = services.ImportStatusProcessing, "formatting", ""
	}))
	if r.hasElapsedSummary() {
		t.Error("运行中的任务不应显示耗时摘要")
	}
	// 已结算 2:06 + 8:13 再加当前阶段的 10 秒；createdAt 在 10 分钟前，但那是入队时刻，与「这条跑了多久」无关
	if !r.hasText("已用 10:29") {
		t.Errorf("应写已用 10:29；画面上有 %q", r.session.Texts())
	}
}

func TestRowWithoutStatsHasNoSummaryShell(t *testing.T) {
	if one(t, finished(func(x *services.ImportTask) { x.StageStats = nil })).hasElapsedSummary() {
		t.Error("老任务没有统计时整块不出现，不摆一个空壳")
	}
}
