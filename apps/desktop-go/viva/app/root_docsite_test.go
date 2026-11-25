package app

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/locales"
	importsscreen "guizhi-mygo-shell/viva/screens/imports"
	"guizhi-mygo-shell/viva/screens/shell"
)

// B-013（采集文档站整页）与 B-015（登录态重试 / NodeSeek 验证 / 诊断里的采集浏览器）经 Root 的端到端测试：
// 服务用假实现，界面是真的 Root 在无头会话里画出来的，点击走真实的命中测试与回调。

// openAuthDocRoot 在采集模块里打开 Root：队列里放给定任务，按需装配文档站与平台账号两个服务。
func openAuthDocRoot(t *testing.T, tasks []services.ImportTask, docs *servicestest.DocSite, auth *servicestest.PlatformAuth) (*rootFixture, *servicestest.Imports, *servicestest.Library) {
	t.Helper()
	return openRootWithServices(t, tasks, func(o *Options) {
		if docs != nil {
			o.Services.DocSite = docs
		}
		if auth != nil {
			o.Services.PlatformAuth = auth
		}
	})
}

func openRootWithServices(t *testing.T, tasks []services.ImportTask, mutate func(*Options)) (*rootFixture, *servicestest.Imports, *servicestest.Library) {
	t.Helper()
	return openCaptureRoot(t, shell.ModuleImports, func(o *Options) {
		counts := map[string]int64{"all": int64(len(tasks))}
		for _, task := range tasks {
			counts[task.Status]++
		}
		o.Services.Imports.(*servicestest.Imports).Set(services.ImportPage{Total: int64(len(tasks)), Counts: counts, Entries: tasks}, nil)
		mutate(o)
	})
}

func failedImportTask(id, source, reason string) services.ImportTask {
	task := importTask(id, "失败的任务")
	task.Status, task.SourceInput, task.Error, task.CaptureStrategy = services.ImportStatusFailed, source, reason, "standard"
	return task
}

func xhsFailure() services.ImportTask {
	return failedImportTask("t1", "https://www.xiaohongshu.com/explore/abc", "[structure_missing] 笔记页未返回数据")
}

func nodeseekFailure() services.ImportTask {
	return failedImportTask("t2", "https://www.nodeseek.com/post-123-1", "nodeseek_verification_required")
}

func crawlJobFixture(id, status string) services.CrawlJob {
	return services.CrawlJob{
		ID: id, Status: status, Counts: map[string]int{services.CrawlPageAdded: 1},
		Input: services.CrawlInput{Seeds: []services.CrawlSeed{{URL: "https://docs.example.com/" + id + "/", Mode: services.CrawlSeedDirectory}}},
	}
}

// hasButtonContaining 报告画面上是否有名字含 part 的按钮。
func (f *rootFixture) hasButtonContaining(part string) bool {
	for _, n := range f.session.Access() {
		if n.Role == gfx.RoleButton && strings.Contains(n.Label, part) {
			return true
		}
	}
	return false
}

func (f *rootFixture) openDocSite(t *testing.T) {
	t.Helper()
	f.clickRole(t, gfx.RoleButton, locales.WebCaptureImportSite())
}

func listCount(docs *servicestest.DocSite) int {
	n := 0
	for _, c := range docs.Calls() {
		if c == "list" {
			n++
		}
	}
	return n
}

// ---- 采集文档站（B-013） ----

// 点页头「采集文档站」打开整页，读到的批次列在页上；选中批次后页面明细是纯展示行：点已入库的页面不跳知识库（渲染层 <li> 不可点）。
func TestRootDocSiteListsBatchesAndShowsPagesAsPlainRows(t *testing.T) {
	docs := &servicestest.DocSite{}
	docs.SetJobs(crawlJobFixture("j1", services.CrawlJobCompleted))
	docs.SetPages("j1", services.CrawlPage{ID: "p1", JobID: "j1", URL: "https://docs.example.com/j1/intro", Status: services.CrawlPageAdded, ItemID: "n1"})
	f, _, _ := openAuthDocRoot(t, nil, docs, nil)

	f.openDocSite(t)
	if !f.hasRole(gfx.RoleHeading, locales.WebCaptureImportSite()) {
		t.Fatalf("应进入文档站整页；画面上有 %q", f.session.Texts())
	}
	f.waitFrames(t, "读到批次列表", func() bool { return f.hasButtonContaining("docs.example.com/j1/") })

	f.clickButtonContaining(t, "docs.example.com/j1/")
	f.waitFrames(t, "读到页面明细", func() bool { return f.session.HasText("docs.example.com/j1/intro") })

	if f.hasButtonContaining("docs.example.com/j1/intro") {
		t.Fatalf("页面行是纯展示，不应是按钮；画面上有 %q", f.session.Texts())
	}
	for _, tx := range f.session.Snapshot().Texts {
		if tx.Value == "https://docs.example.com/j1/intro" {
			if err := f.session.Click(tx.Bounds.X+tx.Bounds.Width/2, tx.Bounds.Y+tx.Bounds.Height/2); err != nil {
				t.Fatalf("点击页面行失败：%v", err)
			}
		}
	}
	f.frames(t, 5)
	if f.root.Window().Module == shell.ModuleLibrary {
		t.Fatalf("点页面行不应切到知识库")
	}
}

func (f *rootFixture) clickButtonContaining(t *testing.T, part string) {
	t.Helper()
	for _, n := range f.session.Access() {
		if n.Role == gfx.RoleButton && strings.Contains(n.Label, part) {
			if err := f.session.Click(n.Bounds.X+n.Bounds.Width/2, n.Bounds.Y+n.Bounds.Height/2); err != nil {
				t.Fatal(err)
			}
			f.frames(t, 3)
			return
		}
	}
	t.Fatalf("找不到名字含「%s」的按钮；画面上有 %q", part, f.session.Texts())
}

// 填入口网址、点开始：服务收到带该入口的创建请求，新批次出现在列表里。
func TestRootDocSiteStartCreatesABatch(t *testing.T) {
	docs := &servicestest.DocSite{}
	f, _, _ := openAuthDocRoot(t, nil, docs, nil)
	f.openDocSite(t)

	f.clickRole(t, gfx.RoleTextField, locales.WebCaptureEntryUrl())
	if err := f.session.Type("https://docs.example.com/guide/"); err != nil {
		t.Fatal(err)
	}
	f.frames(t, 3)
	if n, ok := f.session.AccessByRole(gfx.RoleButton, locales.WebCaptureStart()); !ok || n.Disabled {
		t.Fatalf("填了有效入口后开始按钮应可点：%+v 找到=%v", n, ok)
	}
	f.clickRole(t, gfx.RoleButton, locales.WebCaptureStart())

	waitUntil(t, "服务收到创建请求", func() bool { return len(docs.Created()) == 1 })
	in := docs.Created()[0]
	if len(in.Seeds) != 1 || !strings.Contains(in.Seeds[0].URL, "docs.example.com/guide") {
		t.Errorf("创建请求应带入口网址：%+v", in)
	}
	f.waitFrames(t, "新批次出现在列表里", func() bool { return f.hasButtonContaining("docs.example.com/guide") })
}

// 创建失败：错误提示带原因（可复制的详情），按钮恢复可点，不会报成功。
func TestRootDocSiteStartFailureIsReported(t *testing.T) {
	docs := &servicestest.DocSite{}
	docs.FailOn("create", errors.New("队列已满"))
	f, _, _ := openAuthDocRoot(t, nil, docs, nil)
	f.openDocSite(t)
	f.clickRole(t, gfx.RoleTextField, locales.WebCaptureEntryUrl())
	if err := f.session.Type("https://docs.example.com/guide/"); err != nil {
		t.Fatal(err)
	}
	f.clickRole(t, gfx.RoleButton, locales.WebCaptureStart())

	waitUntil(t, "收到创建请求", func() bool { return slices.Contains(docs.Calls(), "create") })
	f.waitFrames(t, "弹出错误提示", func() bool { return f.root.Notifier().Len() == 1 })
	if len(docs.Created()) != 0 {
		t.Error("创建失败不应留下批次")
	}
	if n, ok := f.session.AccessByRole(gfx.RoleButton, locales.WebCaptureStart()); !ok || n.Disabled {
		t.Errorf("失败后开始按钮应恢复可点：%+v 找到=%v", n, ok)
	}
}

// 第一次读取失败：显示错误态而不是空态，修好服务后点重试能读到。
func TestRootDocSiteLoadErrorThenRetry(t *testing.T) {
	docs := &servicestest.DocSite{}
	docs.FailOn("list", errors.New("服务暂时不可用"))
	docs.SetJobs(crawlJobFixture("j1", services.CrawlJobRunning))
	f, _, _ := openAuthDocRoot(t, nil, docs, nil)
	f.openDocSite(t)

	f.waitFrames(t, "显示读取失败", func() bool { return f.session.HasText(locales.CommonRetry()) })
	if f.hasButtonContaining("docs.example.com/j1/") {
		t.Error("读取失败时不应渲染出批次")
	}
	docs.FailOn("list", nil)
	f.clickRole(t, gfx.RoleButton, locales.CommonRetry())
	f.waitFrames(t, "重试后读到批次", func() bool { return f.hasButtonContaining("docs.example.com/j1/") })
}

// 目标合集下拉读的是知识库的合集：Root 把知识库服务接给文档站 Store，展开下拉能选到知识库里的两个合集。
func TestRootDocSiteCollectionSelectListsTheLibraryCollections(t *testing.T) {
	docs := &servicestest.DocSite{}
	f, _, _ := openAuthDocRoot(t, nil, docs, nil)
	f.openDocSite(t)
	f.waitFrames(t, "文档站读到知识库的合集", func() bool { return len(f.root.imports.DocSite.View().Collections) == 2 })

	f.clickRole(t, gfx.RoleCombobox, locales.WebCaptureCollection())
	for _, name := range []string{locales.WebCaptureUnfiled(), "工作", "灵感"} {
		if _, ok := f.session.AccessByRole(gfx.RoleOption, name); !ok {
			t.Errorf("目标合集下拉里应有「%s」；画面上有 %q", name, f.session.Texts())
		}
	}
}

// 没装配 DocSite 的 Root：「采集文档站」仍是「尚未迁移」的占位，返回钮回到队列。
func TestRootDocSiteUnwiredKeepsThePlaceholder(t *testing.T) {
	f, _, _ := openAuthDocRoot(t, nil, nil, nil)
	f.openDocSite(t)
	if !f.session.HasText(locales.NativeShellNotMigrated(locales.WebCaptureImportSite())) {
		t.Fatalf("没有文档站服务时应显示占位；画面上有 %q", f.session.Texts())
	}
	f.clickRole(t, gfx.RoleButton, locales.CommonBack())
	if f.session.HasText(locales.NativeShellNotMigrated(locales.WebCaptureImportSite())) {
		t.Error("返回后应回到任务队列")
	}
}

// 切到别的模块之后，文档站页的轮询停掉（EndFrame 发现这一帧没画它）。用真实的 2 秒轮询间隔，约 5 秒。
func TestRootDocSiteStopsPollingWhenTheModuleSwitches(t *testing.T) {
	if testing.Short() {
		t.Skip("用真实 2 秒轮询间隔，-short 下跳过")
	}
	docs := &servicestest.DocSite{}
	docs.SetJobs(crawlJobFixture("j1", services.CrawlJobRunning))
	f, _, _ := openAuthDocRoot(t, nil, docs, nil)
	f.openDocSite(t)

	before := listCount(docs)
	deadline := time.Now().Add(4 * time.Second)
	for listCount(docs) <= before {
		if time.Now().After(deadline) {
			t.Fatal("停留在页面上时应每 2 秒轮询一次")
		}
		f.frames(t, 1)
		time.Sleep(20 * time.Millisecond)
	}

	f.clickRole(t, gfx.RoleButton, locales.NavLibrary())
	f.frames(t, 3)
	if f.root.Window().Module != shell.ModuleLibrary {
		t.Fatalf("应已切到知识库，得 %v", f.root.Window().Module)
	}
	time.Sleep(100 * time.Millisecond) // 在途的那次读取落地
	settled := listCount(docs)
	time.Sleep(2500 * time.Millisecond) // 超过一个轮询间隔
	if got := listCount(docs); got != settled {
		t.Errorf("切走之后不应再轮询：%d → %d", settled, got)
	}
}

// ---- 登录态重试与站点验证（B-015） ----

// 没装配 PlatformAuth：四个钩子都是空的，相关按钮照旧什么都不做；装配了才全部接上。
func TestRootAuthHooksFollowThePlatformAuthService(t *testing.T) {
	bare, _, _ := openAuthDocRoot(t, nil, nil, nil)
	h := bare.root.imports.Hooks
	if h.RetryAuthenticated != nil || h.VerifyNodeseek != nil || h.CancelNodeseek != nil || h.CaptureBrowser != nil {
		t.Errorf("没有平台账号服务时钩子应为空：%+v", h)
	}
	wired, _, _ := openAuthDocRoot(t, nil, nil, &servicestest.PlatformAuth{})
	h = wired.root.imports.Hooks
	if h.RetryAuthenticated == nil || h.VerifyNodeseek == nil || h.CancelNodeseek == nil || h.CaptureBrowser == nil {
		t.Errorf("装配平台账号服务后四个钩子都应接上：%+v", h)
	}
	if h.WebSolverEnabled == nil || h.OpenItem == nil || h.AppInfo == nil {
		t.Error("装配平台账号服务不应顶掉原有的钩子")
	}
}

// 小红书匿名解析失败 → 点「使用登录态重试」：没登录先打开登录窗口，登录成功后以登录态策略重试这个任务。
func TestRootAuthenticatedRetryLogsInThenRetries(t *testing.T) {
	auth := &servicestest.PlatformAuth{}
	auth.SetStatuses(services.PlatformStatus{Platform: importsscreen.PlatformXiaohongshu, Browser: "WebView2", BrowserVersion: "130.0", Available: true})
	f, imports, _ := openAuthDocRoot(t, []services.ImportTask{xhsFailure()}, nil, auth)

	f.clickRole(t, gfx.RoleButton, locales.ImportsAuthenticatedRetry())
	waitUntil(t, "任务以登录态策略重试", func() bool { return slices.Contains(imports.Calls(), "retry:t1") })
	if got := imports.RetryOptions().CaptureStrategy; got != "authenticated" {
		t.Errorf("应带登录态重试，得 %q", got)
	}
	want := []string{"status", "login:" + importsscreen.PlatformXiaohongshu}
	if got := auth.Calls(); !slices.Equal(got, want) {
		t.Errorf("平台账号调用 = %v，期望 %v（查状态、再登录）", got, want)
	}
}

// 登录窗口失败（用户关了窗口）：不重试任务，弹出带原因的错误提示。
func TestRootAuthenticatedRetryStopsWhenLoginFails(t *testing.T) {
	auth := &servicestest.PlatformAuth{}
	auth.SetStatuses(services.PlatformStatus{Platform: importsscreen.PlatformXiaohongshu, Available: true})
	auth.FailLogin(importsscreen.PlatformXiaohongshu, errors.New("window closed by user"))
	f, imports, _ := openAuthDocRoot(t, []services.ImportTask{xhsFailure()}, nil, auth)

	f.clickRole(t, gfx.RoleButton, locales.ImportsAuthenticatedRetry())
	f.waitFrames(t, "弹出登录态重试未开始", func() bool { return f.session.HasText(locales.ImportsAuthenticatedRetryFailed()) })
	if slices.Contains(imports.Calls(), "retry:t1") {
		t.Error("登录失败不应重试任务")
	}
}

// openNodeseekDetail 打开 NodeSeek 失败任务的详情弹窗。
func openNodeseekDetail(t *testing.T, auth *servicestest.PlatformAuth) (*rootFixture, *servicestest.Imports) {
	t.Helper()
	f, imports, _ := openAuthDocRoot(t, []services.ImportTask{nodeseekFailure()}, nil, auth)
	f.clickRole(t, gfx.RoleButton, locales.ImportsTaskDetail())
	if !f.hasRole(gfx.RoleButton, locales.ImportsNodeseekVerifyRetry()) {
		t.Fatalf("详情里应有 NodeSeek 验证重试；画面上有 %q", f.session.Texts())
	}
	return f, imports
}

// NodeSeek 验证通过：验证窗口带受阻帖子的地址打开，通过后以登录态策略重试任务。
func TestRootNodeseekVerifyThenRetry(t *testing.T) {
	auth := &servicestest.PlatformAuth{}
	finish := auth.HoldLogin(importsscreen.PlatformNodeseek)
	f, imports := openNodeseekDetail(t, auth)

	f.clickRole(t, gfx.RoleButton, locales.ImportsNodeseekVerifyRetry())
	waitUntil(t, "验证窗口已打开", func() bool { return auth.Pending(importsscreen.PlatformNodeseek) })
	opts := auth.LoginOptions()
	if len(opts) != 1 || opts[0].VerificationURL == "" || !strings.Contains(opts[0].VerificationURL, "nodeseek.com") {
		t.Errorf("验证窗口应带受阻帖子的地址：%+v", opts)
	}
	if slices.Contains(imports.Calls(), "retry:t2") {
		t.Fatal("验证通过之前不应重试")
	}

	finish(nil)
	waitUntil(t, "验证通过后重试", func() bool { return slices.Contains(imports.Calls(), "retry:t2") })
	if got := imports.RetryOptions().CaptureStrategy; got != "authenticated" {
		t.Errorf("验证通过后应带登录态重试，得 %q", got)
	}
}

// NodeSeek 验证中点「取消」：外壳收到取消、验证窗口结束，不重试也不报失败。
func TestRootNodeseekVerifyCancel(t *testing.T) {
	auth := &servicestest.PlatformAuth{}
	auth.HoldLogin(importsscreen.PlatformNodeseek)
	f, imports := openNodeseekDetail(t, auth)

	f.clickRole(t, gfx.RoleButton, locales.ImportsNodeseekVerifyRetry())
	waitUntil(t, "验证窗口已打开", func() bool { return auth.Pending(importsscreen.PlatformNodeseek) })
	f.waitFrames(t, "出现取消按钮", func() bool { return f.hasRole(gfx.RoleButton, locales.CommonCancel()) })

	f.clickRole(t, gfx.RoleButton, locales.CommonCancel())
	waitUntil(t, "外壳收到取消", func() bool { return slices.Contains(auth.Calls(), "cancel:"+importsscreen.PlatformNodeseek) })
	waitUntil(t, "验证窗口结束", func() bool { return !auth.Pending(importsscreen.PlatformNodeseek) })
	f.frames(t, 3)
	if slices.Contains(imports.Calls(), "retry:t2") {
		t.Error("取消之后不应重试")
	}
	if f.session.HasText(locales.ImportsNodeseekVerificationFailed()) {
		t.Error("取消不算失败，不应弹验证失败")
	}
}

// 复制诊断信息：登录态采集的任务带上「采集浏览器」这一行，取自平台账号状态里的浏览器名与版本。
func TestRootCopyReportIncludesTheCaptureBrowser(t *testing.T) {
	auth := &servicestest.PlatformAuth{}
	auth.SetStatuses(services.PlatformStatus{Platform: importsscreen.PlatformXiaohongshu, Browser: "WebView2", BrowserVersion: "130.0", Available: true, LoggedIn: true})
	task := xhsFailure()
	task.CaptureStrategy = "authenticated"
	f, _, _ := openAuthDocRoot(t, []services.ImportTask{task}, nil, auth)

	var mu sync.Mutex
	var copied []string
	f.root.Notifier().Clipboard = func(text string) error {
		mu.Lock()
		copied = append(copied, text)
		mu.Unlock()
		return nil // 测试不碰系统剪贴板
	}
	f.clickRole(t, gfx.RoleButton, locales.ImportsTaskDetail())
	f.clickRole(t, gfx.RoleButton, locales.ImportsCopyReport())
	// 复制经 Store.Spawn 异步写入剪贴板：先等写入到达，再检查内容。
	waitUntil(t, "复制诊断信息写入剪贴板", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(copied) >= 1
	})

	mu.Lock()
	defer mu.Unlock()
	if len(copied) != 1 {
		t.Fatalf("应复制一次，实际 %d 次", len(copied))
	}
	if !strings.Contains(copied[0], locales.ImportsReportBrowser()) || !strings.Contains(copied[0], "WebView2 130.0") {
		t.Errorf("诊断信息应含采集浏览器 WebView2 130.0；实际：\n%s", copied[0])
	}
}
