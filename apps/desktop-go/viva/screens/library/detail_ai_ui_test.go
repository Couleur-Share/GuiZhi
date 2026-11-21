package library

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/notes"
	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件测详情里三项 AI 功能的界面接线（detailsummary.go、detailocr.go、tagsuggest.go，台账 B-053、B-060）：
// AI 摘要卡片、图中文字卡片与标签浮层里的 AI 建议。生成、写回与失败提示背后的规则在 store_ai_test.go 里测过，这里只测界面接对了没有。
// 夹具里后台动作默认同步执行；要观察「在途」就 goAsync 并用服务的 Hold* 卡住。

// errTagSuggest 是标签建议失败用例里注入的原因。
var errTagSuggest = errors.New("模型出错了")

const (
	aiImageID      = "x-ai-image"
	aiImageContent = "![图一](local-image://a.png)\n\n正文。"
)

// aiRig 是壳夹具再追加一条带本地配图的图片条目。
func aiRig(t *testing.T, mutate ...func(*rig)) *rig {
	t.Helper()
	return shellRig(t, append([]func(*rig){func(r *rig) {
		entries := detailEntries()
		for _, x := range shellExtras() {
			entries = append(entries, x.entry)
		}
		entries = append(entries, shellEntry(aiImageID, "一张图", services.LibraryTypeImage))
		r.svc.SeedEntries(entries)
		r.svc.SeedDetail(aiImageID, servicestest.DetailBody{Content: aiImageContent})
	}}, mutate...)...)
}

// callsWithPrefix 是服务收到的、名字以 prefix 开头的调用次数。
func (r *rig) callsWithPrefix(prefix string) int {
	n := 0
	for _, call := range r.svc.Calls() {
		if strings.HasPrefix(call, prefix) {
			n++
		}
	}
	return n
}

// once 把放行函数包成可重复调用的，并在测试结束时自动放行（免得协程悬着）。
func (r *rig) once(gate func()) func() {
	var once sync.Once
	release := func() { once.Do(gate) }
	r.t.Cleanup(release)
	return release
}

// spawnTracker 让后台动作放到别的协程，并能等它们全部结束。
func (r *rig) spawnTracker() *sync.WaitGroup {
	var wg sync.WaitGroup
	r.store.Spawn = func(fn func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fn()
		}()
	}
	return &wg
}

// closeToolsPanel 点面板的收起钮。
func (r *rig) closeToolsPanel() {
	r.t.Helper()
	r.click(gfx.RoleButton, locales.ArticleReaderCloseTools())
	if r.toolsShown() {
		r.t.Fatal("点收起钮应收起面板")
	}
}

// reopenTools 经头部「更多」菜单重新打开面板。
func (r *rig) reopenTools() {
	r.t.Helper()
	r.clickInDetail(gfx.RoleCombobox, locales.LibraryMoreActions())
	r.click(gfx.RoleMenuItem, locales.ArticleReaderTools())
	if !r.toolsShown() {
		r.t.Fatal("经菜单应能重新打开面板")
	}
}

// ---- AI 摘要 ----

const richSummary = "## 要点\n\n- **并发**：goroutine 与 channel\n- 同步：`sync.Mutex`\n\n```go\nx := 1\n```\n"

// 没有摘要：一枚动作钮；点了生成并写回，卡片自动展开、用 Markdown 排版摘要，并弹成功提示。
func TestSummaryCardGeneratesAndAutoExpands(t *testing.T) {
	t.Parallel()
	r := aiRig(t)
	r.svc.SeedSummary(services.LibrarySummaryResult{Text: richSummary}, nil)
	r.openTools(detailNoteID)
	r.click(gfx.RoleButton, locales.LibraryAiSummaryGenerate())
	if got := r.callsWithPrefix("GenerateSummary"); got != 1 {
		t.Fatalf("应发起一次生成，实际 %d 次", got)
	}
	if !r.has(gfx.RoleButton, locales.LibraryAiSummaryCollapse()) {
		t.Fatalf("写回成功后应自动展开（展开钮的名字是「收起」）；画面文字 %q", r.session.Texts())
	}
	text := r.screenText()
	for _, want := range []string{"要点", "并发", "goroutine", "sync.Mutex", "x := 1", "\u2022"} {
		if !strings.Contains(text, want) {
			t.Errorf("展开的摘要里应有「%s」；画面文字 %q", want, text)
		}
	}
	if !r.hasText(locales.LibraryAiSummaryDone()) {
		t.Errorf("应弹成功提示；画面文字 %q", r.session.Texts())
	}
	if r.has(gfx.RoleButton, locales.LibraryAiSummaryGenerate()) {
		t.Error("有摘要之后不再摆「生成 AI 摘要」")
	}
}

// 展开与折叠：点头部那枚钮切换，名字跟着变，折叠时摘要正文不画。
func TestSummaryCardCollapsesAndExpands(t *testing.T) {
	t.Parallel()
	r := aiRig(t, func(r *rig) {
		r.svc.SeedDetail(detailNoteID, servicestest.DetailBody{Content: "正文", Summary: "已有的摘要内容"})
	})
	r.openTools(detailNoteID)
	if r.has(gfx.RoleButton, locales.LibraryAiSummaryCollapse()) || strings.Contains(r.screenText(), "已有的摘要内容") {
		t.Fatal("默认折叠：摘要不抢占首屏")
	}
	if n := r.mustNode(gfx.RoleButton, locales.LibraryAiSummaryExpand()); !n.Expandable || n.Expanded {
		t.Fatalf("折叠着的展开钮应是 aria-expanded=false：%+v", n)
	}
	r.click(gfx.RoleButton, locales.LibraryAiSummaryExpand())
	if n := r.mustNode(gfx.RoleButton, locales.LibraryAiSummaryCollapse()); !n.Expanded {
		t.Fatalf("展开后应是 aria-expanded=true：%+v", n)
	}
	if !strings.Contains(r.screenText(), "已有的摘要内容") {
		t.Fatalf("展开后应画出摘要；画面文字 %q", r.session.Texts())
	}
	r.click(gfx.RoleButton, locales.LibraryAiSummaryCollapse())
	if strings.Contains(r.screenText(), "已有的摘要内容") || !r.has(gfx.RoleButton, locales.LibraryAiSummaryExpand()) {
		t.Fatal("再点应折叠")
	}
}

// 重新生成：有摘要时头部右边是「重新生成」钮，点了再生成一次，写回后展开。
func TestSummaryCardRegenerates(t *testing.T) {
	t.Parallel()
	r := aiRig(t, func(r *rig) {
		r.svc.SeedDetail(detailNoteID, servicestest.DetailBody{Content: "正文", Summary: "旧摘要"})
		r.svc.SeedSummary(services.LibrarySummaryResult{Text: "新摘要"}, nil)
	})
	r.openTools(detailNoteID)
	r.hover(gfx.RoleButton, locales.LibraryAiSummaryRegenerate())
	r.click(gfx.RoleButton, locales.LibraryAiSummaryRegenerate())
	if got := r.callsWithPrefix("GenerateSummary"); got != 1 {
		t.Fatalf("应再生成一次，实际 %d 次", got)
	}
	if text := r.screenText(); !strings.Contains(text, "新摘要") || strings.Contains(text, "旧摘要") {
		t.Fatalf("写回后应展开新摘要；画面文字 %q", r.session.Texts())
	}
}

// 生成在途：卡片出现、头部右边是转圈（没有「重新生成」钮）、正文区是「正在生成摘要…」；完成后展开。
func TestSummaryCardShowsGeneratingThenExpands(t *testing.T) {
	t.Parallel()
	r := aiRig(t)
	r.openTools(detailNoteID)
	wg := r.spawnTracker()
	release := r.once(r.svc.HoldSummary())
	r.click(gfx.RoleButton, locales.LibraryAiSummaryGenerate())
	if !r.hasText(locales.LibraryAiSummaryGenerating()) {
		t.Fatalf("生成中应显示「%s」；画面文字 %q", locales.LibraryAiSummaryGenerating(), r.session.Texts())
	}
	if r.has(gfx.RoleButton, locales.LibraryAiSummaryRegenerate()) || r.has(gfx.RoleButton, locales.LibraryAiSummaryGenerate()) {
		t.Fatal("生成中没有「重新生成」钮，也不再是入口钮")
	}
	if !r.has(gfx.RoleButton, locales.LibraryAiSummaryExpand()) {
		t.Fatal("生成中卡片的展开钮还在（折叠着）")
	}
	release()
	wg.Wait()
	r.frames(3)
	if !r.has(gfx.RoleButton, locales.LibraryAiSummaryCollapse()) || r.hasText(locales.LibraryAiSummaryGenerating()) {
		t.Fatalf("完成后应展开并去掉「正在生成」；画面文字 %q", r.session.Texts())
	}
}

// 侧栏收起期间生成完成：状态（渲染层里面板只是隐藏）保留，重新打开时摘要已展开。
func TestSummaryCardCompletedWhileThePanelIsClosedIsExpandedOnReopen(t *testing.T) {
	t.Parallel()
	r := aiRig(t)
	r.openTools(detailNoteID)
	wg := r.spawnTracker()
	release := r.once(r.svc.HoldSummary())
	r.click(gfx.RoleButton, locales.LibraryAiSummaryGenerate())
	r.closeToolsPanel()
	release()
	wg.Wait()
	r.frames(3)
	r.reopenTools()
	if !r.has(gfx.RoleButton, locales.LibraryAiSummaryCollapse()) {
		t.Fatalf("重新打开时应已展开；画面文字 %q", r.session.Texts())
	}
}

// 换条目：展开状态复位（渲染层按条目重建组件），换回来也是折叠的；音视频条目没有摘要区块。
func TestSummaryCardResetsWhenTheItemChanges(t *testing.T) {
	t.Parallel()
	r := aiRig(t, func(r *rig) {
		r.svc.SeedDetail(detailNoteID, servicestest.DetailBody{Content: "正文", Summary: "甲的摘要"})
		r.svc.SeedDetail(shellCollectedID, servicestest.DetailBody{Content: "正文", Summary: "乙的摘要"})
	})
	r.openTools(detailNoteID)
	r.click(gfx.RoleButton, locales.LibraryAiSummaryExpand())
	if !strings.Contains(r.screenText(), "甲的摘要") {
		t.Fatal("夹具失效：应已展开")
	}
	r.pick(shellCollectedID)
	r.reopenIfClosed()
	if !r.has(gfx.RoleButton, locales.LibraryAiSummaryExpand()) || strings.Contains(r.screenText(), "乙的摘要") {
		t.Fatalf("换条目后应折叠；画面文字 %q", r.session.Texts())
	}
	r.pick(detailNoteID)
	r.reopenIfClosed()
	if !r.has(gfx.RoleButton, locales.LibraryAiSummaryExpand()) || strings.Contains(r.screenText(), "甲的摘要") {
		t.Fatalf("换回来也是折叠的；画面文字 %q", r.session.Texts())
	}

	v := aiRig(t, func(r *rig) {
		r.svc.SeedDetail(detailVideoID, servicestest.DetailBody{Content: "视频", Transcript: "文字稿", Summary: "视频的摘要"})
	})
	v.openTools(detailVideoID)
	if v.has(gfx.RoleButton, locales.LibraryAiSummaryExpand()) || v.has(gfx.RoleButton, locales.LibraryAiSummaryGenerate()) {
		t.Fatal("音视频条目没有摘要区块")
	}
}

// reopenIfClosed 在面板因换条目而收起时重新打开它（工具侧栏的开合属于条目）。
func (r *rig) reopenIfClosed() {
	r.t.Helper()
	if !r.toolsShown() {
		r.reopenTools()
	}
}

// ---- 图中文字 ----

// 只有图片条目且正文里有本地配图引用才出现；点了识别并写回，之后钮上写「重新识别」。
func TestOcrCardAppearsOnlyForImagesAndRegenerates(t *testing.T) {
	t.Parallel()
	r := aiRig(t)
	r.openTools(aiImageID)
	if !r.has(gfx.RoleButton, locales.LibraryOcrGenerate()) {
		t.Fatalf("图片条目应摆「%s」；画面文字 %q", locales.LibraryOcrGenerate(), r.session.Texts())
	}
	r.click(gfx.RoleButton, locales.LibraryOcrGenerate())
	if got := r.callsWithPrefix("RecognizeImageText"); got != 1 {
		t.Fatalf("应识别一张图，实际 %d 次", got)
	}
	if n, _ := r.svc.ItemNow(aiImageID); !strings.Contains(n.Content, notes.OCRSectionHeading) {
		t.Fatalf("识别结果应写回正文：%q", n.Content)
	}
	if !r.has(gfx.RoleButton, locales.LibraryOcrRegenerate()) || r.has(gfx.RoleButton, locales.LibraryOcrGenerate()) {
		t.Fatalf("已有识别结果时钮上应写「%s」；画面文字 %q", locales.LibraryOcrRegenerate(), r.session.Texts())
	}
	r.click(gfx.RoleButton, locales.LibraryOcrRegenerate())
	if got := r.callsWithPrefix("RecognizeImageText"); got != 2 {
		t.Fatalf("重新识别应再识别一次，实际共 %d 次", got)
	}

	for _, c := range []struct {
		name string
		rig  *rig
		id   string
	}{
		{"笔记条目", aiRig(t), detailNoteID},
		{"没有本地配图的图片条目", aiRig(t, func(r *rig) {
			r.svc.SeedDetail(aiImageID, servicestest.DetailBody{Content: "![网络图](https://example.com/a.png)"})
		}), aiImageID},
	} {
		c.rig.openTools(c.id)
		for _, label := range []string{locales.LibraryOcrGenerate(), locales.LibraryOcrRegenerate(), locales.LibraryOcrRunning()} {
			if c.rig.has(gfx.RoleButton, label) {
				t.Errorf("%s不该有图中文字钮「%s」", c.name, label)
			}
		}
	}
}

// 识别在途：钮换成「正在识别图中文字…」并禁用（点不动）；完成后恢复可用。
func TestOcrCardIsDisabledWhileRunning(t *testing.T) {
	t.Parallel()
	r := aiRig(t)
	r.openTools(aiImageID)
	wg := r.spawnTracker()
	release := r.once(r.svc.HoldOcr())
	r.click(gfx.RoleButton, locales.LibraryOcrGenerate())
	n := r.mustNode(gfx.RoleButton, locales.LibraryOcrRunning())
	if !n.Disabled {
		t.Fatal("识别中钮应禁用")
	}
	x, y := centerOf(n)
	r.clickAt(x, y)
	release()
	wg.Wait()
	r.frames(3)
	if got := r.callsWithPrefix("RecognizeImageText"); got != 1 {
		t.Fatalf("禁用的钮点不动，只该识别一次，实际 %d 次", got)
	}
	if n := r.mustNode(gfx.RoleButton, locales.LibraryOcrRegenerate()); n.Disabled {
		t.Fatal("完成后钮应恢复可用")
	}
}

// 面板里的顺序：图中文字、AI 摘要，各占自己的位置（图片条目两者都有）。
func TestDetailAICardsOrderOcrBeforeSummary(t *testing.T) {
	t.Parallel()
	r := aiRig(t)
	r.openTools(aiImageID)
	ocr := r.mustNode(gfx.RoleButton, locales.LibraryOcrGenerate())
	sum := r.mustNode(gfx.RoleButton, locales.LibraryAiSummaryGenerate())
	before := ocr.Bounds.Y < sum.Bounds.Y || (ocr.Bounds.Y == sum.Bounds.Y && ocr.Bounds.X < sum.Bounds.X)
	if !before {
		t.Fatalf("图中文字应排在 AI 摘要之前：%+v %+v", ocr.Bounds, sum.Bounds)
	}
}

// ---- 标签浮层里的 AI 建议 ----

// openTagPopover 打开详情标签行的 + 浮层。
func (r *rig) openTagPopover() {
	r.t.Helper()
	r.click(gfx.RoleButton, locales.LibraryAddTag())
	if !r.has(gfx.RoleButton, locales.LibraryTagsGenerate()) {
		r.t.Fatal("打开标签浮层后应有 AI 建议的「生成」钮")
	}
}

// 生成 → 虚线候选；已带着的标签被滤掉；点候选追加到条目并从候选里消失；X 忽略全部。
func TestTagSuggestChipsFilterApplyAndDismiss(t *testing.T) {
	t.Parallel()
	r := aiRig(t)
	r.svc.SeedTagSuggestions([]string{"alpha", "beta", "gamma"}, nil)
	r.openTools(detailNoteID)
	r.openTagPopover()
	if r.has(gfx.RoleButton, locales.LibraryAiTagsDismiss()) {
		t.Fatal("没有候选时不画「忽略建议」钮")
	}
	r.click(gfx.RoleButton, locales.LibraryTagsGenerate())
	for _, name := range []string{"alpha", "beta", "gamma"} {
		if !r.has(gfx.RoleButton, name) {
			t.Fatalf("应出现候选「%s」；画面文字 %q", name, r.session.Texts())
		}
	}
	if !r.has(gfx.RoleButton, locales.LibraryAiTagsDismiss()) {
		t.Fatal("有候选时应画「忽略建议」钮")
	}

	r.click(gfx.RoleButton, "alpha")
	tags := func() []string {
		var names []string
		for _, tag := range r.view().Detail.Item.Tags {
			names = append(names, tag.Name)
		}
		return names
	}
	if got := tags(); len(got) != 1 || got[0] != "alpha" {
		t.Fatalf("点候选应把它追加到条目标签，实际 %q", got)
	}
	if r.has(gfx.RoleButton, "alpha") || !r.has(gfx.RoleButton, "beta") {
		t.Fatalf("已带上的候选应从候选里消失、其余还在；画面文字 %q", r.session.Texts())
	}

	r.click(gfx.RoleButton, "beta")
	if got := tags(); len(got) != 2 || got[1] != "beta" {
		t.Fatalf("再点一个应接在后面，实际 %q", got)
	}

	r.click(gfx.RoleButton, locales.LibraryAiTagsDismiss())
	if r.has(gfx.RoleButton, "gamma") || r.has(gfx.RoleButton, locales.LibraryAiTagsDismiss()) {
		t.Fatalf("点 X 应清掉全部建议；画面文字 %q", r.session.Texts())
	}
}

// 建议里的标签都已经在条目上（含空建议）：弹「建议的标签都已存在」，不出候选。
func TestTagSuggestAllExistNotice(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		tags []string
	}{{"空建议", nil}, {"全是已有的", []string{"go"}}} {
		r := aiRig(t, func(r *rig) {
			r.svc.SeedEntries(withTags(detailEntries(), detailNoteID, "go"))
		})
		r.svc.SeedTagSuggestions(c.tags, nil)
		r.openTools(detailNoteID)
		r.openTagPopover()
		r.click(gfx.RoleButton, locales.LibraryTagsGenerate())
		if !r.hasText(locales.LibraryAiTagsAllExist()) {
			t.Errorf("%s：应弹「%s」；画面文字 %q", c.name, locales.LibraryAiTagsAllExist(), r.session.Texts())
		}
		if r.has(gfx.RoleButton, locales.LibraryAiTagsDismiss()) {
			t.Errorf("%s：不该有候选", c.name)
		}
	}
}

// withTags 给列表投影里的一条加上标签。
func withTags(entries []services.LibraryEntry, id string, names ...string) []services.LibraryEntry {
	for i := range entries {
		if entries[i].ID != id {
			continue
		}
		for _, name := range names {
			entries[i].Tags = append(entries[i].Tags, services.LibraryTag{ID: "tag-" + name, Name: name, ColorKey: "blue"})
		}
	}
	return entries
}

// 生成中「生成」钮禁用（不能重复发起）；完成后出候选。
func TestTagSuggestGenerateIsDisabledWhileRunning(t *testing.T) {
	t.Parallel()
	r := aiRig(t)
	r.svc.SeedTagSuggestions([]string{"alpha"}, nil)
	r.openTools(detailNoteID)
	r.openTagPopover()
	wg := r.spawnTracker()
	release := r.once(r.svc.HoldSuggestTags())
	r.click(gfx.RoleButton, locales.LibraryTagsGenerate())
	n := r.mustNode(gfx.RoleButton, locales.LibraryTagsGenerate())
	if !n.Disabled {
		t.Fatal("生成中「生成」钮应禁用")
	}
	x, y := centerOf(n)
	r.clickAt(x, y)
	release()
	wg.Wait()
	r.frames(3)
	if got := r.callsWithPrefix("SuggestTags"); got != 1 {
		t.Fatalf("只该发起一次，实际 %d 次", got)
	}
	if !r.has(gfx.RoleButton, "alpha") || r.mustNode(gfx.RoleButton, locales.LibraryTagsGenerate()).Disabled {
		t.Fatalf("完成后应出候选且钮恢复；画面文字 %q", r.session.Texts())
	}
}

// 关闭再打开：候选清空；在途的旧请求回来也不会写进新一次打开（令牌作废），忙标记不卡住新一轮。
func TestTagSuggestClearsOnCloseAndIgnoresStaleResults(t *testing.T) {
	t.Parallel()
	r := aiRig(t)
	r.svc.SeedTagSuggestions([]string{"alpha"}, nil)
	r.openTools(detailNoteID)
	r.openTagPopover()
	r.click(gfx.RoleButton, locales.LibraryTagsGenerate())
	if !r.has(gfx.RoleButton, "alpha") {
		t.Fatal("夹具失效：应有候选")
	}
	r.keys("Escape")
	r.openTagPopover()
	if r.has(gfx.RoleButton, "alpha") || r.has(gfx.RoleButton, locales.LibraryAiTagsDismiss()) {
		t.Fatalf("重新打开不该带着上一次的候选；画面文字 %q", r.session.Texts())
	}

	wg := r.spawnTracker()
	release := r.once(r.svc.HoldSuggestTags())
	r.click(gfx.RoleButton, locales.LibraryTagsGenerate())
	r.keys("Escape")
	r.openTagPopover()
	if r.mustNode(gfx.RoleButton, locales.LibraryTagsGenerate()).Disabled {
		t.Fatal("重新打开后忙标记应已清掉")
	}
	release()
	wg.Wait()
	r.frames(3)
	if r.has(gfx.RoleButton, "alpha") {
		t.Fatalf("旧请求的结果不该落进新一次打开；画面文字 %q", r.session.Texts())
	}
}

// 生成失败：沿用 Store 的错误提示，候选保持原样、钮恢复可用。
func TestTagSuggestFailureKeepsStateAndShowsError(t *testing.T) {
	t.Parallel()
	r := aiRig(t)
	r.svc.SeedTagSuggestions(nil, errTagSuggest)
	r.openTools(detailNoteID)
	r.openTagPopover()
	r.click(gfx.RoleButton, locales.LibraryTagsGenerate())
	if !r.hasText(locales.LibraryAiTagsFailed(errTagSuggest.Error())) {
		t.Fatalf("应弹失败提示；画面文字 %q", r.session.Texts())
	}
	if r.mustNode(gfx.RoleButton, locales.LibraryTagsGenerate()).Disabled || r.has(gfx.RoleButton, locales.LibraryAiTagsDismiss()) {
		t.Fatal("失败后钮应恢复、不出候选")
	}
}
