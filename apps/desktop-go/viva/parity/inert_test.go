package parity

import (
	"slices"
	"testing"
)

func node(role, name string, parent int) Node { return Node{Role: role, Name: name, Parent: parent} }

func nodeNames(nodes []Node) []string {
	out := make([]string, len(nodes))
	for i, n := range nodes {
		out[i] = n.Role + ":" + n.Name
	}
	return out
}

func TestInertNodesAreTheOnesTheModalTookOut(t *testing.T) {
	base := []Node{
		node("group", "页面", -1),
		node("button", "新建", 0),
		node("button", "任务详情", 0),
		node("button", "任务详情", 0),
	}
	// 对话框打开之后的树里只剩一个「任务详情」（重名的按数量计）：另一个、新建与页面本身是被模态拿出去的。
	after := []Node{node("group", "对话框", -1), node("button", "任务详情", 0)}
	got := inertNodes(base, after)
	want := []string{"group:页面", "button:新建", "button:任务详情"}
	if !slices.Equal(nodeNames(got), want) {
		t.Fatalf("惰性节点 = %q，期望 %q", nodeNames(got), want)
	}
	for i, wantParent := range []int{-1, 0, 0} {
		if got[i].Parent != wantParent {
			t.Errorf("惰性节点 %d 的 Parent = %d，期望 %d", i, got[i].Parent, wantParent)
		}
	}
}

func TestInertNodesDropsParentsThatAreNotInertThemselves(t *testing.T) {
	base := []Node{node("group", "页面", -1), node("button", "新建", 0)}
	after := []Node{node("group", "页面", -1)} // 页面还在，只有按钮被拿出去
	got := inertNodes(base, after)
	if len(got) != 1 || got[0].Name != "新建" || got[0].Parent != -1 {
		t.Fatalf("父节点不在结果里时 Parent 应记为 -1：%+v", got)
	}
}

func boxed(role, name string, parent int, x, y, w, h float64) Node {
	n := node(role, name, parent)
	n.Box = []float64{x, y, w, h}
	return n
}

func TestInertNodesTellSameNamedNodesApartByBox(t *testing.T) {
	// 页面里的「关闭」（窗口控制）与对话框里的「关闭」同名同角色：对话框打开后树里只有对话框那个，
	// 页面那个外框不同，仍是被拿出去的惰性节点；背景任务行里的「打开条目」同理。
	base := []Node{
		boxed("button", "关闭", -1, 1240, 4, 36, 32),
		boxed("button", "打开条目", -1, 100, 300, 48, 16),
		boxed("button", "打开条目", -1, 100, 340, 48, 16),
	}
	after := []Node{
		boxed("button", "关闭", -1, 900, 80, 32, 32),
		boxed("button", "打开条目", -1, 300, 440, 48, 16),
	}
	got := inertNodes(base, after)
	if len(got) != 3 {
		t.Fatalf("外框不同的同名节点各算各的，应有 3 个惰性节点：%q", nodeNames(got))
	}
	// 外框一样（ConfirmDialog 不动背景）的同名节点则抵消。
	same := inertNodes(base, append([]Node{}, base[0], base[1]))
	if len(same) != 1 || same[0].Box[1] != 340 {
		t.Fatalf("外框相同的节点应抵消，只剩第三个：%+v", same)
	}
	// 外框取整到像素：亚像素差不算两个节点。
	moved := boxed("button", "关闭", -1, 1240.2, 4.3, 36.1, 31.8)
	if got := inertNodes(base[:1], []Node{moved}); len(got) != 0 {
		t.Errorf("亚像素差不应让节点被当成另一个：%+v", got)
	}
}

func TestNoInertNodesWhenNothingWasTakenOut(t *testing.T) {
	// ConfirmDialog 不圈焦点，背景仍在树里：补回的节点为空，Viva 一侧原样比对。
	base := []Node{node("group", "页面", -1), node("button", "新建", 0)}
	after := append(slices.Clone(base), node("group", "确认", -1))
	if got := inertNodes(base, after); len(got) != 0 {
		t.Fatalf("背景仍在树里，不应有惰性节点：%q", nodeNames(got))
	}
	f := &Facts{Scene: "s", Nodes: after}
	if withInertNodes(f, nil, false) != f {
		t.Fatal("没有惰性节点时应原样返回")
	}
}

func TestWithInertNodesPutsThePageBeforeTheDialog(t *testing.T) {
	inert := []Node{node("group", "页面", -1), node("button", "新建", 0)}
	f := &Facts{Scene: "s", Nodes: []Node{node("group", "对话框", -1), node("button", "关闭", 0)}}
	got := withInertNodes(f, inert, false)
	want := []string{"group:页面", "button:新建", "group:对话框", "button:关闭"}
	if !slices.Equal(nodeNames(got.Nodes), want) {
		t.Fatalf("合并后的节点 = %q，期望 %q", nodeNames(got.Nodes), want)
	}
	if got.Nodes[2].Parent != -1 || got.Nodes[3].Parent != 2 {
		t.Errorf("对话框节点的 Parent 应随之后移：%d、%d", got.Nodes[2].Parent, got.Nodes[3].Parent)
	}
	if len(f.Nodes) != 2 || f.Nodes[1].Parent != 0 {
		t.Error("不得改动传入的事实")
	}
}

func overlayText(text string, overlay bool) Text { return Text{Text: text, Overlay: overlay} }

func textLines(texts []Text) []string {
	out := make([]string, len(texts))
	for i, t := range texts {
		out[i] = t.Text
	}
	return out
}

// dialogFirst：浮层在前、页面在后——节点按「对话框、惰性页面」排，惰性节点的 Parent 后移而对话框节点的 Parent 不动；
// 文字则是浮层里的先、页面的后，各自保持原有相对次序（命令面板在 React 的 App 树最前面）。
func TestWithInertNodesDialogFirstPutsTheDialogBeforeThePage(t *testing.T) {
	inert := []Node{node("group", "页面", -1), node("button", "新建", 0)}
	f := &Facts{
		Scene: "s",
		Nodes: []Node{node("group", "对话框", -1), node("button", "关闭", 0)},
		Texts: []Text{overlayText("页面一", false), overlayText("浮层一", true), overlayText("页面二", false), overlayText("浮层二", true)},
	}
	got := withInertNodes(f, inert, true)
	wantNodes := []string{"group:对话框", "button:关闭", "group:页面", "button:新建"}
	if !slices.Equal(nodeNames(got.Nodes), wantNodes) {
		t.Fatalf("对话框在前的节点 = %q，期望 %q", nodeNames(got.Nodes), wantNodes)
	}
	for i, wantParent := range []int{-1, 0, -1, 2} {
		if got.Nodes[i].Parent != wantParent {
			t.Errorf("节点 %d 的 Parent = %d，期望 %d（对话框节点不动，惰性节点随页面后移 %d 位）", i, got.Nodes[i].Parent, wantParent, len(f.Nodes))
		}
	}
	wantTexts := []string{"浮层一", "浮层二", "页面一", "页面二"}
	if !slices.Equal(textLines(got.Texts), wantTexts) {
		t.Errorf("文字应浮层在前、页面在后且各自保持次序：%q，期望 %q", textLines(got.Texts), wantTexts)
	}
	if len(got.Texts) != len(f.Texts) {
		t.Errorf("只调整顺序、不增删：%d 项变成 %d 项", len(f.Texts), len(got.Texts))
	}
	if !slices.Equal(textLines(f.Texts), []string{"页面一", "浮层一", "页面二", "浮层二"}) || len(f.Nodes) != 2 {
		t.Error("不得改动传入的事实")
	}
}

// dialogFirst 没有惰性节点时（命令面板打开前页面本来就在树里的情形）仍要重排文字，不能走「原样返回」的捷径。
func TestWithInertNodesDialogFirstReordersTextsEvenWithoutInertNodes(t *testing.T) {
	f := &Facts{
		Scene: "s", Nodes: []Node{node("group", "对话框", -1)},
		Texts: []Text{overlayText("页面", false), overlayText("浮层", true)},
	}
	got := withInertNodes(f, nil, true)
	if got == f {
		t.Fatal("dialogFirst 即使没有惰性节点也应返回重排后的副本")
	}
	if !slices.Equal(textLines(got.Texts), []string{"浮层", "页面"}) {
		t.Errorf("文字应浮层在前：%q", textLines(got.Texts))
	}
	if !slices.Equal(nodeNames(got.Nodes), []string{"group:对话框"}) || got.Nodes[0].Parent != -1 {
		t.Errorf("节点不应变化：%q", nodeNames(got.Nodes))
	}
}
