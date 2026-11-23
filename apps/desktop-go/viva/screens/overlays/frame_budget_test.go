package overlays

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

// TestOverlaysFrameBudget 读出全局浮层的帧耗时（口径同 viva/screens/themed 的 TestThemedFrameBudget：无头会话的 Frame() 只记录绘制命令、
// 不栅格化，量的是界面线程的出帧成本；目标是单帧远低于 16 ms 预算）。
//
// 六个场景（五个浮层加一个命令面板压力场景）：
//   - palette_default：命令面板刚打开，静态动作分组（搜索框已聚焦）；
//   - palette_search：命令面板搜索「alpha」命中三路远程来源（条目、Wiki、导入任务）后的分组结果；
//   - palette_search_many（压力）：窗口压到 1280×420，三路来源各 5 条、共 15 条命中，面板结果区滚动；
//   - update_available（压力）：更新对话框「有新版本」，更新说明 30 条 Markdown 列表（折行后约 60 行），对话框夹到视口高度、主体整体滚动，滚动一步量的是它；
//   - setup_checklist：首次使用设置清单（文本模型主卡、折叠的可选项、快速配置与稍后再说）；
//   - migration_confirm：旧版数据迁移确认页（探测到的条目数、迁移范围、来源路径）。
//
// 量四个动作：稳态帧（画面不变，连出 300 帧）、悬停一步（指针在两个有悬停反馈的控件之间来回）、滚动一步（先核对滚动确实改变了画面；
// 没有可滚内容的场景不量）、栅格化加回读（上界）；再给出打开后的 Go 堆增量（含无头会话）。每组都给出 p50、mean、p95 与单帧最大值
// （Windows 上计时器粒度约 0.5～1 ms，小于它的帧 p50 会读成 0.00，看 mean 与 p95）。
//
// 耗时与机器有关，默认跳过：VIVA_FRAME_BUDGET=1 go test ./viva/screens/overlays -run TestOverlaysFrameBudget -v -count=1；
// 多轮读数由调用方串行重复运行并把进程降到 BelowNormal。
func TestOverlaysFrameBudget(t *testing.T) {
	if os.Getenv("VIVA_FRAME_BUDGET") == "" {
		t.Skip("帧耗时读数与机器有关，默认不跑：VIVA_FRAME_BUDGET=1 时运行")
	}
	t.Run("palette_default", func(t *testing.T) {
		measureOverlay(t, "palette_default", overlayBudgetSpec{
			open: func() *uiRig {
				r := newPaletteUI(t)
				if !r.hasRow(locales.CommandPaletteCompileWiki()) || !r.hasRow(locales.CaptureTitle()) {
					t.Fatalf("夹具应列出静态动作；画面上有 %q", r.session.Texts())
				}
				return r
			},
			a: overlayRow(locales.CommandPaletteCompileWiki()), b: overlayRow(locales.CaptureTitle()),
			scrollAt: overlayRow(locales.CommandPaletteCompileWiki()),
		})
	})
	t.Run("palette_search", func(t *testing.T) {
		measureOverlay(t, "palette_search", overlayBudgetSpec{
			open: func() *uiRig {
				r := newPaletteUI(t)
				r.seedPaletteRemote()
				r.searchPalette("alpha")
				if !r.hasRow("alpha note") || !r.hasRow("alpha wiki") || !r.hasRow("alpha task") {
					t.Fatalf("夹具应列出三路来源的命中；画面上有 %q", r.session.Texts())
				}
				return r
			},
			a: overlayRow("alpha note"), b: overlayRow("alpha wiki"),
			scrollAt: overlayRow("alpha note"),
		})
	})
	t.Run("palette_search_many", func(t *testing.T) {
		measureOverlay(t, "palette_search_many", overlayBudgetSpec{
			open: func() *uiRig {
				r := newPaletteUI(t)
				// 每路来源最多 5 条，三路共 15 条；窗口压到 420 高，结果区（60vh）放不下，列表才滚得动。
				if err := r.session.Resize(1280, 420); err != nil {
					t.Fatal(err)
				}
				entries := make([]services.LibraryEntry, 0, 5)
				tasks := make([]services.ImportTask, 0, 5)
				for i := 0; i < 5; i++ {
					id := fmt.Sprintf("%02d", i)
					entries = append(entries, services.LibraryEntry{ID: "i" + id, Title: "needle item " + id})
					r.wiki.PutPage(services.WikiPage{ID: "w" + id, Title: "needle page " + id})
					tasks = append(tasks, services.ImportTask{ID: "t" + id, DisplayName: "needle task " + id, Status: "done"})
				}
				r.lib.SeedEntries(entries)
				r.imports.Set(services.ImportPage{Entries: tasks}, nil)
				r.searchPalette("needle")
				if n := len(r.screen.Palette.View().Items); n != 15 || !r.hasRow("needle item 00") {
					t.Fatalf("夹具应列出 15 条命中（实际 %d）；画面上有 %q", n, r.session.Texts())
				}
				return r
			},
			a: overlayRow("needle item 00"), b: overlayRow("needle item 01"),
			scrollAt: overlayRow("needle item 00"),
		})
	})
	t.Run("update_available", func(t *testing.T) {
		measureOverlay(t, "update_available", overlayBudgetSpec{
			open: func() *uiRig {
				r := newUpdateUI(t)
				var notes strings.Builder
				notes.WriteString("# Highlights\n")
				for i := 0; i < 30; i++ {
					notes.WriteString("- change number " + string(rune('A'+i%26)) + " with a reasonably long description that wraps onto a second line in the dialog\n")
				}
				r.emit(services.UpdateStatus{State: services.UpdateStateAvailable, Version: "2.0.0", ReleaseDate: "2026-11-23", ReleaseNotes: notes.String()})
				// 说明很长时对话框被夹到视口高度、主体整体滚动（与渲染层 Modal 的 overflow-y-auto 一致），页脚的按钮在视口之外，
				// 所以悬停用页眉的刷新钮与关闭钮。
				if !r.hasText(locales.SettingsUpdateAvailable()) || !r.hasButton(locales.SettingsCheckUpdate()) || !r.hasButton(locales.CommonClose()) {
					t.Fatalf("夹具应显示有新版本；画面上有 %q", r.session.Texts())
				}
				return r
			},
			a: overlayPoint(gfx.RoleButton, locales.SettingsCheckUpdate()), b: overlayPoint(gfx.RoleButton, locales.CommonClose()),
			scrollAt: overlayPoint(gfx.RoleGroup, locales.SettingsCheckUpdate()),
		})
	})
	t.Run("setup_checklist", func(t *testing.T) {
		measureOverlay(t, "setup_checklist", overlayBudgetSpec{
			open: func() *uiRig {
				r := newSetupUI(t)
				if !r.hasButton(locales.SetupQuickSetup()) || !r.hasButton(locales.SetupLater()) {
					t.Fatalf("夹具应显示设置清单；画面上有 %q", r.session.Texts())
				}
				return r
			},
			a: overlayPoint(gfx.RoleButton, locales.SetupQuickSetup()), b: overlayPoint(gfx.RoleButton, locales.SetupLater()),
			scrollAt: overlayPoint(gfx.RoleGroup, locales.SetupTitle()),
		})
	})
	t.Run("migration_confirm", func(t *testing.T) {
		measureOverlay(t, "migration_confirm", overlayBudgetSpec{
			open: func() *uiRig {
				r := newMigrationUI(t)
				if !r.hasButton(locales.MigrationSkip()) || !r.hasButton(locales.MigrationStart()) {
					t.Fatalf("夹具应显示迁移确认页；画面上有 %q", r.session.Texts())
				}
				return r
			},
			a: overlayPoint(gfx.RoleButton, locales.MigrationSkip()), b: overlayPoint(gfx.RoleButton, locales.MigrationStart()),
			scrollAt: overlayPoint(gfx.RoleGroup, locales.MigrationTitle()),
		})
	})
}

// overlayBudgetSpec 描述一个场景：open 造出夹具（已经稳定），a、b 是悬停来回的两个控件，scrollAt 是滚轮的落点。
type overlayBudgetSpec struct {
	open     func() *uiRig
	a, b     overlayPointFn
	scrollAt overlayPointFn
}

type overlayPointFn func(r *uiRig) (x, y float32, ok bool)

// overlayPoint 取角色加名称对应控件的中心点。
func overlayPoint(role gfx.Role, name string) overlayPointFn {
	return func(r *uiRig) (float32, float32, bool) {
		n, ok := r.node(role, name)
		if !ok {
			return 0, 0, false
		}
		return n.Bounds.X + n.Bounds.Width/2, n.Bounds.Y + n.Bounds.Height/2, true
	}
}

// overlayRow 取命令面板里一行（按标题前缀匹配）的中心点。
func overlayRow(title string) overlayPointFn {
	return func(r *uiRig) (float32, float32, bool) {
		n, ok := r.rowNode(title)
		if !ok {
			return 0, 0, false
		}
		return n.Bounds.X + n.Bounds.Width/2, n.Bounds.Y + n.Bounds.Height/2, true
	}
}

func measureOverlay(t *testing.T, name string, spec overlayBudgetSpec) {
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

	sx, sy, ok := spec.scrollAt(r)
	if !ok {
		t.Fatalf("找不到滚动落点；画面上有 %q", r.session.Texts())
	}
	before := frametime.Fingerprint(r.session.Access())
	if err := r.session.Scroll(gfx.ScrollEvent{X: sx, Y: sy, DeltaY: 240, Precise: true}); err != nil {
		t.Fatal(err)
	}
	r.frames(2)
	if frametime.Fingerprint(r.session.Access()) == before {
		// 浮层里没有可滚内容（对话框按内容定高）：如实标注，不拿没改变画面的滚动当读数。
		t.Logf("%s 滚动一步：该场景在 1280×800 下没有可滚内容，不量", name)
	} else {
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
