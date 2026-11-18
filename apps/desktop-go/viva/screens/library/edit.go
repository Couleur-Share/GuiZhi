package library

import (
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// editKind 是新建与重命名弹窗的三种用法（渲染层 EditModalState）。
type editKind uint8

const (
	editNone editKind = iota
	editCreateCollection
	editRenameCollection
	editRenameTag
)

// editDialog 是新建合集、重命名合集与重命名标签共用的弹窗（渲染层 SidebarLibraryPanel 的 editModal、editName、editIcon）。
type editDialog struct {
	modal      kit.ModalState
	open       bool
	kind       editKind
	collection services.LibraryCollection // 重命名合集时的目标
	tag        services.LibraryTag        // 重命名标签时的目标
	name       ui.Field
	icon       string // 选中的图标，空串是默认的文件夹
	custom     string // 打开时就在的目录外图标（旧库迁移来的）：单独摆一格，点了别处之后还选得回来
	busy       bool   // 提交请求在途：再次提交被忽略（台账 C-005）
	picker     iconPickerState
}

// editOutcome 是后台提交的结果，由界面线程在帧首取走。
type editOutcome struct{ err error }

// openEdit 打开弹窗并带入初值（渲染层 openEditModal）：名称与图标每次打开都重置，图标选择器随弹窗重新挂载。
func (s *Screen) openEdit(kind editKind, col services.LibraryCollection, tag services.LibraryTag) {
	e := &s.edit
	e.kind, e.collection, e.tag, e.open, e.busy = kind, col, tag, true, false
	name, icon := "", ""
	switch kind {
	case editRenameCollection:
		name, icon = col.Name, col.Icon
	case editRenameTag:
		name = tag.Name
	}
	e.name.SetText(name)
	e.icon = icon
	e.custom = ""
	if icon != "" && !isCatalogIcon(icon) {
		e.custom = icon
	}
	e.picker = iconPickerState{}
}

// submitEdit 提交（渲染层 submitEditModal）：名称为空什么都不做；请求在后台跑，成功关弹窗，失败弹出原因并保持打开。
func (s *Screen) submitEdit() {
	e := &s.edit
	name := jsTrim(e.name.Text())
	if !e.open || name == "" || e.busy {
		return
	}
	icon := jsTrim(e.icon)
	kind, col, tag := e.kind, e.collection, e.tag
	e.busy = true
	s.Store.Spawn(func() {
		var err error
		switch kind {
		case editCreateCollection:
			var created services.LibraryCollection
			if created, err = s.Store.CreateCollection(s.Ctx, name, icon); err == nil {
				s.Store.SelectCollection(s.Ctx, created.ID)
			}
		case editRenameCollection:
			err = s.Store.UpdateCollection(s.Ctx, col.ID, name, icon)
		case editRenameTag:
			err = s.Store.RenameTag(s.Ctx, tag.ID, name)
		}
		s.mu.Lock()
		s.finished = &editOutcome{err: err}
		s.mu.Unlock()
		s.Store.wake()
	})
}

// settleEdit 在帧首处理后台提交的结果：成功关弹窗；失败弹出带原因的错误提示，弹窗保持打开（渲染层 showToast(error.message, "error")）。
func (s *Screen) settleEdit() {
	s.mu.Lock()
	out := s.finished
	s.finished = nil
	s.mu.Unlock()
	if out == nil {
		return
	}
	s.edit.busy = false
	if out.err == nil {
		s.edit.open = false
		return
	}
	if s.Guard.Notify != nil {
		s.Guard.Notify.Error(out.err.Error(), "")
	}
}

// editDialogView 声明弹窗：标题是「新建合集」或「重命名」；带图标选择器的用默认宽度，只改名字的标签用窄的（sm），
// 否则一个输入框独占 500px 宽。
func (s *Screen) editDialogView(c ui.C, _ View) {
	l := s.Look
	e := &s.edit
	title := locales.LibraryRename()
	if e.kind == editCreateCollection {
		title = locales.LibraryNewCollection()
	}
	size := kit.ModalMD
	if e.kind == editRenameTag {
		size = kit.ModalSM
	}
	closed := l.Modal(c.Key("edit-dialog"), &e.modal, kit.ModalSpec{
		Open: e.open, Size: size, Title: title, CloseLabel: locales.CommonClose(),
		Body: func(c ui.C) {
			ui.Column(c.Key("form")).FillWidth().Gap(l.Space(4)).Children(func() {
				input := l.FieldInput(c.Key("name"), &e.name, kit.FieldSpec{Placeholder: locales.LibraryNamePlaceholder(), AutoFocus: true})
				if input.Submitted() {
					s.submitEdit()
				}
				if e.kind != editRenameTag {
					s.iconPicker(c)
				}
				cancel, confirm := s.formButtons(c, formButtons{
					Confirm: locales.CommonConfirm(), PadX: l.Space(4), ConfirmDisabled: jsTrim(e.name.Text()) == "",
				})
				if cancel {
					e.open = false
				}
				if confirm {
					s.submitEdit()
				}
			})
		},
	})
	if closed {
		e.open = false
	}
}
