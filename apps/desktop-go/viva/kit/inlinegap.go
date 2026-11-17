package kit

import "math"

// 行内块元素的基线间隙
//
// <input type="range">、<button> 这类 display: inline-block 的元素放在块容器里时，按基线与行内文字一起排：
// 它们所在的行盒还要容得下「支撑文字」（strut：容器的字体，行高 1.5）基线以下的那一截，于是元素下面会多出一块空白。
// 渲染层里最常遇到的是滑块（<input type="range" class="w-full">）与取色器触发钮的包装 div。
//
// 这一截的高度 = round(0.4 × 根字号)：根字号 14 / 16 / 18px 三档里，React 参照量到 6 / 6 / 7px，与公式相符
// （Chromium 把字体的下降量与半行距各自取整）。依据：2026-11-17 在 settings.appearance.bottom 的 light-royal-blue
// 三个字号档里，背景图调节面板里「标签行 + 间距 + 滑块」一块的高度比各部分之和多 6 / 6 / 7px；
// 取色器包装 div 比触发钮高 1.5 / 1.0 / 1.5px（按钮的基线在内容盒下沿，下沿以下的内边距与边框 4.5 / 5 / 5.5px
// 悬在基线之下，所以多出来的是这一截减去它）。参照文件在 D:\Project\GuiZhi-bench\parity\react-app2（不入库）。

// StrutDescent 是容器的支撑文字在基线以下的高度（逻辑像素）。
func (l *Look) StrutDescent() float32 { return float32(math.Round(0.4 * float64(l.RootPx))) }
