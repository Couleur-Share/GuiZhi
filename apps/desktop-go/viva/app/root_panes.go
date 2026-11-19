package app

import (
	"encoding/json"

	"guizhi-mygo-shell/native/settings"
)

// applyPanePrefs 把二级侧栏的宽度接到界面偏好上（渲染层 ui.store 的 sidebarPanelWidth，持久化在本地）：
// 启动时读已保存的宽度（夹在范围内；没有或坏了就留缺省），拖动、按键、双击定下新宽度后写回。
// 保存失败只记日志：偏好丢了下次启动回到缺省宽度，不影响当前使用。
func (r *Root) applyPanePrefs() {
	prefs := r.opts.Services.Prefs
	if prefs == nil {
		return
	}
	bounds := settings.PaneWidths[0]
	if raw, ok := prefs.Get(bounds.Key); ok {
		var width float64
		if json.Unmarshal(raw, &width) == nil {
			r.win.PanelResizer.Width = float32(bounds.Clamp(width))
		}
	}
	r.win.OnPanelWidth = func(width float32) {
		if err := prefs.Set(bounds.Key, float64(width)); err != nil {
			r.guard.Background("shell.panelWidth", err)
		}
	}
}
