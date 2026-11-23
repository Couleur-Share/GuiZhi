package themed

import (
	"fmt"
	"os"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/internal/frametime"
	"guizhi-mygo-shell/viva/locales"
)

// TestThemedFrameBudget 读出主题阅读界面的帧耗时（口径同 viva/screens/wiki 的 TestWikiFrameBudget：无头会话的 Frame() 只记录绘制命令、
// 不栅格化，量的是界面线程的出帧成本；目标是单帧远低于 16 ms 预算）。
//
// 六个代表性场景加一个压力场景：
//   - themed_setup：没有页面时打开的生成设置对话框（模型、联网、深度、图片、设计要求都在画面上）；
//   - themed_status_running：首次生成中间的居中状态卡（设计阶段、阶段步骤、实时接收进度、后台提示）；
//   - themed_status_failed：失败的状态卡，生成详情展开（失败原因、用量、各阶段耗时）；
//   - themed_tools_menu：已有页面、「阅读页设置」菜单展开（含上一版与版本说明项，共 10 项）；
//   - themed_tasks_list：处理中心的「主题页任务」，40 个任务（执行中、失败、已完成各一部分），滚动一步量的是列表滚动；
//   - themed_search_panel：设置页的「联网搜索」面板（已配置服务、密钥输入框、保存 / 测试 / 清除）；
//   - themed_references_panel（压力）：参考资料面板 60 条，滚动一步量的是对话框里的资料列表。
//
// 量四个动作：稳态帧（画面不变，连出 300 帧）、悬停一步（指针在两个有悬停反馈的控件之间来回）、滚动一步（先核对滚动确实改变了画面；
// 没有可滚内容的场景不量）、栅格化加回读（上界）；再给出打开后的 Go 堆增量（含无头会话）。每组都给出 p50、p95 与单帧最大值
// （Windows 上计时器粒度约 0.5～1 ms，小于它的帧 p50 会读成 0.00，看 mean 与 p95）。
//
// 耗时与机器有关，默认跳过：VIVA_FRAME_BUDGET=1 go test ./viva/screens/themed -run TestThemedFrameBudget -v -count=1。
func TestThemedFrameBudget(t *testing.T) {
	if os.Getenv("VIVA_FRAME_BUDGET") == "" {
		t.Skip("帧耗时读数与机器有关，默认不跑：VIVA_FRAME_BUDGET=1 时运行")
	}
	t.Run("themed_setup", func(t *testing.T) {
		measureThemed(t, "themed_setup", themedBudgetSpec{
			open: func() *uiRig {
				r := newUIRig(t)
				r.openSetup()
				return r
			},
			a: themedPointOf(gfx.RoleButton, locales.ThemedReadingGenerate()), b: themedPointOf(gfx.RoleButton, locales.CommonCancel()),
		})
	})
	t.Run("themed_status_running", func(t *testing.T) {
		measureThemed(t, "themed_status_running", themedBudgetSpec{
			open: func() *uiRig {
				r := newUIRig(t)
				planned := 3
				r.taskIn(services.ThemedTask{
					State: services.ThemedTaskRunning, Stage: services.ThemedStageDesign, Completed: 2, Total: 5, PlannedImages: &planned,
					DesignProgress: &services.ThemedDesignProgress{Attempt: 1, ReceivedChars: 4200},
				})
				r.activate()
				if !r.hasButton(locales.ThemedReadingStop()) || !r.hasButton(runningDetailsName) {
					t.Fatalf("夹具应显示执行中的状态卡；画面上有 %q", r.session.Texts())
				}
				return r
			},
			a: themedPointOf(gfx.RoleButton, locales.ThemedReadingStop()), b: themedPointOf(gfx.RoleButton, runningDetailsName),
		})
	})
	t.Run("themed_status_failed", func(t *testing.T) {
		measureThemed(t, "themed_status_failed", themedBudgetSpec{
			open: func() *uiRig {
				r := newUIRig(t)
				searches, pages, planned := 4, 9, 3
				r.taskIn(services.ThemedTask{
					State: services.ThemedTaskFailed, Stage: services.ThemedStageDesign, PlannedImages: &planned,
					Error:  "model returned 429 rate limit: 请求过于频繁，请稍后重试（上游返回的原始错误可能很长，这里放一段足够长的原因以便折行）",
					Usage:  &services.ThemedUsage{TextCalls: 3, ImageCalls: 1, ImagesSaved: 1, SearchCalls: &searches, PagesRead: &pages},
					Reused: &services.ThemedReused{Chapters: 2, Assets: 1},
					Timings: map[string]int64{
						services.ThemedStagePrepare: 800, services.ThemedStageResearch: 4200, services.ThemedStageWrite: 9100, services.ThemedStageDesign: 15300,
					},
				})
				r.activate()
				r.clickButton(locales.ThemedReadingReaderTaskDetails())
				if !r.hasButton(locales.ThemedReadingReaderCopyError()) || !r.hasButton(locales.ThemedReadingReaderResume()) {
					t.Fatalf("夹具应展开失败任务的生成详情；画面上有 %q", r.session.Texts())
				}
				return r
			},
			a: themedPointOf(gfx.RoleButton, locales.ThemedReadingReaderResume()), b: themedPointOf(gfx.RoleButton, locales.ThemedReadingReaderCopyError()),
		})
	})
	t.Run("themed_tools_menu", func(t *testing.T) {
		measureThemed(t, "themed_tools_menu", themedBudgetSpec{
			open: func() *uiRig {
				r := newUIRig(t, withIsland)
				r.putPage(func(p *services.ThemedPage) { p.Warnings = []string{"note"} })
				r.svc.PutPrevious(reqA, services.ThemedPage{ID: "page-prev"})
				r.screen.Reading.Reload()
				r.activate()
				r.click(gfx.RoleCombobox, locales.ThemedReadingReaderSettings())
				if !r.has(gfx.RoleMenuItem, locales.ThemedReadingReaderAdjust()) || !r.has(gfx.RoleMenuItem, locales.ThemedReadingReaderPageNotes()) {
					t.Fatalf("夹具应展开「阅读页设置」菜单；画面上有 %q", r.session.Texts())
				}
				return r
			},
			a: themedPointOf(gfx.RoleMenuItem, locales.ThemedReadingReaderAdjust()), b: themedPointOf(gfx.RoleMenuItem, locales.ThemedReadingExport()),
		})
	})
	t.Run("themed_tasks_list", func(t *testing.T) {
		measureThemed(t, "themed_tasks_list", themedBudgetSpec{
			open: func() *uiRig {
				r := newUIRig(t, withHost(hostTasks), func(r *uiRig) { budgetTasks(r, 40) })
				r.settle()
				if !r.textHas("Task 000") || !r.hasButton(locales.CommonRefresh()) {
					t.Fatalf("夹具应列出任务；画面上有 %q", r.session.Texts())
				}
				return r
			},
			a: themedPointOf(gfx.RoleButton, locales.CommonRefresh()), b: themedPointOf(gfx.RoleButton, locales.ThemedReadingBackToInbox()),
			scrollX: 640, scrollY: 500,
		})
	})
	t.Run("themed_search_panel", func(t *testing.T) {
		measureThemed(t, "themed_search_panel", themedBudgetSpec{
			open: func() *uiRig {
				r := searchRig(t, services.ThemedSearchStatus{
					Provider: ProviderTavily, Persistent: true, ConfiguredProviders: []string{ProviderTavily, ProviderAnySearch}, Configured: true,
				})
				if !r.hasButton(locales.SettingsTestConnection()) || !r.hasButton(locales.SettingsReadingSearchClearKey()) {
					t.Fatalf("夹具应画出联网搜索面板；画面上有 %q", r.session.Texts())
				}
				return r
			},
			a: themedPointOf(gfx.RoleButton, locales.SettingsTestConnection()), b: themedPointOf(gfx.RoleButton, locales.SettingsReadingSearchClearKey()),
		})
	})
	t.Run("themed_references_panel", func(t *testing.T) {
		measureThemed(t, "themed_references_panel", themedBudgetSpec{
			open: func() *uiRig {
				r := newUIRig(t, withIsland)
				r.putPage()
				refs := make([]services.ThemedReference, 60)
				for i := range refs {
					refs[i] = services.ThemedReference{
						ID: fmt.Sprintf("r%03d", i), Title: fmt.Sprintf("Reference %03d", i),
						URL: fmt.Sprintf("https://example.org/ref/%03d", i), CapturedAt: 1_700_000_000_000 + int64(i)*86_400_000,
					}
				}
				r.svc.SetReferences(reqA, refs...)
				r.activate()
				r.openTool(locales.ThemedReadingReaderReferences())
				r.settle()
				if !r.textHas("Reference 000") || !r.hasButton(locales.CommonClose()) {
					t.Fatalf("夹具应列出参考资料；画面上有 %q", r.session.Texts())
				}
				return r
			},
			a: themedPointOf(gfx.RoleButton, locales.CommonClose()), b: themedPointOf(gfx.RoleLink, "Reference 000"),
			scrollAt: themedPointOf(gfx.RoleLink, "Reference 000"),
		})
	})
}

// runningDetailsName 是展开样式状态卡里「查看生成详情」按钮的可访问名：文字后面带阶段计数（设计阶段是 8 个阶段里的第 5 个）。
var runningDetailsName = locales.ThemedReadingReaderTaskDetails() + " " + locales.ThemedReadingReaderStageCount(5, 8)

// budgetTasks 摆 n 个任务：每三个里执行中、失败、已完成各一个，标题 Task 000…，分属不同条目。
func budgetTasks(r *uiRig, n int) {
	states := []string{services.ThemedTaskRunning, services.ThemedTaskFailed, services.ThemedTaskCompleted}
	stages := []string{services.ThemedStageWrite, services.ThemedStageDesign, services.ThemedStageDone}
	for i := 0; i < n; i++ {
		t := services.ThemedTask{
			ItemID: fmt.Sprintf("item-%03d", i), SourceKind: services.ThemedKindBody, Title: fmt.Sprintf("Task %03d", n-1-i),
			State: states[i%3], Stage: stages[i%3], UpdatedAt: int64(1000 + i),
		}
		if t.State == services.ThemedTaskRunning {
			t.Completed, t.Total = 2, 5
		}
		if t.State == services.ThemedTaskFailed {
			t.Error = "model returned 429 rate limit"
		}
		r.svc.PutTask(t)
	}
}

// themedBudgetSpec 描述一个场景：open 造出夹具（已经稳定），a、b 是悬停来回的两个控件，scrollX/Y 非零（或 scrollAt 非空）时量滚动一步。
type themedBudgetSpec struct {
	open    func() *uiRig
	a, b    themedPointFn
	scrollX float32
	scrollY float32
	// scrollAt 非空时滚轮落在它给出的点上（代替 scrollX/Y，对话框里的滚动区用）。
	scrollAt themedPointFn
}

type themedPointFn func(r *uiRig) (x, y float32, ok bool)

// themedPointOf 取角色加名称对应控件的中心点。
func themedPointOf(role gfx.Role, name string) themedPointFn {
	return func(r *uiRig) (float32, float32, bool) {
		n, ok := r.node(role, name)
		if !ok {
			return 0, 0, false
		}
		return n.Bounds.X + n.Bounds.Width/2, n.Bounds.Y + n.Bounds.Height/2, true
	}
}

func measureThemed(t *testing.T, name string, spec themedBudgetSpec) {
	t.Helper()
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
		before := frametime.Fingerprint(r.session.Access())
		if err := r.session.Scroll(gfx.ScrollEvent{X: sx, Y: sy, DeltaY: 240, Precise: true}); err != nil {
			t.Fatal(err)
		}
		r.frames(2)
		if frametime.Fingerprint(r.session.Access()) == before {
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
	} else {
		t.Logf("%s 滚动一步：该场景没有可滚内容，不量", name)
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
