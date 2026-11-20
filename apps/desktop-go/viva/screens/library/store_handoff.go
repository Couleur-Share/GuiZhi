package library

import (
	"context"
	"errors"
	"strconv"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// 「复制/导出给 AI」（渲染层 components/library/use-ai-handoff.ts）：详情页按钮与列表右键菜单共用。
// 右键菜单拿到的是列表投影（没有正文与转写），所以一律按 ID 重新取完整条目——这一步在服务层（services.Library.AiHandoff）。
// 提示不经变更失败通道（渲染层也不经 runGuardedMutation）：成功给一句话，失败给原因，不写日志。

// withThousands 是千分位：交接稿动辄几千上万字，8432 一眼读不出量级。
func withThousands(value int) string {
	digits := strconv.Itoa(value)
	neg := digits[0] == '-'
	if neg {
		digits = digits[1:]
	}
	out := make([]byte, 0, len(digits)+len(digits)/3+1)
	for i := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, digits[i])
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}

func (s *Store) toastError(message, detail string) {
	if s.guard.Notify != nil {
		s.guard.Notify.Error(message, detail)
	}
}

func (s *Store) toastSuccess(message string) {
	if s.guard.Notify != nil {
		s.guard.Notify.Success(message)
	}
}

// reportHandoff 提示取交接稿失败的原因：条目不存在与内容为空各有专门的一句、没有明细；
// 其余（读取条目本身失败、剪贴板与保存失败）用 failTitle 加原文明细。
func (s *Store) reportHandoff(err error, failTitle string) {
	switch {
	case errors.Is(err, services.ErrLibraryItemMissing):
		s.toastError(locales.LibraryAiHandoffMissing(), "")
	case errors.Is(err, services.ErrHandoffEmpty):
		s.toastError(locales.LibraryAiHandoffEmpty(), "")
	default:
		s.toastError(failTitle, err.Error())
	}
}

// CopyAiHandoff 把一条的交接稿复制到剪贴板（渲染层 copyToClipboard）：full 为假时是精简版（长文本被略去）。
func (s *Store) CopyAiHandoff(ctx context.Context, id string, full bool) {
	doc, err := s.svc.AiHandoff(ctx, id, full)
	if err == nil {
		err = s.guard.Notify.CopyText(doc.Text) // Notify 为空时直接写系统剪贴板
	}
	if err != nil {
		s.reportHandoff(err, locales.LibraryAiHandoffFailed())
		return
	}
	if doc.OmittedChars > 0 {
		s.toastSuccess(locales.LibraryAiHandoffCopiedBrief(withThousands(doc.CharCount), withThousands(doc.OmittedChars)))
		return
	}
	s.toastSuccess(locales.LibraryAiHandoffCopied(withThousands(doc.CharCount)))
}

// SaveAiHandoff 把一条的完整交接稿另存为 .md（渲染层 saveToFile）。另存为始终给完整版：存成文件就是为了让 AI 反复引用，
// 没有省字数的理由。用户取消对话框时不提示。
func (s *Store) SaveAiHandoff(ctx context.Context, id string) {
	doc, err := s.svc.AiHandoff(ctx, id, true)
	if err != nil {
		s.reportHandoff(err, locales.LibraryAiHandoffSaveFailed())
		return
	}
	result, err := s.svc.ExportAiHandoff(ctx, doc.Title, doc.Text)
	switch {
	case err != nil:
		s.toastError(locales.LibraryAiHandoffSaveFailed(), err.Error())
	case result.Canceled:
	case result.Success:
		s.toastSuccess(locales.LibraryAiHandoffSaved(result.FilePath))
	default:
		s.toastError(locales.LibraryAiHandoffSaveFailed(), result.Error)
	}
}
