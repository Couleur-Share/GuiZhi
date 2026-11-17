package imports

import (
	"fmt"
	"strings"
	"testing"

	"guizhi-mygo-shell/native/services"
)

// 对应 import-task-report.test.ts 的 makeTask：一条已完成的视频任务，三个阶段。
func reportTask(patch func(*services.ImportTask)) services.ImportTask {
	task := services.ImportTask{
		ID: "task-1", SourceKind: "url", SourceInput: "https://v.douyin.com/abcdefg/", DisplayName: "API中转站暴利神话的真相",
		Status: services.ImportStatusCompleted, ItemType: "video", ResultItemID: "item-9",
		StageStats: []services.ImportStageStat{
			{Stage: "fetching", Ms: 200},
			{Stage: "transcribing", Ms: 46_000},
			{Stage: "formatting", Ms: 21_000, Calls: 4, FailedCalls: 1, PromptTokens: 3_100, CompletionTokens: 4_724, Models: []string{"qwen3.5-flash"}},
		},
		CreatedAt: 1_800_000_000_000, UpdatedAt: 1_800_000_100_000,
	}
	if patch != nil {
		patch(&task)
	}
	return task
}

// reportContext 与渲染层的 context 一致：绝对时间写成 T<毫秒>，便于断言。
func reportContext() ReportContext {
	return ReportContext{FormatTime: func(ms int64) string { return fmt.Sprintf("T%d", ms) }}
}

func mustContain(t *testing.T, report string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(report, want) {
			t.Errorf("诊断文本缺少 %q：\n%s", want, report)
		}
	}
}

func mustNotContain(t *testing.T, report string, nots ...string) {
	t.Helper()
	for _, not := range nots {
		if strings.Contains(report, not) {
			t.Errorf("诊断文本不该含 %q：\n%s", not, report)
		}
	}
}

func TestReportHeaderHasTaskStatusSourceAndTimes(t *testing.T) {
	report := BuildTaskReport(reportTask(nil), reportContext())
	mustContain(t, report,
		"# 采集任务诊断", "- 任务：API中转站暴利神话的真相", "- 状态：已完成", "- 来源：https://v.douyin.com/abcdefg/",
		"- 入队：T1800000000000", "- 任务 ID：task-1")
}

// 阶段表不折叠亚秒阶段：折叠是为了扫视，排查时少一行就是少一条线索。
func TestReportStageTableDoesNotCollapseSubSecondStages(t *testing.T) {
	report := BuildTaskReport(reportTask(nil), reportContext())
	mustContain(t, report,
		"## 阶段耗时（共 1:07）",
		"| 抓取中 | 0:00 | <1% |  |",
		"| 语音转写 | 0:46 | 68% |  |",
		"| 文字稿排版 | 0:21 | 31% | 4 次调用 · 1 次失败 · 7824 token · qwen3.5-flash |")
}

// 处理中的诊断补上当前阶段的等待，重复生成不累加到原始快照。
func TestReportProcessingAddsCurrentStageWaitWithoutMutatingTheTask(t *testing.T) {
	task := reportTask(func(task *services.ImportTask) {
		task.Status, task.Stage = services.ImportStatusProcessing, "web-preparing"
		task.StageStats = []services.ImportStageStat{{Stage: "fetching", Ms: 36}, {Stage: "web-preparing", Ms: 0}}
	})
	ctx := reportContext()
	ctx.Now = task.UpdatedAt + 381_000
	mustContain(t, BuildTaskReport(task, ctx), "## 阶段耗时（共 6:21）", "| 准备网页采集 | 6:21 | 100% |  |")
	if task.StageStats[1].Ms != 0 {
		t.Errorf("原始快照被改写：%d", task.StageStats[1].Ms)
	}
	ctx.Now = task.UpdatedAt + 382_000
	mustContain(t, BuildTaskReport(task, ctx), "共 6:22")
}

// 拿不到版本时不写那一行，而不是留一个空的「应用：」。
func TestReportOmitsAppLineWithoutVersion(t *testing.T) {
	mustNotContain(t, BuildTaskReport(reportTask(nil), reportContext()), "- 应用：")
	ctx := reportContext()
	ctx.AppVersion, ctx.Platform = "0.12.0", "win32"
	mustContain(t, BuildTaskReport(reportTask(nil), ctx), "- 应用：GuiZhi 0.12.0 · win32")
}

// 报错与缺失提示各占一节；没有就不留空标题。
func TestReportErrorAndWarningSectionsOnlyWhenPresent(t *testing.T) {
	mustNotContain(t, BuildTaskReport(reportTask(nil), reportContext()), "## 报错", "## 缺失提示")
	noisy := BuildTaskReport(reportTask(func(task *services.ImportTask) {
		task.Status, task.Error, task.Warning = services.ImportStatusFailed, "网页抓取失败：HTTP 522", "文字稿生成失败：本地转写服务启动失败"
	}), reportContext())
	mustContain(t, noisy, "- 状态：失败", "## 报错\n\n网页抓取失败：HTTP 522", "## 缺失提示\n\n文字稿生成失败：本地转写服务启动失败")
}

// 平台解析错误码翻成可读文案，并保留错误码一行供排查。
func TestReportParseErrorCodeIsTranslatedAndKept(t *testing.T) {
	report := BuildTaskReport(reportTask(func(task *services.ImportTask) {
		task.Status, task.Error = services.ImportStatusFailed, "[structure_missing] 分享页未返回作品数据（抖音可能已改版）"
	}), reportContext())
	mustContain(t, report, "平台页面结构可能已变更：分享页未返回作品数据（抖音可能已改版）", "错误码：structure_missing")
	mustNotContain(t, report, "[structure_missing]")
}

// 标题里的竖线要转义，否则表格被从中间切断。
func TestReportEscapesPipesInTableCells(t *testing.T) {
	report := BuildTaskReport(reportTask(func(task *services.ImportTask) {
		task.StageStats = []services.ImportStageStat{{Stage: "fetching", Ms: 1_000, Calls: 1, Models: []string{"a|b"}}}
	}), reportContext())
	mustContain(t, report, `a\|b`)
	rows := 0
	for _, line := range strings.Split(report, "\n") {
		if strings.HasPrefix(line, "| 抓取中") {
			rows++
		}
	}
	if rows != 1 {
		t.Errorf("阶段行有 %d 行，要 1 行", rows)
	}
}

// 没有阶段统计时整节不出现，不摆一张空表。
func TestReportOmitsStageSectionWithoutStats(t *testing.T) {
	report := BuildTaskReport(reportTask(func(task *services.ImportTask) {
		task.StageStats, task.Status = nil, services.ImportStatusCanceled
	}), reportContext())
	mustNotContain(t, report, "## 阶段耗时")
	mustContain(t, report, "- 状态：已取消")
}

// 登录态采集的诊断不带原文：来源只写平台，报错只留稳定错误码，缺失提示只说「存在」，不列阶段的 AI 开销。
func TestReportAuthenticatedTaskKeepsOnlySafeFields(t *testing.T) {
	report := BuildTaskReport(reportTask(func(task *services.ImportTask) {
		task.CaptureStrategy, task.SourceInput = "authenticated", "https://www.xiaohongshu.com/explore/abc?xsec_token=secret"
		task.Status, task.Error, task.Warning = services.ImportStatusFailed, "[guest_denied] 页面内容 secret", "原文 secret"
	}), reportContext())
	mustNotContain(t, report, "secret", "- 任务：", "- 入队：", "- 任务 ID：", "4 次调用")
	mustContain(t, report, "- 来源：xiaohongshu", "- 采集策略：authenticated", "## 报错\n\nguest_denied", "## 缺失提示\n\n认证采集存在非阻断 warning")
}

// 占比：四舍五入到 0 却确实花了时间的阶段写 <1%，没花时间的写 0%。
func TestFormatShare(t *testing.T) {
	cases := []struct {
		ms, total int64
		want      string
	}{{200, 67_200, "<1%"}, {0, 67_200, "0%"}, {46_000, 67_200, "68%"}, {5, 0, "<1%"}, {500, 1_000, "50%"}, {1, 1_000, "<1%"}, {5, 1_000, "1%"}}
	for _, c := range cases {
		if got := FormatShare(c.ms, c.total); got != c.want {
			t.Errorf("FormatShare(%d, %d) = %q，要 %q", c.ms, c.total, got, c.want)
		}
	}
}
