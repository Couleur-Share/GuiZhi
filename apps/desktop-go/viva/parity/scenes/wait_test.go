package scenes

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/parity"
	libraryscreen "guizhi-mygo-shell/viva/screens/library"
)

// 场景步骤里「等后台协程改出来的状态」必须自己出帧：场景里的 Store 没接 invalidate，协程改了状态没有人请求出帧，
// 而 Session.Rest 只在有过渡时才走时钟、不出帧。这里用一个没有任何过渡的页面钉住这一点：状态在最后一帧之后才由别的
// 协程改，只靠 Rest 的等法永远读到旧帧（`go test` 里 list-batch-busy 偶发失败的真实成因，M2-3 收口时撞上）；
// waitTextPrefix 每轮先 Frame，必须等得到。
func TestWaitTextPrefixSeesStateChangedByAGoroutineWithoutInvalidate(t *testing.T) {
	var ready atomic.Bool
	page := ui.NewPage(ui.PageConfig{View: func(c ui.C) {
		if ready.Load() {
			ui.Text(c, "ready: 3 items")
			return
		}
		ui.Text(c, "waiting")
	}})
	var canvas gfx.Canvas
	session, err := headless.Open(page.Config(app.Config{Title: t.Name(), Width: 320, Height: 120}), &canvas, headless.Options{Scale: 1})
	if err != nil {
		t.Fatalf("打开无头会话失败：%v", err)
	}
	defer session.Close()
	if err := session.Frame(); err != nil {
		t.Fatalf("出帧失败：%v", err)
	}
	go func() {
		time.Sleep(30 * time.Millisecond)
		ready.Store(true) // 没有 invalidate：不会有人替它请求出帧
	}()

	found, err := waitTextPrefix(session, "ready", 400)
	if err != nil {
		t.Fatalf("等待出错：%v", err)
	}
	if !found {
		t.Fatal("协程改了状态之后画面上一直没有出现 ready：等待的写法没有自己出帧")
	}
}

// 等不到时要如实返回 (false, nil)，而不是挂住或报成功。
func TestWaitTextPrefixReportsAbsence(t *testing.T) {
	page := ui.NewPage(ui.PageConfig{View: func(c ui.C) { ui.Text(c, "waiting") }})
	var canvas gfx.Canvas
	session, err := headless.Open(page.Config(app.Config{Title: t.Name(), Width: 320, Height: 120}), &canvas, headless.Options{Scale: 1})
	if err != nil {
		t.Fatalf("打开无头会话失败：%v", err)
	}
	defer session.Close()
	found, err := waitTextPrefix(session, "ready", 20)
	if err != nil {
		t.Fatalf("等待出错：%v", err)
	}
	if found {
		t.Fatal("画面上没有 ready，却报告等到了")
	}
}

// slowCatalog 让侧栏三份数据里的某几份读取慢一拍，模拟「后台协程晚于截图落地」。
type slowCatalog struct {
	services.Library
	counts, collections, tags time.Duration
}

func (s slowCatalog) Counts(ctx context.Context, q services.LibraryFacetQuery) (services.LibraryCounts, error) {
	time.Sleep(s.counts)
	return s.Library.Counts(ctx, q)
}

func (s slowCatalog) Collections(ctx context.Context) ([]services.LibraryCollection, error) {
	time.Sleep(s.collections)
	return s.Library.Collections(ctx)
}

func (s slowCatalog) Tags(ctx context.Context) ([]services.LibraryTag, error) {
	time.Sleep(s.tags)
	return s.Library.Tags(ctx)
}

// 读取中的场景（faultHold）里后台动作都在协程里，侧栏的计数、合集与标签落地的时刻不确定；
// waitSidebarLoaded 必须等到三份都到了才返回（负载高时没等就截图，侧栏缺内容，`go test` 偶发失败，M2-3 收口时的压测里撞上）。
// 三个子测试各让其中一份慢得多，只等另外两份的写法会在对应的子测试里失败。
func TestWaitSidebarLoadedWaitsForEachOfTheThreeReads(t *testing.T) {
	const slow = 120 * time.Millisecond
	cases := []struct {
		name string
		slow slowCatalog
	}{
		{"counts", slowCatalog{counts: slow}},
		{"collections", slowCatalog{collections: slow}},
		{"tags", slowCatalog{tags: slow}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fix, err := parity.LoadLibraryFixture()
			if err != nil {
				t.Fatal(err)
			}
			data := fix.Variant("data")
			svc := tc.slow
			svc.Library = (&servicestest.Library{}).Seed(data.Counts, data.Collections, data.Tags)
			store := libraryscreen.NewStore(svc, kit.Guard{}, nil)
			store.Spawn = func(fn func()) { go fn() }
			store.Load(context.Background())

			page := ui.NewPage(ui.PageConfig{View: func(c ui.C) { ui.Text(c, "sidebar") }})
			var canvas gfx.Canvas
			session, err := headless.Open(page.Config(app.Config{Title: t.Name(), Width: 320, Height: 120}), &canvas, headless.Options{Scale: 1})
			if err != nil {
				t.Fatalf("打开无头会话失败：%v", err)
			}
			defer session.Close()
			if err := waitSidebarLoaded(session, store); err != nil {
				t.Fatalf("等待出错：%v", err)
			}
			if v := store.View(); v.Counts == nil || len(v.Collections) == 0 || len(v.Tags) == 0 {
				t.Fatalf("返回时三份数据没有都到：计数 %v、合集 %d、标签 %d", v.Counts != nil, len(v.Collections), len(v.Tags))
			}
		})
	}
}
