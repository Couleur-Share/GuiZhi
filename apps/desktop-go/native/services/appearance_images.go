package services

import "context"

// BackgroundImages 是显示设置里「背景图」的数据能力（渲染层 AppearanceSettings 的 handleSelectBackgroundImage 与 LocalImage 读图）：
// 选图并存入应用图片目录，以及按文件名读回已存的背景图。
//
// 文件名是应用图片目录里的纯文件名（image:save 生成的 UUID 加原扩展名），不含目录；界面状态里只存它，不存用户磁盘上的原路径。
type BackgroundImages interface {
	// PickAndSave 打开图片选择框并将选中的第一张图片存入应用图片目录，返回保存后的纯文件名。
	// 用户取消选择时返回 ("", nil)；选中了路径但保存没有产出文件名时返回非 nil 错误（渲染层此时弹「保存背景图失败」）。
	PickAndSave(ctx context.Context) (fileName string, err error)
	// Read 校验文件名安全后读取应用图片目录里对应背景图的原始字节。
	// 文件名含路径分隔符或「..」、扩展名不是位图（含 .svg）、文件不存在或读不出来都返回带原因的错误，绝不静默返回空字节。
	Read(ctx context.Context, fileName string) ([]byte, error)
}
