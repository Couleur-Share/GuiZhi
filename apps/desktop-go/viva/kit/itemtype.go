package kit

import (
	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/locales"
)

// ItemTypeMeta 是条目类型的显示信息（渲染层 components/library/type-meta.tsx 的 ITEM_TYPE_META）。
type ItemTypeMeta struct {
	// Label 是类型名（随界面语言）。
	Label string
	// Icon 是类型图标，渲染层用 h-3.5 w-3.5 画。
	Icon gfx.Icon
}

// GetItemTypeMeta 按类型取显示信息。入参是字符串而不是枚举：类型值直接来自数据库，
// 新版本写入的类型（例如后来新增的 forum）在旧版本里查不到，必须有兜底，而不是让整个列表出错。
func GetItemTypeMeta(itemType string) ItemTypeMeta {
	switch itemType {
	case "note":
		return ItemTypeMeta{locales.LibraryTypeNote(), icons.FileText}
	case "webpage":
		return ItemTypeMeta{locales.LibraryTypeWebpage(), icons.Globe}
	case "video":
		return ItemTypeMeta{locales.LibraryTypeVideo(), icons.Video}
	case "image":
		return ItemTypeMeta{locales.LibraryTypeImage(), icons.Image}
	case "audio":
		return ItemTypeMeta{locales.LibraryTypeAudio(), icons.Music}
	case "document":
		return ItemTypeMeta{locales.LibraryTypeDocument(), icons.File}
	case "snippet":
		return ItemTypeMeta{locales.LibraryTypeSnippet(), icons.Scissors}
	case "forum":
		return ItemTypeMeta{locales.LibraryTypeForum(), icons.MessagesSquare}
	}
	return ItemTypeMeta{locales.LibraryTypeUnknown(), icons.CircleHelp}
}
