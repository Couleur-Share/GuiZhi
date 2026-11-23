package overlays

import (
	"errors"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/locales"
)

// 命令面板界面（L4）：顶部对齐的搜索浮层、打开即聚焦、分组结果、键盘与点击执行、Esc 与点遮罩关闭、空态与搜索中。

func newPaletteUI(t *testing.T) *uiRig {
	t.Helper()
	r := newUIRig(t, withPalette)
	r.screen.Palette.Open()
	r.frames(3)
	return r
}

// seedPaletteRemote 摆好远程来源的结果（与 palette_test 的 seedRemote 同形）。
func (r *uiRig) seedPaletteRemote() {
	r.lib.SeedEntries([]services.LibraryEntry{{ID: "i1", Title: "alpha note", Snippet: "first"}, {ID: "i2", Title: "other", Snippet: "alpha inside"}})
	r.wiki.PutPage(services.WikiPage{ID: "w1", Title: "alpha wiki", Summary: "wiki summary"})
	r.imports.Set(services.ImportPage{Entries: []services.ImportTask{
		{ID: "t1", DisplayName: "alpha task", Status: "failed", Error: "boom"},
	}}, nil)
}

// searchPalette 在搜索框里打字并让防抖到期（取最近排的那个定时器）。
func (r *uiRig) searchPalette(query string) {
	r.t.Helper()
	r.typeText(query)
	if r.timers.count() == 0 {
		r.t.Fatalf("输入 %q 后应排了防抖", query)
	}
	r.timers.fire(r.timers.count() - 1)
	r.frames(3)
}

func (r *uiRig) paletteOpen() bool { return r.has(gfx.RoleGroup, locales.CommandPaletteTitle()) }

func TestUIPaletteClosedDrawsNothing(t *testing.T) {
	r := newUIRig(t, withPalette)
	if r.paletteOpen() || r.hasText(locales.CommandPalettePlaceholder()) {
		t.Fatal("没打开时不应画面板")
	}
}

// 打开：面板顶部对齐（pt-[12vh] = 96）、宽 42rem = 672 水平居中，带搜索框、分组标题与静态动作；没有按键提示行（渲染层没有这一条）。
func TestUIPaletteOpensTopAlignedWithStaticItems(t *testing.T) {
	r := newPaletteUI(t)
	panel, ok := r.node(gfx.RoleGroup, locales.CommandPaletteTitle())
	if !ok {
		r.dump()
		t.Fatal("没有找到面板")
	}
	b := panel.Bounds
	if math.Abs(float64(b.Y-96)) > 1 || math.Abs(float64(b.Width-672)) > 1 || math.Abs(float64(b.X+b.Width/2-640)) > 1 {
		t.Errorf("面板位置不对：%+v，期望 y=96 宽 672 居中", b)
	}
	if !r.has(gfx.RoleTextField, locales.CommandPalettePlaceholder()) {
		t.Error("应有搜索框")
	}
	for _, name := range []string{locales.CommandPaletteGroupActions(), locales.SettingsTitle()} {
		if !r.hasText(name) {
			t.Errorf("缺少文字 %q；画面上有 %q", name, r.session.Texts())
		}
	}
	if r.hasText(locales.NativePaletteHintMove()) || r.hasText(locales.NativePaletteHintRun()) || r.hasText(locales.NativePaletteHintClose()) {
		t.Errorf("渲染层的命令面板没有按键提示行，Viva 不应画：%q", r.session.Texts())
	}
	if !r.hasRow(locales.CommandPaletteCompileWiki()) || !r.hasText(locales.CommandPalettePlaceholder()) {
		t.Errorf("应列出静态动作并显示占位文字：%q", r.session.Texts())
	}
}

// 打开即聚焦：不点搜索框，直接打字就进输入框，并驱动 Palette 的查询。
func TestUIPaletteAutoFocusTypesIntoSearch(t *testing.T) {
	r := newPaletteUI(t)
	r.typeText("wiki")
	if got := r.screen.Palette.View().Query; got != "wiki" {
		t.Fatalf("打字应直接进搜索框并驱动查询：%q", got)
	}
	if !r.hasRow(locales.CommandPaletteCompileWiki()) || r.hasRow(locales.CaptureTitle()) {
		t.Errorf("静态动作应按输入立即过滤：%q", r.session.Texts())
	}
}

// 重新打开是空白搜索框。
func TestUIPaletteReopenClearsQuery(t *testing.T) {
	r := newPaletteUI(t)
	r.typeText("wiki")
	r.keys("Escape")
	if r.paletteOpen() {
		t.Fatal("Esc 应关闭")
	}
	r.screen.Palette.Open()
	r.frames(3)
	if got := r.screen.Palette.View().Query; got != "" {
		t.Errorf("重新打开应清空查询：%q", got)
	}
	if !r.hasRow(locales.CaptureTitle()) {
		t.Error("重新打开应回到全部静态项")
	}
}

// 搜索：防抖到期后按来源分组出现，副标题与分组标题齐全。
func TestUIPaletteSearchShowsGroupedResults(t *testing.T) {
	r := newPaletteUI(t)
	r.seedPaletteRemote()
	r.typeText("alpha")
	if !r.hasText(locales.NativePaletteSearching()) {
		t.Errorf("防抖等待期间应显示「正在搜索」：%q", r.session.Texts())
	}
	r.timers.fire(r.timers.count() - 1)
	r.frames(3)
	for _, name := range []string{"alpha note", "other", "alpha wiki", "alpha task"} {
		if !r.hasRow(name) {
			t.Errorf("缺少结果 %q；画面上有 %q", name, r.session.Texts())
		}
	}
	if !r.hasText(locales.CommandPaletteGroupKnowledge()) || !r.hasText("Wiki") || !r.hasText(locales.ImportsTitle()) {
		t.Errorf("应有三个来源的分组标题：%q", r.session.Texts())
	}
	if r.hasText(locales.NativePaletteSearching()) {
		t.Error("搜索完成后不应再显示「正在搜索」")
	}
}

// 搜不到：静态与远程都没有时是「没有匹配结果」；等待期间说「正在搜索」，不是「没有结果」。
func TestUIPaletteEmptyAndSearchingStates(t *testing.T) {
	r := newPaletteUI(t)
	r.typeText("zzzz-nothing")
	if !r.hasText(locales.NativePaletteSearching()) || r.hasText(locales.CommandPaletteNoResults()) {
		t.Errorf("搜索中应说「正在搜索」而不是「没有匹配结果」：%q", r.session.Texts())
	}
	r.timers.fire(r.timers.count() - 1)
	r.frames(3)
	if !r.hasText(locales.CommandPaletteNoResults()) || r.hasText(locales.NativePaletteSearching()) {
		t.Errorf("搜完无结果应说「没有匹配结果」：%q", r.session.Texts())
	}
}

// 部分来源搜索失败：底部给出说明，其余来源照常显示。
func TestUIPaletteFailedSourceHint(t *testing.T) {
	r := newPaletteUI(t)
	r.seedPaletteRemote()
	r.wiki.Fail(servicestest.WikiOpSearch, errors.New("wiki index offline"))
	r.searchPalette("alpha")
	if !r.hasText(locales.NativePaletteFailedSources("Wiki")) {
		t.Errorf("应说明 Wiki 搜索失败：%q", r.session.Texts())
	}
	if !r.hasRow("alpha note") || r.hasRow("alpha wiki") {
		t.Error("其余来源应照常显示、失败来源没有结果")
	}
}

// 键盘：↓ 移动选中项，Enter 执行选中项并关闭；↑ 从头回绕不越界。
func TestUIPaletteArrowKeysAndEnterRun(t *testing.T) {
	r := newPaletteUI(t)
	if r.screen.Palette.View().Active != 0 {
		t.Fatal("打开时选中第一项")
	}
	r.keys("Down")
	if got := r.screen.Palette.View().Active; got != 1 {
		t.Fatalf("↓ 应选中第二项：%d", got)
	}
	r.keys("Up")
	r.keys("Up")
	if got := r.screen.Palette.View().Active; got < 0 {
		t.Fatalf("↑ 不应越界：%d", got)
	}
	r.keys("Home") // 不相关的键不应被吞掉或改变选中项以外的状态
	r.screen.Palette.SetActive(1)
	r.frames(2)
	r.keys("Enter")
	if got := r.called(); !slices.Equal(got, []string{"inbox"}) {
		t.Fatalf("Enter 应执行第二项「打开处理中心」：%v", got)
	}
	if r.paletteOpen() {
		t.Error("执行后应关闭面板")
	}
}

// 点击一行执行并关闭。
func TestUIPaletteClickRunsRow(t *testing.T) {
	r := newPaletteUI(t)
	r.clickRow(locales.CaptureTitle())
	if got := r.called(); !slices.Equal(got, []string{"capture"}) {
		t.Fatalf("点击应执行「快速采集」：%v", got)
	}
	if r.paletteOpen() {
		t.Error("执行后应关闭面板")
	}
}

// 搜索结果里的知识条目：点击打开该条目。
func TestUIPaletteClickKnowledgeResultOpensItem(t *testing.T) {
	r := newPaletteUI(t)
	r.seedPaletteRemote()
	r.searchPalette("alpha")
	r.clickRow("alpha note")
	if got := r.called(); !slices.Equal(got, []string{"item:i1"}) {
		t.Fatalf("应打开条目 i1：%v", got)
	}
	if r.paletteOpen() {
		t.Error("执行后应关闭面板")
	}
}

// 指针进入一行把它设为选中项；键盘移动之后停在原处的指针不把选中项拉回去。
func TestUIPaletteHoverSelectsButKeyboardWins(t *testing.T) {
	r := newPaletteUI(t)
	n, ok := r.node(gfx.RoleButton, locales.SettingsFullBackupNow())
	if !ok {
		t.Fatal("找不到备份动作")
	}
	if err := r.session.Move(n.Bounds.X+20, n.Bounds.Y+n.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	r.frames(3)
	if got := r.screen.Palette.View().Active; got != 2 {
		t.Fatalf("指针进入第三行应选中它：%d", got)
	}
	r.keys("Down")
	r.frames(2)
	if got := r.screen.Palette.View().Active; got != 3 {
		t.Fatalf("键盘移动之后选中项不应被停着的指针拉回：%d", got)
	}
}

// Esc 关闭；点遮罩关闭；点面板内的空白不关闭。
func TestUIPaletteEscAndScrimClose(t *testing.T) {
	r := newPaletteUI(t)
	r.keys("Escape")
	if r.paletteOpen() || r.screen.Palette.View().Open {
		t.Fatal("Esc 应关闭")
	}

	r.screen.Palette.Open()
	r.frames(3)
	r.clickAt(20, 20) // 面板之外
	if r.paletteOpen() || r.screen.Palette.View().Open {
		t.Fatal("点遮罩应关闭")
	}

	r.screen.Palette.Open()
	r.frames(3)
	panel, _ := r.node(gfx.RoleGroup, locales.CommandPaletteTitle())
	r.clickAt(panel.Bounds.X+panel.Bounds.Width-6, panel.Bounds.Y+panel.Bounds.Height-6) // 底部提示条空白
	if !r.paletteOpen() {
		t.Error("点面板内部不应关闭")
	}
}

// 结果多到超出 60vh：列表滚动，键盘选到最后一项时它被滚进视口。窗口矮一些，15 条结果就放不下。
func TestUIPaletteKeyboardRevealsActiveRow(t *testing.T) {
	r := newPaletteUI(t)
	if err := r.session.Resize(1280, 420); err != nil {
		t.Fatal(err)
	}
	entries := make([]services.LibraryEntry, 0, 5)
	tasks := make([]services.ImportTask, 0, 5)
	for i := 0; i < 5; i++ {
		id := string(rune('a' + i))
		entries = append(entries, services.LibraryEntry{ID: "i" + id, Title: "needle item " + id})
		r.wiki.PutPage(services.WikiPage{ID: "w" + id, Title: "needle page " + id})
		tasks = append(tasks, services.ImportTask{ID: "t" + id, DisplayName: "needle task " + id, Status: "done"})
	}
	r.lib.SeedEntries(entries)
	r.imports.Set(services.ImportPage{Entries: tasks}, nil)
	r.searchPalette("needle")
	count := len(r.screen.Palette.View().Items)
	if count != 15 {
		t.Fatalf("应有 15 条结果：%d", count)
	}
	first, last := r.screen.Palette.View().Items[0], r.screen.Palette.View().Items[count-1]
	if _, ok := r.rowNode(last.Title); ok {
		if n, _ := r.rowNode(last.Title); n.Bounds.Y+n.Bounds.Height <= 420 {
			t.Fatalf("测试台应让最后一项一开始在视口之外：%+v", n.Bounds)
		}
	}
	for i := 0; i < count-1; i++ {
		r.keys("Down")
	}
	r.frames(6)
	n, ok := r.rowNode(last.Title)
	if !ok {
		t.Fatalf("选中到最后一项后它应在画面上：%q", last.Title)
	}
	if n.Bounds.Y+n.Bounds.Height > 420 {
		t.Errorf("最后一项应滚进视口：%+v", n.Bounds)
	}
	for i := 0; i < count-1; i++ {
		r.keys("Up")
	}
	r.frames(6)
	if n, ok := r.rowNode(first.Title); !ok || n.Bounds.Y < 0 {
		t.Errorf("回到第一项后它应在视口里：%+v", n.Bounds)
	}
}

// hasRow 报告面板里有没有这一行：行的无障碍名是「标题 副标题」（渲染层的行是一个整体的按钮，名字取全部文字），所以按标题前缀匹配。
func (r *uiRig) hasRow(title string) bool {
	_, ok := r.rowNode(title)
	return ok
}

func (r *uiRig) rowNode(title string) (app.AccessNode, bool) {
	for _, n := range r.session.Access() {
		if n.Role == gfx.RoleButton && (n.Label == title || strings.HasPrefix(n.Label, title+" ")) {
			return n, true
		}
	}
	return app.AccessNode{}, false
}

// clickRow 点击面板里的一行。
func (r *uiRig) clickRow(title string) {
	r.t.Helper()
	n, ok := r.rowNode(title)
	if !ok {
		r.t.Fatalf("找不到行「%s」；画面上有 %q", title, r.session.Texts())
	}
	r.clickAt(n.Bounds.X+n.Bounds.Width/2, n.Bounds.Y+n.Bounds.Height/2)
}
