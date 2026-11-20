package parity

import (
	"fmt"
	"strings"
	"time"

	"github.com/vivaui/viva/components"
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

// ClickRoleNth 点击第 n 个（从 0 起，按可访问树的顺序）角色与可访问名都匹配的控件的中心：
// 对应渲染层采集器里 getByRole(...).nth(n).click()。
func ClickRoleNth(s *headless.Session, role gfx.Role, name string, n int) error {
	seen := 0
	for _, node := range s.Access() {
		if node.Role != role || node.Label != name {
			continue
		}
		if seen == n {
			return s.Click(node.Bounds.X+node.Bounds.Width/2, node.Bounds.Y+node.Bounds.Height/2)
		}
		seen++
	}
	return fmt.Errorf("找不到第 %d 个 %s「%s」（只有 %d 个）", n, role, name, seen)
}

// MoveInRow 把指针移到「含有第一个角色与可访问名都匹配的控件」的那一行的内边距处：距行左上角 (dx, dy) 逻辑像素。
// 行是比控件大的无名按钮（命中区只用来感知悬停，渲染层里对应的是带 group/row 类的外框）：取包住控件中心的最大的那个。
// 对应渲染层采集器里「取外框、mouse.move(x + dx, y + dy)」。
func MoveInRow(s *headless.Session, role gfx.Role, name string, dx, dy float32) error {
	target, ok := s.AccessByRole(role, name)
	if !ok {
		return fmt.Errorf("找不到 %s「%s」", role, name)
	}
	cx, cy := target.Bounds.X+target.Bounds.Width/2, target.Bounds.Y+target.Bounds.Height/2
	var row gfx.Rect
	found := false
	for _, n := range s.Access() {
		b := n.Bounds
		if n.Role != gfx.RoleButton || n.Label != "" || cx < b.X || cx > b.X+b.Width || cy < b.Y || cy > b.Y+b.Height {
			continue
		}
		if !found || b.Width*b.Height > row.Width*row.Height {
			row, found = b, true
		}
	}
	if !found {
		return fmt.Errorf("「%s」不在任何行里", name)
	}
	return s.Move(row.X+dx, row.Y+dy)
}

// ScrollDown 在窗口坐标 (x, y) 处向下滚 delta 逻辑像素（超出内容就停在末端）。
func ScrollDown(s *headless.Session, x, y, delta float32) error {
	return s.Scroll(gfx.ScrollEvent{X: x, Y: y, DeltaY: delta, Precise: true})
}

// ParkPointer 把指针移到窗口左上角的空白处，避免任何控件处于悬停态（渲染层采集器同样这么做）。
func ParkPointer(s *headless.Session) error { return s.Move(2, 2) }

// ClearFocus 清掉键盘焦点，对应渲染层采集器 parkPointer 里的 activeElement.blur()。
//
// Viva 没有直接清焦点的接口，只有「按在空白处会清掉焦点」这条规则。这里在窗口左上角空白处按下、在窗外松开：
// 松开处没有控件，不构成点击，也就不会误触按下处的东西（模态遮罩的点击关闭等）。
// 已有焦点范围（模态对话框、弹层）时按在空白处不清焦点，这类场景的焦点由场景自己交代。
func ClearFocus(s *headless.Session) error {
	if err := s.Press(2, 2); err != nil {
		return err
	}
	return s.Release(-8, -8)
}

// ParkAndBlur 把指针移开并清掉焦点，是渲染层采集器 parkPointer 的完整对应（指针移出视口 + blur）。
// 行内「焦点在行内就浮出动作条」这类状态要靠它保持一致：鼠标点过行内按钮后，Chromium 采集前会清焦点，Viva 也必须清。
func ParkAndBlur(s *headless.Session) error {
	if err := ClearFocus(s); err != nil {
		return err
	}
	return ParkPointer(s)
}

// ParkCaretDark 把虚拟时钟推进到文本框插入符熄灭的相位中段，供取图用。
//
// 参照采集器把 caret-color 设成透明（scripts/parity/lib/react-state.mjs），参照图里没有插入符；Viva 的插入符聚焦后亮 530 ms、
// 暗 530 ms，周而复始（components.CaretBlinkHalf，20 s 无动静后常亮），取图时恰好亮着就多出一道深色竖线。
// 淡色文字（占位符）旁的一道深线会拉歪墨迹剖面（台账 G-028），所以焦点在文本框、文字又淡的场景，取图前调一次。
//
// since 是插入符开始计时的虚拟时刻：触发聚焦（或光标移动）的步骤开始之前读到的 s.Now()——步骤内部的事件处理不推进虚拟时钟。
// 插入符从第一帧「带着焦点画出来」起计时，而聚焦在事件之后的那一帧才生效，所以这里先出两帧（时钟不动），让上一步的聚焦先落地、
// 计时先开始，否则时钟一跳，计时反而从跳之后才起，取图时又恰好亮着。已经在暗相位中段（两侧各留 1/8 个半周期）则不再推进。
func ParkCaretDark(s *headless.Session, since time.Duration) error {
	for i := 0; i < 2; i++ {
		if err := s.Frame(); err != nil {
			return err
		}
	}
	half := components.CaretBlinkHalf
	period := 2 * half
	phase := (s.Now() - since) % period
	if phase < 0 {
		phase += period
	}
	margin := half / 8
	if phase >= half+margin && phase <= period-margin {
		return nil
	}
	target := half + half/2 // 暗相位正中
	advance := target - phase
	if advance < 0 {
		advance += period
	}
	return s.Advance(advance)
}

// hintHits 是窗口内、提示文字以任一前缀开头的命中区（声明了 Hint 的宿主）。
func hintHits(s *headless.Session, prefixes []string) []gfx.Hit {
	var out []gfx.Hit
	for _, hit := range s.Snapshot().Hits {
		if hit.Block {
			continue
		}
		for _, p := range prefixes {
			if strings.HasPrefix(hit.Hint, p) {
				out = append(out, hit)
				break
			}
		}
	}
	return out
}

func moveToCenter(s *headless.Session, hit gfx.Hit) error {
	return s.Move(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2)
}

// MoveOverRolePrefix 把指针移到第一个角色匹配、可访问名以 prefix 开头的控件的中心（按可访问树的顺序）：
// 对应渲染层采集器里 getByRole(role, { name: /^prefix/ }).first() 取外框后 mouse.move(外框中心)。
func MoveOverRolePrefix(s *headless.Session, role gfx.Role, prefix string) error {
	for _, n := range s.Access() {
		if n.Role == role && strings.HasPrefix(n.Label, prefix) {
			return s.Move(n.Bounds.X+n.Bounds.Width/2, n.Bounds.Y+n.Bounds.Height/2)
		}
	}
	return fmt.Errorf("找不到可访问名以「%s」开头的 %s", prefix, role)
}

// MoveOverRoleNth 把指针移到第 n 个（从 0 起，按可访问树的顺序）角色与可访问名都匹配的控件的中心：
// 对应渲染层采集器里 getByRole(...).nth(n) 取外框后 mouse.move(外框中心)。
func MoveOverRoleNth(s *headless.Session, role gfx.Role, name string, n int) error {
	seen := 0
	for _, node := range s.Access() {
		if node.Role != role || node.Label != name {
			continue
		}
		if seen == n {
			return s.Move(node.Bounds.X+node.Bounds.Width/2, node.Bounds.Y+node.Bounds.Height/2)
		}
		seen++
	}
	return fmt.Errorf("找不到第 %d 个 %s「%s」（只有 %d 个）", n, role, name, seen)
}

// PressRole 在第一个角色与可访问名都匹配的控件的中心按下鼠标并不松开（按下态）：
// 对应渲染层采集器里 mouse.move(外框中心) 加 mouse.down()。
func PressRole(s *headless.Session, role gfx.Role, name string) error {
	node, ok := s.AccessByRole(role, name)
	if !ok {
		return fmt.Errorf("找不到 %s「%s」", role, name)
	}
	return s.Press(node.Bounds.X+node.Bounds.Width/2, node.Bounds.Y+node.Bounds.Height/2)
}

// firstHint 是「提示文字以任一前缀开头」的第一个宿主：最靠上，同一高度取最靠左。
func firstHint(s *headless.Session, prefixes []string) (gfx.Hit, error) {
	hits := hintHits(s, prefixes)
	if len(hits) == 0 {
		return gfx.Hit{}, fmt.Errorf("找不到提示以 %q 开头的宿主", prefixes)
	}
	first := hits[0]
	for _, h := range hits[1:] {
		if h.Bounds.Y < first.Bounds.Y || (h.Bounds.Y == first.Bounds.Y && h.Bounds.X < first.Bounds.X) {
			first = h
		}
	}
	return first, nil
}

// MoveOverHint 把指针移到「提示文字以任一前缀开头」的第一个宿主（最靠上、再靠左）的中心：
// 对应渲染层采集器里对 [title^=…] 取 .first() 后 mouse.move(外框中心)。
func MoveOverHint(s *headless.Session, prefixes ...string) error {
	first, err := firstHint(s, prefixes)
	if err != nil {
		return err
	}
	return moveToCenter(s, first)
}

// MoveOverLowestHint 把指针移到「提示恰好是 hints 之一」的宿主里，完整可见且指针落得到的、下边缘最靠下的那个的中心：
// 对应渲染层采集器的 lowestVisibleRowName（下方放不下气泡时，提示翻到它上方）。
//   - 完整可见：外框高度达到这批宿主里最高的。Viva 的命中区按裁剪区收窄，被滚动区裁掉半截的行，命中区比完整的矮；
//     渲染层判的是「元素的外框整个落在滚动区里」，两者说的是同一件事（这批宿主是同样的单行标题，自然高度相同）。
//   - 落得到：中心点最上层的命中区是它自己，或是落在它外框里的子命中区。
func MoveOverLowestHint(s *headless.Session, hints []string) error {
	want := make(map[string]bool, len(hints))
	for _, h := range hints {
		want[h] = true
	}
	hits := s.Snapshot().Hits
	var hosts []gfx.Hit
	var tallest float32
	for _, h := range hits {
		if !h.Block && want[h.Hint] {
			hosts = append(hosts, h)
			tallest = max(tallest, h.Bounds.Height)
		}
	}
	var best gfx.Hit
	found := false
	for _, h := range hosts {
		if h.Bounds.Height < tallest-0.5 || !topmostIsWithin(hits, h) {
			continue
		}
		if !found || h.Bounds.Y+h.Bounds.Height > best.Bounds.Y+best.Bounds.Height {
			best, found = h, true
		}
	}
	if !found {
		return fmt.Errorf("窗口里没有完整可见且指针落得到的宿主（共 %d 个候选）", len(hosts))
	}
	return moveToCenter(s, best)
}

// topmostIsWithin 报告 host 外框中心点的最上层命中区是不是 host 自己，或落在 host 外框里的子命中区。
func topmostIsWithin(hits []gfx.Hit, host gfx.Hit) bool {
	return reachableAt(hits, host, host.Bounds.X+host.Bounds.Width/2, host.Bounds.Y+host.Bounds.Height/2)
}

// topmostAt 是指针落在 (x, y) 时最上层的命中区，第二个返回值为假表示那里没有。
// 从后往前找（后声明的在上），语义容器与不接收指针的区域不算；遮挡层（Block）算，它把下面的都挡住。
func topmostAt(hits []gfx.Hit, x, y float32) (gfx.Hit, bool) {
	for i := len(hits) - 1; i >= 0; i-- {
		h := hits[i]
		if h.Container || h.MovesWindow || h.IsLabel {
			continue
		}
		if x < h.Bounds.X || x >= h.Bounds.X+h.Bounds.Width || y < h.Bounds.Y || y >= h.Bounds.Y+h.Bounds.Height {
			continue
		}
		return h, true
	}
	return gfx.Hit{}, false
}

// reachableAt 报告指针落在 (x, y) 时，最上层的命中区是不是 host 自己，或落在 host 外框里的子命中区。
func reachableAt(hits []gfx.Hit, host gfx.Hit, x, y float32) bool {
	h, ok := topmostAt(hits, x, y)
	if !ok || h.Block {
		return false
	}
	return h.ID == host.ID || (h.Bounds.X >= host.Bounds.X-0.5 && h.Bounds.Y >= host.Bounds.Y-0.5 &&
		h.Bounds.X+h.Bounds.Width <= host.Bounds.X+host.Bounds.Width+0.5 &&
		h.Bounds.Y+h.Bounds.Height <= host.Bounds.Y+host.Bounds.Height+0.5)
}
