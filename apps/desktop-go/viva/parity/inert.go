package parity

import "fmt"

// 模态背后的惰性节点（台账 A-015）
//
// 渲染层的 Modal 在打开时只把焦点圈在面板里（keydown 处理 Tab），背后的页面照常留在可访问树里：
// Playwright 的 ARIA 快照仍列出背景的全部按钮与输入框。Viva 的模态用 Canvas.TrapFocus 圈住焦点与指针，
// 同时把圈外的一切从可访问树里拿掉（app/painter.go：与 Base UI 的 aria-hidden 一致）。
// 这是 Viva 的行为，去不掉；不去掉 TrapFocus 就没有焦点圈。
//
// 对照 L1 不能因此把背景整体豁免：背景应当与「没有对话框」时的页面一样，逐节点与 React 一致。做法是把
// 「对话框打开之前」的页面渲染一次（遮罩场景本来就要渲染，见 backdrop.go），把其中不在对话框打开后的树里的节点
// 按原顺序补回 Viva 一侧——背景节点的名称、顺序、位置仍与 React 逐一比对，补不回的（Viva 背景里真缺的）照常报告。

// nodeKey 是节点的身份：角色、规整后的名称，有外框时再加取整到像素的外框。
//
// 只用「角色加名称」会认错：页面里的「关闭」（窗口控制）与对话框里的「关闭」同名同角色，
// 对话框的那个会被当成页面那个「还在树里」，页面的「关闭」就补不回来；同理，对话框里的「打开条目」会顶掉背景任务行里的。
// 模态不会挪动背景（没被拿出去的背景节点外框不变），所以带上外框就能把同名的两个节点分开。
func nodeKey(n Node) string {
	key := n.Role + "\x00" + NormText(n.Name)
	if len(n.Box) == 4 {
		key += fmt.Sprintf("\x00%.0f,%.0f,%.0f,%.0f", n.Box[0], n.Box[1], n.Box[2], n.Box[3])
	}
	return key
}

// inertNodes 返回 base 里没有出现在 after 里的节点（按身份计数的多重集差），保持 base 的顺序。
// 结果里节点的 Parent 重新指向结果内的下标；父节点不在结果里的记为 -1。
func inertNodes(base, after []Node) []Node {
	left := make(map[string]int, len(after))
	for _, n := range after {
		left[nodeKey(n)]++
	}
	index := make([]int, len(base)) // base 下标到结果下标，-1 表示不在结果里
	var out []Node
	for i, n := range base {
		key := nodeKey(n)
		if left[key] > 0 {
			left[key]--
			index[i] = -1
			continue
		}
		index[i] = len(out)
		out = append(out, n)
	}
	for i := range out {
		if p := out[i].Parent; p >= 0 && p < len(index) {
			out[i].Parent = index[p]
		} else {
			out[i].Parent = -1
		}
	}
	return out
}

// withInertNodes 返回 f 的副本，节点列表是 inert 加上 f 原有的节点（页面在前、对话框在后，与渲染层的文档顺序一致）。
// f 原有节点的 Parent 随之后移。
func withInertNodes(f *Facts, inert []Node) *Facts {
	if len(inert) == 0 {
		return f
	}
	merged := *f
	merged.Nodes = make([]Node, 0, len(inert)+len(f.Nodes))
	merged.Nodes = append(merged.Nodes, inert...)
	for _, n := range f.Nodes {
		if n.Parent >= 0 {
			n.Parent += len(inert)
		}
		merged.Nodes = append(merged.Nodes, n)
	}
	return &merged
}
