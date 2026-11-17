package imports

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 任务详情弹窗的 L4 测试：先对照渲染层 tests/unit/components/import-task-detail-modal.test.tsx 的同一批断言，
// 再补上交互与异步流程（成果卡、来源刷新对照、各种重试、复制诊断信息、任务被清掉时弹窗消失）。
// 画面上有「弹窗里」与「弹窗背后的行」两份文字，断言只选弹窗里独有的文字，或比较打开前后的出现次数。

const detailTitle = "API中转站暴利神话的真相"

// detailFixture 与渲染层测试的 makeTask 是同一份：抖音视频，抓取 0:00、转写 2:06、排版 8:13（7 次调用、2 次失败）、入库 0:00，总耗时 10:19。
func detailFixture(patch func(*services.ImportTask)) services.ImportTask {
	return mkTask("task-1", func(x *services.ImportTask) {
		x.SourceInput, x.DisplayName = "https://v.douyin.com/abcdefg/", detailTitle
		x.Status, x.Stage, x.ItemType, x.ResultItemID = services.ImportStatusCompleted, "", "video", "item-9"
		x.CreatedAt, x.UpdatedAt = 1_800_000_000_000, 1_800_000_620_000
		x.StageStats = []services.ImportStageStat{
			{Stage: "fetching", Ms: 200},
			{Stage: "transcribing", Ms: 126_000},
			{Stage: "formatting", Ms: 493_000, Calls: 7, FailedCalls: 2, PromptTokens: 9_500, CompletionTokens: 57_900, Models: []string{"qwen3.5-flash"}},
			{Stage: "saving", Ms: 100},
		}
		if patch != nil {
			patch(x)
		}
	})
}

// openedDetail 载入只有这一条任务的采集页，并点它的「任务详情」把弹窗打开。
func openedDetail(t *testing.T, task services.ImportTask, mutate ...func(*rig)) *rig {
	t.Helper()
	r := one(t, task, mutate...)
	r.click(gfx.RoleButton, locales.ImportsTaskDetail())
	if r.screen.detailTaskID != task.ID {
		t.Fatalf("点「任务详情」应打开弹窗，detailTaskID=%q", r.screen.detailTaskID)
	}
	return r
}

// dialogOpen 报告详情弹窗此刻是否在画面上（弹窗是名字等于任务标题的对话框）。
func (r *rig) dialogOpen(title string) bool { return r.has(gfx.RoleGroup, title) }

// withFacts 让服务读得到这些条目的事实（其余条目算「不存在」）。
func withFacts(items ...services.ImportItemFacts) func(*rig) {
	return func(r *rig) { r.svc.SetItemFacts(items...) }
}

func TestDetailOpensFromRowAndEscapeCloses(t *testing.T) {
	r := openedDetail(t, detailFixture(nil))
	if !r.dialogOpen(detailTitle) {
		t.Fatal("点「任务详情」后应出现详情对话框")
	}
	node, _ := r.node(gfx.RoleGroup, detailTitle)
	if node.Description != "抖音" {
		t.Errorf("对话框的可访问描述应是副标题「抖音」，实际 %q", node.Description)
	}
	r.keys("escape")
	if r.dialogOpen(detailTitle) || r.screen.detailTaskID != "" {
		t.Errorf("Esc 应关闭弹窗；detailTaskID=%q", r.screen.detailTaskID)
	}
}

func TestDetailCloseButtonCloses(t *testing.T) {
	r := openedDetail(t, detailFixture(nil))
	r.click(gfx.RoleButton, locales.CommonClose())
	if r.dialogOpen(detailTitle) {
		t.Error("点关闭钮后弹窗应关闭")
	}
}

func TestDetailMobileShowsOriginAndReceivedAt(t *testing.T) {
	const receivedAt = 1_800_001_234_567
	r := openedDetail(t, detailFixture(func(x *services.ImportTask) { x.Origin, x.ReceivedAt = OriginMobile, receivedAt }))
	for _, want := range []string{locales.ImportsOriginMobileLabel(), locales.ImportsReceivedAt(), kit.FormatDateTime(receivedAt, r.screen.loc(), r.screen.Locale)} {
		if !r.hasText(want) {
			t.Errorf("缺少文字 %q", want)
		}
	}
	// 桌面任务没有「桌面接收时间」。
	desktop := openedDetail(t, detailFixture(nil))
	if desktop.hasText(locales.ImportsReceivedAt()) {
		t.Error("桌面任务不应有「桌面接收时间」")
	}
	if !desktop.hasText(locales.ImportsOriginDesktopLabel()) {
		t.Errorf("桌面任务应写「%s」", locales.ImportsOriginDesktopLabel())
	}
}

// 副标题是品牌 logo 加平台名，不是一行域名：v.douyin.com 得先读懂才知道是抖音；完整链接仍在「来源」那一格，信息没丢。
func TestDetailSubtitleIsBrandNotHost(t *testing.T) {
	r := openedDetail(t, detailFixture(nil))
	if !r.hasText("抖音") {
		t.Error("副标题应写平台名「抖音」")
	}
	if got := r.countText("v.douyin.com"); got != 1 { // 唯一一处在弹窗背后行上的来源标记
		t.Errorf("弹窗的副标题不应是一行域名；「v.douyin.com」出现 %d 次（行上的来源标记占 1 次）", got)
	}
	if !r.hasText("https://v.douyin.com/abcdefg/") {
		t.Error("完整链接应在「来源」字段里")
	}
}

// 通用网页给主机名而不是「网页」：后者什么都没说。
func TestDetailSubtitleForGenericWebIsHost(t *testing.T) {
	r := openedDetail(t, detailFixture(func(x *services.ImportTask) { x.SourceInput = "https://blog.example.com/posts/42" }))
	if !r.hasText("blog.example.com") {
		t.Errorf("通用网页的副标题应是主机名；画面上有 %q", r.session.Texts())
	}
	if r.hasText(kit.PlatformLabel("web")) {
		t.Error("通用网页的副标题不应写「网页」")
	}
}

func TestDetailSubtitleForFileIsFolderAndPastedTextHasNone(t *testing.T) {
	file := openedDetail(t, detailFixture(func(x *services.ImportTask) {
		x.SourceKind, x.SourceInput, x.DisplayName = "file", `C:\Users\me\Documents\notes.md`, "notes.md"
	}))
	if !file.hasText(kit.PlatformLabel("local")) {
		t.Errorf("本地文件的副标题应是「%s」；画面上有 %q", kit.PlatformLabel("local"), file.session.Texts())
	}
	text := openedDetail(t, detailFixture(func(x *services.ImportTask) {
		x.SourceKind, x.SourceInput, x.DisplayName = "text", "随手记一段话", "随手记一段话"
	}))
	node, _ := text.node(gfx.RoleGroup, "随手记一段话")
	if node.Description != "" {
		t.Errorf("手工粘贴的文本没有来源可言，不应有副标题；描述 %q", node.Description)
	}
}

// 默认读数是总耗时，悬停某段就地换成那一段，而不是撑出一行或等 320 ms 的气泡。
func TestDetailReadoutIsTotalAndHoverSwapsToStage(t *testing.T) {
	r := openedDetail(t, detailFixture(nil))
	rowTotal := r.countText("共 10:19") // 行上的耗时摘要里有一处，弹窗读数再加一处
	if rowTotal != 2 {
		t.Fatalf("弹窗的读数应写「共 10:19」；该文字出现 %d 次", rowTotal)
	}
	r.hoverText(StageLabel("formatting"), 0) // 行上的摘要「最慢 …」是另一整句文字，恰好等于阶段名的只有图例那一处
	if got := r.countText("共 10:19"); got != 1 {
		t.Errorf("悬停时读数应换成那一段，「共 10:19」应只剩行上那一处；现有 %d 处", got)
	}
	if !r.hasText("8:13 \u00b7 80%") {
		t.Errorf("悬停时读数应是「耗时 · 占比」；画面上有 %q", r.session.Texts())
	}
	// 渲染层的悬停只挂在各阶段的格子与色段上，清空挂在整块容器的 mouseleave：
	// 指针移到块内的空隙或「另有 N 个」钮上读数保持；移出整块（块上方的标题在容器之外）才恢复。
	r.hoverText(locales.ImportsStageNegligible(2), 0)
	if got := r.countText("共 10:19"); got != 1 {
		t.Errorf("指针仍在阶段块内，读数应保持在那一段；「共 10:19」现有 %d 处", got)
	}
	r.hoverText(locales.ImportsReportStages(), 0)
	if got := r.countText("共 10:19"); got != 2 {
		t.Errorf("指针离开阶段块后读数应恢复为总耗时；「共 10:19」现有 %d 处", got)
	}
}

func TestDetailStageRowsShowDurationShareAndAIDetail(t *testing.T) {
	r := openedDetail(t, detailFixture(nil))
	for _, want := range []string{"2:06", "8:13", "20%", "80%"} {
		if !r.hasText(want) {
			t.Errorf("图例应有 %q；画面上有 %q", want, r.session.Texts())
		}
	}
	if !r.hasText("7 次调用 \u00b7 2 次失败 \u00b7 67400 token \u00b7 qwen3.5-flash") {
		t.Errorf("图例应写 AI 开销；画面上有 %q", r.session.Texts())
	}
}

// 亚秒阶段折成一行，点它展开全部：折叠是为了扫视时的信噪比，但排查时「让我看全部」是正当需求。
func TestDetailNegligibleStagesFoldAndExpand(t *testing.T) {
	r := openedDetail(t, detailFixture(nil))
	if r.hasText(StageLabel("fetching")) || r.hasText(StageLabel("saving")) {
		t.Fatal("不足一秒的阶段不应进图例")
	}
	fold := locales.ImportsStageNegligible(2)
	if !r.has(gfx.RoleButton, fold) {
		t.Fatalf("应有「%s」", fold)
	}
	r.click(gfx.RoleButton, fold)
	if !r.hasText(StageLabel("fetching")) || !r.hasText(StageLabel("saving")) {
		t.Errorf("点开后应列出全部阶段；画面上有 %q", r.session.Texts())
	}
	if r.hasTextLike("另有") {
		t.Error("点开后不应再有「另有 N 个」")
	}
}

// 全部阶段都不足一秒时（纯文本笔记）照原样列出：此时「另」指不到任何东西，一行不剩更像是渲染坏了。
func TestDetailAllNegligibleStagesListedAsIs(t *testing.T) {
	r := openedDetail(t, detailFixture(func(x *services.ImportTask) {
		x.StageStats = []services.ImportStageStat{{Stage: "fetching", Ms: 300}, {Stage: "saving", Ms: 200}}
	}))
	if !r.hasText(StageLabel("fetching")) || !r.hasText(StageLabel("saving")) {
		t.Errorf("全是亚秒阶段时应原样列出；画面上有 %q", r.session.Texts())
	}
	if r.hasTextLike("另有") {
		t.Error("没有可折叠的阶段时不应出现「另有 N 个」")
	}
}

// 重新打开弹窗时图例回到折叠状态（弹窗每次打开都是新的一页）。
func TestDetailReopenResetsStageLegend(t *testing.T) {
	r := openedDetail(t, detailFixture(nil))
	r.click(gfx.RoleButton, locales.ImportsStageNegligible(2))
	r.keys("escape")
	r.click(gfx.RoleButton, locales.ImportsTaskDetail())
	if !r.has(gfx.RoleButton, locales.ImportsStageNegligible(2)) {
		t.Error("重新打开后图例应回到折叠状态")
	}
}

func TestDetailWithoutStageStatsSaysSo(t *testing.T) {
	r := openedDetail(t, detailFixture(func(x *services.ImportTask) {
		x.StageStats, x.Status, x.ResultItemID = nil, services.ImportStatusCanceled, ""
	}))
	if !r.hasText(locales.ImportsNoStageStats()) {
		t.Errorf("没有阶段统计时应说明白；画面上有 %q", r.session.Texts())
	}
	if r.countText(locales.ImportsStatusCanceled()) < 2 { // 行上的徽标与弹窗里的状态字段
		t.Error("弹窗应写出状态「已取消」")
	}
}

func TestDetailWarningAndErrorBlocks(t *testing.T) {
	warn := openedDetail(t, detailFixture(func(x *services.ImportTask) { x.Warning = "文字稿生成失败：本地转写服务启动失败" }))
	if !warn.hasText(locales.ImportsReportWarning()) || !warn.hasTextLike("文字稿生成失败") {
		t.Errorf("应有缺失提示块；画面上有 %q", warn.session.Texts())
	}
	if warn.hasText(locales.ImportsReportError()) {
		t.Error("只有缺失提示时不应出现「报错」")
	}
	fail := openedDetail(t, failedTask(func(x *services.ImportTask) { x.Error = "HTTP 522" }))
	if !fail.hasText(locales.ImportsReportError()) {
		t.Errorf("失败的任务应有报错块；画面上有 %q", fail.session.Texts())
	}
}

// 需要网页验证的任务给出下一步的指引，自动求解开着与否说法不同。
func TestDetailWebVerificationErrorGivesGuidance(t *testing.T) {
	task := failedTask(func(x *services.ImportTask) {
		x.SourceInput, x.Error = "https://example.com/post", "403 Forbidden"
		x.DisplayName = "Just a moment..."
	})
	plain := openedDetail(t, task)
	if !plain.hasText(locales.ImportsWebVerificationRequired()) {
		t.Errorf("应写网页验证的指引；画面上有 %q", plain.session.Texts())
	}
	solver := openedDetail(t, task, func(r *rig) { r.solver = true })
	if !solver.hasText(locales.ImportsWebSolverRequired()) {
		t.Errorf("开启自动求解后应写求解的指引；画面上有 %q", solver.session.Texts())
	}
}

// 处理中详情随时钟推进，与复制诊断的耗时一致。
func TestDetailProcessingFollowsClockAndCopyAgrees(t *testing.T) {
	const base = 1_800_000_381_000
	task := detailFixture(func(x *services.ImportTask) {
		x.Status, x.Stage, x.ResultItemID, x.UpdatedAt = services.ImportStatusProcessing, "web-preparing", "", 1_800_000_000_000
		x.StageStats = []services.ImportStageStat{{Stage: "fetching", Ms: 36}, {Stage: "web-preparing", Ms: 0}}
	})
	r := openedDetail(t, task, func(r *rig) { r.nowMs = base })
	if !r.hasText("共 6:21") {
		t.Fatalf("读数应是「共 6:21」；画面上有 %q", r.session.Texts())
	}
	r.nowMs = base + 1000
	r.frames(2)
	if !r.hasText("共 6:22") || r.hasText("共 6:21") {
		t.Errorf("时钟前进一秒后读数应是「共 6:22」；画面上有 %q", r.session.Texts())
	}
	r.click(gfx.RoleButton, locales.ImportsCopyReport())
	if len(r.copied) != 1 || !strings.Contains(r.copied[0], "共 6:22") {
		t.Errorf("复制的诊断信息应与界面同一口径（共 6:22）；复制了 %q", r.copied)
	}
}

func TestDetailCopyReportCopiesMarkdownAndToasts(t *testing.T) {
	r := openedDetail(t, detailFixture(nil))
	r.click(gfx.RoleButton, locales.ImportsCopyReport())
	if len(r.copied) != 1 {
		t.Fatalf("应复制一次，实际 %d 次", len(r.copied))
	}
	for _, want := range []string{
		"# " + locales.ImportsReportTitle(),
		"- " + locales.ImportsReportSource() + "：https://v.douyin.com/abcdefg/",
		"| " + StageLabel("fetching") + " |", // 界面上折起来的亚秒阶段，诊断文本里必须在
		"| " + StageLabel("saving") + " |",
		"GuiZhi 0.28.7 \u00b7 win32",
	} {
		if !strings.Contains(r.copied[0], want) {
			t.Errorf("诊断文本应含 %q；实际：\n%s", want, r.copied[0])
		}
	}
	if !r.toastShown(locales.ImportsReportCopied()) {
		t.Errorf("复制成功应提示；画面上有 %q", r.session.Texts())
	}
}

func TestDetailCopyFailureShowsReasonAndCanRetry(t *testing.T) {
	r := openedDetail(t, detailFixture(nil), func(r *rig) { r.clipErr = errors.New("剪贴板被占用") })
	r.click(gfx.RoleButton, locales.ImportsCopyReport())
	if !r.toastShown(locales.ImportsReportCopyFailed()) {
		t.Fatalf("复制失败应提示；画面上有 %q", r.session.Texts())
	}
	r.click(gfx.RoleButton, locales.CommonViewDetail())
	if !r.hasTextLike("剪贴板被占用") {
		t.Errorf("失败提示应带可展开的原因；画面上有 %q", r.session.Texts())
	}
	// 失败之后按钮仍可再点（复制中的禁用状态不会卡住）。
	r.clipErr = nil
	r.click(gfx.RoleButton, locales.ImportsCopyReport())
	if len(r.copied) != 1 {
		t.Errorf("恢复后再点应能复制；复制了 %d 次", len(r.copied))
	}
}

// 完成的任务给「打开条目」，点了交给外壳并关闭弹窗。
func TestDetailOpenItemClosesDialog(t *testing.T) {
	r := openedDetail(t, detailFixture(nil))
	r.click(gfx.RoleButton, locales.ImportsOpenItem())
	if !slices.Equal(r.opened, []string{"item-9"}) {
		t.Errorf("应打开 item-9，实际 %v", r.opened)
	}
	if r.dialogOpen(detailTitle) {
		t.Error("打开条目后弹窗应关闭")
	}
}

// 取消的任务没有成果，不给「打开条目」，也没有动作区（没有可重试之外的动作时）。
func TestDetailDuplicateOpensExistingAndCanceledGetsRetryOnly(t *testing.T) {
	dup := openedDetail(t, detailFixture(func(x *services.ImportTask) {
		x.Status, x.ResultItemID, x.DuplicateItemID = services.ImportStatusDuplicate, "", "item-3"
	}))
	dup.click(gfx.RoleButton, locales.ImportsOpenExisting())
	if !slices.Equal(dup.opened, []string{"item-3"}) {
		t.Errorf("重复内容应打开已有条目 item-3，实际 %v", dup.opened)
	}

	canceled := openedDetail(t, detailFixture(func(x *services.ImportTask) { x.Status, x.ResultItemID = services.ImportStatusCanceled, "" }))
	if canceled.countText(locales.ImportsOpenItem()) != 0 {
		t.Error("取消的任务不应有「打开条目」")
	}
	if !canceled.has(gfx.RoleButton, locales.ImportsRetry()) {
		t.Error("取消的任务应能重试")
	}
}

func TestDetailCompletionCardShowsFactsAndAsks(t *testing.T) {
	facts := services.ImportItemFacts{ID: "item-9", Title: "AI 中转站调研", BodyLength: 3200, TranscriptLength: 1800, AssetCount: 4, Tags: []string{"a", "b"}}
	r := openedDetail(t, detailFixture(nil), withFacts(facts))
	for _, want := range []string{
		locales.ImportsCompletionTitle(), locales.ImportsCompletionHint(), "AI 中转站调研",
		locales.ImportsCompletionBody(3200), locales.ImportsCompletionTranscript(1800),
		locales.ImportsCompletionAssets(4), locales.ImportsCompletionTags(2),
	} {
		if !r.hasText(want) {
			t.Errorf("成果卡缺少文字 %q；画面上有 %q", want, r.session.Texts())
		}
	}
	if got := r.count(gfx.RoleButton, locales.ImportsOpenItem()); got != 2 { // 成果卡与动作区各一颗
		t.Errorf("成果卡与动作区各有一颗「打开条目」，实际 %d 颗", got)
	}
	r.click(gfx.RoleButton, locales.ImportsAskAboutItem())
	if len(r.asked) != 1 || !strings.Contains(r.asked[0], "AI 中转站调研") {
		t.Errorf("就此提问应以条目标题预填草稿，实际 %q", r.asked)
	}
	if r.dialogOpen(detailTitle) {
		t.Error("发起提问后弹窗应关闭")
	}
}

// 成果卡里为 0 的计数不显示；没有标题的条目写「无标题」。
func TestDetailCompletionCardSkipsZeroCountsAndUntitled(t *testing.T) {
	r := openedDetail(t, detailFixture(nil), withFacts(services.ImportItemFacts{ID: "item-9", BodyLength: 10}))
	if !r.hasText(locales.ImportsCompletionBody(10)) || !r.hasText(locales.LibraryUntitled()) {
		t.Errorf("应有正文字数与「无标题」；画面上有 %q", r.session.Texts())
	}
	for _, absent := range []string{locales.ImportsCompletionTranscript(0), locales.ImportsCompletionAssets(0), locales.ImportsCompletionTags(0)} {
		if r.hasText(absent) {
			t.Errorf("为 0 的计数不应显示：%q", absent)
		}
	}
}

func TestDetailCompletionCardAbsentWhenFactsMissingOrFailing(t *testing.T) {
	missing := openedDetail(t, detailFixture(nil))
	if missing.hasText(locales.ImportsCompletionTitle()) {
		t.Error("读不到条目时不应显示成果卡")
	}
	if got := missing.count(gfx.RoleButton, locales.ImportsOpenItem()); got != 1 {
		t.Errorf("没有成果卡时只有动作区一颗「打开条目」，实际 %d 颗", got)
	}
	// 出错同样不显示，不弹提示，只记日志（成果卡有没有都不该让整张详情出错）。
	failing := openedDetail(t, detailFixture(nil), func(r *rig) { r.svc.SetItemFactsError(errors.New("数据库繁忙")) })
	if failing.hasText(locales.ImportsCompletionTitle()) || failing.notifier.Len() != 0 {
		t.Errorf("读取失败时不应显示成果卡也不应弹提示；提示 %d 条", failing.notifier.Len())
	}
	if entries := failing.log.entries(); len(entries) != 1 || !strings.Contains(entries[0], "数据库繁忙") {
		t.Errorf("读取失败应记一条日志，实际 %v", entries)
	}
	// 同一次打开里只读一次。
	failing.frames(5)
	if !slices.Equal(failing.svc.ItemFactsLoaded(), []string{"item-9"}) {
		t.Errorf("同一次打开里同一个条目只应读一次，实际 %v", failing.svc.ItemFactsLoaded())
	}
}

// 来源刷新对照：原条目与刷新副本都读到才显示；只读到一边不显示。
func TestDetailRefreshPairNeedsBothSides(t *testing.T) {
	refreshed := detailFixture(func(x *services.ImportTask) { x.RefreshOfItemID = "item-1" })
	original := services.ImportItemFacts{ID: "item-1", Title: "旧版", BodyLength: 120}
	copyFacts := services.ImportItemFacts{ID: "item-9", Title: "", BodyLength: 340}

	r := openedDetail(t, refreshed, withFacts(original, copyFacts))
	for _, want := range []string{
		locales.ImportsRefreshComparison(), locales.ImportsRefreshComparisonHint(),
		locales.ImportsRefreshOriginal(), locales.ImportsRefreshNew(), "旧版", "120 字", "340 字",
	} {
		if !r.hasText(want) {
			t.Errorf("对照卡缺少文字 %q；画面上有 %q", want, r.session.Texts())
		}
	}
	r.scrollBy(640, 400, 5000) // 字段、图例与对照卡把动作区挤出了视口，先滚到底
	r.click(gfx.RoleButton, locales.ImportsOpenOriginal())
	if !slices.Equal(r.opened, []string{"item-1"}) {
		t.Errorf("「打开原条目」应打开 item-1，实际 %v", r.opened)
	}

	half := openedDetail(t, refreshed, withFacts(copyFacts))
	if half.hasText(locales.ImportsRefreshComparison()) || half.has(gfx.RoleButton, locales.ImportsOpenOriginal()) {
		t.Error("原条目读不到时不应显示对照卡")
	}
}

func TestDetailRetryRetriesAndCloses(t *testing.T) {
	r := openedDetail(t, failedTask(func(x *services.ImportTask) { x.Error = "HTTP 522" }))
	r.click(gfx.RoleButton, locales.ImportsRetry())
	if got := r.calls(); !slices.Equal(got, []string{"retry:task-1"}) {
		t.Errorf("点重试应调用服务重试，实际 %v", got)
	}
	if r.dialogOpen("手写一个混合检索 RAG 系统") {
		t.Error("重试后弹窗应关闭")
	}
}

// 网页验证的重试带 verifyWeb 标记，成功后关闭弹窗；开启自动求解时按钮改写。
func TestDetailWebVerifyRetryClosesOnSuccess(t *testing.T) {
	task := failedTask(func(x *services.ImportTask) {
		x.SourceInput, x.Error, x.DisplayName = "https://example.com/post", "web_verification_required: 需要网页验证", "文章"
	})
	r := openedDetail(t, task)
	if r.has(gfx.RoleButton, locales.ImportsRetry()) {
		t.Error("需要网页验证时用验证按钮取代普通重试")
	}
	r.click(gfx.RoleButton, locales.ImportsWebVerifyRetry())
	if got := r.calls(); !slices.Equal(got, []string{"retry:task-1"}) {
		t.Errorf("应调用服务重试，实际 %v", got)
	}
	if r.svc.RetryOptions().VerifyWeb != true {
		t.Errorf("网页验证重试应带 verifyWeb，实际 %+v", r.svc.RetryOptions())
	}
	if r.dialogOpen("文章") {
		t.Error("成功后弹窗应关闭")
	}
	solver := openedDetail(t, task, func(r *rig) { r.solver = true })
	if !solver.has(gfx.RoleButton, locales.ImportsWebSolverRetry()) {
		t.Errorf("开启自动求解后应写「%s」", locales.ImportsWebSolverRetry())
	}
}

func TestDetailWebVerifyRetryFailureKeepsDialogAndReports(t *testing.T) {
	task := failedTask(func(x *services.ImportTask) {
		x.SourceInput, x.Error, x.DisplayName = "https://example.com/post", "web_verification_required: 需要网页验证", "文章"
	})
	r := openedDetail(t, task, func(r *rig) { r.svc.FailOn("retry", errors.New("队列已满")) })
	r.click(gfx.RoleButton, locales.ImportsWebVerifyRetry())
	if !r.dialogOpen("文章") {
		t.Error("重试失败时弹窗应保留，用户才能再试")
	}
	if r.notifier.Len() == 0 {
		t.Error("重试失败应有错误提示")
	}
	if node, ok := r.node(gfx.RoleButton, locales.ImportsWebVerifyRetry()); !ok || node.Disabled {
		t.Error("失败后验证按钮应恢复可点")
	}
}

// 登录态重试：钩子报告「已发起」才关闭弹窗；没发起（登录窗口被关等）弹窗保留。
func TestDetailAuthenticatedRetryClosesOnlyWhenStarted(t *testing.T) {
	task := failedTask(func(x *services.ImportTask) {
		x.SourceInput, x.Error, x.DisplayName = "https://www.xiaohongshu.com/explore/abc", "[structure_missing] 笔记页未返回数据", "笔记"
	})
	notStarted := openedDetail(t, task)
	notStarted.click(gfx.RoleButton, locales.ImportsAuthenticatedRetry())
	if len(notStarted.authRetried) != 1 || !notStarted.dialogOpen("笔记") {
		t.Errorf("没发起登录态重试时弹窗应保留；钩子调用 %v", notStarted.authRetried)
	}
	started := openedDetail(t, task, func(r *rig) { r.authOK = true })
	started.click(gfx.RoleButton, locales.ImportsAuthenticatedRetry())
	if started.dialogOpen("笔记") {
		t.Error("已发起登录态重试后弹窗应关闭")
	}
}

func nodeseekTask() services.ImportTask {
	return failedTask(func(x *services.ImportTask) {
		x.SourceInput, x.Error, x.DisplayName = "https://www.nodeseek.com/post-123-1", "nodeseek_verification_required", "帖子"
	})
}

// NodeSeek：验证通过后带登录态重试并关闭弹窗；验证失败弹错误（带原因），弹窗保留。
func TestDetailNodeseekVerifyFlow(t *testing.T) {
	r := openedDetail(t, nodeseekTask())
	if !r.has(gfx.RoleButton, locales.ImportsRetry()) || !r.has(gfx.RoleButton, locales.ImportsNodeseekVerifyRetry()) {
		t.Fatal("NodeSeek 的失败应同时有普通重试与验证重试")
	}
	r.click(gfx.RoleButton, locales.ImportsNodeseekVerifyRetry())
	if !slices.Equal(r.nodeCalls, []string{"verify:task-1"}) {
		t.Errorf("应打开验证窗口，实际 %v", r.nodeCalls)
	}
	if r.svc.RetryOptions().CaptureStrategy != "authenticated" {
		t.Errorf("验证通过后应带登录态重试，实际 %+v", r.svc.RetryOptions())
	}
	if r.dialogOpen("帖子") {
		t.Error("重试成功后弹窗应关闭")
	}

	failed := openedDetail(t, nodeseekTask(), func(r *rig) { r.nodeErr = errors.New("验证窗口被关闭") })
	failed.click(gfx.RoleButton, locales.ImportsNodeseekVerifyRetry())
	if !failed.dialogOpen("帖子") || !failed.toastShown(locales.ImportsNodeseekVerificationFailed()) {
		t.Errorf("验证失败应提示且保留弹窗；画面上有 %q", failed.session.Texts())
	}
	if len(failed.calls()) != 0 {
		t.Errorf("验证失败不应重试任务：%v", failed.calls())
	}
}

// 验证进行中：按钮变成转圈的「等待网页验证…」并出现「取消」；取消后不重试也不报失败；关闭弹窗同样会取消。
func TestDetailNodeseekPendingCancelAndClose(t *testing.T) {
	// pendingRig 让验证窗口一直开着，直到外壳收到「取消」才返回：与真实的 platformCapture.login / cancelLogin 的关系一致。
	// 后台动作真的放到 goroutine 里跑（默认测试里是同步的），钩子的记录用锁保护。
	pendingRig := func(t *testing.T) (r *rig, calls func() []string) {
		var mu sync.Mutex
		var log []string
		record := func(s string) {
			mu.Lock()
			log = append(log, s)
			mu.Unlock()
		}
		release := make(chan struct{})
		r = openedDetail(t, nodeseekTask())
		r.screen.Hooks.VerifyNodeseek = func(_ context.Context, task services.ImportTask) error {
			record("verify:" + task.ID)
			<-release // 等用户在验证窗口里操作；被取消时返回
			return errors.New("验证已取消")
		}
		r.screen.Hooks.CancelNodeseek = func() error {
			record("cancel")
			close(release)
			return nil
		}
		r.store.Spawn = func(fn func()) { go fn() }
		return r, func() []string {
			mu.Lock()
			defer mu.Unlock()
			return slices.Clone(log)
		}
	}
	idle := func(r *rig) bool {
		r.frames(1)
		return r.has(gfx.RoleButton, locales.ImportsNodeseekVerifyRetry()) && !r.has(gfx.RoleButton, locales.CommonCancel())
	}

	t.Run("点取消", func(t *testing.T) {
		r, calls := pendingRig(t)
		r.click(gfx.RoleButton, locales.ImportsNodeseekVerifyRetry())
		if node, ok := r.node(gfx.RoleButton, locales.ImportsNodeseekVerifying()); !ok || !node.Disabled {
			t.Fatal("等待验证时主按钮应写「等待网页验证…」并禁用")
		}
		r.click(gfx.RoleButton, locales.CommonCancel())
		waitFor(t, "验证结束、按钮恢复", func() bool { return idle(r) })
		if got := calls(); !slices.Equal(got, []string{"verify:task-1", "cancel"}) {
			t.Errorf("应先开验证窗口、再收到取消，实际 %v", got)
		}
		if r.toastShown(locales.ImportsNodeseekVerificationFailed()) || len(r.calls()) != 0 {
			t.Errorf("取消之后不应弹失败也不应重试；画面上有 %q，调用 %v", r.session.Texts(), r.calls())
		}
	})

	t.Run("关闭弹窗", func(t *testing.T) {
		r, calls := pendingRig(t)
		r.click(gfx.RoleButton, locales.ImportsNodeseekVerifyRetry())
		r.keys("escape")
		waitFor(t, "外壳收到取消", func() bool { return slices.Contains(calls(), "cancel") })
		if r.dialogOpen("帖子") {
			t.Error("Esc 应关闭弹窗")
		}
		r.frames(2)
		if r.toastShown(locales.ImportsNodeseekVerificationFailed()) || len(r.calls()) != 0 {
			t.Errorf("关闭弹窗取消验证不应弹失败也不应重试；画面上有 %q，调用 %v", r.session.Texts(), r.calls())
		}
	})
}

// 任务被「清理已完成」删掉时弹窗跟着消失。
func TestDetailVanishesWhenTaskIsRemoved(t *testing.T) {
	r := openedDetail(t, detailFixture(nil))
	r.svc.Set(pageOf(), nil)
	r.store.Fetch(r.screen.Ctx)
	r.frames(3)
	if r.dialogOpen(detailTitle) || r.screen.detailTaskID != "" {
		t.Errorf("任务没了弹窗应消失；detailTaskID=%q", r.screen.detailTaskID)
	}
}
