package imports

import (
	"strings"
	"testing"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件逐条对应渲染层 tests/unit/import/import-task-meta.test.ts 与 import-task-row.test.tsx 里的纯逻辑用例。

func makeTask(patch func(*services.ImportTask)) services.ImportTask {
	t := services.ImportTask{
		ID:          "task-1",
		SourceKind:  "url",
		SourceInput: "https://example.com/a",
		DisplayName: "示例条目",
		Status:      services.ImportStatusCompleted,
	}
	if patch != nil {
		patch(&t)
	}
	return t
}

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		name string
		ms   int64
		want string
	}{
		{"零", 0, "0:00"},
		{"七秒", 7_000, "0:07"},
		{"三分零七秒", 187_000, "3:07"},
		{"一小时", 3_600_000, "1:00:00"},
		{"一小时十二分零五秒", 4_325_000, "1:12:05"},
		{"负数按 0 处理（时钟偏移不该显示成 -1:59）", -5_000, "0:00"},
	}
	for _, c := range cases {
		if got := FormatDuration(c.ms); got != c.want {
			t.Errorf("%s：FormatDuration(%d)=%q，期望 %q", c.name, c.ms, got, c.want)
		}
	}
}

func TestResolveTaskHostAndFolder(t *testing.T) {
	host := func(task services.ImportTask) (string, bool) { return ResolveTaskHost(task) }
	if got, ok := host(makeTask(func(x *services.ImportTask) { x.SourceInput = "https://www.v2ex.com/t/1" })); !ok || got != "v2ex.com" {
		t.Errorf("URL 任务取域名并去掉 www.：%q %v", got, ok)
	}
	if got, ok := host(makeTask(func(x *services.ImportTask) { x.SourceInput = "https://www.iesdouyin.com/share/video/1/" })); !ok || got != "iesdouyin.com" {
		t.Errorf("iesdouyin：%q %v", got, ok)
	}
	if _, ok := host(makeTask(func(x *services.ImportTask) { x.SourceKind, x.SourceInput = "text", "一段笔记" })); ok {
		t.Error("文本任务不应给域名")
	}
	if _, ok := host(makeTask(func(x *services.ImportTask) { x.SourceInput = "not a url" })); ok {
		t.Error("非法链接不应给域名")
	}

	file := makeTask(func(x *services.ImportTask) { x.SourceKind, x.SourceInput = "file", `D:\docs\读书笔记.md` })
	if got, ok := ResolveTaskFolder(file); !ok || got != "D:/docs" {
		t.Errorf("文件任务给出所在目录：%q %v", got, ok)
	}
	if _, ok := ResolveTaskFolder(makeTask(nil)); ok {
		t.Error("URL 任务没有目录")
	}
	// 只有文件名、没有目录：split 之后弹出末段为空，回落到 null
	if _, ok := ResolveTaskFolder(makeTask(func(x *services.ImportTask) { x.SourceKind, x.SourceInput = "file", "a.md" })); ok {
		t.Error("只有文件名时没有目录")
	}
	if got, ok := ResolveTaskFolder(makeTask(func(x *services.ImportTask) { x.SourceKind, x.SourceInput = "file", "/a/b.md" })); !ok || got != "/a" {
		t.Errorf("以分隔符开头的绝对路径应保留前导斜杠：%q %v", got, ok)
	}
}

func TestNeedsCaptureToolSetup(t *testing.T) {
	if !NeedsCaptureToolSetup("检测到B站视频链接，但尚未安装 yt-dlp，无法解析视频信息。") {
		t.Error("应认得出外部工具缺失")
	}
	if NeedsCaptureToolSetup("HTTP 522：源站没有响应") {
		t.Error("普通抓取失败不给安装入口")
	}
	if NeedsCaptureToolSetup("yt-dlp 退出码 1: 视频不存在") {
		t.Error("工具已安装但执行失败不给安装入口")
	}
	if NeedsCaptureToolSetup("") {
		t.Error("空原因不给安装入口")
	}
}

func TestFormatTaskError(t *testing.T) {
	got := FormatTaskError("[structure_missing] 分享页未返回作品数据（抖音可能已改版）")
	if want := "平台页面结构可能已变更：分享页未返回作品数据（抖音可能已改版）"; got != want {
		t.Errorf("把 [structure_missing] 翻成可读标签，正文保留：\n got %q\nwant %q", got, want)
	}
	if got := FormatTaskError("HTTP 522：源站没有响应"); got != "HTTP 522：源站没有响应" {
		t.Errorf("没有稳定码时原样返回：%q", got)
	}
	report := FormatTaskErrorForReport("[token_invalid] 小红书拒绝了该链接")
	if want := "链接缺少访问令牌，请用分享面板复制完整链接：小红书拒绝了该链接\n错误码：token_invalid"; report != want {
		t.Errorf("诊断文本额外带上错误码一行：\n got %q\nwant %q", report, want)
	}
	// 只有标签、没有正文：直接给标签；正文以标签开头：不重复拼接
	// 「[network]」没有正文时 body 回落为原文（等于 error），只给标签
	if got := FormatTaskError("[network]"); got != locales.ImportsParseErrorNetwork() {
		t.Errorf("无正文的稳定码应只给标签：%q", got)
	}
	if got := FormatTaskError("[network] 网络或站点暂时不可达：连接超时"); got != "网络或站点暂时不可达：连接超时" {
		t.Errorf("正文已带标签时不重复：%q", got)
	}
	if got := FormatTaskError("web_verification_required: 验证被取消"); got != "验证被取消" {
		t.Errorf("网页验证的具体失败原因应去掉内部前缀后原样给出：%q", got)
	}
	if got := FormatTaskError("web_verification_required"); got != locales.ImportsWebVerificationRequired() {
		t.Errorf("网页验证待办：%q", got)
	}
	if FormatTaskError("") != "" {
		t.Error("空错误返回空串")
	}
}

func TestFormatTaskWarning(t *testing.T) {
	if got := FormatTaskWarning("文字稿生成失败：HTTP 403"); !strings.Contains(got, "平台拒绝访问音频（HTTP 403）") {
		t.Errorf("旧任务里的文字稿 HTTP 403 应翻成可行动的说明：%q", got)
	}
	const unknown = "文字稿生成失败：本地转写服务启动失败"
	if got := FormatTaskWarning(unknown); got != unknown {
		t.Errorf("未知的降级原因仍保留原文：%q", got)
	}
	if got := FormatTaskWarning("别的警告 HTTP 403"); got != "别的警告 HTTP 403" {
		t.Errorf("非文字稿失败的警告原样返回：%q", got)
	}
}

func TestAuthenticatedRetryPlatform(t *testing.T) {
	failed := func(url, errText string, patch func(*services.ImportTask)) services.ImportTask {
		return makeTask(func(x *services.ImportTask) {
			x.SourceInput, x.Status, x.Error = url, services.ImportStatusFailed, errText
			if patch != nil {
				patch(x)
			}
		})
	}
	if got := AuthenticatedRetryPlatform(failed("https://www.xiaohongshu.com/explore/abc", "[structure_missing] 笔记页未返回数据", nil)); got != PlatformXiaohongshu {
		t.Errorf("匿名平台解析失败应给登录态重试：%q", got)
	}
	if got := AuthenticatedRetryPlatform(failed("https://www.douyin.com/video/123", "[note_unavailable] 作品已删除", nil)); got != "" {
		t.Errorf("作品删除不显示登录态重试：%q", got)
	}
	authenticated := failed("https://www.douyin.com/video/123", "[structure_missing] 页面变化", func(x *services.ImportTask) { x.CaptureStrategy = "authenticated" })
	if got := AuthenticatedRetryPlatform(authenticated); got != "" {
		t.Errorf("已认证任务不显示登录态重试：%q", got)
	}
	if got := AuthenticatedRetryPlatform(failed("https://www.nodeseek.com/post-1-1", "Just a moment...", nil)); got != PlatformNodeseek {
		t.Errorf("NodeSeek 的验证类失败按错误文本判断：%q", got)
	}
	if got := AuthenticatedRetryPlatform(failed("https://nodeseek.com:8443/post-1-1", "403", nil)); got != "" {
		t.Errorf("NodeSeek 带端口的链接不被信任：%q", got)
	}
}

func TestNeedsWebVerification(t *testing.T) {
	mk := func(url, errText, name string) services.ImportTask {
		return makeTask(func(x *services.ImportTask) {
			x.SourceInput, x.Status, x.Error, x.DisplayName = url, services.ImportStatusFailed, errText, name
		})
	}
	if !NeedsWebVerification(mk("https://example.com/x", "web_verification_required", "t")) {
		t.Error("错误码指向网页验证应命中")
	}
	if !NeedsWebVerification(mk("https://example.com/x", "", "Just a moment...")) {
		t.Error("标题是验证页应命中")
	}
	if NeedsWebVerification(mk("https://example.com/x", "HTTP 403", "t")) {
		t.Error("403 本身不能证明需要网页验证")
	}
	if NeedsWebVerification(mk("https://www.douyin.com/video/1", "web_verification_required", "t")) {
		t.Error("平台任务走登录态路径，不走通用网页验证")
	}
}

// 以下用例对应 import-task-row.test.tsx：阶段耗时口径。

func TestWorkElapsedExcludesQueueWait(t *testing.T) {
	const now = int64(1_800_000_000_000)
	// 入队 1 小时前，真正开跑只有 2 分 10 秒（已结算 2:00 + 当前阶段 10 秒）
	task := makeTask(func(x *services.ImportTask) {
		x.Status, x.Stage = services.ImportStatusProcessing, "transcribing"
		x.CreatedAt, x.UpdatedAt = now-3_600_000, now-10_000
		x.StageStats = []services.ImportStageStat{{Stage: "fetching", Ms: 5_000}, {Stage: "video-metadata", Ms: 115_000}, {Stage: "transcribing", Ms: 0}}
	})
	if got := FormatDuration(ResolveWorkElapsed(task, now)); got != "2:10" {
		t.Errorf("处理中的「已用」不含排队等待：%q", got)
	}
	// 没有统计的老任务退回 now - createdAt
	old := makeTask(func(x *services.ImportTask) { x.Status, x.CreatedAt = services.ImportStatusProcessing, now-600_000 })
	if got := FormatDuration(ResolveWorkElapsed(old, now)); got != "10:00" {
		t.Errorf("老任务退回入队时刻口径：%q", got)
	}
}

func TestResolveStageStatsAddsCurrentStage(t *testing.T) {
	const now = int64(1_800_000_000_000)
	// 已结算 2:06 + 8:13，再加当前阶段的 10 秒 → 10:29
	task := makeTask(func(x *services.ImportTask) {
		x.Status, x.Stage = services.ImportStatusProcessing, "formatting"
		x.CreatedAt, x.UpdatedAt = now-600_000, now-10_000
		x.StageStats = []services.ImportStageStat{{Stage: "transcribing", Ms: 126_000}, {Stage: "formatting", Ms: 493_000}}
	})
	if got := FormatDuration(ResolveWorkElapsed(task, now)); got != "10:29" {
		t.Errorf("运行中任务的已用：%q，期望 10:29", got)
	}
	stats := ResolveStageStats(task, now)
	if len(stats) != 2 || stats[1].Ms != 503_000 || task.StageStats[1].Ms != 493_000 {
		t.Errorf("当前阶段应补算且不改动原快照：%+v 原：%+v", stats, task.StageStats)
	}
	// 当前阶段不在快照里：追加一条
	fresh := makeTask(func(x *services.ImportTask) {
		x.Status, x.Stage, x.UpdatedAt = services.ImportStatusProcessing, "saving", now-4_000
		x.StageStats = []services.ImportStageStat{{Stage: "fetching", Ms: 1_000}}
	})
	if stats := ResolveStageStats(fresh, now); len(stats) != 2 || stats[1].Stage != "saving" || stats[1].Ms != 4_000 {
		t.Errorf("缺失的当前阶段应追加：%+v", stats)
	}
}

func TestStageLabelFallsBackToFetching(t *testing.T) {
	if got := StageLabel("transcribing"); got != "语音转写" {
		t.Errorf("转写阶段文案：%q", got)
	}
	if got, want := StageLabel(""), locales.ImportsStageFetching(); got != want {
		t.Errorf("空阶段回落到抓取中：%q", got)
	}
	if got, want := StageLabel("no-such-stage"), locales.ImportsStageFetching(); got != want {
		t.Errorf("未知阶段回落到抓取中：%q", got)
	}
}

func TestPlatformDetection(t *testing.T) {
	cases := []struct {
		url  string
		want string
	}{
		{"https://www.xiaohongshu.com/explore/abc", PlatformXiaohongshu},
		{"https://xhslink.com/a/b", PlatformXiaohongshu},
		{"https://v.douyin.com/abc/", PlatformDouyin},
		{"https://linux.do/t/topic/1", PlatformLinuxdo},
		{"https://www.nodeseek.com/post-1-1", PlatformNodeseek},
		{"http://www.douyin.com/video/1", ""},  // 非 https 不识别
		{"https://evildouyin.com/video/1", ""}, // 后缀相同但不是子域名
		{"https://user:pw@nodeseek.com/", ""},  // NodeSeek 带凭据不识别
		{"not a url", ""},
	}
	for _, c := range cases {
		if got := DetectPlatformCapturePlatform(c.url); got != c.want {
			t.Errorf("DetectPlatformCapturePlatform(%q)=%q，期望 %q", c.url, got, c.want)
		}
	}
}
