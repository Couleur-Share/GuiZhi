package themed

import (
	"errors"
	"strings"
	"testing"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/locales"
)

func TestPaneBodyTable(t *testing.T) {
	v3 := &services.ThemedPage{ID: "p", FormatVersion: 3}
	v1 := &services.ThemedPage{ID: "p", FormatVersion: 1}
	run := &services.ThemedTask{ID: "t", State: services.ThemedTaskRunning, Stage: services.ThemedStageDesign}
	done := &services.ThemedTask{ID: "t", State: services.ThemedTaskCompleted, Stage: services.ThemedStageDone}
	failed := &services.ThemedTask{ID: "t", State: services.ThemedTaskFailed, Stage: services.ThemedStageDesign}
	res := func(p *services.ThemedPage, doc string) *services.ThemedReadResult {
		return &services.ThemedReadResult{Page: p, Document: doc}
	}
	cases := []struct {
		name     string
		view     ReadingView
		original bool
		body     PaneBody
		strip    bool
		expanded bool
	}{
		{"读取中", ReadingView{ShowPage: true}, false, BodyNone, false, true},
		{"没有页面没有任务", ReadingView{ShowPage: true, Result: res(nil, "")}, false, BodyEmpty, false, true},
		{"首次生成中", ReadingView{ShowPage: true, Result: res(nil, ""), Task: run}, false, BodyTaskStatus, false, true},
		{"失败可重试", ReadingView{ShowPage: true, Result: res(nil, ""), Task: failed}, false, BodyTaskStatus, false, true},
		{"完成后载入成品", ReadingView{ShowPage: true, Result: res(nil, ""), Task: done}, false, BodyWaitDone, false, true},
		{"当前格式页面", ReadingView{ShowPage: true, Result: res(v3, ""), Task: done}, false, BodyNativePage, true, false},
		{"当前格式页面无任务", ReadingView{ShowPage: true, Result: res(v3, "")}, false, BodyNativePage, false, false},
		{"当前格式页面更新中", ReadingView{ShowPage: true, Result: res(v3, ""), Task: run}, false, BodyNativePage, true, false},
		{"旧格式页面", ReadingView{ShowPage: true, Result: res(v1, "<html/>"), Task: done}, false, BodyLegacyFrame, false, false},
		{"页面不显示时只显示原文", ReadingView{ShowPage: false, Result: res(v3, "")}, true, BodyOriginal, false, false},
		{"读取失败", ReadingView{ShowPage: true, LoadError: "boom", Result: res(nil, "")}, false, BodyNone, false, true},
	}
	for _, c := range cases {
		m := c.view.Pane(false, c.original)
		if m.Body != c.body || (c.body != BodyOriginal && (m.StatusStrip != c.strip || m.StatusExpanded != c.expanded)) {
			t.Errorf("%s: body=%v strip=%v expanded=%v，期望 %v/%v/%v", c.name, m.Body, m.StatusStrip, m.StatusExpanded, c.body, c.strip, c.expanded)
		}
	}
}

func TestPaneNotesAndToolbar(t *testing.T) {
	v3 := &services.ThemedPage{ID: "p", FormatVersion: 3}
	v := ReadingView{ShowPage: true, Result: &services.ThemedReadResult{Page: v3, Stale: true}}
	m := v.Pane(true, false)
	if !m.ReadOnlyNote || !m.StaleNote || m.LegacyNote || !m.Toolbar.Show || !m.Toolbar.Tools || m.Toolbar.StartOver {
		t.Fatalf("%+v", m)
	}
	v.Result.Page = &services.ThemedPage{ID: "p", FormatVersion: 2}
	if m := v.Pane(false, false); !m.LegacyNote {
		t.Fatal("旧格式提示只读")
	}
	// 没有页面、有已结束的任务：「重新生成」，只读时禁用
	v = ReadingView{ShowPage: true, Result: &services.ThemedReadResult{}, Task: &services.ThemedTask{State: services.ThemedTaskFailed}}
	m = v.Pane(false, false)
	if !m.Toolbar.StartOver || m.Toolbar.StartOverDisabled || m.Toolbar.Tools {
		t.Fatalf("%+v", m.Toolbar)
	}
	if m := v.Pane(true, false); !m.Toolbar.StartOverDisabled {
		t.Fatal("只读时重新生成禁用")
	}
	// 工具栏在页面不显示或什么也没有时不出现
	if (ReadingView{ShowPage: true, Result: &services.ThemedReadResult{}}).Pane(false, false).Toolbar.Show {
		t.Fatal("没页面没任务不显示工具栏")
	}
	if (ReadingView{ShowPage: false, Result: &services.ThemedReadResult{Page: v3}}).Pane(false, false).Toolbar.Show {
		t.Fatal("页面不显示时不显示工具栏")
	}
}

func TestSetupFormDefaultsAndRules(t *testing.T) {
	text, image := "gpt", "img"
	full := services.ThemedModels{Text: &text, Image: &image}
	f := NewSetupForm(nil, full, &services.ThemedSearchStatus{Configured: false, DefaultEnabled: true})
	if !f.Research || !f.GenerateImages || f.MaxImages != 3 || f.ResearchDepth != services.ThemedDepthStandard || !f.EnhancedInteraction {
		t.Fatalf("新建默认值: %+v", f)
	}
	if f.Configured() || !f.ResearchMissingSearch() || !f.NeedsConfigHint() {
		t.Fatal("默认联网但搜索未配置：不能提交，要提示配置")
	}
	if g := f.WithResearch(false); !g.Configured() || g.NeedsConfigHint() {
		t.Fatal("关掉联网后可以提交")
	}
	// 没有生图模型：默认不配图，仍可仅排版
	noImg := services.ThemedModels{Text: &text}
	f = NewSetupForm(nil, noImg, nil)
	if f.GenerateImages || !f.Configured() || !f.NeedsConfigHint() || !f.ConfigHintImageOptional() {
		t.Fatalf("无生图模型: %+v", f)
	}
	if f.WithGenerateImages(true).Configured() {
		t.Fatal("勾了配图但没有生图模型不能提交")
	}
	if NewSetupForm(nil, services.ThemedModels{}, nil).Configured() {
		t.Fatal("没有文本模型不能提交")
	}
}

func TestSetupFormAdjustAndOptions(t *testing.T) {
	text, image := "gpt", "img"
	off := false
	cur := &services.ThemedOptions{Style: "warm", EnhancedInteraction: &off, ResearchDepth: services.ThemedDepthDeep}
	f := NewSetupForm(cur, services.ThemedModels{Text: &text, Image: &image}, &services.ThemedSearchStatus{})
	if !f.Adjusting || f.Action != services.ThemedActionRedesign || f.Style != "warm" || f.EnhancedInteraction || f.ResearchDepth != services.ThemedDepthDeep || f.GenerateImages {
		t.Fatalf("调整沿用当前选项并默认不再生图: %+v", f)
	}
	if f.ShowsResearch() {
		t.Fatal("仅调整设计不涉及联网")
	}
	// 仅调整设计即使勾了联网也不要求搜索
	if !f.WithResearch(true).Configured() {
		t.Fatal("仅调整设计不要求搜索服务")
	}
	o := f.Options()
	if !o.FromCurrent || o.Action != services.ThemedActionRedesign || o.EnhancedInteraction == nil || *o.EnhancedInteraction || o.Style != "warm" {
		t.Fatalf("%+v", o)
	}
	if o := f.WithAction(services.ThemedActionRefresh).Options(); o.FromCurrent {
		t.Fatal("按最新原文重做不沿用当前内容")
	}
	if o := f.WithAction(services.ThemedActionRevise).Options(); !o.FromCurrent {
		t.Fatal("调整内容沿用当前内容")
	}
	if f.WithAction("bogus").Action != services.ThemedActionRedesign {
		t.Fatal("非法调整方式忽略")
	}
	if f.WithMaxImages(99).MaxImages != 5 || f.WithMaxImages(0).MaxImages != 1 {
		t.Fatal("图片数限制在 1 到 5")
	}
	if f.WithResearchDepth("bogus").ResearchDepth != services.ThemedDepthDeep {
		t.Fatal("非法查证深度忽略")
	}
}

func TestClampStyleUsesUTF16UnitsWithoutSplittingChars(t *testing.T) {
	long := strings.Repeat("字", 2500)
	if got := ClampStyle(long); len([]rune(got)) != StyleMaxUnits {
		t.Fatalf("BMP 字符按一个码元计: %d", len([]rune(got)))
	}
	// 补充平面字符占两个码元：2000 个码元 = 1000 个字符，且不能在边界劈开
	emoji := strings.Repeat("😀", 1500)
	got := ClampStyle(emoji)
	if n := len([]rune(got)); n != 1000 {
		t.Fatalf("应保留 1000 个完整字符: %d", n)
	}
	odd := "a" + strings.Repeat("😀", 1500)
	if n := len([]rune(ClampStyle(odd))); n != 1000 {
		t.Fatalf("边界落在代理对中间时丢掉半个字符（1 + 999 个字符）: %d", n)
	}
	if short := "短文本"; ClampStyle(short) != short {
		t.Fatal("未超限原样返回")
	}
}

func TestSubmitSetupClosesOnlyOnSuccess(t *testing.T) {
	r := newRig(t)
	r.open(reqA)
	r.reading.OpenSetup(SetupNew)
	form := r.reading.NewSetupForm(SetupNew).WithResearch(false).WithGenerateImages(false)
	r.svc.Fail(servicestest.ThemedOpGenerate, errors.New("busy backend"))
	if r.reading.SubmitSetup(form) || r.view().Setup != SetupNew {
		t.Fatal("失败时保留对话框，用户可调整后重试")
	}
	r.svc.Fail(servicestest.ThemedOpGenerate, nil)
	if !r.reading.SubmitSetup(form) || r.view().Setup != SetupNone {
		t.Fatal("成功后关闭对话框")
	}
	g := r.svc.Calls().Generates
	if len(g) != 2 || g[1].Opts.EnhancedInteraction == nil || g[1].Opts.Action != services.ThemedActionCreate {
		t.Fatalf("选项要显式传下去: %+v", g)
	}
	// 不满足前置条件：什么也不发
	r.svc.SetModels("", "")
	r.reading.Reload()
	bad := r.reading.NewSetupForm(SetupNew)
	n := len(r.svc.Calls().Generates)
	if r.reading.SubmitSetup(bad) || len(r.svc.Calls().Generates) != n {
		t.Fatal("没有文本模型不应发出请求")
	}
}

func TestAdjustFormUsesCurrentPageOptions(t *testing.T) {
	r := newRig(t)
	r.svc.PutPage(reqA, services.ThemedPage{Options: services.ThemedOptions{Style: "ink"}})
	r.open(reqA)
	f := r.reading.NewSetupForm(SetupAdjust)
	if f.Style != "ink" || !f.Adjusting {
		t.Fatalf("%+v", f)
	}
	if g := r.reading.NewSetupForm(SetupNew); g.Style != "" || g.Adjusting {
		t.Fatal("重新生成不沿用当前设计要求")
	}
}

func TestToolsMenuByState(t *testing.T) {
	ids := func(items []ToolItem) (out []ToolID) {
		for _, it := range items {
			out = append(out, it.ID)
		}
		return out
	}
	v3 := &services.ThemedPage{ID: "p", FormatVersion: 3}
	v := ReadingView{Result: &services.ThemedReadResult{Page: v3}}
	if got := ids(v.ToolsMenu(false)); len(got) != 8 || got[0] != ToolAdjust || got[len(got)-1] != ToolDelete {
		t.Fatalf("基础菜单: %v", got)
	}
	v.Result = &services.ThemedReadResult{Page: v3, Previous: true, Stale: true}
	got := ids(v.ToolsMenu(false))
	if !containsID(got, ToolRestore) || !containsID(got, ToolNotes) || !containsID(got, ToolExportStatic) {
		t.Fatalf("有上一版 / 过期时应有恢复与版本说明: %v", got)
	}
	// 旧格式：没有调整与静态导出
	v.Result = &services.ThemedReadResult{Page: &services.ThemedPage{FormatVersion: 1, Warnings: []string{"w"}}}
	got = ids(v.ToolsMenu(false))
	if containsID(got, ToolAdjust) || containsID(got, ToolExportStatic) || !containsID(got, ToolNotes) {
		t.Fatalf("旧格式菜单: %v", got)
	}
	// 生成中 / 只读：调整、重新生成、恢复、删除禁用；导出仍可用
	v = ReadingView{Result: &services.ThemedReadResult{Page: v3, Previous: true}, Task: &services.ThemedTask{State: services.ThemedTaskRunning}}
	for _, it := range v.ToolsMenu(false) {
		switch it.ID {
		case ToolAdjust, ToolRegenerate, ToolRestore, ToolDelete:
			if !it.Disabled {
				t.Errorf("生成中应禁用 %v", it.ID)
			}
		case ToolExport:
			if it.Disabled {
				t.Error("生成中仍可导出")
			}
		}
	}
	if (ReadingView{}).ToolsMenu(false) != nil {
		t.Fatal("没有页面没有菜单")
	}
}

func containsID(ids []ToolID, id ToolID) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func TestInvokeRespectsDisabledAndDispatches(t *testing.T) {
	r := newRig(t)
	r.svc.PutPage(reqA, services.ThemedPage{})
	r.svc.SetExportPath("C:/o.html")
	r.open(reqA)
	if !r.reading.Invoke(ToolAdjust, false) || r.view().Setup != SetupAdjust {
		t.Fatal("调整打开设置")
	}
	r.reading.CloseSetup()
	if !r.reading.Invoke(ToolRegenerate, false) || r.view().Setup != SetupNew {
		t.Fatal("重新生成打开设置")
	}
	r.reading.CloseSetup()
	if r.reading.Invoke(ToolDelete, true) || r.view().DeleteConfirm {
		t.Fatal("只读时删除被禁用，点了也不能生效")
	}
	if !r.reading.Invoke(ToolDelete, false) || !r.view().DeleteConfirm {
		t.Fatal("删除先确认")
	}
	r.reading.CancelDelete()
	r.reading.Invoke(ToolExportStatic, false)
	if e := r.svc.Calls().Exports; len(e) != 1 || !e[0].StaticOnly {
		t.Fatalf("%+v", e)
	}
	if r.reading.Invoke(ToolRestore, false) {
		t.Fatal("没有上一版时没有恢复项")
	}
}

func TestReferencesPanelLoadsAndRetries(t *testing.T) {
	r := newRig(t)
	r.svc.PutPage(reqA, services.ThemedPage{})
	r.svc.SetReferences(reqA, services.ThemedReference{ID: "r1", Title: "Doc", URL: "https://example.com", CapturedAt: 1_700_000_000_000, Status: "ready"})
	r.open(reqA)
	r.svc.Fail(servicestest.ThemedOpReferences, errors.New("offline"))
	r.reading.OpenPanel(PanelReferences)
	v := r.view()
	if v.Panel != PanelReferences || v.Refs.Error != "offline" || v.Refs.Loading {
		t.Fatalf("读取失败要显示可重试的错误态，不能当作没有资料: %+v", v.Refs)
	}
	r.svc.Fail(servicestest.ThemedOpReferences, nil)
	r.reading.OpenPanel(PanelReferences)
	v = r.view()
	if v.Refs.Error != "" || len(v.Refs.Items) != 1 || v.Refs.Items[0].Title != "Doc" {
		t.Fatalf("重试后应读到: %+v", v.Refs)
	}
	if got := ReferenceDate(v.Refs.Items[0]); !strings.Contains(got, "2023-11-") {
		t.Fatalf("日期文案: %q", got)
	}
	r.reading.ClosePanel()
	if r.view().Panel != PanelNone {
		t.Fatal("应关闭")
	}
	// 没有页面不能打开面板
	r.open(reqB)
	r.reading.OpenPanel(PanelAssets)
	if r.view().Panel != PanelNone {
		t.Fatal("没有页面不打开面板")
	}
}

func TestReferencesLateResponseAfterSwitchIgnored(t *testing.T) {
	r := newRig(t)
	r.svc.PutPage(reqA, services.ThemedPage{})
	r.svc.PutPage(reqB, services.ThemedPage{})
	r.svc.SetReferences(reqA, services.ThemedReference{ID: "ra", Title: "A-doc"})
	r.open(reqA)
	release, arrived := r.svc.Hold(servicestest.ThemedOpReferences)
	done := make(chan struct{})
	go func() { r.reading.OpenPanel(PanelReferences); close(done) }()
	<-arrived
	r.open(reqB)
	release()
	<-done
	if v := r.view(); len(v.Refs.Items) != 0 || v.Panel != PanelNone {
		t.Fatalf("A 的资料不能串到 B: %+v", v.Refs)
	}
}

func TestAssetRows(t *testing.T) {
	page := &services.ThemedPage{FormatVersion: 3, Assets: []services.ThemedAsset{
		{ID: "g-ready", Role: services.ThemedAssetGenerated, Status: services.ThemedAssetReady, FileName: "a.png", Purpose: "封面"},
		{ID: "o-ready", Role: services.ThemedAssetOriginal, Status: services.ThemedAssetReady, FileName: "b.png", Alt: "alt"},
		{ID: "o-failed", Role: services.ThemedAssetOriginal, Status: services.ThemedAssetFailed, Error: "404"},
	}}
	v := ReadingView{Result: &services.ThemedReadResult{Page: page}}
	rows := v.AssetRows(false)
	if len(rows) != 3 || !rows[0].Redo || rows[0].RedoLabel != locales.ThemedReadingRedraw() || rows[0].ThumbFile != "a.png" || rows[0].Caption != "封面" {
		t.Fatalf("生成图可重做: %+v", rows[0])
	}
	if rows[1].Redo || rows[1].Caption != "alt" {
		t.Fatalf("成功的原图没有重做按钮，无用途时用替代文字: %+v", rows[1])
	}
	if !rows[2].Redo || rows[2].RedoLabel != locales.ThemedReadingRetryOriginal() || rows[2].ThumbFile != "" {
		t.Fatalf("失败的原图可重试: %+v", rows[2])
	}
	if rows := v.AssetRows(true); !rows[0].RedoDisabled {
		t.Fatal("只读时重做禁用")
	}
	page.FormatVersion = 1
	for _, row := range v.AssetRows(false) {
		if row.Redo {
			t.Fatal("旧格式页面没有重做")
		}
	}
}

func TestNotesModel(t *testing.T) {
	v := ReadingView{Result: &services.ThemedReadResult{Page: &services.ThemedPage{Warnings: []string{"w1", "w2"}}, Stale: true}}
	n := v.Notes()
	if n.StaleDetail == "" || len(n.Warnings) != 2 {
		t.Fatalf("%+v", n)
	}
	if (ReadingView{}).Notes().StaleDetail != "" {
		t.Fatal("没有页面没有说明")
	}
}
