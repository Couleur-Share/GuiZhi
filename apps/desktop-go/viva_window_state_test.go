package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
)

func readStateFile(t *testing.T, file string) persistedWindowState {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("读窗口状态文件失败：%v", err)
	}
	var st persistedWindowState
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	return st
}

// waitState 等防抖定时器写盘（最多 2s）。
func waitState(t *testing.T, file string, ok func(persistedWindowState) bool) persistedWindowState {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if raw, err := os.ReadFile(file); err == nil {
			var st persistedWindowState
			if json.Unmarshal(raw, &st) == nil && ok(st) {
				return st
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("等待窗口状态写盘超时")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func newTestPersister(t *testing.T, initial *windowBounds) (*vivaWindowStatePersister, string) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "sub", "window-state.json")
	p := newVivaWindowStatePersister(file, initial)
	p.debounce = 20 * time.Millisecond
	t.Cleanup(p.flush)
	return p, file
}

func TestPersisterDebouncesFrameChanges(t *testing.T) {
	p, file := newTestPersister(t, nil)
	p.debounce = 150 * time.Millisecond // 拉长一些，慢机器上循环也不会跨过防抖窗口
	// 拖动期间的连续事件：只写最后一次
	for i := 0; i < 20; i++ {
		p.onFrame(gfx.Rect{X: float32(100 + i), Y: 50, Width: 1000, Height: 700})
	}
	if _, err := os.Stat(file); err == nil {
		t.Fatal("防抖期内不该已经写盘")
	}
	st := waitState(t, file, func(s persistedWindowState) bool { return s.Bounds.X == 119 })
	want := persistedWindowState{Version: 1, Bounds: windowBounds{X: 119, Y: 50, Width: 1000, Height: 700}}
	if st != want {
		t.Fatalf("got %+v want %+v", st, want)
	}
}

func TestPersisterRoundsLogicalPixels(t *testing.T) {
	p, file := newTestPersister(t, nil)
	p.onFrame(gfx.Rect{X: 10.5, Y: -3.4, Width: 1279.6, Height: 800.4})
	p.flush()
	if st := readStateFile(t, file); st.Bounds != (windowBounds{X: 11, Y: -3, Width: 1280, Height: 800}) {
		t.Fatalf("应四舍五入（与 Math.round 一致）：%+v", st.Bounds)
	}
}

// 过小的边界（记忆校验不接受）忽略，保留上一次有效的普通边界。
func TestPersisterIgnoresTooSmallFrames(t *testing.T) {
	p, file := newTestPersister(t, nil)
	p.onFrame(gfx.Rect{X: 1, Y: 2, Width: 1000, Height: 700})
	p.onFrame(gfx.Rect{X: 5, Y: 5, Width: 300, Height: 200})
	p.flush()
	if st := readStateFile(t, file); st.Bounds != (windowBounds{X: 1, Y: 2, Width: 1000, Height: 700}) {
		t.Fatalf("got %+v", st.Bounds)
	}
}

// 最大化期间 Viva 不报边界：最大化时保留最近一次普通边界并照实写入 isMaximized；还原后 isMaximized 回到 false。
func TestPersisterMaximizeKeepsNormalBounds(t *testing.T) {
	p, file := newTestPersister(t, nil)
	p.onFrame(gfx.Rect{X: 200, Y: 100, Width: 1100, Height: 750})
	p.onMaximize(true)
	p.flush()
	st := readStateFile(t, file)
	if !st.IsMaximized || st.Bounds != (windowBounds{X: 200, Y: 100, Width: 1100, Height: 750}) {
		t.Fatalf("最大化：%+v", st)
	}
	p.onMaximize(false)
	p.flush()
	if st := readStateFile(t, file); st.IsMaximized {
		t.Fatalf("还原后不该仍是最大化：%+v", st)
	}
}

// 启动时按记忆打开、没动过窗口就直接最大化：用启动边界预填，也能落盘有效的普通边界。
func TestPersisterPrefilledByLaunchBounds(t *testing.T) {
	initial := windowBounds{X: 40, Y: 30, Width: 1300, Height: 820}
	p, file := newTestPersister(t, &initial)
	p.onMaximize(true)
	p.flush()
	st := readStateFile(t, file)
	if !st.IsMaximized || st.Bounds != initial {
		t.Fatalf("got %+v", st)
	}
}

// 没有任何有效边界时（默认窗口、还没收到过边界）不写盘：位置是系统摆放的，不能凭空写 (0,0)。
func TestPersisterWritesNothingWithoutKnownBounds(t *testing.T) {
	p, file := newTestPersister(t, nil)
	p.onMaximize(true)
	p.flush()
	if _, err := os.Stat(file); err == nil {
		t.Fatal("没有已知边界时不该写文件")
	}
}

// 全屏期间不写盘（全屏不记录），退出全屏后恢复记录。
func TestPersisterFullscreenIsNotRecorded(t *testing.T) {
	p, file := newTestPersister(t, nil)
	p.onFrame(gfx.Rect{X: 10, Y: 20, Width: 1000, Height: 700})
	p.flush()
	before := readStateFile(t, file)
	p.onFullscreen(true)
	p.onMaximize(true)
	p.flush()
	if after := readStateFile(t, file); after != before {
		t.Fatalf("全屏期间文件不该变：%+v → %+v", before, after)
	}
	p.onFullscreen(false)
	p.flush()
	if st := readStateFile(t, file); !st.IsMaximized {
		t.Fatalf("退出全屏后应恢复记录：%+v", st)
	}
}

// flush 同步落盘并取消待定防抖；可对 nil 与重复调用。
func TestPersisterFlushIsSynchronousAndSafe(t *testing.T) {
	p, file := newTestPersister(t, nil)
	p.debounce = time.Hour // 不 flush 就永远不会写
	p.onFrame(gfx.Rect{X: 7, Y: 8, Width: 900, Height: 650})
	p.flush()
	if st := readStateFile(t, file); st.Bounds.X != 7 {
		t.Fatalf("flush 应同步写盘：%+v", st)
	}
	p.flush()
	var nilP *vivaWindowStatePersister
	nilP.flush() // 不 panic
}

// 与 Electron / React 版互通：写出的文件能被现有读取逻辑原样读回，格式是 version / bounds / isMaximized。
func TestPersisterFileRoundTripsWithReadWindowLaunchState(t *testing.T) {
	p, file := newTestPersister(t, nil)
	p.onFrame(gfx.Rect{X: 100, Y: 80, Width: 1200, Height: 800})
	p.onMaximize(true)
	p.flush()

	raw, _ := os.ReadFile(file)
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	if generic["version"] != float64(1) || generic["isMaximized"] != true || generic["bounds"] == nil {
		t.Fatalf("文件格式：%s", raw)
	}
	work := []windowBounds{{X: 0, Y: 0, Width: 1920, Height: 1040}}
	got := readWindowLaunchState(file, work)
	if !got.HasPosition || !got.ShouldMaximize || got.Bounds != (windowBounds{X: 100, Y: 80, Width: 1200, Height: 800}) {
		t.Fatalf("读回 = %+v", got)
	}
}

func TestPlanVivaWindow(t *testing.T) {
	remembered := windowLaunchState{Bounds: windowBounds{X: 120, Y: 60, Width: 1400, Height: 900}, HasPosition: true}
	noPos := windowLaunchState{Bounds: windowBounds{Width: 1200, Height: 800}, ShouldMaximize: true}

	if g := planVivaWindow(remembered, false, 1280, 800); g.Width != 1400 || g.Height != 900 || g.Origin == nil || *g.Origin != (gfx.Point{X: 120, Y: 60}) {
		t.Errorf("有记忆：%+v", g)
	}
	if g := planVivaWindow(noPos, false, 1280, 800); g.Width != 1200 || g.Height != 800 || g.Origin != nil {
		t.Errorf("无记忆位置：大小取启动边界、不给 Origin：%+v", g)
	}
	// 命令行显式给了大小：按命令行（对照、截图要确定的大小），不带位置
	if g := planVivaWindow(remembered, true, 1600, 1000); g.Width != 1600 || g.Height != 1000 || g.Origin != nil {
		t.Errorf("显式大小：%+v", g)
	}
}

func TestLogicalWorkAreaScalesByMonitorDPI(t *testing.T) {
	cases := []struct {
		name        string
		l, tp, r, b int32
		dpi         uint32
		want        windowBounds
	}{
		{"100%", 0, 0, 1920, 1040, 96, windowBounds{0, 0, 1920, 1040}},
		{"150%（144 DPI）", 0, 0, 3840, 2080, 144, windowBounds{0, 0, 2560, 1387}},
		{"副显示器在左侧（负坐标）、125%", -2400, 100, 0, 1300, 120, windowBounds{-1920, 80, 1920, 960}},
		{"dpi 为 0 按 96", 10, 20, 1930, 1060, 0, windowBounds{10, 20, 1920, 1040}},
	}
	for _, c := range cases {
		if got := logicalWorkArea(c.l, c.tp, c.r, c.b, c.dpi); got != c.want {
			t.Errorf("%s：got %+v want %+v", c.name, got, c.want)
		}
	}
}

func TestPrimaryFirstOrdersPrimaryAhead(t *testing.T) {
	a := monitorWorkArea{area: windowBounds{X: -1920, Width: 1920, Height: 1040}}
	b := monitorWorkArea{area: windowBounds{X: 0, Width: 2560, Height: 1400}, primary: true}
	c := monitorWorkArea{area: windowBounds{X: 2560, Width: 1920, Height: 1040}}
	got := primaryFirst([]monitorWorkArea{a, b, c})
	want := []windowBounds{b.area, a.area, c.area}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
	if got := primaryFirst(nil); len(got) != 0 {
		t.Fatalf("nil → %v", got)
	}
}
