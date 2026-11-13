package main

// IPC 频道启动自检：渲染进程（preload）能调用的频道必须都有处理器。
// 频道清单由 gen-channels.mts 从 preload 生成（ipc_channels_gen.go）；研究、平台发现、手机收集、
// 正文配图、阅读同步已从 preload 与渲染进程移除，不再出现在清单里，也不再有占位处理器。
// Startup self-check: every channel the renderer can reach must have a handler.

import "sort"

// unimplementedChannels 返回渲染进程能调用、却没有处理器的频道（正常应为空）。
func (b *Bridge) unimplementedChannels() []string {
	var out []string
	for _, ch := range rendererInvokeChannels {
		if b.handlers[ch] == nil {
			out = append(out, ch)
		}
	}
	for _, ch := range rendererSendChannels {
		if b.sends[ch] == nil {
			out = append(out, "(send) "+ch)
		}
	}
	sort.Strings(out)
	return out
}
