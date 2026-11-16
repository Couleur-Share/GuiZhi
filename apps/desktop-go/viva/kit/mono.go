package kit

import "unicode/utf8"

// consolasAdvance 是 Consolas 的字距（em 的 1126/2048）。渲染层的 font-mono 在中文 Windows 上解析成它
// （React 采集的平台字体取证：Consolas；快捷键提示 11 个字符、10px 的排版宽度 60.48px = 11 × 5.4980）。
const consolasAdvance = 1126.0 / 2048.0

// MonoAdvance 是 Chromium 排一行 font-mono 文字的宽度：每个字符占一个字距（只适用于不含宽字符的文字）。
//
// Viva 的固定间距文字把字格宽度取整到整数设备像素（gfx.Canvas.MonoCellSize 的文档：rounded to the nearest），
// 5.498px 的字距被排成 5px，一行 11 个字符窄 5.48px。需要与渲染层同宽的版面（顶栏的快捷键提示）用这个值留出位置
// （台账 A-006）。
func MonoAdvance(s string, size float32) float32 {
	return float32(utf8.RuneCountInString(s)) * size * consolasAdvance
}
