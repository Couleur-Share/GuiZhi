package parity

import (
	"fmt"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
)

// 场景操作步骤的小工具：和渲染层采集器里 Playwright 的 click / wheel / mouse.move 一一对应。

// ClickRole 点击第一个角色与可访问名都匹配的控件的中心。
func ClickRole(s *headless.Session, role gfx.Role, name string) error {
	node, ok := s.AccessByRole(role, name)
	if !ok {
		return fmt.Errorf("找不到 %s「%s」", role, name)
	}
	return s.Click(node.Bounds.X+node.Bounds.Width/2, node.Bounds.Y+node.Bounds.Height/2)
}

// ScrollDown 在窗口坐标 (x, y) 处向下滚 delta 逻辑像素（超出内容就停在末端）。
func ScrollDown(s *headless.Session, x, y, delta float32) error {
	return s.Scroll(gfx.ScrollEvent{X: x, Y: y, DeltaY: delta, Precise: true})
}

// ParkPointer 把指针移到窗口左上角的空白处，避免任何控件处于悬停态（渲染层采集器同样这么做）。
func ParkPointer(s *headless.Session) error { return s.Move(2, 2) }
