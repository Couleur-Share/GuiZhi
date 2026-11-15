package kit

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// TestPaneSpecsMatchRenderer 读渲染层 ui.store.ts 里的九个常量对拍：改一边忘另一边会失败。
// 切换（cutover）删除渲染层后，这条测试随之删除；渲染层源码不在时跳过。
func TestPaneSpecsMatchRenderer(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "desktop", "src", "renderer", "stores", "ui.store.ts"))
	if err != nil {
		t.Skipf("读不到渲染层源码（已切换或在分离的检出里）：%v", err)
	}
	num := func(name string) float32 {
		m := regexp.MustCompile(`export const ` + name + `\s*=\s*(\d+)\s*;`).FindSubmatch(src)
		if m == nil {
			t.Fatalf("ui.store.ts 里找不到常量 %s", name)
		}
		v, _ := strconv.Atoi(string(m[1]))
		return float32(v)
	}
	for prefix, spec := range map[string]PaneSpec{
		"SIDEBAR_PANEL_WIDTH":     PaneSidebar,
		"ITEM_LIST_PANE_WIDTH":    PaneItemList,
		"WIKI_CATALOG_PANE_WIDTH": PaneWikiCatalog,
	} {
		if got := num(prefix + "_DEFAULT"); got != spec.Default {
			t.Errorf("%s 默认值：渲染层 %v，原生 %v", prefix, got, spec.Default)
		}
		if got := num(prefix + "_MIN"); got != spec.Min {
			t.Errorf("%s 下限：渲染层 %v，原生 %v", prefix, got, spec.Min)
		}
		if got := num(prefix + "_MAX"); got != spec.Max {
			t.Errorf("%s 上限：渲染层 %v，原生 %v", prefix, got, spec.Max)
		}
	}
}

func TestPaneSpecClamp(t *testing.T) {
	s := PaneItemList
	cases := []struct {
		in, want float32
	}{
		{320, 320}, {240, 240}, {720, 720}, {100, 240}, {-5, 240}, {9999, 720},
		{float32(math.NaN()), 240}, {float32(math.Inf(1)), 240}, {float32(math.Inf(-1)), 240}, // 渲染层：非有限数取下限
	}
	for _, c := range cases {
		if got := s.Clamp(c.in); got != c.want {
			t.Errorf("Clamp(%v) = %v，期望 %v", c.in, got, c.want)
		}
	}
}

func TestPaneLoadsAndClampsStoredWidth(t *testing.T) {
	cases := map[string]float32{
		"":         320, // 没保存过：默认
		"400":      400,
		"400.5":    400.5,
		"10":       240, // 越界夹进范围
		"99999":    720,
		"-3":       240,
		`"abc"`:    320, // 脏数据：默认，不崩
		"null":     320,
		"[]":       320,
		"{}":       320,
		"true":     320,
		"not json": 320,
	}
	for raw, want := range cases {
		var s MemStore
		if raw != "" {
			s.Put(PaneItemList.Key, raw)
		}
		if got := NewPane(PaneItemList, &s).Width(); got != want {
			t.Errorf("保存的是 %q：宽度 %v，期望 %v", raw, got, want)
		}
	}
	if got := NewPane(PaneItemList, nil).Width(); got != 320 {
		t.Errorf("没有 store 时应取默认宽度：%v", got)
	}
}

func newFastPane(spec PaneSpec, store PrefStore) *Pane {
	p := NewPane(spec, store)
	p.save.delay = 20 * time.Millisecond
	return p
}

func storedNumber(t *testing.T, s *MemStore, key string) float64 {
	t.Helper()
	raw, ok := s.Get(key)
	if !ok {
		t.Fatalf("%s 没有保存", key)
	}
	var v float64
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("%s 保存的不是数字：%s", key, raw)
	}
	return v
}

func TestPaneSetClampsAndDebouncesSave(t *testing.T) {
	var s MemStore
	p := newFastPane(PaneItemList, &s)

	// 拖动时每帧都在改：只在静止后写一次
	for _, w := range []float32{330, 340, 350, 360, 370, 99999} {
		p.Set(w)
		time.Sleep(2 * time.Millisecond)
	}
	if p.Width() != 720 {
		t.Fatalf("宽度应夹到上限 720：%v", p.Width())
	}
	waitUntil(t, "去抖后保存", func() bool { return s.Writes() > 0 })
	time.Sleep(60 * time.Millisecond)
	if s.Writes() != 1 {
		t.Errorf("拖动过程中不该每帧写：写了 %d 次", s.Writes())
	}
	if got := storedNumber(t, &s, PaneItemList.Key); got != 720 {
		t.Errorf("保存的应是最后的值：%v", got)
	}

	// 值没变不写
	p.Set(720)
	p.Set(99999)
	time.Sleep(60 * time.Millisecond)
	if s.Writes() != 1 {
		t.Errorf("值没变不该再写：%d", s.Writes())
	}
}

func TestPaneFlushWritesImmediately(t *testing.T) {
	var s MemStore
	p := NewPane(PaneSidebar, &s) // 默认 400ms 去抖
	p.Set(500)
	if _, ok := s.Get(PaneSidebar.Key); ok {
		t.Fatal("去抖期内还不该写")
	}
	p.Flush()
	if got := storedNumber(t, &s, PaneSidebar.Key); got != 500 {
		t.Errorf("Flush 应立即保存最后一次拖动：%v", got)
	}
	// 重新打开读回来
	if got := NewPane(PaneSidebar, &s).Width(); got != 500 {
		t.Errorf("重新打开应读回 500：%v", got)
	}
}

func TestPaneReset(t *testing.T) {
	var s MemStore
	s.Put(PaneWikiCatalog.Key, "450")
	p := newFastPane(PaneWikiCatalog, &s)
	if p.Width() != 450 {
		t.Fatalf("应读到 450：%v", p.Width())
	}
	p.Reset()
	if p.Width() != 300 {
		t.Errorf("Reset 应回到默认 300：%v", p.Width())
	}
	p.Flush()
	if got := storedNumber(t, &s, PaneWikiCatalog.Key); got != 300 {
		t.Errorf("Reset 的结果应保存：%v", got)
	}
}

func TestPaneSaveFailureIsReportedNotFatal(t *testing.T) {
	s := &MemStore{FailSet: errors.New("磁盘已满")}
	p := newFastPane(PaneItemList, s)
	var failures atomic.Int32
	p.save.onError = func(error) { failures.Add(1) }
	p.Set(400)
	p.Flush()
	if failures.Load() != 1 {
		t.Errorf("保存失败应走 onError：%d", failures.Load())
	}
	if p.Width() != 400 {
		t.Error("保存失败不该影响界面上的宽度")
	}
	// 下一次变化仍会再试
	s.FailSet = nil
	p.Set(410)
	p.Flush()
	if got := storedNumber(t, s, PaneItemList.Key); got != 410 {
		t.Errorf("恢复后应能保存：%v", got)
	}
}

func TestPaneWithoutStoreKeepsWidthInMemory(t *testing.T) {
	p := NewPane(PaneItemList, nil)
	p.Set(500)
	p.Flush()
	if p.Width() != 500 {
		t.Errorf("无 store 时仍应记住宽度：%v", p.Width())
	}
}

// TestPaneSplitDragsAndResets 用真实的 ui.Split 验证：第一栏宽度就是窗格宽度，
// 拖动分隔条会落到 Set（夹紧、保存），双击恢复默认。
func TestPaneSplitDragsAndResets(t *testing.T) {
	var s MemStore
	s.Put(PaneItemList.Key, "400")
	p := newFastPane(PaneItemList, &s)
	l := lightLook()
	tr := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(l.Theme)
		ui.Column(c).Fill().Children(func() {
			p.Split(c, func() { ui.Text(c, "左栏内容") }, func() { ui.Text(c, "右栏内容") }).Grow(1)
		})
	}, 1200, 600)
	tr.Frame()

	left, ok := tr.Find("左栏内容")
	if !ok {
		t.Fatal("找不到左栏")
	}
	right, _ := tr.Find("右栏内容")
	// 左栏（含内边距）占 400，右栏内容在其右侧
	if right.X < 400 {
		t.Fatalf("右栏应在 400 之后开始：左 %+v 右 %+v", left, right)
	}

	// 拖分隔条（在 x≈400）向右 100
	tr.Press(400, 300)
	tr.Move(450, 300)
	tr.Move(500, 300)
	tr.Release(500, 300)
	tr.Frame()
	if got := p.Width(); got < 480 || got > 520 {
		t.Fatalf("拖动后宽度应约 500：%v", got)
	}
	p.Flush()
	if got := storedNumber(t, &s, PaneItemList.Key); got < 480 || got > 520 {
		t.Errorf("拖动结果应保存：%v", got)
	}

	// 拖到最右：夹进上限
	tr.Press(p.Width(), 300)
	tr.Move(1100, 300)
	tr.Release(1100, 300)
	tr.Frame()
	if got := p.Width(); got != 720 {
		t.Errorf("拖过头应夹到上限 720：%v", got)
	}
}
