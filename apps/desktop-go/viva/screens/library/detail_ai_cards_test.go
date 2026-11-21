package library

import (
	"math"
	"reflect"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 本文件补三项 AI 功能界面接线（detailsummary.go、detailocr.go、tagsuggest.go）的边角（M2-6c 变异检查的存活项）：
// 图标与转圈、悬停提示、状态归属、候选去重、令牌与唤醒。主流程见 detail_ai_ui_test.go。

// extraWidth 是按钮比它自己的文字宽出多少：图标、转圈、内边距、间距都算在里面。
func (r *rig) extraWidth(role gfx.Role, name string) float32 {
	r.t.Helper()
	n := r.mustNode(role, name)
	for _, label := range r.session.Snapshot().Texts {
		if label.Value == name && label.Bounds.X >= n.Bounds.X-1 && label.Bounds.X+label.Bounds.Width <= n.Bounds.X+n.Bounds.Width+1 {
			return n.Bounds.Width - label.Bounds.Width
		}
	}
	r.t.Fatalf("按钮「%s」里找不到它的文字", name)
	return 0
}

// buttonCount 是画面上这个可访问名的按钮数。
func (r *rig) buttonCount(name string) int {
	count := 0
	for _, n := range r.session.Access() {
		if n.Role == gfx.RoleButton && n.Label == name {
			count++
		}
	}
	return count
}

// iconPoints 是落在 area 里的图标像素（描边路径被光栅成一像素高的小矩形）的中心点；比图标大的矩形是底色，不算。
func (r *rig) iconPoints(area gfx.Rect) []gfx.Point {
	var out []gfx.Point
	for _, p := range r.session.Snapshot().Primitives {
		b := p.Bounds
		if p.Kind != app.RectanglePrimitive || b.Width > 4 || b.Height > 4 {
			continue
		}
		if b.X < area.X || b.X+b.Width > area.X+area.Width || b.Y < area.Y || b.Y+b.Height > area.Y+area.Height {
			continue
		}
		out = append(out, gfx.Point{X: b.X + b.Width/2, Y: b.Y + b.Height/2})
	}
	return out
}

// chevronPointsDown 判断展开钮右端的箭头是不是朝下（「v」：两端高、中间低）。
func (r *rig) chevronPointsDown(toggle app.AccessNode) bool {
	r.t.Helper()
	size := r.look.Rem(0.875)
	// 按钮内容靠左排（Grow 只拉宽按钮），箭头是内容里最右的一个图标：取按钮内全部图标顶点，再留最右一个图标宽度内的。
	all := r.iconPoints(toggle.Bounds)
	if len(all) == 0 {
		r.t.Fatal("展开钮里找不到图标")
	}
	right := math.Inf(-1)
	for _, p := range all {
		right = math.Max(right, float64(p.X))
	}
	var points []gfx.Point
	for _, p := range all {
		if float64(p.X) >= right-float64(size)+1 {
			points = append(points, p)
		}
	}
	minX, maxX := math.MaxFloat64, -math.MaxFloat64
	for _, p := range points {
		minX, maxX = math.Min(minX, float64(p.X)), math.Max(maxX, float64(p.X))
	}
	mean := func(from, to float64) float64 {
		sum, n := 0.0, 0
		for _, p := range points {
			if float64(p.X) >= from && float64(p.X) <= to {
				sum += float64(p.Y)
				n++
			}
		}
		if n == 0 {
			r.t.Fatalf("箭头里 x∈[%.1f, %.1f] 没有顶点", from, to)
		}
		return sum / float64(n)
	}
	mid := (minX + maxX) / 2
	end, centre := mean(minX, minX+1.5), mean(mid-1.5, mid+1.5)
	if math.Abs(end-centre) < 1 {
		r.t.Fatalf("箭头两端与中间应有明显高低差：端 %.1f，中 %.1f", end, centre)
	}
	return end < centre
}

// ---- AI 摘要 ----

// 摘要的 Markdown 解析按原文缓存：同一份文字命中缓存（不每帧重解析），文字变了重新解析。
func TestSummaryMarkdownIsCachedByText(t *testing.T) {
	var u summaryUI
	first := u.blocksOf("## 甲\n\n正文")
	if len(first) == 0 {
		t.Fatal("夹具失效：应解析出块")
	}
	if again := u.blocksOf("## 甲\n\n正文"); &again[0] != &first[0] {
		t.Fatal("同一份文字应命中缓存，不重新解析")
	}
	changed := u.blocksOf("乙的段落")
	if !reflect.DeepEqual(changed, kit.ParseMarkdown("乙的段落")) || reflect.DeepEqual(changed, first) {
		t.Fatalf("摘要文字变了应重新解析：%+v", changed)
	}
}

// 展开钮的箭头：折叠时朝下，展开时朝上；重新生成钮带悬停提示。
func TestSummaryCardChevronAndRegenerateHint(t *testing.T) {
	t.Parallel()
	r := aiRig(t, func(r *rig) {
		r.svc.SeedDetail(detailNoteID, servicestest.DetailBody{Content: "正文", Summary: "已有的摘要"})
	})
	r.openTools(detailNoteID)
	if !r.chevronPointsDown(r.mustNode(gfx.RoleButton, locales.LibraryAiSummaryExpand())) {
		t.Fatal("折叠时箭头应朝下")
	}
	r.click(gfx.RoleButton, locales.LibraryAiSummaryExpand())
	if r.chevronPointsDown(r.mustNode(gfx.RoleButton, locales.LibraryAiSummaryCollapse())) {
		t.Fatal("展开时箭头应朝上")
	}
	if !r.hasHint(locales.LibraryAiSummaryRegenerate()) {
		t.Fatal("重新生成钮应带悬停提示（图标钮，文案走提示）")
	}
}

// 摘要卡片的在途与展开序号只认属于这一条的工具状态：别的条目的状态（界面走不到，直接写 Store）不能让这一条显示生成中或自动展开。
func TestSummaryCardIgnoresToolsStateOfAnotherItem(t *testing.T) {
	t.Parallel()
	r := aiRig(t, func(r *rig) {
		r.svc.SeedDetail(detailNoteID, servicestest.DetailBody{Content: "正文", Summary: "已有的摘要"})
	})
	r.openTools(detailNoteID)
	r.setDetail(func(d *DetailView) {
		d.Tools.ItemID, d.Tools.SummaryGenerating, d.Tools.SummaryExpandSeq = "别的条目", true, 5
	})
	if !r.has(gfx.RoleButton, locales.LibraryAiSummaryExpand()) || r.hasText(locales.LibraryAiSummaryGenerating()) || r.has(gfx.RoleButton, locales.LibraryAiSummaryCollapse()) {
		t.Fatalf("别的条目的在途与展开序号不该影响这一条；画面文字 %q", r.session.Texts())
	}
}

// ---- 图中文字 ----

// 笔记类型的条目即使正文里有本地配图引用，也不出图中文字钮（只有图片条目）。
func TestOcrCardIsOnlyForImageItems(t *testing.T) {
	t.Parallel()
	r := aiRig(t, func(r *rig) {
		r.svc.SeedDetail(detailNoteID, servicestest.DetailBody{Content: aiImageContent})
	})
	r.openTools(detailNoteID)
	for _, label := range []string{locales.LibraryOcrGenerate(), locales.LibraryOcrRegenerate(), locales.LibraryOcrRunning()} {
		if r.has(gfx.RoleButton, label) {
			t.Fatalf("笔记条目不该有图中文字钮「%s」", label)
		}
	}
}

// 图中文字钮：识别中是转圈代替图标（同宽），不是没有转圈，也不是转圈加图标；识别中的状态只认属于这一条的工具状态。
func TestOcrCardBusySpinnerReplacesTheIcon(t *testing.T) {
	t.Parallel()
	r := aiRig(t)
	r.openTools(aiImageID)
	idle := r.extraWidth(gfx.RoleButton, locales.LibraryOcrGenerate())
	if idle < r.look.Rem(0.875) {
		t.Fatalf("空闲时钮上应有图标：多出的宽度只有 %.1f", idle)
	}
	wg := r.spawnTracker()
	release := r.once(r.svc.HoldOcr())
	r.click(gfx.RoleButton, locales.LibraryOcrGenerate())
	busy := r.extraWidth(gfx.RoleButton, locales.LibraryOcrRunning())
	release()
	wg.Wait()
	if math.Abs(float64(busy-idle)) > 2 {
		t.Fatalf("识别中应是转圈代替图标（同宽）：空闲多出 %.1f，识别中多出 %.1f", idle, busy)
	}

	other := aiRig(t)
	other.openTools(aiImageID)
	other.setDetail(func(d *DetailView) { d.Tools.ItemID, d.Tools.OcrRunning = "别的条目", true })
	if n := other.mustNode(gfx.RoleButton, locales.LibraryOcrGenerate()); n.Disabled {
		t.Fatal("别的条目的识别状态不该让这一条的钮禁用")
	}
}

// ---- 标签浮层里的 AI 建议 ----

// tagSuggestUI 的状态机：令牌只在有状态可清时才加；在途时不能再开；没产出建议时候选不动；候选是副本；过期的令牌什么也不写。
func TestTagSuggestStateMachine(t *testing.T) {
	var u tagSuggestUI
	u.reset()
	u.reset()
	if u.token != 0 {
		t.Fatalf("空闲时 reset 不该动令牌：%d", u.token)
	}

	token, ok := u.begin()
	if !ok || token != 0 {
		t.Fatalf("第一次应能开始：%d %v", token, ok)
	}
	if _, ok := u.begin(); ok {
		t.Fatal("在途时不能再开始")
	}
	if busy, _ := u.snapshot(); !busy {
		t.Fatal("开始后应是忙的")
	}

	given := []string{"go", "rust"}
	if !u.finish(token, given, true) {
		t.Fatal("令牌对得上应写入")
	}
	given[0] = "被改了"
	busy, suggestions := u.snapshot()
	if busy || !slices.Equal(suggestions, []string{"go", "rust"}) {
		t.Fatalf("候选应是写入时的副本、忙标记放下：%v %v", busy, suggestions)
	}
	suggestions[0] = "也被改了"
	if _, again := u.snapshot(); !slices.Equal(again, []string{"go", "rust"}) {
		t.Fatalf("snapshot 返回的应是副本：%v", again)
	}

	token, _ = u.begin()
	if !u.finish(token, nil, false) {
		t.Fatal("没产出建议也算写入（放下忙标记）")
	}
	if _, kept := u.snapshot(); !slices.Equal(kept, []string{"go", "rust"}) {
		t.Fatalf("没产出建议时候选保持原样：%v", kept)
	}

	// 空建议也是「产出了」：候选是非 nil 的空切片，reset 因此要作废令牌。
	token, _ = u.begin()
	u.finish(token, nil, true)
	before := u.token
	u.reset()
	if u.token != before+1 {
		t.Fatalf("有候选（含空切片）时 reset 应作废令牌：%d→%d", before, u.token)
	}

	// 令牌过期：什么也不写，也不放下新一轮的忙标记。
	stale, _ := u.begin()
	u.reset()
	fresh, _ := u.begin()
	if u.finish(stale, []string{"旧"}, true) {
		t.Fatal("过期的令牌不该写入")
	}
	if busy, got := u.snapshot(); !busy || len(got) != 0 {
		t.Fatalf("过期的结果不该碰新一轮：%v %v", busy, got)
	}
	u.finish(fresh, nil, false)
}

// allIn：空列表为真，缺任何一项为假。
func TestAllIn(t *testing.T) {
	cases := []struct {
		list, set []string
		want      bool
	}{
		{nil, nil, true},
		{nil, []string{"a"}, true},
		{[]string{"a"}, []string{"a", "b"}, true},
		{[]string{"a", "b"}, []string{"a"}, false},
		{[]string{"x"}, nil, false},
	}
	for _, c := range cases {
		if got := allIn(c.list, c.set); got != c.want {
			t.Errorf("allIn(%v, %v) = %v，应为 %v", c.list, c.set, got, c.want)
		}
	}
}

// 候选：重复的建议只出一枚；已带着的不出；新建议出来时不弹「都已存在」。
func TestTagSuggestDeduplicatesAndOnlyAnnouncesWhenAllExist(t *testing.T) {
	t.Parallel()
	r := aiRig(t, func(r *rig) { r.svc.SeedEntries(withTags(detailEntries(), detailNoteID, "go")) })
	r.svc.SeedTagSuggestions([]string{"alpha", "alpha", "go", "beta"}, nil)
	r.openTools(detailNoteID)
	r.openTagPopover()
	r.click(gfx.RoleButton, locales.LibraryTagsGenerate())
	if r.buttonCount("alpha") != 1 || r.buttonCount("beta") != 1 {
		t.Fatalf("重复的建议只该出一枚；画面文字 %q", r.session.Texts())
	}
	if r.hasText(locales.LibraryAiTagsAllExist()) {
		t.Fatal("还有新建议时不该弹「建议的标签都已存在」")
	}
}

// 生成失败：不弹「都已存在」（没有产出建议）。
func TestTagSuggestFailureDoesNotAnnounceAllExist(t *testing.T) {
	t.Parallel()
	r := aiRig(t)
	r.svc.SeedTagSuggestions(nil, errTagSuggest)
	r.openTools(detailNoteID)
	r.openTagPopover()
	r.click(gfx.RoleButton, locales.LibraryTagsGenerate())
	if r.hasText(locales.LibraryAiTagsAllExist()) || !r.hasText(locales.LibraryAiTagsFailed(errTagSuggest.Error())) {
		t.Fatalf("只该有失败提示；画面文字 %q", r.session.Texts())
	}
}

// 「生成」钮：空闲有图标、生成中是转圈代替图标（同宽）；忽略钮与候选的图标、悬停提示；没候选时不多占一块空区域。
func TestTagSuggestControlsShowIconsAndHints(t *testing.T) {
	t.Parallel()
	r := aiRig(t)
	r.svc.SeedTagSuggestions([]string{"alpha"}, nil)
	r.openTools(detailNoteID)
	r.openTagPopover()

	generate := locales.LibraryTagsGenerate()
	idle := r.extraWidth(gfx.RoleButton, generate)
	if idle < r.look.Rem(0.875) {
		t.Fatalf("空闲时「生成」钮上应有图标：多出的宽度只有 %.1f", idle)
	}
	gapBelow := func() float32 {
		panel := r.mustNode(gfx.RoleGroup, locales.LibraryEditTags())
		btn := r.mustNode(gfx.RoleButton, generate)
		return panel.Bounds.Y + panel.Bounds.Height - (btn.Bounds.Y + btn.Bounds.Height)
	}
	if gap := gapBelow(); gap > r.look.Space(3)+3 {
		t.Fatalf("没有候选时「生成」钮之下只该是浮层的内边距，实际 %.1f", gap)
	}

	wg := r.spawnTracker()
	release := r.once(r.svc.HoldSuggestTags())
	r.click(gfx.RoleButton, generate)
	busy := r.extraWidth(gfx.RoleButton, generate)
	release()
	wg.Wait()
	r.frames(3)
	if math.Abs(float64(busy-idle)) > 2 {
		t.Fatalf("生成中应是转圈代替图标（同宽）：空闲多出 %.1f，生成中多出 %.1f", idle, busy)
	}

	if !r.hasHint(locales.LibraryAiTagsDismiss()) {
		t.Fatal("忽略钮应带悬停提示")
	}
	chip := r.extraWidth(gfx.RoleButton, "alpha")
	if want := 2*r.look.Space(2) + r.look.Rem(0.75); chip < want {
		t.Fatalf("候选上应有加号图标：多出的宽度 %.1f，至少 %.1f", chip, want)
	}
}

// 在途时再触发生成什么也不做（令牌 begin 的早退），只请求一次。
func TestTagSuggestGenerateIgnoresATriggerWhileRunning(t *testing.T) {
	t.Parallel()
	r := aiRig(t)
	r.svc.SeedTagSuggestions([]string{"alpha"}, nil)
	wg := r.spawnTracker()
	release := r.once(r.svc.HoldSuggestTags())
	d := tagPickerData{ai: &tagSuggestUI{}, itemID: detailNoteID}
	r.screen.generateTagSuggestions(d, nil)
	r.screen.generateTagSuggestions(d, nil)
	release()
	wg.Wait()
	if got := r.callsWithPrefix("SuggestTags"); got != 1 {
		t.Fatalf("在途时再触发应被挡住，实际请求 %d 次", got)
	}
}

// 生成回来：唤醒界面重画；宿主没有提示条（空通知器）时「都已存在」也不能崩溃。
func TestTagSuggestWakesTheUIAndToleratesNoNotifier(t *testing.T) {
	t.Parallel()
	r := aiRig(t)
	var wakes atomic.Int32
	r.store.invalidate = func() { wakes.Add(1) }
	r.svc.SeedTagSuggestions([]string{"alpha"}, nil)
	r.screen.generateTagSuggestions(tagPickerData{ai: &tagSuggestUI{}, itemID: detailNoteID}, nil)
	if wakes.Load() == 0 {
		t.Fatal("生成回来后应唤醒界面")
	}

	r.screen.Guard = kit.Guard{Log: r.log} // 空通知器
	r.svc.SeedTagSuggestions(nil, nil)
	r.screen.generateTagSuggestions(tagPickerData{ai: &tagSuggestUI{}, itemID: detailNoteID}, nil) // 空建议 = 都已存在：不该 panic
}
