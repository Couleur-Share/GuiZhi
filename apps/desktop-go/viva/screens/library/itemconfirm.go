package library

import (
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// itemConfirmView 声明「彻底删除 / 清空回收站」确认框（渲染层 ItemConfirmDialog）：每一帧都要经过 ConfirmDialog，
// 关着的那一帧它负责复位。点取消、点遮罩或按 Esc 关闭；确认或按 Enter 才真的执行，期间确认钮转圈、复选框禁用
// （忙碌中 Esc 与 Enter 不再起作用，点遮罩仍然关闭——渲染层没有禁它）。
func (s *Screen) itemConfirmView(c ui.C, _ View) {
	cf := &s.bulk.confirm
	var spec kit.ConfirmSpec
	if cf.open {
		spec = s.itemConfirmSpec(cf)
	}
	switch s.Look.ConfirmDialog(c.Key("item-confirm"), &cf.st, spec) {
	case kit.ConfirmAccepted:
		s.runConfirm()
	case kit.ConfirmDismissed:
		cf.open = false
	}
}

// itemConfirmSpec 是确认框的内容：标题按类型，说明按类型与条数；说明区是两段话加「同时清除历史问答中的相关证据」复选框
// （space-y-3 text-left）。
func (s *Screen) itemConfirmSpec(cf *itemConfirm) kit.ConfirmSpec {
	l := s.Look
	t := l.T
	title := locales.LibraryDeleteForever()
	message := locales.LibraryDeleteForeverConfirm()
	switch {
	case cf.kind == confirmEmptyTrash:
		title, message = locales.LibraryEmptyTrash(), locales.LibraryEmptyTrashConfirm()
	case len(cf.ids) > 1:
		message = locales.LibraryDeleteForeverConfirmMany(len(cf.ids))
	}
	hint, clearLabel := locales.LibraryDeleteEvidenceHint(), locales.LibraryDeleteEvidenceClear()
	busy := cf.busy
	return kit.ConfirmSpec{
		Open:        true,
		Title:       title,
		Message:     message,
		Description: message + " " + hint + " " + clearLabel,
		Body: func(c ui.C) {
			l.Text(c.Key("message"), message, kit.TextSM, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
			l.Text(c.Key("hint"), hint, kit.TextSM, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
			l.Checkbox(c.Key("clear"), &cf.clear, kit.CheckboxSpec{Name: clearLabel, Label: clearLabel, Disabled: busy})
		},
		ConfirmText: locales.CommonConfirm(),
		CancelText:  locales.CommonCancel(),
		Destructive: true,
		Loading:     busy,
	}
}
