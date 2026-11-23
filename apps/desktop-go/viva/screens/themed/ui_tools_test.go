package themed

import (
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// withPage 是已有一份当前页面、并已选中 AI 阅读的测试台。
func withPage(t *testing.T, mutate ...func(*services.ThemedPage)) *uiRig {
	t.Helper()
	r := newUIRig(t, withIsland)
	r.putPage(mutate...)
	r.activate()
	return r
}

func TestToolsMenuListsItemsInOrder(t *testing.T) {
	r := withPage(t, func(p *services.ThemedPage) { p.Warnings = []string{"note"} })
	r.svc.PutPrevious(reqA, services.ThemedPage{ID: "page-prev"})
	r.screen.Reading.Reload()
	r.settle()
	r.click(gfx.RoleCombobox, locales.ThemedReadingReaderSettings())
	want := []string{
		locales.ThemedReadingReaderAdjust(), locales.ThemedReadingReaderRegenerate(), locales.ThemedReadingReaderReferences(),
		locales.ThemedReadingAssets(), locales.ThemedReadingExport(), locales.ThemedReadingExportText(),
		locales.ThemedReadingReaderExportStatic(), locales.ThemedReadingReaderRestore(), locales.ThemedReadingReaderPageNotes(),
		locales.ThemedReadingReaderDelete(),
	}
	var got []string
	for _, n := range r.session.Access() {
		if n.Role == gfx.RoleMenuItem {
			got = append(got, n.Label)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("菜单项 = %q，想要 %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("第 %d 项 = %q，想要 %q", i, got[i], want[i])
		}
	}
}

func TestToolsMenuDisabledWhileGenerating(t *testing.T) {
	r := withPage(t)
	r.taskIn(services.ThemedTask{State: services.ThemedTaskRunning, Stage: services.ThemedStageDesign})
	r.screen.Reading.Reload()
	r.settle()
	r.click(gfx.RoleCombobox, locales.ThemedReadingReaderSettings())
	for _, label := range []string{locales.ThemedReadingReaderAdjust(), locales.ThemedReadingReaderRegenerate(), locales.ThemedReadingReaderDelete()} {
		if n, ok := r.node(gfx.RoleMenuItem, label); !ok || !n.Disabled {
			t.Fatalf("生成中「%s」应禁用：%+v ok=%v", label, n, ok)
		}
	}
	if n, ok := r.node(gfx.RoleMenuItem, locales.ThemedReadingExport()); !ok || n.Disabled {
		t.Fatalf("生成中仍可导出：%+v ok=%v", n, ok)
	}
}

func TestToolsExportVariantsPassFlags(t *testing.T) {
	cases := []struct {
		label               string
		withoutImages, only bool
	}{
		{locales.ThemedReadingExport(), false, false},
		{locales.ThemedReadingExportText(), true, false},
		{locales.ThemedReadingReaderExportStatic(), false, true},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			r := withPage(t)
			r.svc.SetExportPath("C:/out/page.html")
			r.openTool(tc.label)
			r.settle()
			got := r.svc.Calls().Exports
			if len(got) != 1 || got[0].WithoutImages != tc.withoutImages || got[0].StaticOnly != tc.only || got[0].ThemedRequest != reqA {
				t.Fatalf("导出入参不对：%+v", got)
			}
			if tm := r.lastToast(); tm.Message != locales.ThemedReadingExported() {
				t.Fatalf("导出成功应有提示：%+v", tm)
			}
		})
	}
}

func TestToolsExportCancelledIsSilent(t *testing.T) {
	r := withPage(t)
	r.openTool(locales.ThemedReadingExport())
	r.settle()
	if n := len(r.svc.Calls().Exports); n != 1 {
		t.Fatalf("应调用导出，实际 %d", n)
	}
	if n := len(r.toasts()); n != 0 {
		t.Fatalf("用户取消保存不算失败也不弹提示：%+v", r.toasts())
	}
}

func TestToolsExportFailureToastsReason(t *testing.T) {
	r := withPage(t)
	r.svc.Fail("export", errTest("disk is full"))
	r.openTool(locales.ThemedReadingExport())
	r.settle()
	if tm := r.lastToast(); tm.Detail == "" {
		t.Fatalf("导出失败应带可复制的原因：%+v", tm)
	}
}

func TestToolsRestorePreviousVersion(t *testing.T) {
	r := withPage(t)
	r.svc.PutPrevious(reqA, services.ThemedPage{ID: "page-prev"})
	r.screen.Reading.Reload()
	r.settle()
	r.openTool(locales.ThemedReadingReaderRestore())
	r.settle()
	if got := r.svc.Calls().Restores; len(got) != 1 || got[0] != reqA {
		t.Fatalf("应恢复上一版：%+v", got)
	}
}

func TestToolsDeleteNeedsConfirmation(t *testing.T) {
	r := withPage(t)
	r.openTool(locales.ThemedReadingReaderDelete())
	if !r.textHas(locales.ThemedReadingDeleteHint()) {
		t.Fatalf("应弹出确认；画面上有 %q", r.session.Texts())
	}
	if n := len(r.svc.Calls().Removes); n != 0 {
		t.Fatalf("确认前不该删除，实际 %d 次", n)
	}
	r.clickButton(locales.CommonCancel())
	if r.textHas(locales.ThemedReadingDeleteHint()) {
		t.Fatal("取消后确认框应关闭")
	}
	if n := len(r.svc.Calls().Removes); n != 0 {
		t.Fatalf("取消不该删除，实际 %d 次", n)
	}
	if !r.aiOn() {
		t.Fatal("取消后仍在 AI 阅读")
	}
}

func TestToolsDeleteConfirmedReturnsToOriginal(t *testing.T) {
	r := withPage(t)
	r.openTool(locales.ThemedReadingReaderDelete())
	r.clickButton(locales.ThemedReadingReaderDelete())
	r.settle()
	if got := r.svc.Calls().Removes; len(got) != 1 || got[0] != reqA {
		t.Fatalf("应删除 A 的主题页：%+v", got)
	}
	if r.aiOn() {
		t.Fatal("删除后应退回原文")
	}
	r.wantNoSnapshot()
}

func TestToolsDeleteFailureKeepsConfirmation(t *testing.T) {
	r := withPage(t)
	r.svc.Fail("remove", errTest("file is in use"))
	r.openTool(locales.ThemedReadingReaderDelete())
	r.clickButton(locales.ThemedReadingReaderDelete())
	r.settle()
	if !r.textHas(locales.ThemedReadingDeleteHint()) {
		t.Fatalf("删除失败应保留确认框；画面上有 %q", r.session.Texts())
	}
	if tm := r.lastToast(); tm.Detail == "" {
		t.Fatalf("删除失败应带可复制的原因：%+v", tm)
	}
	if !r.aiOn() {
		t.Fatal("删除失败不该退回原文")
	}
}

func TestToolsReferencesListAndOpenLink(t *testing.T) {
	r := withPage(t)
	r.svc.SetReferences(reqA,
		services.ThemedReference{ID: "r1", Title: "Primary Source", URL: "https://example.org/a", CapturedAt: 1_700_000_000_000},
		services.ThemedReference{ID: "r2", Title: "Unsafe Source", URL: "javascript:alert(1)", CapturedAt: 1_700_000_000_000},
	)
	r.openTool(locales.ThemedReadingReaderReferences())
	r.settle()
	if !r.textHas("Primary Source") || !r.textHas("Unsafe Source") {
		t.Fatalf("应列出参考资料；画面上有 %q", r.session.Texts())
	}
	if !r.textHas(ReferenceDate(services.ThemedReference{CapturedAt: 1_700_000_000_000}, nil, "")) {
		t.Fatal("应显示获取日期")
	}
	if r.has(gfx.RoleButton, "Unsafe Source") || r.has(gfx.RoleLink, "Unsafe Source") {
		t.Fatal("非 http(s) 链接不能做成可点击")
	}
	if n, ok := r.node(gfx.RoleButton, "Primary Source"); ok {
		r.clickAt(n.Bounds.X+n.Bounds.Width/2, n.Bounds.Y+n.Bounds.Height/2)
	} else {
		r.click(gfx.RoleLink, "Primary Source")
	}
	if got := r.links(); len(got) != 1 || got[0] != "https://example.org/a" {
		t.Fatalf("应交给系统浏览器打开：%q", got)
	}
}

func TestToolsReferencesOpenLinkFailureToasts(t *testing.T) {
	r := newUIRig(t, withIsland)
	r.screen.Hooks.OpenExternal = func(string) error { return errTest("no browser") }
	r.putPage()
	r.svc.SetReferences(reqA, services.ThemedReference{ID: "r1", Title: "Primary Source", URL: "https://example.org/a"})
	r.activate()
	r.openTool(locales.ThemedReadingReaderReferences())
	r.settle()
	if n, ok := r.node(gfx.RoleButton, "Primary Source"); ok {
		r.clickAt(n.Bounds.X+n.Bounds.Width/2, n.Bounds.Y+n.Bounds.Height/2)
	} else {
		r.click(gfx.RoleLink, "Primary Source")
	}
	if tm := r.lastToast(); tm.Detail == "" {
		t.Fatalf("打开链接失败应带原因：%+v", tm)
	}
}

func TestToolsReferencesEmptyAndError(t *testing.T) {
	r := withPage(t)
	r.openTool(locales.ThemedReadingReaderReferences())
	r.settle()
	if !r.textHas(locales.ThemedReadingReaderNoReferences()) {
		t.Fatalf("没有资料应说明；画面上有 %q", r.session.Texts())
	}
	r.clickButton(locales.CommonClose())

	r.svc.Fail("references", errTest("index is corrupted"))
	r.openTool(locales.ThemedReadingReaderReferences())
	r.settle()
	if !r.textHas("index is corrupted") || r.textHas(locales.ThemedReadingReaderNoReferences()) {
		t.Fatalf("读取失败应显示原因，不能当成「没有资料」；画面上有 %q", r.session.Texts())
	}
	r.svc.Fail("references", nil)
	r.svc.SetReferences(reqA, services.ThemedReference{ID: "r1", Title: "Primary Source", URL: "https://example.org/a"})
	r.clickButton(locales.CommonRetry())
	r.settle()
	if !r.textHas("Primary Source") {
		t.Fatalf("重试成功后应列出资料；画面上有 %q", r.session.Texts())
	}
}

func TestToolsAssetsPanelListsAssetsAndRedraws(t *testing.T) {
	r := withPage(t, func(p *services.ThemedPage) {
		p.Assets = []services.ThemedAsset{
			{ID: "g1", Role: services.ThemedAssetGenerated, Purpose: "hero picture", Status: services.ThemedAssetReady, FileName: "g1.png"},
			{ID: "o1", Role: services.ThemedAssetOriginal, Purpose: "original figure", Status: services.ThemedAssetFailed, Error: "download failed"},
		}
	})
	r.openTool(locales.ThemedReadingAssets())
	r.settle()
	for _, want := range []string{"hero picture", "original figure", "download failed", locales.ThemedReadingGeneratedAsset(), locales.ThemedReadingOriginalAsset()} {
		if !r.textHas(want) {
			t.Fatalf("图片面板应显示 %q；画面上有 %q", want, r.session.Texts())
		}
	}
	r.clickButton(locales.ThemedReadingRedraw())
	r.settle()
	got := r.svc.Calls().Regenerates
	if len(got) != 1 || got[0].AssetID != "g1" || got[0].Req != reqA {
		t.Fatalf("应重做 g1：%+v", got)
	}
	// 重做是一个正在执行的任务：其间页面操作一律禁用，不能再点第二张。
	if n, ok := r.node(gfx.RoleButton, locales.ThemedReadingRetryOriginal()); !ok || !n.Disabled {
		t.Fatalf("任务执行期间「重试原图」应禁用：%+v ok=%v", n, ok)
	}
}

func TestToolsAssetsPanelRetriesFailedOriginal(t *testing.T) {
	r := withPage(t, func(p *services.ThemedPage) {
		p.Assets = []services.ThemedAsset{
			{ID: "o1", Role: services.ThemedAssetOriginal, Purpose: "original figure", Status: services.ThemedAssetFailed, Error: "download failed"},
			{ID: "o2", Role: services.ThemedAssetOriginal, Purpose: "fine figure", Status: services.ThemedAssetReady, FileName: "o2.png"},
		}
	})
	r.openTool(locales.ThemedReadingAssets())
	r.settle()
	r.clickButton(locales.ThemedReadingRetryOriginal())
	if got := r.svc.Calls().Regenerates; len(got) != 1 || got[0].AssetID != "o1" {
		t.Fatalf("应只重试失败的原图 o1：%+v", got)
	}
}
func TestToolsAssetsPanelEmpty(t *testing.T) {
	r := withPage(t)
	r.openTool(locales.ThemedReadingAssets())
	if !r.textHas(locales.ThemedReadingNoAssets()) {
		t.Fatalf("没有图片应说明；画面上有 %q", r.session.Texts())
	}
}

func TestToolsNotesPanelShowsWarningsAndStale(t *testing.T) {
	r := withPage(t, func(p *services.ThemedPage) { p.Warnings = []string{"one image was skipped"} })
	r.svc.SetStale(reqA, true)
	r.screen.Reading.Reload()
	r.settle()
	r.openTool(locales.ThemedReadingReaderPageNotes())
	if !r.textHas("one image was skipped") || !r.textHas(locales.ThemedReadingReaderStaleDetail()) {
		t.Fatalf("版本说明应含提示与过期说明；画面上有 %q", r.session.Texts())
	}
	r.clickButton(locales.CommonClose())
	if r.textHas("one image was skipped") {
		t.Fatal("关闭后面板应消失")
	}
}

func TestToolsMenuClosesWhenModeLeavesAI(t *testing.T) {
	r := withPage(t)
	r.click(gfx.RoleCombobox, locales.ThemedReadingReaderSettings())
	if !r.has(gfx.RoleMenuItem, locales.ThemedReadingExport()) {
		t.Fatal("菜单应已展开")
	}
	r.screen.Mode.Select(false)
	r.settle()
	if r.has(gfx.RoleMenuItem, locales.ThemedReadingExport()) {
		t.Fatal("离开 AI 阅读后菜单应收起")
	}
}
