package wiki

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/internal/frametime"
	"guizhi-mygo-shell/viva/locales"
)

// TestWikiFrameBudget 读出 Wiki 模块的帧耗时（口径同 viva/screens/ask 的 TestAskFrameBudget：无头会话的 Frame() 只记录绘制命令、
// 不栅格化，量的是界面线程的出帧成本；目标是单帧远低于 16 ms 预算）。
//
// 六个代表性场景加两个压力场景：
//   - wiki_empty：库里没有任何页面的空态（对照组）；
//   - wiki_typical_detail：60 页目录（各带摘要、类型、被引用数）加当前页的完整详情——12 节正文（含列表、代码块与 [[链接]]）、页内目录、
//     反向链接、3 个来源、手动编辑标记，页面菜单与历史版本齐备；
//   - wiki_edit：同一页进入正文编辑态（20 行编辑框、保存与取消）；
//   - wiki_suggestion：同一页下方展开的更新建议（说明、摘要与长正文）加旧版覆盖提示；
//   - wiki_compile_dialog：全文编译对话框里 60 条可执行资料与一个已有任务；
//   - wiki_graph：关系图谱，150 个节点、约 200 条边（代表性节点数），滚动一步量的是滚轮缩放；
//   - wiki_large_catalog（压力）：300 页目录，滚动一步量的是目录列滚动；
//   - wiki_graph_cap（压力）：图谱上限 300 个节点。
//
// 量四个动作：稳态帧（画面不变，连出 300 帧）、悬停一步（指针在两个有悬停反馈的控件之间来回）、滚动一步（先核对滚动确实改变了画面）、
// 栅格化加回读（上界）；再给出打开后的 Go 堆增量（含无头会话）。每组都给出 p50、p95 与单帧最大值
// （Windows 上计时器粒度约 0.5～1 ms，小于它的帧 p50 会读成 0.00，看 mean 与 p95）。
//
// 耗时与机器有关，默认跳过：VIVA_FRAME_BUDGET=1 go test ./viva/screens/wiki -run TestWikiFrameBudget -v -count=1。
func TestWikiFrameBudget(t *testing.T) {
	if os.Getenv("VIVA_FRAME_BUDGET") == "" {
		t.Skip("帧耗时读数与机器有关，默认不跑：VIVA_FRAME_BUDGET=1 时运行")
	}
	t.Run("wiki_empty", func(t *testing.T) {
		measureWiki(t, "wiki_empty", wikiBudgetSpec{
			open: func() *uiRig { return newUIRig(t) },
			a:    wikiPointOf(gfx.RoleButton, locales.WikiViewCatalog()), b: wikiPointOf(gfx.RoleButton, locales.WikiViewGraph()),
		})
	})
	t.Run("wiki_typical_detail", func(t *testing.T) {
		measureWiki(t, "wiki_typical_detail", wikiBudgetSpec{
			open: func() *uiRig { return openBudgetDetail(t) },
			a:    wikiPointOf(gfx.RoleButton, locales.WikiSortTitle()), b: wikiPointOf(gfx.RoleButton, locales.WikiSortLinked()),
			scrollX: 900, scrollY: 300,
		})
	})
	t.Run("wiki_edit", func(t *testing.T) {
		measureWiki(t, "wiki_edit", wikiBudgetSpec{
			open: func() *uiRig {
				r := openBudgetDetail(t)
				r.store.BeginEdit()
				r.frames(4)
				if !r.store.View().Editing || !r.hasButton(locales.CommonSave()) {
					t.Fatalf("夹具应进入编辑态：%q", r.session.Texts())
				}
				return r
			},
			a: wikiPointOf(gfx.RoleButton, locales.CommonSave()), b: wikiPointOf(gfx.RoleButton, locales.CommonCancel()),
		})
	})
	t.Run("wiki_suggestion", func(t *testing.T) {
		measureWiki(t, "wiki_suggestion", wikiBudgetSpec{
			open: func() *uiRig {
				r := newUIRig(t, budgetCatalog(60), budgetDetailPage("p000"), func(r *uiRig) {
					r.svc.PutPage(services.WikiPage{
						ID: "p000", Title: "Page 000", Summary: "summary 000", Body: budgetBody(), PromptVersion: "v0-legacy",
					})
					r.svc.PutSuggestion("p000", &services.WikiSuggestion{
						Draft: services.WikiSuggestionDraft{
							Title: "Page 000", Summary: "建议摘要：来源更新后补充了并发取舍。", Kind: services.WikiKindTopic,
							Body: strings.Repeat("建议正文的一段：补充了新来源里的取舍与限制，并保留原有结论。\n", 24),
						},
						Model: "m", Reason: budgetSuggestionReason, Token: "t1",
					})
				})
				r.clickButton("Page 000")
				r.scrollBy(900, 300, 100000) // 更新建议在正文之后：先滚到底，它才在画面里
				r.clickButton(budgetSuggestionReason)
				r.scrollBy(900, 300, 100000)
				r.scrollBy(900, 300, -100) // 停在中段：之后的滚轮来回两个方向都真的会动
				if !r.hasButton(locales.WikiSuggestionAccept()) || !r.hasButton(budgetSuggestionReason) {
					t.Fatalf("夹具应展开更新建议：%q", r.session.Texts())
				}
				return r
			},
			a: wikiPointOf(gfx.RoleButton, locales.WikiSuggestionAccept()), b: wikiPointOf(gfx.RoleButton, budgetSuggestionReason),
			scrollX: 900, scrollY: 300,
		})
	})
	t.Run("wiki_compile_dialog", func(t *testing.T) {
		measureWiki(t, "wiki_compile_dialog", wikiBudgetSpec{
			open: func() *uiRig {
				r := newUIRig(t, budgetCatalog(30), withReady(60), func(r *uiRig) {
					r.svc.PutJob(services.WikiCompileJob{ID: "job-old", Status: services.WikiJobPaused, Total: 40, Completed: 12})
				})
				r.openCompile()
				if !r.hasButton(locales.WikiCompileStart()) {
					t.Fatalf("夹具应列出编译预览：%q", r.session.Texts())
				}
				return r
			},
			a: wikiPointOf(gfx.RoleButton, locales.WikiCompileStart()), b: wikiPointOf(gfx.RoleButton, locales.CommonClose()),
			scrollAt: wikiPointOf(gfx.RoleCheckbox, entryName("i000", 2)), // 资料列表在对话框自己的滚动区里：滚轮落在第一条资料上
		})
	})
	t.Run("wiki_graph", func(t *testing.T) {
		measureWiki(t, "wiki_graph", budgetGraph(t, 150))
	})
	t.Run("wiki_large_catalog", func(t *testing.T) {
		measureWiki(t, "wiki_large_catalog", wikiBudgetSpec{
			open: func() *uiRig { return newUIRig(t, budgetCatalog(300)) },
			a:    wikiPointOf(gfx.RoleButton, locales.WikiSortTitle()), b: wikiPointOf(gfx.RoleButton, locales.WikiSortLinked()),
			scrollX: 400, scrollY: 400,
		})
	})
	t.Run("wiki_graph_cap", func(t *testing.T) {
		measureWiki(t, "wiki_graph_cap", budgetGraph(t, 300))
	})
}

// budgetSuggestionReason 是夹具里更新建议的原因（折叠面板的摘要，也是它的可访问名）。
const budgetSuggestionReason = "来源在你编辑之后又更新了"

// budgetCatalog 摆 n 页：每页有摘要，类型轮流取主题、实体、概念，每 7 页有一页手动编辑过；页与页之间连成环再加跨度链接，
// 所以目录里的被引用数大多不为零。
func budgetCatalog(n int) func(*uiRig) {
	return func(r *uiRig) {
		manual := int64(5)
		kinds := []string{services.WikiKindTopic, services.WikiKindEntity, services.WikiKindConcept}
		for i := 0; i < n; i++ {
			p := services.WikiPage{
				ID: fmt.Sprintf("p%03d", i), Title: fmt.Sprintf("Page %03d", i), Kind: kinds[i%3],
				Summary: fmt.Sprintf("第 %d 页的摘要：记录并发模式的一个侧面，写得足够长，目录里会折成两行显示。", i),
				Body:    fmt.Sprintf("body of page %d", i), PromptVersion: services.WikiCompilerVersion, UpdatedAt: int64(1000 + i),
			}
			if i%7 == 0 {
				p.ManualEditedAt = &manual
			}
			r.svc.PutPage(p)
		}
		for i := 0; i < n; i++ {
			r.svc.PutLink(fmt.Sprintf("p%03d", i), fmt.Sprintf("p%03d", (i+1)%n))
			if i%3 == 0 {
				r.svc.PutLink(fmt.Sprintf("p%03d", i), fmt.Sprintf("p%03d", (i+n/3)%n))
			}
		}
	}
}

// budgetBody 是一页典型的长正文：12 节，每节有段落、列表、代码块与指向别的页面的 [[链接]]。
func budgetBody() string {
	var b strings.Builder
	for i := 0; i < 12; i++ {
		fmt.Fprintf(&b, "## 第 %d 节：并发模式的取舍\n\n", i+1)
		b.WriteString(strings.Repeat("并发模式的要点在于把共享状态收窄到最小的临界区，再用消息传递替代锁的层层嵌套。", 3))
		fmt.Fprintf(&b, "相关页面见 [[Page %03d]] 与 [[Page %03d|别名 %d]]。\n\n", i+1, i+2, i)
		b.WriteString("- 第一点：先测量再优化\n- 第二点：优先使用通道\n- 第三点：显式传递取消信号\n\n")
		if i%3 == 0 {
			b.WriteString("### 小结\n\n```go\nfunc worker(ctx context.Context, in <-chan int) {\n\tfor v := range in {\n\t\t_ = v\n\t}\n}\n```\n\n")
		}
	}
	return b.String()
}

// budgetDetailPage 把 id 页换成完整详情的夹具：长正文、手动编辑标记、3 个来源、两份历史版本，且被别的页引用。
func budgetDetailPage(id string) func(*uiRig) {
	return func(r *uiRig) {
		manual := int64(5)
		r.svc.PutPage(services.WikiPage{
			ID: id, Title: "Page 000", Kind: services.WikiKindTopic, Summary: "summary 000", Body: budgetBody(),
			ManualEditedAt: &manual, PromptVersion: services.WikiCompilerVersion, UpdatedAt: 5000,
		})
		r.svc.PutSources(id,
			services.WikiPageSource{ItemID: "i1", Title: "来源一"},
			services.WikiPageSource{ItemID: "i2", Title: "来源二"},
			services.WikiPageSource{ItemID: "i3", Title: "来源三"})
		r.svc.PutRevision(services.WikiRevision{PageID: id, Body: "old body"})
		r.svc.PutRevision(services.WikiRevision{PageID: id, Body: "older body"})
	}
}

// openBudgetDetail 打开典型详情：60 页目录，选中第 0 页。
func openBudgetDetail(t *testing.T) *uiRig {
	t.Helper()
	r := newUIRig(t, budgetCatalog(60), budgetDetailPage("p000"))
	r.clickButton("Page 000")
	if v := r.store.View(); v.Detail == nil || v.SelectedPageID != "p000" {
		t.Fatalf("夹具应选中第 0 页：%+v", v)
	}
	if !r.has(gfx.RoleHeading, "Page 000") {
		t.Fatalf("详情应显示标题：%q", r.session.Texts())
	}
	return r
}

// budgetGraph 是图谱场景：n 个节点，环加跨度链接；滚动一步是滚轮缩放（以画布中心为指针位置），
// 图谱的缩放不进无障碍树，所以用缩放与平移当画面指纹。
func budgetGraph(t *testing.T, n int) wikiBudgetSpec {
	return wikiBudgetSpec{
		open: func() *uiRig {
			r := newUIRig(t, budgetCatalog(n))
			m := r.openGraph()
			if len(m.Nodes) != n {
				t.Fatalf("图谱应有 %d 个节点：%d", n, len(m.Nodes))
			}
			return r
		},
		a: wikiPointOf(gfx.RoleButton, locales.WikiViewCatalog()), b: wikiPointOf(gfx.RoleButton, locales.WikiViewGraph()),
		scrollX: 760, scrollY: 430, checkDY: -240, // 向上滚是放大
		print: func(r *uiRig) string {
			g := &r.screen.graph
			return fmt.Sprintf("%.4f|%.2f|%.2f", g.zoom, g.panX, g.panY)
		},
	}
}

// wikiBudgetSpec 描述一个场景：open 造出夹具（已经稳定），a、b 是悬停来回的两个控件，scrollX/Y 非零时量滚动一步；
// print 非空时用它代替无障碍树指纹判断滚动是否改变了画面（图谱缩放用）。
type wikiBudgetSpec struct {
	open    func() *uiRig
	a, b    wikiPointFn
	scrollX float32
	scrollY float32
	// scrollAt 非空时滚轮落在它给出的点上（代替 scrollX/Y，对话框里的滚动区用）。
	scrollAt wikiPointFn
	// checkDY 是核对滚动有效的那一下滚轮的量；0 取向下滚 240（详情与目录都停在顶部，向上滚不会动）。
	checkDY float32
	print   func(r *uiRig) string
}

type wikiPointFn func(r *uiRig) (x, y float32, ok bool)

// wikiPointOf 取角色加名称对应控件的中心点。
func wikiPointOf(role gfx.Role, name string) wikiPointFn {
	return func(r *uiRig) (float32, float32, bool) {
		n, ok := r.node(role, name)
		if !ok {
			return 0, 0, false
		}
		return n.Bounds.X + n.Bounds.Width/2, n.Bounds.Y + n.Bounds.Height/2, true
	}
}

func measureWiki(t *testing.T, name string, spec wikiBudgetSpec) {
	heapBefore := frametime.HeapInUse()
	r := spec.open()
	r.frames(4)
	t.Logf("%s 打开后 Go 堆增量（含无头会话）：%d KiB", name, (int64(frametime.HeapInUse())-int64(heapBefore))/1024)

	idle, err := frametime.Measure(300, func(int) error { return r.session.Frame() })
	if err != nil {
		t.Fatalf("稳态帧失败：%v", err)
	}
	t.Logf("%s 稳态帧（布局+记录）：%s", name, idle)

	ax, ay, okA := spec.a(r)
	bx, by, okB := spec.b(r)
	if !okA || !okB {
		t.Fatalf("找不到悬停目标（a=%v b=%v）；画面上有 %q", okA, okB, r.session.Texts())
	}
	if ax == bx && ay == by {
		t.Fatal("两个悬停点重合，悬停读数无效")
	}
	hover, err := frametime.Measure(200, func(i int) error {
		x, y := ax, ay
		if i%2 == 1 {
			x, y = bx, by
		}
		if err := r.session.Move(x, y); err != nil {
			return err
		}
		return r.session.Frame()
	})
	if err != nil {
		t.Fatalf("悬停帧失败：%v", err)
	}
	t.Logf("%s 悬停一步（移动指针+出帧）：%s", name, hover)

	sx, sy := spec.scrollX, spec.scrollY
	if spec.scrollAt != nil {
		var ok bool
		if sx, sy, ok = spec.scrollAt(r); !ok {
			t.Fatalf("找不到滚动落点；画面上有 %q", r.session.Texts())
		}
	}
	if sx != 0 || sy != 0 {
		fingerprint := func() string { return frametime.Fingerprint(r.session.Access()) }
		if spec.print != nil {
			fingerprint = func() string { return spec.print(r) }
		}
		before := fingerprint()
		checkDY := spec.checkDY
		if checkDY == 0 {
			checkDY = 240
		}
		if err := r.session.Scroll(gfx.ScrollEvent{X: sx, Y: sy, DeltaY: checkDY, Precise: true}); err != nil {
			t.Fatal(err)
		}
		r.frames(2)
		if fingerprint() == before {
			t.Fatal("滚动没有改变画面，滚动读数无效")
		}
		scroll, err := frametime.Measure(120, func(i int) error {
			dy := float32(120)
			if i%2 == 1 {
				dy = -120
			}
			if err := r.session.Scroll(gfx.ScrollEvent{X: sx, Y: sy, DeltaY: dy, Precise: true}); err != nil {
				return err
			}
			return r.session.Frame()
		})
		if err != nil {
			t.Fatalf("滚动帧失败：%v", err)
		}
		t.Logf("%s 滚动一步（滚轮+出帧）：%s", name, scroll)
	}

	raster, err := frametime.Measure(30, func(int) error {
		_, err := r.session.Image()
		return err
	})
	if err != nil {
		t.Fatalf("栅格化失败：%v", err)
	}
	t.Logf("%s 栅格化+回读（Image，上界）：%s", name, raster)
}
