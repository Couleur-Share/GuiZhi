package kit

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// 每个字符 10px 的度量：断行测试不依赖字体。
func tenPerRune(s string) float32 { return float32(len([]rune(s))) * 10 }

func TestTooltipRectPlacement(t *testing.T) {
	const vw, vh, w, h = 800, 600, 60, 30
	cases := []struct {
		name   string
		anchor gfx.Rect
		want   gfx.Rect
	}{
		// 居中对着宿主，放在下方 8px：宿主 {100,50,40,20}，中心 120，下边缘 70。
		{"below-centered", gfx.Rect{X: 100, Y: 50, Width: 40, Height: 20}, gfx.Rect{X: 90, Y: 78, Width: w, Height: h}},
		// 贴着左边缘：中心夹在 半宽 + 8 = 38，气泡左缘 = 8。
		{"clamp-left", gfx.Rect{X: 0, Y: 50, Width: 20, Height: 20}, gfx.Rect{X: 8, Y: 78, Width: w, Height: h}},
		// 贴着右边缘：中心夹在 800 − 30 − 8 = 762，气泡左缘 = 732。
		{"clamp-right", gfx.Rect{X: 790, Y: 50, Width: 20, Height: 20}, gfx.Rect{X: 732, Y: 78, Width: w, Height: h}},
		// 下方放不下（588 + 30 + 8 > 600）：翻到上方，下边缘离宿主上边缘 8px：560 − 8 − 30 = 522。
		{"flip-above", gfx.Rect{X: 100, Y: 560, Width: 40, Height: 20}, gfx.Rect{X: 90, Y: 522, Width: w, Height: h}},
		// 上方也放不下：贴着视口顶部的 8px 留白。
		{"flip-clamp-top", gfx.Rect{X: 100, Y: 5, Width: 40, Height: 20}, gfx.Rect{X: 90, Y: 8, Width: w, Height: h}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vhCase := float32(vh)
			if tc.name == "flip-clamp-top" {
				vhCase = 40 // 视口只有 40 高：下方（33 + 30 + 8）与上方都放不下
			}
			got := tooltipRect(tc.anchor, w, h, vw, vhCase)
			if got != tc.want {
				t.Fatalf("tooltipRect(%v) = %v，期望 %v", tc.anchor, got, tc.want)
			}
		})
	}
}

func TestTooltipLines(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		width float32
		want  []string
	}{
		{"fits", "保存 草稿", 100, []string{"保存 草稿"}},
		{"collapse-spaces", "a   b", 100, []string{"a b"}},
		{"trim", "  a b  ", 100, []string{"a b"}},
		{"words-wrap", "abc def ghi jkl", 100, []string{"abc def", "ghi jkl"}},
		{"cjk-wraps-anywhere", "一二三四五六七八九十一二", 100, []string{"一二三四五六七八九十", "一二"}},
		{"keeps-newlines", "a\nb", 100, []string{"a", "b"}},
		{"keeps-blank-line", "a\n\nb", 100, []string{"a", "", "b"}},
		{"breaks-overlong-word", "abcdefghijklmn", 100, []string{"abcdefghij", "klmn"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tooltipLines(tc.text, tc.width, tenPerRune); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("tooltipLines(%q, %v)\n got: %q\nwant: %q", tc.text, tc.width, got, tc.want)
			}
		})
	}
}

// tipPage 是一页散布着带提示的宿主：用来观察延迟、换宿主、按下、夹取与翻转。
type tipPage struct {
	st      TooltipState
	clicked int // 宿主 A 被点击的次数
	below   int // 没有提示的宿主 F（紧贴在 A 的气泡位置上）被点击的次数
}

const (
	tipA = "保存"
	tipB = "删除"
	tipC = "底部提示"
	tipD = "贴左边"
	tipE = "这是一段很长很长的提示文字，用来确认气泡最宽不超过二十个 rem，放不下的部分按断行机会折到下一行继续显示出来。"
	tipG = "整组提示"

	tipDesc = "辛的描述" // 宿主「辛」平时的可访问描述，同时也是它的提示文字
)

func (p *tipPage) view(l *Look, c ui.C) {
	ui.Column(c).Fill().Children(func() {
		host := func(key string, x, y, w, h float32, name, hint string) {
			hc := c.Key(key)
			id := ui.ID(hc)
			box := ui.Row(hc).Absolute().Left(x).Top(y).Size(w, h).HitWith(id, name, gfx.HitOptions{Role: gfx.RoleButton, Hint: hint})
			if box.Clicked() {
				switch key {
				case "a":
					p.clicked++
				case "f":
					p.below++
				}
			}
		}
		host("a", 20, 20, 80, 32, "甲", tipA)
		host("f", 20, 60, 80, 32, "己", "") // 正好在 A 的气泡（宿主下方 8px）所在的位置
		host("b", 200, 20, 80, 32, "乙", tipB)
		host("c", 20, 560, 80, 32, "丙", tipC)
		host("d", 0, 100, 20, 20, "丁", tipD)
		host("e", 300, 100, 80, 32, "戊", tipE)
		// 带可访问描述的宿主：描述平时在，被提示期间（悬停或聚焦）渲染层的 title 被摘走，描述也跟着没了。
		dc := c.Key("described")
		did := ui.ID(dc)
		ui.Row(dc).Absolute().Left(300).Top(300).Size(80, 32).
			HitWith(did, "辛", gfx.HitOptions{Role: gfx.RoleButton, Description: l.HintDescription(did, tipDesc), Hint: tipDesc})
		// 提示挂在容器上（渲染层的 title 在 div 上）：里面的按钮自己没有提示，指针落在它们身上也显示容器的提示。
		gc := c.Key("group")
		ui.Row(gc).Absolute().Left(400).Top(200).Size(200, 48).Gap(8).Pad(8).
			Draw(func(d *ui.DrawContext) { l.HintGroup(d.Canvas, ui.ID(gc), d.Rect, "分组", tipG) }).
			Children(func() {
				host2 := func(key, name string) {
					hc := c.Key(key)
					ui.Row(hc).Size(80, 32).HitWith(ui.ID(hc), name, gfx.HitOptions{Role: gfx.RoleButton})
				}
				host2("g1", "子一")
				host2("g2", "子二")
			})
	})
	l.Tooltips(c, &p.st)
}

func openTipPage(t *testing.T) (*harness, *tipPage) {
	t.Helper()
	p := &tipPage{}
	return openPage(t, still(), 800, 600, p.view), p
}

// moveTo 把指针移到页面坐标并出几帧（不推进时钟）。
func (h *harness) moveTo(x, y float32) {
	h.t.Helper()
	if err := h.session.Move(x, y); err != nil {
		h.t.Fatalf("移动指针到 (%g,%g) 失败：%v", x, y, err)
	}
	h.frames(3)
}

// advance 推进虚拟时钟。
func (h *harness) advance(d time.Duration) {
	h.t.Helper()
	if err := h.session.Advance(d); err != nil {
		h.t.Fatalf("推进 %v 失败：%v", d, err)
	}
}

// bubble 取气泡（名称是提示文字的分组）。
func (h *harness) bubble(text string) (gfx.Rect, bool) {
	n, ok := h.node(gfx.RoleGroup, text)
	return n.Bounds, ok
}

// 悬停 320 ms 才出气泡：差一毫秒都不出，动效关闭档也一样（它是防误触的延迟）。
func TestTooltipAppearsAfterDelay(t *testing.T) {
	h, _ := openTipPage(t)
	h.moveTo(60, 36)
	h.advance(TooltipDelay - time.Millisecond)
	if _, ok := h.bubble(tipA); ok {
		t.Fatal("悬停不足 320 ms 就出了气泡")
	}
	// 传感器在 320 ms 那一帧定下「该显示」，气泡在紧接着的下一帧声明（至多一帧的滞后，不超过一个刷新间隔）。
	h.advance(2 * time.Millisecond)
	h.frames(2)
	rect, ok := h.bubble(tipA)
	if !ok {
		t.Fatal("悬停 320 ms 后没有出气泡")
	}
	// 宿主 {20,20,80,32}：气泡水平居中、在下边缘（52）之下 8px。
	if want := float32(60); math.Abs(float64(rect.X+rect.Width/2-want)) > 0.51 {
		t.Errorf("气泡中心 x = %v，期望 %v", rect.X+rect.Width/2, want)
	}
	if want := float32(52 + 8); math.Abs(float64(rect.Y-want)) > 0.01 {
		t.Errorf("气泡上缘 y = %v，期望 %v", rect.Y, want)
	}
}

// 指针移开就收起。
func TestTooltipHidesWhenPointerLeaves(t *testing.T) {
	h, _ := openTipPage(t)
	h.moveTo(60, 36)
	h.advance(TooltipDelay + 10*time.Millisecond)
	if _, ok := h.bubble(tipA); !ok {
		t.Fatal("悬停 320 ms 后没有出气泡")
	}
	h.moveTo(500, 400)
	if _, ok := h.bubble(tipA); ok {
		t.Fatal("指针移开后气泡仍在")
	}
}

// 换到另一个宿主：旧气泡立刻收起，新宿主从头计 320 ms。
func TestTooltipRestartsOnNewHost(t *testing.T) {
	h, _ := openTipPage(t)
	h.moveTo(60, 36)
	h.advance(TooltipDelay + 10*time.Millisecond)
	if _, ok := h.bubble(tipA); !ok {
		t.Fatal("悬停 320 ms 后没有出气泡")
	}
	h.moveTo(240, 36)
	if _, ok := h.bubble(tipA); ok {
		t.Fatal("换到另一个宿主后，旧气泡仍在")
	}
	h.advance(TooltipDelay - 20*time.Millisecond)
	if _, ok := h.bubble(tipB); ok {
		t.Fatal("新宿主不足 320 ms 就出了气泡")
	}
	h.advance(40 * time.Millisecond)
	if _, ok := h.bubble(tipB); !ok {
		t.Fatal("新宿主悬停 320 ms 后没有出气泡")
	}
}

// 按下立刻收起，并且在指针离开宿主之前不再提示；离开再进入后恢复。
func TestTooltipSuppressedAfterPress(t *testing.T) {
	h, p := openTipPage(t)
	h.moveTo(60, 36)
	h.advance(TooltipDelay + 10*time.Millisecond)
	if _, ok := h.bubble(tipA); !ok {
		t.Fatal("悬停 320 ms 后没有出气泡")
	}
	if err := h.session.Press(60, 36); err != nil {
		t.Fatal(err)
	}
	h.frames(3)
	if _, ok := h.bubble(tipA); ok {
		t.Fatal("按下后气泡仍在")
	}
	if err := h.session.Release(60, 36); err != nil {
		t.Fatal(err)
	}
	h.frames(3)
	h.advance(2 * TooltipDelay)
	if _, ok := h.bubble(tipA); ok {
		t.Fatal("点击过的宿主在指针离开之前不应再提示")
	}
	if p.clicked != 1 {
		t.Fatalf("点击次数 = %d，期望 1", p.clicked)
	}
	h.moveTo(500, 400) // 离开
	h.moveTo(60, 36)   // 再进入
	h.advance(TooltipDelay + 10*time.Millisecond)
	if _, ok := h.bubble(tipA); !ok {
		t.Fatal("离开再进入后应当重新提示")
	}
}

// 气泡不挡指针（pointer-events-none）：它盖在另一个宿主 F 上时，点气泡的位置仍然点中背后的 F。
func TestTooltipDoesNotBlockPointer(t *testing.T) {
	h, p := openTipPage(t)
	h.moveTo(60, 36)
	h.advance(TooltipDelay + 10*time.Millisecond)
	rect, ok := h.bubble(tipA)
	if !ok {
		t.Fatal("没有出气泡")
	}
	x, y := rect.X+rect.Width/2, rect.Y+rect.Height/2
	if y < 60 || y > 92 {
		t.Fatalf("测试布局失效：气泡中心 (%g,%g) 不在宿主 F（y 60~92）上", x, y)
	}
	if err := h.session.Click(x, y); err != nil {
		t.Fatal(err)
	}
	h.frames(3)
	if p.below != 1 {
		t.Fatalf("点击气泡位置时背后的宿主被点中 %d 次，期望 1（气泡不应吸收指针）", p.below)
	}
}

// 下方放不下时翻到宿主上方。
func TestTooltipFlipsAboveNearBottom(t *testing.T) {
	h, _ := openTipPage(t)
	h.moveTo(60, 576)
	h.advance(TooltipDelay + 10*time.Millisecond)
	rect, ok := h.bubble(tipC)
	if !ok {
		t.Fatal("底部宿主没有出气泡")
	}
	if rect.Y+rect.Height > 560-8+0.01 {
		t.Fatalf("气泡应翻到宿主（上缘 560）上方 8px：%v", rect)
	}
}

// 贴着窗口左缘的宿主：气泡夹回 8px 留白以内。
func TestTooltipClampsToViewport(t *testing.T) {
	h, _ := openTipPage(t)
	h.moveTo(10, 110)
	h.advance(TooltipDelay + 10*time.Millisecond)
	rect, ok := h.bubble(tipD)
	if !ok {
		t.Fatal("贴边宿主没有出气泡")
	}
	if rect.X < 8-0.01 {
		t.Fatalf("气泡超出了左侧 8px 留白：%v", rect)
	}
}

// 长文字折行，最宽不超过 20rem（max-w-xs）。
func TestTooltipLongTextWraps(t *testing.T) {
	h, _ := openTipPage(t)
	h.moveTo(340, 116)
	h.advance(TooltipDelay + 10*time.Millisecond)
	rect, ok := h.bubble(tipE)
	if !ok {
		t.Fatal("长文字宿主没有出气泡")
	}
	// w-max max-w-xs：折行不缩窄气泡——不折行的宽度远超 320，所以折行后气泡也是整整 20rem，不随最宽一行缩水。
	if math.Abs(float64(rect.Width-320)) > 0.01 {
		t.Fatalf("气泡宽 %v，折行的气泡应当是 max-w-xs（320）", rect.Width)
	}
	if line := float32(12 * 1.625); rect.Height < 2*line+8+2 {
		t.Fatalf("长文字应当折成多行，气泡高 %v", rect.Height)
	}
}

// 键盘聚焦同样排期（focusin）：Tab 聚焦到宿主 320 ms 后出气泡。
func TestTooltipShowsOnKeyboardFocus(t *testing.T) {
	h, _ := openTipPage(t)
	h.keys("Tab")
	h.advance(TooltipDelay - time.Millisecond)
	if _, ok := h.bubble(tipA); ok {
		t.Fatal("聚焦不足 320 ms 就出了气泡")
	}
	h.advance(2 * time.Millisecond)
	h.frames(2)
	if _, ok := h.bubble(tipA); !ok {
		t.Fatal("聚焦 320 ms 后没有出气泡")
	}
}

// 提示挂在容器上时，指针落在容器里没有提示的子元素上也显示它（渲染层 closest("[title]")）；
// 在同一容器的子元素之间移动不重新计时、气泡也不跳；离开容器才收起。气泡对着容器的外框（不是子元素）。
func TestTooltipContainerHintAppliesToChildren(t *testing.T) {
	h, _ := openTipPage(t)
	// 容器 {400,200,200,48}，子元素一 {408,208,80,32}、子元素二 {496,208,80,32}。
	h.moveTo(448, 224)
	h.advance(TooltipDelay + 10*time.Millisecond)
	h.frames(2)
	rect, ok := h.bubble(tipG)
	if !ok {
		t.Fatal("指针在容器的子元素上，320 ms 后没有出容器的提示")
	}
	if mid := rect.X + rect.Width/2; math.Abs(float64(mid-500)) > 0.51 {
		t.Errorf("气泡中心 x = %v，期望对着容器的中心 500", mid)
	}
	if want := float32(200 + 48 + 8); math.Abs(float64(rect.Y-want)) > 0.51 {
		t.Errorf("气泡顶 y = %v，期望在容器下边缘之下 8px（%v）", rect.Y, want)
	}

	h.moveTo(536, 224) // 子元素二：同一个宿主
	if _, ok := h.bubble(tipG); !ok {
		t.Error("在同一容器的子元素之间移动，气泡不该收起或重新计时")
	}
	h.moveTo(700, 400)
	if _, ok := h.bubble(tipG); ok {
		t.Error("离开容器后气泡应当收起")
	}
}

// 渲染层的 title 同时是可访问描述；TooltipLayer 在悬停一开始就把它摘下来、移开才装回去：
// 宿主在被提示期间（包括气泡还没出的 320 ms 里）没有描述，移开后描述回来。
func TestTooltipHostLosesDescriptionWhileHinted(t *testing.T) {
	h, _ := openTipPage(t)
	desc := func() string {
		n, ok := h.node(gfx.RoleButton, "辛")
		if !ok {
			t.Fatal("没有找到宿主「辛」")
		}
		return n.Description
	}
	if got := desc(); got != tipDesc {
		t.Fatalf("平时的描述 = %q，期望 %q", got, tipDesc)
	}
	h.moveTo(340, 316) // 进入宿主：title 立刻被摘，气泡要等 320 ms
	if got := desc(); got != "" {
		t.Fatalf("悬停中（气泡未出）描述 = %q，期望为空", got)
	}
	h.advance(TooltipDelay + 10*time.Millisecond)
	h.frames(2)
	if _, ok := h.bubble(tipDesc); !ok {
		t.Fatal("宿主没有出气泡")
	}
	if got := desc(); got != "" {
		t.Fatalf("气泡显示期间描述 = %q，期望为空", got)
	}
	h.moveTo(700, 500) // 移开：title 装回去
	if got := desc(); got != tipDesc {
		t.Fatalf("移开后描述 = %q，期望 %q", got, tipDesc)
	}
}

// 气泡宽度是 w-max 的内容宽度（每个逻辑行不折行时最宽的一个）：只认换行符，空白折叠，不看折行结果。
func TestTooltipMaxContent(t *testing.T) {
	cases := []struct {
		name string
		text string
		want float32
	}{
		{"single", "abc", 30},
		{"widest-line", "ab\nabcde\nabc", 50},
		{"collapses-spaces", "a   b", 30}, // 折叠成 "a b"
		{"trims", "  ab  ", 20},
		{"long-line-not-wrapped", "abcdefghijklmnopqrst", 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tooltipMaxContent(tc.text, tenPerRune); got != tc.want {
				t.Fatalf("tooltipMaxContent(%q) = %v，期望 %v", tc.text, got, tc.want)
			}
		})
	}
}
