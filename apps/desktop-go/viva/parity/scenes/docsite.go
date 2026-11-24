package scenes

import (
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"

	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/parity"
	importsscreen "guizhi-mygo-shell/viva/screens/imports"
)

// 采集文档站整页（渲染层 components/imports/DocumentSitePanel.tsx，M3-6b）的整窗对照场景：rail + 顶栏 + 二级侧栏 + 工作区里的文档站页。
// 与 React 采集器（scripts/parity/scenes/react-docsite-scenes.mjs、lib/react-docsite.mjs）一一对应：同一份夹具
// （viva/parity/testdata/docsite-fixture.json）、同样的操作。React 一侧在页内替换 window.api.webCapture 的读取与操作接口，
// Viva 一侧用假服务（servicestest.DocSite）装配同样的批次与页面明细。
// 采集队列本身取空队列（与 React 一侧一致）：文档站页盖在工作区里，侧栏只显示队列的概览。

// newDocSiteStore 按夹具里名为 variant 的数据组装配文档站页的状态：批次与页面明细进假服务，后台读取同步执行、轮询不起真实定时器，
// 画面因此是确定的（首帧就是读完的结果）。
func newDocSiteStore(variant string) *importsscreen.DocSiteStore {
	fix, err := parity.LoadDocSiteFixture()
	if err != nil {
		panic(err) // 夹具是编译进来的常量，解码失败是程序错误
	}
	v, err := fix.Variant(variant)
	if err != nil {
		panic(err)
	}
	site := &servicestest.DocSite{}
	site.SetJobs(v.Jobs...)
	for id, pages := range v.Pages {
		site.SetPages(id, pages...)
	}
	store := importsscreen.NewDocSiteStore(site, nil, kit.Guard{}, nil)
	store.After = func(time.Duration, func()) func() { return func() {} }
	store.Spawn = func(fn func()) { fn() }
	return store
}

// openDocSite 对应 React 采集器点工作区页头的「采集文档站」：切到文档站整页，等入场后的读取落定。
func openDocSite(s *headless.Session, _ *importsEnv) error {
	if err := parity.ClickRole(s, gfx.RoleButton, locales.WebCaptureImportSite()); err != nil {
		return err
	}
	return s.Rest()
}

// fillDocField 对应 React 采集器对标签文字为 label 的输入框 fill：点进输入框、键入文字，再让这一帧把文字推给 Store。
// label 在执行步骤时才取（英文变体在场景登记之后才切换界面语言）。
func fillDocField(label func() string, text string) importsSteps {
	return func(s *headless.Session, _ *importsEnv) error {
		if err := parity.ClickRole(s, gfx.RoleTextField, label()); err != nil {
			return err
		}
		if err := s.Type(text); err != nil {
			return err
		}
		return s.Rest()
	}
}

// selectDocJob 对应 React 采集器点批次标题（「入口网址 · 状态」）展开它的页面明细。
func selectDocJob(jobID string) importsSteps {
	return func(s *headless.Session, e *importsEnv) error {
		fix, err := parity.LoadDocSiteFixture()
		if err != nil {
			return err
		}
		v, err := fix.Variant(e.docSite)
		if err != nil {
			return err
		}
		job, err := v.Job(jobID)
		if err != nil {
			return err
		}
		title := importsscreen.JobTitleURL(job) + " \u00b7 " + importsscreen.JobStatusLabel(job.Status)
		if err := parity.ClickRole(s, gfx.RoleButton, title); err != nil {
			return err
		}
		return s.Rest()
	}
}

// docSiteSteps 依次执行：点进文档站页、再做 steps，最后把指针移开并清掉焦点（输入框与按钮点过会持有焦点；渲染层采集前同样 blur）。
func docSiteSteps(steps ...importsSteps) importsSteps {
	return func(s *headless.Session, e *importsEnv) error {
		if err := openDocSite(s, e); err != nil {
			return err
		}
		for _, step := range steps {
			if err := step(s, e); err != nil {
				return err
			}
		}
		return parity.ParkAndBlur(s)
	}
}

// docSiteScene 登记一个文档站场景：采集队列取空队列，文档站页取夹具里名为 variant 的数据组。
func docSiteScene(id, variant string, steps ...importsSteps) parity.Scene {
	return importsScene(id, importsOptions{mode: "empty", docSite: variant}, docSiteSteps(steps...))
}

// docSiteScopeSteps 是「已填入口网址与目录范围」：依次往入口网址与目录范围里键入夹具里的内容。
func docSiteScopeSteps() []importsSteps {
	fix, err := parity.LoadDocSiteFixture()
	if err != nil {
		panic(err)
	}
	return []importsSteps{
		fillDocField(locales.WebCaptureEntryUrl, fix.Scope.URL),
		fillDocField(locales.WebCaptureDirectoryScope, fix.Scope.Directory),
	}
}

func init() {
	// 初始态：0 个批次，默认表单（页数 50、深度 2、未归合集、跳过重复），开始按钮禁用，范围提示是「请输入有效网址和目录」。
	registry["app.window.imports.docsite-empty"] = docSiteScene("app.window.imports.docsite-empty", "empty")
	// 已填入口网址与目录范围：实时的「实际范围」预览，开始按钮可点。
	registry["app.window.imports.docsite-scope"] = docSiteScene("app.window.imports.docsite-scope", "empty", docSiteScopeSteps()...)
	// 运行中的批次与已完成的批次，点开运行中批次的页面明细：计数、暂停 / 取消按钮、多状态页面行与失败原因。
	registry["app.window.imports.docsite-running"] = docSiteScene("app.window.imports.docsite-running", "running", selectDocJob("crawl-running"))
	// 已暂停（有失败项）、等待继续、已取消三个批次，点开已暂停批次：继续 / 重试失败项 / 取消按钮，批次错误与页面错误说明。
	registry["app.window.imports.docsite-failed"] = docSiteScene("app.window.imports.docsite-failed", "failed", selectDocJob("crawl-paused"))
}
