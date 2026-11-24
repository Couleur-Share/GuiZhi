package imports

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/internal/frametime"
	"guizhi-mygo-shell/viva/locales"
)

// TestFrameBudget 读出采集队列的帧耗时（模块完成定义第 4 条：稳态帧 ≤ 2 ms、滚动一步 ≤ 6 ms）。
//
// 无头会话的 Frame() 只记录绘制命令、不栅格化，所以量的是界面线程的出帧成本（布局 + 记录）；
// 栅格化加回读另量一次（Image()），作为真窗口里绘制成本的上界。
// 耗时与机器有关，默认跳过：VIVA_FRAME_BUDGET=1 go test ./viva/screens/imports -run TestFrameBudget -v。
// 结果记进开发文档 §8.4。滚动读数前先核对滚动确实改变了画面，不让「什么都没做」的空操作混进读数。
func TestFrameBudget(t *testing.T) {
	if os.Getenv("VIVA_FRAME_BUDGET") == "" {
		t.Skip("帧耗时读数与机器有关，默认不跑：VIVA_FRAME_BUDGET=1 时运行")
	}
	mixed := []string{
		services.ImportStatusCompleted, services.ImportStatusProcessing, services.ImportStatusFailed,
		services.ImportStatusPending, services.ImportStatusCanceled, services.ImportStatusDuplicate,
	}
	settled := []string{ // 全是终态任务：画面上没有转圈的进度图标
		services.ImportStatusCompleted, services.ImportStatusFailed, services.ImportStatusCanceled, services.ImportStatusDuplicate,
	}
	cases := []struct {
		name     string
		n        int
		statuses []string
	}{
		{"50_rows", 50, mixed},
		{"500_rows", 500, mixed},
		{"50_rows_settled", 50, settled},
	}
	for _, c := range cases {
		n := c.n
		t.Run(c.name, func(t *testing.T) {
			tasks := make([]services.ImportTask, n)
			for i := range tasks {
				status := c.statuses[i%len(c.statuses)]
				tasks[i] = mkTask(fmt.Sprintf("task-%03d", i), func(task *services.ImportTask) {
					task.Status = status
					task.DisplayName = fmt.Sprintf("第 %d 条：手写一个混合检索 RAG 系统并比较三种重排策略的效果", i)
				})
			}
			heapBefore := frametime.HeapInUse()
			r := loaded(t, pageOf(tasks...))
			t.Logf("%s 打开页面后 Go 堆增量（含无头会话）：%d KiB", c.name, (int64(frametime.HeapInUse())-int64(heapBefore))/1024)

			// 稳态帧：画面不变，连出 300 帧。
			idle, err := frametime.Measure(300, func(int) error { return r.session.Frame() })
			if err != nil {
				t.Fatalf("稳态帧失败：%v", err)
			}
			t.Logf("%s 稳态帧（布局+记录）：%s", c.name, idle)

			// 悬停帧：指针在两行之间来回，每次移动都改变悬停态（行内动作钮出现与消失）。
			points := [][2]float32{{700, 300}, {700, 380}}
			hover, err := frametime.Measure(200, func(i int) error {
				p := points[i%2]
				if err := r.session.Move(p[0], p[1]); err != nil {
					return err
				}
				return r.session.Frame()
			})
			if err != nil {
				t.Fatalf("悬停帧失败：%v", err)
			}
			t.Logf("%s 悬停帧（移动指针+出帧）：%s", c.name, hover)

			// 滚动一步：每步 40 逻辑像素。先预检：向下 20 步（800 px）画面必须变，再原路滚回。
			scrollStep := func(dy float32) error {
				if err := r.session.Scroll(gfx.ScrollEvent{X: 760, Y: 450, DeltaY: dy, Precise: true}); err != nil {
					return err
				}
				return r.session.Frame()
			}
			top := frametime.Fingerprint(r.session.Access())
			for i := 0; i < 20; i++ {
				if err := scrollStep(40); err != nil {
					t.Fatalf("预检滚动失败：%v", err)
				}
			}
			if top == frametime.Fingerprint(r.session.Access()) {
				t.Fatalf("向下滚动 800 px 之后画面没有变化，滚动读数无效")
			}
			for i := 0; i < 20; i++ {
				if err := scrollStep(-40); err != nil {
					t.Fatalf("预检回滚失败：%v", err)
				}
			}
			// 正式读数：向下 20 步、向上 20 步，共 200 步。
			scroll, err := frametime.Measure(200, func(i int) error {
				if (i/20)%2 == 1 {
					return scrollStep(-40)
				}
				return scrollStep(40)
			})
			if err != nil {
				t.Fatalf("滚动帧失败：%v", err)
			}
			t.Logf("%s 滚动一步（滚动+出帧）：%s", c.name, scroll)

			// 栅格化加回读：真窗口里绘制成本的上界。
			raster, err := frametime.Measure(30, func(int) error {
				_, err := r.session.Image()
				return err
			})
			if err != nil {
				t.Fatalf("栅格化失败：%v", err)
			}
			t.Logf("%s 栅格化+回读（Image，上界）：%s", c.name, raster)
		})
	}
}

// docBudgetTarget 是悬停读数的一个落点：角色加名称对应的控件中心。
type docBudgetTarget struct {
	role gfx.Role
	name string
}

// docBudgetScroll 是一个滚轮落点；required 为真时滚动必须改变画面（否则读数无效、测试失败），为假时没有可滚内容就跳过这一项读数。
type docBudgetScroll struct {
	name     string
	x, y     float32
	required bool
}

// docBudgetPlan 是一个文档站场景要量哪些动作。
type docBudgetPlan struct {
	hover  []docBudgetTarget // 指针在这些控件之间来回（至少两个）
	typing bool              // 量「输入一步」：在入口网址里输入、退格交替，每步重算实际范围（实时范围预览）
	scroll []docBudgetScroll
}

// docBudgetPages 造 n 个页面：状态轮换，失败页带会折行的原因；long 为真时网址长到在页面明细里折成两行以上（break-all）。
func docBudgetPages(job string, n int, long bool) []services.CrawlPage {
	statuses := []string{
		services.CrawlPageAdded, services.CrawlPagePending, services.CrawlPageRunning, services.CrawlPageFailed,
		services.CrawlPageDuplicate, services.CrawlPageSkipped, services.CrawlPageUpdated, services.CrawlPageUnchanged,
	}
	pages := make([]services.CrawlPage, n)
	for i := range pages {
		p := crawlPage(job, fmt.Sprintf("p%03d", i), statuses[i%len(statuses)])
		if long {
			p.URL = fmt.Sprintf("https://docs.example.com/guide/section-%02d/reference/configuration/advanced-options/"+
				"very-long-page-name-for-wrapping-%03d/sub-section/details?lang=zh-CN&version=2026.11&utm_source=docs-crawler&token=abcdefghijklmnopqrstuvwxyz0123456789", i/4, i)
		}
		if p.Status == services.CrawlPageFailed {
			p.Error = fmt.Sprintf("请求第 %d 页失败：服务器返回 503，已重试 3 次仍然没有成功，请稍后在批次上点「重试失败项」再试一次", i)
		}
		pages[i] = p
	}
	return pages
}

// TestFrameBudgetDocSite 读出「采集文档站」整页的帧耗时（口径同 TestFrameBudget：无头会话的 Frame() 只记录绘制命令，
// 量界面线程的出帧成本；栅格化加回读另量一次作为上界；目标是单帧远低于 16 ms 预算，这里对 p95 设了 16 ms 的硬上限）。
//
// 四个场景：
//   - docsite_empty：表单初态（六个表单项、禁用的开始按钮、无效范围提示、没有批次）；
//   - docsite_scope：填了入口网址与目录，实时范围预览（break-all 折成多行）；多量「输入一步」（输入、退格交替，每步重算范围）；
//   - docsite_running：三个批次（运行中、暂停、完成带错误），选中运行中的批次展开 12 条页面明细；
//   - docsite_many_pages（压力）：六个批次，选中的批次展开 40 条页面明细，网址长到每条折成多行、失败页带折行的原因；
//     量两种滚动：列表内滚动（固定高度 20rem 的明细列表）与整页滚动，读数前核对滚动确实改变了画面。
//
// 耗时与机器有关，默认跳过：VIVA_FRAME_BUDGET=1 go test ./viva/screens/imports -run TestFrameBudgetDocSite -v -count=1；
// 多轮读数由调用方串行重复运行并把进程降到 BelowNormal。
func TestFrameBudgetDocSite(t *testing.T) {
	if os.Getenv("VIVA_FRAME_BUDGET") == "" {
		t.Skip("帧耗时读数与机器有关，默认不跑：VIVA_FRAME_BUDGET=1 时运行")
	}
	back := docBudgetTarget{gfx.RoleButton, locales.WebCaptureBack()}
	start := docBudgetTarget{gfx.RoleButton, locales.WebCaptureStart()}
	pageScroll := docBudgetScroll{name: "整页滚动", x: 760, y: 150}

	t.Run("docsite_empty", func(t *testing.T) {
		heap := frametime.HeapInUse()
		d := openedDocs(t, nil)
		measureDocSite(t, "docsite_empty", d, heap, docBudgetPlan{hover: []docBudgetTarget{back, start}})
	})

	t.Run("docsite_scope", func(t *testing.T) {
		heap := frametime.HeapInUse()
		d := openedDocs(t, nil)
		d.fill(locales.WebCaptureEntryUrl(), "https://docs.example.com/guide/getting-started/installation/windows/very/deep/nested/directory/layout/index.html")
		d.fill(locales.WebCaptureDirectoryScope(), "/guide/getting-started/installation/")
		if !d.hasTextLike("https://docs.example.com/guide/getting-started/installation/") {
			t.Fatalf("实际范围预览应显示 origin 加目录；画面上有 %q", d.session.Texts())
		}
		measureDocSite(t, "docsite_scope", d, heap, docBudgetPlan{hover: []docBudgetTarget{back, start}, typing: true})
	})

	t.Run("docsite_running", func(t *testing.T) {
		heap := frametime.HeapInUse()
		d := openedDocs(t, func(site *servicestest.DocSite, _ *fakeCollections) {
			done := crawlJob("j3", services.CrawlJobCompleted, map[string]int{services.CrawlPageAdded: 30, services.CrawlPageUnchanged: 4, services.CrawlPageSkipped: 1})
			done.Error = "robots.txt 暂时不可用，已按允许处理，部分页面可能被站点限制抓取"
			site.SetJobs(
				crawlJob("j1", services.CrawlJobRunning, map[string]int{services.CrawlPagePending: 12, services.CrawlPageRunning: 2, services.CrawlPageAdded: 20, services.CrawlPageFailed: 3}),
				crawlJob("j2", services.CrawlJobPaused, map[string]int{services.CrawlPageAdded: 8, services.CrawlPageDuplicate: 2}),
				done,
			)
			site.SetPages("j1", docBudgetPages("j1", 12, false)...)
		})
		d.click(gfx.RoleButton, jobTitle("j1", locales.WebCaptureCapturing()))
		if !d.hasRuns(locales.WebCaptureAdded(), " \u00b7 ", "https://a.com/p000") {
			t.Fatalf("选中批次后应列出页面明细；画面上有 %q", d.session.Texts())
		}
		measureDocSite(t, "docsite_running", d, heap, docBudgetPlan{
			hover:  []docBudgetTarget{{gfx.RoleButton, jobTitle("j1", locales.WebCaptureCapturing())}, {gfx.RoleButton, locales.WebCapturePause()}},
			scroll: []docBudgetScroll{pageScroll},
		})
	})

	t.Run("docsite_many_pages", func(t *testing.T) {
		heap := frametime.HeapInUse()
		d := openedDocs(t, func(site *servicestest.DocSite, _ *fakeCollections) {
			site.SetJobs(
				crawlJob("j1", services.CrawlJobRunning, map[string]int{services.CrawlPagePending: 10, services.CrawlPageRunning: 2, services.CrawlPageAdded: 20, services.CrawlPageFailed: 5, services.CrawlPageDuplicate: 3}),
				crawlJob("j2", services.CrawlJobPaused, map[string]int{services.CrawlPageAdded: 8}),
				crawlJob("j3", services.CrawlJobCompleted, map[string]int{services.CrawlPageAdded: 30, services.CrawlPageFailed: 1}),
				crawlJob("j4", services.CrawlJobInterrupted, map[string]int{services.CrawlPageAdded: 4, services.CrawlPageCanceled: 6}),
				crawlJob("j5", services.CrawlJobCanceled, map[string]int{services.CrawlPageCanceled: 12}),
				crawlJob("j6", services.CrawlJobPending, nil),
			)
			site.SetPages("j1", docBudgetPages("j1", 40, true)...)
		})
		d.click(gfx.RoleButton, jobTitle("j1", locales.WebCaptureCapturing()))
		first := "https://docs.example.com/guide/section-00/reference/configuration/advanced-options/very-long-page-name-for-wrapping-000/sub-section/details?lang=zh-CN&version=2026.11&utm_source=docs-crawler&token=abcdefghijklmnopqrstuvwxyz0123456789"
		if !d.hasTextLike("https://docs.example.com/guide/section-00/") || d.hasText(first) {
			t.Fatalf("40 条长网址应都在列表里且折成多行（没有任何一个 Label 装得下整条网址）；画面上有 %d 段文字", len(d.session.Texts()))
		}
		// 列表内滚动的落点：第一条页面明细的网址第一截。
		var listX, listY float32
		for _, tx := range d.session.Snapshot().Texts {
			if strings.HasPrefix(tx.Value, "https://docs.example.com/guide/section-00/") {
				listX, listY = tx.Bounds.X+tx.Bounds.Width/2, tx.Bounds.Y+tx.Bounds.Height/2
				break
			}
		}
		if listY == 0 {
			t.Fatalf("找不到第一条页面明细；画面上有 %q", d.session.Texts())
		}
		measureDocSite(t, "docsite_many_pages", d, heap, docBudgetPlan{
			hover: []docBudgetTarget{{gfx.RoleButton, jobTitle("j1", locales.WebCaptureCapturing())}, {gfx.RoleButton, locales.WebCapturePause()}},
			scroll: []docBudgetScroll{
				{name: "列表内滚动", x: listX, y: listY, required: true},
				{name: "整页滚动", x: pageScroll.x, y: pageScroll.y, required: true},
			},
		})
	})
}

// docFingerprint 是画面指纹：无障碍树（按钮等控件的位置）加每段文字的位置。页面明细行是纯展示文字、不在无障碍树里，
// 列表内滚动只移动这些文字，所以指纹要把文字的纵坐标也算进去。
func docFingerprint(d *docRig) string {
	var b strings.Builder
	b.WriteString(frametime.Fingerprint(d.session.Access()))
	for _, tx := range d.session.Snapshot().Texts {
		fmt.Fprintf(&b, "%s|%.0f;", tx.Value, tx.Bounds.Y)
	}
	return b.String()
}

// measureDocSite 在已经打开并就绪的文档站页上量：稳态 300 帧、悬停一步、（可选）输入一步、各滚动目标的一步、栅格化加回读，并给出 Go 堆增量。
// 稳态、悬停、输入与滚动的 p95 超过 16 ms 视为失败；栅格化只读数（它是真窗口绘制成本的上界）。
func measureDocSite(t *testing.T, name string, d *docRig, heapBefore uint64, plan docBudgetPlan) {
	t.Helper()
	const budget = 16 * time.Millisecond
	check := func(what string, s frametime.Stats) {
		t.Helper()
		t.Logf("%s %s：%s", name, what, s)
		if s.P95 > budget {
			t.Errorf("%s %s：p95 %.2f ms 超过 16 ms 预算", name, what, float64(s.P95)/float64(time.Millisecond))
		}
	}
	d.frames(3)
	t.Logf("%s 打开页面后 Go 堆增量（含无头会话）：%d KiB", name, (int64(frametime.HeapInUse())-int64(heapBefore))/1024)

	// 稳态帧：画面不变，连出 300 帧。
	idle, err := frametime.Measure(300, func(int) error { return d.session.Frame() })
	if err != nil {
		t.Fatalf("稳态帧失败：%v", err)
	}
	check("稳态帧（布局+记录）", idle)

	// 悬停帧：指针在两个控件之间来回，每次移动都改变悬停态。
	var points [][2]float32
	for _, target := range plan.hover {
		n, ok := d.node(target.role, target.name)
		if !ok {
			t.Fatalf("找不到悬停目标 %v「%s」", target.role, target.name)
		}
		points = append(points, [2]float32{n.Bounds.X + n.Bounds.Width/2, n.Bounds.Y + n.Bounds.Height/2})
	}
	hover, err := frametime.Measure(200, func(i int) error {
		p := points[i%len(points)]
		if err := d.session.Move(p[0], p[1]); err != nil {
			return err
		}
		return d.session.Frame()
	})
	if err != nil {
		t.Fatalf("悬停帧失败：%v", err)
	}
	check("悬停帧（移动指针+出帧）", hover)

	// 输入一步：先点进入口网址框，再输入一个字符与退格交替，每步之后实际范围重算、折行重排。
	if plan.typing {
		d.click(gfx.RoleTextField, locales.WebCaptureEntryUrl())
		typed, err := frametime.Measure(200, func(i int) error {
			if i%2 == 0 {
				if err := d.session.Type("a"); err != nil {
					return err
				}
			} else if err := d.session.Keys("backspace"); err != nil {
				return err
			}
			return d.session.Frame()
		})
		if err != nil {
			t.Fatalf("输入帧失败：%v", err)
		}
		check("输入一步（输入/退格+重算范围+出帧）", typed)
	}

	// 滚动一步：每步 40 逻辑像素。先预检：向下 20 步（800 px）画面必须变，再原路滚回。
	for _, sc := range plan.scroll {
		step := func(dy float32) error {
			if err := d.session.Scroll(gfx.ScrollEvent{X: sc.x, Y: sc.y, DeltaY: dy, Precise: true}); err != nil {
				return err
			}
			return d.session.Frame()
		}
		top := docFingerprint(d)
		for i := 0; i < 20; i++ {
			if err := step(40); err != nil {
				t.Fatalf("%s 预检滚动失败：%v", sc.name, err)
			}
		}
		moved := top != docFingerprint(d)
		for i := 0; i < 20; i++ {
			if err := step(-40); err != nil {
				t.Fatalf("%s 预检回滚失败：%v", sc.name, err)
			}
		}
		if !moved {
			if sc.required {
				t.Fatalf("%s：向下滚动 800 px 之后画面没有变化，滚动读数无效", sc.name)
			}
			t.Logf("%s %s：这个场景没有可滚的内容（画面不变），不量", name, sc.name)
			continue
		}
		if back := docFingerprint(d); back != top {
			t.Fatalf("%s：原路滚回之后画面应回到起点", sc.name)
		}
		scroll, err := frametime.Measure(200, func(i int) error {
			if (i/20)%2 == 1 {
				return step(-40)
			}
			return step(40)
		})
		if err != nil {
			t.Fatalf("%s 滚动帧失败：%v", sc.name, err)
		}
		check(sc.name+"一步（滚动+出帧）", scroll)
	}

	// 栅格化加回读：真窗口里绘制成本的上界。
	raster, err := frametime.Measure(30, func(int) error {
		_, err := d.session.Image()
		return err
	})
	if err != nil {
		t.Fatalf("栅格化失败：%v", err)
	}
	t.Logf("%s 栅格化+回读（Image，上界）：%s", name, raster)
}
