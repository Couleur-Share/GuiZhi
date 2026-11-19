package library

import (
	"slices"

	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
)

// 知识库列表的窗口级键盘（渲染层 components/library/use-item-keyboard.ts，卡片视图与列表视图共用，台账 B-045）。
//
// 渲染层在窗口上挂一个 keydown 监听（不看 defaultPrevented，也不看焦点在不在浮层里）：
//
//   - 按住 Ctrl / Alt / Meta 时不动（Shift 照常：Shift+↓ 与 ↓ 一样）；
//   - 焦点在输入框、文本域、下拉框、可编辑区、CodeMirror 里时不动（复选框是原生 input，也算）；
//   - 当前页没有条目时不动，也不拦截按键；
//   - ↓ ↑ 选下一条 / 上一条（没有选中时 ↓ 选第一条、↑ 选最后一条；到头不循环），Home / End 选第一条 / 最后一条，
//     都 preventDefault；选中项变了之后，卡片列表用 scrollToIndex 把它滚进视野（cardReveal），表格由 tableReveal 处理；
//   - 空格切换选中条目的多选勾选（preventDefault）；焦点在 button、a、[role=menuitem] 上时让给它们（它们自己的空格是点击）；
//   - Enter 打开选中条目（只在表格视图、焦点不在按钮链接上；随 M2-4 的详情浮层接）、Delete 删除多选或当前条目
//     （回收站里是彻底删除要确认；随 M2-3 的批量与删除接），这两个键现在原样放行。
//
// 浮层里的按键：渲染层的菜单与下拉用 document 监听处理方向键，但不 stopPropagation，列表的窗口监听照样收到
// （读数：排序下拉或右键菜单打开时按 ↓，选中行照样往下走，探针 E1、E2）。这里同样照做；只是浮层有自己的按键处理，
// 焦点在浮层里（层 > 0）时钩子不拦截按键（不报 Handled），让浮层自己的处理照常运行。

// ListKey 是窗口按键的前置钩子里属于知识库列表的那一环（Root.OnKey 在 kit.Look.OnKey 之后调用）。
// canvas 读此刻的键盘焦点；窗口线程、两次绘制之间调用。工作区这一帧没有挂载（切到别的模块）时不动。
func (s *Screen) ListKey(e gfx.KeyEvent, canvas *gfx.Canvas) gfx.EventResult {
	if !s.wsActive || e.Release {
		return gfx.EventResult{}
	}
	if e.Modifiers&(gfx.ModifierControl|gfx.ModifierAlt|gfx.ModifierMeta) != 0 {
		return gfx.EventResult{}
	}
	focus := kit.FocusOf(canvas)
	if focus.Editable() {
		return gfx.EventResult{}
	}
	list := s.Store.View().List
	if len(list.Entries) == 0 {
		return gfx.EventResult{}
	}
	switch e.Key {
	case gfx.KeyDown:
		s.Store.MoveSelection(MoveNext)
	case gfx.KeyUp:
		s.Store.MoveSelection(MovePrev)
	case gfx.KeyHome:
		s.Store.MoveSelection(MoveFirst)
	case gfx.KeyEnd:
		s.Store.MoveSelection(MoveLast)
	case gfx.KeySpace:
		if list.SelectedID == "" || focus.Activatable() {
			return gfx.EventResult{}
		}
		s.Store.ToggleSelection(list.SelectedID)
	default:
		return gfx.EventResult{}
	}
	return gfx.EventResult{Handled: !focus.Floating(), Repaint: true}
}

// cardReveal 是渲染层 ItemList 里那个 effect 的对应：依赖 selectedId 与 entries，任何一个换了，
// 选中行不在视野里就 scrollToIndex(index, { align: "auto" })（挂载时也跑一次）。选中行不在本页时什么也不做。
type cardReveal struct {
	seen bool
	id   string
	rev  int
}

// noteReveal 在声明卡片行之前调用：选中项或条目数组换了，就请求 window 把选中行滚进视野。
func (r *cardReveal) noteReveal(list ListView, window *kit.RowWindow) {
	if r.seen && r.id == list.SelectedID && r.rev == list.EntriesRev {
		return
	}
	r.seen, r.id, r.rev = true, list.SelectedID, list.EntriesRev
	if list.SelectedID == "" {
		return
	}
	if i := slices.IndexFunc(list.Entries, func(e services.LibraryEntry) bool { return e.ID == list.SelectedID }); i >= 0 {
		window.ScrollToIndex(i)
	}
}
