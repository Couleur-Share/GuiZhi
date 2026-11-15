package kit

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// 本文件用真实的 ui.Table 验证 ColumnSet 与表格布局的配合：
// 偏好是真相源，表格里留着的布局每帧由 Sync 读回，代码改了偏好后布局以偏好为准。

// headerY 是表头文字所在的纵向中线。
const headerY = 16

// colEdge 返回某列右边缘的 x：表格从 0 开始，列按声明顺序排开，宽度取表格实际画的宽度
// （固定列受 MinWidth 约束，比如操作列默认 84 但最小 148）。
func colEdge(t *testing.T, h *tableHarness, id string) float32 {
	t.Helper()
	var x float32
	for _, c := range h.cs.Visible() {
		x += max(c.Width, c.MinWidth)
		if c.ID == id {
			return x
		}
	}
	t.Fatalf("没有显示的列 %s", id)
	return 0
}

// drag 在 y 行上从 x 按下，向右拖 dx 再松开（分几步，像真实指针）。
// 先让测试时钟走两秒：Tester 的时间不会自己走，同一位置紧挨着的两次按下会被当成双击
// （双击列边缘是「适应内容宽度」），那不是这里要测的。
func drag(tr *ui.Tester, x, y, dx float32) {
	tr.Advance(2 * time.Second)
	tr.Press(x, y)
	tr.Move(x+dx/2, y)
	tr.Move(x+dx, y)
	tr.Release(x+dx, y)
	tr.Frame()
}

func TestColumnSetWithRealTableDragResizes(t *testing.T) {
	var store MemStore
	h := &tableHarness{cs: newFastColumnSet(&store), rows: 3}
	tr := newTableTester(t, h)

	// 表头右边缘有几像素的抓取区：在边缘内侧 1px 按下，向右拖 80
	drag(tr, colEdge(t, h, "title")-1, headerY, 80)
	if got := colByID(t, h.cs.Columns(), "title").Width; got != 340 {
		t.Fatalf("拖动表头边缘后 title 应是 340：%v", got)
	}
	if got := h.state.Columns.Widths["title"]; got != 340 {
		t.Errorf("表格布局里也应是 340：%v", got)
	}
	// 后面的列被推开
	if r, _ := tr.Find("列:snippet"); r.X != 44+340+10 {
		t.Errorf("snippet 应被推到 %v：%v", 44+340+10, r.X)
	}
	h.cs.Flush()
	for _, c := range decodeColumns(t, &store, "library.tableConfig") {
		if c.ID == "title" && c.Width != 340 {
			t.Errorf("保存的宽度应与界面一致：%v", c.Width)
		}
	}
}

func TestColumnSetWithRealTableClampsDrag(t *testing.T) {
	var store MemStore
	h := &tableHarness{cs: newFastColumnSet(&store), rows: 3}
	tr := newTableTester(t, h)
	drag(tr, colEdge(t, h, "title")-1, headerY, 600)
	if got := colByID(t, h.cs.Columns(), "title").Width; got != 480 {
		t.Errorf("拖过头应夹到上限 480：%v", got)
	}
	drag(tr, colEdge(t, h, "title")-1, headerY, -600)
	if got := colByID(t, h.cs.Columns(), "title").Width; got != 150 {
		t.Errorf("拖过头应夹到下限 150：%v", got)
	}
}

func TestColumnSetWithRealTableFixedColumnsStay(t *testing.T) {
	var store MemStore
	h := &tableHarness{cs: newFastColumnSet(&store), rows: 3}
	tr := newTableTester(t, h)
	drag(tr, colEdge(t, h, "checkbox")-1, headerY, 80)
	drag(tr, colEdge(t, h, "actions")-1, headerY, 80)
	h.cs.Flush()
	if got := colByID(t, h.cs.Columns(), "checkbox").Width; got != 44 {
		t.Errorf("固定列不该被拖动改宽：%v", got)
	}
	if got := colByID(t, h.cs.Columns(), "actions").Width; got != 148 {
		t.Errorf("固定列不该被拖动改宽：%v", got)
	}
}

// 渲染层的列顺序固定为声明顺序：把表头拖到别的列上，松手后列仍在原位。
func TestColumnSetWithRealTableOrderIsFixed(t *testing.T) {
	var store MemStore
	h := &tableHarness{cs: newFastColumnSet(&store), rows: 3}
	tr := newTableTester(t, h)
	title, _ := tr.Find("列:title")
	snippet, _ := tr.Find("列:snippet")
	// 把 snippet 的表头向左拖过 title
	tr.Press(snippet.X+10, headerY)
	tr.Move(snippet.X-40, headerY)
	tr.Move(title.X+5, headerY)
	tr.Release(title.X+5, headerY)
	// 不再手动 Frame：Sync 清顺序后要求再画一帧，Tester 会自己画完
	title2, _ := tr.Find("列:title")
	snippet2, _ := tr.Find("列:snippet")
	if !(title2.X < snippet2.X) {
		t.Errorf("松手后列应回到声明顺序：title %v snippet %v", title2.X, snippet2.X)
	}
	if len(h.state.Columns.Order) != 0 {
		t.Errorf("布局里不该留着顺序：%v", h.state.Columns.Order)
	}
}

// 恢复默认必须赢过表格里留着的旧布局，且之后不能被旧布局写回去。
func TestColumnSetWithRealTableResetWins(t *testing.T) {
	var store MemStore
	h := &tableHarness{cs: newFastColumnSet(&store), rows: 3}
	tr := newTableTester(t, h)
	drag(tr, colEdge(t, h, "title")-1, headerY, 80)
	if colByID(t, h.cs.Columns(), "title").Width != 340 {
		t.Fatal("前提：先拖到 340")
	}

	h.cs.Reset()
	tr.Frame()
	tr.Frame()
	if got := colByID(t, h.cs.Columns(), "title").Width; got != 260 {
		t.Fatalf("恢复默认后不能被旧布局写回：%v", got)
	}
	if got := h.state.Columns.Widths["title"]; got != 260 {
		t.Errorf("表格布局应被重写成默认：%v", got)
	}
	if r, _ := tr.Find("列:snippet"); r.X != 44+260+10 {
		t.Errorf("界面上 snippet 应回到 %v：%v", 44+260+10, r.X)
	}
	h.cs.Flush()
	for _, c := range decodeColumns(t, &store, "library.tableConfig") {
		if c.ID == "title" && c.Width != 260 {
			t.Errorf("保存的应是默认宽度：%v", c.Width)
		}
	}

	// 之后继续拖动仍然有效
	drag(tr, colEdge(t, h, "title")-1, headerY, 40)
	if got := colByID(t, h.cs.Columns(), "title").Width; got != 300 {
		t.Errorf("恢复默认后拖动应照常生效：%v", got)
	}
}

func TestColumnSetWithRealTableProgrammaticResizeWins(t *testing.T) {
	var store MemStore
	h := &tableHarness{cs: newFastColumnSet(&store), rows: 3}
	tr := newTableTester(t, h)
	drag(tr, colEdge(t, h, "title")-1, headerY, 80) // 表格布局里留着 340
	h.cs.Resize("title", 200)
	tr.Frame()
	tr.Frame()
	if got := colByID(t, h.cs.Columns(), "title").Width; got != 200 {
		t.Errorf("代码设置的宽度不能被旧布局覆盖：%v", got)
	}
	if r, _ := tr.Find("列:snippet"); r.X != 44+200+10 {
		t.Errorf("界面应按 200 画：snippet 在 %v", r.X)
	}
}

// 双击列边缘是 MyGo 表格自带的「适应内容宽度」（渲染层没有这个功能，记在对等差异表里）：
// 适应出来的宽度同样要夹进该列的范围、记进偏好，不能把列缩成一条缝。
func TestColumnSetWithRealTableDoubleClickFitsWithinRange(t *testing.T) {
	var store MemStore
	h := &tableHarness{cs: newFastColumnSet(&store), rows: 3}
	tr := newTableTester(t, h)
	x := colEdge(t, h, "title") - 1
	tr.Advance(2 * time.Second)
	tr.ClickAt(x, headerY)
	tr.ClickAt(x, headerY)
	tr.Frame()
	tr.Frame()
	got := colByID(t, h.cs.Columns(), "title").Width
	if got < 150 || got >= 260 {
		t.Errorf("单元格内容很短：适应后应缩到下限 150 附近，不低于 150：%v", got)
	}
	if w := h.state.Columns.Widths["title"]; w != got {
		t.Errorf("表格布局与偏好应一致：布局 %v 偏好 %v", w, got)
	}
	h.cs.Flush()
	for _, c := range decodeColumns(t, &store, "library.tableConfig") {
		if c.ID == "title" && float32(c.Width) != got {
			t.Errorf("保存的应与界面一致：%v", c.Width)
		}
	}
}

func TestColumnSetWithRealTableToggle(t *testing.T) {
	h := &tableHarness{cs: newFastColumnSet(&MemStore{}), rows: 2}
	tr := newTableTester(t, h)
	if !tr.HasText("列:snippet") || tr.HasText("列:collection") {
		t.Fatal("前提：默认显示 snippet，不显示 collection")
	}
	h.cs.Toggle("snippet")
	h.cs.Toggle("collection")
	tr.Frame()
	if tr.HasText("列:snippet") || !tr.HasText("列:collection") {
		t.Error("切换后应隐藏 snippet、显示 collection")
	}
}
