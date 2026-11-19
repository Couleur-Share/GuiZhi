package library

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/viva/locales"
)

// 侧栏的合集与标签行是窗口化的（台账 B-035，kit.FlexColumn.Run）：只声明看得见的几行加上下余量。
// 这里验证窗口化之后的行为与整表声明时一致：滚到哪里都点得到、右键与选中状态正确、键盘 Tab 能一路走过窗口边界；
// 窗口与整表的逐像素对拍在 kit（flexrun_test.go）。

// paintedPrefix 是画面（含窗口外已声明、被裁掉的几行）里以 prefix 开头的文字个数：声明了多少行，就有多少。
func (r *rig) paintedPrefix(prefix string) int {
	n := 0
	for _, text := range r.session.Texts() {
		if strings.HasPrefix(text, prefix) {
			n++
		}
	}
	return n
}

// wheelTo 把侧栏滚动区滚一下（正数向下），再出几帧让窗口与几何收敛。
func (r *rig) wheelTo(dy float32) {
	r.t.Helper()
	if err := r.session.Scroll(gfx.ScrollEvent{X: 120, Y: 400, DeltaY: dy, Precise: true}); err != nil {
		r.t.Fatalf("滚动失败：%v", err)
	}
	r.frames(4)
}

func collectionName(i int) string { return fmt.Sprintf("合集 %03d：读书笔记与项目资料", i) }
func tagName(i int) string        { return fmt.Sprintf("标签 %03d", i) }

// 1000 个合集 + 3000 个标签：一帧里画出来的行数只与视口有关；滚到末尾、再滚回开头，行都点得到，选中的是点的那一行。
func TestSidebarPaintsOnlyTheWindowOfHugeLists(t *testing.T) {
	r := newRig(t, func(r *rig) { r.svc.Seed(bigLibrary(1000, 3000)) })
	const bound = 40 // 一屏约 22 行（800 高）+ 上下各 3 行余量 + 视口外那一段留的几行
	if n := r.paintedPrefix("合集 "); n < 10 || n > bound {
		t.Fatalf("顶部：画了 %d 个合集行，应在 [10, %d] 之内", n, bound)
	}
	if n := r.paintedPrefix("标签 "); n > 8 {
		t.Fatalf("顶部：视口之外的标签段画了 %d 行，只该留一行加余量", n)
	}

	r.wheelTo(1e7) // 滚到最底：最后一个标签与回收站
	if n := r.paintedPrefix("标签 "); n < 10 || n > bound {
		t.Fatalf("底部：画了 %d 个标签行，应在 [10, %d] 之内", n, bound)
	}
	if n := r.paintedPrefix("合集 "); n > 8 {
		t.Fatalf("底部：视口之上的合集段画了 %d 行，只该留一行加余量", n)
	}
	for _, name := range []string{tagName(2999), locales.LibraryScopeTrash()} {
		if !r.has(gfx.RoleButton, name) {
			t.Fatalf("滚到底之后看不见「%s」", name)
		}
	}
	r.click(gfx.RoleButton, tagName(2999))
	if got := r.view().Filters.TagID; got != "t2999" {
		t.Fatalf("点最后一个标签：筛选的标签是 %q，期望 t2999", got)
	}
	if !r.mustNode(gfx.RoleButton, tagName(2999)).Selected {
		t.Error("被点的标签行应是选中态")
	}

	r.wheelTo(-1e7) // 回到顶：第一个合集
	r.click(gfx.RoleButton, collectionName(0))
	if got := r.view().Filters.CollectionID; got != "c000" {
		t.Fatalf("点第一个合集：筛选的合集是 %q，期望 c000", got)
	}
}

// 滚到合集的中段：该处的行是第一次声明出来的，右键打开的是它自己的菜单，点「更多」也一样。
func TestSidebarRowsInTheMiddleOfAHugeListOpenTheirOwnMenus(t *testing.T) {
	r := newRig(t, func(r *rig) { r.svc.Seed(bigLibrary(1000, 20)) })
	r.wheelTo(16000)
	var middle string
	for _, n := range r.session.Access() {
		if n.Role == gfx.RoleButton && strings.HasPrefix(n.Label, "合集 ") && n.Bounds.Y > 300 {
			middle = n.Label
			break
		}
	}
	if middle == "" {
		t.Fatal("滚到中段之后看不见任何合集行")
	}
	var idx int
	if _, err := fmt.Sscanf(middle, "合集 %d", &idx); err != nil || idx < 300 || idx > 700 {
		t.Fatalf("滚 16000 px 之后的合集行是 %q（序号 %d，应在 300–700 之间）", middle, idx)
	}
	r.rightClick(gfx.RoleButton, middle)
	if !r.has(gfx.RoleMenuItem, locales.LibraryRename()) || !r.has(gfx.RoleMenuItem, deleteCollectionLabel()) {
		t.Fatal("右键中段的合集行应打开合集菜单")
	}
	r.keys("Escape")
	if r.has(gfx.RoleMenu, "") {
		t.Fatal("按 Esc 之后行菜单应关闭")
	}
	r.click(gfx.RoleButton, middle)
	if got := r.view().Filters.CollectionID; got != fmt.Sprintf("c%03d", idx) {
		t.Fatalf("点中段的合集：筛选的合集是 %q，期望 c%03d", got, idx)
	}
}

// 选中的行被滚出窗口、再滚回来：它回来时就是选中态（窗口外的行没有声明，状态在 Store 里，不在行里）。
func TestSidebarSelectedRowIsStillSelectedAfterLeavingTheWindow(t *testing.T) {
	r := newRig(t, func(r *rig) { r.svc.Seed(bigLibrary(300, 20)) })
	r.click(gfx.RoleButton, collectionName(0))
	if !r.mustNode(gfx.RoleButton, collectionName(0)).Selected {
		t.Fatal("点了之后第一个合集应是选中态")
	}
	r.wheelTo(6000) // 远远滚出窗口
	if r.has(gfx.RoleButton, collectionName(0)) {
		t.Fatal("前提不成立：滚 6000 px 之后第一个合集应已不在画面里")
	}
	r.wheelTo(-1e7)
	n := r.mustNode(gfx.RoleButton, collectionName(0))
	if !n.Selected {
		t.Error("滚回来之后第一个合集应仍是选中态")
	}
	if got := r.view().Filters.CollectionID; got != "c000" {
		t.Errorf("筛选的合集 = %q，期望 c000", got)
	}
}

// 键盘 Tab 一路走过窗口边界：每个合集行的主按钮都轮得到、聚焦时在视野里；再按 Shift+Tab 能原路回到第一行。
// 窗口只多声明上下各几行，下一个停靠点总在已声明的行里；kit 把它滚进来之后，窗口随之前移。
func TestSidebarTabWalksThroughWindowEdgesAndBack(t *testing.T) {
	r := newRig(t, func(r *rig) { r.svc.Seed(bigLibrary(150, 10)) })
	first := collectionName(0)
	r.click(gfx.RoleButton, first)
	visited := map[int]bool{0: true} // 点击已经把焦点给了第一行
	furthest := 0
	for i := 0; i < 400 && furthest < 70; i++ {
		r.keys("Tab")
		label, inView, ok := r.focusedLabel()
		if !ok {
			continue
		}
		if !inView {
			t.Fatalf("第 %d 次 Tab：聚焦的「%s」不在视野里，滚动区没有跟着滚", i+1, label)
		}
		var idx int
		if _, err := fmt.Sscanf(label, "合集 %d", &idx); err == nil {
			visited[idx] = true
			furthest = max(furthest, idx)
		}
	}
	if furthest < 70 {
		t.Fatalf("按 Tab 只走到第 %d 个合集（应能越过好几个窗口走到 70 以后）", furthest)
	}
	for idx := 0; idx <= furthest; idx++ {
		if !visited[idx] {
			t.Errorf("按 Tab 一路走下去没有轮到第 %d 个合集（窗口边界处的行被跳过了）", idx)
		}
	}
	for i := 0; i < 400; i++ {
		r.keys("shift+Tab")
		label, inView, ok := r.focusedLabel()
		if !ok {
			continue
		}
		if !inView {
			t.Fatalf("第 %d 次 Shift+Tab：聚焦的「%s」不在视野里", i+1, label)
		}
		if label == first {
			return
		}
	}
	t.Fatal("一路按 Shift+Tab 没有回到第一个合集")
}
