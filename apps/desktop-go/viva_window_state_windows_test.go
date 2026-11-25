//go:build windows

package main

import "testing"

// 直接读 Win32 显示器信息（只读系统调用，不依赖任何事件循环）：至少有一台显示器，工作区宽高为正；
// 返回的工作区要能被现有的窗口记忆恢复逻辑使用。
func TestVivaDisplayWorkAreasReadsMonitors(t *testing.T) {
	areas := vivaDisplayWorkAreas()
	if len(areas) == 0 {
		t.Skip("当前会话没有可枚举的显示器（无头环境）")
	}
	for i, a := range areas {
		if a.Width <= 0 || a.Height <= 0 {
			t.Errorf("显示器 %d 的工作区无效：%+v", i, a)
		}
	}
	// 能放进主显示器工作区的窗口，恢复后位置与大小原样保留
	wa := areas[0]
	b := windowBounds{X: wa.X + 10, Y: wa.Y + 10, Width: min(1000, wa.Width), Height: min(700, wa.Height)}
	if fitted := fitBoundsToDisplays(b, areas); fitted == nil {
		t.Errorf("主显示器内的窗口应可恢复：%+v / %+v", b, areas)
	}
}
