package kit

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"

	"guizhi-mygo-shell/native/kit/icons"
	"guizhi-mygo-shell/native/locales"
)

// testSource 是按标题包含关系过滤的静态来源，并记下收到的 QueryChanged。
type testSource struct {
	mu      sync.Mutex
	all     []PaletteItem
	queries []string
}

func (s *testSource) Items(q string) []PaletteItem {
	q = strings.ToLower(strings.TrimSpace(q))
	var out []PaletteItem
	for _, it := range s.all {
		if q == "" || strings.Contains(strings.ToLower(it.Title), q) {
			out = append(out, it)
		}
	}
	return out
}

func (s *testSource) QueryChanged(q string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queries = append(s.queries, q)
}

func (s *testSource) seen() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.queries...)
}

// paletteFixture 是三条动作 + 两条设置，分两个组。
func paletteFixture(ran map[string]int) *testSource {
	run := func(k string) func() { return func() { ran[k]++ } }
	return &testSource{all: []PaletteItem{
		{Key: "action:capture", Group: "常用动作", Title: "快速采集", Icon: icons.Sparkles, Run: run("capture")},
		{Key: "action:inbox", Group: "常用动作", Title: "打开处理中心", Run: run("inbox")},
		{Key: "action:backup", Group: "常用动作", Title: "立即完整备份", Subtitle: "备份知识库与设置", Icon: icons.DatabaseBackup, Run: run("backup")},
		{Key: "setting:ai", Group: "设置", Title: "AI 模型服务", Icon: icons.Settings, Run: run("ai")},
		{Key: "setting:data", Group: "设置", Title: "数据与备份", Icon: icons.Settings, Run: run("data")},
	}}
}

func newPaletteTester(t *testing.T, l *Look, p *Palette) *ui.Tester {
	t.Helper()
	return newDialogTester(t, l, nil, func(c *ui.Context) { p.View(c, l) })
}

func selectedOption(tr *ui.Tester, titles ...string) string {
	for _, title := range titles {
		if a, ok := tr.AccessByRole(ui.RoleOption, title); ok && a.Selected {
			return title
		}
	}
	return ""
}

var fixtureTitles = []string{"快速采集", "打开处理中心", "立即完整备份", "AI 模型服务", "数据与备份"}

func TestPaletteOpenShowsGroupedResultsAndFocusesInput(t *testing.T) {
	l := lightLook()
	src := paletteFixture(map[string]int{})
	p := NewPalette(src)
	tr := newPaletteTester(t, l, p)
	if tr.HasText(locales.CommandPalettePlaceholder()) {
		t.Fatal("没打开时不该画面板")
	}
	p.Open()
	tr.Frame()

	if _, ok := tr.AccessByRole(ui.RoleDialog, locales.CommandPaletteTitle()); !ok {
		t.Fatalf("应有名为「%s」的对话框：%v", locales.CommandPaletteTitle(), tr.Texts())
	}
	for _, s := range append([]string{"常用动作", "设置"}, fixtureTitles...) {
		if !tr.HasText(s) {
			t.Errorf("缺少 %q", s)
		}
	}
	if !tr.HasText("备份知识库与设置") {
		t.Error("应显示副标题")
	}
	if !tr.Focused(locales.CommandPalettePlaceholder()) {
		t.Error("打开后焦点应在输入框：直接打字就是搜索")
	}
	if got := selectedOption(tr, fixtureTitles...); got != "快速采集" {
		t.Errorf("初始高亮应是第一项，得到 %q", got)
	}
	// 读屏：高亮项是输入框的 ActiveDescendant，每个选项都有名字
	if a, ok := tr.AccessByRole(ui.RoleOption, "快速采集"); !ok || !a.Focused {
		t.Errorf("高亮项应被读屏当作焦点所在：%+v ok=%v", a, ok)
	}
	for _, a := range tr.AccessAll() {
		if a.Role == ui.RoleOption && a.Name == "" {
			t.Errorf("%v 处有个没有名字的选项", a.Bounds)
		}
	}
}

func TestPaletteTypingFiltersAndTellsSourceOnce(t *testing.T) {
	l := lightLook()
	src := paletteFixture(map[string]int{})
	p := NewPalette(src)
	tr := newPaletteTester(t, l, p)
	p.Open()
	tr.Frame()

	tr.Type("备份")
	tr.Frame()
	if p.Query() != "备份" {
		t.Fatalf("查询 = %q", p.Query())
	}
	for _, gone := range []string{"快速采集", "打开处理中心", "AI 模型服务"} {
		if tr.HasText(gone) {
			t.Errorf("%q 不匹配，不应出现", gone)
		}
	}
	for _, kept := range []string{"立即完整备份", "数据与备份"} {
		if !tr.HasText(kept) {
			t.Errorf("%q 应保留", kept)
		}
	}
	// 输入这一帧里结果就是新查询的，不落后一帧
	if got := src.seen(); len(got) != 1 || got[0] != "备份" {
		t.Errorf("来源应只被告知一次最终查询：%v", got)
	}

	tr.Type("zzz")
	tr.Frame()
	if !tr.HasText(locales.CommandPaletteNoResults()) {
		t.Errorf("没有匹配时应显示「%s」：%v", locales.CommandPaletteNoResults(), tr.Texts())
	}
	// 有结果时回车无事可做，也不能崩
	tr.Key(0, ui.KeyEnter)
	tr.Frame()
	if !p.Visible() {
		t.Error("没有结果时回车不应关闭面板")
	}
}

func TestPaletteArrowsMoveAndStopAtEnds(t *testing.T) {
	l := lightLook()
	p := NewPalette(paletteFixture(map[string]int{}))
	tr := newPaletteTester(t, l, p)
	p.Open()
	tr.Frame()

	tr.Key(0, ui.KeyUp)
	tr.Frame()
	if got := selectedOption(tr, fixtureTitles...); got != "快速采集" {
		t.Errorf("在第一项按 ↑ 应停住，得到 %q", got)
	}
	tr.Key(0, ui.KeyDown)
	tr.Key(0, ui.KeyDown)
	tr.Frame()
	if got := selectedOption(tr, fixtureTitles...); got != "立即完整备份" {
		t.Errorf("↓↓ 后应在第三项，得到 %q", got)
	}
	for i := 0; i < 10; i++ {
		tr.Key(0, ui.KeyDown)
	}
	tr.Frame()
	if got := selectedOption(tr, fixtureTitles...); got != "数据与备份" {
		t.Errorf("到最后一项应停住（不循环），得到 %q", got)
	}
	if !tr.Focused(locales.CommandPalettePlaceholder()) {
		t.Error("方向键不该把焦点带出输入框")
	}
}

func TestPaletteHighlightResetsWhenResultsChange(t *testing.T) {
	l := lightLook()
	p := NewPalette(paletteFixture(map[string]int{}))
	tr := newPaletteTester(t, l, p)
	p.Open()
	tr.Frame()
	tr.Key(0, ui.KeyDown)
	tr.Key(0, ui.KeyDown)
	tr.Frame()
	tr.Type("备份")
	tr.Frame()
	if got := selectedOption(tr, "立即完整备份", "数据与备份"); got != "立即完整备份" {
		t.Errorf("查询变化后高亮应回到第一项，得到 %q", got)
	}
}

func TestPaletteEnterRunsHighlightedAndCloses(t *testing.T) {
	l := lightLook()
	ran := map[string]int{}
	p := NewPalette(paletteFixture(ran))
	tr := newPaletteTester(t, l, p)
	p.Open()
	tr.Frame()
	tr.Key(0, ui.KeyDown)
	tr.Key(0, ui.KeyEnter)
	tr.Frame()
	if ran["inbox"] != 1 || len(ran) != 1 {
		t.Fatalf("应只执行第二项：%v", ran)
	}
	if p.Visible() {
		t.Error("执行后面板应关闭")
	}
}

func TestPaletteCloseTellsSourceToCancel(t *testing.T) {
	l := lightLook()
	src := paletteFixture(map[string]int{})
	p := NewPalette(src)
	tr := newPaletteTester(t, l, p)
	p.Open()
	tr.Frame()
	tr.Type("备份")
	tr.Frame()
	tr.Key(0, ui.KeyEscape)
	tr.Frame()
	got := src.seen()
	if len(got) != 2 || got[0] != "备份" || got[1] != "" {
		t.Errorf("来源应先后收到「备份」与关闭时的清空（取消后台搜索）：%v", got)
	}
}

func TestPaletteClickRunsThatRow(t *testing.T) {
	l := lightLook()
	ran := map[string]int{}
	p := NewPalette(paletteFixture(ran))
	tr := newPaletteTester(t, l, p)
	p.Open()
	tr.Frame()
	if err := tr.Click("数据与备份"); err != nil {
		t.Fatal(err)
	}
	tr.Frame()
	if ran["data"] != 1 || len(ran) != 1 || p.Visible() {
		t.Errorf("点击应执行那一行并关闭：%v 打开 %v", ran, p.Visible())
	}
}

func TestPaletteEscapeAndBackdropClose(t *testing.T) {
	l := lightLook()
	for name, dismiss := range map[string]func(tr *ui.Tester){
		"Esc": func(tr *ui.Tester) { tr.Key(0, ui.KeyEscape) },
		"点遮罩": func(tr *ui.Tester) { tr.ClickAt(880, 480) },
	} {
		ran := map[string]int{}
		p := NewPalette(paletteFixture(ran))
		tr := newPaletteTester(t, l, p)
		p.Open()
		tr.Frame()
		tr.Type("备")
		tr.Frame()
		dismiss(tr)
		tr.Frame()
		if p.Visible() || len(ran) != 0 {
			t.Errorf("%s：打开 %v，执行 %v", name, p.Visible(), ran)
		}
	}
}

func TestPaletteReopenStartsFresh(t *testing.T) {
	l := lightLook()
	p := NewPalette(paletteFixture(map[string]int{}))
	tr := newPaletteTester(t, l, p)
	p.Open()
	tr.Frame()
	tr.Type("备份")
	tr.Frame()
	tr.Key(0, ui.KeyEscape)
	tr.Frame()
	p.Open()
	tr.Frame()
	if p.Query() != "" {
		t.Errorf("重新打开应清空查询：%q", p.Query())
	}
	if !tr.HasText("快速采集") {
		t.Error("重新打开应显示全部结果")
	}
	if !tr.Focused(locales.CommandPalettePlaceholder()) {
		t.Error("重新打开焦点应在输入框")
	}
}

func TestPaletteRestoresFocusOnClose(t *testing.T) {
	l := lightLook()
	p := NewPalette(paletteFixture(map[string]int{}))
	tr := newPaletteTester(t, l, p)
	if err := tr.Click("页面按钮"); err != nil {
		t.Fatal(err)
	}
	if !tr.Focused("页面按钮") {
		t.Skip("点击没有让页面按钮取得焦点，无从比较")
	}
	p.Open()
	tr.Frame()
	tr.Key(0, ui.KeyEscape)
	tr.Frame()
	tr.Frame()
	if !tr.Focused("页面按钮") {
		t.Error("关闭后焦点应回到打开前的元素")
	}
}

func TestPaletteHoverMovesHighlightOnlyOnEntry(t *testing.T) {
	l := lightLook()
	p := NewPalette(paletteFixture(map[string]int{}))
	tr := newPaletteTester(t, l, p)
	p.Open()
	tr.Frame()

	r, ok := tr.Find("AI 模型服务")
	if !ok {
		t.Fatal("找不到 AI 模型服务这一行")
	}
	tr.Move(r.X+r.W/2, r.Y+r.H/2)
	tr.Frame()
	tr.Frame()
	if got := selectedOption(tr, fixtureTitles...); got != "AI 模型服务" {
		t.Fatalf("指针移进一行，高亮应跟过去，得到 %q", got)
	}
	// 指针不动，键盘把高亮移走：不能被静止的指针抢回去
	tr.Key(0, ui.KeyDown)
	tr.Frame()
	tr.Frame()
	if got := selectedOption(tr, fixtureTitles...); got != "数据与备份" {
		t.Errorf("键盘移走的高亮不该被静止的指针抢回，得到 %q", got)
	}
}

func TestPaletteLongListScrollsAndKeepsActiveVisible(t *testing.T) {
	l := lightLook()
	src := &testSource{}
	for i := 0; i < 40; i++ {
		src.all = append(src.all, PaletteItem{Key: fmt.Sprintf("k%d", i), Group: "条目", Title: fmt.Sprintf("条目 %02d", i), Run: func() {}})
	}
	p := NewPalette(src)
	tr := newPaletteTester(t, l, p)
	p.Open()
	tr.Frame()

	dlg, ok := tr.AccessByRole(ui.RoleDialog, locales.CommandPaletteTitle())
	if !ok {
		t.Fatal("找不到面板")
	}
	if maxH := float32(500*paletteMaxFrac) + paletteInputH + 2; dlg.Bounds.H > maxH+1 {
		t.Errorf("面板高度 %.0f 超过窗口的 60%% + 输入行（%.0f）", dlg.Bounds.H, maxH)
	}
	for i := 0; i < 35; i++ {
		tr.Key(0, ui.KeyDown)
	}
	tr.Frame()
	tr.Frame()
	a, ok := tr.AccessByRole(ui.RoleOption, "条目 35")
	if !ok {
		t.Fatal("找不到高亮的第 36 项")
	}
	if !a.Selected {
		t.Fatalf("第 36 项应高亮")
	}
	if a.Offscreen || a.Bounds.Y < dlg.Bounds.Y || a.Bounds.Y+a.Bounds.H > dlg.Bounds.Y+dlg.Bounds.H+1 {
		t.Errorf("键盘移到的高亮项应滚进可视区：项 %+v，面板 %+v", a.Bounds, dlg.Bounds)
	}
}

func TestPaletteShortListIsAsTallAsItsContent(t *testing.T) {
	l := lightLook()
	p := NewPalette(paletteFixture(map[string]int{}))
	tr := newPaletteTester(t, l, p)
	p.Open()
	tr.Frame()
	dlg, _ := tr.AccessByRole(ui.RoleDialog, locales.CommandPaletteTitle())
	if dlg.Bounds.H > 400 {
		t.Errorf("5 条结果不该撑满 60%% 高度：面板高 %.0f", dlg.Bounds.H)
	}
	if dlg.Bounds.W < 600 || dlg.Bounds.W > 700 {
		t.Errorf("面板宽度应约 672：%.0f", dlg.Bounds.W)
	}
}

func TestPaletteDarkModeRenders(t *testing.T) {
	l := darkLook()
	p := NewPalette(paletteFixture(map[string]int{}))
	tr := newPaletteTester(t, l, p)
	p.Open()
	tr.Frame()
	if !tr.HasText("快速采集") {
		t.Fatal("暗色下应画出面板")
	}
}
