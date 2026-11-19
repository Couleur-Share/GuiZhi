package kit

import (
	"slices"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// ColumnResizer 的键盘与指针（渲染层 ColumnResizer.tsx）：← → 每次 16 px，Shift 加成 64 px，Home、End、回车、空格回到默认；
// 双击回到默认；拖动时宽度 = 起点宽度 + 指针位移（夹在上下限里），松开时才提交；按键与双击每次定下宽度都提交一次。
//
// 键盘曾经整个不起作用：快捷键挂在拿焦点的把手自己身上，而具名容器按「焦点落在容器里面」匹配，永远匹配不到
// （列表宽度、侧栏面板宽度、表头列宽的把手都是这样）；界面级测试补上之前，没有任何测试按过这些键。

const resizerName = "调整宽度"

type resizerPage struct {
	st      ColumnResizerState
	commits []float32
}

func (p *resizerPage) view(l *Look, c ui.C) {
	ui.Row(c).Fill().Pad(24).Children(func() {
		l.ColumnResizer(c.Key("resizer"), &p.st, ColumnResizerSpec{
			Name: resizerName, Min: 100, Max: 400, Default: 200,
			OnCommit: func(w float32) { p.commits = append(p.commits, w) },
		})
	})
}

func openResizer(t *testing.T) (*harness, *resizerPage) {
	t.Helper()
	p := &resizerPage{}
	return openPage(t, still(), 400, 300, p.view), p
}

func (h *harness) separator() gfx.Rect {
	h.t.Helper()
	return h.mustNode(gfx.RoleSeparator, resizerName)
}

// 键盘：Tab 走到把手上，← → 步进 16、Shift 步进 64，夹在 [100, 400]；每一下都提交。
func TestColumnResizerKeyboardSteps(t *testing.T) {
	t.Parallel()
	h, p := openResizer(t)
	if p.st.Width != 200 {
		t.Fatalf("夹具：起始宽度应是默认值 200，实际 %v", p.st.Width)
	}
	h.keys("Tab")
	if got := h.focusedName(); got != resizerName {
		t.Fatalf("Tab 应把焦点交给把手，实际在「%s」", got)
	}
	steps := []struct {
		keys string
		want float32
	}{
		{"Right", 216}, {"shift+Right", 280}, {"Left", 264}, {"shift+Left", 200},
		{"shift+Right", 264}, {"shift+Right", 328}, {"shift+Right", 392}, {"shift+Right", 400}, // 到上限夹住
		{"shift+Left", 336}, {"shift+Left", 272}, {"shift+Left", 208}, {"shift+Left", 144}, {"shift+Left", 100}, // 到下限夹住
	}
	for _, s := range steps {
		h.keys(s.keys)
		if p.st.Width != s.want {
			t.Fatalf("按 %s：宽度应是 %v，实际 %v", s.keys, s.want, p.st.Width)
		}
	}
	if len(p.commits) != len(steps) || p.commits[len(p.commits)-1] != 100 {
		t.Fatalf("每一下按键都该提交一次：提交 %v", p.commits)
	}
}

// Home、End、回车、空格都回到默认宽度并提交。
func TestColumnResizerKeysThatResetToDefault(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"Home", "End", "Enter", "Space"} {
		h, p := openResizer(t)
		h.keys("Tab")
		h.keys("shift+Right")
		if p.st.Width != 264 {
			t.Fatalf("夹具：Shift+→ 之后应是 264，实际 %v", p.st.Width)
		}
		h.keys(key)
		if p.st.Width != 200 || !slices.Equal(p.commits, []float32{264, 200}) {
			t.Errorf("%s 应回到默认 200 并提交；宽度 %v，提交 %v", key, p.st.Width, p.commits)
		}
	}
}

// 双击回到默认宽度并提交一次；单击不改宽度、也不提交。
func TestColumnResizerDoubleClickResetsToDefaultAndSingleClickDoesNothing(t *testing.T) {
	t.Parallel()
	h, p := openResizer(t)
	x, y := centerOf(h.separator())
	h.clickAt(x, y)
	if p.st.Width != 200 || len(p.commits) != 0 {
		// 渲染层只在 onResize（移动、按键、双击）里写宽度：点一下没动，不该把当前宽度写成偏好。
		t.Fatalf("单击：宽度应不变、也不提交，实际 %v，提交 %v", p.st.Width, p.commits)
	}
	h.keys("shift+Right") // 单击已把焦点交给把手
	p.commits = nil
	x, y = centerOf(h.separator())
	if err := h.session.DoubleClick(x, y); err != nil {
		t.Fatal(err)
	}
	h.frames(3)
	if p.st.Width != 200 || len(p.commits) == 0 || p.commits[0] != 200 {
		t.Errorf("双击应回到默认 200 并提交；宽度 %v，提交 %v", p.st.Width, p.commits)
	}
}

// 拖动：宽度 = 起点 + 位移，夹在上下限里，拖动中不提交，松开提交一次；可访问节点的取值跟着走。
func TestColumnResizerDragFollowsThePointerAndCommitsOnRelease(t *testing.T) {
	t.Parallel()
	h, p := openResizer(t)
	x, y := centerOf(h.separator())
	h.press(x, y)
	if err := h.session.Move(x+30, y); err != nil {
		t.Fatal(err)
	}
	h.frames(2)
	if p.st.Width != 230 || len(p.commits) != 0 {
		t.Fatalf("拖动 30 px：宽度应是 230、尚未提交；宽度 %v，提交 %v", p.st.Width, p.commits)
	}
	if n, _ := h.node(gfx.RoleSeparator, resizerName); n.Range.Now != 230 || n.Range.Min != 100 || n.Range.Max != 400 {
		t.Errorf("可访问取值应跟着宽度走：%+v", n.Range)
	}
	h.release(x+30, y)
	if p.st.Width != 230 || !slices.Equal(p.commits, []float32{230}) {
		t.Fatalf("松开应提交一次 230；宽度 %v，提交 %v", p.st.Width, p.commits)
	}
	// 拖过上限、下限夹住。
	x, y = centerOf(h.separator())
	h.press(x, y)
	if err := h.session.Move(x+1000, y); err != nil {
		t.Fatal(err)
	}
	h.frames(2)
	h.release(x+1000, y)
	if p.st.Width != 400 {
		t.Fatalf("拖过上限应夹在 400，实际 %v", p.st.Width)
	}
	x, y = centerOf(h.separator())
	h.press(x, y)
	if err := h.session.Move(x-2000, y); err != nil {
		t.Fatal(err)
	}
	h.frames(2)
	h.release(x-2000, y)
	if p.st.Width != 100 {
		t.Fatalf("拖过下限应夹在 100，实际 %v", p.st.Width)
	}
}
