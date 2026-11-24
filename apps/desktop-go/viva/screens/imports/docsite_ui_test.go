package imports

import (
	"errors"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 「采集文档站」整页（渲染层 DocumentSitePanel）的 L4 界面测试：表单与实际范围预览、下拉、创建批次、批次列表与页面明细、
// 暂停 / 继续 / 重试失败 / 取消、点已入库页面打开条目、读取失败的错误态与重试、返回队列时摘掉轮询。
// 状态机本身的并发与竞态在 docsite_test.go 里测，这里只测「画面上看得见、点得动」的那一层。

// docRig 是在采集工作区测试台上装了 DocSite 的会话：后台动作同步执行、轮询靠手动时钟推进，每一步之后画面是确定的。
type docRig struct {
	*rig
	site   *servicestest.DocSite
	cols   *fakeCollections
	store  *DocSiteStore
	dclock *manualClock
}

// newDocRig 打开采集工作区（队列为空），文档站的服务是假实现；setup 在首帧之前往假服务里放数据。
func newDocRig(t *testing.T, setup func(site *servicestest.DocSite, cols *fakeCollections)) *docRig {
	t.Helper()
	return newDocRigWith(t, nil, setup)
}

// newDocRigWith 同 newDocRig，wrap 可以把假服务包一层再交给 Store（构造「创建进行中」之类的窗口）。
func newDocRigWith(t *testing.T, wrap func(*servicestest.DocSite) services.DocSite, setup func(site *servicestest.DocSite, cols *fakeCollections)) *docRig {
	t.Helper()
	d := &docRig{site: &servicestest.DocSite{}, cols: &fakeCollections{}, dclock: newManualClock()}
	if setup != nil {
		setup(d.site, d.cols)
	}
	var svc services.DocSite = d.site
	if wrap != nil {
		svc = wrap(d.site)
	}
	d.rig = newRig(t, pageOf(), func(r *rig) {
		guard := kit.Guard{Notify: r.notifier, Log: r.log}
		d.store = NewDocSiteStore(svc, d.cols, guard, nil)
		d.store.After = d.dclock.After
		d.store.Spawn = func(fn func()) { fn() }
		r.screen.DocSite = d.store
	})
	return d
}

// openedDocs 打开文档站页：点页头的「采集文档站」。
func openedDocs(t *testing.T, setup func(site *servicestest.DocSite, cols *fakeCollections)) *docRig {
	t.Helper()
	d := newDocRig(t, setup)
	d.openPage()
	return d
}

func (d *docRig) openPage() {
	d.t.Helper()
	d.click(gfx.RoleButton, locales.WebCaptureImportSite())
	if !d.screen.documents {
		d.t.Fatal("点页头的「采集文档站」应切到文档站页")
	}
}

// poll 让 2 秒的轮询到点（读取同步发生），再出几帧让画面跟上。
func (d *docRig) poll() {
	d.t.Helper()
	d.dclock.Advance(docSitePollInterval)
	d.frames(3)
}

// pending 是手动时钟上还没到点的计时器数：轮询挂着时恒有一个。
func (c *manualClock) pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.timers)
}

func (d *docRig) listCalls() int {
	n := 0
	for _, c := range d.site.Calls() {
		if c == "list" {
			n++
		}
	}
	return n
}

// field 读一个文本框此刻的值。
func (d *docRig) field(label string) string {
	d.t.Helper()
	n, ok := d.node(gfx.RoleTextField, label)
	if !ok {
		d.t.Fatalf("找不到文本框「%s」；画面上有 %q", label, d.session.Texts())
	}
	return n.Value
}

// fill 点进文本框并输入文字（框里原有的文字先清掉）。
func (d *docRig) fill(label, text string) {
	d.t.Helper()
	d.click(gfx.RoleTextField, label)
	d.keys("cmd+a") // 无头会话里「全选」是 cmd+a（编辑快捷键）
	if text == "" {
		d.keys("backspace")
		return
	}
	d.typeText(text)
}

// pick 在下拉里选一项。
func (d *docRig) pick(label, option string) {
	d.t.Helper()
	d.click(gfx.RoleCombobox, label)
	d.click(gfx.RoleOption, option)
}

// jobTitle 是批次标题行的文字（入口网址 · 状态）。
func jobTitle(id, status string) string { return "https://a.com/" + id + "/ \u00b7 " + status }

func setJobs(jobs ...services.CrawlJob) func(*servicestest.DocSite, *fakeCollections) {
	return func(site *servicestest.DocSite, _ *fakeCollections) { site.SetJobs(jobs...) }
}

// ---- 打开与返回 ----

// 点页头「采集文档站」切到整页：返回钮、标题、说明、六个表单项、开始按钮；范围无效时开始按钮禁用并提示。
func TestDocPageOpensWithFormAndDisabledStart(t *testing.T) {
	d := openedDocs(t, nil)
	if _, ok := d.node(gfx.RoleHeading, locales.WebCaptureImportSite()); !ok {
		t.Fatalf("应有页标题；画面上有 %q", d.session.Texts())
	}
	for _, want := range []string{locales.WebCaptureBack(), locales.WebCaptureSiteDescription()} {
		if !d.hasTextLike(want) {
			t.Errorf("缺少文字 %q", want)
		}
	}
	for _, label := range []string{locales.WebCaptureEntryUrl(), locales.WebCaptureDirectoryScope(), locales.WebCapturePageLimit(), locales.WebCaptureDepthLimit()} {
		if !d.has(gfx.RoleTextField, label) {
			t.Errorf("缺少文本框「%s」", label)
		}
	}
	for _, label := range []string{locales.WebCaptureCollection(), locales.WebCaptureDuplicatePolicy()} {
		if !d.has(gfx.RoleCombobox, label) {
			t.Errorf("缺少下拉「%s」", label)
		}
	}
	if n, ok := d.node(gfx.RoleButton, locales.WebCaptureStart()); !ok || !n.Disabled {
		t.Errorf("还没填入口时开始按钮应禁用：%+v 找到=%v", n, ok)
	}
	want := locales.WebCaptureActualScope() + locales.WebCaptureValidScope() + locales.WebCaptureScopeHint()
	if !d.hasText(want) {
		t.Errorf("范围无效时应提示「%s」；画面上有 %q", want, d.session.Texts())
	}
	if d.listCalls() == 0 {
		t.Error("打开页面应立刻读一次批次列表")
	}
}

// 表单的默认值：最多页数 50、发现深度 2、目标合集「未归合集」、重复处理「跳过重复条目」。
func TestDocPageFormDefaults(t *testing.T) {
	d := openedDocs(t, nil)
	if got := d.field(locales.WebCapturePageLimit()); got != "50" {
		t.Errorf("最多页数默认应是 50，得 %q", got)
	}
	if got := d.field(locales.WebCaptureDepthLimit()); got != "2" {
		t.Errorf("发现深度默认应是 2，得 %q", got)
	}
	if n, _ := d.node(gfx.RoleCombobox, locales.WebCaptureCollection()); n.Value != locales.WebCaptureUnfiled() {
		t.Errorf("目标合集默认应是「%s」，得 %q", locales.WebCaptureUnfiled(), n.Value)
	}
	if n, _ := d.node(gfx.RoleCombobox, locales.WebCaptureDuplicatePolicy()); n.Value != locales.WebCaptureSkipDuplicates() {
		t.Errorf("重复处理默认应是「%s」，得 %q", locales.WebCaptureSkipDuplicates(), n.Value)
	}
	if got := d.field(locales.WebCaptureEntryUrl()); got != "" {
		t.Errorf("入口网址默认应为空，得 %q", got)
	}
}

// 没有装配 DocSite 时仍是「尚未迁移」的占位，返回钮可用（兼容旧单测与自动化实例）。
func TestDocPagePlaceholderWithoutDocSite(t *testing.T) {
	r := loaded(t, pageOf())
	r.click(gfx.RoleButton, locales.WebCaptureImportSite())
	if !r.hasText(locales.NativeShellNotMigrated(locales.WebCaptureImportSite())) {
		t.Fatalf("没有 DocSite 应显示占位；画面上有 %q", r.session.Texts())
	}
	r.click(gfx.RoleButton, locales.CommonBack())
	if r.screen.documents {
		t.Error("占位页的返回钮应回到任务队列")
	}
}

// 返回钮回到任务队列，并立刻摘掉轮询：之后时钟再走也不再读取；再进来重新挂上。
func TestDocPageBackDetachesAndReopenReattaches(t *testing.T) {
	d := openedDocs(t, setJobs(crawlJob("j1", services.CrawlJobRunning, nil)))
	d.poll()
	before := d.listCalls()
	if before < 2 {
		t.Fatalf("挂着时每 2 秒应轮询一次，读取 %d 次", before)
	}
	d.click(gfx.RoleButton, locales.WebCaptureBack())
	if d.screen.documents {
		t.Fatal("返回钮应回到任务队列")
	}
	if !d.has(gfx.RoleHeading, locales.ImportsTitle()) {
		t.Errorf("回到队列后应看到队列页头；画面上有 %q", d.session.Texts())
	}
	d.dclock.Advance(10 * time.Second)
	d.frames(3)
	if got := d.listCalls(); got != before {
		t.Errorf("离开之后不应再轮询：%d → %d", before, got)
	}
	d.openPage()
	if got := d.listCalls(); got != before+1 {
		t.Errorf("再进来应立刻读一次：%d → %d", before, got)
	}
	d.poll()
	if got := d.listCalls(); got != before+2 {
		t.Errorf("再进来之后应恢复轮询：%d → %d", before, got)
	}
}

// 表单在离开与回来之间保留：输入框里的字与 Store 里的表单值一致，不会回到空白。
func TestDocPageFormSurvivesLeavingAndReturning(t *testing.T) {
	d := openedDocs(t, nil)
	d.fill(locales.WebCaptureEntryUrl(), "https://docs.example.com/guide/intro.html")
	d.click(gfx.RoleButton, locales.WebCaptureBack())
	d.openPage()
	if got := d.field(locales.WebCaptureEntryUrl()); got != "https://docs.example.com/guide/intro.html" {
		t.Errorf("回来后入口网址应保留，得 %q", got)
	}
	if n, _ := d.node(gfx.RoleButton, locales.WebCaptureStart()); n.Disabled {
		t.Error("回来后范围仍有效，开始按钮应可点")
	}
}

// 这一帧没画文档站页（切走了模块或页面）而它还挂着：EndFrame 把它摘掉；画着的帧里不动它。
func TestDocPageEndFrameDetachesWhenNotDrawn(t *testing.T) {
	d := openedDocs(t, nil)
	d.screen.EndFrame()
	if d.dclock.pending() == 0 {
		t.Fatal("这一帧画着文档站页，EndFrame 不应摘掉轮询")
	}
	d.screen.EndFrame() // 上一次 EndFrame 清掉了「画过」标记；没有再画就是离开了
	if d.dclock.pending() != 0 {
		t.Fatal("没有再画文档站页，EndFrame 应摘掉轮询")
	}
	before := d.listCalls()
	d.dclock.Advance(10 * time.Second)
	if d.listCalls() != before {
		t.Error("摘掉之后不应再读取")
	}
	d.frames(2) // 页面又被画出来：重新挂上
	if d.dclock.pending() == 0 {
		t.Error("再次画出文档站页应重新挂上轮询")
	}
}

// 关窗口：Close 停掉轮询。
func TestDocPageCloseStopsPolling(t *testing.T) {
	d := openedDocs(t, nil)
	d.screen.Close()
	if d.dclock.pending() != 0 {
		t.Error("Close 应停掉轮询")
	}
	var nilSite Screen
	nilSite.EndFrame() // 没有装配 DocSite 的 Screen 不崩
	nilSite.Close()
}

// ---- 表单与实际范围 ----

// 填入口网址：实际范围实时跟着变（origin + 入口所在目录），开始按钮随之可点。
func TestDocPageScopePreviewFollowsTheEntryUrl(t *testing.T) {
	d := openedDocs(t, nil)
	d.fill(locales.WebCaptureEntryUrl(), "https://docs.example.com/guide/intro.html")
	want := locales.WebCaptureActualScope() + "https://docs.example.com/guide/" + locales.WebCaptureScopeHint()
	if !d.hasText(want) {
		t.Fatalf("应预览入口所在目录；画面上有 %q", d.session.Texts())
	}
	if n, _ := d.node(gfx.RoleButton, locales.WebCaptureStart()); n.Disabled {
		t.Error("范围有效时开始按钮应可点")
	}
	if got := d.store.View().Form.URL; got != "https://docs.example.com/guide/intro.html" {
		t.Errorf("Store 里的入口应与输入一致，得 %q", got)
	}
}

// 目录范围覆盖入口所在目录；不是 / 开头的目录无效，开始按钮禁用；清空后回到入口所在目录。
func TestDocPageDirectoryOverridesAndInvalidDirectoryDisablesStart(t *testing.T) {
	d := openedDocs(t, nil)
	d.fill(locales.WebCaptureEntryUrl(), "https://docs.example.com/guide/intro.html")
	d.fill(locales.WebCaptureDirectoryScope(), "/guide/api")
	if !d.hasText(locales.WebCaptureActualScope() + "https://docs.example.com/guide/api/" + locales.WebCaptureScopeHint()) {
		t.Fatalf("目录范围应覆盖入口目录；画面上有%q", d.session.Texts())
	}
	d.fill(locales.WebCaptureDirectoryScope(), "guide")
	if !d.hasText(locales.WebCaptureActualScope()+locales.WebCaptureValidScope()+locales.WebCaptureScopeHint()) || !d.disabledStart() {
		t.Fatalf("目录没有以 / 开头应无效且禁用开始；画面上有 %q", d.session.Texts())
	}
	d.fill(locales.WebCaptureDirectoryScope(), "")
	if !d.hasText(locales.WebCaptureActualScope()+"https://docs.example.com/guide/"+locales.WebCaptureScopeHint()) || d.disabledStart() {
		t.Fatalf("清空目录应回到入口所在目录；画面上有 %q", d.session.Texts())
	}
}

func (d *docRig) disabledStart() bool {
	d.t.Helper()
	n, ok := d.node(gfx.RoleButton, locales.WebCaptureStart())
	if !ok {
		d.t.Fatal("找不到「开始采集」按钮")
	}
	return n.Disabled
}

// 非 http(s) 网址无效。
func TestDocPageNonHTTPEntryIsInvalid(t *testing.T) {
	d := openedDocs(t, nil)
	d.fill(locales.WebCaptureEntryUrl(), "ftp://docs.example.com/guide/")
	if !d.disabledStart() {
		t.Error("非 http(s) 入口应禁用开始")
	}
}

// 数值框：输入的整数进入表单；读不出来的（空串、字母）按 0 交给主进程校验，而不是悄悄换成默认值。
func TestDocPageNumberFieldsFeedTheForm(t *testing.T) {
	d := openedDocs(t, nil)
	d.fill(locales.WebCapturePageLimit(), "120")
	d.fill(locales.WebCaptureDepthLimit(), "4")
	if f := d.store.View().Form; f.MaxPages != 120 || f.MaxDepth != 4 {
		t.Fatalf("数值应进入表单：%+v", f)
	}
	d.fill(locales.WebCapturePageLimit(), "")
	if got := d.store.View().Form.MaxPages; got != 0 {
		t.Errorf("清空的最多页数应按 0 交给主进程，得 %d", got)
	}
	d.fill(locales.WebCaptureDepthLimit(), "abc")
	if got := d.store.View().Form.MaxDepth; got != 0 {
		t.Errorf("读不出来的深度应按 0 交给主进程，得 %d", got)
	}
}

// 目标合集下拉：第一项是「未归合集」，其后是知识库的全部合集；选中后创建批次带上它。
func TestDocPageCollectionSelect(t *testing.T) {
	d := openedDocs(t, func(_ *servicestest.DocSite, cols *fakeCollections) {
		cols.set([]services.LibraryCollection{{ID: "c1", Name: "工作"}, {ID: "c2", Name: "灵感"}}, nil)
	})
	d.click(gfx.RoleCombobox, locales.WebCaptureCollection())
	for _, name := range []string{locales.WebCaptureUnfiled(), "工作", "灵感"} {
		if !d.has(gfx.RoleOption, name) {
			t.Errorf("下拉里缺少「%s」", name)
		}
	}
	d.click(gfx.RoleOption, "灵感")
	if got := d.store.View().Form.CollectionID; got != "c2" {
		t.Fatalf("选中后表单的合集应是 c2，得 %q", got)
	}
	if n, _ := d.node(gfx.RoleCombobox, locales.WebCaptureCollection()); n.Value != "灵感" {
		t.Errorf("触发器应显示所选合集，得 %q", n.Value)
	}
	d.fill(locales.WebCaptureEntryUrl(), "https://docs.example.com/guide/")
	d.click(gfx.RoleButton, locales.WebCaptureStart())
	in := d.site.Created()
	if len(in) != 1 || in[0].CollectionID == nil || *in[0].CollectionID != "c2" {
		t.Fatalf("创建应带上所选合集：%+v", in)
	}
	d.pick(locales.WebCaptureCollection(), locales.WebCaptureUnfiled())
	if got := d.store.View().Form.CollectionID; got != "" {
		t.Errorf("选回「未归合集」应清空，得 %q", got)
	}
}

// 合集读不出来：下拉里只剩「未归合集」，不影响填表；原因进日志、不弹提示。
func TestDocPageCollectionLoadFailureLeavesOnlyUnfiled(t *testing.T) {
	d := openedDocs(t, func(_ *servicestest.DocSite, cols *fakeCollections) { cols.set(nil, errors.New("db locked")) })
	d.click(gfx.RoleCombobox, locales.WebCaptureCollection())
	if !d.has(gfx.RoleOption, locales.WebCaptureUnfiled()) || d.count(gfx.RoleOption, "工作") != 0 {
		t.Error("读不出合集时只应有「未归合集」")
	}
	if d.notifier.Len() != 0 {
		t.Error("合集读取失败是后台失败，不应弹提示")
	}
	if e := d.log.entries(); len(e) != 1 || e[0] != "webCapture.collections|db locked" {
		t.Errorf("原因应进日志：%v", e)
	}
}

// 重复处理下拉：「检查原文更新」进入创建的输入。
func TestDocPagePolicySelect(t *testing.T) {
	d := openedDocs(t, nil)
	d.click(gfx.RoleCombobox, locales.WebCaptureDuplicatePolicy())
	for _, name := range []string{locales.WebCaptureSkipDuplicates(), locales.WebCaptureUpdatePolicy()} {
		if !d.has(gfx.RoleOption, name) {
			t.Errorf("下拉里缺少「%s」", name)
		}
	}
	d.click(gfx.RoleOption, locales.WebCaptureUpdatePolicy())
	if got := d.store.View().Form.Policy; got != services.CrawlDuplicateUpdate {
		t.Fatalf("表单策略应是 update，得 %q", got)
	}
	d.fill(locales.WebCaptureEntryUrl(), "https://docs.example.com/guide/")
	d.click(gfx.RoleButton, locales.WebCaptureStart())
	if in := d.site.Created(); len(in) != 1 || in[0].DuplicatePolicy != services.CrawlDuplicateUpdate {
		t.Fatalf("创建应带上「检查原文更新」：%+v", in)
	}
}

// ---- 创建批次 ----

// 点「开始采集」：以表单创建批次（用途 documents、入口模式 directory），批次出现在列表里，表单保留。
func TestDocPageStartCreatesTheBatch(t *testing.T) {
	d := openedDocs(t, nil)
	d.fill(locales.WebCaptureEntryUrl(), "https://docs.example.com/guide/intro.html")
	d.fill(locales.WebCaptureDirectoryScope(), "/guide")
	d.fill(locales.WebCapturePageLimit(), "30")
	d.fill(locales.WebCaptureDepthLimit(), "1")
	d.click(gfx.RoleButton, locales.WebCaptureStart())
	got := d.site.Created()
	if len(got) != 1 {
		t.Fatalf("应创建一个批次，得 %d 个", len(got))
	}
	in := got[0]
	if in.Purpose != "documents" || in.MaxPages != 30 || in.MaxDepth != 1 || in.DuplicatePolicy != services.CrawlDuplicateSkip || in.CollectionID != nil {
		t.Errorf("创建输入不对：%+v", in)
	}
	if len(in.Seeds) != 1 || in.Seeds[0] != (services.CrawlSeed{URL: "https://docs.example.com/guide/intro.html", Mode: services.CrawlSeedDirectory, Directory: "/guide"}) {
		t.Errorf("入口不对：%+v", in.Seeds)
	}
	if !d.has(gfx.RoleButton, "https://docs.example.com/guide/intro.html \u00b7 "+locales.WebCapturePending()) {
		t.Errorf("创建之后应读到新批次并列出；画面上有 %q", d.session.Texts())
	}
	if got := d.field(locales.WebCaptureEntryUrl()); got != "https://docs.example.com/guide/intro.html" {
		t.Errorf("创建后表单应保留，得 %q", got)
	}
}

// 创建失败：弹带原因的提示（详情可复制）、表单保持原样、按钮恢复可点，改好再点就是重试。
func TestDocPageStartFailureReportsAndRetries(t *testing.T) {
	d := openedDocs(t, nil)
	d.site.FailOn("create", errors.New("maxPages must be between 1 and 300"))
	d.fill(locales.WebCaptureEntryUrl(), "https://docs.example.com/guide/")
	d.fill(locales.WebCapturePageLimit(), "9999")
	d.click(gfx.RoleButton, locales.WebCaptureStart())
	if !d.toastShown(locales.CommonOperationFailed(locales.WebCaptureOperation())) {
		t.Fatalf("应弹「操作失败」提示；画面上有 %q", d.session.Texts())
	}
	d.click(gfx.RoleButton, locales.CommonViewDetail())
	if !d.hasTextLike("maxPages must be between 1 and 300") {
		t.Errorf("详情里应有原因；画面上有 %q", d.session.Texts())
	}
	if d.disabledStart() {
		t.Error("失败后开始按钮应恢复可点")
	}
	if got := d.field(locales.WebCapturePageLimit()); got != "9999" {
		t.Errorf("失败后表单应保持原样，得 %q", got)
	}
	d.site.FailOn("create", nil)
	d.fill(locales.WebCapturePageLimit(), "30")
	d.click(gfx.RoleButton, locales.WebCaptureStart())
	if len(d.site.Created()) != 1 {
		t.Fatalf("改好之后再点应创建成功，已创建 %d 个", len(d.site.Created()))
	}
}

// 创建进行中：开始按钮禁用（不能连点出两个批次），结束后恢复。
func TestDocPageStartIsDisabledWhileCreating(t *testing.T) {
	var gated *gatedDocSite
	d := newDocRigWith(t, func(site *servicestest.DocSite) services.DocSite {
		gated = &gatedDocSite{DocSite: site, entered: make(chan struct{}), release: make(chan struct{})}
		return gated
	}, nil)
	d.store.Spawn = func(fn func()) { go fn() }
	d.openPage()
	d.fill(locales.WebCaptureEntryUrl(), "https://docs.example.com/guide/")
	d.click(gfx.RoleButton, locales.WebCaptureStart())
	waitFor(t, "创建已发出", func() bool {
		select {
		case <-gated.entered:
			return true
		default:
			return false
		}
	})
	waitFor(t, "按钮禁用", func() bool { d.frames(1); return d.disabledStart() })
	close(gated.release)
	waitFor(t, "创建结束、按钮恢复", func() bool { d.frames(1); return !d.disabledStart() })
	if len(d.site.Created()) != 1 {
		t.Errorf("只应创建一个批次，得 %d 个", len(d.site.Created()))
	}
}

// ---- 批次列表 ----

// 批次标题是「入口网址 · 状态」，下面是各状态的页数（固定顺序）、批次错误。
func TestDocPageJobCardShowsTitleCountsAndError(t *testing.T) {
	job := crawlJob("j1", services.CrawlJobRunning, map[string]int{
		services.CrawlPageFailed: 1, services.CrawlPageAdded: 3, services.CrawlPagePending: 2,
	})
	job.Error = "robots.txt unreachable"
	d := openedDocs(t, setJobs(job))
	if !d.has(gfx.RoleButton, jobTitle("j1", locales.WebCaptureCapturing())) {
		t.Fatalf("应有批次标题；画面上有 %q", d.session.Texts())
	}
	counts := locales.WebCapturePending() + " 2 \u00b7 " + locales.WebCaptureAdded() + " 3 \u00b7 " + locales.WebCaptureFailed() + " 1"
	if !d.hasText(counts) {
		t.Errorf("应按固定顺序列出各状态页数 %q；画面上有 %q", counts, d.session.Texts())
	}
	if !d.hasText("robots.txt unreachable") {
		t.Error("批次的错误应显示出来")
	}
}

// 各状态的标题文案。
func TestDocPageJobStatusLabels(t *testing.T) {
	cases := map[string]string{
		services.CrawlJobPending: locales.WebCapturePending(), services.CrawlJobRunning: locales.WebCaptureCapturing(),
		services.CrawlJobPaused: locales.WebCapturePaused(), services.CrawlJobInterrupted: locales.WebCaptureInterrupted(),
		services.CrawlJobCompleted: locales.WebCaptureCompleted(), services.CrawlJobCanceled: locales.WebCaptureCanceled(),
	}
	for status, label := range cases {
		d := openedDocs(t, setJobs(crawlJob("j1", status, nil)))
		if !d.has(gfx.RoleButton, jobTitle("j1", label)) {
			t.Errorf("状态 %s 的标题应写「%s」；画面上有 %q", status, label, d.session.Texts())
		}
	}
}

// 读到的批次为空：空态说明；读取失败时不写（那不是「没有批次」）。
func TestDocPageEmptyStateOnlyAfterASuccessfulRead(t *testing.T) {
	d := openedDocs(t, nil)
	if !d.hasText(locales.NativeDocSiteEmpty()) {
		t.Errorf("读到空列表应写空态说明；画面上有 %q", d.session.Texts())
	}
	failing := openedDocs(t, func(site *servicestest.DocSite, _ *fakeCollections) { site.FailOn("list", errors.New("db locked")) })
	if failing.hasText(locales.NativeDocSiteEmpty()) {
		t.Error("读取失败不能画成「没有批次」")
	}
	// 还没读到（读取卡住）时也不写空态。
	pending := newDocRig(t, nil)
	release := pending.site.HoldList()
	pending.store.Spawn = func(fn func()) { go fn() }
	pending.openPage()
	if pending.hasText(locales.NativeDocSiteEmpty()) {
		t.Error("还没读到时不能写空态")
	}
	release()
	waitFor(t, "读到空列表", func() bool { pending.frames(1); return pending.hasText(locales.NativeDocSiteEmpty()) })
}

// 读取失败整块错误态（带原因与重试），不是空列表；排除故障后点重试恢复。
func TestDocPageLoadErrorStateAndRetry(t *testing.T) {
	d := openedDocs(t, func(site *servicestest.DocSite, _ *fakeCollections) {
		site.SetJobs(crawlJob("j1", services.CrawlJobRunning, nil))
		site.FailOn("list", errors.New("database is locked"))
	})
	if !d.has(gfx.RoleStatus, "") || !d.hasText(locales.CommonLoadFailed()) || !d.hasText("database is locked") {
		t.Fatalf("读取失败应显示错误态与原因；画面上有 %q", d.session.Texts())
	}
	if d.has(gfx.RoleButton, jobTitle("j1", locales.WebCaptureCapturing())) {
		t.Error("读取失败时不应有批次")
	}
	d.site.FailOn("list", nil)
	d.click(gfx.RoleButton, locales.CommonRetry())
	if d.hasText(locales.CommonLoadFailed()) {
		t.Error("重试成功后错误态应消失")
	}
	if !d.has(gfx.RoleButton, jobTitle("j1", locales.WebCaptureCapturing())) {
		t.Errorf("重试成功后应列出批次；画面上有 %q", d.session.Texts())
	}
}

// 轮询把后台新增的批次与变化的状态带进画面。
func TestDocPagePollRefreshesJobs(t *testing.T) {
	d := openedDocs(t, setJobs(crawlJob("j1", services.CrawlJobRunning, nil)))
	d.site.SetJobs(
		crawlJob("j2", services.CrawlJobPending, nil),
		crawlJob("j1", services.CrawlJobCompleted, map[string]int{services.CrawlPageAdded: 5}),
	)
	d.poll()
	if !d.has(gfx.RoleButton, jobTitle("j2", locales.WebCapturePending())) || !d.has(gfx.RoleButton, jobTitle("j1", locales.WebCaptureCompleted())) {
		t.Fatalf("轮询后应出现新批次并更新状态；画面上有 %q", d.session.Texts())
	}
}

// ---- 页面明细 ----

func pagesFixture() func(*servicestest.DocSite, *fakeCollections) {
	return func(site *servicestest.DocSite, _ *fakeCollections) {
		site.SetJobs(
			crawlJob("j1", services.CrawlJobCompleted, map[string]int{services.CrawlPageAdded: 1, services.CrawlPageFailed: 1}),
			crawlJob("j2", services.CrawlJobRunning, map[string]int{services.CrawlPagePending: 1}),
		)
		added := crawlPage("j1", "p1", services.CrawlPageAdded)
		added.ItemID = "item-1"
		failed := crawlPage("j1", "p2", services.CrawlPageFailed)
		failed.Error = "HTTP 404"
		site.SetPages("j1", added, failed)
		site.SetPages("j2", crawlPage("j2", "q1", services.CrawlPagePending))
	}
}

// 点批次标题展开它的页面明细：每页「状态 · 网址」，失败页下面写原因；别的批次的页面不显示；
// 轮询带来页面状态的变化。
func TestDocPageSelectJobShowsItsPages(t *testing.T) {
	d := openedDocs(t, pagesFixture())
	if d.hasTextLike("https://a.com/p1") {
		t.Fatal("没选批次时不应有页面明细")
	}
	d.click(gfx.RoleButton, jobTitle("j1", locales.WebCaptureCompleted()))
	if d.store.View().Selected != "j1" {
		t.Fatalf("应选中 j1，得 %q", d.store.View().Selected)
	}
	if !d.hasText(locales.WebCaptureAdded()+" \u00b7 https://a.com/p1") || !d.hasText(locales.WebCaptureFailed()+" \u00b7 https://a.com/p2") {
		t.Fatalf("应列出 j1 的页面；画面上有 %q", d.session.Texts())
	}
	if !d.hasText("HTTP 404") {
		t.Error("失败页下面应写原因")
	}
	if d.hasTextLike("https://a.com/q1") {
		t.Error("别的批次的页面不应显示")
	}
	// 切到 j2：j1 的页面收起、显示 j2 的。
	d.click(gfx.RoleButton, jobTitle("j2", locales.WebCaptureCapturing()))
	if d.hasTextLike("https://a.com/p1") || !d.hasText(locales.WebCapturePending()+" \u00b7 https://a.com/q1") {
		t.Errorf("切换后应只显示 j2 的页面；画面上有 %q", d.session.Texts())
	}
	// 轮询：页面状态变化跟上。
	d.site.SetPages("j2", crawlPage("j2", "q1", services.CrawlPageAdded))
	d.poll()
	if !d.hasText(locales.WebCaptureAdded() + " \u00b7 https://a.com/q1") {
		t.Errorf("轮询后页面状态应更新；画面上有 %q", d.session.Texts())
	}
}

// 已入库的页面（有关联条目）是可点的按钮，点了经 OpenItem 打开知识库里的条目；没有条目的页面是纯文字。
func TestDocPageClickingAStoredPageOpensTheItem(t *testing.T) {
	d := openedDocs(t, pagesFixture())
	d.click(gfx.RoleButton, jobTitle("j1", locales.WebCaptureCompleted()))
	added := locales.WebCaptureAdded() + " \u00b7 https://a.com/p1"
	failed := locales.WebCaptureFailed() + " \u00b7 https://a.com/p2"
	n, ok := d.node(gfx.RoleButton, added)
	if !ok {
		t.Fatalf("已入库的页面应是按钮；画面上有 %q", d.session.Texts())
	}
	if n.Description != locales.ImportsOpenItem() && n.Hint != locales.ImportsOpenItem() {
		t.Errorf("按钮应带「%s」的提示：%+v", locales.ImportsOpenItem(), n)
	}
	if d.has(gfx.RoleButton, failed) {
		t.Error("没有关联条目的页面不应可点")
	}
	d.click(gfx.RoleButton, added)
	if !slices.Equal(d.opened, []string{"item-1"}) {
		t.Errorf("点已入库的页面应打开条目 item-1，实际 %v", d.opened)
	}
}

// 没有注入 OpenItem 时已入库的页面是纯文字，不是什么都不做的按钮。
func TestDocPageStoredPageIsPlainTextWithoutOpenItem(t *testing.T) {
	d := openedDocs(t, pagesFixture())
	d.screen.Hooks.OpenItem = nil
	d.click(gfx.RoleButton, jobTitle("j1", locales.WebCaptureCompleted()))
	if d.has(gfx.RoleButton, locales.WebCaptureAdded()+" \u00b7 https://a.com/p1") {
		t.Error("没有 OpenItem 时不应有可点的页面")
	}
	if !d.hasText(locales.WebCaptureAdded() + " \u00b7 https://a.com/p1") {
		t.Error("页面仍应显示")
	}
}

// 选中的批次还没有页面：写一句说明；读回来之前不写。
func TestDocPageSelectedJobWithoutPages(t *testing.T) {
	d := openedDocs(t, setJobs(crawlJob("j1", services.CrawlJobRunning, nil)))
	d.click(gfx.RoleButton, jobTitle("j1", locales.WebCaptureCapturing()))
	if !d.hasText(locales.NativeDocSiteNoPages()) {
		t.Errorf("没有页面应写说明；画面上有 %q", d.session.Texts())
	}
}

// 页面很多时明细在固定高度的列表里滚动，整页仍可用。
func TestDocPageManyPagesScrollInsideTheList(t *testing.T) {
	d := openedDocs(t, func(site *servicestest.DocSite, _ *fakeCollections) {
		site.SetJobs(crawlJob("j1", services.CrawlJobRunning, map[string]int{services.CrawlPageAdded: 300}))
		var pages []services.CrawlPage
		for i := 0; i < 300; i++ {
			p := crawlPage("j1", "p"+strconv.Itoa(i), services.CrawlPageAdded)
			p.ItemID = "item-" + strconv.Itoa(i) // 有关联条目的页是按钮：被裁掉的按钮不进无障碍树，以此判断哪些在视口里
			pages = append(pages, p)
		}
		site.SetPages("j1", pages...)
	})
	d.click(gfx.RoleButton, jobTitle("j1", locales.WebCaptureCapturing()))
	first := locales.WebCaptureAdded() + " \u00b7 https://a.com/p0"
	last := locales.WebCaptureAdded() + " \u00b7 https://a.com/p299"
	if !d.has(gfx.RoleButton, first) {
		t.Fatalf("应显示第一页；画面上有 %q", d.session.Texts()[:10])
	}
	if d.has(gfx.RoleButton, last) {
		t.Fatal("最后一页应在列表的视口之外")
	}
	// 在列表里向下滚到底：末尾的页进入视口，第一页出去（窗口比列表矮，最后一两页仍可能在窗口之外，故取靠近末尾的一页）。
	near := locales.WebCaptureAdded() + " \u00b7 https://a.com/p290"
	n, _ := d.node(gfx.RoleButton, first)
	d.scrollBy(n.Bounds.X+n.Bounds.Width/2, n.Bounds.Y+n.Bounds.Height/2, 30000)
	if !d.has(gfx.RoleButton, near) || d.has(gfx.RoleButton, first) {
		t.Errorf("滚到底之后应看到靠近末尾的页、第一页出视口；first=%v near=%v", d.has(gfx.RoleButton, first), d.has(gfx.RoleButton, near))
	}
}

// ---- 批次控制 ----

func (d *docRig) actionNames() []string {
	var out []string
	for _, name := range []string{locales.WebCapturePause(), locales.WebCaptureResume(), locales.WebCaptureRetryFailed(), locales.WebCaptureCancel()} {
		if d.has(gfx.RoleButton, name) {
			out = append(out, name)
		}
	}
	return out
}

// 按钮随批次状态显隐：运行中「暂停」、其余未取消的「继续」；「重试失败项」总在；已结束与已取消的没有「取消」。
func TestDocPageActionVisibilityFollowsStatus(t *testing.T) {
	pause, resume, retry, cancel := locales.WebCapturePause(), locales.WebCaptureResume(), locales.WebCaptureRetryFailed(), locales.WebCaptureCancel()
	cases := []struct {
		status string
		want   []string
	}{
		{services.CrawlJobPending, []string{resume, retry, cancel}},
		{services.CrawlJobRunning, []string{pause, retry, cancel}},
		{services.CrawlJobPaused, []string{resume, retry, cancel}},
		{services.CrawlJobInterrupted, []string{resume, retry, cancel}},
		{services.CrawlJobCompleted, []string{resume, retry}},
		{services.CrawlJobCanceled, []string{retry}},
	}
	for _, c := range cases {
		d := openedDocs(t, setJobs(crawlJob("j1", c.status, nil)))
		if got := d.actionNames(); !slices.Equal(got, c.want) {
			t.Errorf("状态 %s 的按钮应是 %q，实际 %q", c.status, c.want, got)
		}
	}
}

// 「重试失败项」没有失败页时禁用；有失败页时可点。
func TestDocPageRetryEnabledOnlyWithFailedPages(t *testing.T) {
	d := openedDocs(t, setJobs(
		crawlJob("clean", services.CrawlJobCompleted, map[string]int{services.CrawlPageAdded: 2}),
		crawlJob("bad", services.CrawlJobCompleted, map[string]int{services.CrawlPageFailed: 1}),
	))
	retry := locales.WebCaptureRetryFailed()
	first, _ := d.session.AccessByRole(gfx.RoleButton, retry)
	if !first.Disabled {
		t.Error("没有失败页的批次，「重试失败项」应禁用")
	}
	d.clickNth(gfx.RoleButton, retry, 0) // 禁用的按钮点了没有反应
	if got := d.site.Calls(); slices.Contains(got, "retry:clean") {
		t.Fatalf("禁用的按钮不应发出请求：%v", got)
	}
	d.clickNth(gfx.RoleButton, retry, 1)
	if !slices.Contains(d.site.Calls(), "retry:bad") {
		t.Fatalf("有失败页的批次应重试；调用 %v", d.site.Calls())
	}
}

// 暂停 → 继续 → 取消：每次点击发出对应请求，之后重新读取，按钮随新状态翻转。
func TestDocPagePauseResumeCancel(t *testing.T) {
	d := openedDocs(t, setJobs(crawlJob("j1", services.CrawlJobRunning, nil)))
	d.click(gfx.RoleButton, locales.WebCapturePause())
	if !slices.Contains(d.site.Calls(), "pause:j1") {
		t.Fatalf("应发出暂停；调用 %v", d.site.Calls())
	}
	if !d.has(gfx.RoleButton, jobTitle("j1", locales.WebCapturePaused())) || !d.has(gfx.RoleButton, locales.WebCaptureResume()) || d.has(gfx.RoleButton, locales.WebCapturePause()) {
		t.Fatalf("暂停后标题应写「已暂停」且按钮翻成「继续」；画面上有 %q", d.session.Texts())
	}
	d.click(gfx.RoleButton, locales.WebCaptureResume())
	if !slices.Contains(d.site.Calls(), "resume:j1") || !d.has(gfx.RoleButton, locales.WebCapturePause()) {
		t.Fatalf("继续后应回到运行中；调用 %v", d.site.Calls())
	}
	d.click(gfx.RoleButton, locales.WebCaptureCancel())
	if !slices.Contains(d.site.Calls(), "cancel:j1") {
		t.Fatalf("应发出取消；调用 %v", d.site.Calls())
	}
	if !d.has(gfx.RoleButton, jobTitle("j1", locales.WebCaptureCanceled())) || d.has(gfx.RoleButton, locales.WebCaptureCancel()) ||
		d.has(gfx.RoleButton, locales.WebCaptureResume()) {
		t.Fatalf("取消后标题写「已取消」，且不再有取消与继续；画面上有 %q", d.session.Texts())
	}
}

// 重试失败项：发出请求并重新读取，失败页回到排队。
func TestDocPageRetryFailedRequeues(t *testing.T) {
	d := openedDocs(t, setJobs(crawlJob("j1", services.CrawlJobCompleted, map[string]int{services.CrawlPageFailed: 2})))
	d.click(gfx.RoleButton, locales.WebCaptureRetryFailed())
	if !slices.Contains(d.site.Calls(), "retry:j1") {
		t.Fatalf("应发出重试；调用 %v", d.site.Calls())
	}
	if !d.has(gfx.RoleButton, jobTitle("j1", locales.WebCaptureCapturing())) {
		t.Errorf("重试后批次应回到运行中；画面上有 %q", d.session.Texts())
	}
}

// 操作失败：弹带原因的提示，状态不变；按钮仍在，可重试。
func TestDocPageOperationFailureReportsTheReason(t *testing.T) {
	d := openedDocs(t, setJobs(crawlJob("j1", services.CrawlJobRunning, nil)))
	d.site.FailOn("pause:j1", errors.New("pause refused"))
	d.click(gfx.RoleButton, locales.WebCapturePause())
	if !d.toastShown(locales.CommonOperationFailed(locales.WebCaptureOperation())) {
		t.Fatalf("应弹「操作失败」提示；画面上有 %q", d.session.Texts())
	}
	d.click(gfx.RoleButton, locales.CommonViewDetail())
	if !d.hasTextLike("pause refused") {
		t.Errorf("详情里应有原因；画面上有 %q", d.session.Texts())
	}
	if !d.has(gfx.RoleButton, jobTitle("j1", locales.WebCaptureCapturing())) || !d.has(gfx.RoleButton, locales.WebCapturePause()) {
		t.Error("失败后状态与按钮应保持原样")
	}
	d.site.FailOn("pause:j1", nil)
	d.click(gfx.RoleButton, locales.WebCapturePause())
	if !d.has(gfx.RoleButton, jobTitle("j1", locales.WebCapturePaused())) {
		t.Error("排除故障后再点应暂停成功")
	}
}

// 重试按钮在有操作进行时禁用（不并发发出第二个）。
func TestDocPageRetryDisabledWhileBusy(t *testing.T) {
	d := openedDocs(t, setJobs(crawlJob("j1", services.CrawlJobCompleted, map[string]int{services.CrawlPageFailed: 1})))
	d.store.mu.Lock()
	d.store.view.Busy = true
	d.store.mu.Unlock()
	d.frames(2)
	if n, _ := d.node(gfx.RoleButton, locales.WebCaptureRetryFailed()); !n.Disabled {
		t.Error("有操作进行时「重试失败项」应禁用")
	}
}
