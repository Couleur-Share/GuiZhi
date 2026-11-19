package kit

import "github.com/vivaui/viva/gfx"

// FocusInfo 是此刻键盘焦点所在控件的种类。窗口级键盘钩子（知识库列表的方向键 / 空格，台账 B-045）据此判断
// 渲染层 use-item-keyboard.ts 里的两个条件：焦点在可编辑控件里时按键属于那个控件；焦点在按钮、链接、菜单项上时 Enter 与空格让给它们。
type FocusInfo struct {
	Held  bool     // 有控件持有键盘焦点
	Role  gfx.Role // 它的角色；命中区找不到（例如被滚出了视野）为 0
	Layer uint16   // 它所在的层：0 是页面层，菜单、下拉与对话框的层更高
}

// FocusOf 读 canvas 上此刻持有键盘焦点的命中区（上一次绘制留下的）。窗口线程、两次绘制之间调用。
func FocusOf(canvas *gfx.Canvas) FocusInfo {
	if canvas == nil {
		return FocusInfo{}
	}
	id := canvas.Focused()
	if id.IsZero() {
		return FocusInfo{}
	}
	for _, h := range canvas.Hits() {
		if h.ID == id {
			return FocusInfo{Held: true, Role: h.Role, Layer: h.Layer}
		}
	}
	return FocusInfo{Held: true}
}

// Editable 对应渲染层的 isEditableTarget：输入框、文本域、下拉框（原生 select）、可编辑区与 CodeMirror。
// 渲染层的复选框是原生 input，也算；它的开关与自绘下拉（都是 button）不算。
func (f FocusInfo) Editable() bool {
	switch f.Role {
	case gfx.RoleTextField, gfx.RoleCheckbox, gfx.RoleSlider, gfx.RoleRadio, gfx.RoleTerminal:
		return true
	}
	return false
}

// Activatable 对应渲染层的 isActivatableTarget：button、a 与 [role=menuitem]（自己会处理 Enter 与空格）。
// 渲染层的自绘下拉触发器与选项、开关、页签都是 button。
func (f FocusInfo) Activatable() bool {
	switch f.Role {
	case gfx.RoleButton, gfx.RoleLink, gfx.RoleMenuItem, gfx.RoleTab, gfx.RoleSwitch, gfx.RoleCombobox, gfx.RoleOption:
		return true
	}
	return false
}

// Floating 报告焦点是否在浮层里（菜单、下拉、对话框）：那里的方向键、空格也要照常到达浮层自己的处理。
func (f FocusInfo) Floating() bool { return f.Layer > 0 }
