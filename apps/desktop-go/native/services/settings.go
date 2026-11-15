package services

import (
	"encoding/json"

	"guizhi-mygo-shell/native/settings"
)

// Settings 是应用设置的读写入口。
//
// 设置的归属与存放见 native/settings 包说明：界面设置在 config/ui-prefs.json，
// Go 侧设置在 SQLite 的 settings 表；AI 连接、快捷键各有自己的服务，不在这里。
// 实现保证：任何一次写入要么整体生效，要么什么都不改并返回错误。
type Settings interface {
	// Snapshot 返回当前全部设置（已归一化）。
	Snapshot() (settings.Values, error)

	// Update 在当前值上应用 fn 并保存，返回保存后的值。fn 只改自己关心的字段，其余原样保留。
	// 校验不通过（FlareSolverr 非法、可执行文件路径没经过文件选择器等）返回错误，且不写入任何内容。
	// 没有变化时不写盘、不通知。
	Update(fn func(*settings.Values)) (settings.Values, error)

	// Subscribe 订阅设置变化；fn 在写入方的 goroutine 上同步调用，必须很快返回
	// （界面线程的更新用 Host.Invalidate 之类的方式转交）。返回取消函数，可重复调用。
	Subscribe(fn func(settings.Values)) (cancel func())
}

// Prefs 是界面偏好的键值存储：窗格宽度、列配置、阅读位置、已关闭的提示等，键见 settings.Pref*。
// 满足 kit.PrefStore（app 包里有编译期断言）。实现必须并发安全：去抖保存在定时器的 goroutine 里调用 Set。
type Prefs interface {
	Get(key string) (json.RawMessage, bool)
	Set(key string, value any) error
}
