package main

// 对应 apps/desktop/src/main/index.ts 里的 window:* 频道：归知窗口是无边框的，
// 最小化/最大化/关闭按钮由渲染进程绘制，再经 IPC 回到主进程。

import (
	"context"
	"encoding/json"

	"github.com/egoist/mygo"
)

// registerWindow 在窗口创建后调用，把窗口控制类频道登记到 Bridge。
func (b *Bridge) registerWindow(win *mygo.Window) {
	b.sends["window:minimize"] = func([]json.RawMessage) { win.Minimize() }
	// 与 TS 一致：已最大化则还原，否则最大化
	b.sends["window:maximize"] = func([]json.RawMessage) {
		if win.IsMaximized() {
			win.Unmaximize()
		} else {
			win.Maximize()
		}
	}
	b.sends["window:close"] = func([]json.RawMessage) { win.Close() }
	b.sends["window:exitFullscreen"] = func([]json.RawMessage) { win.SetFullScreen(false) }

	b.handlers["window:isFullscreen"] = func(context.Context, []json.RawMessage) (any, error) {
		return win.IsFullScreen(), nil
	}
	b.handlers["window:isVisible"] = func(context.Context, []json.RawMessage) (any, error) {
		return win.IsVisible(), nil
	}
}
