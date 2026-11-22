package wiki

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/locales"
)

// 目录与侧栏（L4）：列表、选择、筛选、排序、搜索防抖、空态与加载失败。

func TestUICatalogListsPagesWithCounts(t *testing.T) {
	r := newUIRig(t, withSeed)
	for _, name := range []string{"Alpha", "Beta", "Gamma"} {
		if !r.hasButton(name) {
			t.Errorf("目录里应有「%s」；画面上有 %q", name, r.session.Texts())
		}
	}
	// 侧栏：全部 3、主题 2、实体 1、概念 0、孤立 2（Alpha→Beta 只有 Alpha 与 Gamma 没被引用）、手动编辑 1。
	for _, name := range []string{
		locales.WikiPanelAll() + " 3", locales.WikiKindTopic() + " 2", locales.WikiKindEntity() + " 1",
		locales.WikiKindConcept() + " 0", locales.WikiPanelOrphan() + " 2", locales.WikiPanelManual() + " 1",
	} {
		if !r.hasButton(name) {
			t.Errorf("侧栏缺少「%s」", name)
		}
	}
	if !r.hasText(locales.WikiCatalogCount(3)) {
		t.Errorf("缺少页数提示：%q", r.session.Texts())
	}
	if !r.hasText(locales.WikiStatusLine(3, 0, 0)) {
		t.Errorf("顶栏缺少状态行：%q", r.session.Texts())
	}
	if !r.hasText(locales.WikiNoSelection()) {
		t.Error("没选页面时详情区应提示在左侧选择")
	}
}

func TestUICatalogClickOpensDetail(t *testing.T) {
	r := newUIRig(t, withSeed)
	r.clickButton("Alpha")
	if got := r.store.View().SelectedPageID; got != "alpha" {
		t.Fatalf("选中应是 alpha：%q", got)
	}
	if !r.has(gfx.RoleHeading, "Alpha") || !r.hasText("body of alpha") {
		t.Fatalf("详情应显示标题与正文：%q", r.session.Texts())
	}
	if r.hasText(locales.WikiNoSelection()) {
		t.Error("选中之后不该再提示选择页面")
	}
}

func TestUICatalogKindFilterNarrowsList(t *testing.T) {
	r := newUIRig(t, withSeed)
	r.clickButton(locales.WikiKindEntity() + " 1")
	if !r.hasButton("Gamma") || r.hasButton("Alpha") || r.hasButton("Beta") {
		t.Fatalf("只剩实体 Gamma：%q", r.session.Texts())
	}
	// 孤立页：Alpha 与 Gamma。
	r.clickButton(locales.WikiPanelOrphan() + " 2")
	if !r.hasButton("Alpha") || !r.hasButton("Gamma") || r.hasButton("Beta") {
		t.Fatalf("孤立页应是 Alpha 与 Gamma：%q", r.session.Texts())
	}
	if !strings.Contains(r.allText(), locales.WikiPanelOrphanHint()) {
		t.Error("孤立页筛选下侧栏应有说明")
	}
	r.clickButton(locales.WikiPanelManual() + " 1")
	if !r.hasButton("Beta") || r.hasButton("Alpha") {
		t.Fatalf("手动编辑过只剩 Beta：%q", r.session.Texts())
	}
	r.clickButton(locales.WikiPanelAll() + " 3")
	if !r.hasButton("Alpha") || !r.hasButton("Gamma") {
		t.Fatalf("回到全部页面：%q", r.session.Texts())
	}
}

func TestUICatalogEmptyFilterShowsFilterEmpty(t *testing.T) {
	r := newUIRig(t, withSeed)
	r.clickButton(locales.WikiKindConcept() + " 0")
	if !r.hasText(locales.WikiFilterEmpty()) {
		t.Fatalf("概念筛选没有页面：%q", r.session.Texts())
	}
}

func TestUICatalogSortReordersRows(t *testing.T) {
	r := newUIRig(t, withSeed)
	// 默认最近更新：Gamma（最新）→ Beta → Alpha。
	if !(r.top(gfx.RoleButton, "Gamma") < r.top(gfx.RoleButton, "Beta") && r.top(gfx.RoleButton, "Beta") < r.top(gfx.RoleButton, "Alpha")) {
		t.Fatal("默认应按最近更新排")
	}
	r.clickButton(locales.WikiSortTitle())
	if !(r.top(gfx.RoleButton, "Alpha") < r.top(gfx.RoleButton, "Beta") && r.top(gfx.RoleButton, "Beta") < r.top(gfx.RoleButton, "Gamma")) {
		t.Fatal("按标题应是 Alpha、Beta、Gamma")
	}
	r.clickButton(locales.WikiSortLinked())
	if !(r.top(gfx.RoleButton, "Beta") < r.top(gfx.RoleButton, "Alpha")) {
		t.Fatal("被引用最多：Beta 在最前")
	}
}

// 搜索防抖：停止输入 250 ms 才查询，期间再输入会重排；结果回来后目录只剩命中的页面。
func TestUISearchDebouncesThenFilters(t *testing.T) {
	r := newUIRig(t, withSeed)
	r.focusSearch()
	r.typeText("bet")
	if got := len(r.svc.Searches()); got != 0 {
		t.Fatalf("防抖期内不该查询：%d", got)
	}
	r.clock.Advance(searchDebounce - time.Millisecond)
	r.frames(2)
	if got := len(r.svc.Searches()); got != 0 {
		t.Fatalf("249 ms 时还不该查询：%d", got)
	}
	r.typeText("a") // 再输入：重排防抖，旧的计时作废
	r.clock.Advance(searchDebounce - time.Millisecond)
	r.frames(2)
	if got := len(r.svc.Searches()); got != 0 {
		t.Fatalf("重排之后 249 ms 时仍不该查询：%d", got)
	}
	r.clock.Advance(time.Millisecond)
	r.frames(3)
	calls := r.svc.Searches()
	if len(calls) != 1 || calls[0].Query != "beta" {
		t.Fatalf("应只查一次 beta：%+v", calls)
	}
	if !r.hasButton("Beta") || r.hasButton("Alpha") || r.hasButton("Gamma") {
		t.Fatalf("目录应只剩命中的 Beta：%q", r.session.Texts())
	}
	if got := r.store.View().SearchQuery; got != "beta" {
		t.Fatalf("Store 里的查询：%q", got)
	}
}

func TestUISearchClearButtonRestoresCatalog(t *testing.T) {
	r := newUIRig(t, withSeed)
	r.focusSearch()
	r.typeText("beta")
	r.clock.Advance(searchDebounce)
	r.frames(3)
	if r.hasButton("Alpha") {
		t.Fatal("搜索后不该还有 Alpha")
	}
	r.clickButton(locales.HeaderClearSearch())
	if got := r.store.View().SearchQuery; got != "" {
		t.Fatalf("清除后查询应为空：%q", got)
	}
	if !r.hasButton("Alpha") || !r.hasButton("Gamma") {
		t.Fatalf("清除后应回到完整目录：%q", r.session.Texts())
	}
}

func TestUISearchNoHitsShowsCatalogEmpty(t *testing.T) {
	r := newUIRig(t, withSeed)
	r.focusSearch()
	r.typeText("zzz")
	r.clock.Advance(searchDebounce)
	r.frames(3)
	if !r.hasText(locales.WikiFilterEmpty()) {
		t.Fatalf("没有命中应提示没有匹配的页面：%q", r.session.Texts())
	}
}

func TestUIEmptyWikiShowsGuidance(t *testing.T) {
	r := newUIRig(t)
	// 折行会吃掉断点处的空格，整句比对时两边都去掉空格。
	if !r.hasText(locales.WikiEmpty()) || !strings.Contains(noSpace(r.allText()), noSpace(locales.WikiEmptyHint())) {
		t.Fatalf("没有页面应显示空态与提示：%q", r.session.Texts())
	}
	if r.hasButton(locales.WikiViewGraph()) {
		// 空 Wiki 仍保留顶栏；只有目录与图谱主体不画。
		t.Log("顶栏的视图切换仍在")
	}
}

// 首次读取失败：显示加载失败而不是空态，重试成功后恢复正常。
func TestUILoadFailureShowsRetry(t *testing.T) {
	r := newUIRig(t, withSeed, func(r *uiRig) { r.svc.Fail(servicestest.WikiOpCatalog, errors.New("disk offline")) })
	if r.hasText(locales.WikiEmpty()) {
		t.Fatalf("加载失败不能渲染成空态：%q", r.session.Texts())
	}
	if !r.hasButton(locales.CommonRetry()) {
		t.Fatalf("加载失败应有重试：%q", r.session.Texts())
	}
	r.svc.Fail(servicestest.WikiOpCatalog, nil)
	r.clickButton(locales.CommonRetry())
	if !r.hasButton("Alpha") {
		t.Fatalf("重试成功后应显示目录：%q", r.session.Texts())
	}
}

// 离开 Wiki（这一帧没画）视为卸载，再回来重读目录。
func TestUILeavingAndReturningRefreshes(t *testing.T) {
	r := newUIRig(t, withSeed)
	before := r.svc.CatalogCalls()
	r.hidden = true
	r.frames(3)
	r.svc.PutPage(services.WikiPage{ID: "delta", Title: "Delta", Body: "new"})
	r.hidden = false
	r.frames(3)
	if r.svc.CatalogCalls() <= before {
		t.Fatalf("回到 Wiki 应重读目录：%d → %d", before, r.svc.CatalogCalls())
	}
	if !r.hasButton("Delta") {
		t.Fatalf("新页面应出现：%q", r.session.Texts())
	}
}

// 工具栏的视图切换按钮在目录与图谱之间切换，二级侧栏随之换成图谱提示。
func TestUIViewToggleSwitchesSidebar(t *testing.T) {
	r := newUIRig(t, withSeed)
	r.clickButton(locales.WikiViewGraph())
	if r.store.View().Mode != ViewGraph {
		t.Fatal("应切到图谱视图")
	}
	if !r.hasText(locales.WikiPanelGraphHint()) {
		t.Fatalf("图谱视图下侧栏应换成提示：%q", r.session.Texts())
	}
	r.clickButton(locales.WikiViewCatalog())
	if r.store.View().Mode != ViewCatalog || !r.hasButton("Alpha") {
		t.Fatalf("应回到目录视图：%q", r.session.Texts())
	}
}

// 搜索失败：目录列显示失败原因与重试，不能画成「没有匹配的页面」。
func TestUISearchFailureShowsErrorNotEmpty(t *testing.T) {
	r := newUIRig(t, withSeed)
	r.svc.Fail(servicestest.WikiOpSearch, errors.New("fts offline"))
	r.focusSearch()
	r.typeText("beta")
	r.clock.Advance(searchDebounce)
	r.frames(3)
	if r.hasText(locales.WikiFilterEmpty()) {
		t.Fatalf("搜索失败不能画成无结果：%q", r.session.Texts())
	}
	if !r.hasText("fts offline") || !r.hasButton(locales.CommonRetry()) {
		t.Fatalf("应显示失败原因与重试：%q", r.session.Texts())
	}
	r.svc.Fail(servicestest.WikiOpSearch, nil)
	r.clickButton(locales.CommonRetry())
	if !r.hasButton("Beta") || r.hasButton("Alpha") {
		t.Fatalf("重试成功后应只剩命中的 Beta：%q", r.session.Texts())
	}
}

// 目录栏宽度：启动时读偏好，拖动分隔条结束后写回偏好。
func TestUICatalogPaneWidthLoadsAndSaves(t *testing.T) {
	base := newUIRig(t, withSeed)
	n0, ok := base.node(gfx.RoleSeparator, locales.WikiResizeCatalog())
	if !ok {
		t.Fatal("应有目录宽度分隔条")
	}
	wide := newUIRig(t, withSeed, func(r *uiRig) { _ = r.prefs.Set(settings.PrefWikiCatalogPaneWidth, 360.0) })
	n1, _ := wide.node(gfx.RoleSeparator, locales.WikiResizeCatalog())
	if got := n1.Bounds.X - n0.Bounds.X; math.Abs(float64(got)-(360-float64(catalogPaneBounds.Default))) > 1 {
		t.Fatalf("偏好 360 应比默认宽 %.0f：差 %.1f", 360-float64(catalogPaneBounds.Default), got)
	}
	// 拖动分隔条 +50 px，抬起后保存。
	x, y := n0.Bounds.X+n0.Bounds.Width/2, n0.Bounds.Y+n0.Bounds.Height/2
	base.drag(x, y, x+50, y)
	raw, ok := base.prefs.Get(settings.PrefWikiCatalogPaneWidth)
	if !ok {
		t.Fatal("拖动结束后应保存目录栏宽度")
	}
	var saved float64
	if err := json.Unmarshal(raw, &saved); err != nil || math.Abs(saved-float64(catalogPaneBounds.Default)-50) > 1 {
		t.Fatalf("保存的宽度应是默认值加 50：%s", raw)
	}
}